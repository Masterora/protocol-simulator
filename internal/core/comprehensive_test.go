package core

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestAutomaticDoublePointStatesAndFixedValueBounds(t *testing.T) {
	for _, typ := range []byte{3, 31} {
		for _, mode := range []string{"fixed", "random", "increment"} {
			t.Run(fmt.Sprintf("type%d/%s", typ, mode), func(t *testing.T) {
				e, c, s := dataLoopback(t, "iec104")
				id := s.Devices[0].ID
				if err := e.SetDevicePoints(s.ID, id, []Point{{Kind: "yx", Address: 1, WireType: typ, Value: 1}}); err != nil {
					t.Fatal(err)
				}
				rule := AutoDataRule{Enabled: true, Kind: "yx", Points: "1", Mode: mode, Min: 1, Max: 2, Step: 1, IntervalMS: 200}
				if mode == "fixed" {
					rule.Min = 2
					rule.Max = 0
				} // Upper bound is inactive for fixed values.
				config := AutoDataConfig{Rules: []AutoDataRule{rule}, StopOnDisconnect: true}
				if _, err := e.SaveDataTest(s.ID, id, "telemetry", config); err != nil {
					t.Fatal("valid double-point states rejected:", err)
				}
				if _, err := e.StartDataTest(s.ID, id, "telemetry"); err != nil {
					t.Fatal(err)
				}
				eventually(t, func() bool {
					d, _ := e.Device(c.ID, c.Devices[0].ID)
					p, ok := findRemotePoint(d.Points, PointKey{"yx", 1})
					return ok && p.Value == 2
				})
				if _, err := e.SetDataTestState(s.ID, id, "telemetry", "stop"); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(t.TempDir(), "double-point.json")
				if err := e.Save(path); err != nil {
					t.Fatal(err)
				}
				other := NewEngine()
				defer other.StopAll()
				if err := other.Load(path); err != nil {
					t.Fatal(err)
				}
				view, err := other.GetDataTest(s.ID, id, "telemetry")
				if err != nil || view.Status != "stopped" || !reflect.DeepEqual(view.Config, config) {
					t.Fatal("double-point recipe did not roundtrip", view, err)
				}
			})
		}
	}
	// A hidden upper bound must not reject a legal fixed normalized value.
	config := AutoDataConfig{Rules: []AutoDataRule{{Enabled: true, Kind: "yc", Points: "1", Mode: "fixed", Min: .5, Max: 100, IntervalMS: 200}}}
	if err := validateAutoData("iec104", "server", &config, Point{Kind: "yc", Address: 1, WireType: 9}); err != nil {
		t.Fatal("inactive fixed upper bound rejected:", err)
	}
	for _, p := range []Point{{Kind: "yx", Address: 1, WireType: 1}, {Kind: "discrete", Address: 1}} {
		protocol := "iec104"
		if p.Kind == "discrete" {
			protocol = "modbus-tcp"
		}
		for _, value := range []float64{.5, 2, -1} {
			config.Rules[0] = AutoDataRule{Enabled: true, Kind: p.Kind, Points: "1", Mode: "fixed", Min: value, IntervalMS: 200}
			if err := validateAutoData(protocol, "server", &config, p); err == nil {
				t.Fatalf("invalid single-bit value accepted: %+v %v", p, value)
			}
		}
	}
}

