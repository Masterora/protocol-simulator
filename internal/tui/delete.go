package tui

import (
	"fmt"
	"strings"
)

type deleteConfirmation struct {
	operation    configurationOperation
	name, impact string
	confirm      bool
}

func (m *model) canDelete() bool {
	if m.view < 0 || m.view > 4 {
		return false
	}
	if m.view == 0 {
		rows := m.rows()
		if len(rows) == 0 {
			return false
		}
		id := rows[min(m.cursor, len(rows)-1)].id
		for _, v := range m.instances {
			if v.socket == id {
				return v.err == nil && v.info.ConfigurationOperations && v.info.DeletionOperations
			}
		}
		return false
	}
	_, edit := m.configurationActions()
	return edit && m.detail.DeletionOperations
}
func (m *model) beginDelete() {
	if !m.canDelete() {
		return
	}
	op, ok := m.configurationTarget("delete")
	if !ok {
		return
	}
	rows := m.rows()
	if len(rows) == 0 {
		return
	}
	r := rows[min(m.cursor, len(rows)-1)]
	name := r.text
	if len(r.cells) > 0 {
		name = r.cells[0]
	}
	if m.view == 0 && len(r.cells) > 1 {
		name = r.cells[1]
	}
	impact := "删除选中的配置并保存。"
	switch m.view {
	case 0:
		op.request.Channel = ""
		op.request.Device = ""
		impact = "停止工程并删除文件及全部配置。"
	case 1:
		impact = "停止通道并删除所属装置、点位和规则。"
	case 2:
		impact = "删除装置及所属点位和规则。"
	case 3:
		if p, ok := m.selectedPoint(); ok {
			name = fmt.Sprintf("%s %d · %s", translated(p.Kind, kindLabels), p.Address, p.Name)
		}
		impact = "点位被规则或绑定引用时，需先解除引用。"
	case 4:
		impact = "删除选中的自动上报规则。"
	}
	m.deleting = &deleteConfirmation{operation: op, name: sanitize(name), impact: impact}
}
func (m *model) deletionKey(key string) {
	d := m.deleting
	switch strings.ToLower(key) {
	case "esc":
		m.deleting = nil
	case "left", "right":
		d.confirm = !d.confirm
	case "enter":
		if !d.confirm {
			m.deleting = nil
			return
		}
		operation := d.operation
		m.deleting = nil
		m.pendingConfiguration = &operation
		m.operationBusy = true
	}
}
func (m *model) deletionBody(width int) []string {
	d := m.deleting
	cancel, confirm := "› 取消", "  永久删除"
	if d.confirm {
		cancel, confirm = "  取消", "› 永久删除"
	}
	return []string{clipped("目标："+d.name, min(width-7, 108)-4, false), d.impact, "此操作不可恢复。", cancel + "    " + confirm}
}
