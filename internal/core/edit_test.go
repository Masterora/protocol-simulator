package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChannelMessageClearAndRename(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "before", Protocol: "raw", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	e.log(id, "", "RX", []byte{1}, "first")
	e.log("other", "", "RX", []byte{2}, "keep")
	e.ClearChannelMessages(id)
	snap := e.Snapshot()
	if len(snap.Messages) != 1 || snap.Messages[0].ChannelID != "other" {
		t.Fatalf("cleared another channel: %+v", snap.Messages)
	}
	if err = e.RenameChannel(id, "  after  "); err != nil {
		t.Fatal(err)
	}
	if got := e.Snapshot().Project.Channels[0].Name; got != "after" {
		t.Fatal(got)
	}
	if e.RenameChannel(id, "  ") == nil {
		t.Fatal("accepted empty name")
	}
}

func TestPointPatchPreservesLiveChanges(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "edit", Protocol: "iec104", Transport: "tcp-server", Role: "server", Address: "127.0.0.1:0", Points: []Point{{Address: 1, Kind: "yc", Name: "before", Value: 1}, {Address: 2, Kind: "yc", Value: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	stale := e.Snapshot().Project.Channels[0].Points[0]
	e.applyPoints(id, []Point{{Address: 1, Kind: "yc", Value: 99, Quality: 0x80}, {Address: 2, Kind: "yc", Value: 77}})
	stale.Name = "renamed"
	if err = e.PatchPoint(id, &PointKey{Kind: "yc", Address: 1}, stale, []string{"name"}); err != nil {
		t.Fatal(err)
	}
	points := e.Snapshot().Project.Channels[0].Points
	if points[0].Value != 99 || points[0].Quality != 0x80 || points[0].Name != "renamed" || points[1].Value != 77 {
		t.Fatalf("live data overwritten: %+v", points)
	}
	if err = e.PatchPoint(id, &PointKey{Kind: "yc", Address: 1}, Point{Address: 2}, []string{"address"}); err == nil {
		t.Fatal("duplicate identity accepted")
	}
	if e.Snapshot().Project.Channels[0].Points[0].Address != 1 {
		t.Fatal("failed edit changed original")
	}
	if err = e.DeletePoint(id, PointKey{Kind: "yc", Address: 1}); err != nil {
		t.Fatal(err)
	}
	if err = e.PatchPoint(id, &PointKey{Kind: "yc", Address: 1}, stale, []string{"name"}); err == nil {
		t.Fatal("stale edit recreated removed point")
	}
	if len(e.Snapshot().Project.Channels[0].Points) != 1 {
		t.Fatal("unrelated point removed")
	}
}

func TestInvalidReplacementKeepsConnection(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	id, err := e.UpsertChannel(Channel{Name: "active", Protocol: "raw", Transport: "tcp-server", Role: "server", Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "bad.json")
	if err = os.WriteFile(file, []byte(`{"version":1,"name":"bad","channels":[{"id":"x","name":"bad","protocol":"invalid"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err = e.LoadReplacing(file); err == nil {
		t.Fatal("invalid file accepted")
	}
	if e.Snapshot().States[0].Status != "listening" {
		t.Fatal("invalid file stopped existing connection")
	}
	if err = e.ReplaceProjectAndStop(Project{Version: 1, Name: "replacement", Channels: []Channel{}}, ""); err != nil {
		t.Fatal(err)
	}
	if len(e.Snapshot().Project.Channels) != 0 || len(e.running) != 0 {
		t.Fatal("replacement left running session")
	}
}
