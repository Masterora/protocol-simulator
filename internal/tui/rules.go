package tui

import (
	"gytest-go/internal/core"
	"gytest-go/internal/observe"
	"strconv"
)

type ruleOperation struct {
	socket  string
	request observe.RuleRequest
}
type ruleReply struct {
	operation ruleOperation
	result    observe.RuleResult
	err       error
}

func (m *model) canControlRules() bool {
	if m.view != 4 || !m.connected || !m.detail.RuleOperations || !m.detail.RuleItemOperations || m.channel == "" || m.device == "" || m.detail.View.Rules == nil {
		return false
	}
	if _, ok := m.selectedRule(); !ok {
		return false
	}
	for _, c := range m.detail.View.Channels {
		if c.ID == m.channel {
			return c.Role == "server" && (c.Protocol == "iec104" || c.Protocol == "modbus-tcp" || c.Protocol == "modbus-rtu")
		}
	}
	return false
}
func ruleControlAction(status string) (action, label string) {
	switch status {
	case "running":
		return "stop", "停止规则"
	case "paused":
		return "resume", "恢复规则"
	default:
		return "start", "启动规则"
	}
}

func (m *model) beginRuleControl() {
	if !m.canControlRules() {
		m.setOperationNotice("请选择规则；旧工程服务需升级后才能单独控制规则")
		return
	}
	index, _ := m.selectedRule()
	action, _ := ruleControlAction(core.RuleRuntimeStatus(m.detail.View.Rules, index))
	m.pendingRule = &ruleOperation{m.socket, observe.RuleRequest{Index: &index, Channel: m.channel, Device: m.device, Revision: m.detail.ConfigurationRevision, Action: action}}
	m.operationBusy = true
}
func (m *model) applyRule(reply ruleReply) {
	m.operationBusy = false
	if reply.err != nil {
		m.setOperationNotice(reply.err.Error())
		return
	}
	if reply.operation.socket == m.socket && reply.operation.request.Channel == m.channel && reply.operation.request.Device == m.device {
		m.detail.View.Rules = reply.result.View
		m.refreshDetail()
	}
	m.setOperationNotice("所选规则" + map[string]string{"start": "已启动", "pause": "已暂停", "resume": "已恢复", "stop": "已停止"}[reply.operation.request.Action])
}

// Row IDs retain the original index after filtering.
func (m *model) selectedRule() (int, bool) {
	rows := m.rows()
	if len(rows) == 0 || m.detail.View.Rules == nil {
		return 0, false
	}
	i, err := strconv.Atoi(rows[min(m.cursor, len(rows)-1)].id)
	return i, err == nil && i >= 0 && i < len(m.detail.View.Rules.Config.Rules)
}
func ruleStatus(snapshot *core.AutoDataSnapshot, index int) string {
	return core.RuleRuntimeStatus(snapshot, index)
}
func ruleRuntimeState(snapshot *core.AutoDataSnapshot, index int) (string, string) {
	state := ruleStatus(snapshot, index)
	switch state {
	case "running":
		return state, green
	case "paused":
		return state, yellow
	case "error":
		return state, red
	default:
		return state, muted
	}
}
