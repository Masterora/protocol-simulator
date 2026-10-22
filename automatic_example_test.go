package main

import (
	"gytest-go/internal/core"
	"testing"
)

func TestAutomaticExampleLoadsWithoutStartingConnections(t *testing.T) {
	e := core.NewEngine()
	defer e.StopAll()
	if err := e.Load("examples/automatic-loopback.json"); err != nil {
		t.Fatal(err)
	}
	snapshot := e.Snapshot()
	if len(snapshot.Project.Channels) != 2 {
		t.Fatal("invalid example channels")
	}
	for _, state := range snapshot.States {
		if state.Status != "stopped" {
			t.Fatal("loading the example started a connection")
		}
	}
	for _, channel := range snapshot.Project.Channels {
		if channel.Role == "client" {
			v, err := e.PrepareAutoControl(channel.ID, channel.Devices[0].ID, "yk", core.AutoControlSeed{})
			if err != nil || len(v.Rows) != 2 || v.Rows[0].FeedbackName == "未关联" {
				t.Fatalf("example does not demonstrate the original automatic table: %+v %v", v, err)
			}
		}
	}
}
