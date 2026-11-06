package core

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestServerConfigurationFailureDoesNotTouchLiveValuesAndSuccessfulMetadataEditPreservesThem(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	c := Channel{ID: "c", Name: "C", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: []Device{{ID: "d", Name: "D", Unit: 1, CommonAddress: 1, Points: []Point{{Kind: "holding", Address: 0, Name: "old", Value: 1}}}}}
	c.Defaults()
	previous := Project{Version: 2, Name: "P", Channels: []Channel{c}}
	if err := e.ReplaceProject(previous, filepath.Join(t.TempDir(), "p.json")); err != nil {
		t.Fatal(err)
	}
	if err := e.Start("c"); err != nil {
		t.Fatal(err)
	}
	value := float64(99)
	metadata := e.Inspect(InspectRequest{}).Metadata
	if _, err := e.OperatePoint(context.Background(), ManualPointRequest{Channel: "c", Device: "d", Kind: "holding", Address: 0, Action: "set", Value: &value, Metadata: metadata}); err != nil {
		t.Fatal(err)
	}
	next := cloneProject(previous)
	next.Channels[0].Devices[0].Points[0].Name = "new"
	before := e.Snapshot()
	if _, err := e.ApplyServerConfiguration(previous, next, ServerPointHint{}, func() error { return errors.New("disk full") }); err == nil {
		t.Fatal("failed commit accepted")
	}
	if !reflect.DeepEqual(before.Project, e.Snapshot().Project) {
		t.Fatal("save failure changed runtime")
	}
	effect, err := e.ApplyServerConfiguration(previous, next, ServerPointHint{}, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	saved := e.Snapshot()
	if len(effect.StoppedChannels) != 0 || len(effect.ResetDevices) != 0 || saved.Project.Channels[0].Devices[0].Points[0].Value != 99 || saved.Project.Channels[0].Devices[0].Points[0].Name != "new" {
		t.Fatal(effect, saved)
	}
	if saved.States[0].Status != before.States[0].Status || saved.States[0].Address != before.States[0].Address {
		t.Fatal("transport restarted", before.States, saved.States)
	}
}
func TestPersistedManualPointFailureDoesNotApplyOrSend(t *testing.T) {
	e := NewEngine()
	c := Channel{ID: "c", Name: "C", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: []Device{{ID: "d", Name: "D", CommonAddress: 1, Points: []Point{{Kind: "yc", Address: 1, Value: 1}}}}}
	c.Defaults()
	if err := e.ReplaceProject(Project{Version: 2, Name: "P", Channels: []Channel{c}}, "p.json"); err != nil {
		t.Fatal(err)
	}
	v := float64(42)
	req := ManualPointRequest{Channel: "c", Device: "d", Kind: "yc", Address: 1, Action: "set", Value: &v, Metadata: e.Inspect(InspectRequest{}).Metadata}
	result, err := e.OperatePointWithCommit(context.Background(), req, func() error { return errors.New("read only") })
	if err == nil || result.Applied || result.Saved {
		t.Fatal(result, err)
	}
	if e.Snapshot().Project.Channels[0].Devices[0].Points[0].Value != 1 {
		t.Fatal("changed despite failed save")
	}
}

func TestDeletingDeviceCancelsOnlyItsJobsAfterSuccessfulCommit(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	cfg := &AutoDataConfig{Rules: []AutoDataRule{{Kind: "holding", Points: "0", Enabled: true, IntervalMS: 200, Mode: "fixed", Min: 3}}}
	c := Channel{ID: "c", Name: "C", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: []Device{
		{ID: "a", Name: "A", Unit: 1, Points: []Point{{Kind: "holding", Address: 0}}, DataTests: map[string]*AutoDataConfig{"telemetry": cfg}},
		{ID: "b", Name: "B", Unit: 2, Points: []Point{{Kind: "holding", Address: 0}}, DataTests: map[string]*AutoDataConfig{"telemetry": cfg}},
	}}
	c.Defaults()
	before := Project{Version: 2, Name: "P", Channels: []Channel{c}}
	if err := e.ReplaceProject(before, filepath.Join(t.TempDir(), "p.json")); err != nil {
		t.Fatal(err)
	}
	if err := e.Start("c"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err := e.StartDataTest("c", id, "telemetry"); err != nil {
			t.Fatal(err)
		}
	}
	state := e.ChannelStates()[0]
	e.mu.Lock()
	steady := e.dataJobs[dataJobKey("c", "b", "telemetry")]
	e.mu.Unlock()
	next := cloneProject(before)
	next.Channels[0].Devices = next.Channels[0].Devices[1:]
	next.Channels[0].Points = nil
	if _, err := e.ApplyServerConfiguration(before, next, ServerPointHint{}, func() error { return errors.New("save failed") }); err == nil {
		t.Fatal("failed deletion committed")
	}
	e.mu.Lock()
	present := e.dataJobs[dataJobKey("c", "a", "telemetry")] != nil
	e.mu.Unlock()
	if !present {
		t.Fatal("save failure cancelled job")
	}
	if _, err := e.ApplyServerConfiguration(before, next, ServerPointHint{}, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	gone := e.dataJobs[dataJobKey("c", "a", "telemetry")] == nil
	same := e.dataJobs[dataJobKey("c", "b", "telemetry")] == steady
	e.mu.Unlock()
	if !gone || !same || e.ChannelStates()[0].Address != state.Address {
		t.Fatal("unrelated device or transport changed")
	}
}
