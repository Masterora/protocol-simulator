package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func twoDevices(protocol string, populated bool) []Device {
	devices := []Device{{ID: "one", Name: "装置一", Unit: 1, CommonAddress: 1, Points: []Point{}}, {ID: "two", Name: "装置二", Unit: 2, CommonAddress: 2, Points: []Point{}}}
	if populated {
		for i := range devices {
			kind := "holding"
			if protocol == "iec104" {
				kind = "yc"
			}
			devices[i].Points = []Point{{Address: 0, Kind: kind, Name: "same address", Value: float64(111 * (i + 1))}}
		}
	}
	return devices
}
func multiPair(t *testing.T, protocol string) (*Engine, string, string) {
	t.Helper()
	e := NewEngine()
	t.Cleanup(e.StopAll)
	server, err := e.UpsertChannel(Channel{Name: "server", Protocol: protocol, Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: twoDevices(protocol, true)})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(server); err != nil {
		t.Fatal(err)
	}
	client, err := e.UpsertChannel(Channel{Name: "client", Protocol: protocol, Role: "client", Transport: "tcp-client", Address: e.Snapshot().States[0].Address, Devices: twoDevices(protocol, false)})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(client); err != nil {
		t.Fatal(err)
	}
	waitConnected(t, e, client)
	return e, server, client
}
func deviceValue(e *Engine, c, d string) float64 {
	v, err := e.Device(c, d)
	if err != nil || len(v.Points) == 0 {
		return -1
	}
	return v.Points[0].Value
}

func TestMultiDeviceModbusSharedConnection(t *testing.T) {
	e, server, client := multiPair(t, "modbus-tcp")
	for _, id := range []string{"one", "two"} {
		if err := e.ReadDeviceModbus(client, id, 3, 0, 1); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, func() bool { return deviceValue(e, client, "one") == 111 && deviceValue(e, client, "two") == 222 })
	if err := e.WriteDeviceModbus(client, "two", 6, 0, 333); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return deviceValue(e, server, "two") == 333 })
	if deviceValue(e, server, "one") != 111 {
		t.Fatal("write leaked into another device")
	}
	for _, s := range e.Snapshot().States {
		if s.Peers != 1 {
			t.Fatalf("devices must share one connection: %+v", s)
		}
	}
	if err := e.DeleteDevice(client, "one"); err != nil {
		t.Fatal(err)
	}
	if e.ReadDeviceModbus(client, "one", 3, 0, 1) == nil {
		t.Fatal("deleted device still accepts operations")
	}
	if err := e.ReadDeviceModbus(client, "two", 3, 0, 1); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return deviceValue(e, client, "two") == 333 })
}

func TestIEC104DeviceReplacementMatchesOriginal(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	id, err := e.UpsertChannel(Channel{Name: "104", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: twoDevices("iec104", true)[:1]})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	if _, err = e.UpsertDevice(id, "iec104", Device{Name: "new", Unit: 1, CommonAddress: 2}, false); err == nil {
		t.Fatal("unconfirmed replacement accepted")
	}
	if _, err = e.UpsertDevice(id, "iec104", Device{Name: "", Unit: 1, CommonAddress: 2}, true); err == nil {
		t.Fatal("invalid replacement accepted")
	}
	if e.Snapshot().States[0].Status != "listening" || deviceValue(e, id, "one") != 111 {
		t.Fatal("rejected replacement stopped original channel")
	}
	replacement, err := e.UpsertDevice(id, "iec104", Device{Name: "new", Unit: 1, CommonAddress: 2}, true)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := e.Snapshot()
	if snapshot.States[0].Status != "stopped" || len(snapshot.Project.Channels[0].Devices) != 1 {
		t.Fatal("replacement must stop channel and retain one device")
	}
	if _, err = e.Device(id, "one"); err == nil {
		t.Fatal("old 104 device survived replacement")
	}
	d, err := e.Device(id, replacement)
	if err != nil || d.CommonAddress != 2 || len(d.Points) != 0 {
		t.Fatal("replacement retained old point table")
	}
	c := snapshot.Project.Channels[0]
	c.Devices = twoDevices("iec104", true)
	if _, err = e.UpsertChannel(c); err == nil {
		t.Fatal("multi-device 104 project accepted despite original rule")
	}
}

func TestMultiDevicePendingResponseIdentity(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "master", Protocol: "modbus-tcp", Role: "client", Transport: "tcp-client", Address: "127.0.0.1:502", Devices: twoDevices("modbus-tcp", false)})
	if err != nil {
		t.Fatal(err)
	}
	s := newSession(e, e.Snapshot().Project.Channels[0])
	request, _ := ReadPDU(3, 0, 1)
	p := &peer{s: s, pending: map[uint16]pendingRequest{1: {deviceID: "one", unit: 1, pdu: request, until: time.Now().Add(time.Second)}, 2: {deviceID: "two", unit: 2, pdu: request, until: time.Now().Add(time.Second)}}}
	if err = p.handleModbus(EncodeTCP(2, 1, []byte{3, 2, 0, 77})); err != nil {
		t.Fatal(err)
	}
	if len(p.pending) != 2 {
		t.Fatal("wrong unit consumed pending request")
	}
	if err = p.handleModbus(EncodeTCP(2, 2, []byte{3, 2, 0, 88})); err != nil {
		t.Fatal(err)
	}
	if deviceValue(e, id, "two") != 88 || deviceValue(e, id, "one") != -1 {
		t.Fatal("response attributed to wrong device")
	}
	if err = e.DeleteDevice(id, "one"); err != nil {
		t.Fatal(err)
	}
	if err = p.handleModbus(EncodeTCP(1, 1, []byte{3, 2, 0, 99})); err != nil {
		t.Fatal(err)
	}
	if len(e.Snapshot().Project.Channels[0].Devices) != 1 {
		t.Fatal("late response recreated deleted device")
	}
}

