package core

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestDeviceRangesRejectSharedAndPartialRegisterOverlap(t *testing.T) {
	for _, format := range []string{"0", "4"} {
		config := &DeviceConfiguration{Version: "TCP版", Values: map[string]string{"cb_ycFormat": format}, Ranges: []PointRange{{Tab: "yc", Kind: "holding", Address: 100, Count: 2, Name: "遥测"}, {Tab: "yt", Kind: "holding", Address: 101, Count: 2, Name: "遥调"}}}
		if _, err := configuredPoints("modbus-tcp", config, nil); err == nil || !strings.Contains(err.Error(), "重叠") {
			t.Fatalf("overlap accepted: %s %v", format, err)
		}
	}
}
func TestClockRangesUseActualRegisterWidthsAndIgnoreEmptyTables(t *testing.T) {
	c := &DeviceConfiguration{Version: "TCP版", Values: map[string]string{"clockEnabled": "true", "TimeAddr": "0xC", "cb_ycFormat": "0"}, Ranges: []PointRange{{Tab: "yc", Kind: "holding", Address: 10, Count: 2, Name: "遥测"}, {Tab: "yt", Kind: "holding", Address: 12, Count: 0, Name: "遥调"}}}
	if err := validateConfiguration("modbus-tcp", c); err != nil {
		t.Fatalf("adjacent uint16 registers rejected: %v", err)
	}
	c.Values["cb_ycFormat"] = "4"
	if err := validateConfiguration("modbus-tcp", c); err == nil || !strings.Contains(err.Error(), "校时") {
		t.Fatalf("float32 overlap accepted: %v", err)
	}
	for _, raw := range []string{"12", "0xC", "CH"} {
		if v, err := parseAddress(raw, 65535); err != nil || v != 12 {
			t.Fatalf("address %q: %v %v", raw, v, err)
		}
	}
}
func TestDisabledDeviceSettingsRetainDraftWithoutBlockingSave(t *testing.T) {
	c := &DeviceConfiguration{Version: "标准版", Values: map[string]string{"mcEnable": "false", "txtMC_Time": "draft", "timeEnable": "false", "txtSyn_Time": "draft", "cbxRetry": "false", "retryNum": "draft", "retryInterval": "draft", "ck_IsDataSentWaitS": "true", "sendWindow": "draft"}}
	if err := validateConfiguration("iec104", c); err != nil {
		t.Fatal(err)
	}
	c.Values["mcEnable"] = "true"
	if err := validateConfiguration("iec104", c); err == nil || !strings.Contains(err.Error(), "脉冲召唤间隔") {
		t.Fatalf("bad enabled interval accepted: %v", err)
	}
	if c.Values["txtMC_Time"] != "draft" {
		t.Fatal("disabled draft was changed")
	}
}
func TestDeviceOverlapValidationDoesNotDisconnectOrModifyExistingData(t *testing.T) {
	e, id := func() (*Engine, string) {
		e := NewEngine()
		id, err := e.UpsertChannel(Channel{Name: "s", Protocol: "raw", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0"})
		if err != nil {
			t.Fatal(err)
		}
		return e, id
	}()
	t.Cleanup(e.StopAll)
	config := &DeviceConfiguration{Version: "TCP版", Ranges: []PointRange{{Tab: "yc", Kind: "holding", Address: 100, Count: 2, Name: "遥测"}}}
	did, err := e.UpsertDevice(id, "modbus-tcp", Device{Name: "device", Unit: 1, CommonAddress: 1, Configuration: config}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.EditPointCell(id, did, PointKey{Kind: "holding", Address: 100}, "value", Point{Value: 123}); err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	d, _ := e.Device(id, did)
	d.Configuration.Ranges = append(d.Configuration.Ranges, PointRange{Tab: "yt", Kind: "holding", Address: 100, Count: 1, Name: "遥调"})
	if _, err = e.UpsertDevice(id, "modbus-tcp", d, false); err == nil {
		t.Fatal("overlap accepted")
	}
	d, _ = e.Device(id, did)
	if len(d.Points) != 2 || d.Points[0].Value != 123 || e.Snapshot().States[0].Status != "listening" {
		t.Fatal("invalid edit changed the live device")
	}
}
func TestConfiguredServerAdjustmentAndStepExecuteOverReal104Connection(t *testing.T) {
	e, client, server := controlLoopback(t)
	d, _ := e.Device(server.ID, server.Devices[0].ID)
	d.Configuration = &DeviceConfiguration{Version: "标准版", Ranges: []PointRange{{Tab: "yt", Kind: "yt", Address: 25089, Count: 1, Name: "遥调"}, {Tab: "step", Kind: "step", Address: 24833, Count: 1, Name: "步调节"}}}
	if _, err := e.UpsertDevice(server.ID, "iec104", d, false); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(server.ID); err != nil {
		t.Fatal(err)
	}
	// The demo binds port zero, so update the client to the newly assigned port.
	snap := e.Snapshot()
	for _, st := range snap.States {
		if st.ID == server.ID {
			client.Address = st.Address
		}
	}
	e.Stop(client.ID)
	if _, err := e.UpsertChannel(client); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(client.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		s, err := e.getSession(client.ID)
		if err != nil {
			return false
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for p := range s.peers {
			p.mu.Lock()
			ready := p.transfer && !p.closed
			p.mu.Unlock()
			if ready {
				return true
			}
		}
		return false
	})
	for _, req := range []Control104Request{{Type: 50, Address: 25089, Value: 12.5, Operation: "execute", Direct: true, TimeoutMS: 1500}, {Type: 47, Address: 24833, Value: 2, Operation: "execute", Direct: true, TimeoutMS: 1500}} {
		result, err := e.Command104Device(client.ID, client.Devices[0].ID, req)
		if err != nil || result.Stage != "completed" {
			t.Fatalf("adjustment failed: %+v %v", result, err)
		}
	}
	d, _ = e.Device(server.ID, d.ID)
	for _, p := range d.Points {
		if p.Kind == "yt" && p.Value != 12.5 || p.Kind == "step" && p.Value != 1 {
			t.Fatalf("wrong adjusted point: %+v", p)
		}
	}
}
func Test104ConfiguredPartialFrameTimeoutKeepsIdleConnectionOpen(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "timeout", Protocol: "iec104", Role: "client", Transport: "tcp-client", Address: "127.0.0.1:2404", TimeoutMS: 3000, Devices: []Device{{ID: "d", Name: "d", Unit: 1, CommonAddress: 1, Configuration: &DeviceConfiguration{Version: "标准版", Values: map[string]string{"txt_FrameOverTime": "30"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	cfg := e.Snapshot().Project.Channels[0]
	if cfg.ID != id {
		t.Fatal("wrong channel")
	}
	p := &peer{s: newSession(e, cfg), conn: client}
	r := &packetReader{Reader: bufio.NewReader(client), peer: p}
	done := make(chan error, 1)
	go func() { b := make([]byte, 2); _, err := io.ReadFull(r, b); done <- err }()
	time.Sleep(60 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("idle timed out: %v", err)
	default:
	}
	if _, err = server.Write([]byte{0x68}); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err == nil {
			t.Fatal("partial frame accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("configured frame timeout did not apply")
	}
}

func TestOriginalConfigurationBuildsIndependentPointTables(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "通道0", Protocol: "raw", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	config := &DeviceConfiguration{Version: "TCP版", Values: map[string]string{"txtAdd": "1", "txtYX_Add": "1", "txtYX_Num": "18"}, Ranges: []PointRange{{Tab: "yx", Kind: "discrete", Address: 1, Count: 18, Name: "遥信"}, {Tab: "yc", Kind: "holding", Address: 1024, Count: 18, Name: "遥测"}, {Tab: "mc", Kind: "input", Address: 2048, Count: 18, Name: "脉冲"}, {Tab: "yk", Kind: "coil", Address: 2560, Count: 18, Name: "遥控"}}}
	deviceID, err := e.UpsertDevice(id, "modbus-tcp", Device{Name: "Modbus", Unit: 1, CommonAddress: 1, Configuration: config}, false)
	if err != nil {
		t.Fatal(err)
	}
	d, err := e.Device(id, deviceID)
	if err != nil || len(d.Points) != 72 || d.Points[18].Address != 1024 {
		t.Fatalf("configuration did not build point table: %+v %v", d, err)
	}
	config.Values["txtAdd"] = "99"
	d.Configuration.Ranges[0].Address = 77
	snapshot := e.Snapshot()
	snapshot.Project.Channels[0].Devices[0].Configuration.Values["txtAdd"] = "88"
	stored, _ := e.Device(id, deviceID)
	if stored.Configuration.Values["txtAdd"] != "1" || stored.Configuration.Ranges[0].Address != 1 {
		t.Fatal("configuration was not deeply copied")
	}
	second, err := e.UpsertDevice(id, "modbus-tcp", Device{Name: "Modbus", Unit: 2, CommonAddress: 1, Configuration: stored.Configuration}, false)
	if err != nil {
		t.Fatal(err)
	}
	e.applyDevicePoints(id, deviceID, []Point{{Kind: "holding", Address: 1024, Value: 666}})
	stored, _ = e.Device(id, deviceID)
	if _, err = e.UpsertDevice(id, "modbus-tcp", stored, false); err != nil {
		t.Fatal(err)
	}
	stored, _ = e.Device(id, deviceID)
	other, _ := e.Device(id, second)
	if stored.Points[18].Value != 666 || other.Points[18].Value != 0 {
		t.Fatal("configuration editing overwrote live data or another device")
	}
	defer e.StopAll()
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	stored.Configuration.Ranges[0].Address = 65535
	stored.Configuration.Ranges[0].Count = 2
	if _, err = e.UpsertDevice(id, "modbus-tcp", stored, false); err == nil {
		t.Fatal("overflowing point range accepted")
	}
	if e.Snapshot().States[0].Status != "listening" {
		t.Fatal("invalid configuration stopped current connection")
	}
}

func TestRTUOverTCPPreservesIndependentCommunicationConfiguration(t *testing.T) {
	e, server, client := multiPair(t, "modbus-rtu")
	for _, id := range []string{"one", "two"} {
		if err := e.ReadDeviceModbus(client, id, 3, 0, 1); err != nil {
			t.Fatal(err)
		}
		expected := 111.0
		if id == "two" {
			expected = 222
		}
		eventually(t, func() bool { return deviceValue(e, client, id) == expected })
	}
	if err := e.WriteDeviceModbus(client, "two", 6, 0, 444); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return deviceValue(e, server, "two") == 444 })
	if deviceValue(e, server, "one") != 111 {
		t.Fatal("RTU write changed another device")
	}
	if got := e.Snapshot().Project.Channels[0].Transport; got != "tcp-server" {
		t.Fatal("adding RTU devices changed communication settings")
	}
}

func TestClientAdjustmentRangePreservesTelemetryAndPersists(t *testing.T) {
	for _, protocol := range []string{"iec104", "modbus-tcp"} {
		t.Run(protocol, func(t *testing.T) {
			e := NewEngine()
			channel, err := e.UpsertChannel(Channel{Name: "client", Protocol: "raw", Role: "client", Transport: "tcp-client", Address: "127.0.0.1:2404"})
			if err != nil {
				t.Fatal(err)
			}
			version, telemetry, adjustment := "标准版", "yc", "yt"
			if protocol == "modbus-tcp" {
				version, telemetry, adjustment = "TCP版", "holding", "holding"
			}
			config := &DeviceConfiguration{Version: version, Values: map[string]string{}, Ranges: []PointRange{{Tab: "yc", Kind: telemetry, Address: 10, Count: 2, Name: "遥测"}}}
			device, err := e.UpsertDevice(channel, protocol, Device{Name: "d", Unit: 1, CommonAddress: 1, Configuration: config}, false)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = e.EditPointCell(channel, device, PointKey{Kind: telemetry, Address: 10}, "value", Point{Value: 12}); err != nil {
				t.Fatal(err)
			}
			d, _ := e.Device(channel, device)
			d.Configuration.Ranges = append(d.Configuration.Ranges, PointRange{Tab: "yt", Kind: adjustment, Address: 500, Count: 3, Name: "遥调"})
			d.Configuration.Values["txtYT_Add"], d.Configuration.Values["txtYT_Num"] = "500", "3"
			if _, err = e.UpsertDevice(channel, protocol, d, false); err != nil {
				t.Fatal(err)
			}
			path := t.TempDir() + "/adjustment.json"
			if err = e.Save(path); err != nil {
				t.Fatal(err)
			}
			restored := NewEngine()
			if err = restored.Load(path); err != nil {
				t.Fatal(err)
			}
			d, err = restored.Device(channel, device)
			if err != nil {
				t.Fatal(err)
			}
			if len(d.Points) != 5 {
				t.Fatalf("unexpected point count: %+v", d.Points)
			}
			found, yt := false, 0
			for _, p := range d.Points {
				if p.Tab == "yc" && p.Address == 10 && p.Value == 12 {
					found = true
				}
				if p.Tab == "yt" {
					yt++
					if p.Kind != adjustment || p.Address < 500 || p.Address > 502 {
						t.Fatalf("invalid adjustment: %+v", p)
					}
				}
			}
			if !found || yt != 3 || d.Configuration.Values["txtYT_Num"] != "3" {
				t.Fatalf("lost telemetry or adjustment range: %+v", d)
			}
			if protocol == "modbus-tcp" && len(modbusPollTasks(d)) != 1 {
				t.Fatal("adjustment points must not add automatic read requests")
			}
		})
	}
}

func legacyNormalizedDevice(typ byte) Device {
	return Device{ID: "legacy", Name: "legacy", Unit: 1, CommonAddress: 1,
		Configuration: &DeviceConfiguration{Version: "标准版", Values: map[string]string{"cbYC_Type": fmt.Sprint(typ)}},
		Points:        []Point{{Kind: "yc", Address: 10, Value: 0.5}, {Kind: "yc", Address: 11, Value: 0.25}},
	}
}

func TestLegacyNormalizedFormatRejectsInvalidEditsBeforeChangingData(t *testing.T) {
	for _, typ := range []byte{9, 21, 34} {
		t.Run(fmt.Sprint(typ), func(t *testing.T) {
			e := NewEngine()
			t.Cleanup(e.StopAll)
			d := legacyNormalizedDevice(typ)
			id, err := e.UpsertChannel(Channel{Name: "normalized", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: []Device{d}})
			if err != nil {
				t.Fatal(err)
			}
			before, _ := e.Device(id, d.ID)
			for _, value := range []float64{-1.1, 1, 100} {
				if _, err = e.SetPointValue(id, d.ID, PointKey{"yc", 10}, value); err == nil || !strings.Contains(err.Error(), "点位 10") || !strings.Contains(err.Error(), "归一化") {
					t.Fatalf("missing normalized format context: %v", err)
				}
				if _, err = e.SetTelemetryRange(id, d.ID, TelemetryRangeEdit{Category: "yc", Points: "10-11", Mode: "set", Value: value}); err == nil {
					t.Fatal("invalid normalized batch accepted")
				}
			}
			key := PointKey{"yc", 10}
			if err = e.PatchDevicePoint(id, d.ID, &key, Point{Value: 100}, []string{"value"}); err == nil {
				t.Fatal("detail editor bypassed device format")
			}
			if err = e.SetDevicePoints(id, d.ID, []Point{{Kind: "yc", Address: 10, Value: 100}}); err == nil {
				t.Fatal("point replacement bypassed device format")
			}
			config := AutoDataConfig{Rules: []AutoDataRule{{Enabled: true, Kind: "yc", Points: "10-11", Mode: "increment", Min: 0, Max: 100, Step: 1, IntervalMS: 200}}}
			if _, err = e.SaveDataTest(id, d.ID, "yc", config); err == nil || !strings.Contains(err.Error(), "归一化") {
				t.Fatal("invalid automatic range accepted")
			}
			after, _ := e.Device(id, d.ID)
			if !reflect.DeepEqual(before.Points, after.Points) || after.DataTests["yc"] != nil {
				t.Fatal("rejected operations changed stored values or rules")
			}
			config.Rules[0].Max, config.Rules[0].Step = 0.9, 0.1
			if _, err = e.SaveDataTest(id, d.ID, "yc", config); err != nil {
				t.Fatalf("reasonable normalized defaults rejected: %v", err)
			}
			if _, err = e.SetPointValue(id, d.ID, key, -1); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLegacyNormalizedDefaultsRunAndReportOnReal104Connection(t *testing.T) {
	for _, typ := range []byte{9, 21, 34} {
		t.Run(fmt.Sprint(typ), func(t *testing.T) {
			e := NewEngine()
			t.Cleanup(e.StopAll)
			d := legacyNormalizedDevice(typ)
			sid, err := e.UpsertChannel(Channel{Name: "server", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: []Device{d}})
			if err != nil {
				t.Fatal(err)
			}
			if err = e.Start(sid); err != nil {
				t.Fatal(err)
			}
			cid, err := e.UpsertChannel(Channel{Name: "client", Protocol: "iec104", Role: "client", Transport: "tcp-client", Address: e.Snapshot().States[0].Address, Unit: 1, CommonAddress: 1})
			if err != nil {
				t.Fatal(err)
			}
			if err = e.Start(cid); err != nil {
				t.Fatal(err)
			}
			waitConnected(t, e, cid)
			eventually(t, func() bool { s, _ := e.getSession(sid); return len(readyDataPeers(s)) > 0 })
			before, _ := e.Device(sid, d.ID)
			config := AutoDataConfig{Rules: []AutoDataRule{{Enabled: true, Kind: "yc", Points: "10-11", Mode: "report", IntervalMS: 200}}}
			if _, err = e.SaveDataTest(sid, d.ID, "yc-report", config); err != nil {
				t.Fatal(err)
			}
			if _, err = e.StartDataTest(sid, d.ID, "yc-report"); err != nil {
				t.Fatal(err)
			}
			client := findChannel(e.Snapshot().Project, "client")
			eventually(t, func() bool {
				got, _ := e.Device(cid, client.Devices[0].ID)
				p, ok := findRemotePoint(got.Points, PointKey{"yc", 10})
				return ok && p.WireType == typ && p.Value == 0.5
			})
			if _, err = e.SetDataTestState(sid, d.ID, "yc-report", "stop"); err != nil {
				t.Fatal(err)
			}
			after, _ := e.Device(sid, d.ID)
			if !reflect.DeepEqual(before.Points, after.Points) {
				t.Fatal("reporting changed legacy point metadata or values")
			}
			config.Rules[0].Mode, config.Rules[0].Min, config.Rules[0].Max, config.Rules[0].Step = "increment", 0, 0.9, 0.1
			if _, err = e.SaveDataTest(sid, d.ID, "yc", config); err != nil {
				t.Fatal(err)
			}
			if _, err = e.StartDataTest(sid, d.ID, "yc"); err != nil {
				t.Fatal(err)
			}
			eventually(t, func() bool {
				got, _ := e.Device(cid, client.Devices[0].ID)
				p, ok := findRemotePoint(got.Points, PointKey{"yc", 10})
				return ok && p.WireType == typ && math.Abs(p.Value-0.6) < 1.0/32768
			})
			view, _ := e.GetDataTest(sid, d.ID, "yc")
			if view.LastError != "" || view.Status != "running" {
				t.Fatalf("valid normalized defaults failed while running: %+v", view)
			}
		})
	}
}
