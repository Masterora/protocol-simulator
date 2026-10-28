package core

import (
	"encoding/hex"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func pointAt(t *testing.T, e *Engine, c Channel, kind string, addr uint32) Point {
	t.Helper()
	d, err := e.Device(c.ID, c.Devices[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := findRemotePoint(d.Points, PointKey{kind, addr})
	if !ok {
		t.Fatalf("missing %s:%d", kind, addr)
	}
	return p
}
func serverASDUs(e *Engine, id string) [][]byte {
	out := [][]byte{}
	for _, m := range e.Snapshot().Messages {
		if m.ChannelID != id || m.Direction != "TX" {
			continue
		}
		b, _ := hex.DecodeString(m.Hex)
		if len(b) > 9 && b[2]&1 == 0 {
			out = append(out, b[6:])
		}
	}
	return out
}
func TestFourRemote104IndependentResponsesMappingsAndOrder(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yk", Address: 100}, {Kind: "yt", Address: 101}, {Kind: "yx", Address: 1}, {Kind: "yc", Address: 10, WireType: 13}})
	sid, cid := s.Devices[0].ID, c.Devices[0].ID
	yk := ControlSimulation{Mode: "deny", DelayMS: 60, Mappings: []ControlMapping{{Source: PointKey{"yk", 100}, Target: PointKey{"yx", 1}}}}
	yt := ControlSimulation{Mode: "normal", DelayMS: 60, Mappings: []ControlMapping{{Source: PointKey{"yt", 101}, Target: PointKey{"yc", 10}}}}
	if err := e.SaveControlSimulation(s.ID, sid, "yk", yk); err != nil {
		t.Fatal(err)
	}
	if err := e.SaveControlSimulation(s.ID, sid, "yt", yt); err != nil {
		t.Fatal(err)
	}
	denyStart := time.Now()
	if _, err := e.Command104Device(c.ID, cid, Control104Request{Type: 45, Address: 100, Value: 1, Operation: "select", TimeoutMS: 500}); err == nil {
		t.Fatal("denied YK succeeded")
	}
	if time.Since(denyStart) < 50*time.Millisecond {
		t.Fatal("negative reply ignored configured delay")
	}
	start := time.Now()
	if _, err := e.Command104Device(c.ID, cid, Control104Request{Type: 50, Address: 101, Value: 42.5, Operation: "execute", Direct: true, TimeoutMS: 1000}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Fatal("YT delay ignored")
	}
	eventually(t, func() bool { return pointAt(t, e, c, "yc", 10).Value == 42.5 })
	if pointAt(t, e, s, "yk", 100).Value != 0 || pointAt(t, e, s, "yx", 1).Value != 0 {
		t.Fatal("denied YK mutated points")
	}
	yk.Mode = "normal"
	if err := e.SaveControlSimulation(s.ID, sid, "yk", yk); err != nil {
		t.Fatal(err)
	}
	e.ClearMessages()
	r := Control104Request{Type: 45, Address: 100, Value: 1, Operation: "select", TimeoutMS: 1000}
	if _, err := e.Command104Device(c.ID, cid, r); err != nil {
		t.Fatal(err)
	}
	if pointAt(t, e, s, "yx", 1).Value != 0 {
		t.Fatal("selection applied mapping")
	}
	r.Operation = "cancel"
	if _, err := e.Command104Device(c.ID, cid, r); err != nil {
		t.Fatal(err)
	}
	if pointAt(t, e, s, "yx", 1).Value != 0 {
		t.Fatal("cancel applied mapping")
	}
	e.ClearMessages()
	r.Operation = "execute"
	r.Direct = true
	if _, err := e.Command104Device(c.ID, cid, r); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return pointAt(t, e, c, "yx", 1).Value == 1 })
	// The receiver can process a frame before the sending goroutine records its
	// successful write. Wait for that log boundary before checking wire order.
	eventually(t, func() bool { return len(serverASDUs(e, s.ID)) >= 3 })
	frames := serverASDUs(e, s.ID)
	if len(frames) != 3 || frames[0][0] != 45 || frames[0][2] != 7 || frames[1][2] != 10 || frames[2][0] != 1 || frames[2][2] != 3 {
		t.Fatalf("wrong confirmation/termination/report order: %x", frames)
	}
	got, _ := e.GetControlSimulation(s.ID, sid, "yt")
	if !reflect.DeepEqual(got, yt) {
		t.Fatal("YK changes overwrote YT config")
	}
}
func TestFourRemoteYTStepUsesExistingYTTableAndNormalizedMapping(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yt", Address: 101, Value: 10}, {Kind: "yc", Address: 10, WireType: 9}})
	cfg := ControlSimulation{Mode: "normal", Mappings: []ControlMapping{{Source: PointKey{"yt", 101}, Target: PointKey{"yc", 10}}}}
	if err := e.SaveControlSimulation(s.ID, s.Devices[0].ID, "yt", cfg); err != nil {
		t.Fatal(err)
	}
	// A step command targets the same four-remote YT table; it has no fifth tab.
	if _, err := e.Command104Device(c.ID, c.Devices[0].ID, Control104Request{Type: 47, Address: 101, Value: 2, Operation: "execute", Direct: true, TimeoutMS: 500}); err == nil {
		t.Fatal("invalid normalized feedback value should reject entire command")
	}
	if pointAt(t, e, s, "yt", 101).Value != 10 {
		t.Fatal("invalid mapping partially applied control")
	}
	if _, err := e.Command104Device(c.ID, c.Devices[0].ID, Control104Request{Type: 48, Address: 101, Value: 16384, Operation: "execute", Direct: true, TimeoutMS: 500}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return pointAt(t, e, c, "yc", 10).Value == 0.5 })
	cfg.Mappings = nil
	if err := e.SaveControlSimulation(s.ID, s.Devices[0].ID, "yt", cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Command104Device(c.ID, c.Devices[0].ID, Control104Request{Type: 47, Address: 101, Value: 1, Operation: "execute", Direct: true, TimeoutMS: 500}); err != nil {
		t.Fatal(err)
	}
	if pointAt(t, e, s, "yt", 101).Value != 16383 {
		t.Fatal("step lowering ignored YT point")
	}
}
func TestFourRemoteScansUseCorrectCauseAndKeepStoredData(t *testing.T) {
	for _, tc := range []struct {
		category string
		cause    byte
		typ      byte
	}{{"yc", 1, 36}, {"yc", 2, 35}, {"yx", 2, 30}} {
		t.Run(tc.category+string(rune('0'+tc.cause)), func(t *testing.T) {
			e, c, s := dataLoopback(t, "iec104")
			addr := uint32(10)
			value := 2.5
			if tc.category == "yx" {
				value = 1
			}
			if tc.typ == 35 {
				value = 12
			}
			p := Point{Kind: tc.category, Address: addr, Value: value, WireType: tc.typ, Quality: 16, Timestamp: time.Now().Add(-time.Hour).Format(time.RFC3339Nano)}
			if err := e.SetDevicePoints(s.ID, s.Devices[0].ID, []Point{p}); err != nil {
				t.Fatal(err)
			}
			before, _ := e.Device(s.ID, s.Devices[0].ID)
			e.ClearMessages()
			cfg := AutoDataConfig{StopOnDisconnect: true, Rules: []AutoDataRule{{Enabled: true, Kind: tc.category, Start: addr, End: addr, Mode: "report", Cause: tc.cause, IntervalMS: 200}}}
			if _, err := e.SaveDataTest(s.ID, s.Devices[0].ID, tc.category+"-report", cfg); err != nil {
				t.Fatal(err)
			}
			if _, err := e.StartDataTest(s.ID, s.Devices[0].ID, tc.category+"-report"); err != nil {
				t.Fatal(err)
			}
			eventually(t, func() bool { return len(serverASDUs(e, s.ID)) > 0 })
			a := serverASDUs(e, s.ID)[0]
			if a[2] != tc.cause || timeTagged(a[0]) {
				t.Fatalf("scan cause/type incorrect: %x", a)
			}
			eventually(t, func() bool {
				d, err := e.Device(c.ID, c.Devices[0].ID)
				if err != nil {
					return false
				}
				p, ok := findRemotePoint(d.Points, PointKey{tc.category, addr})
				return ok && p.Value == value
			})
			after, _ := e.Device(s.ID, s.Devices[0].ID)
			if !reflect.DeepEqual(before.Points, after.Points) {
				t.Fatal("scan changed stored data")
			}
			stopped, err := e.SetDataTestState(s.ID, s.Devices[0].ID, tc.category+"-report", "stop")
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(250 * time.Millisecond)
			v, _ := e.GetDataTest(s.ID, s.Devices[0].ID, tc.category+"-report")
			if v.Updates != stopped.Updates {
				t.Fatal("scan continued after stop")
			}
		})
	}
}
func TestFourRemoteRelationsCascadeAllFiveTypesPersistAndRejectCycles(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yx", Address: 1}, {Kind: "yx", Address: 2}, {Kind: "yx", Address: 3}, {Kind: "yc", Address: 10}, {Kind: "yc", Address: 11}, {Kind: "yc", Address: 12}, {Kind: "yc", Address: 13, Value: 5}})
	rows := []TelemetryRelation{
		{Enabled: true, Type: "yx-yx", Source: 1, Min: 1, Max: 1, Targets: []uint32{2}, Value: 1},
		{Enabled: true, Type: "yx-yc", Source: 2, Min: 1, Max: 1, Targets: []uint32{10}, Value: 20},
		{Enabled: true, Type: "yc-yx", Source: 10, Min: 20, Max: 30, Targets: []uint32{3}, Value: 1},
		{Enabled: true, Type: "yc-yc", Source: 10, Min: 20, Max: 30, Targets: []uint32{11}, Value: 30},
		{Enabled: true, Type: "sum", SumSources: []uint32{10, 11, 13}, Targets: []uint32{12}},
	}
	sid := s.Devices[0].ID
	if err := e.SaveTelemetryRelations(s.ID, sid, rows); err != nil {
		t.Fatal(err)
	}
	if _, err := e.SetPointValue(s.ID, sid, PointKey{"yx", 1}, 1); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return pointAt(t, e, c, "yc", 12).Value == 55 && pointAt(t, e, c, "yx", 3).Value == 1 })
	cyclic := append(cloneRelations(rows), TelemetryRelation{Enabled: true, Type: "yx-yx", Source: 2, Min: 1, Max: 1, Targets: []uint32{1}, Value: 0})
	if err := e.SaveTelemetryRelations(s.ID, sid, cyclic); err == nil || !strings.Contains(err.Error(), "循环") {
		t.Fatalf("cycle accepted: %v", err)
	}
	saved, _ := e.GetTelemetryRelations(s.ID, sid)
	if !reflect.DeepEqual(saved, rows) {
		t.Fatal("invalid save replaced valid rules")
	}
	saved[0].Targets[0] = 999
	again, _ := e.GetTelemetryRelations(s.ID, sid)
	if again[0].Targets[0] != 2 {
		t.Fatal("snapshot aliases rules")
	}
	file := filepath.Join(t.TempDir(), "relations.json")
	if err := e.Save(file); err != nil {
		t.Fatal(err)
	}
	other := NewEngine()
	defer other.StopAll()
	if err := other.Load(file); err != nil {
		t.Fatal(err)
	}
	restored, _ := other.GetTelemetryRelations(s.ID, sid)
	if !reflect.DeepEqual(restored, rows) {
		t.Fatal("relations did not roundtrip")
	}
	// Numeric range comparisons include both endpoints, as in the original DLL.
	if _, err := e.SetPointValue(s.ID, sid, PointKey{"yc", 10}, 30); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return pointAt(t, e, c, "yc", 12).Value == 65 })
}
func TestFourRemoteVariationTriggersRelationsAndSequentialYXOrder(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yx", Address: 2}, {Kind: "yx", Address: 1}, {Kind: "yc", Address: 10}, {Kind: "yc", Address: 11}})
	sid := s.Devices[0].ID
	if err := e.SaveTelemetryRelations(s.ID, sid, []TelemetryRelation{{Enabled: true, Type: "yc-yc", Source: 10, Min: 1, Max: 10, Targets: []uint32{11}, Value: 42}}); err != nil {
		t.Fatal(err)
	}
	cfg := AutoDataConfig{StopOnDisconnect: true, Rules: []AutoDataRule{{Enabled: true, Kind: "yc", Start: 10, End: 11, Mode: "fixed", Min: 5, Max: 5, IntervalMS: 200}}}
	if _, err := e.SaveDataTest(s.ID, sid, "yc", cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartDataTest(s.ID, sid, "yc"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return pointAt(t, e, c, "yc", 11).Value == 42 })
	_, _ = e.SetDataTestState(s.ID, sid, "yc", "stop")
	e.ClearMessages()
	cfg.Rules = []AutoDataRule{{Enabled: true, Kind: "yx", Start: 1, End: 2, Mode: "sequence", Min: 0, Max: 1, IntervalMS: 200}}
	if _, err := e.SaveDataTest(s.ID, sid, "yx", cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartDataTest(s.ID, sid, "yx"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return len(serverASDUs(e, s.ID)) >= 3 })
	frames := serverASDUs(e, s.ID)
	for i, want := range []uint32{1, 2, 1} {
		pts, err := Decode104Points(frames[i], 1)
		if err != nil || len(pts) != 1 || pts[0].Address != want {
			t.Fatalf("sequential variation: %x %v", frames, err)
		}
	}
}
func TestFourRemoteModbusMappingCOSBatchAndAutomaticControls(t *testing.T) {
	e, c, s := dataLoopback(t, "modbus-tcp")
	sid, cid := s.Devices[0].ID, c.Devices[0].ID
	points := []Point{{Kind: "coil", Tab: "yk", Address: 100}, {Kind: "discrete", Tab: "yx", Address: 1}, {Kind: "holding", Tab: "yt", Address: 200}, {Kind: "holding", Tab: "yt", Address: 201}, {Kind: "input", Tab: "yc", Address: 10}}
	for _, ch := range []Channel{c, s} {
		if err := e.SetDevicePoints(ch.ID, ch.Devices[0].ID, points); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.SaveControlSimulation(s.ID, sid, "yk", ControlSimulation{Mode: "normal", Mappings: []ControlMapping{{Source: PointKey{"coil", 100}, Target: PointKey{"discrete", 1}}}}); err != nil {
		t.Fatal(err)
	}
	if err := e.SaveControlSimulation(s.ID, sid, "yt", ControlSimulation{Mode: "normal", Mappings: []ControlMapping{{Source: PointKey{"holding", 200}, Target: PointKey{"input", 10}}}}); err != nil {
		t.Fatal(err)
	}
	if err := e.ReadDeviceModbus(c.ID, cid, 2, 1, 1); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		for _, m := range e.Snapshot().Messages {
			if m.ChannelID == c.ID && m.Direction == "RX" {
				b, _ := hex.DecodeString(m.Hex)
				if len(b) > 7 && b[7] == 2 {
					return true
				}
			}
		}
		return false
	})
	if len(e.Snapshot().Events) != 0 {
		t.Fatal("baseline poll created COS")
	}
	if err := e.WriteDeviceModbus(c.ID, cid, 5, 100, 1); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return pointAt(t, e, s, "discrete", 1).Value == 1 })
	if err := e.ReadDeviceModbus(c.ID, cid, 2, 1, 1); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return len(e.Snapshot().Events) == 1 })
	for i := 0; i < 3; i++ {
		if err := e.ReadDeviceModbus(c.ID, cid, 2, 1, 1); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if len(e.Snapshot().Events) != 1 {
		t.Fatal("unchanged polls duplicate COS")
	}
	if err := e.WriteDeviceModbusBatch(c.ID, cid, 16, 200, []uint16{42, 43}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return pointAt(t, e, s, "holding", 201).Value == 43 && pointAt(t, e, s, "input", 10).Value == 42 && pointAt(t, e, c, "holding", 201).Value == 43
	})
	v, err := e.PrepareAutoControl(c.ID, cid, "yk", AutoControlSeed{Function: 5})
	if err != nil || len(v.Rows) != 1 {
		t.Fatalf("Modbus auto prepare: %+v %v", v, err)
	}
	o := v.Options
	o.IntervalMS = 0
	o.TimeoutMS = 500
	o.CheckFeedback = true
	_, _ = e.UpdateAutoOptions(c.ID, cid, "yk", o)
	if _, err = e.ApplyAutoBulk(c.ID, cid, AutoControlBulk{ChangeCount: true, Count: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.StartAutoControl(c.ID, cid, "yk"); err != nil {
		t.Fatal(err)
	}
	v = autoWait(t, e, c, "yk", "completed")
	if v.Total != 4 || v.Success != 4 || v.Failure != 0 {
		t.Fatalf("Modbus auto results: %+v", v)
	}
	if _, err = e.PrepareAutoControl(c.ID, cid, "yt", AutoControlSeed{Addresses: "200-201", Value: "99", ValueType: 49, Function: 6}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.StartAutoControl(c.ID, cid, "yt"); err != nil {
		t.Fatal(err)
	}
	v = autoWait(t, e, c, "yt", "completed")
	if v.Success != 2 || pointAt(t, e, s, "holding", 201).Value != 99 || pointAt(t, e, s, "input", 10).Value != 99 {
		t.Fatalf("Modbus auto YT failed: %+v", v)
	}
}
func TestFourRemoteBatchTelemetryRejectsPartialEditsAndReportsQuality(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yc", Address: 10, WireType: 13, Value: 1}, {Kind: "yc", Address: 11, WireType: 11, Value: 2}, {Kind: "yx", Address: 1}})
	sid := s.Devices[0].ID
	if _, err := e.SetTelemetryRange(s.ID, sid, TelemetryRangeEdit{Category: "yc", Start: 10, End: 11, Mode: "set", Value: 1.5}); err == nil {
		t.Fatal("fraction accepted for scaled point")
	}
	if pointAt(t, e, s, "yc", 10).Value != 1 {
		t.Fatal("invalid range edit partially applied")
	}
	quality := byte(128)
	r, err := e.SetTelemetryRange(s.ID, sid, TelemetryRangeEdit{Category: "yc", Start: 10, End: 11, Mode: "set", Value: 20, Quality: &quality})
	if err != nil || r.Points != 2 || r.Receivers != 1 {
		t.Fatalf("batch edit: %+v %v", r, err)
	}
	eventually(t, func() bool {
		return pointAt(t, e, c, "yc", 11).Value == 20 && pointAt(t, e, c, "yc", 10).Quality == 128
	})
	if _, err = e.SetTelemetryRange(s.ID, sid, TelemetryRangeEdit{Category: "yx", Start: 1, End: 1, Mode: "flip"}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return pointAt(t, e, c, "yx", 1).Value == 1 })
}

func TestFourRemoteUnsigned16BitTelemetrySetting(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yc", Address: 10, WireType: 11}, {Kind: "yc", Address: 11, WireType: 9}})
	if _, err := e.SetTelemetryRange(s.ID, s.Devices[0].ID, TelemetryRangeEdit{Category: "yc", Start: 10, End: 11, Mode: "raw16", Value: 65535}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return pointAt(t, e, c, "yc", 10).Value == -1 && pointAt(t, e, c, "yc", 11).Value == -1.0/32768
	})
	if _, err := e.SetTelemetryRange(s.ID, s.Devices[0].ID, TelemetryRangeEdit{Category: "yc", Start: 10, End: 11, Mode: "raw16", Value: 65536}); err == nil {
		t.Fatal("out of range raw value accepted")
	}
}

