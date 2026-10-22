package core

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestControl104WireVectors(t *testing.T) {
	cases := []struct {
		typ       byte
		value     float64
		operation string
		payload   []byte
	}{
		{45, 1, "select", []byte{0x81}}, {45, 0, "execute", []byte{0}},
		{46, 2, "select", []byte{0x82}}, {46, 1, "cancel", []byte{1}},
		{47, 2, "execute", []byte{2}}, {48, -32768, "select", []byte{0, 0x80, 0x80}},
		{49, 1234, "execute", []byte{0xd2, 4, 0}}, {50, 1.5, "execute", []byte{0, 0, 0xc0, 0x3f, 0}},
	}
	for _, test := range cases {
		t.Run(fmt.Sprintf("%d-%s", test.typ, test.operation), func(t *testing.T) {
			request := Control104Request{Type: test.typ, Address: 0x123456, Value: test.value, Operation: test.operation}
			a, err := EncodeControl104(0x1234, request)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a[6:9], []byte{0x56, 0x34, 0x12}) || !bytes.Equal(a[9:], test.payload) || binary.LittleEndian.Uint16(a[4:]) != 0x1234 {
				t.Fatalf("wrong wire bytes: %x", a)
			}
			cot := byte(6)
			if test.operation == "cancel" {
				cot = 8
			}
			if a[0] != test.typ || a[1] != 1 || a[2] != cot {
				t.Fatalf("wrong header: %x", a)
			}
			decoded, err := decodeControl104(a)
			if err != nil || decoded.Type != request.Type || decoded.Address != request.Address || decoded.Value != request.Value || decoded.Operation != request.Operation {
				t.Fatalf("round trip: %+v %v", decoded, err)
			}
		})
	}
	invalid := []Control104Request{{Type: 44, Operation: "execute"}, {Type: 45, Value: 2, Operation: "execute"}, {Type: 46, Value: 3, Operation: "execute"}, {Type: 47, Value: 0, Operation: "select"}, {Type: 48, Value: 1.5, Operation: "execute"}, {Type: 49, Value: 32768, Operation: "execute"}, {Type: 50, Value: 1e100, Operation: "execute"}, {Type: 45, Value: 1, Address: 0x1000000, Operation: "select"}, {Type: 45, Value: 1, Operation: "invalid"}}
	for _, r := range invalid {
		if _, err := EncodeControl104(1, r); err == nil {
			t.Fatalf("invalid request accepted: %+v", r)
		}
	}
	a, _ := EncodeControl104(1, Control104Request{Type: 45, Value: 1, Operation: "select"})
	for _, bad := range [][]byte{a[:9], append(a, 0), append([]byte{45, 2}, a[2:]...)} {
		if _, err := decodeControl104(bad); err == nil {
			t.Fatal("malformed ASDU accepted")
		}
	}
}

