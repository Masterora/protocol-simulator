package core

import (
	"reflect"
	"testing"
	"time"
)

func TestIndividualRulesKeepSiblingTimersCyclesAndConfiguration(t *testing.T) {
	e := NewEngine()
	t.Cleanup(e.StopAll)
	cfg := AutoDataConfig{Rules: []AutoDataRule{
		{Enabled: true, Kind: "holding", Points: "0", Mode: "increment", Min: 0, Max: 1000, Step: 1, IntervalMS: 200},
		{Enabled: true, Kind: "holding", Points: "1", Mode: "increment", Min: 0, Max: 1000, Step: 1, IntervalMS: 200},
		{Enabled: false, Kind: "holding", Points: "2", Mode: "fixed", Min: 7, Max: 7, IntervalMS: 200},
	}}
	p := Project{Version: 2, Name: "single", Channels: []Channel{{ID: "c", Name: "channel", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: []Device{{ID: "d", Name: "device", Unit: 1, Points: []Point{{Kind: "holding", Address: 0}, {Kind: "holding", Address: 1}, {Kind: "holding", Address: 2}}, DataTests: map[string]*AutoDataConfig{"telemetry": &cfg}}}}}}
	if err := e.ReplaceProjectAndStop(p, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.Start("c"); err != nil {
		t.Fatal(err)
	}
	control := func(i int, action string) AutoDataSnapshot {
		t.Helper()
		v, err := e.ControlDataRule("c", "d", "telemetry", i, action)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	v := control(0, "start")
	if !reflect.DeepEqual(v.RuleStates, []string{"running", "stopped", "disabled"}) {
		t.Fatal(v)
	}
	control(1, "start")
	e.op.Lock()
	e.mu.Lock()
	j := e.dataJobs[dataJobKey("c", "d", "telemetry")]
	next := j.next[1]
	e.mu.Unlock()
	e.op.Unlock()
	v = control(0, "stop")
	e.mu.Lock()
	sameTimer := j.next[1] == next
	e.mu.Unlock()
	if v.Status != "running" || v.RuleStates[1] != "running" || !sameTimer {
		t.Fatal("sibling reset", v)
	}
	time.Sleep(500 * time.Millisecond)
	v, _ = e.GetDataTest("c", "d", "telemetry")
	if v.Cycles[0] != 0 || v.Cycles[1] == 0 {
		t.Fatal("wrong rule executed", v)
	}
	control(0, "start")
	control(2, "start")
	if _, err := e.SetDataTestState("c", "d", "telemetry", "pause"); err != nil {
		t.Fatal(err)
	}
	v = control(0, "resume")
	if !reflect.DeepEqual(v.RuleStates, []string{"running", "paused", "paused"}) {
		t.Fatal(v)
	}
	control(0, "stop")
	control(1, "stop")
	v = control(2, "stop")
	if v.Status != "stopped" {
		t.Fatal(v)
	}
	v = control(2, "start")
	if !reflect.DeepEqual(v.RuleStates, []string{"stopped", "stopped", "running"}) || !reflect.DeepEqual(v.Config, cfg) {
		t.Fatal(v)
	}
	v.RuleStates[2] = "corrupt"
	after, _ := e.GetDataTest("c", "d", "telemetry")
	if after.RuleStates[2] != "running" {
		t.Fatal("snapshot aliases state")
	}
	inspection := e.Inspect(InspectRequest{Channel: "c", Device: "d"})
	inspection.Rules.RuleStates[2] = "corrupt"
	after, _ = e.GetDataTest("c", "d", "telemetry")
	if after.RuleStates[2] != "running" {
		t.Fatal("inspection aliases state")
	}
	for _, i := range []int{-1, 3} {
		if _, err := e.ControlDataRule("c", "d", "telemetry", i, "stop"); err == nil {
			t.Fatal("invalid index accepted")
		}
	}
	if got := TelemetryConfiguration(e.Snapshot().Project.Channels[0].Devices[0]); !reflect.DeepEqual(got, cfg) {
		t.Fatal("configuration modified", got)
	}
}
