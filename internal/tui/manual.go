package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"gytest-go/internal/core"
)

type manualOperation struct {
	socket  string
	request core.ManualPointRequest
}
type manualReply struct {
	operation manualOperation
	result    core.ManualPointResult
	err       error
}
type pointEditor struct {
	operation manualOperation
	name      string
	values    [2]string
	dirty     [2]bool
	replace   [2]bool
	focus     int
	error     string
}

func (m *model) selectedPoint() (core.Point, bool) {
	id := ""
	if m.detailOpen && m.detailTarget.view == 3 {
		id = m.detailTarget.id
	} else {
		rows := m.rows()
		if len(rows) > 0 {
			id = rows[min(m.cursor, len(rows)-1)].id
		}
	}
	for _, p := range m.detail.View.Points {
		if p.Kind+":"+fmt.Sprint(p.Address) == id {
			return p, true
		}
	}
	return core.Point{}, false
}
func (m *model) manualTarget(action string) (manualOperation, core.Point, bool) {
	var operation manualOperation
	if m.view != 3 || !m.connected {
		m.setOperationNotice("请先选择已连接工程的点位")
		return operation, core.Point{}, false
	}
	if !m.detail.PointOperations {
		m.setOperationNotice("此工程仍为只读版本，请升级该工程的服务进程")
		return operation, core.Point{}, false
	}
	p, ok := m.selectedPoint()
	if !ok {
		m.setOperationNotice("请选择有效点位")
		return operation, p, false
	}
	protocol, role := "", ""
	for _, c := range m.detail.View.Channels {
		if c.ID == m.channel {
			protocol, role = c.Protocol, c.Role
		}
	}
	allowed := protocol == "iec104" && (p.Kind == "yx" || p.Kind == "yc") || strings.HasPrefix(protocol, "modbus-") && (p.Kind == "coil" || p.Kind == "discrete" || p.Kind == "holding" || p.Kind == "input")
	if role != "server" || !allowed {
		m.setOperationNotice("只支持子站数据点，不下发遥控、遥调")
		return operation, p, false
	}
	if action == "report" && protocol != "iec104" {
		m.setOperationNotice("Modbus 由主站读取，不支持主动上报")
		return operation, p, false
	}
	operation = manualOperation{m.socket, core.ManualPointRequest{Channel: m.channel, Device: m.device, Kind: p.Kind, Address: p.Address, Metadata: m.detail.View.Metadata, Action: action}}
	return operation, p, true
}
func (m *model) beginPointEdit() {
	operation, p, ok := m.manualTarget("set")
	if !ok {
		return
	}
	m.editor = &pointEditor{operation: operation, name: p.Name, values: [2]string{strconv.FormatFloat(p.Value, 'g', -1, 64), fmt.Sprint(p.Quality)}, replace: [2]bool{true, true}}
}
func (m *model) reportPoint() {
	if operation, _, ok := m.manualTarget("report"); ok {
		m.pendingOperation = &operation
		m.operationBusy = true
	}
}
func (m *model) setOperationNotice(s string) { m.operationNotice = s; m.operationNoticeAt = time.Now() }
func (m *model) editKey(key string) {
	e := m.editor
	if e == nil || m.operationBusy {
		return
	}
	i := e.focus
	switch key {
	case "esc":
		m.editor = nil
		return
	case "tab", "down", "up":
		e.focus = 1 - e.focus
		e.replace[e.focus] = true
		return
	case "ctrl-u":
		e.values[i] = ""
		e.dirty[i] = true
		e.replace[i] = false
	case "backspace":
		if e.replace[i] {
			e.values[i] = ""
		} else if len(e.values[i]) > 0 {
			runes := []rune(e.values[i])
			e.values[i] = string(runes[:len(runes)-1])
		}
		e.dirty[i] = true
		e.replace[i] = false
	case "enter":
		request := e.operation.request
		if e.dirty[0] {
			v, err := strconv.ParseFloat(strings.TrimSpace(e.values[0]), 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				e.error = "数值必须是有限数字"
				return
			}
			request.Value = &v
		}
		if e.dirty[1] {
			base := 10
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(e.values[1])), "0x") {
				base = 0
			}
			q, err := strconv.ParseUint(strings.TrimSpace(e.values[1]), base, 8)
			if err != nil {
				e.error = "品质应为 0–255 整数"
				return
			}
			b := byte(q)
			request.Quality = &b
		}
		if request.Value == nil && request.Quality == nil {
			m.editor = nil
			m.setOperationNotice("没有修改")
			return
		}
		operation := e.operation
		operation.request = request
		m.pendingOperation = &operation
		m.operationBusy = true
		return
	default:
		r, _ := utf8.DecodeRuneInString(key)
		used := len(e.values[i])
		if e.replace[i] {
			used = 0
		}
		if utf8.RuneCountInString(key) != 1 || unicode.IsControl(r) || used+len(key) > 64 {
			return
		}
		if e.replace[i] {
			e.values[i] = ""
		}
		e.values[i] += key
		e.dirty[i] = true
		e.replace[i] = false

	}
	e.error = ""
}
func (m *model) applyManual(reply manualReply) {
	m.operationBusy = false
	if reply.operation.socket != m.socket || reply.operation.request.Channel != m.channel || reply.operation.request.Device != m.device {
		m.editor = nil
		return
	}
	result := reply.result
	if reply.err != nil {
		prefix := "操作未完成："
		if result.Error == "" && result.Point == nil {
			prefix = "提交结果未确认，请查看最新点值："
		}
		if result.Applied {
			prefix = "点值已修改，上报未完成："
		}
		m.setOperationNotice(prefix + reply.err.Error())
		if m.editor != nil {
			if result.Applied {
				m.editor = nil
			} else {
				m.editor.error = reply.err.Error()
			}
		}
		return
	}
	m.editor = nil
	switch result.Mode {
	case "reported":
		m.setOperationNotice(fmt.Sprintf("已上报 %d 个连接", result.Receivers))
	case "offline":
		m.setOperationNotice("已修改运行值；连接未激活，未上报")
	case "polled":
		m.setOperationNotice("已修改运行值，主站下次读取生效")
	}
}
func (m *model) editorBody() []string {
	e := m.editor
	if e == nil {
		return nil
	}
	fields := make([]string, 2)
	for i, label := range []string{"数值", "品质"} {
		prefix := "  "
		if i == e.focus {
			prefix = "› "
		}
		fields[i] = prefix + label + "  " + e.values[i]
	}
	lines := fields
	if e.error != "" {
		lines = []string{e.error, fields[e.focus], fields[1-e.focus]}
	}
	lines = append(lines, "", fmt.Sprintf("%s · %s:%d", e.name, translated(e.operation.request.Kind, kindLabels), e.operation.request.Address))
	for _, p := range m.detail.View.Points {
		if p.Kind == e.operation.request.Kind && p.Address == e.operation.request.Address {
			lines = append(lines, fmt.Sprintf("当前值 %g · 品质 %d", p.Value, p.Quality))
			break
		}
	}
	lines = append(lines, "", "输入替换当前字段；Tab 或 ↑↓ 切换字段。", "自动规则继续运行，新值按实际变化同步。")
	if m.operationBusy {
		lines = append([]string{"正在提交，请等待实际结果…"}, lines...)
	}
	return lines
}
func (m *model) pointOperationsAvailable() (bool, bool) {
	if m.view != 3 || !m.connected || !m.detail.PointOperations {
		return false, false
	}
	p, ok := m.selectedPoint()
	if !ok {
		return false, false
	}
	for _, c := range m.detail.View.Channels {
		if c.ID == m.channel && c.Role == "server" {
			if c.Protocol == "iec104" {
				ok = p.Kind == "yx" || p.Kind == "yc"
				return ok, ok
			}
			if strings.HasPrefix(c.Protocol, "modbus-") {
				return p.Kind == "holding" || p.Kind == "input" || p.Kind == "coil" || p.Kind == "discrete", false
			}
		}
	}
	return false, false
}