func TestMultiDeviceRTUBroadcastUpdatesAll(t *testing.T) {
	e := NewEngine()
	_, err := e.UpsertChannel(Channel{Name: "rtu", Protocol: "modbus-rtu", Role: "server", Transport: "serial", Address: "COM1", Devices: twoDevices("modbus-rtu", true)})
	if err != nil {
		t.Fatal(err)
	}
	cfg := e.Snapshot().Project.Channels[0]
	p := &peer{s: newSession(e, cfg)}
	write, _ := WritePDU(6, 0, 555)
	if err = p.handleModbus(EncodeRTU(0, write)); err != nil {
		t.Fatal(err)
	}
	if deviceValue(e, cfg.ID, "one") != 555 || deviceValue(e, cfg.ID, "two") != 555 {
		t.Fatal("RTU broadcast did not reach every slave")
	}
}

func TestDeviceMigrationPersistenceAndIsolation(t *testing.T) {
	old := Project{Version: 1, Name: "legacy", Channels: []Channel{{ID: "old", Name: "old", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Unit: 7, Points: []Point{{Kind: "holding", Address: 0, Value: 7}}}}}
	file := filepath.Join(t.TempDir(), "legacy.json")
	data, _ := json.Marshal(old)
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	e := NewEngine()
	if err := e.Load(file); err != nil {
		t.Fatal(err)
	}
	first := e.Snapshot().Project.Channels[0].Devices[0]
	if first.Unit != 7 || first.Points[0].Value != 7 {
		t.Fatal("legacy data lost")
	}
	second, err := e.UpsertDevice("old", "modbus-tcp", Device{Name: "second", Unit: 8, CommonAddress: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.SetDevicePoints("old", second, []Point{{Kind: "holding", Address: 0, Value: 8}}); err != nil {
		t.Fatal(err)
	}
	if err = e.PatchDevicePoint("old", second, &PointKey{Kind: "holding", Address: 0}, Point{Name: "renamed"}, []string{"name"}); err != nil {
		t.Fatal(err)
	}
	snapshot := e.Snapshot()
	snapshot.Project.Channels[0].Devices[1].Points[0].Value = 999
	if deviceValue(e, "old", second) != 8 || deviceValue(e, "old", first.ID) != 7 {
		t.Fatal("snapshot or point edit leaked")
	}
	if _, err = e.UpsertDevice("old", "modbus-tcp", Device{Name: "duplicate", Unit: 7}, false); err == nil {
		t.Fatal("duplicate unit accepted")
	}
	if err = e.Save(file); err != nil {
		t.Fatal(err)
	}
	loaded := NewEngine()
	if err = loaded.Load(file); err != nil {
		t.Fatal(err)
	}
	if len(loaded.Snapshot().Project.Channels[0].Devices) != 2 || deviceValue(loaded, "old", second) != 8 {
		t.Fatal("multi-device roundtrip failed")
	}
	for _, id := range []string{first.ID, second} {
		if err = loaded.DeleteDevice("old", id); err != nil {
			t.Fatal(err)
		}
	}
	if err = loaded.Save(file); err != nil {
		t.Fatal(err)
	}
	empty := NewEngine()
	if err = empty.Load(file); err != nil {
		t.Fatal(err)
	}
	if len(empty.Snapshot().Project.Channels[0].Devices) != 0 {
		t.Fatal("last deleted device was resurrected by legacy migration")
	}
}

func TestDeviceProtocolChangeIsAtomic(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "switch", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: twoDevices("modbus-tcp", true)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.UpsertDevice(id, "modbus-rtu", Device{Name: "RTU", Unit: 3, CommonAddress: 1}, false); err == nil {
		t.Fatal("unconfirmed replacement accepted")
	}
	if _, err = e.UpsertDevice(id, "modbus-rtu", Device{Name: "RTU", Unit: 0, CommonAddress: 1}, true); err == nil {
		t.Fatal("invalid new protocol committed")
	}
	if got := e.Snapshot().Project.Channels[0]; got.Protocol != "modbus-tcp" || len(got.Devices) != 2 {
		t.Fatal("failed validation removed existing devices")
	}
	if _, err = e.UpsertDevice(id, "modbus-rtu", Device{Name: "RTU", Unit: 3, CommonAddress: 1}, true); err != nil {
		t.Fatal(err)
	}
	got := e.Snapshot().Project.Channels[0]
	if got.Transport != "tcp-server" || got.Address != "127.0.0.1:0" || len(got.Devices) != 1 || len(got.Devices[0].Points) != 0 {
		t.Fatalf("invalid replacement: %+v", got)
	}
}

func TestLastDeviceRemovalStopsChannel(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	id, err := e.UpsertChannel(Channel{Name: "last", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: twoDevices("modbus-tcp", true)[:1]})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	if err = e.DeleteDevice(id, "one"); err != nil {
		t.Fatal(err)
	}
	snapshot := e.Snapshot()
	if snapshot.States[0].Status != "stopped" || snapshot.Project.Channels[0].Protocol != "raw" || len(snapshot.Project.Channels[0].Devices) != 0 {
		t.Fatal("removing last device must stop channel and clear protocol")
	}
}
