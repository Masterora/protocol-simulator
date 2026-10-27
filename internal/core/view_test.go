package core

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func viewRequest(v View) ViewRequest {
	return ViewRequest{Channel: v.Channel, Device: v.Device, Metadata: v.Metadata, PointIdentity: v.PointIdentity, Points: v.PointVersion, Messages: v.MessageSequence, MessageEpoch: v.MessageEpoch, Events: v.EventSequence, EventEpoch: v.EventEpoch}
}
func viewFixture(t testing.TB) *Engine {
	t.Helper()
	e := NewEngine()
	c := probeChannel([]Point{{Kind: "yc", Address: 1}, {Kind: "yx", Address: 2}})
	c.ID = "a"
	c.Devices[0].Points = []Point{{Kind: "yc", Address: 1}, {Kind: "yx", Address: 2}}
	c.syncLegacy()
	if _, err := e.UpsertChannel(c); err != nil {
		t.Fatal(err)
	}
	return e
}
func TestViewOnlySendsSelectedPointsAndChanges(t *testing.T) {
	e := viewFixture(t)
	first := e.View(ViewRequest{})
	if first.Project == nil || !first.FullPoints || len(first.Points) != 2 {
		t.Fatalf("initial: %+v", first)
	}
	for _, c := range first.Project.Channels {
		if len(c.Points) != 0 {
			t.Fatal("legacy points leaked")
		}
		for _, d := range c.Devices {
			if len(d.Points) != 0 {
				t.Fatal("unselected table leaked")
			}
		}
	}
	next := e.View(viewRequest(first))
	if next.Project != nil || len(next.Points) != 0 || len(next.Messages) != 0 {
		t.Fatal("idle view copied payload")
	}
	if _, err := e.SetPointValue(first.Channel, first.Device, PointKey{"yc", 1}, 2); err != nil {
		t.Fatal(err)
	}
	delta := e.View(viewRequest(next))
	if delta.FullPoints || len(delta.Points) != 1 || delta.Points[0].Value != 2 || delta.Project != nil {
		t.Fatalf("delta: %+v", delta)
	}
	if _, err := e.SetPointValue(first.Channel, first.Device, PointKey{"yc", 1}, 3); err != nil {
		t.Fatal(err)
	}
	lagged := e.View(viewRequest(first))
	if len(lagged.Points) != 1 || lagged.Points[0].Value != 3 {
		t.Fatal("lagged reader lost latest point")
	}
	if err := e.RenameDevice(first.Channel, first.Device, "renamed"); err != nil {
		t.Fatal(err)
	}
	changed := e.View(viewRequest(lagged))
	if changed.Project == nil || !changed.FullPoints || len(changed.Points) != 2 {
		t.Fatal("metadata change did not reset table")
	}
	// Returned data is detached even if clients modify it.
	changed.Points[0].Value = 999
	changed.Project.Channels[0].Devices[0].Name = "bad"
	if reflect.DeepEqual(e.View(ViewRequest{}).Points, changed.Points) {
		t.Fatal("aliased point data")
	}
}
func TestViewMessageCursorsAndClear(t *testing.T) {
	e := viewFixture(t)
	first := e.View(ViewRequest{})
	e.log(first.Channel, "peer", "TX", []byte{1}, "sent")
	v := e.View(viewRequest(first))
	if len(v.Messages) != 1 || v.FirstMessage != v.Messages[0].Sequence {
		t.Fatal("missing appended message")
	}
	idle := e.View(viewRequest(v))
	if len(idle.Messages) != 0 {
		t.Fatal("resent log")
	}
	e.ClearMessages()
	cleared := e.View(viewRequest(idle))
	if !cleared.ResetMessages || len(cleared.Messages) != 0 {
		t.Fatal("clear not delivered")
	}
	e.log(first.Channel, "peer", "RX", []byte{2}, "received")
	after := e.View(viewRequest(cleared))
	if len(after.Messages) != 1 || after.Messages[0].Summary != "received" {
		t.Fatal("cursor after clear")
	}
	e.mu.Lock()
	for i := 0; i < 3000; i++ {
		e.sequence++
		e.messages = append(e.messages, Message{Sequence: e.sequence})
	}
	e.messages = e.messages[len(e.messages)-2:]
	e.mu.Unlock()
	trim := e.View(viewRequest(after))
	if !trim.ResetMessages || len(trim.Messages) != 2 {
		t.Fatal("trimmed ring not reset")
	}
}
func benchmarkViewEngine(b *testing.B) *Engine {
	e := NewEngine()
	e.project.Channels = make([]Channel, 64)
	for i := range e.project.Channels {
		c := probeChannel([]Point{{Kind: "yc", Address: 1}, {Kind: "yx", Address: 2}})
		c.ID = fmt.Sprint(i)
		c.Devices[0].Points = make([]Point, 1000)
		for j := range c.Devices[0].Points {
			c.Devices[0].Points[j] = Point{Kind: "yc", Address: uint32(j), Value: 1, Name: "p", WireType: 13}
		}
		c.syncLegacy()
		e.project.Channels[i] = c
	}
	return e
}
func BenchmarkViewIdle64K(b *testing.B) {
	e := benchmarkViewEngine(b)
	r := viewRequest(e.View(ViewRequest{}))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(e.View(r)); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkViewOneChange64K(b *testing.B) {
	e := benchmarkViewEngine(b)
	r := viewRequest(e.View(ViewRequest{}))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.mu.Lock()
		e.markPointsLocked(r.Channel, r.Device, []Point{{Kind: "yc", Address: 1, Value: float64(i)}})
		e.mu.Unlock()
		v := e.View(r)
		if _, err := json.Marshal(v); err != nil {
			b.Fatal(err)
		}
		r = viewRequest(v)
	}
}

