package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

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
	caret     textCursor
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
	channel := m.selectedChannel()
	protocol, role := channel.Protocol, channel.Role
	allowed := protocol == "iec104" && (p.Kind == "yx" || p.Kind == "yc") || strings.HasPrefix(protocol, "modbus-") && (p.Kind == "coil" || p.Kind == "discrete" || p.Kind == "holding" || p.Kind == "input")
	if role != "server" || !allowed {
		m.setOperationNotice("只支持子站数据点，不下发遥控、遥调")
		return operation, p, false
	}
	if action == "report" && protocol != "iec104" {
		m.setOperationNotice("Modbus 由主站读取，不支持主动上报")
		return operation, p, false
	}
	if action == "report" && channel.State.ReadyPeers == 0 {
		m.setOperationNotice(iec104ReportStatus(channel.State))
		return operation, p, false
	}
	operation = manualOperation{m.socket, core.ManualPointRequest{Channel: m.channel, Device: m.device, Kind: p.Kind, Address: p.Address, Metadata: m.detail.View.Metadata, Action: action}}
	return operation, p, true
}

func (m *model) selectedChannel() core.InspectChannel {
	for _, channel := range m.detail.View.Channels {
		if channel.ID == m.channel {
			return channel
		}
	}
	return core.InspectChannel{}
}

func iec104ReportStatus(state core.ChannelState) string {
	switch {
	case state.ReadyPeers > 0:
		return "IEC104 已激活，可上报"
	case state.Status == "stopped" || state.Status == "error":
		return "未上报：请在通道页启动通道"
	case state.Peers > 0:
		return "未上报：等待主站发送 STARTDT"
	default:
		return "未上报：等待主站连接"
	}
}

func (m *model) reportShortcut() string {
	if m.selectedChannel().State.ReadyPeers == 0 {
		return "[s]上报状态"
	}
	return "[s]上报"
}
func (m *model) beginPointEdit() {
	operation, p, ok := m.manualTarget("set")
	if !ok {
		return
	}
	persist := m.detail.ConfigurationOperations
	operation.request.Persist = &persist
	operation.request.Revision = m.detail.ConfigurationRevision
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
	case " ":
		choices := m.manualChoices(i)
		if len(choices) == 0 {
			return
		}
		label, field := "数值", "value"
		if i == 1 {
			label, field = "品质", "quality"
		}
		m.openChoices(label, field, e.values[i], choices, func(v string) {
			e.values[i], e.dirty[i], e.replace[i] = v, true, true
			e.caret, e.error = textCursor{}, ""
		})
		return
	case "esc":
		m.editor = nil
		return
	case "tab", "down", "up", "shift-tab":
		e.focus = 1 - e.focus
		e.replace[e.focus] = true
		e.caret = textCursor{}
		return
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
		if e.caret.key(&e.values[i], &e.replace[i], key, 64) {
			e.dirty[i] = true
			e.error = ""
		}
	}
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
	prefix := ""
	if result.Saved {
		prefix = "配置已保存；"
		m.detail.ConfigurationRevision = result.Revision
	}
	switch result.Mode {
	case "reported":
		m.setOperationNotice(prefix + "已上报")
	case "offline":
		m.setOperationNotice(prefix + "已修改运行值；连接未激活，未上报")
	case "polled":
		m.setOperationNotice(prefix + "已修改运行值，主站下次读取生效")
	}
}
func (m *model) editorBody(width int) []string {
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
		value := e.values[i]
		if i == e.focus {
			value = e.caret.render(value, min(width-7, 108)-12)
		} else if len(m.manualChoices(i)) > 0 {
			field := "value"
			if i == 1 {
				field = "quality"
			}
			value = "‹ " + choiceLabel(field, value) + " ›"
		}
		fields[i] = prefix + label + "  " + clipped(value, min(width-7, 108)-12, i == e.focus)
	}
	target := clipped(fmt.Sprintf("%s · %s:%d", e.name, translated(e.operation.request.Kind, kindLabels), e.operation.request.Address), min(width-7, 108)-4, false)
	lines := append([]string{target}, fields...)
	if e.error != "" {
		return []string{target, clipped("错误 "+e.error, min(width-7, 108)-4, false), fields[0], fields[1]}
	}
	if e.operation.request.Persist != nil && *e.operation.request.Persist {
		lines = append(lines, "修改会保存到工程 JSON。")
	} else {
		lines = append(lines, "仅修改运行值。")
	}
	for _, p := range m.detail.View.Points {
		if p.Kind == e.operation.request.Kind && p.Address == e.operation.request.Address {
			lines = append(lines, fmt.Sprintf("当前值 %g · 品质 %d", p.Value, p.Quality))
			break
		}
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

func (m *model) manualChoices(field int) []string {
	if field == 1 {
		return qualityChoices()
	}
	p, ok := m.selectedPoint()
	if !ok {
		return nil
	}
	switch p.Kind {
	case "yx":
		if p.WireType == 3 || p.WireType == 31 {
			return []string{"0", "1", "2", "3"}
		}
		return []string{"0", "1"}
	case "coil", "discrete":
		return []string{"0", "1"}
	}
	return nil
}
