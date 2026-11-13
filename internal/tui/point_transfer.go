package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
	"gytest-go/internal/pointtable"
	"gytest-go/internal/projectfile"
)

type pointTransfer struct {
	path, format, kind, mode, revision string
	channel, device, socket            string
	export                             bool
}

func (m *model) pointTransferAvailable() bool {
	return m.view == 3 && m.connected && m.channel != "" && m.device != ""
}

func (m *model) beginPointTransfer(export bool) {
	t := pointTransfer{export: export, channel: m.channel, device: m.device, socket: m.socket, revision: m.detail.ConfigurationRevision}
	category := m.pointKind
	fields := []configurationField{{key: "path", label: "服务器文件路径"}}
	values := []string{""}
	title := "导入点表"
	if export {
		title = "导出点表"
		fields = append(fields, configurationField{"exportFormat", "格式", []string{"csv", "tsv", "json", "xlsx"}}, configurationField{"exportScope", "范围", []string{"all", "category"}})
		values = append(values, "csv", "all")
	} else {
		kinds := m.pointKinds()
		kind := m.pointKind
		if !containsChoice(kinds, kind) {
			kind = kinds[0]
		}
		fields = append(fields, configurationField{"tableMode", "方式", []string{"merge", "replace-kind", "replace-all"}}, configurationField{"pointKind", "缺省/替换类型", kinds})
		values = append(values, "merge", kind)
	}
	m.configurationEditor = toolEditor(title, "预览", fields, values, func(e *configurationEditor) error {
		path := strings.TrimSpace(e.value("path"))
		if path == "" {
			return errors.New("请填写文件路径")
		}
		if strings.HasPrefix(path, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			path = filepath.Join(home, path[2:])
		}
		t.path = path
		if export {
			t.format = e.value("exportFormat")
			t.kind = ""
			if e.value("exportScope") == "category" {
				t.kind = category
			}
			if filepath.Ext(t.path) == "" {
				t.path += "." + t.format
			} else if !strings.EqualFold(filepath.Ext(t.path), "."+t.format) {
				return errors.New("文件扩展名应与导出格式一致")
			}
		} else {
			t.kind, t.mode = e.value("pointKind"), e.value("tableMode")
		}
		op := configurationOperation{socket: t.socket, transfer: &t}
		m.pendingConfiguration, m.operationBusy = &op, true
		return nil
	})
	if export {
		m.configurationEditor.submitLabel = "导出"
	}
}

func performPointTransfer(ctx context.Context, t pointTransfer, reply *configurationResponse) {
	if t.export {
		points, err := readLivePointTable(ctx, t)
		if err == nil {
			var data []byte
			data, err = pointtable.Export(t.format, points)
			if err == nil && ctx.Err() == nil {
				err = writeNewPointTable(t.path, data)
			} else if err == nil {
				err = ctx.Err()
			}
		}
		reply.err = err
		reply.result.Message = "点表已导出 · " + t.path
		return
	}
	info, err := os.Stat(t.path)
	if err != nil {
		reply.err = err
		return
	}
	if !info.Mode().IsRegular() || info.Size() > pointtable.MaxBytes {
		reply.err = errors.New("请选择不超过 16 MB 的普通点表文件")
		return
	}
	f, err := os.Open(t.path)
	if err != nil {
		reply.err = err
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, pointtable.MaxBytes+1))
	if err != nil {
		reply.err = err
		return
	}
	rows, err := pointtable.Parse(t.path, data, t.kind)
	if err != nil {
		reply.err = err
		return
	}
	request := projectfile.Request{Resource: "point-table", Action: "preview", Revision: t.revision, Channel: t.channel, Device: t.device,
		Fields: projectfile.Fields(map[string]any{"table": projectfile.PointTableImport{Rows: rows, Kind: t.kind, Mode: t.mode}})}
	reply.view, reply.result, reply.err = observe.Configure(ctx, t.socket, request)
	if reply.err == nil {
		request.Action = "import"
		reply.preview = &pointTransferPreview{request: request, socket: t.socket, path: t.path, summary: reply.view.Value, rows: rows[:min(5, len(rows))]}
	}
}

func readLivePointTable(ctx context.Context, t pointTransfer) ([]core.Point, error) {
	query := url.Values{"channel": {t.channel}, "device": {t.device}, "kind": {t.kind}}
	points := []core.Point{}
	var metadata uint64
	for {
		query.Set("offset", strconv.Itoa(len(points)))
		info, err := observe.Fetch(ctx, t.socket, "/v1/view?"+query.Encode())
		if err != nil {
			return nil, err
		}
		if len(points) == 0 {
			metadata = info.View.Metadata
		}
		if metadata != info.View.Metadata || t.revision != info.ConfigurationRevision {
			return nil, projectfile.ErrConflict
		}
		if info.View.PointMatches > pointtable.MaxRows {
			return nil, errors.New("一次最多导出 100000 个点位")
		}
		found := false
		for _, d := range info.View.Devices {
			found = found || d.ID == t.device
		}
		if !found {
			return nil, errors.New("装置已不存在")
		}
		points = append(points, info.View.Points...)
		if len(points) == info.View.PointMatches {
			return points, nil
		}
		if len(info.View.Points) == 0 || len(points) > info.View.PointMatches {
			return nil, errors.New("点表分页已变化，请重新导出")
		}
	}
}

func writeNewPointTable(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".prs-point-table-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Link(f.Name(), path); os.IsExist(err) {
		return errors.New("文件已存在，请换一个文件名")
	}
	return err
}

type pointTransferPreview struct {
	request projectfile.Request
	socket  string
	path    string
	summary map[string]json.RawMessage
	rows    []core.Point
}

func (m *model) pointTransferPreviewKey(key string) {
	switch key {
	case "esc":
		m.pointPreview = nil
	case "enter":
		op := configurationOperation{socket: m.pointPreview.socket, request: m.pointPreview.request}
		m.pendingConfiguration, m.operationBusy = &op, true
	}
}

func (p *pointTransferPreview) body(width int) []string {
	lines := []string{clipped(p.path, width, true), fmt.Sprintf("新增 %s · 覆盖 %s · 删除 %s", p.summary["added"], p.summary["updated"], p.summary["removed"])}
	for _, row := range p.rows {
		line := fmt.Sprintf("%s %d · %g · %s", translated(row.Kind, kindLabels), row.Address, row.Value, row.Name)
		lines = append(lines, clipped(line, width, false))
	}
	return lines
}

func (m *model) applyPointTransfer(reply configurationResponse) {
	switch {
	case reply.err != nil:
		if reply.result.Saved {
			m.pointPreview, m.configurationEditor = nil, nil
			m.setOperationNotice("配置已保存 · " + reply.err.Error())
		} else if m.configurationEditor != nil {
			m.configurationEditor.error = reply.err.Error()
		} else {
			m.pointPreview = nil
			m.setOperationNotice(reply.err.Error())
		}
	case reply.preview != nil:
		m.configurationEditor = nil
		m.pointPreview = reply.preview
	default:
		m.pointPreview, m.configurationEditor = nil, nil
		m.setOperationNotice(reply.result.Message)
	}
}