func TestViewSwitchAndProjectReplacementResetReadModel(t *testing.T) {
	e := NewEngine()
	c := Channel{ID: "multi", Name: "multi", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: []Device{{ID: "one", Name: "one", Unit: 1, CommonAddress: 1, Points: []Point{{Kind: "holding", Address: 1, Value: 10}}}, {ID: "two", Name: "two", Unit: 2, CommonAddress: 1, Points: []Point{{Kind: "holding", Address: 2, Value: 20}}}}}
	c.Defaults()
	if _, err := e.UpsertChannel(c); err != nil {
		t.Fatal(err)
	}
	first := e.View(ViewRequest{Device: "one"})
	r := viewRequest(first)
	r.Device = "two"
	other := e.View(r)
	if !other.FullPoints || len(other.Points) != 1 || other.Points[0].Value != 20 || other.Project != nil {
		t.Fatal("device switch did not send full selected table")
	}
	e.mu.Lock()
	e.eventSequence++
	e.events = append(e.events, PointEvent{Sequence: e.eventSequence, ChannelID: c.ID, Address: 1})
	e.mu.Unlock()
	events := e.View(viewRequest(other))
	if len(events.Events) != 1 {
		t.Fatal("event missing")
	}
	project := e.Snapshot().Project
	if err := e.ReplaceProject(project, ""); err != nil {
		t.Fatal(err)
	}
	replaced := e.View(viewRequest(events))
	if replaced.Project == nil || !replaced.FullPoints || !replaced.ResetEvents || !replaced.ResetMessages || replaced.Generation == events.Generation || len(replaced.Events) != 0 {
		t.Fatal("same-ID replacement leaked previous runtime view")
	}
}
func TestViewPayloadForSaveable64KProject(t *testing.T) {
	e := benchmarkViewEngine(nil)
	persisted := cloneProject(e.project)
	for i := range persisted.Channels {
		persisted.Channels[i].Points = nil
	}
	saved, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil || len(saved) > 16<<20 {
		t.Fatal("fixture not saveable")
	}
	full, _ := json.Marshal(e.Snapshot())
	v := e.View(ViewRequest{})
	initial, _ := json.Marshal(v)
	idle, _ := json.Marshal(e.View(viewRequest(v)))
	if len(idle) > 20000 || len(initial) > len(full)/10 {
		t.Fatal("incremental payload scales with unrelated points")
	}
	t.Logf("points=64000 saveable_json_bytes=%d full_snapshot_bytes=%d initial_selected_view_bytes=%d idle_view_bytes=%d", len(saved), len(full), len(initial), len(idle))
}

func TestRuntimeDetailsAreBoundedDetachedAndDoNotChangeTheRecipe(t *testing.T) {
	points := make([]Point, 251)
	for i := range points {
		points[i] = Point{Kind: "yk", Address: uint32(100 + i)}
	}
	e, c, _ := autoFixture(t, points)
	prepareYK(t, e, c)
	id := c.Devices[0].ID
	e.autoMu.Lock()
	job := e.autoJobs[autoKey(c.ID, id, "yk")]
	e.autoMu.Unlock()
	job.change(func(v *AutoControlSnapshot) {
		for i := 0; i < 27; i++ {
			v.Errors = append(v.Errors, fmt.Sprint(i))
		}
		v.Current = 205
		v.Rows[205].Execution = "executed"
	})
	for _, tc := range []struct{ request, offset, size int }{{-10, 0, 100}, {100, 100, 100}, {200, 200, 51}, {99999, 200, 51}} {
		v, err := e.AutoControlDetails(c.ID, id, "yk", tc.request)
		if err != nil || v.Offset != tc.offset || len(v.Rows) != tc.size || v.RowCount != 251 || v.Current != 205 {
			t.Fatal(tc, v, err)
		}
		if len(v.Errors) != 20 || v.Errors[0] != "7" {
			t.Fatal("unbounded error output", v.Errors)
		}
		if v.Rows[0].Address != uint32(100+tc.offset) {
			t.Fatal("wrong runtime page")
		}
		v.Rows[0].Address = 999
		v.Errors[0] = "mutated"
	}
	v, _ := e.AutoControlDetails(c.ID, id, "yk", 200)
	if v.Rows[5].Execution != "executed" || v.Rows[0].Address != 300 || v.Errors[0] != "7" {
		t.Fatal("runtime response aliases live state", v)
	}
	if _, err := e.AutoControlDetails(c.ID, "deleted", "yk", 0); err == nil {
		t.Fatal("missing device accepted")
	}
	if _, err := e.AutoControlDetails(c.ID, id, "yx", 0); err == nil {
		t.Fatal("invalid category accepted")
	}
	if job.snapshot().Status != "idle" {
		t.Fatal("reading details started a task")
	}
}
