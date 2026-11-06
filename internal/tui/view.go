package tui

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/rivo/uniseg"
	"gytest-go/internal/attribution"
	"gytest-go/internal/core"
)

const (
	applicationName = "Protocol-Simulator"
	reset           = "\x1b[0m"
	cyan            = "\x1b[38;5;81m"
	purple          = "\x1b[38;5;141m"
	muted           = "\x1b[38;5;246m"
	white           = "\x1b[38;5;252m"
	green           = "\x1b[38;5;114m"
	yellow          = "\x1b[38;5;221m"
	red             = "\x1b[38;5;203m"
	selected        = "\x1b[1;38;5;231;48;5;24m"
	active          = "\x1b[1;38;5;231;48;5;60m"
)

var viewLabels = []string{"工程", "通道", "装置", "点位", "规则", "报文", "日志"}

type row struct {
	id, text string
	cells    []string
	tone     string
}

const tableColumnGap = 5

func tableGap(width int) int {
	if width < 100 {
		return 3
	}
	return tableColumnGap
}

type column struct {
	title string
	width int
}

func commandAlias(s string) int {
	aliases := [][]string{{"project", "prj", "ctx"}, {"channel", "ch"}, {"device", "dev"}, {"point", "pt"}, {"rule", "r"}, {"frame", "f"}, {"log", "l"}}
	for i, names := range aliases {
		for _, name := range names {
			if name == s || s == viewLabels[i] {
				return i
			}
		}
	}
	return -1
}
func resourceRow(id, tone string, cells ...string) row {
	return row{id, strings.Join(cells, "  "), cells, tone}
}
func byteCount(n uint64) string {
	if n < 1024 {
		return fmt.Sprint(n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1fK", float64(n)/1024)
	}
	return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
}
func (m *model) rows() []row {
	var rows []row
	switch m.view {
	case 0:
		for _, item := range m.instances {
			state, tone := "运行中", green
			name, path := item.info.View.Name, item.info.View.Path
			if item.err != nil {
				state, tone = "不可用", red
				name = filepath.Base(item.socket)
				path = connectionMessage(item.err)
			}
			rows = append(rows, resourceRow(item.socket, tone, fmt.Sprint(item.info.PID), name, state, fmt.Sprint(len(item.info.View.Channels)), path))
		}
	case 1:
		for _, c := range m.detail.View.Channels {
			tone := green
			if c.State.LastError != "" {
				tone = red
			} else if c.State.Status != "connected" {
				tone = yellow
			}
			rows = append(rows, resourceRow(c.ID, tone, c.Name, strings.ToUpper(c.Protocol), translated(c.Role, roleLabels), translated(c.Transport, transportLabels), translated(c.State.Status, stateLabels), fmt.Sprint(c.State.Peers), byteCount(c.State.RX), byteCount(c.State.TX), m.channelAddress(c)))
		}
	case 2:
		for _, d := range m.detail.View.Devices {
			rows = append(rows, resourceRow(d.ID, white, d.Name, fmt.Sprint(d.Unit), fmt.Sprint(d.CommonAddress), fmt.Sprint(d.Points), d.ID))
		}
	case 3:
		for _, p := range m.detail.View.Points {
			tone := white
			if p.Quality != 0 {
				tone = yellow
			}
			id := p.Kind + ":" + fmt.Sprint(p.Address)
			binding := "—"
			if mappings, ok := m.detail.View.PointBindings[id]; ok {
				binding = bindingText(mappings, m.detail.View.BindingTargetNames)
			}
			r := resourceRow(id, tone, fmt.Sprint(p.Address), fmt.Sprintf("0x%X", p.Address), translated(p.Kind, kindLabels), fmt.Sprintf("%.8g", p.Value), fmt.Sprint(p.Quality), formatPointTime(p.CommunicationAt, time.TimeOnly), p.Name)
			if len(m.detail.View.PointBindings) > 0 {
				r.cells = append(r.cells, binding)
				r.text += "  " + binding
			}
			rows = append(rows, r)
		}
	case 4:
		if s := m.detail.View.Rules; s != nil {
			for i, r := range s.Config.Rules {
				n := uint64(0)
				if i < len(s.Cycles) {
					n = s.Cycles[i]
				}
				status, tone := ruleRuntimeState(s, i)
				label := translated(status, stateLabels)
				rows = append(rows, resourceRow(fmt.Sprint(i), tone, fmt.Sprint(i+1), label, translated(r.Kind, kindLabels), translated(r.Mode, modeLabels), fmt.Sprintf("%g 秒", float64(r.IntervalMS)/1000), fmt.Sprint(n), r.Points))
			}
		}
	case 5:
		for _, message := range m.messages {
			if (message.Direction != "RX" && message.Direction != "TX") || (m.channel != "" && message.ChannelID != m.channel) {
				continue
			}
			tone := cyan
			if message.Direction == "TX" {
				tone = green
			}
			rows = append(rows, resourceRow(fmt.Sprint(message.Sequence), tone, message.Time, translated(message.Direction, directionLabels), message.Peer, message.Summary, message.Hex))
		}
	case 6:
		for _, e := range m.logs {
			tone := white
			if e.Level == "WARN" {
				tone = yellow
			} else if e.Level == "ERROR" {
				tone = red
			}
			rows = append(rows, resourceRow(fmt.Sprint(e.Sequence), tone, e.Time.Local().Format("15:04:05.000"), translated(e.Level, levelLabels), translated(e.Message, eventLabels), logFields(e.Message, e.Fields)))
		}
	}
	if m.filter != "" && m.view != 3 {
		matched := rows[:0]
		for _, r := range rows {
			if strings.Contains(strings.ToLower(r.text), strings.ToLower(m.filter)) {
				matched = append(matched, r)
			}
		}
		rows = matched
	}
	return rows
}
func (m *model) channelAddressTitle() string {
	title := ""
	for _, c := range m.detail.View.Channels {
		label := addressLabel(c.Transport)
		if title != "" && title != label {
			return "通信地址"
		}
		title = label
	}
	if title == "" {
		return "地址"
	}
	return title
}
func (m *model) channelAddress(c core.InspectChannel) string {
	if m.channelAddressTitle() == "通信地址" {
		return addressLabel(c.Transport) + "：" + c.Address
	}
	return c.Address
}
func (m *model) columns(width int) ([]column, []int) {
	var cols []column
	switch m.view {
	case 0:
		cols = []column{{"进程", 8}, {"工程", 9}, {"状态", 7}, {"通道数", 7}, {"配置文件", 14}}
	case 1:
		cols = []column{{"名称", 9}, {"协议", 7}, {"角色", 5}, {"连接方式", 12}, {"状态", 7}, {"连接数", 7}, {"接收量", 7}, {"发送量", 7}, {m.channelAddressTitle(), 20}}
	case 2:
		cols = []column{{"装置", 12}, {"站号", 5}, {"公共地址", 9}, {"点位数", 7}, {"标识", 20}}
	case 3:
		cols = []column{{"点位", 9}, {"十六进制", 9}, {"类型", 11}, {"数值", 14}, {"品质", 5}, {"上报时间", 10}, {"描述", 9}}
		if len(m.detail.View.PointBindings) > 0 {
			cols = append(cols, column{"绑定点位", 14})
		}
	case 4:
		cols = []column{{"序号", 5}, {"状态", 7}, {"类型", 11}, {"模式", 5}, {"周期", 7}, {"执行次数", 9}, {"点位范围", 9}}
	case 5:
		cols = []column{{"时间", 13}, {"方向", 5}, {"对端", 20}, {"摘要", 9}, {"原始报文", 17}}
	case 6:
		cols = []column{{"时间", 13}, {"级别", 5}, {"事件", 9}, {"字段", 12}}
	}
	indices := make([]int, len(cols))
	for i := range indices {
		indices[i] = i
	}
	remove := func(original int) {
		for i, n := range indices {
			if n == original {
				indices = append(indices[:i], indices[i+1:]...)
				return
			}
		}
	}
	switch m.view {
	case 0:
		remove(3)
	case 1:
		remove(5)
		remove(6)
		remove(7)
	case 2:
		remove(3)
	case 3:
		if len(m.detail.View.PointBindings) > 0 {
			indices = []int{0, 7, 3, 5, 6}
		}
	case 4:
		remove(0)
		remove(5)
	}
	if m.view == 2 {
		protocol := m.pointProtocol()
		if protocol == "iec104" {
			remove(1)
		} else if strings.HasPrefix(protocol, "modbus-") {
			remove(2)
		}
	}
	if width < 70 {
		switch m.view {
		case 3:
			remove(1)
		case 5:
			remove(4)
		}
	}
	// Headers and data use the same terminal-cell widths. Short fields stay
	// compact; longer fields share remaining space instead of being clipped
	// while a neighbouring fixed-width column is mostly empty.
	gap := tableGap(width)
	natural := make([]int, len(cols))
	for i, c := range cols {
		natural[i] = uniseg.StringWidth(c.title) + gap
	}
	for _, r := range m.rows() {
		for i, value := range r.cells {
			natural[i] = max(natural[i], uniseg.StringWidth(sanitize(value))+gap)
		}
	}
	total := 0
	for _, i := range indices {
		cols[i].width = max(uniseg.StringWidth(cols[i].title)+gap, min(cols[i].width, natural[i]))
		total += cols[i].width
	}
	for total > width {
		changed := false
		for _, i := range indices {
			if total <= width {
				break
			}
			if cols[i].width > uniseg.StringWidth(cols[i].title)+gap {
				cols[i].width--
				total--
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	for total < width {
		changed := false
		for _, i := range indices {
			if total >= width {
				break
			}
			if cols[i].width < natural[i] {
				cols[i].width++
				total++
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	// Surplus padding belongs after the final field, never between values.
	if len(indices) > 0 && total < width {
		cols[indices[len(indices)-1]].width += width - total
	}
	return cols, indices
}
func cellsLine(cells []string, cols []column, indices []int) string {
	var s strings.Builder
	width := 0
	for _, i := range indices {
		width += cols[i].width
	}
	gap := tableGap(width)
	for _, i := range indices {
		v := ""
		if i < len(cells) {
			v = cells[i]
		}
		s.WriteString(fit(clipped(v, max(0, cols[i].width-gap), cols[i].title == "配置文件"), max(0, cols[i].width-gap)))
		s.WriteString(strings.Repeat(" ", gap))
	}
	return s.String()
}
func (m *model) scope() (project, channel, device string) {
	project = m.detail.View.Name
	if project == "" {
		project = "—"
	}
	channel, device = "—", "—"
	for _, c := range m.detail.View.Channels {
		if c.ID == m.channel {
			channel = c.Name + " · " + translated(c.Role, roleLabels)
		}
	}
	for _, d := range m.detail.View.Devices {
		if d.ID == m.device {
			device = d.Name
		}
	}
	return
}
func painted(text string, width int, tone string) string { return tone + fit(text, width) + reset }
func projectUptime(started, now time.Time) string {
	if started.IsZero() || now.Before(started) {
		return ""
	}
	seconds := int64(now.Sub(started) / time.Second)
	return fmt.Sprintf("%dd %02d:%02d:%02d", seconds/86400, seconds/3600%24, seconds/60%60, seconds%60)
}

func splitLine(left, right string, width int, tone string) string {
	right = sanitize(right)
	rw := uniseg.StringWidth(right)
	if width-rw < 12 {
		return painted(clipped(left, width, false), width, tone)
	}
	return tone + fit(clipped(left, width-rw, false), width-rw) + right + reset
}
func (m *model) emptyMessage() string {
	if m.filter != "" {
		return "没有匹配结果 · Esc 清除筛选"
	}
	if !m.discovered {
		return "正在发现运行中的工程…"
	}
	if len(m.instances) == 0 {
		return "未发现运行工程 · 请先启动工程，并确认运行用户与观测目录相同"
	}
	if m.socket == "" {
		return "正在选择工程…"
	}
	if !m.connected {
		return "正在连接所选工程…"
	}
	switch m.view {
	case 1:
		return "此工程没有通道"
	case 2:
		return "此通道没有装置"
	case 3:
		if m.detail.View.PointKinds[m.pointKind] > 0 {
			return "正在读取所选分类…"
		}
		return "此分类暂无点位"
	case 4:
		return "此装置没有自动上报规则"
	case 5:
		return "等待通道收发报文…"
	case 6:
		return "暂无运行日志"
	}
	return "暂无工程"
}
func (m *model) render(width, height int) string {
	if width < 60 || height < 12 {
		return "\x1b[H\x1b[2J终端太小，请调整至至少 60 列、12 行。q 退出，工程继续运行\r\n"
	}
	w := width - 1
	project, channel, device := m.scope()
	status, tone := "", green
	if !m.discovered {
		status, tone = "正在发现", yellow
	} else if m.socket == "" {
		status, tone = "等待工程", yellow
	}
	if m.socket != "" && !m.connected {
		status, tone = "正在连接", yellow
		if m.notice != "" {
			status, tone = "连接中断", red
		}
	}
	lines := []string{
		splitLine(" 工程  "+project, m.accessLabel(), w, purple),
		m.shortcutHeader(" 通道  "+channel, w),
		painted(clipped(" 装置  "+device, w, false), w, tone),
	}
	labels := make([]string, len(viewLabels))
	for i, n := range viewLabels {
		labels[i] = fmt.Sprintf("%d:%s", i+1, n)
	}
	lines = append(lines, navigation(labels, m.view, w, " "))
	rows := m.rows()
	if len(rows) > 0 {
		m.cursor = min(m.cursor, len(rows)-1)
	} else {
		m.cursor = 0
	}
	available := height - len(lines) - 5
	title := fmt.Sprintf(" %s ", viewLabels[m.view])
	top := "┌" + title + strings.Repeat("─", max(0, w-2-uniseg.StringWidth(title))) + "┐"
	if m.view == 3 {
		prefix := "┌ 点位 ·"
		top = cyan + prefix + reset + m.pointNavigation(w-uniseg.StringWidth(prefix)-1) + cyan + "┐" + reset
		lines = append(lines, top)
	} else {
		lines = append(lines, painted(top, w, cyan))
	}
	numberWidth := m.numberWidth(len(rows))
	cols, indices := m.columns(w - 4 - numberWidth)
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.title
	}
	lines = append(lines, cyan+"│"+yellow+fit("  "+strings.Repeat(" ", numberWidth)+cellsLine(headers, cols, indices), w-2)+cyan+"│"+reset)
	start := max(0, m.cursor-available+1)
	for n := 0; n < available; n++ {
		i := start + n
		text, tone := "", white
		if i < len(rows) {
			r := rows[i]
			prefix := "  "
			tone = r.tone
			if i == m.cursor {
				prefix = "› "
				tone = selected
			}
			text = prefix + fit(fmt.Sprint(m.rowNumber(i)), numberWidth) + cellsLine(r.cells, cols, indices)
		} else if len(rows) == 0 && n == 0 {
			text = "  " + m.emptyMessage()
			tone = muted
		}
		lines = append(lines, cyan+"│"+painted(text, w-2, tone)+cyan+"│"+reset)
	}
	bottom := "└" + strings.Repeat("─", w-2) + "┘"
	lines = append(lines, painted(bottom, w, cyan))

	credit := attribution.Label()
	brandWidth := max(uniseg.StringWidth(applicationName), uniseg.StringWidth(credit))
	leftWidth := w - brandWidth - 1
	lines = append(lines, paintShortcuts(m.actionHints(leftWidth), leftWidth)+" "+muted+fit(applicationName, brandWidth)+reset)
	prompt := status
	if m.notice != "" {
		prompt = m.notice
		tone = red
	} else if m.operationNotice != "" && time.Since(m.operationNoticeAt) < 8*time.Second {
		prompt = m.operationNotice
	} else if status == "" {
		prompt = projectUptime(m.detail.Started, time.Now())
	}
	if m.filter != "" {
		if prompt != "" {
			prompt += " · "
		}
		prompt += "/ " + m.filter
	}
	prompt = " " + prompt
	if m.mode != "" {
		prompt = " 搜索 / " + m.input + "█"
		if m.mode == ":" {
			prompt = " 命令 : " + m.input + "█"
		}
		tone = yellow
	}
	lines = append(lines, painted(clipped(prompt, leftWidth, false), leftWidth, tone)+" "+muted+credit+reset)
	if m.deleting != nil {
		lines = m.overlay(lines, w, height, "确认永久删除", m.deletionBody(width), 0)
	} else if m.picker != nil {
		lines = m.overlay(lines, w, height, m.picker.title, m.pickerBody(width, height), 0)
	} else if m.configurationEditor != nil {
		lines = m.overlay(lines, w, height, m.configurationEditor.title, m.configurationBody(width, height), 0)
	} else if m.editor != nil {
		lines = m.overlay(lines, w, height, "修改运行点值", m.editorBody(width), 0)
	} else if m.help {
		body := []string{
			"字母快捷键不区分大小写；文本输入保留原样。",
			"退出界面后工程继续运行；显式编辑和删除会保存配置。",
			"1–7 / Tab / Shift+Tab   切换工程、通道、装置、点位、规则、报文、日志。",
			"? / F1      帮助；Esc 逐层返回，Ctrl+C 退出界面。",
			"空格        表单选项列表；↑↓ 选择，Enter 确认，Esc 取消。",
			"n           当前资源页新增；工程页新建独立工程。",
			"e           编辑当前工程、通道、装置、点位或规则。",
			"点位绑定    在 e 编辑中选择同装置遥信/遥测，可一对多。",
			"x / Delete  永久删除；确认窗口默认取消，删除不可恢复。",
			"c           通道页连接或停止；通信参数保存后需手动连接。",
			"r           切换选中规则：停止时启动，运行时停止，暂停时恢复。",
			"仅改变选中规则的运行状态，其他规则和已保存配置保持不变。",
			"保存写入配置文件，自动生成的运行值不会持续回写。",
			"Enter       进入下一级；点位、规则、报文、日志打开详情。",
			"i           工程、通道、装置页查看详情；Esc 返回列表。",
			"↑↓          选择行；Home/End 跳到首行/末行。",
			"/           搜索；Enter 应用；Esc 清除筛选。",
			":           输入工程、通道、装置、点位、规则、报文或日志。",
			"f / l       查看当前通道报文 / 工程日志。",
			"g           报文和日志跟随最新记录。",
			"←/→         切换点位分类；四遥分别显示，点位从小到大。",
			"PgUp/PgDn   点位每类每页 100 条；其他表格翻页。",
			"v / s       修改数据点数值、品质 / 立即上报所选点。",
			"q / Ctrl+C  退出界面。",
		}

		lines = m.overlay(lines, w, height, "快捷键与资源视图", body, m.detailScroll)
	} else if m.detailOpen {
		lines = m.overlay(lines, w, height, m.detailTitle, m.detailLines, m.detailScroll)
	}
	return "\x1b[H" + strings.Join(lines, "\x1b[K\r\n") + "\x1b[0m\x1b[K"
}
func (m *model) overlayTop(height int) int {
	return min(m.tableTop(), max(3, height-9))
}

func (m *model) overlay(screen []string, w, height int, title string, body []string, scroll int) []string {
	mw := min(w-6, 108)
	left := (w - mw) / 2
	contentWidth := mw - 4
	var lines []string
	for _, line := range body {
		lines = append(lines, wrap(line, contentWidth)...)
	}
	top := m.overlayTop(height)
	capacity := height - top - 5
	offset := min(scroll, max(0, len(lines)-capacity))
	if m.editor == nil && (m.detailOpen || m.help) {
		m.detailScroll = offset
	}
	frame := func(s, tone string) string {
		return painted(strings.Repeat(" ", left), left, white) + painted(s, mw, tone) + painted("", w-left-mw, white)
	}
	label := " " + sanitize(title) + " "
	label = clipped(label, mw-4, false)
	screen[top] = frame("╭"+label+strings.Repeat("─", max(0, mw-2-uniseg.StringWidth(label)))+"╮", purple)
	for i := 0; i < capacity; i++ {
		text := ""
		if offset+i < len(lines) {
			text = lines[offset+i]
		}
		screen[top+1+i] = frame("│ "+fit(text, contentWidth)+" │", white)
	}
	screen[top+capacity+1] = frame("╰"+strings.Repeat("─", mw-2)+"╯", purple)
	return screen
}
func (m *model) openDetail() {
	rows := m.rows()
	if len(rows) == 0 {
		return
	}
	id := rows[min(m.cursor, len(rows)-1)].id
	m.detailTarget = detailTarget{view: m.view, id: id, socket: m.socket, channel: m.channel, device: m.device}
	if m.view == 0 {
		m.detailTarget.socket = id
	}
	if m.view == 4 && m.detail.View.Rules != nil {
		for i, r := range m.detail.View.Rules.Config.Rules {
			if fmt.Sprint(i) == id {
				m.detailTarget.ruleKey = ruleIdentity(r)
				m.detailTarget.ruleIndex = i
				m.detailTarget.ruleLayout = fmt.Sprintf("%#v", m.detail.View.Rules.Config.Rules)
				break
			}
		}
		// Determine ambiguity from the configuration, not from changing cycle counters.
		matches := 0
		for _, r := range m.detail.View.Rules.Config.Rules {
			if ruleIdentity(r) == m.detailTarget.ruleKey {
				matches++
			}
		}
		m.detailTarget.ruleDuplicate = matches > 1
	}
	m.detailOpen = true
	m.detailScroll = 0
	m.refreshDetail()
}
func ruleIdentity(r core.AutoDataRule) string {
	return fmt.Sprintf("%s|%s|%d|%d", r.Kind, r.Points, r.Start, r.End)
}
func (m *model) refreshDetail() {
	if !m.detailOpen {
		return
	}
	target := m.detailTarget
	m.detailTitle = viewLabels[target.view] + "详情"
	if target.view == 5 || target.view == 6 {
		m.detailTitle += " #" + target.id
	}
	unavailable := func(reason string) {
		m.detailLines = []string{reason, "请返回列表重新选择；工程仍在运行。"}
	}
	if target.view > 0 {
		if target.socket != m.socket || (target.view >= 2 && target.view <= 4 && target.channel != m.channel) || ((target.view == 3 || target.view == 4) && target.device != m.device) {
			unavailable("所选工程、通道或装置已变化。")
			return
		}
		if m.discovered && !m.connected {
			m.detailLines = []string{"观测连接中断，正在等待重新连接。", "连接恢复后自动更新详情。"}
			return
		}
	}
	var data any
	switch target.view {
	case 0:
		if len(m.instances) == 0 && m.notice != "" {
			m.detailLines = []string{m.notice, "连接恢复后自动更新详情。"}
			return
		}
		for _, item := range m.instances {
			if item.socket == target.id {
				if item.err != nil {
					m.detailLines = []string{connectionMessage(item.err)}
					return
				}
				data = map[string]any{"pid": item.info.PID, "project": item.info.View.Name, "configuration": item.info.View.Path, "started": item.info.Started, "channels": item.info.View.Channels}
				break
			}
		}
	case 1:
		for _, c := range m.detail.View.Channels {
			if c.ID == target.id {
				data = c
				break
			}
		}
	case 2:
		for _, d := range m.detail.View.Devices {
			if d.ID == target.id {
				data = d
				break
			}
		}
	case 3:
		for _, point := range m.detail.View.Points {
			if point.Kind+":"+fmt.Sprint(point.Address) == target.id {
				mappings, bindable := m.detail.View.PointBindings[target.id]
				m.detailLines = pointDetailLines(m.pointProtocol(), point, mappings, m.detail.View.BindingTargetNames, bindable)
				return
			}
		}
	case 4:
		if snapshot := m.detail.View.Rules; snapshot != nil {
			if target.ruleDuplicate && target.ruleLayout != fmt.Sprintf("%#v", snapshot.Config.Rules) {
				unavailable("同范围规则的配置或顺序已变化，请重新选择。")
				return
			}
			matches := []int{}
			for i, r := range snapshot.Config.Rules {
				if ruleIdentity(r) == target.ruleKey {
					matches = append(matches, i)
				}
			}
			index := -1
			if len(matches) == 1 {
				index = matches[0]
			} else if len(matches) > 1 {
				if target.ruleLayout == fmt.Sprintf("%#v", snapshot.Config.Rules) {
					index = target.ruleIndex
				} else {
					unavailable("同范围规则的配置或顺序已变化，请重新选择。")
					return
				}
			}

			if index >= 0 {
				r := snapshot.Config.Rules[index]
				cycles := uint64(0)
				if index < len(snapshot.Cycles) {
					cycles = snapshot.Cycles[index]
				}
				data = map[string]any{"status": ruleStatus(snapshot, index), "cycles": cycles, "lastError": snapshot.LastError, "rule": r}
			}
		}
	case 5:
		for _, msg := range m.messages {
			if fmt.Sprint(msg.Sequence) == target.id {
				data = map[string]any{"sequence": msg.Sequence, "time": msg.Time, "direction": msg.Direction, "channel": msg.ChannelID, "peer": msg.Peer, "summary": msg.Summary}
				m.detailLines = detailJSONLines(data)
				m.detailLines = append(m.detailLines, "", "十六进制报文")
				if b, err := hex.DecodeString(strings.ReplaceAll(msg.Hex, " ", "")); err == nil {
					for offset := 0; offset < len(b); offset += 16 {
						chunk := b[offset:min(offset+16, len(b))]
						m.detailLines = append(m.detailLines, fmt.Sprintf("%04X  % X", offset, chunk))
					}
				} else {
					m.detailLines = append(m.detailLines, msg.Hex)
				}
				m.detailTitle = "报文详情 · " + translated(msg.Direction, directionLabels) + " #" + target.id
				return
			}
		}
	case 6:
		for _, entry := range m.logs {
			if fmt.Sprint(entry.Sequence) == target.id {
				data = map[string]any{"sequence": entry.Sequence, "time": entry.Time, "level": entry.Level, "message": entry.Message, "fields": logFields(entry.Message, entry.Fields), "eventCode": entry.Message, "rawFields": entry.Fields}
				break
			}
		}
	}
	if data == nil {
		switch target.view {
		case 3:
			unavailable("所选点位已不在当前分页或筛选结果中。")
		case 4:
			unavailable("所选规则已移除，或点位范围已变更。")
		case 5, 6:
			unavailable("所选记录已移出历史缓冲区。")
		default:
			unavailable("所选记录已移除。")
		}
		return
	}
	m.detailLines = detailJSONLines(data)
}

// navigation styles renderer-owned segments without counting ANSI codes as cells.
func navigation(labels []string, index, width int, fill string) string {
	var out strings.Builder
	used := 0
	for i, label := range labels {
		text := " " + label + " "
		n := min(width-used, uniseg.StringWidth(text))
		if n <= 0 {
			break
		}
		tone := cyan
		if i == index {
			tone = active
		}
		out.WriteString(painted(text, n, tone))
		used += n
	}
	out.WriteString(painted(strings.Repeat(fill, max(0, width-used)), max(0, width-used), cyan))
	return out.String()
}

func (m *model) rowNumber(index int) int {
	if m.view == 3 {
		return m.pointOffset + index + 1
	}
	return index + 1
}
func (m *model) numberWidth(rows int) int {
	return max(2, len(fmt.Sprint(m.rowNumber(max(0, rows-1))))) + 1
}
func (m *model) tableTop() int {
	return 4
}
func (m *model) accessLabel() string {
	if m.detail.PointOperations {
		return ""
	}
	return "只读"
}
