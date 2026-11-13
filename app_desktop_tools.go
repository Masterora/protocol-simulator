package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"gytest-go/internal/core"
	"gytest-go/internal/desktoptools"
	"gytest-go/internal/pointtable"
)

type PointTablePreview struct {
	Removed      int          `json:"removed"`
	Revision     uint64       `json:"revision"`
	SourceSHA256 string       `json:"sourceSHA256"`
	Total        int          `json:"total"`
	Existing     int          `json:"existing"`
	Added        int          `json:"added"`
	Updated      int          `json:"updated"`
	Rows         []core.Point `json:"rows"`
}

func readPointTable(data, name, kind string) ([]core.Point, string, error) {
	if len(data) > ((pointtable.MaxBytes+2)/3)*4 {
		return nil, "", errors.New("点表不能超过16MB")
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, "", errors.New("点表文件读取失败，请重新选择")
	}
	points, err := pointtable.Parse(name, b, kind)
	if err != nil {
		return nil, "", err
	}
	hash := sha256.Sum256(b)
	return points, hex.EncodeToString(hash[:]), nil
}
func (a *App) PreviewPointTable(channel, device, name, data, kind, mode string) (PointTablePreview, error) {
	current, err := a.engine.PointTable(channel, device)
	if err != nil {
		return PointTablePreview{}, err
	}
	rows, hash, err := readPointTable(data, name, kind)
	if err != nil {
		return PointTablePreview{}, err
	}
	removed, err := a.engine.CheckPointTable(channel, device, current.Revision, rows, mode, kind)
	if err != nil {
		return PointTablePreview{}, err
	}
	old := map[core.PointKey]bool{}
	for _, p := range current.Points {
		old[core.PointKey{Kind: p.Kind, Address: p.Address}] = true
	}
	added, updated := 0, 0
	for _, p := range rows {
		if old[core.PointKey{Kind: p.Kind, Address: p.Address}] {
			updated++
		} else {
			added++
		}
	}
	preview := rows
	if len(preview) > 100 {
		preview = preview[:100]
	}
	return PointTablePreview{Revision: current.Revision, SourceSHA256: hash, Total: len(rows), Existing: len(current.Points), Added: added, Updated: updated, Removed: removed, Rows: preview}, nil
}
func (a *App) ApplyPointTable(channel, device, name, data, kind, mode, expectedHash string, revision uint64) error {
	points, hash, err := readPointTable(data, name, kind)
	if err != nil {
		return err
	}
	if hash != expectedHash {
		return errors.New("导入文件已变化，请重新预览")
	}
	return a.engine.ImportPointTable(channel, device, revision, points, mode, kind)
}
func (a *App) ExportPointTable(channel, device, kind, format string) (string, error) {
	view, err := a.engine.PointTable(channel, device)
	if err != nil {
		return "", err
	}
	points := view.Points
	if kind != "" {
		points = nil
		for _, p := range view.Points {
			if p.Kind == kind || strings.HasPrefix(kind, "tab:") && (p.Tab == strings.TrimPrefix(kind, "tab:") || p.Tab == "" && p.Kind == strings.TrimPrefix(kind, "tab:")) {
				points = append(points, p)
			}
		}
	}
	b, err := pointtable.Export(format, points)
	if err != nil {
		return "", err
	}
	suggested := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`/\:*?"<>|`, r) {
			return '_'
		}
		return r
	}, view.Name) + "-点表." + format
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "导出点表", DefaultFilename: suggested, Filters: []runtime.FileFilter{{DisplayName: strings.ToUpper(format) + " 点表", Pattern: "*." + format}}})
	if err != nil || path == "" {
		return "", err
	}
	if !strings.EqualFold(filepath.Ext(path), "."+format) {
		path += "." + format
	}
	if err = core.AtomicWrite(path, b); err != nil {
		return "", fmt.Errorf("点表未导出：%w", err)
	}
	return path, nil
}
func (a *App) RunDesktopTool(request desktoptools.Request) (desktoptools.Result, error) {
	return desktoptools.Convert(request)
}
func (a *App) CopyDesktopText(text string) error {
	if len(text) > 1<<20 {
		return errors.New("复制内容不能超过1MB")
	}
	return runtime.ClipboardSetText(a.ctx, text)
}
