package tui

import (
	"errors"
	"strings"
	"testing"

	"gytest-go/internal/core"
	processinstance "gytest-go/internal/instance"
	"gytest-go/internal/observe"
	"gytest-go/internal/projectfile"
)

func TestConfigurationEditorPreservesDraftAndOnlySavesExplicitChanges(t *testing.T) {
	m := editableModel()
	m.detail.ConfigurationOperations = true
	m.detail.ConfigurationRevision = "revision"
	m.key("E")
	if m.pendingConfiguration == nil || m.pendingConfiguration.request.Point.Address != 10 {
		t.Fatal("wrong selected target")
	}
	op := *m.pendingConfiguration
	m.pendingConfiguration = nil
	view := projectfile.View{Revision: "revision", Protocol: "iec104", Value: projectfile.Object(core.Point{Kind: "yc", Address: 10, Name: "Voltage", Value: 42})}
	m.applyConfiguration(configurationResponse{operation: op, view: view})
	m.key("tab")
	m.key("tab")
	m.key("新")
	m.key("名")
	m.detail.View.Points[0].Value = 99
	m.key("enter")
	if m.pendingConfiguration == nil || len(m.pendingConfiguration.request.Fields) != 1 || string(m.pendingConfiguration.request.Fields["name"]) != "\"新名\"" {
		t.Fatal(m.pendingConfiguration)
	}
	m.applyConfiguration(configurationResponse{err: errors.New("disk full")})
	if m.configurationEditor == nil || m.configurationEditor.values[2] != "新名" {
		t.Fatal("failed save lost draft")
	}
	for _, size := range [][2]int{{60, 12}, {90, 24}, {150, 30}} {
		if !strings.Contains(stripStyles(m.render(size[0], size[1])), "disk full") {
			t.Fatal("save error hidden", m.render(size[0], size[1]))
		}
	}
	m.pendingConfiguration = nil
	m.key("esc")
	if m.configurationEditor != nil || m.pendingConfiguration != nil {
		t.Fatal("cancel wrote config")
	}
	m.applyConfiguration(configurationResponse{operation: op, view: view})
	m.key("enter")
	if m.pendingConfiguration != nil || m.configurationEditor != nil {
		t.Fatal("unchanged editor wrote config")
	}
}
func TestNewConfigurationFormsExposeDefaultsAndKeepRuleZeroIndex(t *testing.T) {
	for _, resource := range []string{"device", "point", "rule"} {
		op := configurationOperation{request: projectfile.Request{Resource: resource, Action: "defaults"}}
		e := newConfigurationEditor(op, projectfile.View{Protocol: "iec104", Value: projectfile.Fields(map[string]any{"pointCount": 50, "count": 1, "enabled": false})})
		if !strings.HasPrefix(e.title, "新增") {
			t.Fatal(e.title)
		}
		if resource == "device" {
			found := false
			for _, f := range e.fields {
				found = found || f.key == "pointCount"
			}
			if !found {
				t.Fatal("default point count unavailable")
			}
		}
	}
	m := editableModel()
	m.view = 4
	m.detail.ConfigurationOperations = true
	m.detail.View.Rules = &core.AutoDataSnapshot{Config: core.AutoDataConfig{Rules: []core.AutoDataRule{{Kind: "yc", Points: "10"}}}}
	op, ok := m.configurationTarget("edit")
	if !ok || op.request.Index == nil || *op.request.Index != 0 {
		t.Fatal(op, ok)
	}
}

func TestNewProjectRemainsSelectedAfterOldPollResponse(t *testing.T) {
	m := editableModel()
	m.view = 0
	m.instances = []instance{{socket: "s", info: m.detail}}
	old := request{socket: m.socket, channel: m.channel, device: m.device}
	child := processinstance.Started{Socket: "new", Info: observe.Info{View: core.Inspection{Name: "new"}}}
	m.applyConfiguration(configurationResponse{operation: configurationOperation{create: true}, started: &child})
	m.apply(update{request: old, instances: []instance{{socket: "s"}}})
	m.key("2")
	if m.socket != "new" || len(m.instances) != 2 {
		t.Fatal("new project selection lost to old polling response", m.socket, m.instances)
	}
}
