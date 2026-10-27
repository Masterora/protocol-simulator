package core

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestPointCellClientEditsStayLocalAndPreserveLiveMetadata(t *testing.T) {
	e, client, server := dataLoopback(t, "iec104")
	if err := e.SetDevicePoints(client.ID, client.Devices[0].ID, []Point{{Kind: "yc", Address: 10, Value: 1, Quality: 0x80, Name: "before"}}); err != nil {
		t.Fatal(err)
	}
	e.ClearMessages()
	for _, edit := range []struct {
		field string
		point Point
	}{{"value", Point{Value: 12.5}}, {"name", Point{Name: "after"}}, {"quality", Point{Quality: 0}}} {
		r, err := e.EditPointCell(client.ID, client.Devices[0].ID, PointKey{Kind: "yc", Address: 10}, edit.field, edit.point)
		if err != nil || r.Mode != "stored" {
			t.Fatalf("%+v %v", r, err)
		}
	}
	if len(e.Snapshot().Messages) != 0 {
		t.Fatal("client local edit sent a frame")
	}
	d, _ := e.Device(client.ID, client.Devices[0].ID)
	if d.Points[0].Value != 12.5 || d.Points[0].Name != "after" || d.Points[0].Quality != 0 {
		t.Fatal(d.Points)
	}
	remote, _ := e.Device(server.ID, server.Devices[0].ID)
	for _, p := range remote.Points {
		if p.Kind == "yc" && p.Address == 10 && p.Value == 12.5 {
			t.Fatal("remote modified")
		}
	}
}

func TestPointCellQualitySendsOnlyChangedServerPoint(t *testing.T) {
	e, client, server := dataLoopback(t, "iec104")
	if err := e.SetDevicePoints(server.ID, server.Devices[0].ID, []Point{{Kind: "yx", Address: 1, Value: 1, Name: "breaker"}, {Kind: "yx", Address: 2}}); err != nil {
		t.Fatal(err)
	}
	r, err := e.EditPointCell(server.ID, server.Devices[0].ID, PointKey{Kind: "yx", Address: 1}, "quality", Point{Quality: 0x80})
	if err != nil || r.Mode != "reported" {
		t.Fatalf("%+v %v", r, err)
	}
	eventually(t, func() bool {
		d, _ := e.Device(client.ID, client.Devices[0].ID)
		return len(d.Points) == 1 && d.Points[0].Address == 1 && d.Points[0].Value == 1 && d.Points[0].Quality == 0x80
	})
	d, _ := e.Device(server.ID, server.Devices[0].ID)
	if d.Points[0].Name != "breaker" {
		t.Fatal("description overwritten")
	}
}

