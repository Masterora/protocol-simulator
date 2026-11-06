package tui

import (
	"gytest-go/internal/core"
	"gytest-go/internal/projectfile"
	"strings"
	"testing"
)

func configField(e *configurationEditor, key string) int {
	for i, f := range e.fields {
		if f.key == key {
			return i
		}
	}
	panic(key)
}
func channelEditorModel() model {
	return model{configurationEditor: newConfigurationEditor(configurationOperation{request: projectfile.Request{Resource: "channel", Action: "read"}}, projectfile.View{Value: projectfile.Object(core.Channel{Name: "Existing", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:2404", Baud: 9600, Parity: "N", DataBits: 8, StopBits: 1, TimeoutMS: 3000})})}
}
func TestConfigurationChoicesAreVisibleDraftOnlyAndProtocolAware(t *testing.T) {
	m := channelEditorModel()
	e := m.configurationEditor
	e.focus = configField(e, "protocol")
	for _, key := range []string{"x", "backspace", "ctrl-u"} {
		m.key(key)
	}
	if e.value("protocol") != "iec104" {
		t.Fatal("finite option accepted text")
	}
	m.key(" ")
	if m.picker == nil {
		t.Fatal("options not discoverable")
	}
	m.key("down")
	m.key("esc")
	if e.value("protocol") != "iec104" || m.configurationEditor == nil {
		t.Fatal("cancel changed draft")
	}
	m.key(" ")
	m.key("down")
	m.key("down")
	m.key("enter")
	if e.value("protocol") != "modbus-rtu" || m.pendingConfiguration != nil {
		t.Fatal("selection submitted form", e.values)
	}
	e.focus = configField(e, "transport")
	m.key(" ")
	m.key("home")
	m.key("enter")
	if e.value("transport") != "serial" {
		t.Fatal(e.values)
	}
	e.focus = configField(e, "protocol")
	m.key(" ")
	m.key("home")
	m.key("enter")
	if e.value("protocol") != "iec104" || e.value("transport") != "tcp-server" {
		t.Fatal("incompatible transport retained", e.values)
	}
	e.focus = configField(e, "role")
	m.key(" ")
	for _, size := range [][2]int{{60, 12}, {90, 24}, {150, 30}} {
		frame := stripStyles(m.render(size[0], size[1]))
		if !strings.Contains(frame, "选择角色") || !strings.Contains(frame, "子站") || !strings.Contains(frame, "[Esc]") {
			t.Fatal("choice or exit hidden", frame)
		}
	}
	m.key("down")
	m.key("enter")
	m.key("enter")
	if m.pendingConfiguration == nil || string(m.pendingConfiguration.request.Fields["role"]) != "\"client\"" || len(m.pendingConfiguration.request.Fields) != 1 {
		t.Fatal("wrong change set", m.pendingConfiguration)
	}
}
func TestWireTypeChoicesFollowKindAndPersistNumericCode(t *testing.T) {
	m := model{configurationEditor: newConfigurationEditor(configurationOperation{request: projectfile.Request{Resource: "point", Action: "read"}}, projectfile.View{Protocol: "iec104", Value: projectfile.Object(core.Point{Kind: "yc", Address: 10, Value: 0, WireType: 13})})}
	e := m.configurationEditor
	e.focus = configField(e, "kind")
	m.key("left")
	if e.value("kind") != "yx" || e.value("wireType") != "0" {
		t.Fatal("wire type did not follow kind", e.values)
	}
	e.focus = configField(e, "wireType")
	m.key(" ")
	if got := strings.Join(m.picker.values, ","); got != "0,1,3,30,31" {
		t.Fatal("unexpected YX choices", got)
	}
	m.key("down")
	m.key("down")
	m.key("enter")
	m.key("enter")
	if m.pendingConfiguration == nil || string(m.pendingConfiguration.request.Fields["wireType"]) != "3" {
		t.Fatal(m.pendingConfiguration)
	}
}
func TestWireTypePickerDoesNotOfferUnsupportedSavedType(t *testing.T) {
	m := model{configurationEditor: newConfigurationEditor(configurationOperation{request: projectfile.Request{Resource: "point", Action: "read"}}, projectfile.View{Protocol: "iec104", Value: projectfile.Object(core.Point{Kind: "yx", Address: 10, WireType: 2})})}
	e := m.configurationEditor
	e.focus = configField(e, "wireType")
	m.key(" ")
	if got := strings.Join(m.picker.values, ","); got != "0,1,3,30,31" {
		t.Fatal("unsupported saved code offered", got)
	}
	m.key("esc")
	if got := e.value("wireType"); got != "2" {
		t.Fatal("opening the picker changed the saved code", got)
	}
}

func TestManualQualityChoicesDoNotSavePolledValue(t *testing.T) {
	m := editableModel()
	m.key("v")
	m.key("tab")
	m.key(" ")
	m.key("down")
	m.key("enter")
	if m.editor.values[1] != "128" || m.pendingOperation != nil {
		t.Fatal("quality picker wrote runtime")
	}
	m.detail.View.Points[0].Value = 999
	m.key("enter")
	if m.pendingOperation == nil || m.pendingOperation.request.Value != nil || m.pendingOperation.request.Quality == nil || *m.pendingOperation.request.Quality != 128 {
		t.Fatal(m.pendingOperation)
	}
}

func TestListenerChoicesComeFromTargetProcessAndKeepPort(t *testing.T) {
	m := channelEditorModel()
	e := m.configurationEditor
	e.localAddresses = []string{"10.20.12.198", "127.0.0.1"}
	i := configField(e, "address")
	choices := e.fieldChoices(i)
	if len(choices) != 2 || choices[0] != "10.20.12.198:2404" || choices[1] != "127.0.0.1:2404" {
		t.Fatal(choices)
	}
	e.focus = i
	m.key(" ")
	m.key("home")
	m.key("enter")
	if e.value("address") != "10.20.12.198:2404" || m.pendingConfiguration != nil {
		t.Fatal("selection wrote or changed port", e.values)
	}
	if len(newConfigurationEditor(e.operation, projectfile.View{Value: projectfile.Object(core.Channel{Transport: "tcp-server", Address: "127.0.0.1:2404"})}).localAddresses) != 0 {
		t.Fatal("legacy server used viewer host addresses")
	}
}
func TestShortcutFocusAndInputScopes(t *testing.T) {
	m := channelEditorModel()
	m.key("shift-tab")
	if m.configurationEditor.focus != len(m.configurationEditor.fields)-1 {
		t.Fatal("reverse traversal")
	}
	m.configurationEditor.focus = configField(m.configurationEditor, "name")
	m.key("n")
	m.key(" ")
	m.key("q")
	if m.configurationEditor.value("name") != "n q" || m.pendingConfiguration != nil {
		t.Fatal("text activated global action")
	}
	if !strings.Contains(m.actionHints(59), "[Esc]") || strings.Contains(m.actionHints(59), "[n]") {
		t.Fatal("form shows list actions")
	}
	m.key("esc")
	m.view = 0
	m.key("shift-tab")
	if m.view != 6 {
		t.Fatal("reverse page traversal")
	}
	m.key("f1")
	if !m.help {
		t.Fatal("F1 help")
	}
	m.key("end")
	m.key("home")
	if m.detailScroll != 0 {
		t.Fatal("help navigation")
	}
	m.key("esc")
	m.key("/")
	for _, key := range []string{"shift-tab", "home", "end", "f1", "ctrl-a"} {
		m.key(key)
	}
	m.key("测")
	if m.input != "测" {
		t.Fatal("special key leaked into input", m.input)
	}
}
func TestCustomBaudAndExistingMetadataArePreserved(t *testing.T) {
	m := channelEditorModel()
	e := m.configurationEditor
	e.focus = configField(e, "baud")
	for _, r := range "12345" {
		m.key(string(r))
	}
	m.key(" ")
	if !containsChoice(m.picker.values, "12345") {
		t.Fatal("custom baud missing")
	}
	m.key("esc")
	m.key("enter")
	if m.pendingConfiguration == nil || string(m.pendingConfiguration.request.Fields["baud"]) != "12345" {
		t.Fatal(m.pendingConfiguration)
	}
}

func TestGlobalAndContextActionsRemainVisibleInNarrowTerminals(t *testing.T) {
	m := editableModel()
	for _, width := range []int{59, 89, 149} {
		for view := 0; view < 7; view++ {
			m.view = view
			lines := strings.Split(stripStyles(m.render(width+1, 24)), "\r\n")
			if !strings.Contains(lines[1], "[q]退出") || !strings.Contains(lines[1], "[?]帮助") || strings.Contains(lines[len(lines)-2], "[q]") {
				t.Fatal(width, view, lines)
			}
			if len(m.rows()) > 0 && !strings.Contains(lines[len(lines)-2], "[Enter]") {
				t.Fatal("primary action hidden", width, view, lines)
			}
			for _, line := range []string{lines[1], lines[len(lines)-2]} {
				if strings.Count(line, "[") != strings.Count(line, "]") {
					t.Fatal("partial shortcut", width, view, line)
				}
			}
		}
	}
	m = channelEditorModel()
	m.configurationEditor.focus = configField(m.configurationEditor, "role")
	m.key(" ")
	for _, width := range []int{59, 89, 149} {
		line := fit(m.actionHints(width), width)
		if !strings.Contains(line, "[Enter]") || !strings.Contains(line, "[Esc]") {
			t.Fatal(width, line)
		}
		if header := m.globalHints(width); !strings.Contains(header, "[Ctrl+C]") || strings.Contains(header, "[q]") {
			t.Fatal("text input shows global letter shortcuts", header)
		}
	}
}

func TestSelectingCurrentOptionDoesNotRewriteOtherMetadata(t *testing.T) {
	for _, tc := range []struct {
		resource, key string
		value         map[string]any
	}{
		{"rule", "kind", map[string]any{"kind": "yc", "cause": 0, "mode": "report"}},
		{"point", "dataType", map[string]any{"kind": "holding", "dataType": "uint16", "byteOrder": "ABCD"}},
	} {
		protocol := "iec104"
		if tc.resource == "point" {
			protocol = "modbus-tcp"
		}
		m := model{configurationEditor: newConfigurationEditor(configurationOperation{request: projectfile.Request{Resource: tc.resource, Action: "read"}}, projectfile.View{Protocol: protocol, Value: projectfile.Fields(tc.value)})}
		e := m.configurationEditor
		e.focus = configField(e, tc.key)
		m.key(" ")
		m.key("enter")
		m.key("enter")
		if m.pendingConfiguration != nil || m.configurationEditor != nil {
			t.Fatal("unchanged selection wrote configuration", m.pendingConfiguration)
		}
	}
}
