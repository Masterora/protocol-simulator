package tui

import (
	"gytest-go/internal/core"
	"gytest-go/internal/observe"
	"strings"
	"testing"
)

func deletableModel() model {
	m := editableModel()
	m.detail.ConfigurationOperations = true
	m.detail.DeletionOperations = true
	m.detail.ConfigurationRevision = "original"
	return m
}
func TestDeleteConfirmationPinsTargetAndRevisionAndDefaultsToCancel(t *testing.T) {
	m := deletableModel()
	m.key("X")
	if m.deleting == nil || m.deleting.confirm {
		t.Fatal("no safe default")
	}
	for _, size := range [][2]int{{60, 12}, {90, 24}, {150, 30}} {
		s := stripStyles(m.render(size[0], size[1]))
		if !strings.Contains(s, "确认永久删除") || !strings.Contains(s, "取消") {
			t.Fatal(s)
		}
	}
	m.key("enter")
	if m.deleting != nil || m.pendingConfiguration != nil {
		t.Fatal("default Enter deleted")
	}
	m.key("delete")
	m.detail.ConfigurationRevision = "later"
	m.detail.View.Points[0].Address = 99
	m.key("right")
	m.key("enter")
	op := m.pendingConfiguration
	if op == nil || op.request.Action != "delete" || op.request.Point.Address != 10 || op.request.Revision != "original" {
		t.Fatal("target changed with refresh", op)
	}
}
func TestCaseInsensitiveCommandsAndOriginalCaseText(t *testing.T) {
	for _, letter := range []string{"n", "e", "v", "i", "x", "c", "p", "f", "l", "g", "q"} {
		low, high := deletableModel(), deletableModel()
		if letter == "c" {
			low.view, high.view = 1, 1
			low.detail.View.Channels = []core.InspectChannel{{ID: "c"}}
			high.detail.View.Channels = low.detail.View.Channels
		}
		a, b := low.key(letter), high.key(strings.ToUpper(letter))
		if a != b || low.view != high.view || (low.pendingConfiguration == nil) != (high.pendingConfiguration == nil) || (low.editor == nil) != (high.editor == nil) || (low.deleting == nil) != (high.deleting == nil) || low.detailOpen != high.detailOpen {
			t.Fatal("case-dependent shortcut", letter)
		}
	}
	m := channelEditorModel()
	m.configurationEditor.focus = configField(m.configurationEditor, "name")
	for _, r := range "AbCd" {
		m.key(string(r))
	}
	if m.configurationEditor.value("name") != "AbCd" {
		t.Fatal("text folded")
	}
	m.key("esc")
	m.key("/")
	for _, r := range "AbCd" {
		m.key(string(r))
	}
	if m.input != "AbCd" {
		t.Fatal("search text folded")
	}
}
func TestDeleteProjectUsesSelectedProjectWithoutNestedTarget(t *testing.T) {
	m := deletableModel()
	m.view = 0
	m.instances = []instance{{socket: "s", info: observe.Info{DeletionOperations: true, ConfigurationOperations: true, ConfigurationRevision: "project-revision", View: core.Inspection{Name: "Target"}}}}
	m.key("x")
	if m.deleting == nil || m.deleting.operation.request.Channel != "" || m.deleting.operation.request.Device != "" {
		t.Fatal(m.deleting)
	}
	m.key("esc")
	if m.pendingConfiguration != nil {
		t.Fatal("cancel submitted")
	}
}
