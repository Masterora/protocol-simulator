package core

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func TestManualPointAtomicSampleAndSinglePointReport(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yc", Address: 10, WireType: 13, Value: 1}, {Kind: "yc", Address: 11, WireType: 13, Value: 2}, {Kind: "yx", Address: 1}})
	view := e.Inspect(InspectRequest{})
	for _, channel := range view.Channels {
		if channel.ID == s.ID && channel.State.ReadyPeers != 1 {
			t.Fatalf("active IEC104 connection missing from inspection: %+v", channel.State)
		}
	}
	device := s.Devices[0].ID
	req := ManualPointRequest{Channel: s.ID, Device: device, Kind: "yc", Address: 10, Action: "set", Metadata: e.Inspect(InspectRequest{}).Metadata}
	value, quality := 42.5, byte(128)
	req.Value, req.Quality = &value, &quality
	e.ClearMessages()
	result, err := e.OperatePoint(context.Background(), req)
	if err != nil || !result.Applied || result.Mode != "reported" || result.Receivers != 1 {
		t.Fatal(result, err)
	}
	eventually(t, func() bool { p := pointAt(t, e, c, "yc", 10); return p.Value == 42.5 && p.Quality == 128 })
	frames := serverASDUs(e, s.ID)
	if len(frames) != 1 || frames[0][0] != 13 || frames[0][6] != 10 || frames[0][13] != 128 || math.Float32frombits(binary.LittleEndian.Uint32(frames[0][9:13])) != 42.5 {
		t.Fatal(frames)
	}
	if pointAt(t, e, s, "yc", 11).Value != 2 {
		t.Fatal("unselected point changed")
	}
	if _, err = e.SetPointValue(s.ID, device, PointKey{"yc", 10}, 55); err != nil {
		t.Fatal(err)
	}
	before := pointAt(t, e, s, "yc", 10)
	e.ClearMessages()
	req.Action = "report"
	req.Value, req.Quality = nil, nil
	result, err = e.OperatePoint(context.Background(), req)
	if err != nil || result.Applied || result.Point.Value != 55 || result.Point.Updated != before.Updated || len(serverASDUs(e, s.ID)) != 1 {
		t.Fatal(result, err)
	}
}

func TestManualQualityPatchKeepsLatestValueAndAutomaticJobRunning(t *testing.T) {
	e, _, s := dataLoopback(t, "modbus-tcp")
	device := s.Devices[0].ID
	saveStartData(t, e, s, AutoDataConfig{Rules: []AutoDataRule{{Enabled: true, Kind: "holding", Points: "0", IntervalMS: 200, Mode: "increment", Min: 1, Max: 1000, Step: 1}}})
	metadata := e.Inspect(InspectRequest{}).Metadata
	eventually(t, func() bool { v, _ := e.GetAutoData(s.ID, device); return v.Cycles[0] > 1 })
	value, quality := 800.0, byte(128)
	request := ManualPointRequest{Channel: s.ID, Device: device, Kind: "holding", Address: 0, Metadata: metadata, Action: "set", Value: &value}
	result, err := e.OperatePoint(context.Background(), request)
	if err != nil || !result.Applied || result.Mode != "polled" {
		t.Fatal(result, err)
	}
	request.Value = nil
	request.Quality = &quality
	result, err = e.OperatePoint(context.Background(), request)
	if err != nil || result.Point.Value < 800 || result.Point.Quality != 128 {
		t.Fatal(result, err)
	}
	before, _ := e.GetAutoData(s.ID, device)
	eventually(t, func() bool {
		v, _ := e.GetAutoData(s.ID, device)
		return v.Status == "running" && v.Cycles[0] > before.Cycles[0]
	})
	if pointAt(t, e, s, "holding", 0).Value <= 800 {
		t.Fatal("automatic generation did not follow manual value")
	}
}

func TestManualPointRejectsStaleInvalidClientAndControlTargets(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yc", Address: 10, WireType: 13, Value: 1}, {Kind: "yk", Address: 100}})
	metadata := e.Inspect(InspectRequest{}).Metadata
	value := math.NaN()
	request := ManualPointRequest{Channel: s.ID, Device: s.Devices[0].ID, Kind: "yc", Address: 10, Metadata: metadata, Action: "set", Value: &value}
	if r, err := e.OperatePoint(context.Background(), request); err == nil || r.Applied {
		t.Fatal(r, err)
	}
	value = 2
	request.Channel = c.ID
	request.Device = c.Devices[0].ID
	if r, err := e.OperatePoint(context.Background(), request); err == nil || r.Applied {
		t.Fatal(r, err)
	}
	request.Channel = s.ID
	request.Device = s.Devices[0].ID
	request.Kind = "yk"
	request.Address = 100
	if r, err := e.OperatePoint(context.Background(), request); err == nil || r.Applied {
		t.Fatal(r, err)
	}
	request.Kind = "yc"
	request.Address = 10
	if err := e.RenameChannel(s.ID, "renamed"); err != nil {
		t.Fatal(err)
	}
	if r, err := e.OperatePoint(context.Background(), request); !errors.Is(err, ErrPointConfigurationChanged) || r.Applied {
		t.Fatal(r, err)
	}
	if pointAt(t, e, s, "yc", 10).Value != 1 {
		t.Fatal("rejected request modified data")
	}
}
