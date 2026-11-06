package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func communicationAt(t *testing.T, e *Engine, c Channel, kind string, address uint32) string {
	t.Helper()
	v := e.Inspect(InspectRequest{Channel: c.ID, Device: c.Devices[0].ID})
	for _, p := range v.Points {
		if p.Kind == kind && p.Address == address {
			return p.CommunicationAt
		}
	}
	return ""
}
func TestPointCommunicationTracksFourRemoteWireTrafficAndUnchangedReports(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yx", Address: 1}, {Kind: "yc", Address: 10, WireType: 13, Value: 42}, {Kind: "yk", Address: 100}, {Kind: "yt", Address: 200}})
	sid := s.Devices[0].ID
	for _, ch := range []Channel{c, s} {
		if communicationAt(t, e, ch, "yc", 10) != "" {
			t.Fatal("unreported point has communication time")
		}
	}
	if err := e.PublishDevice(s.ID, sid); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return communicationAt(t, e, c, "yc", 10) != "" })
	first := communicationAt(t, e, s, "yc", 10)
	if _, err := time.Parse(time.RFC3339Nano, first); err != nil {
		t.Fatal(first, err)
	}
	if communicationAt(t, e, s, "yk", 100) != "" {
		t.Fatal("data report stamped unrelated control")
	}
	initial := e.View(ViewRequest{Channel: s.ID, Device: sid})
	if err := e.PublishDevice(s.ID, sid); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return communicationAt(t, e, s, "yc", 10) != first })
	delta := e.View(ViewRequest{Channel: s.ID, Device: sid, Metadata: initial.Metadata, PointIdentity: initial.PointIdentity, Points: initial.PointVersion})
	if delta.FullPoints || len(delta.Points) != 2 || delta.PointVersion <= initial.PointVersion {
		t.Fatal("unchanged report not delivered incrementally", delta)
	}
	for _, p := range delta.Points {
		if p.CommunicationAt == "" || p.Value != pointAt(t, e, s, p.Kind, p.Address).Value {
			t.Fatal(p)
		}
	}
	for _, r := range []Control104Request{{Type: 45, Address: 100, Value: 1, Operation: "execute", Direct: true, TimeoutMS: 1000}, {Type: 50, Address: 200, Value: 2, Operation: "execute", Direct: true, TimeoutMS: 1000}} {
		if _, err := e.Command104Device(c.ID, c.Devices[0].ID, r); err != nil {
			t.Fatal(err)
		}
		kind := controlKind(r.Type)
		eventually(t, func() bool {
			return communicationAt(t, e, s, kind, r.Address) != "" && communicationAt(t, e, c, kind, r.Address) != ""
		})
	}
	before := communicationAt(t, e, s, "yc", 10)
	// Observation and local edits must never be confused with wire communication.
	for i := 0; i < 3; i++ {
		e.Inspect(InspectRequest{Channel: s.ID, Device: sid})
		e.View(ViewRequest{Channel: s.ID, Device: sid})
	}
	if _, err := e.EditPointCell(c.ID, c.Devices[0].ID, PointKey{"yc", 10}, "value", Point{Value: 43}); err != nil {
		t.Fatal(err)
	}
	if communicationAt(t, e, s, "yc", 10) != before {
		t.Fatal("read/local edit advanced timestamp")
	}
	e.Stop(s.ID)
	value := 55.0
	_, err := e.OperatePoint(context.Background(), ManualPointRequest{Channel: s.ID, Device: sid, Kind: "yc", Address: 10, Action: "set", Value: &value, Metadata: e.Inspect(InspectRequest{}).Metadata})
	if err != nil || communicationAt(t, e, s, "yc", 10) != before {
		t.Fatal("offline edit stamped communication", err)
	}
	path := filepath.Join(t.TempDir(), "project.json")
	if err := e.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "communicationAt") {
		t.Fatal("runtime communication persisted")
	}
	if err := e.LoadReplacing(path); err != nil {
		t.Fatal(err)
	}
	if communicationAt(t, e, s, "yc", 10) != "" {
		t.Fatal("new runtime inherited old communication")
	}
}

func TestPointCommunicationWaitsForActual104WriteAndIgnoresFailure(t *testing.T) {
	e := NewEngine()
	c := probeChannel([]Point{{Kind: "yc", Address: 1, WireType: 13}})
	e.project = Project{Version: 2, Name: "wire", Channels: []Channel{c}}
	s := newSession(e, c)
	e.running[c.ID] = s
	sink := &controlSink{}
	p := &peer{s: s, conn: sink, transfer: true, done: make(chan struct{})}
	// A full send window queues the point; queue acceptance is not transmission.
	p.unacked = []sentI{{sequence: 0, at: time.Now()}}
	p.tx = 1
	c.Devices[0].Configuration = &DeviceConfiguration{Values: map[string]string{"sendWindow": "1"}}
	e.project.Channels[0] = c
	d := c.Devices[0]
	if err := p.publishPoints(d, d.Points, 3); err != nil {
		t.Fatal(err)
	}
	if communicationAt(t, e, c, "yc", 1) != "" {
		t.Fatal("queued point stamped before write")
	}
	p.mu.Lock()
	err := p.ackLocked(1)
	p.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	first := communicationAt(t, e, c, "yc", 1)
	if first == "" {
		t.Fatal("successful flush missing time")
	}
	p.mu.Lock()
	p.unacked = nil
	p.conn = &failedPointWriter{}
	p.mu.Unlock()
	if err := p.publishPoints(d, d.Points, 3); err == nil {
		t.Fatal("write failure not surfaced")
	}
	if communicationAt(t, e, c, "yc", 1) != first {
		t.Fatal("failed write advanced timestamp")
	}
}

type failedPointWriter struct{}

func (*failedPointWriter) Read([]byte) (int, error)  { return 0, errors.New("failed") }
func (*failedPointWriter) Write([]byte) (int, error) { return 0, errors.New("failed") }
func (*failedPointWriter) Close() error              { return nil }

func TestPointCommunicationModbusPollAndWriteAreScopedToActualPoints(t *testing.T) {
	e, c, s := dataLoopback(t, "modbus-tcp")
	if err := e.ReadModbus(c.ID, 3, 0, 1); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return communicationAt(t, e, c, "holding", 0) != "" && communicationAt(t, e, s, "holding", 0) != ""
	})
	first := communicationAt(t, e, s, "holding", 0)
	if communicationAt(t, e, s, "coil", 0) != "" {
		t.Fatal("read stamped another type at same address")
	}
	if err := e.ReadModbus(c.ID, 3, 0, 1); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return communicationAt(t, e, s, "holding", 0) != first })
	if err := e.WriteModbus(c.ID, 5, 0, 1); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return communicationAt(t, e, c, "coil", 0) != "" && communicationAt(t, e, s, "coil", 0) != ""
	})
}