func TestFourRemoteControlConfigurationsRoundtripAndLegacyMappingCanBeCleared(t *testing.T) {
	e, _, s := autoFixture(t, []Point{{Kind: "yk", Address: 100, Feedback: "!1"}, {Kind: "yx", Address: 1}, {Kind: "yt", Address: 200}, {Kind: "yc", Address: 10}})
	sid := s.Devices[0].ID
	cfg, err := e.GetControlSimulation(s.ID, sid, "yk")
	if err != nil || len(cfg.Mappings) != 1 || !cfg.Mappings[0].Invert {
		t.Fatalf("legacy mapping not visible: %+v %v", cfg, err)
	}
	cfg.Mode = "silent"
	cfg.DelayMS = 80
	if err = e.SaveControlSimulation(s.ID, sid, "yk", cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Mappings[0].Target.Address = 999
	again, _ := e.GetControlSimulation(s.ID, sid, "yk")
	if again.Mappings[0].Target.Address != 1 {
		t.Fatal("saved mapping aliases caller")
	}
	d, _ := e.Device(s.ID, sid)
	if d.Points[0].Feedback != "" {
		t.Fatal("hidden legacy mapping retained after explicit save")
	}
	if err = e.DeleteDevicePoint(s.ID, sid, PointKey{"yx", 1}); err == nil {
		t.Fatal("deleted a referenced point")
	}
	if _, err = e.EditPointCell(s.ID, sid, PointKey{"yx", 1}, "address", Point{Address: 2}); err == nil {
		t.Fatal("changed a referenced address")
	}
	edit := d
	edit.Name = "edited"
	edit.Configuration = cloneConfiguration(d.Configuration)
	edit.Configuration.Controls = nil
	if _, err = e.UpsertDevice(s.ID, "iec104", edit, false); err != nil {
		t.Fatal(err)
	}
	got, _ := e.GetControlSimulation(s.ID, sid, "yk")
	if !reflect.DeepEqual(got, again) {
		t.Fatal("device edit dropped independent configuration")
	}
	file := filepath.Join(t.TempDir(), "controls.json")
	if err = e.Save(file); err != nil {
		t.Fatal(err)
	}
	other := NewEngine()
	defer other.StopAll()
	if err = other.Load(file); err != nil {
		t.Fatal(err)
	}
	got, _ = other.GetControlSimulation(s.ID, sid, "yk")
	if !reflect.DeepEqual(got, again) {
		t.Fatal("control config did not persist")
	}
	again.Mappings = nil
	if err = e.SaveControlSimulation(s.ID, sid, "yk", again); err != nil {
		t.Fatal(err)
	}
	got, _ = e.GetControlSimulation(s.ID, sid, "yk")
	if len(got.Mappings) != 0 {
		t.Fatal("cleared mapping returned")
	}
	if err = e.DeleteDevicePoint(s.ID, sid, PointKey{"yx", 1}); err != nil {
		t.Fatal(err)
	}
}
func TestFourRemoteModbusDenialIsAnAutomaticFailure(t *testing.T) {
	e, c, s := dataLoopback(t, "modbus-tcp")
	points := []Point{{Kind: "coil", Tab: "yk", Address: 100}}
	for _, ch := range []Channel{c, s} {
		if err := e.SetDevicePoints(ch.ID, ch.Devices[0].ID, points); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.SaveControlSimulation(s.ID, s.Devices[0].ID, "yk", ControlSimulation{Mode: "deny"}); err != nil {
		t.Fatal(err)
	}
	v, err := e.PrepareAutoControl(c.ID, c.Devices[0].ID, "yk", AutoControlSeed{Function: 5})
	if err != nil {
		t.Fatal(err)
	}
	o := v.Options
	o.AutoPause = false
	o.IntervalMS = 0
	_, _ = e.UpdateAutoOptions(c.ID, c.Devices[0].ID, "yk", o)
	if _, err = e.StartAutoControl(c.ID, c.Devices[0].ID, "yk"); err != nil {
		t.Fatal(err)
	}
	v = autoWait(t, e, c, "yk", "completed")
	if v.Success != 0 || v.Failure != 2 || pointAt(t, e, s, "coil", 100).Value != 0 {
		t.Fatalf("negative responses counted as success: %+v", v)
	}
}

func TestFourRemoteDeliveredExampleLoadsWithAllCategoriesAndNoAutomaticStart(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	if err := e.Load(filepath.Join("..", "..", "examples", "automatic-loopback.json")); err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	for _, c := range s.Project.Channels {
		categories := map[string]bool{}
		for _, p := range c.Devices[0].Points {
			categories[controlCategory(dataPointCategory(c.Protocol, p))] = true
		}
		if len(categories) != 4 {
			t.Fatal("example missing a four-remote category")
		}
	}
	if len(s.States) != 0 {
		for _, state := range s.States {
			if state.Status != "stopped" {
				t.Fatal("example started on load")
			}
		}
	}
	if len(e.running) != 0 || len(e.dataJobs) != 0 {
		t.Fatal("example restored an active task")
	}
}

func TestFourRemoteModbusRTUAutomaticAndMultipleRegisterWrites(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	points := []Point{{Kind: "coil", Tab: "yk", Address: 100}, {Kind: "discrete", Tab: "yx", Address: 1}, {Kind: "holding", Tab: "yt", Address: 200}, {Kind: "holding", Tab: "yt", Address: 201}}
	sid, err := e.UpsertChannel(Channel{Name: "RTU server", Protocol: "modbus-rtu", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Unit: 1, CommonAddress: 1, Points: points})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(sid); err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot().Project.Channels[0]
	address := e.Snapshot().States[0].Address
	cid, err := e.UpsertChannel(Channel{Name: "RTU client", Protocol: "modbus-rtu", Role: "client", Transport: "tcp-client", Address: address, Unit: 1, CommonAddress: 1, Points: points})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(cid); err != nil {
		t.Fatal(err)
	}
	waitConnected(t, e, cid)
	c := findChannel(e.Snapshot().Project, "client")
	if err = e.SaveControlSimulation(sid, s.Devices[0].ID, "yk", ControlSimulation{Mode: "normal", Mappings: []ControlMapping{{Source: PointKey{"coil", 100}, Target: PointKey{"discrete", 1}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.PrepareAutoControl(cid, c.Devices[0].ID, "yk", AutoControlSeed{Function: 5}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.StartAutoControl(cid, c.Devices[0].ID, "yk"); err != nil {
		t.Fatal(err)
	}
	v := autoWait(t, e, c, "yk", "completed")
	if v.Success != 2 || v.Failure != 0 {
		t.Fatalf("RTU automation: %+v", v)
	}
	if err = e.WriteDeviceModbusBatch(cid, c.Devices[0].ID, 16, 200, []uint16{321, 654}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return pointAt(t, e, c, "holding", 201).Value == 654 && pointAt(t, e, s, "holding", 200).Value == 321
	})
}

func TestFourRemoteDelayedModbusWritePreservesConcurrentRegisterChanges(t *testing.T) {
	e, c, s := dataLoopback(t, "modbus-tcp")
	sid := s.Devices[0].ID
	points := []Point{{Kind: "holding", Tab: "yt", Address: 200, DataType: "uint32", Value: 0x11112222}, {Kind: "input", Tab: "yc", Address: 10, DataType: "uint32"}}
	for _, ch := range []Channel{c, s} {
		if err := e.SetDevicePoints(ch.ID, ch.Devices[0].ID, points); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.SaveControlSimulation(s.ID, sid, "yt", ControlSimulation{Mode: "normal", DelayMS: 160, Mappings: []ControlMapping{{Source: PointKey{"holding", 200}, Target: PointKey{"input", 10}}}}); err != nil {
		t.Fatal(err)
	}
	e.ClearMessages()
	if err := e.WriteDeviceModbus(c.ID, c.Devices[0].ID, 6, 201, 0x4444); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		for _, m := range e.Snapshot().Messages {
			if m.ChannelID == s.ID && m.Direction == "RX" {
				return true
			}
		}
		return false
	})
	if _, err := e.EditPointCell(s.ID, sid, PointKey{"holding", 200}, "value", Point{Value: 0x33332222}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return pointAt(t, e, s, "holding", 200).Value == 0x33334444 })
	if pointAt(t, e, s, "input", 10).Value != 0x33334444 {
		t.Fatal("mapping used a stale register image")
	}
}
