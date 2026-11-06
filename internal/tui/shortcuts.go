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

func paintShortcuts(text string, width int) string {
	text = fit(text, width)
	var out strings.Builder
	out.WriteString(white)
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
		out.WriteString(white)
		text = text[end:]
	}
	out.WriteString(text)
	out.WriteString(reset)
	return out.String()
}

func (m *model) globalHints(width int) string {
	if m.operationBusy || m.deleting != nil || m.picker != nil || m.configurationEditor != nil || m.editor != nil || m.mode != "" {
		return " [Ctrl+C]退出界面"
	}
	if m.help {
		return " [?]返回 [q]退出"
	}
	hints := []shortcutHint{}
	if !m.detailOpen {
		if width >= 100 {
			hints = append(hints, shortcutHint{"[Tab]切页", 1})
		}
		hints = append(hints, shortcutHint{"[/]筛选", 0})
	}
	if !m.detailOpen {
		hints = append(hints, shortcutHint{"[?]帮助", 0})
	}
	hints = append(hints, shortcutHint{"[q]退出", 0})
	return shortcutHints(width-18, hints...)
}

func (m *model) shortcutHeader(left string, width int) string {
	hints := m.globalHints(width)
	leftWidth := width - uniseg.StringWidth(hints)
	if index := strings.LastIndex(left, " · "); index >= 0 {
		suffix := left[index:]
		left = clipped(left[:index], leftWidth-uniseg.StringWidth(suffix), false) + suffix
	} else {
		left = clipped(left, leftWidth, false)
	}
	return painted(left, leftWidth, white) + paintShortcuts(hints, uniseg.StringWidth(hints))
}

// Configuration hints reflect the selected process's capabilities, including
// legacy observers and an empty or disconnected resource view.
func (m *model) configurationHints() (add, edit bool) {
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
	return add, add && len(rows) > 0
}

// The footer belongs to the focused view or dialog. Global actions stay above.
func (m *model) actionHints(width int) string {
	canEdit, canReport := m.pointOperationsAvailable()
	switch {
	case m.operationBusy:
		return " 正在提交…"
	case m.deleting != nil:
		choice := "取消"
		if m.deleting.confirm {
			choice = "永久删除"
		}
		return shortcutHints(width, shortcutHint{"[" + choice + "]", 0}, shortcutHint{"[←→/Tab]切换", 1}, shortcutHint{"[Enter]确认", 0}, shortcutHint{"[Esc]取消", 0})
	case m.picker != nil:
		return shortcutHints(width, shortcutHint{"[↑↓]选择", 1}, shortcutHint{"[PgUp/PgDn]翻页", 2}, shortcutHint{"[Enter]确认", 0}, shortcutHint{"[Esc]取消", 0})
	case m.configurationEditor != nil:
		hints := []shortcutHint{{"[Tab/↑↓]字段", 1}}
		if len(m.configurationEditor.fieldChoices(m.configurationEditor.focus)) > 0 {
			hints = append(hints, shortcutHint{"[←→]切换", 2}, shortcutHint{"[空格]选项", 1})
		}
		hints = append(hints, shortcutHint{"[Enter]保存", 0}, shortcutHint{"[Esc]取消", 0})
		return shortcutHints(width, hints...)
	case m.editor != nil:
		return shortcutHints(width, shortcutHint{"[Tab/↑↓]字段", 1}, shortcutHint{"[←→]切换", 2}, shortcutHint{"[空格]选项", 1}, shortcutHint{"[Enter]保存", 0}, shortcutHint{"[Esc]取消", 0})
	case m.mode != "":
		return shortcutHints(width, shortcutHint{"[Enter]确认", 0}, shortcutHint{"[Esc]取消", 0}, shortcutHint{"[Ctrl+U]清空", 1})
	case m.help:
		return shortcutHints(width, shortcutHint{"[↑↓]滚动", 1}, shortcutHint{"[PgUp/PgDn]翻页", 2}, shortcutHint{"[Home/End]首尾", 2}, shortcutHint{"[Esc]返回", 0})
	}
	if m.detailOpen {
		hints := []shortcutHint{{"[Enter/Esc]返回", 0}}
		if canEdit {
			hints = append(hints, shortcutHint{"[v]改值", 0})
		}
		if canReport {
			hints = append(hints, shortcutHint{"[s]上报", 0})
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
	if m.canControlRules() {
		index, _ := m.selectedRule()
		_, label := ruleControlAction(ruleStatus(m.detail.View.Rules, index))
		hints = append(hints, shortcutHint{"[r]" + label, 0})
	}
	add, edit := m.configurationHints()
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
	if m.canDelete() {
		hints = append(hints, shortcutHint{"[x]删除", 0})
	}
	if canEdit {
		hints = append(hints, shortcutHint{"[v]改值", 0})
	}
	if canReport {
		hints = append(hints, shortcutHint{"[s]上报", 0})
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
	if m.view >= 5 && !m.follow {
		hints = append(hints, shortcutHint{"[g]跟随", 0})
	}
	if m.view == 3 {
		hints = append(hints, shortcutHint{"[←→]分类", 0})
	}
	if hasRows {
		hints = append(hints, shortcutHint{"[↑↓]选择", 2}, shortcutHint{"[PgUp/PgDn]翻页", 3})
	}
	if m.view != 0 || m.filter != "" {
		hints = append(hints, shortcutHint{"[Esc]返回", 2})
	}
	return shortcutHints(width, hints...)
}
