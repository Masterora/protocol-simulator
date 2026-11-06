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
				return v.info.DeletionOperations
			}
		}
		return false
	}
	return m.connected && m.detail.DeletionOperations && len(m.rows()) > 0
}
func (m *model) beginDelete() {
	if !m.canDelete() {
		m.setOperationNotice("此工程服务需升级后才能删除配置")
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
	impact := "永久删除选中配置并保存，无法恢复。"
	switch m.view {
	case 0:
		op.request.Channel = ""
		op.request.Device = ""
		impact = "停止此工程，永久删除工程文件及全部通道、装置、点位和规则。其他工程继续运行。"
	case 1:
		impact = "停止并永久删除此通道及其全部装置、点位和规则。其他通道继续运行。"
	case 2:
		impact = "永久删除此装置及其全部点位和规则。此通道与其他装置继续运行。"
	case 3:
		if p, ok := m.selectedPoint(); ok {
			name = fmt.Sprintf("%s %d · %s", translated(p.Kind, kindLabels), p.Address, p.Name)
		}
		impact = "永久删除此点位。被规则或关联引用的点位需先解除引用。"
	case 4:
		impact = "永久删除此自动上报规则，其余规则继续运行。"
	}
	m.deleting = &deleteConfirmation{operation: op, name: sanitize(name), impact: impact}
}
func (m *model) deletionKey(key string) {
	d := m.deleting
	switch strings.ToLower(key) {
	case "esc":
		m.deleting = nil
	case "left", "right", "up", "down", "tab", "shift-tab", " ":
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
	return []string{clipped("目标："+d.name, min(width-7, 108)-4, false), "此操作不可恢复。", d.impact, "", cancel, confirm}
}
