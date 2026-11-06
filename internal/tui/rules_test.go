package tui

import (
	"errors"
	"gytest-go/internal/core"
	"gytest-go/internal/observe"
	"strings"
	"testing"
)

func ruleModel(status string) model {
	m := compactTableModel()
	m.view = 4
	m.detail.RuleOperations = true
	m.detail.RuleItemOperations = true
	m.detail.ConfigurationRevision = "revision"
	m.detail.View.Rules.Status = status
	return m
}

func TestSingleRuleShortcutSelectsCurrentActionAndPreservesTarget(t *testing.T) {
	for _, tc := range []struct{ status, key, action string }{
		{"stopped", "r", "start"}, {"stopped", "R", "start"},
		{"paused", "r", "resume"}, {"paused", "R", "resume"},
		{"running", "r", "stop"}, {"running", "R", "stop"},
		{"", "r", "start"},
	} {
		m := ruleModel(tc.status)
		m.key(tc.key)
		if m.picker != nil || !m.operationBusy || m.pendingRule == nil {
			t.Fatal(tc, "shortcut opened a menu or did not submit", m)
		}
		op := *m.pendingRule
		if op.request.Index == nil || *op.request.Index != 0 || op.request.Action != tc.action || op.socket != "s" || op.request.Channel != "c" || op.request.Device != "dev" || op.request.Revision != "revision" {
			t.Fatal(tc, op)
		}
		m.key(tc.key)
		if *m.pendingRule != op {
			t.Fatal("busy shortcut replaced the pending operation")
		}
		m.applyRule(ruleReply{operation: op, result: observe.RuleResult{View: &core.AutoDataSnapshot{Status: "running", Config: m.detail.View.Rules.Config}}})
		if m.operationBusy || m.detail.View.Rules.Status != "running" {
			t.Fatal(m.detail)
		}
	}
}

func TestRemovedRuleStopKeyAndCurrentCapability(t *testing.T) {
	for _, tc := range []struct{ status, key string }{{"running", "s"}, {"stopped", "s"}, {"paused", "S"}} {
		m := ruleModel(tc.status)
		m.key(tc.key)
		if m.pendingRule != nil || m.operationBusy || m.picker != nil {
			t.Fatal("removed stop key submitted a command", tc)
		}
	}
	m := ruleModel("running")
	m.key("r")
	m.applyRule(ruleReply{operation: *m.pendingRule, err: errors.New("configuration changed")})
	if m.operationBusy || !strings.Contains(m.operationNotice, "configuration changed") {
		t.Fatal(m.operationNotice)
	}
	for _, key := range []string{"r", "R"} {
		m = ruleModel("stopped")
		m.detail.RuleOperations = false
		m.key(key)
		if m.picker != nil || m.pendingRule != nil {
			t.Fatal("legacy service accepted operation")
		}
	}
	m = ruleModel("stopped")
	m.key("/")
	m.key("R")
	m.key("S")
	if m.input != "RS" || m.pendingRule != nil {
		t.Fatal("search triggered a task command", m)
	}
	m = ruleModel("stopped")
	m.key("?")
	m.key("r")
	if m.pendingRule != nil {
		t.Fatal("help triggered a task command")
	}
}

func TestDisplayAlwaysRefreshesAfterFormerPauseKeys(t *testing.T) {
	for view := 0; view < len(viewNames); view++ {
		for _, detail := range []bool{false, true} {
			m := ruleModel("running")
			m.view, m.detailOpen = view, detail
			for _, key := range []string{"p", "P", " "} {
				m.key(key)
				if m.pendingRule != nil || m.picker != nil || m.operationBusy {
					t.Fatal("former pause key changed task state", view, detail, key)
				}
				applyDetail(&m, core.Inspection{Points: []core.Point{{Kind: "yc", Address: 1, Value: 7}}})
				if len(m.detail.View.Points) != 1 || m.detail.View.Points[0].Value != 7 {
					t.Fatal("latest values not applied", view, detail, key)
				}
				text := stripStyles(m.render(150, 30))
				if strings.Contains(text, "[p]") {
					t.Fatal("removed pause action still displayed", text)
				}
			}
		}
	}
}

