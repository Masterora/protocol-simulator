package tui

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gytest-go/internal/core"
	processinstance "gytest-go/internal/instance"
	"gytest-go/internal/observe"
	"gytest-go/internal/projectfile"
)

func TestDeviceCreationSubmitsMultiplePointKindsTogether(t *testing.T) {
	for _, protocol := range []string{"iec104", "modbus-tcp", "modbus-rtu"} {
		t.Run(protocol, func(t *testing.T) {
			project := core.Project{Version: 2, Name: "测试", Channels: []core.Channel{projectfile.DefaultChannel("通道")}}
			channel := &project.Channels[0]
			channel.Protocol = protocol
			op := configurationOperation{request: projectfile.Request{Resource: "device", Action: "defaults", Channel: channel.ID}}
			view, err := projectfile.DefaultView(projectfile.Document{Project: project}, op.request)
			if err != nil {
				t.Fatal(err)
			}
			m := model{configurationEditor: newConfigurationEditor(op, view)}
			editor := m.configurationEditor
			kinds := projectfile.DevicePointKinds(protocol)
			for i, f := range editor.fields {
				if kind, ok := strings.CutPrefix(f.key, initialCount); ok {
					editor.values[i] = "0"
					if kind == kinds[0] {
						editor.values[i-1], editor.values[i] = "3", "3"
					} else if kind == kinds[1] {
						editor.values[i-1], editor.values[i] = "1", "1"
					}
				}
			}
			m.configurationKey("enter")
			if m.pendingConfiguration == nil {
				t.Fatal(editor.error)
			}
			fields := m.pendingConfiguration.request.Fields
			var selections map[string]string
			if err := json.Unmarshal(fields["initialPoints"], &selections); err != nil || len(selections) != 2 || fields["pointKind"] != nil {
				t.Fatal(selections, err, fields)
			}
			result, _, err := projectfile.Apply(project, m.pendingConfiguration.request)
			if err != nil {
				t.Fatal(err)
			}
			points := result.Channels[0].Devices[0].Points
			if len(points) != 4 || points[0].Kind != kinds[0] || points[2].Address != 5 || points[3].Kind != kinds[1] {
				t.Fatal(points)
			}
		})
	}
}

func TestInitialDeviceTableKeepsTheActiveCellVisible(t *testing.T) {
	channel := projectfile.DefaultChannel("通道")
	op := configurationOperation{request: projectfile.Request{Resource: "device", Action: "defaults", Channel: channel.ID}}
	view, err := projectfile.DefaultView(projectfile.Document{Project: core.Project{Version: 2, Name: "工程", Channels: []core.Channel{channel}}}, op.request)
	if err != nil {
		t.Fatal(err)
	}
	editor := newConfigurationEditor(op, view)
	m := model{configurationEditor: editor}
	editor.focusField(editor.initialTable)
	m.configurationKey("tab")
	m.configurationKey("down")
	if editor.fields[editor.focus].key != initialCount+"yc" {
		t.Fatal("down moved out of the count column")
	}
	for _, size := range [][2]int{{60, 12}, {90, 24}} {
		body := m.configurationBody(size[0], size[1])
		if len(body) > m.overlayRows(size[1]) {
			t.Fatal("table overflowed the dialog", body)
		}
		if !strings.Contains(strings.Join(body, "\n"), initialPointHeading()) {
			t.Fatal("table heading is missing", body)
		}
		found := false
		for _, row := range body {
			found = found || strings.HasPrefix(row, "› 遥测") && strings.HasSuffix(row, "50"+inputCaret)
		}
		if !found {
			t.Fatal("active count is not visible on the telemetry row", body)
		}
	}
}

func TestInitialDeviceTableUsesProtocolSpecificStarts(t *testing.T) {
	tests := []struct {
		protocol string
		starts   map[string]string
	}{
		{"iec104", map[string]string{"yx": "1", "yc": "16385", "yk": "24577", "yt": "25089", "mc": "25601", "position": "4097", "step": "24833"}},
		{"modbus-tcp", map[string]string{"discrete": "1", "holding": "1024", "coil": "2560", "input": "0"}},
		{"modbus-rtu", map[string]string{"discrete": "1", "holding": "1024", "coil": "2560", "input": "0"}},
	}
	for _, tt := range tests {
		t.Run(tt.protocol, func(t *testing.T) {
			// An already-running project can still send the former shared pointStart value.
			op := configurationOperation{request: projectfile.Request{Resource: "device", Action: "defaults"}}
			view := projectfile.View{Protocol: tt.protocol, Value: projectfile.Fields(map[string]any{"initialPoints": map[string]string{}, "pointStart": 1})}
			editor := newConfigurationEditor(op, view)
			for kind, want := range tt.starts {
				if got := editor.value(initialStart + kind); got != want {
					t.Errorf("%s start = %q, want %q", kind, got, want)
				}
				if got := editor.value(initialCount + kind); got != "50" {
					t.Errorf("%s count = %q, want 50", kind, got)
				}
			}
		})
	}
}

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