// Exercise live reporting, lagged incremental readers, persistence, socket
// reconnects and stop/start boundaries together, rather than only in isolation.
func TestComprehensiveReportingRecoverySoak(t *testing.T) {
	duration := 2 * time.Second
	if value := os.Getenv("GYLAB_SOAK_DURATION"); value != "" {
		var err error
		duration, err = time.ParseDuration(value)
		if err != nil || duration < time.Second || duration > 10*time.Minute {
			t.Fatal("invalid GYLAB_SOAK_DURATION")
		}
	}
	e, c, s := dataLoopback(t, "iec104")
	id := s.Devices[0].ID
	points := []Point{{Kind: "yx", Address: 1}, {Kind: "yx", Address: 2}, {Kind: "yc", Address: 17014, WireType: 13}, {Kind: "yc", Address: 17015, WireType: 11}, {Kind: "yc", Address: 99, Value: 77, WireType: 13}}
	if err := e.SetDevicePoints(s.ID, id, points); err != nil {
		t.Fatal(err)
	}
	config := AutoDataConfig{StopOnDisconnect: false, Rules: []AutoDataRule{
		{Enabled: true, Kind: "yx", Points: "1，2", Mode: "toggle", Max: 1, IntervalMS: 200},
		{Enabled: true, Kind: "yc", Points: "17014-17015", Mode: "increment", Min: 10, Max: 20, Step: 1, IntervalMS: 200},
		{Enabled: true, Kind: "yc", Points: "17014，17015", Mode: "report", Cause: 2, IntervalMS: 200},
	}}
	if _, err := e.SaveDataTest(s.ID, id, "telemetry", config); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartDataTest(s.ID, id, "telemetry"); err != nil {
		t.Fatal(err)
	}
	view := e.View(ViewRequest{Channel: s.ID, Device: id})
	model := map[PointKey]Point{}
	for _, p := range view.Points {
		model[PointKey{p.Kind, p.Address}] = p
	}
	cycles, reconnects := 0, 0
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		time.Sleep(225 * time.Millisecond)
		view = e.View(viewRequest(view))
		if view.FullPoints {
			clear(model)
		}
		for _, p := range view.Points {
			model[PointKey{p.Kind, p.Address}] = p
		}
		// Freeze this task before comparing independent API reads.
		if _, err := e.SetDataTestState(s.ID, id, "telemetry", "pause"); err != nil {
			v, _ := e.GetDataTest(s.ID, id, "telemetry")
			t.Fatal(err, v)
		}
		view = e.View(viewRequest(view))
		for _, p := range view.Points {
			model[PointKey{p.Kind, p.Address}] = p
		}
		d, _ := e.Device(s.ID, id)
		for _, p := range d.Points {
			if !reflect.DeepEqual(model[PointKey{p.Kind, p.Address}], p) {
				t.Fatal("incremental view lost live data", p, model)
			}
		}
		if d.Points[4].Value != 77 {
			t.Fatal("unselected point changed")
		}
		path := filepath.Join(t.TempDir(), "soak.json")
		if err := e.Save(path); err != nil {
			t.Fatal(err)
		}
		other := NewEngine()
		if err := other.Load(path); err != nil {
			t.Fatal(err)
		}
		loaded, _ := other.GetDataTest(s.ID, id, "telemetry")
		if loaded.Status != "stopped" || !reflect.DeepEqual(loaded.Config, config) {
			t.Fatal("runtime resumed or recipe changed on load")
		}
		if _, err := e.SetDataTestState(s.ID, id, "telemetry", "resume"); err != nil {
			t.Fatal(err)
		}
		cycles++
		if cycles%4 == 0 {
			e.Stop(c.ID)
			if err := e.Start(c.ID); err != nil {
				t.Fatal(err)
			}
			eventually(t, func() bool {
				for _, state := range e.Snapshot().States {
					if state.ID == c.ID {
						return state.ReadyPeers > 0
					}
				}
				return false
			})
			reconnects++
		}
	}
	stopped, err := e.SetDataTestState(s.ID, id, "telemetry", "stop")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Updates == 0 {
		t.Fatal("no automatic cycles ran")
	}
	// A final online value is causally checked on the master after the churn.
	if _, err := e.SetPointValue(s.ID, id, PointKey{"yc", 17014}, 321.5); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return pointAt(t, e, c, "yc", 17014).Value == 321.5 })
	time.Sleep(250 * time.Millisecond)
	after, _ := e.GetDataTest(s.ID, id, "telemetry")
	if after.Status != "stopped" || after.Updates != stopped.Updates || after.LastError != "" {
		t.Fatal("stopped task advanced or acquired spurious error", after)
	}
	for _, m := range e.Snapshot().Messages {
		if m.Direction == "ERR" {
			t.Fatal("unexpected protocol error during recovery", m.Summary)
		}
	}
	t.Logf("soak duration=%s checkpoints=%d reconnects=%d updates=%d; live views/save-load/final feedback verified", duration, cycles, reconnects, after.Updates)
}