func TestRuleToggleUsesReplyStateAndUpdatesItsOnlyHint(t *testing.T) {
	m := ruleModel("stopped")
	for _, tc := range []struct{ action, next, label string }{
		{"start", "running", "启动规则"}, {"stop", "stopped", "停止规则"},
		{"start", "running", "启动规则"}, {"stop", "stopped", "停止规则"},
	} {
		hints := m.actionHints(150)
		if !strings.Contains(hints, "[r]"+tc.label) || strings.Contains(hints, "[s]") {
			t.Fatal(hints)
		}
		m.key("r")
		if m.pendingRule == nil || m.pendingRule.request.Action != tc.action {
			t.Fatal(tc, m.pendingRule)
		}
		m.applyRule(ruleReply{operation: *m.pendingRule, result: observe.RuleResult{View: &core.AutoDataSnapshot{Status: tc.next, Config: m.detail.View.Rules.Config}}})
	}
	m = ruleModel("paused")
	if !strings.Contains(m.actionHints(150), "[r]恢复规则") {
		t.Fatal(m.actionHints(150))
	}
	m.key("R")
	m.applyRule(ruleReply{operation: *m.pendingRule, result: observe.RuleResult{View: &core.AutoDataSnapshot{Status: "running", Config: m.detail.View.Rules.Config}}})
	if !strings.Contains(m.actionHints(150), "[r]停止规则") {
		t.Fatal(m.actionHints(150))
	}
}

func TestRuleHeadingIsPlainAndHintFollowsLiveUpdates(t *testing.T) {
	m := ruleModel("stopped")
	for _, tc := range []struct{ status, label string }{{"running", "停止规则"}, {"paused", "恢复规则"}, {"stopped", "启动规则"}} {
		info := m.detail
		snapshot := *info.View.Rules
		snapshot.Status = tc.status
		info.View.Rules = &snapshot
		m.apply(update{request: request{socket: m.socket, channel: m.channel, device: m.device, kind: m.pointKind}, socket: m.socket, channel: m.channel, device: m.device, kind: m.pointKind, detail: info})
		text := stripStyles(m.render(150, 30))
		title := strings.Split(text, "\r\n")[m.tableTop()]
		if !strings.Contains(title, "规则") || strings.Contains(title, " · ") {
			t.Fatal(title)
		}
		if !strings.Contains(text, "[r]"+tc.label) {
			t.Fatal(tc, text)
		}
	}
}

func TestRuleRowsFollowLiveTaskStateAndKeepDisabledRulesDistinct(t *testing.T) {
	m := ruleModel("stopped")
	config := core.AutoDataConfig{Rules: []core.AutoDataRule{{Enabled: true, Kind: "yc", Mode: "increment", Points: "1", IntervalMS: 200}, {Enabled: false, Kind: "yx", Mode: "toggle", Points: "2", IntervalMS: 200}}}
	for _, tc := range []struct{ state, label, tone string }{{"running", "运行中", green}, {"paused", "已暂停", yellow}, {"stopped", "已停止", muted}, {"running", "运行中", green}} {
		view := m.detail.View
		view.Rules = &core.AutoDataSnapshot{Status: tc.state, Config: config}
		applyDetail(&m, view)
		rows := m.rows()
		if len(rows) != 2 || rows[0].cells[1] != tc.label || rows[0].tone != tc.tone || rows[1].cells[1] != "已停用" {
			t.Fatalf("%s: %#v", tc.state, rows)
		}
		text := stripStyles(m.render(150, 30))
		if !strings.Contains(text, "状态") || strings.Contains(text, "启用") {
			t.Fatal(text)
		}
	}
	m.detail.View.Rules.Status = "stopped"
	m.detail.View.Rules.LastError = "send failed"
	if m.rows()[0].cells[1] != "异常" || m.rows()[1].cells[1] != "已停用" {
		t.Fatal(m.rows())
	}
}

