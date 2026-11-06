//go:build devassets

package main

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed scripts/dev_reload.js
var developmentScript string

type DevelopmentState struct {
	Enabled bool   `json:"enabled"`
	Code    string `json:"code"`
	Styles  string `json:"styles"`
}
type developmentFS struct{ root string }

func developmentRoot() (string, error) {
	root := os.Getenv("PRS_DEV_ASSETS")
	if !filepath.IsAbs(root) {
		return "", errors.New("开发资源目录必须是绝对路径，请使用 npm run dev")
	}
	info, err := os.Stat(filepath.Join(root, "index.html"))
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fs.ErrInvalid
	}
	return root, nil
}
func developmentAssets(fs.FS) (fs.FS, error) {
	root, err := developmentRoot()
	return developmentFS{root}, err
}
func developmentProjectLaunch() bool { _, err := developmentRoot(); return err == nil }
func (d developmentFS) read(name string) ([]byte, error) {
	if !fs.ValidPath(name) || strings.Contains(name, "/") {
		return nil, fs.ErrNotExist
	}
	switch filepath.Ext(name) {
	case ".html", ".css", ".js", ".svg":
	default:
		return nil, fs.ErrNotExist
	}
	info, err := os.Lstat(filepath.Join(d.root, name))
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fs.ErrPermission
	}
	return os.ReadFile(filepath.Join(d.root, name))
}
func (d developmentFS) state() (DevelopmentState, error) {
	entries, err := os.ReadDir(d.root)
	if err != nil {
		return DevelopmentState{}, err
	}
	code, styles := sha256.New(), sha256.New()
	for _, entry := range entries {
		name := entry.Name()
		switch filepath.Ext(name) {
		case ".html", ".css", ".js", ".svg":
		default:
			continue
		}
		b, err := d.read(name)
		if err != nil {
			return DevelopmentState{}, err
		}
		h := code
		if filepath.Ext(name) == ".css" {
			h = styles
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(b)
	}
	return DevelopmentState{true, hex.EncodeToString(code.Sum(nil)), hex.EncodeToString(styles.Sum(nil))}, nil
}
func (*App) GetDevelopmentState() (DevelopmentState, error) {
	root, err := developmentRoot()
	if err != nil {
		return DevelopmentState{}, err
	}
	return (developmentFS{root}).state()
}
func (d developmentFS) Open(name string) (fs.File, error) {
	if name == "." {
		info, err := os.Stat(d.root)
		if err != nil {
			return nil, err
		}
		all, err := os.ReadDir(d.root)
		if err != nil {
			return nil, err
		}
		entries := []fs.DirEntry{}
		for _, entry := range all {
			if entry.Type().IsRegular() {
				switch filepath.Ext(entry.Name()) {
				case ".html", ".css", ".js", ".svg":
					entries = append(entries, entry)
				}
			}
		}
		return &developmentDirectory{info: info, entries: entries}, nil
	}

	b, err := d.read(name)
	if err != nil {
		return nil, err
	}
	if name == "index.html" {
		state, err := d.state()
		if err != nil {
			return nil, err
		}
		initial, _ := json.Marshal(state)
		script := strings.Replace(developmentScript, "__PRS_INITIAL_REVISION__", string(initial), 1)
		b = bytes.Replace(b, []byte("</head>"), []byte("<script>"+script+"</script></head>"), 1)
	}
	return &developmentFile{Reader: bytes.NewReader(b), name: name, size: int64(len(b))}, nil
}

type developmentFile struct {
	*bytes.Reader
	name string
	size int64
}

func (*developmentFile) Close() error                 { return nil }
func (f *developmentFile) Stat() (fs.FileInfo, error) { return developmentInfo{f.name, f.size}, nil }

type developmentInfo struct {
	name string
	size int64
}

func (f developmentInfo) Name() string     { return f.name }
func (f developmentInfo) Size() int64      { return f.size }
func (developmentInfo) Mode() fs.FileMode  { return 0444 }
func (developmentInfo) ModTime() time.Time { return time.Time{} }
func (developmentInfo) IsDir() bool        { return false }
func (developmentInfo) Sys() any           { return nil }

// Reports successful WebView loads to the development terminal only.
func (*App) ReportDevelopmentReload(kind, revision string) {
	if kind != "page" && kind != "styles" {
		return
	}
	if b, err := hex.DecodeString(revision); err != nil || len(b) != sha256.Size {
		return
	}
	fmt.Printf("[dev %d] %s %s\n", os.Getpid(), kind, revision)
}

type developmentDirectory struct {
	info    fs.FileInfo
	entries []fs.DirEntry
	offset  int
}

func (d *developmentDirectory) Stat() (fs.FileInfo, error) { return d.info, nil }
func (*developmentDirectory) Close() error                 { return nil }
func (*developmentDirectory) Read([]byte) (int, error)     { return 0, fs.ErrInvalid }
func (d *developmentDirectory) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.offset >= len(d.entries) && n > 0 {
		return nil, io.EOF
	}
	end := len(d.entries)
	if n > 0 && d.offset+n < end {
		end = d.offset + n
	}
	items := d.entries[d.offset:end]
	d.offset = end
	return items, nil
}
