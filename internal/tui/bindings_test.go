package tui

import (
	"encoding/json"
	"errors"
	"gytest-go/internal/core"
	"gytest-go/internal/projectfile"
	"strings"
	"testing"
)

func TestPointEditorIncludesBindingsWithoutAdditionalShortcut(t *testing.T) {
	m := editableModel()
	m.detail.ConfigurationOperations = true
	m.pointKind = "yk"
	m.detail.View.Points = []core.Point{{Kind: "yk", Address: 100}}
	m.detail.View.PointBindings = map[string][]core.ControlMapping{"yk:100": {}}
	for _, width := range []int{60, 80, 150} {
		screen := stripStyles(m.render(width, 24))
		if strings.Contains(screen, "[b]") || !strings.Contains(screen, "[e]编辑") {
			t.Fatal(screen)
		}
	}
	for _, key := range []string{"b", "B"} {
		m.key(key)
		if m.pendingConfiguration != nil || m.configurationEditor != nil {
			t.Fatal("removed shortcut still active")
		}
	}
	m.key("E")
	if m.pendingConfiguration == nil || m.pendingConfiguration.request.Resource != "point" {
		t.Fatal("not editing point")
	}
	op := *m.pendingConfiguration
	m.pendingConfiguration = nil
	values := projectfile.Object(core.Point{Kind: "yk", Address: 100, Name: "old"})
	values["category"] = json.RawMessage(`"yk"`)
	values["mappings"] = json.RawMessage(`[]`)
	view := projectfile.View{Protocol: "iec104", Revision: "r", BindingTargets: []core.Point{{Kind: "yx", Address: 2, Name: "二"}, {Kind: "yx", Address: 1, Name: "一"}}, Value: values}
	m.applyConfiguration(configurationResponse{operation: op, view: view})
	editor := m.configurationEditor
	if editor.title != "编辑点位" || editor.bindingStart != 6 {
		t.Fatal(editor)
	}
	editor.focus = 2
	m.key("新")
	m.key("名")
	editor.focus = editor.bindingStart
	m.key("1")
	m.key("down")
	m.key("2")
	m.key("enter")
	var mappings []core.ControlMapping
	if m.pendingConfiguration == nil || m.pendingConfiguration.request.Resource != "point" {
		t.Fatal("point and bindings not saved together")
	}
	req := m.pendingConfiguration.request
	json.Unmarshal(req.Fields["mappings"], &mappings)
	if len(req.Fields) != 2 || string(req.Fields["name"]) != `"新名"` || len(mappings) != 2 || mappings[0].Target.Address != 1 || mappings[0].Invert || !mappings[1].Invert {
		t.Fatal(req, mappings)
	}
	m.applyConfiguration(configurationResponse{err: errors.New("disk full")})
	if m.configurationEditor != editor || editor.values[editor.bindingStart+1] != "2" || editor.error != "disk full" {
		t.Fatal("failed save lost draft")
	}
	m.pendingConfiguration = nil
	m.key("esc")
	if m.configurationEditor != nil {
		t.Fatal("cancel failed")
	}
	m.applyConfiguration(configurationResponse{operation: op, view: view})
	m.key("enter")
	if m.pendingConfiguration != nil || m.configurationEditor != nil {
		t.Fatal("unchanged editor saved")
	}
	m.applyConfiguration(configurationResponse{operation: op, view: projectfile.View{Value: values}})
	if m.configurationEditor == nil || !strings.Contains(strings.Join(m.configurationBody(100, 24), "\n"), "无可绑定点位") {
		t.Fatal("empty target list broke ordinary editing")
	}
}
func TestModbusBindingChoicesDistinguishEqualAddresses(t *testing.T) {
	key := core.PointKey{Kind: "holding", Address: 200}
	op := configurationOperation{request: projectfile.Request{Resource: "point", Action: "read", Point: &key}}
	values := projectfile.Object(core.Point{Kind: "holding", Address: 200})
	values["category"] = json.RawMessage(`"yt"`)
	values["mappings"] = json.RawMessage(`[]`)
	e := newConfigurationEditor(op, projectfile.View{Protocol: "modbus-tcp", Value: values, BindingTargets: []core.Point{{Kind: "holding", Address: 1}, {Kind: "input", Address: 1}}})
	if e.fields[e.bindingStart].label == e.fields[e.bindingStart+1].label {
		t.Fatal("ambiguous Modbus targets")
	}
	if len(e.fields[e.bindingStart].choices) != 0 || len(e.bindingInputs) != 2 {
		t.Fatal("YT inverse offered")
	}
}

func TestBindingAddressInputPreservesRangesAndRejectsInvalidTargets(t *testing.T) {
	source := core.PointKey{Kind: "yk", Address: 100}
	op := configurationOperation{request: projectfile.Request{Resource: "point", Action: "read", Point: &source}}
	values := projectfile.Object(core.Point{Kind: "yk", Address: 100})
	values["category"] = json.RawMessage(`"yk"`)
	values["mappings"], _ = json.Marshal([]core.ControlMapping{{Source: source, Target: core.PointKey{Kind: "yx", Address: 1}}, {Source: source, Target: core.PointKey{Kind: "yx", Address: 2}}, {Source: source, Target: core.PointKey{Kind: "yx", Address: 10}, Invert: true}})
	e := newConfigurationEditor(op, projectfile.View{Protocol: "iec104", Value: values, BindingTargets: []core.Point{{Kind: "yx", Address: 1}, {Kind: "yx", Address: 2}, {Kind: "yx", Address: 10}}})
	if len(e.bindingInputs) != 2 || e.values[e.bindingStart] != "1-2" || e.values[e.bindingStart+1] != "10" {
		t.Fatal("existing bindings not compact", e.values)
	}
	if len(e.fields) != 8 || len(e.fieldChoices(e.bindingStart)) != 0 {
		t.Fatal("target list still offered")
	}
	for _, test := range []struct {
		forward, reverse string
		valid            bool
	}{{"1-2", "10", true}, {"1,10", "2", true}, {"1，2", "", true}, {"", "", true}, {"1-3", "", false}, {"1", "1", false}, {"16777216", "", false}, {"1,,2", "", false}, {"2-1", "", false}, {"1-10001", "", false}} {
		e.values[e.bindingStart], e.values[e.bindingStart+1] = test.forward, test.reverse
		fields, err := e.bindingFields()
		if (err == nil) != test.valid {
			t.Fatal(test, fields, err)
		}
		if test.valid && test.forward == "" && test.reverse == "" && string(fields["mappings"]) != "[]" {
			t.Fatal("clear did not produce empty bindings")
		}
	}
	m := editableModel()
	m.configurationEditor = e
	e.values[e.bindingStart] = "999"
	e.values[e.bindingStart+1] = ""
	m.key("enter")
	if m.pendingConfiguration != nil || m.configurationEditor != e || e.values[e.bindingStart] != "999" || !strings.Contains(e.error, "999") {
		t.Fatal("invalid input queued or lost draft")
	}
}
