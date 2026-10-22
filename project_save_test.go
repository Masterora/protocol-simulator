package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectSaveFilename(t *testing.T) {
	for _, item := range []struct{ name, want string }{
		{"工程1", "工程1.json"},
		{"现场联调", "现场联调.json"},
		{"工程2.json", "工程2.json"},
		{"现场/测试:1", "现场_测试_1.json"},
		{"", "工程1.json"},
	} {
		if got := projectSaveFilename(item.name); got != item.want {
			t.Errorf("%q: got %q want %q", item.name, got, item.want)
		}
	}
}

func TestSaveExistingProjectKeepsItsChosenPath(t *testing.T) {
	a := NewApp()
	if err := a.engine.EnsureWorkspaceChannel("client"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "custom-name.json")
	if err := a.engine.Save(path); err != nil {
		t.Fatal(err)
	}
	id := a.engine.Snapshot().Project.Channels[0].ID
	if err := a.engine.RenameChannel(id, "updated channel"); err != nil {
		t.Fatal(err)
	}
	got, err := a.SaveProject(false)
	if err != nil || got != path {
		t.Fatalf("save changed existing path: %q %v", got, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "updated channel") {
		t.Fatalf("existing file was not updated: %v", err)
	}
}