func TestSameCycleVariationAndReportsUseFinalValues(t *testing.T) {
	for _, reportFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("reportFirst=%v", reportFirst), func(t *testing.T) {
			e, c, s := dataLoopback(t, "iec104")
			id := s.Devices[0].ID
			if err := e.SetDevicePoints(s.ID, id, []Point{{Kind: "yc", Address: 10, Value: 1, WireType: 13}}); err != nil {
				t.Fatal(err)
			}
			rules := []AutoDataRule{
				{Enabled: true, Kind: "yc", Points: "10", Mode: "fixed", Min: 12.5, Max: 12.5, IntervalMS: 200},
				{Enabled: true, Kind: "yc", Points: "10", Mode: "report", Cause: 2, IntervalMS: 200},
			}
			if reportFirst {
				rules[0], rules[1] = rules[1], rules[0]
			}
			if _, err := e.SaveDataTest(s.ID, id, "telemetry", AutoDataConfig{Rules: rules, StopOnDisconnect: true}); err != nil {
				t.Fatal(err)
			}
			e.ClearMessages()
			if _, err := e.StartDataTest(s.ID, id, "telemetry"); err != nil {
				t.Fatal(err)
			}
			eventually(t, func() bool {
				v, _ := e.GetDataTest(s.ID, id, "telemetry")
				if v.Status != "running" {
					t.Fatal("overlapping variation/report stopped task", v.LastError)
				}
				return v.Cycles[0] >= 3 && v.Cycles[1] >= 3
			})
			if _, err := e.SetDataTestState(s.ID, id, "telemetry", "stop"); err != nil {
				t.Fatal(err)
			}
			eventually(t, func() bool {
				d, _ := e.Device(c.ID, c.Devices[0].ID)
				p, ok := findRemotePoint(d.Points, PointKey{"yc", 10})
				return ok && p.Value == 12.5
			})
			causes := map[byte]int{}
			for _, a := range serverASDUs(e, s.ID) {
				if a[0] != 13 {
					continue
				}
				points, err := Decode104Points(a, 1)
				if err != nil || len(points) != 1 || points[0].Value != 12.5 {
					t.Fatalf("stale same-cycle report %x: %v", a, err)
				}
				causes[a[2]]++
			}
			if causes[2] < 3 || causes[3] < 3 {
				t.Fatal("lost scheduled or change reports", causes)
			}
		})
	}
}

func FuzzProtocolDecoders(f *testing.F) {
	for _, b := range [][]byte{nil, {0x68, 4, 7, 0, 0, 0}, {1, 3, 0, 0, 0, 10, 0xc5, 0xcd}, PointASDU(Point{Kind: "yc", Address: 17014, Value: 12.5}, 1, 3), {3, 2, 0, 10}} {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 4096 {
			return
		}
		_, _ = Read104(bytes.NewReader(b))
		_, _ = ReadModbusTCP(bytes.NewReader(b))
		_, _ = ReadModbusRTU(bytes.NewReader(b), true)
		_, _ = ReadModbusRTU(bytes.NewReader(b), false)
		points, err := Decode104Points(b, 1)
		if err == nil {
			for _, p := range points {
				if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) || p.Address > 0xffffff {
					t.Fatal("invalid decoded point", p)
				}
			}
		}
		_, _ = decodeControl104(b)
		_, _ = ModbusReply(b, []Point{{Kind: "holding", Address: 0}, {Kind: "coil", Address: 0}})
		_, _ = DecodeModbusResponse([]byte{3, 0, 0, 0, 1}, b)
	})
}