func controlLoopback(t *testing.T) (*Engine, Channel, Channel) {
	t.Helper()
	e := NewEngine()
	t.Cleanup(e.StopAll)
	_, err := e.Demo("iec104")
	if err != nil {
		t.Fatal(err)
	}
	client, server := findChannel(e.Snapshot().Project, "client"), findChannel(e.Snapshot().Project, "server")
	eventually(t, func() bool {
		s, err := e.getSession(client.ID)
		if err != nil {
			return false
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for p := range s.peers {
			p.mu.Lock()
			ready := p.transfer
			p.mu.Unlock()
			if ready {
				return true
			}
		}
		return false
	})
	return e, client, server
}
func TestControl104LoopbackSelectExecuteCancel(t *testing.T) {
	for _, typ := range []byte{45, 46, 47, 48, 49, 50} {
		t.Run(fmt.Sprint(typ), func(t *testing.T) {
			e, client, server := controlLoopback(t)
			kind := controlKind(typ)
			value, expected := float64(1), float64(1)
			if typ == 46 {
				value = 2
			}
			if typ == 47 {
				value = 2
				expected = 11
			}
			if typ == 48 {
				value = -1234
				expected = value
			}
			if typ == 49 {
				value = 1234
				expected = value
			}
			if typ == 50 {
				value = 1.5
				expected = value
			}
			initial := float64(0)
			if typ == 47 {
				initial = 10
			}
			points := []Point{{Kind: kind, Address: 5000, Value: initial, Name: "control"}, {Kind: "yx", Address: 1, Value: 0}}
			if err := e.SetPoints(server.ID, points); err != nil {
				t.Fatal(err)
			}
			r := Control104Request{Type: typ, Address: 5000, Value: value, Operation: "select", TimeoutMS: 1000}
			result, err := e.Command104Device(client.ID, client.Devices[0].ID, r)
			if err != nil || result.Stage != "selected" {
				t.Fatalf("selection: %+v %v", result, err)
			}
			d, _ := e.Device(server.ID, server.Devices[0].ID)
			if d.Points[0].Value != initial {
				t.Fatal("selection changed output")
			}
			r.Operation = "execute"
			result, err = e.Command104Device(client.ID, client.Devices[0].ID, r)
			if err != nil || result.Stage != "completed" {
				t.Fatalf("execution: %+v %v", result, err)
			}
			d, _ = e.Device(server.ID, server.Devices[0].ID)
			if d.Points[0].Value != expected || d.Points[1].Value != 0 {
				t.Fatalf("wrong target/output: %+v", d.Points)
			}
			r.Operation = "select"
			if _, err = e.Command104Device(client.ID, client.Devices[0].ID, r); err != nil {
				t.Fatal(err)
			}
			r.Operation = "cancel"
			result, err = e.Command104Device(client.ID, client.Devices[0].ID, r)
			if err != nil || result.Stage != "cancelled" {
				t.Fatalf("cancel: %+v %v", result, err)
			}
			r.Operation = "execute"
			if _, err = e.Command104Device(client.ID, client.Devices[0].ID, r); err == nil {
				t.Fatal("executed after cancellation without new selection")
			}
			// Control points must never be encoded as telemetry during GI or publication.
			if err = e.InterrogateDevice(client.ID, client.Devices[0].ID); err != nil {
				t.Fatal(err)
			}
			eventually(t, func() bool {
				d, _ := e.Device(client.ID, client.Devices[0].ID)
				return len(d.Points) == 1 && d.Points[0].Kind == "yx"
			})
		})
	}
}
func TestControl104RejectsUnknownAddressAndChangedSelection(t *testing.T) {
	e, client, server := controlLoopback(t)
	if err := e.SetPoints(server.ID, []Point{{Kind: "yk", Address: 5000, Value: 0}}); err != nil {
		t.Fatal(err)
	}
	r := Control104Request{Type: 45, Address: 5001, Value: 1, Operation: "execute", Direct: true, TimeoutMS: 1000}
	result, err := e.Command104Device(client.ID, client.Devices[0].ID, r)
	if err == nil || result.Stage != "rejected" {
		t.Fatalf("unknown IOA: %+v %v", result, err)
	}
	r.Address = 5000
	r.Operation = "select"
	if _, err = e.Command104Device(client.ID, client.Devices[0].ID, r); err != nil {
		t.Fatal(err)
	}
	r.Operation = "execute"
	r.Value = 0
	r.Direct = false
	if _, err = e.Command104Device(client.ID, client.Devices[0].ID, r); err == nil {
		t.Fatal("changed value was executed")
	}
	// Direct operate is supported, but it cannot silently override an active,
	// conflicting selection on the controlled station.
	r.Direct = true
	result, err = e.Command104Device(client.ID, client.Devices[0].ID, r)
	if err == nil || result.Stage != "rejected" {
		t.Fatalf("conflicting selection: %+v %v", result, err)
	}
	d, _ := e.Device(server.ID, server.Devices[0].ID)
	if d.Points[0].Value != 0 {
		t.Fatal("negative response changed output")
	}
}
func TestControl104CorrelatesResponsesAndRequiresTermination(t *testing.T) {
	e := NewEngine()
	r := Control104Request{Type: 50, Address: 123, Value: 1.5, Operation: "execute", Direct: true}
	a, _ := EncodeControl104(1, r)
	wait := &controlWait{asdu: a, request: r, result: make(chan controlOutcome, 1)}
	p := &peer{s: &session{e: e, cfg: Channel{ID: "test"}}, control: wait}
	wrong := append([]byte{}, a...)
	wrong[6]++
	wrong[2] = 7
	_ = p.controlReply104(wrong)
	if p.control != wait {
		t.Fatal("unrelated response consumed request")
	}
	reply := append([]byte{}, a...)
	reply[2] = 10
	_ = p.controlReply104(reply)
	if len(wait.result) != 0 {
		t.Fatal("termination before activation accepted")
	}
	reply[2] = 7
	_ = p.controlReply104(reply)
	if len(wait.result) != 0 || !wait.activated {
		t.Fatal("activation reported as execution completion")
	}
	reply[2] = 10
	_ = p.controlReply104(reply)
	if outcome := <-wait.result; outcome.err != nil || outcome.result.Stage != "completed" {
		t.Fatal(outcome)
	}
}

type controlSink struct{ bytes.Buffer }

func (s *controlSink) Close() error { return nil }
func TestControl104TimeoutQuarantinesLateResponses(t *testing.T) {
	e := NewEngine()
	p := &peer{s: &session{e: e, cfg: Channel{ID: "test", TimeoutMS: 1000}}, conn: &controlSink{}, transfer: true, done: make(chan struct{})}
	r := Control104Request{Type: 45, Address: 123, Value: 1, Operation: "execute", Direct: true, TimeoutMS: 100}
	a, _ := EncodeControl104(1, r)
	if _, err := p.command104(a, r); err == nil || !strings.Contains(err.Error(), "结果未知") {
		t.Fatal(err)
	}
	late := append([]byte{}, a...)
	late[2] = 7
	_ = p.controlReply104(late)
	if _, err := p.command104(a, r); err == nil || !strings.Contains(err.Error(), "重连") {
		t.Fatalf("late reply could be reassociated: %v", err)
	}
	// A transport close releases an in-flight wait immediately.
	p = &peer{s: &session{e: e, cfg: Channel{ID: "test", TimeoutMS: 1000}}, conn: &controlSink{}, transfer: true, done: make(chan struct{})}
	result := make(chan error, 1)
	go func() { _, err := p.command104(a, r); result <- err }()
	time.Sleep(10 * time.Millisecond)
	p.close()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("closed connection succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("command blocked after close")
	}
}

var _ io.ReadWriteCloser = (*controlSink)(nil)

func TestControl104UnknownCommonAddressAndStopDT(t *testing.T) {
	e := NewEngine()
	sink := &controlSink{}
	p := &peer{s: &session{e: e, cfg: Channel{ID: "test", Protocol: "iec104", Role: "server", TimeoutMS: 1000}}, conn: sink, transfer: true, done: make(chan struct{})}
	r := Control104Request{Type: 45, Address: 123, Value: 1, Operation: "select"}
	a, _ := EncodeControl104(2, r)
	if err := p.handle104(IFrame(0, 0, a)); err != nil {
		t.Fatal(err)
	}
	_, _ = Read104(sink)
	reply, err := Read104(sink)
	if err != nil || reply[8] != (46|0x40) {
		t.Fatalf("missing unknown CA response: %x %v", reply, err)
	}
	wait := &controlWait{asdu: a, request: r, result: make(chan controlOutcome, 1)}
	p.control = wait
	p.clientSelections = map[controlKey]controlSelection{controlID(a): {controlSignature(a), time.Now().Add(time.Minute)}}
	if err = p.handle104(UFrame(StopAct)); err != nil {
		t.Fatal(err)
	}
	if p.transfer || len(p.clientSelections) != 0 || !p.controlUncertain {
		t.Fatal("STOPDT kept stale control state")
	}
	if outcome := <-wait.result; outcome.err == nil {
		t.Fatal("interrupted command was reported as successful")
	}
}
