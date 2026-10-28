package tui

import (
	"gytest-go/internal/core"
	"gytest-go/internal/observe"
	"strings"
	"testing"
)

func editableModel() model {
	return model{view: 3, socket: "s", channel: "c", device: "d", connected: true, discovered: true, detail: observe.Info{PointOperations: true, View: core.Inspection{Metadata: 1, Channels: []core.InspectChannel{{ID: "c", Protocol: "iec104", Role: "server"}}, Points: []core.Point{{Kind: "yc", Address: 10, Value: 42, Name: "Voltage"}}, PointMatches: 1, PointTotal: 94}}}
}
func TestPointEditorPatchesOnlyEditedFieldsWhileLiveValuesChange(t *testing.T) {
	m := editableModel()
	m.key("e")
	if m.editor == nil {
		t.Fatal("no editor")
	}
	m.detail.View.Points[0].Value = 99
	m.key("tab")
	for _, k := range []string{"1", "2", "8", "enter"} {
		m.key(k)
	}
	if m.pendingOperation == nil || m.pendingOperation.request.Value != nil || m.pendingOperation.request.Quality == nil || *m.pendingOperation.request.Quality != 128 || !m.operationBusy {
		t.Fatal(m.pendingOperation)
	}
	m.key("enter")
	if m.pendingOperation == nil {
		t.Fatal("queued request vanished")
	}
	m.applyManual(manualReply{operation: *m.pendingOperation, result: core.ManualPointResult{Applied: true, Mode: "reported", Receivers: 1}})
	if m.editor != nil || m.operationBusy || m.paused {
		t.Fatal("manual operation changed lifecycle")
	}
}
func TestEditorCancellationAndInvalidValuesDoNotWrite(t *testing.T) {
	m := editableModel()
	m.key("e")
	m.key("esc")
	if m.pendingOperation != nil || m.editor != nil {
		t.Fatal(m)
	}
	m.key("e")
	for _, k := range []string{"-", "enter"} {
		m.key(k)
	}
	if m.pendingOperation != nil || m.editor.error == "" {
		t.Fatal(m.editor)
	}
	for _, size := range [][2]int{{60, 12}, {90, 24}, {150, 35}} {
		if !strings.Contains(stripStyles(m.render(size[0], size[1])), "数值必须") {
			t.Fatal("validation hidden")
		}
	}
	m.key("esc")
	m.detail.PointOperations = false
	m.key("e")
	if m.editor != nil || m.pendingOperation != nil {
		t.Fatal("old backend accepts writes")
	}
}
func TestPointSummaryIsOnlyPresentForSearchAndPaginationStaysAtBottom(t *testing.T) {
	m := editableModel()
	for _, filter := range []string{"", "Voltage"} {
		m.filter = filter
		plain := stripStyles(m.render(150, 30))
		lines := strings.Split(plain, "\r\n")
		if strings.Contains(plain, "装置总计") || strings.Contains(plain, "点位 1-") {
			t.Fatal(plain)
		}
		if strings.Contains(plain, "匹配") != (filter != "") {
			t.Fatal(plain)
		}
		if !strings.Contains(lines[len(lines)-3], "1-1 / 1") {
			t.Fatal("pagination moved", plain)
		}
	}
}

func TestEditorRejectsNegativeQualityAndUnparseableTextRatherThanDroppingCharacters(t *testing.T) {
	for _, text := range []string{"-1", "abc123", "品质128"} {
		m := editableModel()
		m.key("e")
		m.key("tab")
		for _, r := range text {
			m.key(string(r))
		}
		m.key("enter")
		if m.pendingOperation != nil || m.editor == nil || m.editor.error == "" {
			t.Fatal(text, m.editor)
		}
	}
	m := editableModel()
	m.key("e")
	m.key("中")
	m.key("backspace")
	if m.editor.values[0] != "" {
		t.Fatal(m.editor.values[0])
	}
}
