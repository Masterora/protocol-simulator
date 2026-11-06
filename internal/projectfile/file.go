// Package projectfile implements explicit, revision-checked project-file edits.
package projectfile

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gytest-go/internal/core"
)

var ErrConflict = errors.New("配置文件已被其他操作修改，请刷新后重新编辑")

type Document struct {
	Project  core.Project `json:"project"`
	Revision string       `json:"revision"`
	Path     string       `json:"path"`
}

func Revision(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func Read(path string) (Document, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Document{}, err
	}
	f, err := os.Open(absolute)
	if err != nil {
		return Document{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Document{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > core.MaxProjectFileBytes {
		return Document{}, errors.New("工程文件无效或过大")
	}
	data, err := io.ReadAll(io.LimitReader(f, core.MaxProjectFileBytes+1))
	if err != nil {
		return Document{}, err
	}
	p, err := core.ParseProject(data)
	if err != nil {
		return Document{}, err
	}
	return Document{p, Revision(data), absolute}, nil
}
func Encode(p core.Project) ([]byte, error) {
	p, err := core.NormalizeProject(p)
	if err != nil {
		return nil, err
	}
	p.Version = 2
	for i := range p.Channels {
		p.Channels[i].Points = nil
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')
	if int64(len(data)) > core.MaxProjectFileBytes {
		return nil, errors.New("工程文件过大")
	}
	return data, nil
}
func NewID() string {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}
func DefaultChannel(name string) core.Channel {
	c := core.Channel{ID: NewID(), Name: name, Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: core.DefaultListenIP() + ":2404", Devices: []core.Device{}}
	c.Defaults()
	return c
}
func Create(path, name string) (Document, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return Document{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 100 {
		return Document{}, errors.New("工程名称应为 1–100 字")
	}
	p := core.Project{Version: 2, Name: name, Channels: []core.Channel{DefaultChannel("通道1")}}
	data, err := Encode(p)
	if err != nil {
		return Document{}, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return Document{}, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Document{}, err
	}
	success := false
	defer func() {
		f.Close()
		if !success {
			os.Remove(path)
		}
	}()
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if err != nil {
		return Document{}, err
	}
	if err = f.Close(); err != nil {
		return Document{}, err
	}
	success = true
	return Document{p, Revision(data), path}, nil
}

// Commit preserves permissions and never creates or replaces a missing/symlinked
// target. The persistent sidecar lock is shared by cooperating CLI/server writers.
func Commit(path, expected string, p core.Project) (string, error) {
	if expected == "" {
		return "", errors.New("保存必须指定原文件修订号")
	}
	data, err := Encode(p)
	if err != nil {
		return "", err
	}
	release, err := lock(path + ".lock")
	if err != nil {
		return "", fmt.Errorf("无法锁定配置文件：%w", err)
	}
	defer release()
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("只允许编辑普通工程文件；符号链接或只读挂载请在源文件修改")
	}
	current, err := Read(path)
	if err != nil {
		return "", err
	}
	if current.Revision != expected {
		return "", ErrConflict
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".prs-config-*.tmp")
	if err != nil {
		return "", err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(info.Mode().Perm()); err == nil {
		err = preserveOwner(f, info)
	}
	if err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	// Recheck editors which do not participate in the sidecar lock.
	current, err = Read(path)
	if err != nil {
		return "", err
	}
	if current.Revision != expected {
		return "", ErrConflict
	}
	if err = os.Rename(name, path); err != nil {
		return "", err
	}
	return Revision(data), nil
}
