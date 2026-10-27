package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func probeChannel(points []Point) Channel {
	c := Channel{ID: "review", Name: "review", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", CommonAddress: 1, Points: points}
	c.Defaults()
	return c
}
func TestScaledAutomaticGenerationUsesWireIntegerType(t *testing.T) {
	for _, typ := range []byte{11, 35} {
		p := Point{Kind: "yc", Address: 1, WireType: typ, Value: 0}
		r := AutoDataRule{Enabled: true, Kind: "yc", Points: "1", IntervalMS: 200, Mode: "increment", Min: 0, Max: 10, Step: .5}
		config := AutoDataConfig{Rules: []AutoDataRule{r}}
		if err := validateAutoData("iec104", "server", &config, p); err == nil {
			t.Fatal("fractional integer step accepted")
		}
		r.Mode = "random"
		r.Step = 1
		config.Rules[0] = r
		if err := validateAutoData("iec104", "server", &config, p); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 200; i++ {
			p.Value = nextDataValue(p, r)
			if err := validatePoints([]Point{p}, "iec104"); err != nil {
				t.Fatal("random integer value invalid", p.Value, err)
			}
		}
	}
}
func TestAllPointTableMutationsRespectDeviceWidth(t *testing.T) {
	for _, op := range []string{"cell", "patch", "table"} {
		t.Run(op, func(t *testing.T) {
			c := probeChannel([]Point{{Kind: "yc", Address: 1, Value: 0, WireType: 13}})
			c.Devices[0].Configuration = &DeviceConfiguration{Version: "标准版", Values: map[string]string{"txtMess_Byte": "1"}}
			e := NewEngine()
			if _, err := e.UpsertChannel(c); err != nil {
				t.Fatal(err)
			}
			key := PointKey{"yc", 1}
			p := Point{Kind: "yc", Address: 256, WireType: 13}
			var err error
			switch op {
			case "cell":
				_, err = e.ConfigurePointCell(c.ID, c.Devices[0].ID, key, "address", p)
			case "patch":
				err = e.ConfigureDevicePoint(c.ID, c.Devices[0].ID, &key, p, []string{"address"})
			case "table":
				err = e.ConfigureDevicePoints(c.ID, c.Devices[0].ID, []Point{p})
			}
			if err == nil {
				t.Fatal("narrow address accepted")
			}
			path := t.TempDir() + "/review.json"
			if err := e.Save(path); err != nil {
				t.Fatal(err)
			}
			if err := NewEngine().Load(path); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestStoppedAutomaticCycleCannotCommitAfterPeerWait(t *testing.T) {
	c := probeChannel([]Point{{Kind: "yc", Address: 1, Value: 0, WireType: 13}})
	e := NewEngine()
	e.project = Project{Version: 2, Name: "probe", Channels: []Channel{c}}
	s := newSession(e, c)
	e.running[c.ID] = s
	p := &peer{s: s, transfer: true}
	s.peers[p] = true
	ctx, cancel := context.WithCancel(context.Background())
	pending, cancelPending := context.WithCancel(ctx)
	j := &autoDataJob{view: AutoDataSnapshot{Channel: c.ID, Device: c.Devices[0].ID, Status: "running", Config: AutoDataConfig{Rules: []AutoDataRule{{Enabled: true, Kind: "yc", Points: "1", IntervalMS: 200, Mode: "increment", Min: 0, Max: 10, Step: 1}}}, Cycles: []uint64{0}}, ctx: ctx, cancel: cancel, pending: pending, cancelPending: cancelPending, next: []time.Time{time.Time{}}}
	e.dataJobs = map[string]*autoDataJob{dataJobKey(c.ID, c.Devices[0].ID, ""): j}
	s.mu.Lock()
	done := make(chan struct{})
	go func() { e.stepAutoData(j, time.Now()); close(done) }()
	blocked := false
	for k := 0; k < 100; k++ {
		buf := make([]byte, 128<<10)
		n := runtime.Stack(buf, true)
		if strings.Contains(string(buf[:n]), "gytest-go/internal/core.readyDataPeers") {
			blocked = true
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !blocked {
		s.mu.Unlock()
		t.Fatal("probe didn't reach peer-lock boundary")
	}
	_, err := e.SetAutoDataState(c.ID, c.Devices[0].ID, "stop")
	if err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	stopped, _ := e.Device(c.ID, c.Devices[0].ID)
	s.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("step did not return")
	}
	after, _ := e.Device(c.ID, c.Devices[0].ID)
	if stopped.Points[0].Value != 0 || after.Points[0].Value != 0 {
		t.Fatalf("probe unexpected states %v -> %v", stopped.Points, after.Points)
	}

}
func BenchmarkReviewSnapshot64K(b *testing.B) {
	e := NewEngine()
	for i := 0; i < 64; i++ {
		p := make([]Point, 1000)
		for j := range p {
			p[j] = Point{Kind: "yc", Address: uint32(j), WireType: 13, Name: "p", Value: 1}
		}
		c := probeChannel(p)
		c.ID = fmt.Sprint(i)
		e.project.Channels = append(e.project.Channels, c)
	}
	persisted := cloneProject(e.project)
	for i := range persisted.Channels {
		persisted.Channels[i].Points = nil
	}
	data, _ := json.MarshalIndent(persisted, "", "  ")
	if len(data) > 16<<20 {
		b.Fatal("fixture not saveable", len(data))
	}
	b.Log("saveable project bytes", len(data))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(e.Snapshot())
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestSaveRejectsInvalidStateBeforeReplacingFile(t *testing.T) {
	c := probeChannel([]Point{{Kind: "yc", Address: 1, Value: 0, WireType: 11}})
	e := NewEngine()
	if _, err := e.UpsertChannel(c); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "/valid.json"
	if err := e.Save(path); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	e.project.Channels[0].Devices[0].Points[0].Value = .5
	e.dirty = true
	e.mu.Unlock()
	if err := e.Save(path); err == nil {
		t.Fatal("invalid state was persisted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing file was replaced")
	}
	if err := NewEngine().Load(path); err != nil {
		t.Fatal(err)
	}
}
func TestUncertainAutomaticControlTerminatesWithoutRetrySpin(t *testing.T) {
	c := probeChannel([]Point{{Kind: "yk", Address: 100, Value: 0}})
	c.Role = "client"
	e := NewEngine()
	e.project = Project{Version: 2, Name: "probe", Channels: []Channel{c}}
	s := newSession(e, c)
	e.running[c.ID] = s
	p := &peer{s: s, transfer: true, controlUncertain: true, batchOwner: "review-job"}
	s.peers[p] = true
	j := &autoControlJob{view: AutoControlSnapshot{ID: "review-job", Channel: c.ID, Device: c.Devices[0].ID, Mode: "yk", Status: "running", Options: AutoControlOptions{IntervalMS: 1000, TimeoutMS: 1000, ValueType: 50, Cycle: true, AutoPause: false}, Rows: []AutoControlRow{{Enabled: true, Address: 100, Action: "合闸", Count: 1}}}, wake: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.runAutoControl(j, ctx, s, c.Devices[0], func() {}); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker didn't stop")
	}
	v := j.snapshot()
	if v.Failure != 1 || v.Status != "error" {
		t.Fatal("uncertain result must end the task", v.Failure, v.Status)
	}

}
