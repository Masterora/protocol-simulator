package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSaveRenamesProjectAndPersistsChosenFilename(t *testing.T) {
	e := NewEngine()
	if err := e.EnsureWorkspaceChannel("client"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	var firstPath string
	for i, filename := range []string{"现场联调.json", "工程2.v1.JSON"} {
		path := filepath.Join(root, filename)
		if err := e.Save(path); err != nil {
			t.Fatal(err)
		}
		want := []string{"现场联调", "工程2.v1"}[i]
		got := e.Snapshot()
		if got.Project.Name != want || got.Path != path || got.Dirty {
			t.Fatalf("save metadata not synchronized: %+v", got)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var project Project
		if err = json.Unmarshal(data, &project); err != nil || project.Name != want {
			t.Fatalf("persisted name: %q %v", project.Name, err)
		}
		loaded := NewEngine()
		if err = loaded.Load(path); err != nil || loaded.Snapshot().Project.Name != want {
			t.Fatalf("reopened name not preserved: %v", err)
		}
		if i == 0 {
			firstPath = path
		}
	}
	data, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	var original Project
	if err = json.Unmarshal(data, &original); err != nil || original.Name != "现场联调" {
		t.Fatal("save as changed the old file")
	}
}

func TestFailedSavePreservesNamePathAndDirtyState(t *testing.T) {
	e := NewEngine()
	if err := e.EnsureWorkspaceChannel("client"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := e.Save(filepath.Join(root, "原工程.json")); err != nil {
		t.Fatal(err)
	}
	if err := e.RenameChannel(e.Snapshot().Project.Channels[0].ID, "changed"); err != nil {
		t.Fatal(err)
	}
	before := e.Snapshot()
	for _, path := range []string{filepath.Join(root, "missing", "失败.json"), filepath.Join(root, ".json")} {
		if err := e.Save(path); err == nil {
			t.Fatal("invalid save unexpectedly succeeded")
		}
		if !reflect.DeepEqual(e.Snapshot(), before) {
			t.Fatal("failed save changed current project")
		}
	}
}