func TestPointCellAddressValidationAndTabSurviveSave(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "table", Protocol: "raw", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	device, err := e.UpsertDevice(id, "modbus-tcp", Device{Name: "d", Unit: 1, CommonAddress: 1, Configuration: &DeviceConfiguration{Version: "TCP版", Ranges: []PointRange{{Tab: "yc", Kind: "holding", Address: 1, Count: 2, Name: "YC"}}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	key := PointKey{Kind: "holding", Address: 1}
	if _, err = e.EditPointCell(id, device, key, "address", Point{Address: 2}); err == nil {
		t.Fatal("duplicate address accepted")
	}
	if _, err = e.EditPointCell(id, device, key, "address", Point{Address: 65536}); err == nil {
		t.Fatal("out of range address accepted")
	}
	if _, err = e.EditPointCell(id, device, key, "kind", Point{}); err == nil {
		t.Fatal("unsupported field accepted")
	}
	if _, err = e.EditPointCell(id, device, key, "address", Point{Address: 1000}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "table.json")
	if err = e.Save(file); err != nil {
		t.Fatal(err)
	}
	restored := NewEngine()
	if err = restored.Load(file); err != nil {
		t.Fatal(err)
	}
	d, _ := restored.Device(id, device)
	if d.Points[0].Address != 1000 || d.Points[0].Tab != "yc" || d.Points[1].Address != 2 {
		t.Fatal(d.Points)
	}
	if _, err = restored.EditPointCell(id, device, key, "value", Point{Value: 50}); err == nil {
		t.Fatal("stale key recreated an old address")
	}
}

func TestInline104ValueReportsOnlyEditedPointAndPreservesMetadata(t *testing.T) {
	e, client, server := dataLoopback(t, "iec104")
	points := []Point{{Kind: "yc", Address: 10, Value: 2, Quality: 0x80, Name: "voltage"}, {Kind: "yc", Address: 11, Value: 99}, {Kind: "yx", Address: 1}}
	if err := e.SetDevicePoints(server.ID, server.Devices[0].ID, points); err != nil {
		t.Fatal(err)
	}
	result, err := e.SetPointValue(server.ID, server.Devices[0].ID, PointKey{Kind: "yc", Address: 10}, 12.5)
	if err != nil || result.Mode != "reported" || result.Receivers != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	eventually(t, func() bool {
		d, _ := e.Device(client.ID, client.Devices[0].ID)
		return len(d.Points) == 1 && d.Points[0].Address == 10 && d.Points[0].Value == 12.5 && d.Points[0].Quality == 0x80
	})
	d, _ := e.Device(server.ID, server.Devices[0].ID)
	if d.Points[0].Name != "voltage" || d.Points[0].Quality != 0x80 || d.Points[1].Value != 99 {
		t.Fatal("unrelated data overwritten")
	}
	if _, err = e.SetPointValue(server.ID, server.Devices[0].ID, PointKey{Kind: "yx", Address: 1}, 1); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		d, _ := e.Device(client.ID, client.Devices[0].ID)
		for _, p := range d.Points {
			if p.Kind == "yx" && p.Address == 1 && p.Value == 1 {
				return len(d.Points) == 2
			}
		}
		return false
	})
}

func TestInlineValueOfflineValidationAndDeletedTargets(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "offline", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Points: []Point{{Kind: "yc", Address: 1, Value: 5}, {Kind: "yx", Address: 2}}})
	if err != nil {
		t.Fatal(err)
	}
	device := e.Snapshot().Project.Channels[0].Devices[0].ID
	result, err := e.SetPointValue(id, device, PointKey{Kind: "yc", Address: 1}, 12)
	if err != nil || result.Mode != "offline" || len(e.Snapshot().Messages) != 0 {
		t.Fatalf("%+v %v", result, err)
	}
	for _, value := range []float64{math.NaN(), math.Inf(1), math.MaxFloat64} {
		if _, err = e.SetPointValue(id, device, PointKey{Kind: "yc", Address: 1}, value); err == nil {
			t.Fatal("invalid analog accepted")
		}
	}
	if _, err = e.SetPointValue(id, device, PointKey{Kind: "yx", Address: 2}, 2); err == nil {
		t.Fatal("invalid bit accepted")
	}
	d, _ := e.Device(id, device)
	if d.Points[0].Value != 12 || d.Points[1].Value != 0 {
		t.Fatal("validation changed data")
	}
	if _, err = e.SetPointValue(id, "deleted", PointKey{Kind: "yc", Address: 1}, 88); err == nil {
		t.Fatal("missing device accepted")
	}
	if err = e.DeleteDevicePoint(id, device, PointKey{Kind: "yc", Address: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = e.SetPointValue(id, device, PointKey{Kind: "yc", Address: 1}, 88); err == nil {
		t.Fatal("deleted point recreated")
	}
	client, err := e.UpsertChannel(Channel{Name: "client", Protocol: "iec104", Role: "client", Transport: "tcp-client", Address: "127.0.0.1:2404", Points: []Point{{Kind: "yx", Address: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.SetPointValue(client, "", PointKey{Kind: "yx", Address: 1}, 1); err == nil {
		t.Fatal("client transmitted simulated monitoring data")
	}
}

func TestInlineModbusValueIsReturnedOnNextPollWithoutUnsolicitedFrame(t *testing.T) {
	e, server, client := multiPair(t, "modbus-tcp")
	e.ClearMessages()
	result, err := e.SetPointValue(server, "two", PointKey{Kind: "holding", Address: 0}, 1234)
	if err != nil || result.Mode != "polled" {
		t.Fatalf("%+v %v", result, err)
	}
	if len(e.Snapshot().Messages) != 0 {
		t.Fatal("Modbus emitted an unsolicited frame")
	}
	if err = e.ReadDeviceModbus(client, "two", 3, 0, 1); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return deviceValue(e, client, "two") == 1234 })
	if deviceValue(e, server, "one") != 111 {
		t.Fatal("updated another device")
	}
}

func TestPointSelectionGrammarAndLimits(t *testing.T) {
	for _, text := range []string{"1，3-5，8", "8, 3 - 5，1,4", "001，3-5，8"} {
		got, err := ParsePointSelection(text, 65535)
		if err != nil || !reflect.DeepEqual(got, []uint32{1, 3, 4, 5, 8}) {
			t.Fatalf("%q: %v %v", text, got, err)
		}
	}
	for _, text := range []string{"", "1，", "1,,2", "-1", "5-3", "1-2-3", "0x10", "10H", "1.5", "1e2", "65536", "0-10000"} {
		if _, err := ParsePointSelection(text, 65535); err == nil {
			t.Fatalf("accepted invalid selection %q", text)
		}
	}
	for _, text := range []string{"0-9999", "65535", "4294967295"} {
		if _, err := ParsePointSelection(text, ^uint32(0)); err != nil {
			t.Fatal(err)
		}
	}
	// Old projects used a broad interval to select the existing sparse points.
	if _, err := selectionSpans("", 0, 0xffffff, 0xffffff); err != nil {
		t.Fatalf("legacy interval rejected: %v", err)
	}
}

func TestMixedPointDeviceConfigurationPersistsAndRetainsPointData(t *testing.T) {
	e := NewEngine()
	t.Cleanup(e.StopAll)
	id, err := e.UpsertChannel(Channel{Name: "points", Protocol: "raw", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	config := &DeviceConfiguration{Version: "标准版", Values: map[string]string{}, Ranges: []PointRange{
		{Tab: "yx", Kind: "yx", Address: 1, Count: 5, Selection: "1，3-5，8", Name: "遥信"},
		{Tab: "yc", Kind: "yc", Address: 100, Count: 2, Selection: "100，103", Name: "遥测"},
		{Tab: "yk", Kind: "yk", Address: 200, Count: 1, Selection: "200", Name: "遥控"},
		{Tab: "yt", Kind: "yt", Address: 300, Count: 1, Selection: "300", Name: "遥调"},
	}}
	did, err := e.UpsertDevice(id, "iec104", Device{Name: "device", Unit: 1, CommonAddress: 1, Configuration: config}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.EditPointCell(id, did, PointKey{"yx", 1}, "value", Point{Value: 1}); err != nil {
		t.Fatal(err)
	}
	d, _ := e.Device(id, did)
	if len(d.Points) != 9 {
		t.Fatalf("created extra points: %+v", d.Points)
	}
	d.Configuration.Ranges[0].Selection = "1，3-5，9"
	if _, err = e.UpsertDevice(id, "iec104", d, false); err != nil {
		t.Fatal(err)
	}
	d, _ = e.Device(id, did)
	if p, ok := findRemotePoint(d.Points, PointKey{"yx", 1}); !ok || p.Value != 1 {
		t.Fatal("retained point data was lost")
	}
	if _, ok := findRemotePoint(d.Points, PointKey{"yx", 8}); ok {
		t.Fatal("removed point remains")
	}
	if _, ok := findRemotePoint(d.Points, PointKey{"yx", 9}); !ok {
		t.Fatal("new point was not created")
	}
	path := filepath.Join(t.TempDir(), "mixed.json")
	if err = e.Save(path); err != nil {
		t.Fatal(err)
	}
	restored := NewEngine()
	t.Cleanup(restored.StopAll)
	if err = restored.Load(path); err != nil {
		t.Fatal(err)
	}
	got, _ := restored.Device(id, did)
	if !reflect.DeepEqual(got.Points, d.Points) || got.Configuration.Ranges[0].Selection != "1，3-5，9" {
		t.Fatal("mixed point configuration did not survive project reload")
	}
	// Equal minimum and count are insufficient when migrating an imported table.
	if rangesDescribeExisting(config.Ranges[:1], []Point{{Kind: "yx", Address: 1}, {Kind: "yx", Address: 3}, {Kind: "yx", Address: 4}, {Kind: "yx", Address: 5}, {Kind: "yx", Address: 7}}) {
		t.Fatal("different sparse point set treated as unchanged")
	}
}

func TestMixedPointRegisterWidthsAndPolling(t *testing.T) {
	c := &DeviceConfiguration{Version: "TCP版", Values: map[string]string{"cb_ycFormat": "4", "clockEnabled": "true", "TimeAddr": "106"}, Ranges: []PointRange{
		{Tab: "yc", Kind: "holding", Address: 100, Count: 3, Selection: "100，102，110", Name: "遥测"},
		{Tab: "yt", Kind: "holding", Address: 104, Count: 1, Selection: "104", Name: "遥调"},
	}}
	points, err := configuredPoints("modbus-tcp", c, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := modbusPollTasks(Device{Configuration: c, Points: points})
	want := [][]byte{{3, 0, 100, 0, 4}, {3, 0, 110, 0, 2}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("poll read unselected gaps or split a value: %x", got)
	}
	c.Ranges[1].Address, c.Ranges[1].Selection = 103, "103"
	if err = validateConfiguration("modbus-tcp", c); err == nil {
		t.Fatal("overlap with second register accepted")
	}
	c.Ranges = c.Ranges[:1]
	c.Ranges[0].Count, c.Ranges[0].Selection = 3, "100-102"
	if err = validateConfiguration("modbus-tcp", c); err == nil {
		t.Fatal("overlapping float starts accepted")
	}
	c.Ranges[0].Count, c.Ranges[0].Selection = 2, ""
	legacy, err := configuredPoints("modbus-tcp", c, nil)
	if err != nil || len(legacy) != 2 || legacy[1].Address != 102 {
		t.Fatalf("legacy width expansion changed: %+v %v", legacy, err)
	}
}

func TestMixedPointBatchChangesAndTimedReportsExcludeGaps(t *testing.T) {
	e, _, s := dataLoopback(t, "iec104")
	did := s.Devices[0].ID
	points := []Point{}
	for n := uint32(1); n <= 8; n++ {
		points = append(points, Point{Kind: "yx", Address: n, Value: 1})
	}
	if err := e.SetDevicePoints(s.ID, did, points); err != nil {
		t.Fatal(err)
	}
	q := byte(16)
	result, err := e.SetTelemetryRange(s.ID, did, TelemetryRangeEdit{Category: "yx", Points: "1，3-5，8", Mode: "flip", Quality: &q})
	if err != nil || result.Points != 5 {
		t.Fatalf("mixed batch failed: %+v %v", result, err)
	}
	for n := uint32(1); n <= 8; n++ {
		p := pointAt(t, e, s, "yx", n)
		selected := n == 1 || n >= 3 && n <= 5 || n == 8
		if selected && (p.Value != 0 || p.Quality != 16) || !selected && (p.Value != 1 || p.Quality != 0) {
			t.Fatalf("batch changed wrong point: %+v", p)
		}
	}
	config := AutoDataConfig{Rules: []AutoDataRule{
		{Enabled: true, Kind: "yx", Points: "1，8", Mode: "fixed", IntervalMS: 200, Min: 1, Max: 1, Step: 1},
		{Enabled: true, Kind: "yx", Points: "3-5", Mode: "fixed", IntervalMS: 200, Min: 0, Max: 0, Step: 1},
	}}
	if _, err = e.SaveDataTest(s.ID, did, "yx", config); err != nil {
		t.Fatalf("disjoint interleaved sets rejected: %v", err)
	}
	if _, err = e.StartDataTest(s.ID, did, "yx"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { v, _ := e.GetDataTest(s.ID, did, "yx"); return v.Updates >= 5 })
	if _, err = e.SetDataTestState(s.ID, did, "yx", "stop"); err != nil {
		t.Fatal(err)
	}
	if pointAt(t, e, s, "yx", 2).Value != 1 || pointAt(t, e, s, "yx", 6).Value != 1 {
		t.Fatal("automatic changes included the gaps")
	}
	config.Rules[1].Points = "3-5，8"
	if _, err = e.SaveDataTest(s.ID, did, "yx", config); err == nil {
		t.Fatal("overlapping mixed sets accepted")
	}
	e.ClearMessages()
	report := AutoDataConfig{Rules: []AutoDataRule{{Enabled: true, Kind: "yx", Points: "1，3-5，8", Mode: "report", IntervalMS: 200}}}
	if _, err = e.SaveDataTest(s.ID, did, "yx-report", report); err != nil {
		t.Fatal(err)
	}
	if _, err = e.StartDataTest(s.ID, did, "yx-report"); err != nil {
		t.Fatal(err)
	}
	reported := func() map[uint32]bool {
		seen := map[uint32]bool{}
		for _, m := range e.Snapshot().Messages {
			if m.ChannelID != s.ID || m.Direction != "TX" || !strings.Contains(m.Summary, "定时上报") {
				continue
			}
			b, _ := hex.DecodeString(m.Hex)
			decoded, err := Decode104Points(b[6:], 1)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range decoded {
				seen[p.Address] = true
			}
		}
		return seen
	}
	eventually(t, func() bool { return len(reported()) >= 5 })
	seen := reported()
	if !reflect.DeepEqual(seen, map[uint32]bool{1: true, 3: true, 4: true, 5: true, 8: true}) {
		t.Fatalf("reports included unselected points: %v", seen)
	}
}

func pointRequestPDUs(e *Engine, channel, protocol string) [][]byte {
	out := [][]byte{}
	for _, m := range e.Snapshot().Messages {
		if m.ChannelID != channel || m.Direction != "TX" || !strings.Contains(m.Summary, "点位操作") {
			continue
		}
		b, _ := hex.DecodeString(m.Hex)
		if protocol == "modbus-rtu" {
			out = append(out, b[1:len(b)-2])
		} else {
			out = append(out, b[7:])
		}
	}
	return out
}

func TestModbusMixedPointReadWriteRoundTrip(t *testing.T) {
	for _, protocol := range []string{"modbus-tcp", "modbus-rtu"} {
		t.Run(protocol, func(t *testing.T) {
			e, server, client := multiPair(t, protocol)
			points := []Point{}
			for n := uint32(0); n < 260; n++ {
				points = append(points, Point{Kind: "holding", Address: n, Value: 7})
			}
			if err := e.SetDevicePoints(server, "one", points); err != nil {
				t.Fatal(err)
			}
			result, err := e.WriteDeviceModbusPoints(client, "one", 16, "1，3-5，8", 42)
			if err != nil || result.Points != 5 || result.Requests != 3 {
				t.Fatalf("mixed writes: %+v %v", result, err)
			}
			d, _ := e.Device(server, "one")
			for _, p := range d.Points {
				selected := p.Address == 1 || p.Address >= 3 && p.Address <= 5 || p.Address == 8
				if selected && p.Value != 42 || !selected && p.Value != 7 {
					t.Fatalf("write crossed a gap: %+v", p)
				}
			}
			e.ClearMessages()
			result, err = e.ReadDeviceModbusPoints(client, "one", 3, "8，3-5，1")
			if err != nil || result.Points != 5 || result.Requests != 3 {
				t.Fatalf("mixed reads: %+v %v", result, err)
			}
			pdus := pointRequestPDUs(e, client, protocol)
			if !reflect.DeepEqual(pdus, [][]byte{{3, 0, 1, 0, 1}, {3, 0, 3, 0, 3}, {3, 0, 8, 0, 1}}) {
				t.Fatalf("wrong wire ranges: %x", pdus)
			}
			if deviceValue(e, server, "two") != 222 {
				t.Fatal("request modified another unit")
			}
			result, err = e.WriteDeviceModbusPoints(client, "one", 16, "100-249", 99)
			if err != nil || result.Requests != 2 || result.Points != 150 {
				t.Fatalf("large write was not split at FC16 limit: %+v %v", result, err)
			}
			before := len(e.Snapshot().Messages)
			if _, err = e.WriteDeviceModbusPoints(client, "one", 16, "1，3-", 2); err == nil {
				t.Fatal("invalid selection sent")
			}
			if _, err = e.WriteDeviceModbusPoints(client, "one", 15, "1，3-5", 2); err == nil {
				t.Fatal("invalid coil value sent")
			}
			if len(e.Snapshot().Messages) != before {
				t.Fatal("invalid batch sent a partial request")
			}
			d, _ = e.Device(server, "one")
			for n := uint32(1); n <= 8; n++ {
				d.Points = append(d.Points, Point{Kind: "coil", Address: n})
			}
			if err = e.SetDevicePoints(server, "one", d.Points); err != nil {
				t.Fatal(err)
			}
			result, err = e.WriteDeviceModbusPoints(client, "one", 15, "1，3-5，8", 1)
			if err != nil || result.Requests != 3 || result.Points != 5 {
				t.Fatalf("mixed coil writes: %+v %v", result, err)
			}
			if _, err = e.ReadDeviceModbusPoints(client, "one", 1, "1，3-5，8"); err != nil {
				t.Fatal(err)
			}
			d, _ = e.Device(server, "one")
			for _, p := range d.Points {
				if p.Kind == "coil" {
					selected := p.Address == 1 || p.Address >= 3 && p.Address <= 5 || p.Address == 8
					if selected && p.Value != 1 || !selected && p.Value != 0 {
						t.Fatalf("coil write crossed a gap: %+v", p)
					}
				}
			}
		})
	}
}

func TestModbusMixedPointReadsKeep32BitValuesIntactAcrossChunks(t *testing.T) {
	for _, protocol := range []string{"modbus-tcp", "modbus-rtu"} {
		t.Run(protocol, func(t *testing.T) {
			e, server, client := multiPair(t, protocol)
			points, selections := []Point{}, []string{}
			for n := uint32(0); n < 80; n++ {
				points = append(points, Point{Kind: "holding", Tab: "yc", Address: 100 + n*2, Value: float64(n) + 0.5, DataType: "float32"})
				selections = append(selections, fmt.Sprint(100+n*2))
			}
			points = append(points, Point{Kind: "holding", Tab: "yc", Address: 400, Value: 123.5, DataType: "float32"})
			selections = append(selections, "400")
			if err := e.SetDevicePoints(server, "one", points); err != nil {
				t.Fatal(err)
			}
			zeros := append([]Point{}, points...)
			for i := range zeros {
				zeros[i].Value = 0
			}
			if err := e.SetDevicePoints(client, "one", zeros); err != nil {
				t.Fatal(err)
			}
			e.ClearMessages()
			result, err := e.ReadDeviceModbusPoints(client, "one", 3, strings.Join(selections, "，"))
			if err != nil || result.Points != 81 || result.Requests != 3 {
				t.Fatalf("wide read: %+v %v", result, err)
			}
			pdus := pointRequestPDUs(e, client, protocol)
			if len(pdus) != 3 || binary.BigEndian.Uint16(pdus[0][3:]) != 124 || binary.BigEndian.Uint16(pdus[1][3:]) != 36 || binary.BigEndian.Uint16(pdus[2][1:]) != 400 || binary.BigEndian.Uint16(pdus[2][3:]) != 2 {
				t.Fatalf("read split a 32-bit value or included sparse gaps: %x", pdus)
			}
			d, _ := e.Device(client, "one")
			for _, expected := range points {
				got, ok := findRemotePoint(d.Points, PointKey{expected.Kind, expected.Address})
				if !ok || got.Value != expected.Value {
					t.Fatalf("wide value corrupted: %+v expected %+v", got, expected)
				}
			}
		})
	}
}
