package tui

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/rivo/uniseg"
	"gytest-go/internal/core"
)

const (
	reset    = "\x1b[0m"
	cyan     = "\x1b[38;5;81m"
	purple   = "\x1b[38;5;141m"
	muted    = "\x1b[38;5;246m"
	white    = "\x1b[38;5;252m"
	green    = "\x1b[38;5;114m"
	yellow   = "\x1b[38;5;221m"
	red      = "\x1b[38;5;203m"
	selected = "\x1b[1;38;5;231;48;5;24m"
	active   = "\x1b[1;38;5;231;48;5;60m"
)

var viewLabels = []string{"工程", "通道", "装置", "点位", "规则", "报文", "日志"}

type row struct {
	id, text string
	cells    []string
	tone     string
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
			rows = append(rows, resourceRow(c.ID, tone, c.Name, strings.ToUpper(c.Protocol), translated(c.State.Status, stateLabels), fmt.Sprint(c.State.Peers), byteCount(c.State.RX), byteCount(c.State.TX), c.Address))
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
			rows = append(rows, resourceRow(p.Kind+":"+fmt.Sprint(p.Address), tone, fmt.Sprint(p.Address), fmt.Sprintf("0x%X", p.Address), translated(p.Kind, kindLabels), fmt.Sprintf("%.8g", p.Value), fmt.Sprint(p.Quality), p.Name))
		}
	case 4:
		if s := m.detail.View.Rules; s != nil {
			for i, r := range s.Config.Rules {
				n := uint64(0)
				if i < len(s.Cycles) {
					n = s.Cycles[i]
				}
				enabled, tone := "启用", green
				if !r.Enabled {
					enabled, tone = "停用", muted
				}
				rows = append(rows, resourceRow(fmt.Sprint(i), tone, fmt.Sprint(i+1), enabled, translated(r.Kind, kindLabels), translated(r.Mode, modeLabels), fmt.Sprintf("%g 秒", float64(r.IntervalMS)/1000), fmt.Sprint(n), r.Points))
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
func (m *model) columns(width int) ([]column, []int) {
	var cols []column
	switch m.view {
	case 0:
		cols = []column{{"进程", 8}, {"工程", 24}, {"状态", 12}, {"通道数", 8}, {"配置文件", 0}}
	case 1:
		cols = []column{{"名称", 24}, {"协议", 12}, {"状态", 12}, {"连接数", 8}, {"接收量", 10}, {"发送量", 10}, {"通信地址", 0}}
	case 2:
		cols = []column{{"装置", 0}, {"站号", 6}, {"公共地址", 10}, {"点位数", 8}, {"标识", 18}}
	case 3:
		cols = []column{{"地址", 9}, {"十六进制", 11}, {"类型", 12}, {"数值", 14}, {"品质", 6}, {"描述", 0}}
	case 4:
		cols = []column{{"序号", 6}, {"启用", 6}, {"类型", 12}, {"模式", 8}, {"周期", 10}, {"执行次数", 10}, {"点位范围", 0}}
	case 5:
		cols = []column{{"时间", 13}, {"方向", 6}, {"对端", 23}, {"摘要", 0}, {"原始报文", 36}}
	case 6:
		cols = []column{{"时间", 13}, {"级别", 7}, {"事件", 24}, {"字段", 0}}
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
	if width < 105 {
		switch m.view {
		case 0:
			remove(3)
		case 1:
			remove(4)
			remove(5)
		case 2:
			remove(4)
		case 3:
			remove(1)
		case 4:
			remove(1)
		case 5:
			remove(4)
		}
	}
	if width < 78 {
		switch m.view {
		case 0:
			remove(2)
		case 1:
			remove(3)
		case 4:
			remove(2)
		case 5:
			remove(2)
		}
	}
	// Allocate column widths using terminal cells, including CJK descriptions.
	fixed, flex := 0, -1
	for _, i := range indices {
		if cols[i].width == 0 {
			flex = i
		} else {
			fixed += cols[i].width
		}
	}
	if flex >= 0 {
		cols[flex].width = max(10, width-fixed)
	}
	total := 0
	for _, i := range indices {
		total += cols[i].width
	}
	for total > width {
		changed := false
		for _, i := range indices {
			if total <= width {
				break
			}
			if cols[i].width > 7 {
				cols[i].width--
				total--
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return cols, indices
}
func cellsLine(cells []string, cols []column, indices []int) string {
	var s strings.Builder
	for _, i := range indices {
		v := ""
		if i < len(cells) {
			v = cells[i]
		}
		s.WriteString(fit(v, max(0, cols[i].width-1)))
		s.WriteByte(' ')
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
			channel = c.Name
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
func splitLine(left, right string, width int, tone string) string {
	right = sanitize(right)
	rw := uniseg.StringWidth(right)
	if width-rw < 12 {
		return painted(left, width, tone)
	}
	return tone + fit(left, width-rw) + right + reset
}
func ruleLineTitle(m *model) string {
	if m.view == 4 && m.detail.View.Rules != nil {
		return " · " + translated(m.detail.View.Rules.Status, stateLabels)
	}
	return ""
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
		return "正在连接所选工程 · 连接失败时查看底部错误信息"
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
		return "此分类暂无点位 · ←/→ 切换分类"
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
	status, tone := "实时刷新", green
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
	if m.paused {
		status, tone = "显示已暂停", yellow
	}
	lines := []string{
		splitLine(" 工程  "+project, m.accessLabel(), w, purple),
		splitLine(" 通道  "+channel, "[?] 帮助   / 筛选   : 资源命令", w, white),
		splitLine(" 装置  "+device, fmt.Sprintf("进程 %d", m.detail.PID), w, tone),
	}
	labels := make([]string, len(viewLabels))
	for i, n := range viewLabels {
		labels[i] = fmt.Sprintf("%d:%s", i+1, n)
	}
	lines = append(lines, navigation(labels, m.view, w))
	if m.view == 3 {
		lines = append(lines, m.pointNavigation(w))
	}

	if m.view == 3 {
		if m.filter != "" {
			lines = append(lines, painted(fmt.Sprintf(" 匹配 %d 条", m.detail.View.PointMatches), w, muted))
		}
	} else {
		summary := fmt.Sprintf(" 工程 %d 个 · 通道 %d 个 · 装置 %d 个 · 点位 %d 个", len(m.instances), len(m.detail.View.Channels), len(m.detail.View.Devices), m.detail.View.PointTotal)
		lines = append(lines, painted(summary, w, muted))
	}

	rows := m.rows()
	if len(rows) > 0 {
		m.cursor = min(m.cursor, len(rows)-1)
	} else {
		m.cursor = 0
	}
	available := height - len(lines) - 5
	title := fmt.Sprintf(" %s [%d]%s ", viewLabels[m.view], len(rows), ruleLineTitle(m))
	if m.view == 3 {
		title = fmt.Sprintf(" 点位 [%d] · %s ", len(rows), translated(m.pointKind, kindLabels))
	}
	top := "┌" + title + strings.Repeat("─", max(0, w-2-uniseg.StringWidth(title))) + "┐"
	lines = append(lines, painted(top, w, cyan))
	cols, indices := m.columns(w - 4)
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.title
	}
	lines = append(lines, cyan+"│"+yellow+fit("  "+cellsLine(headers, cols, indices), w-2)+cyan+"│"+reset)
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
			text = prefix + cellsLine(r.cells, cols, indices)
		} else if len(rows) == 0 && n == 0 {
			text = "  " + m.emptyMessage()
			tone = muted
		}
		lines = append(lines, cyan+"│"+painted(text, w-2, tone)+cyan+"│"+reset)
	}
	position := fmt.Sprintf(" %d/%d ", min(m.cursor+1, len(rows)), len(rows))
	if m.view == 3 {
		position = fmt.Sprintf(" %d-%d / %d ", min(m.pointOffset+1, m.detail.View.PointMatches), m.pointOffset+len(m.detail.View.Points), m.detail.View.PointMatches)
	}
	bottom := "└" + strings.Repeat("─", max(0, w-2-uniseg.StringWidth(position))) + position + "┘"
	lines = append(lines, painted(bottom, w, cyan))

	action := " [Enter] 进入  [v] 详情  [f] 报文  [l] 日志  [Esc] 返回  [p] 暂停  [q] 退出"
	if m.view >= 3 {
		action = " [Enter] 详情  [↑↓] 选择  [PgUp/PgDn] 翻页  [p] 暂停  [q] 退出"
	}
	if m.view == 3 && !m.detailOpen && !m.help {
		action = " [←/→] 分类  " + strings.TrimSpace(action)
	}

	if m.detailOpen {
		action = " [Esc/Enter] 返回  [↑↓] 滚动  [PgUp/PgDn] 翻页  [p] 暂停  [q] 退出"
	}
	if m.help {
		action = " [Esc/?] 返回  [↑↓] 滚动  [PgUp/PgDn] 翻页  [q] 退出"
	}
	if m.mode != "" {
		action = " [Enter] 确认  [Esc] 取消  [Ctrl+U] 清空  [Ctrl+C] 退出"
	}
	if w < 105 {
		action = " [Enter] 进入 [v] 详情 [p] 暂停 [q] 退出 [?] 帮助"
		if m.view == 3 {
			action = " [←→]分类 [Enter]详情 [PgUp/Dn]翻页 [p]暂停 [q]退出"
		}
		if m.detailOpen {
			action = " [Esc] 返回 [↑↓] 滚动 [p] 暂停 [q] 退出"
		}
		if m.help {
			action = " [Esc/?] 返回 [↑↓] 滚动 [q] 退出"
		}
		if m.mode != "" {
			action = " [Enter] 确认 [Esc] 取消 [Ctrl+U] 清空 [Ctrl+C] 退出"
		}
	}
	canEdit, canReport := m.pointOperationsAvailable()
	if canEdit && !m.help && m.mode == "" {
		if w >= 105 {
			action = strings.Replace(action, "[p] 暂停", "[e] 修改  [s] 上报  [p] 暂停", 1)
		} else {
			action = " [←→]分类 [Enter]详情 [e]改值 [s]上报 [p]暂停 [q]退出"
			if m.detailOpen {
				action = " [Esc]返回 [↑↓]滚动 [e]改值 [s]上报 [p]暂停 [q]退出"
			}
		}
	}
	if !canReport {
		action = strings.ReplaceAll(strings.ReplaceAll(action, "[s] 上报  ", ""), "[s]上报 ", "")
	}
	if m.editor != nil {
		action = " [Tab/↑↓] 字段  [Enter] 应用  [Esc] 取消  [Ctrl+U] 清空"
		if w < 105 {
			action = " [Tab]字段 [Enter]提交 [Esc]取消 [Ctrl+U]清空"
		}
	}
	if m.operationBusy {
		action = " 正在提交，请等待实际结果…"
	}
	if m.paused {
		action = strings.ReplaceAll(strings.ReplaceAll(action, "[p] 暂停", "[p] 恢复"), "[p]暂停", "[p]恢复")
	}
	lines = append(lines, painted(action, w, cyan))
	prompt := fmt.Sprintf(" %s · 报文缺口 %d · 日志缺口 %d", status, m.missed, m.logMissed)
	if !m.updatedAt.IsZero() {
		prompt += " · 界面更新 " + m.updatedAt.Local().Format("15:04:05")
	}
	if m.filter != "" {
		prompt += "  / " + m.filter
	}
	if m.operationNotice != "" && time.Since(m.operationNoticeAt) < 8*time.Second {
		prompt += " · " + m.operationNotice
	}
	if m.notice != "" {
		prompt = " " + status + " · " + m.notice
		tone = red
	}
	if m.paused {
		prompt += "  ·  仅暂停显示，工程持续运行"
	}
	if m.mode != "" {
		prompt = " 搜索 / " + m.input + "█"
		if m.mode == ":" {
			prompt = " 命令 : " + m.input + "█"
		}
		tone = yellow
	}
	lines = append(lines, painted(prompt, w, tone))
	if m.editor != nil {
		lines = m.overlay(lines, w, height, "修改运行点值", m.editorBody(), 0)
	} else if m.help {
		body := []string{
			"1–7 / Tab   切换工程、通道、装置、点位、规则、报文、日志。",
			"Enter       进入下一级；点位、规则、报文、日志打开详情。",
			"v           查看当前行实时详情；Esc 返回列表。",
			"↑↓          选择行；g/G 跳到首行/末行。",
			"/           搜索；Enter 应用；Esc 清除筛选。",
			":           输入工程、通道、装置、点位、规则、报文或日志。",
			"f / l       查看当前通道报文 / 工程日志。",
			"G           报文和日志跟随最新记录。",
			"←/→         切换点位分类；四遥分别显示，地址从小到大。",
			"PgUp/PgDn   点位每类每页 100 条；其他表格翻页。",
			"e / s       修改数据点数值、品质 / 立即上报所选点。",
			"p / 空格    暂停或恢复显示，工程继续运行。",
			"q / Ctrl+C  退出界面，工程继续运行。",
			"", "界面每 250 毫秒读取最新状态；自动或手动变化均实时显示。",
			"详情持续刷新；p 暂停或恢复显示，历史缺口见状态栏。",
		}

		lines = m.overlay(lines, w, height, "快捷键与资源视图", body, m.detailScroll)
	} else if m.detailOpen {
		lines = m.overlay(lines, w, height, m.detailTitle+" · "+status, m.detailLines, m.detailScroll)
	}
	return "\x1b[H" + strings.Join(lines, "\x1b[K\r\n") + "\x1b[0m\x1b[K"
}
func (m *model) overlay(screen []string, w, height int, title string, body []string, scroll int) []string {
	mw := min(w-6, 108)
	left := (w - mw) / 2
	contentWidth := mw - 4
	var lines []string
	for _, line := range body {
		lines = append(lines, wrap(line, contentWidth)...)
	}
	top := m.tableTop()
	capacity := height - top - 5
	offset := min(scroll, max(0, len(lines)-capacity))
	if m.editor == nil && (m.detailOpen || m.help) {
		m.detailScroll = offset
	}
	frame := func(s, tone string) string {
		return painted(strings.Repeat(" ", left), left, white) + painted(s, mw, tone) + painted("", w-left-mw, white)
	}
	label := " " + sanitize(title) + " "
	label = fit(label, min(uniseg.StringWidth(label), mw-4))
	screen[top] = frame("╭"+label+strings.Repeat("─", max(0, mw-2-uniseg.StringWidth(label)))+"╮", purple)
	for i := 0; i < capacity; i++ {
		text := ""
		if offset+i < len(lines) {
			text = lines[offset+i]
		}
		screen[top+1+i] = frame("│ "+fit(text, contentWidth)+" │", white)
	}
	foot := fmt.Sprintf(" %d/%d ", min(offset+capacity, len(lines)), len(lines))
	screen[top+capacity+1] = frame("╰"+strings.Repeat("─", max(0, mw-2-uniseg.StringWidth(foot)))+foot+"╯", purple)
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
				data = point
				break
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
				data = map[string]any{"status": snapshot.Status, "cycles": cycles, "lastError": snapshot.LastError, "rule": r}
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
func navigation(labels []string, index, width int) string {
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
	out.WriteString(painted("", max(0, width-used), cyan))
	return out.String()
}

func (m *model) tableTop() int {
	if m.view == 3 {
		if m.filter != "" {
			return 6
		}
		return 5
	}
	return 5
}
func (m *model) accessLabel() string {
	if m.detail.PointOperations {
		return "PRS"
	}
	return "PRS / 只读"
}