func TestRuleToggleUpdatesVisibleStateImmediately(t *testing.T) {
	m := ruleModel("stopped")
	config := m.detail.View.Rules.Config
	for _, state := range []string{"running", "stopped", "running"} {
		m.beginRuleControl()
		op := *m.pendingRule
		m.applyRule(ruleReply{operation: op, result: observe.RuleResult{View: &core.AutoDataSnapshot{Status: state, Config: config}}})
		if got := m.rows()[0].cells[1]; got != translated(state, stateLabels) {
			t.Fatal(state, got)
		}
	}
}

func TestRuleRowsTrackEngineLifecycleWithoutChangingEnabledConfiguration(t *testing.T) {
	e := core.NewEngine()
	t.Cleanup(e.StopAll)
	config := core.AutoDataConfig{Rules: []core.AutoDataRule{{Enabled: true, Kind: "holding", Points: "0", Mode: "fixed", Min: 42, Max: 42, IntervalMS: 200}, {Enabled: false, Kind: "holding", Points: "1", Mode: "fixed", Min: 7, Max: 7, IntervalMS: 200}}}
	p := core.Project{Version: 2, Name: "rules", Channels: []core.Channel{{ID: "c", Name: "c", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: []core.Device{{ID: "dev", Name: "dev", Unit: 1, Points: []core.Point{{Kind: "holding", Address: 0}, {Kind: "holding", Address: 1}}, DataTests: map[string]*core.AutoDataConfig{"telemetry": &config}}}}}}
	if err := e.ReplaceProjectAndStop(p, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.Start("c"); err != nil {
		t.Fatal(err)
	}
	m := ruleModel("stopped")
	check := func(want string) {
		t.Helper()
		applyDetail(&m, e.Inspect(core.InspectRequest{Channel: "c", Device: "dev"}))
		rows := m.rows()
		if len(rows) != 2 || rows[0].cells[1] != want || rows[1].cells[1] != "已停用" {
			t.Fatal(want, rows)
		}
		if !m.detail.View.Rules.Config.Rules[0].Enabled {
			t.Fatal("runtime operation disabled configuration")
		}
	}
	check("已停止")
	if _, err := e.StartDataTest("c", "dev", "telemetry"); err != nil {
		t.Fatal(err)
	}
	check("运行中")
	for _, tc := range []struct{ action, label string }{{"pause", "已暂停"}, {"resume", "运行中"}, {"stop", "已停止"}} {
		if _, err := e.SetDataTestState("c", "dev", "telemetry", tc.action); err != nil {
			t.Fatal(err)
		}
		check(tc.label)
	}
	if _, err := e.StartDataTest("c", "dev", "telemetry"); err != nil {
		t.Fatal(err)
	}
	check("运行中")
	e.Stop("c")
	check("已停止")
}

func TestRuleShortcutUsesFilteredSelectionAndIndividualState(t *testing.T) {
	m := ruleModel("running")
	m.detail.View.Rules.Config.Rules = []core.AutoDataRule{{Enabled: true, Kind: "yc", Mode: "random", Points: "1"}, {Enabled: false, Kind: "yx", Mode: "toggle", Points: "2"}}
	m.detail.View.Rules.RuleStates = []string{"running", "stopped"}
	m.filter = "遥信"
	if !strings.Contains(m.actionHints(150), "[r]启动规则") {
		t.Fatal(m.actionHints(150))
	}
	m.key("R")
	if m.pendingRule == nil || m.pendingRule.request.Index == nil || *m.pendingRule.request.Index != 1 || m.pendingRule.request.Action != "start" {
		t.Fatal(m.pendingRule)
	}
	m = ruleModel("running")
	m.filter = "no-matching-rule"
	m.key("r")
	if m.pendingRule != nil {
		t.Fatal("empty selection submitted")
	}
	m = ruleModel("running")
	m.detail.RuleItemOperations = false
	m.key("r")
	if m.pendingRule != nil {
		t.Fatal("legacy observer submitted device-wide operation")
	}
}
