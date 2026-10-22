package core

import (
	"path/filepath"
	"testing"
)

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
