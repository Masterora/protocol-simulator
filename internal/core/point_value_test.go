package core

import (
	"math"
	"testing"
)

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
