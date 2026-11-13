package tui

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
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
			status := translated(c.State.Status, stateLabels)
			if c.State.LastError != "" {
				tone = red
			} else if c.State.Status != "connected" {
				tone = yellow
			}
			if c.Protocol == "iec104" && c.State.Peers > 0 && c.State.ReadyPeers == 0 {
				status, tone = "待激活", yellow
			}
			rows = append(rows, resourceRow(c.ID, tone, c.Name, strings.ToUpper(c.Protocol), translated(c.Role, roleLabels), translated(c.Transport, transportLabels), status, fmt.Sprint(c.State.Peers), byteCount(c.State.RX), byteCount(c.State.TX), m.channelAddress(c)))
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
func (m *model) columns(width int, rows []row) ([]column, []int) {
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
		case 1:
			remove(3)
		case 3:
			remove(1)
			if m.pointKind != "" {
				remove(2)
			}
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
	for _, r := range rows {
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
func cellsLine(cells []string, cols []column, indices []int, gap int) string {
	var s strings.Builder
	for _, i := range indices {
		v := ""
		if i < len(cells) {
			v = cells[i]
		}
		width := max(0, cols[i].width-gap)
		s.WriteString(fit(clipped(v, width, cols[i].title == "配置文件"), width))
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
	if m.view == 3 && m.connected && m.pointLoading {
		return "正在读取点位…"
	}
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
		if m.channel == "" {
			return "请先选择通道"
		}
		return "此通道没有装置"
	case 3:
		if m.device == "" {
			return "请先选择装置"
		}
		return "此分类暂无点位"
	case 4:
		if m.device == "" {
			return "请先选择装置"
		}
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
		return "\x1b[H\x1b[2J终端太小，请调整至至少 60 列、12 行。Ctrl+C 退出界面\r\n"
	}
	m.terminalHeight = height
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
	brand := applicationName
	if access := m.accessLabel(); access != "" {
		brand += " · " + access
	}
	lines := []string{
		splitLine(" 工程  "+project, brand, w, purple),
		painted(clipped(" 通道  "+channel, w, false), w, white),
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
	credit := attribution.Label()
	brandWidth := max(uniseg.StringWidth(applicationName), uniseg.StringWidth(credit))
	leftWidth := w - brandWidth - 1
	shortcutLine := len(lines)
	lines = append(lines, "")
	available := m.tableRows(height)
	title := " " + viewLabels[m.view]
	if m.view >= 5 {
		if m.follow {
			title += " · 跟随"
		} else {
			title += " · 浏览"
		}
	}
	title += " "
	top := "┌" + title + strings.Repeat("─", max(0, w-2-uniseg.StringWidth(title))) + "┐"
	if m.view == 3 {
		prefix := "┌ 点位 ·"
		top = cyan + prefix + reset + m.pointNavigation(w-uniseg.StringWidth(prefix)-1) + cyan + "┐" + reset
		lines = append(lines, top)
	} else {
		lines = append(lines, painted(top, w, cyan))
	}
	numberWidth := m.numberWidth(len(rows))
	columnWidth := w - 4 - numberWidth
	cols, indices := m.columns(columnWidth, rows)
	gap := tableGap(columnWidth)
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.title
	}
	lines = append(lines, cyan+"│"+yellow+fit("  "+strings.Repeat(" ", numberWidth)+cellsLine(headers, cols, indices, gap), w-2)+cyan+"│"+reset)
	if m.view == 5 {
		m.frameRows = m.frameRows[:0]
	}
	if m.view == 5 && len(rows) > 0 {
		lines = append(lines, m.renderFrameRows(rows, cols, indices, gap, numberWidth, available, w-2)...)
	} else {
		m.listStart = visibleStart(m.listStart, m.cursor, available, len(rows))
		for n := 0; n < available; n++ {
			i := m.listStart + n
			text, tone := "", white
			if i < len(rows) {
				r := rows[i]
				prefix := "  "
				tone = r.tone
				if i == m.cursor {
					prefix = "› "
					tone = selected
				}
				text = prefix + fit(fmt.Sprint(m.rowNumber(i)), numberWidth) + cellsLine(r.cells, cols, indices, gap)
			} else if len(rows) == 0 && n == 0 {
				text = "  " + m.emptyMessage()
				tone = muted
			}
			lines = append(lines, cyan+"│"+painted(text, w-2, tone)+cyan+"│"+reset)
		}
	}
	lines[shortcutLine] = paintShortcuts(m.actionHints(w), w)
	bottom := "└" + strings.Repeat("─", w-2) + "┘"
	lines = append(lines, painted(bottom, w, cyan))

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
	if m.searching {
		prompt = " 筛选 / " + m.searchCaret.render(m.input, leftWidth-8)
		tone = yellow
	}
	lines = append(lines, painted(clipped(prompt, leftWidth, false), leftWidth, tone)+" "+muted+credit+reset)
	if m.deleting != nil {
		lines = m.overlay(lines, w, height, "确认永久删除", m.deletionBody(width), 0)
	} else if m.picker != nil {
		lines = m.overlay(lines, w, height, m.picker.title, m.pickerBody(width, height), 0)
	} else if m.configurationEditor != nil {
		lines = m.overlay(lines, w, height, m.configurationEditor.title, m.configurationBody(width, height), 0, initialPointHeading())
	} else if m.pointPreview != nil {
		lines = m.overlay(lines, w, height, "确认导入点表", m.pointPreview.body(min(w-6, 108)-4), 0)
	} else if m.editor != nil {
		lines = m.overlay(lines, w, height, "修改运行点值", m.editorBody(width), 0)
	} else if m.help {
		body, headings := helpBody(min(w-6, 108) - 4)
		lines = m.overlay(lines, w, height, "帮助", body, m.detailScroll, headings...)
	} else if m.detailOpen {
		lines = m.overlay(lines, w, height, m.detailTitle, m.detailLines, m.detailScroll)
	}
	return "\x1b[H" + strings.Join(lines, "\x1b[K\r\n") + "\x1b[0m\x1b[K"
}

const frameHexColumn = 4

func (m *model) renderFrameRows(rows []row, cols []column, indices []int, gap, numberWidth, available, width int) []string {
	var contents []string
	first := 0
	for i, r := range rows {
		if i == m.cursor {
			first = len(contents)
		}
		prefix := "  "
		if i == m.cursor {
			prefix = "› "
		}
		indent := strings.Repeat(" ", 2+numberWidth)
		cells := slices.Clone(r.cells)
		raw := sanitize(cells[frameHexColumn])
		var chunks []string
		if slices.Contains(indices, frameHexColumn) {
			for _, col := range indices {
				if col == frameHexColumn {
					break
				}
				indent += strings.Repeat(" ", cols[col].width)
			}
			chunks = wrap(raw, (cols[frameHexColumn].width-gap)/2*2)
			cells[frameHexColumn] = chunks[0]
			chunks = chunks[1:]
		} else if raw != "" {
			chunks = wrap(raw, (width-len(indent))/2*2)
		}
		contents = append(contents, prefix+fit(fmt.Sprint(m.rowNumber(i)), numberWidth)+cellsLine(cells, cols, indices, gap))
		m.frameRows = append(m.frameRows, i)
		for _, chunk := range chunks {
			contents = append(contents, indent+chunk)
			m.frameRows = append(m.frameRows, i)
		}
	}
	if m.follow {
		m.frameLine = len(contents) - 1
	} else if m.frameLine >= len(contents) || m.frameRows[m.frameLine] != m.cursor {
		m.frameLine = first
	}
	m.frameTop = visibleStart(m.frameTop, m.frameLine, available, len(contents))
	lines := make([]string, 0, available)
	for i := m.frameTop; i < m.frameTop+available; i++ {
		text, tone := "", white
		if i < len(contents) {
			text = contents[i]
			row := m.frameRows[i]
			tone = rows[row].tone
			if row == m.cursor {
				tone = selected
			}
		}
		lines = append(lines, cyan+"│"+painted(text, width, tone)+cyan+"│"+reset)
	}
	return lines
}

func helpBody(width int) (body, headings []string) {
	groups := []struct {
		title string
		rows  [][2]string
	}{
		{"配置速查", [][2]string{
			{"工程", "详情查看路径和 PID。在线用 --pid，离线用 --file。"},
			{"通道", "TCP Server 填监听 IP:端口。"},
			{"", "TCP Client 填目标 IP:端口。"},
			{"四遥", "yx/yc/yk/yt。单点遥信 wireType=1，值 0/1。"},
			{"绑定", "编辑遥控或遥调，按类型和点位号指定同装置目标点。"},
			{"上报", "周期用毫秒。连续点用 1-3，不连续用 1,3。"},
			{"", "启用规则，服务加 --start-data-rules。"},
			{"校验", "prs validate FILE --json"},
			{"生效", "备份原文件，临时文件通过校验后替换。默认热加载。"},
		}},
		{"浏览快捷键", [][2]string{
			{"1–7 / Tab", "切换视图，Shift+Tab 反向。"},
			{"Enter / i", "进入下一级或打开详情 / 查看资源详情。"},
			{"↑↓ / ←→", "选择行 / 切换点位分类。"},
			{"PgUp/PgDn", "翻页，Home/End 跳到首尾。"},
			{"/ / Esc", "筛选 / 清除筛选、返回上层或关闭弹窗。"},
			{"g", "在报文和日志页恢复跟随。"},
			{"q / Ctrl+C", "退出界面，工程继续运行。"},
		}},
		{"配置快捷键", [][2]string{
			{"n / e / x", "新增 / 编辑 / 删除配置。删除确认默认取消。"},
			{"c", "连接或停止通道。通信参数变更后需手动连接。"},
			{"r", "切换选中规则的运行状态。"},
			{"v / s", "修改子站数据点的值和品质 / 立即上报 IEC104 数据点。"},
			{"m / o", "点位页导入 / 导出点表。"},
			{"Tab / ↑↓", "选择字段，Shift+Tab 反向，PgUp/PgDn 翻页。"},
			{"←→ / 空格", "移动光标或切换可选值 / 打开选项列表。"},
			{"Ctrl+U", "清空文本字段或筛选内容。"},
			{"Enter / Esc", "确认或保存 / 取消。"},
		}},
	}
	const labelWidth = 13
	for _, group := range groups {
		if len(body) > 0 {
			body = append(body, "")
		}
		headings = append(headings, group.title)
		body = append(body, group.title)
		for _, row := range group.rows {
			label := row[0]
			for _, text := range wrap(row[1], width-labelWidth) {
				body = append(body, fit(label, labelWidth)+text)
				label = ""
			}
		}
	}
	return body, headings
}

func (m *model) overlayTop(_ int) int {
	return m.tableTop()
}

func (m *model) tableRows(height int) int {
	return max(1, height-m.tableTop()-4)
}

func (m *model) overlayRows(height int) int {
	return max(1, height-m.overlayTop(height)-3)
}

func visibleStart(start, cursor, capacity, length int) int {
	return min(max(start, cursor-capacity+1), cursor, max(0, length-capacity))
}

func (m *model) overlay(screen []string, w, height int, title string, body []string, scroll int, headings ...string) []string {
	mw := min(w-6, 108)
	left := (w - mw) / 2
	contentWidth := mw - 4
	var lines []string
	for _, line := range body {
		lines = append(lines, wrap(line, contentWidth)...)
	}
	top := m.overlayTop(height)
	capacity := m.overlayRows(height)
	if m.deleting != nil {
		capacity = min(capacity, len(lines))
	} else if editor := m.configurationEditor; editor != nil && editor.initialTable > 0 && title == editor.title {
		capacity = min(capacity, max(1, len(lines)))
	}
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
		text, tone := "", white
		if offset+i < len(lines) {
			text = lines[offset+i]
		}
		if m.deleting != nil {
			padding := strings.Repeat(" ", max(0, (contentWidth-uniseg.StringWidth(text))/2))
			if offset+i == len(lines)-1 {
				cancel, confirm, _ := strings.Cut(text, "    ")
				cancelTone, confirmTone := selected, red
				if m.deleting.confirm {
					cancelTone, confirmTone = white, "\x1b[1;38;5;231;48;5;124m"
				}
				outer := painted("", left, white)
				inside := painted(cancel, uniseg.StringWidth(cancel), cancelTone) + "    " + painted(confirm, uniseg.StringWidth(confirm), confirmTone)
				tail := max(0, contentWidth-len(padding)-uniseg.StringWidth(text))
				screen[top+1+i] = outer + purple + "│ " + padding + inside + strings.Repeat(" ", tail) + purple + " │" + reset + painted("", w-left-mw, white)
				continue
			}
			text, tone = padding+text, muted
		} else if slices.Contains(headings, text) {
			tone = cyan
		} else if m.deleting == nil && strings.HasPrefix(text, "› ") {
			tone = selected
		} else if (m.configurationEditor != nil || m.editor != nil) && strings.HasPrefix(text, "错误 ") {
			tone = red
		}
		screen[top+1+i] = frame("│ "+fit(text, contentWidth)+" │", tone)
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
				m.detailLines = []string{"十六进制报文"}
				if b, err := hex.DecodeString(strings.ReplaceAll(msg.Hex, " ", "")); err == nil {
					// Keep complete byte groups on one line at the minimum terminal width.
					const bytesPerLine = 14
					for offset := 0; offset < len(b); offset += bytesPerLine {
						chunk := b[offset:min(offset+bytesPerLine, len(b))]
						m.detailLines = append(m.detailLines, fmt.Sprintf("%04X  % X", offset, chunk))
					}
				} else {
					m.detailLines = append(m.detailLines, msg.Hex)
				}
				m.detailLines = append(m.detailLines, "")
				m.detailLines = append(m.detailLines, formatDetailLines(data)...)
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
	m.detailLines = formatDetailLines(data)
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
	return 5
}
func (m *model) accessLabel() string {
	if m.detail.PointOperations {
		return ""
	}
	return "只读"
}
