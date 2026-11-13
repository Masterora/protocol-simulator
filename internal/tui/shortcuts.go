package tui

import (
	"strings"

	"github.com/rivo/uniseg"
)

type shortcutHint struct {
	text     string
	priority int
}

// Fit whole hints, retaining primary actions before secondary navigation.
func shortcutHints(width int, hints ...shortcutHint) string {
	selected := make([]bool, len(hints))
	used := 0
	for priority := 0; priority <= 3; priority++ {
		for i, hint := range hints {
			size := uniseg.StringWidth(hint.text) + 1
			if hint.priority == priority && used+size <= width {
				selected[i], used = true, used+size
			}
		}
	}
	var out strings.Builder
	for i, hint := range hints {
		if selected[i] {
			out.WriteByte(' ')
			out.WriteString(hint.text)
		}
	}
	return out.String()
}

const shortcutDescriptionColor = "\x1b[38;5;158m"

func paintShortcuts(text string, width int) string {
	text = fit(text, width)
	var out strings.Builder
	out.WriteString(shortcutDescriptionColor)
	for {
		start := strings.IndexByte(text, '[')
		if start < 0 {
			break
		}
		end := strings.IndexByte(text[start:], ']')
		if end < 0 {
			break
		}
		end += start + 1
		out.WriteString(text[:start])
		out.WriteString(yellow)
		out.WriteString(text[start:end])
		out.WriteString(shortcutDescriptionColor)
		text = text[end:]
	}
	out.WriteString(text)
	out.WriteString(reset)
	return out.String()
}

func (m *model) configurationActions() (add, edit bool) {
	if m.view < 0 || m.view > 4 {
		return
	}
	rows := m.rows()
	if m.view == 0 {
		add = true
		if len(rows) > 0 {
			id := rows[min(m.cursor, len(rows)-1)].id
			for _, item := range m.instances {
				if item.socket == id {
					edit = item.err == nil && item.info.ConfigurationOperations
				}
			}
		}
		return
	}
	add = m.connected && m.detail.ConfigurationOperations
	if m.view >= 2 {
		add = add && m.channel != ""
	}
	if m.view >= 3 {
		add = add && m.device != ""
	}
	edit = add && len(rows) > 0
	if add && m.view == 2 {
		for _, c := range m.detail.View.Channels {
			if c.ID == m.channel {
				add = c.Protocol != "iec104" || c.Devices == 0
				break
			}
		}
	}
	return add, edit
}

func (m *model) canFollow() bool {
	return m.view >= 5 && !m.follow && len(m.rows()) > 0
}

func (m *model) listCanPage() bool {
	count := len(m.rows())
	if m.view == 5 {
		count = max(count, len(m.frameRows))
	}
	if m.view == 3 {
		count = m.pointRowCount()
	}
	return count > m.tableRows(m.terminalHeight)
}

// Show the shortcuts for the current input focus.
func (m *model) actionHints(width int) string {
	canEdit, canReport := m.pointOperationsAvailable()
	switch {
	case m.operationBusy:
		return " 正在提交… [Ctrl+C]退出界面"
	case m.deleting != nil:
		return shortcutHints(width, shortcutHint{"[Enter]确认", 0}, shortcutHint{"[Esc]取消", 0}, shortcutHint{"[←→]选择", 1})
	case m.picker != nil:
		return shortcutHints(width, shortcutHint{"[Enter]确认", 0}, shortcutHint{"[Esc]取消", 0}, shortcutHint{"[↑↓]选择", 1}, shortcutHint{"[PgUp/PgDn]翻页", 2}, shortcutHint{"[Home/End]首尾", 2})
	case m.configurationEditor != nil:
		label := m.configurationEditor.submitLabel
		if label == "" {
			label = "保存"
		}
		hints := []shortcutHint{{"[Enter]" + label, 0}, {"[Esc]取消", 0}, {"[Tab/Shift+Tab]字段", 1}}
		if m.configurationEditor.initialTable > 0 {
			hints = append(hints, shortcutHint{"[↑↓]选行", 2})
		}
		if len(m.configurationEditor.fieldChoices(m.configurationEditor.focus)) > 0 {
			hints = append(hints, shortcutHint{"[空格]选项", 1})
		}
		if m.configurationEditor.acceptsText(m.configurationEditor.focus) {
			hints = append(hints, shortcutHint{"[←→]光标", 1}, shortcutHint{"[Ctrl+U]清空", 2})
		} else {
			hints = append(hints, shortcutHint{"[←→]切换", 2})
		}
		hints = append(hints, shortcutHint{"[PgUp/PgDn]翻页", 3})
		return shortcutHints(width, hints...)
	case m.pointPreview != nil:
		return shortcutHints(width, shortcutHint{"[Enter]导入", 0}, shortcutHint{"[Esc]取消", 0})
	case m.editor != nil:
		hints := []shortcutHint{{"[Enter]保存", 0}, {"[Esc]取消", 0}, {"[Tab/Shift+Tab]字段", 1}, {"[←→]光标", 1}}
		if len(m.manualChoices(m.editor.focus)) > 0 {
			hints = append(hints, shortcutHint{"[空格]选项", 1})
		}
		hints = append(hints, shortcutHint{"[Ctrl+U]清空", 2})
		return shortcutHints(width, hints...)
	case m.searching:
		return shortcutHints(width, shortcutHint{"[Enter]筛选", 0}, shortcutHint{"[Esc]取消", 0}, shortcutHint{"[←→]光标", 1}, shortcutHint{"[Ctrl+U]清空", 2})
	case m.help:
		return shortcutHints(width, shortcutHint{"[Esc]返回", 0}, shortcutHint{"[↑↓]滚动", 1}, shortcutHint{"[PgUp/PgDn]翻页", 2}, shortcutHint{"[Home/End]首尾", 2})
	}
	if m.detailOpen {
		hints := []shortcutHint{{"[Enter/Esc]返回", 0}}
		if canEdit {
			hints = append(hints, shortcutHint{"[v]改值", 0})
		}
		if canReport {
			hints = append(hints, shortcutHint{m.reportShortcut(), 0})
		}
		hints = append(hints, shortcutHint{"[↑↓]滚动", 1}, shortcutHint{"[PgUp/PgDn]翻页", 3}, shortcutHint{"[Home/End]首尾", 2})
		return shortcutHints(width, hints...)
	}
	hasRows := len(m.rows()) > 0
	hints := []shortcutHint{}
	if hasRows {
		enter := "详情"
		if m.view < 3 {
			enter = []string{"通道", "装置", "点位"}[m.view]
		}
		hints = append(hints, shortcutHint{"[Enter]" + enter, 0})
		if m.view < 3 {
			hints = append(hints, shortcutHint{"[i]详情", 2})
		}
	}
	if m.view == 5 && hasRows {
		hints = append(hints, shortcutHint{"[c]复制报文", 0})
	}
	add, edit := m.configurationActions()
	if add {
		label := "新增"
		if m.view == 0 {
			label = "新建"
		}
		hints = append(hints, shortcutHint{"[n]" + label, 1})
	}
	if edit {
		hints = append(hints, shortcutHint{"[e]编辑", 0})
	}
	if canEdit {
		hints = append(hints, shortcutHint{"[v]改值", 1})
	}
	if canReport {
		hints = append(hints, shortcutHint{m.reportShortcut(), 1})
	}
	if m.view == 1 && edit {
		label := "连接"
		id := m.rows()[min(m.cursor, len(m.rows())-1)].id
		for _, c := range m.detail.View.Channels {
			if c.ID == id && c.State.Status != "stopped" && c.State.Status != "error" {
				label = "停止"
			}
		}
		hints = append(hints, shortcutHint{"[c]" + label, 0})
	}
	if m.canControlRules() {
		index, _ := m.selectedRule()
		_, label := ruleControlAction(ruleStatus(m.detail.View.Rules, index))
		hints = append(hints, shortcutHint{"[r]" + label, 0})
	}
	if m.canFollow() {
		hints = append(hints, shortcutHint{"[g]跟随", 1})
	}
	if m.pointTransferAvailable() {
		if m.detail.ToolOperations {
			hints = append(hints, shortcutHint{"[m]导入", 0})
		}
		hints = append(hints, shortcutHint{"[o]导出", 0})
	}
	if m.canDelete() {
		hints = append(hints, shortcutHint{"[x]删除", 0})
	}
	if m.view == 3 && len(m.pointKinds()) > 1 {
		hints = append(hints, shortcutHint{"[←→]分类", 1})
	}
	if hasRows {
		hints = append(hints, shortcutHint{"[↑↓]选择", 2})
		if m.listCanPage() {
			hints = append(hints, shortcutHint{"[PgUp/PgDn]翻页", 0}, shortcutHint{"[Home/End]首尾", 3})
		}
	}
	hints = append(hints, shortcutHint{"[/]筛选", 2}, shortcutHint{"[Tab]切页", 3})
	if m.filter != "" {
		hints = append(hints, shortcutHint{"[Esc]清筛选", 2})
	} else if m.view != 0 {
		hints = append(hints, shortcutHint{"[Esc]返回", 2})
	}
	utilities := " [?]帮助 [q]退出"
	actionWidth := max(0, width-uniseg.StringWidth(utilities))
	return fit(shortcutHints(actionWidth, hints...), actionWidth) + utilities
}
