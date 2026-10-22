package core

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtended104WireFormatsAndEventTime(t *testing.T) {
	stamp := "2026-10-22T12:34:56.789+08:00"
	for _, tc := range []struct {
		typ  byte
		kind string
		v    float64
	}{{1, "yx", 1}, {3, "yx", 2}, {5, "position", -12}, {9, "yc", .5}, {11, "yc", -12}, {13, "yc", 12.5}, {15, "mc", -123456}, {21, "yc", -.5}, {30, "yx", 1}, {31, "yx", 2}, {32, "position", -12}, {34, "yc", .5}, {35, "yc", -12}, {36, "yc", 12.5}, {37, "mc", 123456}} {
		t.Run(fmt.Sprint(tc.typ), func(t *testing.T) {
			p := Point{Kind: tc.kind, Address: 20, Value: tc.v, WireType: tc.typ, Timestamp: stamp}
			a, err := pointsASDU([]Point{p}, 1, 3, false)
			if err != nil {
				t.Fatal(err)
			}
			got, err := Decode104Points(a, 1)
			if err != nil || len(got) != 1 || got[0].Value != tc.v {
				t.Fatalf("%x %+v %v", a, got, err)
			}
			if timeTagged(tc.typ) && !strings.Contains(got[0].Timestamp, "56.789") {
				t.Fatalf("event time lost: %+v", got[0])
			}
		})
	}
	a, _ := pointsASDU([]Point{{Kind: "yx", Address: 5, Value: 1}, {Kind: "yx", Address: 6, Value: 0}}, 1, 3, true)
	if a[1] != 130 {
		t.Fatal("SQ1 not used")
	}
	got, err := Decode104Points(a, 1)
	if err != nil || got[1].Address != 6 {
		t.Fatal(got, err)
	}
	bad := encodeCP56(time.Now())
	bad[5] = 13
	if _, _, err := decodeCP56(bad); err == nil {
		t.Fatal("invalid date accepted")
	}
}
func TestTimed104ControlsRoundtrip(t *testing.T) {
	for typ := byte(58); typ <= 63; typ++ {
		v := 1.0
		if typ == 59 || typ == 60 {
			v = 2
		}
		r := Control104Request{Type: typ, Address: 22, Value: v, Operation: "select", Timestamp: "2026-10-22T01:02:03.456Z"}
		a, err := EncodeControl104(1, r)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeControl104(a)
		if err != nil || got.Type != typ || got.Value != v || got.Operation != "select" || got.Timestamp == "" {
			t.Fatalf("%+v %v", got, err)
		}
		r.Operation = "execute"
		r.Timestamp = "2026-10-22T01:02:04.456Z"
		b, _ := EncodeControl104(1, r)
		if !bytes.Equal(controlSignature(a), controlSignature(b)) {
			t.Fatal("time changed selection identity")
		}
	}
}
func TestConfiguredASDUFieldWidths(t *testing.T) {
	for _, typ := range []byte{1, 13, 45, 63, 100, 101, 103} {
		var a []byte
		switch typ {
		case 100:
			a = interrogation(1, 6)
		case 101:
			a = ASDU(101, 1, 6, 1, []byte{0, 0, 0, 5})
		case 103:
			a = ASDU(103, 1, 6, 1, append([]byte{0, 0, 0}, encodeCP56(time.Now())...))
		case 45, 63:
			a, _ = EncodeControl104(1, Control104Request{Type: typ, Address: 12, Value: 1, Operation: "select"})
		default:
			a = PointASDU(Point{Kind: "yc", WireType: typ, Address: 12, Value: 1}, 1, 3)
		}
		wire, err := convertASDU(a, 2, 2, 3, 1, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := convertASDU(wire, 1, 1, 1, 2, 2, 3)
		if err != nil || !bytes.Equal(a, restored) {
			t.Fatalf("%d %x %x %v", typ, a, restored, err)
		}
	}
}
func configuredLoopback(t *testing.T, serverValues, clientValues map[string]string) (*Engine, Channel, Channel) {
	t.Helper()
	e := NewEngine()
	t.Cleanup(e.StopAll)
	points := []Point{{Kind: "yx", Address: 1, Value: 1}, {Kind: "yc", Address: 10, Value: 12.5}, {Kind: "mc", Address: 20, Value: 12345}, {Kind: "yk", Address: 30, Value: 0}}
	sid, err := e.UpsertChannel(Channel{Name: "server", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", TimeoutMS: 3000, Devices: []Device{{ID: "s", Name: "server", CommonAddress: 1, Points: points, Configuration: &DeviceConfiguration{Version: "标准版", Values: serverValues}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(sid); err != nil {
		t.Fatal(err)
	}
	cid, err := e.UpsertChannel(Channel{Name: "client", Protocol: "iec104", Role: "client", Transport: "tcp-client", Address: e.Snapshot().States[0].Address, TimeoutMS: 3000, Devices: []Device{{ID: "c", Name: "client", CommonAddress: 1, Points: []Point{}, Configuration: &DeviceConfiguration{Version: "标准版", Values: clientValues}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(cid); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		s, _ := e.getSession(cid)
		ready := false
		_ = s.forPeers(func(p *peer) error { p.mu.Lock(); ready = p.transfer; p.mu.Unlock(); return nil })
		return ready
	})
	return e, findChannel(e.Snapshot().Project, "client"), findChannel(e.Snapshot().Project, "server")
}
func Test104PeriodicCounterClockAndCancellation(t *testing.T) {
	e, c, _ := configuredLoopback(t, nil, map[string]string{"txtTotal_Time": "1", "txtSyn_Time": "1", "timeEnable": "true", "txtMC_Time": "1", "mcEnable": "true"})
	s, _ := e.getSession(c.ID)
	now := time.Now()
	_ = s.forPeers(func(p *peer) error { p.periodic(now); p.periodic(now.Add(61 * time.Second)); return nil })
	eventually(t, func() bool { d, _ := e.Device(c.ID, "c"); return len(d.Points) >= 3 })
	foundClock := false
	for _, m := range e.Snapshot().Messages {
		if strings.Contains(m.Summary, "收到校时") {
			foundClock = true
		}
	}
	if !foundClock {
		t.Fatal("no clock sync")
	}
	e.Stop(c.ID)
	before := e.Snapshot().States[1].TX
	time.Sleep(100 * time.Millisecond)
	if e.Snapshot().States[1].TX != before {
		t.Fatal("periodic sends survived stop")
	}
}
func Test104ResponsePoliciesAndDelayCancellation(t *testing.T) {
	for _, mode := range []string{"normal", "deny", "silent"} {
		t.Run(mode, func(t *testing.T) {
			values := map[string]string{"responseMode": mode, "responseDelayMs": "80"}
			e, c, server := configuredLoopback(t, values, nil)
			r := Control104Request{Type: 58, Address: 30, Value: 1, Operation: "execute", Direct: true, TimeoutMS: 250}
			started := time.Now()
			_, err := e.Command104Device(c.ID, "c", r)
			if mode == "normal" {
				if err != nil || time.Since(started) < 70*time.Millisecond {
					t.Fatal("delayed command", err)
				}
				d, _ := e.Device(server.ID, "s")
				if d.Points[3].Value != 1 {
					t.Fatal("not applied")
				}
			} else if err == nil {
				t.Fatal("fault policy ignored")
			}
		})
	}
	e, c, s := configuredLoopback(t, map[string]string{"responseDelayMs": "500"}, nil)
	if err := e.InterrogateDevice(c.ID, "c"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	e.Stop(s.ID)
	time.Sleep(550 * time.Millisecond)
	d, _ := e.Device(c.ID, "c")
	if len(d.Points) > 0 {
		t.Fatal("delayed reply survived stop")
	}
}
func Test104SOECOSRetainedAndClearDoesNotDirty(t *testing.T) {
	e, c, s := configuredLoopback(t, nil, nil)
	_ = e.Save(filepath.Join(t.TempDir(), "events.json"))
	session, _ := e.getSession(s.ID)
	stamp := time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
	_ = session.forPeers(func(p *peer) error {
		return p.publishPoints(Device{CommonAddress: 1}, []Point{{Kind: "yx", Address: 1, Value: 1, WireType: 30, Timestamp: stamp}}, 3)
	})
	eventually(t, func() bool { return len(e.Snapshot().Events) > 0 })
	snap := e.Snapshot()
	if snap.Dirty || snap.Events[0].Category != "SOE" || snap.Events[0].Timestamp == "" {
		t.Fatal("SOE state", snap.Dirty, snap.Events)
	}
	e.ClearEvents(c.ID, "COS")
	if len(e.Snapshot().Events) != 1 {
		t.Fatal("wrong category cleared")
	}
	e.ClearEvents(c.ID, "SOE")
	if len(e.Snapshot().Events) != 0 {
		t.Fatal("not cleared")
	}
}
func TestModbusTypedRegisterVectorsAndPartialWrites(t *testing.T) {
	for _, tc := range []struct {
		typ string
		v   float64
	}{{"int16", -123}, {"uint32", 4000000000}, {"int32", -1234567}, {"float32", 12.5}} {
		orders := []string{"AB", "BA"}
		if tc.typ != "int16" {
			orders = []string{"ABCD", "BADC", "CDAB", "DCBA"}
		}
		for _, order := range orders {
			t.Run(tc.typ+order, func(t *testing.T) {
				p := Point{Kind: "holding", Address: 10, DataType: tc.typ, ByteOrder: order, Value: tc.v}
				d := Device{Points: []Point{p}}
				req, _ := ReadPDU(3, 10, uint16(registerWidth(p)))
				reply, _ := ModbusDeviceReply(req, d)
				wire, err := DecodeModbusResponse(req, reply)
				if err != nil {
					t.Fatal(err)
				}
				mapped, err := collapseRegisters(d, wire, false)
				if err != nil || len(mapped) != 1 || mapped[0].Value != tc.v {
					t.Fatal(mapped, err)
				}
				write := []byte{16, 0, 10, 0, byte(registerWidth(p)), byte(len(registerBytes(p)))}
				write = append(write, registerBytes(p)...)
				_, updates := ModbusDeviceReply(write, d)
				if len(updates) != 1 || updates[0].Value != tc.v {
					t.Fatal(updates)
				}
				if registerWidth(p) == 2 {
					part := wire[:1]
					mapped, err = collapseRegisters(d, part, false)
					if err != nil || len(mapped) != 0 {
						t.Fatal("partial read corrupted typed point")
					}
				}
			})
		}
	}
	p := Point{Kind: "holding", Address: 0, DataType: "float32", Value: 1}
	_, updates := ModbusDeviceReply([]byte{16, 0, 0, 0, 2, 4, 0x7f, 0xc0, 0, 0}, Device{Points: []Point{p}})
	if len(updates) != 0 {
		t.Fatal("NaN write applied")
	}
}
func TestModbusPeriodicReadsRealPeerAndDeviceIsolation(t *testing.T) {
	e, server, client := multiPair(t, "modbus-tcp")
	e.Stop(client)
	d, _ := e.Device(client, "one")
	d.Configuration = &DeviceConfiguration{Version: "TCP版", Values: map[string]string{"pollEnabled": "true", "txtSendInterval": "20"}, Ranges: []PointRange{{Tab: "yc", Kind: "holding", Address: 0, Count: 1}}}
	if _, err := e.UpsertDevice(client, "modbus-tcp", d, false); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(client); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return deviceValue(e, client, "one") == 111 })
	if deviceValue(e, client, "two") != -1 {
		t.Fatal("unconfigured device polled")
	}
	if deviceValue(e, server, "one") != 111 {
		t.Fatal("poll changed server")
	}
}
func TestDeviceEditValidatesBeforeStopAndPreservesPointData(t *testing.T) {
	e, _, client := multiPair(t, "modbus-tcp")
	d, _ := e.Device(client, "one")
	d.Points = []Point{{Kind: "holding", Address: 2, Value: 55}}
	if err := e.SetDevicePoints(client, d.ID, d.Points); err != nil {
		t.Fatal(err)
	}
	d.Configuration = &DeviceConfiguration{Version: "TCP版", Values: map[string]string{"txtSendInterval": "0"}}
	if _, err := e.UpsertDevice(client, "modbus-tcp", d, false); err == nil {
		t.Fatal("invalid timer accepted")
	}
	if e.Snapshot().States[1].Status != "connected" {
		t.Fatal("invalid edit stopped channel")
	}
	d.Configuration.Values["txtSendInterval"] = "200"
	d.Configuration = nil
	d.Name = "edited"
	if _, err := e.UpsertDevice(client, "modbus-tcp", d, false); err != nil {
		t.Fatal(err)
	}
	if e.Snapshot().States[1].Status != "stopped" || deviceValue(e, client, "one") != 55 {
		t.Fatal("edit lost data or reconnected")
	}
}
func TestKnownIEEEFloatWireVector(t *testing.T) {
	p := Point{Kind: "holding", DataType: "float32", ByteOrder: "CDAB", Value: 12.5}
	if !bytes.Equal(registerBytes(p), []byte{0, 0, 0x41, 0x48}) {
		t.Fatalf("%x", registerBytes(p))
	}
	if binary.BigEndian.Uint32([]byte{0x41, 0x48, 0, 0}) != math.Float32bits(12.5) {
		t.Fatal("bad fixture")
	}
}

func Test104AcknowledgementBatchRetryAndNonstandardLayoutOverTCP(t *testing.T) {
	e, c, s := configuredLoopback(t, map[string]string{"textEdit2": "1", "textEdit3": "1", "txtAdd": "1", "txt_DeviceAddr": "1"}, map[string]string{"txtCOT": "1", "txtASDU_Byte": "1", "txtMess_Byte": "1", "text_replySICount": "8", "ackDelayMs": "1000"})
	session, _ := e.getSession(s.ID)
	_ = session.forPeers(func(p *peer) error {
		for i := 0; i < 8; i++ {
			if err := p.sendI(PointASDU(Point{Kind: "yx", Address: 1, Value: float64(i % 2)}, 1, 3), "batch"); err != nil {
				return err
			}
		}
		return nil
	})
	eventually(t, func() bool { return len(e.Snapshot().Events) == 8 })
	acks := 0
	for _, m := range e.Snapshot().Messages {
		if m.ChannelID == c.ID && m.Direction == "TX" && strings.HasPrefix(m.Hex, "680401") {
			acks++
		}
	}
	if acks != 1 {
		t.Fatalf("expected one S ack for eight I frames, got %d", acks)
	}
	e2, c2, _ := configuredLoopback(t, map[string]string{"responseMode": "silent"}, map[string]string{"txtTotal_Time": "1", "cbxRetry": "true", "retryInterval": "1", "retryNum": "1"})
	session2, _ := e2.getSession(c2.ID)
	now := time.Now()
	_ = session2.forPeers(func(p *peer) error {
		p.periodic(now)
		p.periodic(now.Add(time.Second))
		p.periodic(now.Add(2 * time.Second))
		p.periodic(now.Add(3 * time.Second))
		return nil
	})
	retries := 0
	for _, m := range e2.Snapshot().Messages {
		if m.Direction == "TX" && m.Summary == "104 命令超时重发" {
			retries++
		}
	}
	if retries != 1 {
		t.Fatalf("retry count %d", retries)
	}
}
func TestModbusClockAndCRCVariants(t *testing.T) {
	d := Device{Configuration: &DeviceConfiguration{Version: "标准版(RTU)", Values: map[string]string{"clockEnabled": "true", "TimeAddr": "1000H", "cb_check": "0:AB"}}}
	now := time.Date(2026, 10, 22, 12, 34, 56, 789000000, time.Local)
	write := modbusClockPDU(d, now)
	reply, updated, handled := modbusTimeReply(write, d, now.Add(time.Hour))
	if !handled || len(reply) != 5 || !updated.Equal(now) {
		t.Fatal(reply, updated)
	}
	read, _ := ReadPDU(3, 4096, 4)
	reply, _, handled = modbusTimeReply(read, d, updated)
	if !handled || len(reply) != 10 || reply[0] != 3 || reply[1] != 8 {
		t.Fatal(reply)
	}
	frame := encodeDeviceRTU(1, read, d)
	got, err := readModbusRTU(bytes.NewReader(frame), true, func(byte) bool { return true })
	if err != nil || !bytes.Equal(frame, got) {
		t.Fatal(err)
	}
	if _, err = ReadModbusRTU(bytes.NewReader(frame), true); err == nil {
		t.Fatal("reversed checksum accepted as standard")
	}
	write[6] = 0xff
	write[7] = 0xff
	reply, _, _ = modbusTimeReply(write, d, now)
	if len(reply) != 2 || reply[0] != 0x90 {
		t.Fatal("invalid clock accepted")
	}
}
func TestTypedRegistersAndFormatsSurviveProjectSaveLoad(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "format", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: []Device{{ID: "d", Name: "d", Unit: 1, Points: []Point{{Kind: "holding", Address: 0, Value: -123.5, DataType: "float32", ByteOrder: "DCBA"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "format.json")
	if err = e.Save(file); err != nil {
		t.Fatal(err)
	}
	loaded := NewEngine()
	if err = loaded.Load(file); err != nil {
		t.Fatal(err)
	}
	d, _ := loaded.Device(id, "d")
	if d.Points[0].DataType != "float32" || d.Points[0].Value != -123.5 || d.Points[0].ByteOrder != "DCBA" || loaded.Snapshot().States[0].Status != "stopped" {
		t.Fatal(d)
	}
}

func TestSettingsEditPreservesSparseLegacyAndManualControlPoints(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "legacy", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: []Device{{ID: "d", Name: "legacy", CommonAddress: 1, Points: []Point{{Kind: "yx", Address: 3, Value: 1, Name: "A"}, {Kind: "yx", Address: 9, Value: 0, Name: "B"}, {Kind: "yt", Address: 100, Value: 12.5, Name: "manual"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	d, _ := e.Device(id, "d")
	d.Configuration = &DeviceConfiguration{Version: "标准版", Values: map[string]string{"txtUTest_Time": "10"}, Ranges: []PointRange{{Tab: "yx", Kind: "yx", Address: 3, Count: 2}}}
	if _, err = e.UpsertDevice(id, "iec104", d, false); err != nil {
		t.Fatal(err)
	}
	d, _ = e.Device(id, "d")
	if len(d.Points) != 3 || d.Points[1].Address != 9 || d.Points[1].Name != "B" || d.Points[2].Value != 12.5 {
		t.Fatal("legacy points changed", d.Points)
	}
}

func TestPartialFrameTimeoutDoesNotTimeoutIdleConnection(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	e := NewEngine()
	p := &peer{s: newSession(e, Channel{Protocol: "modbus-tcp", TimeoutMS: 30}), conn: client}
	r := &packetReader{Reader: bufio.NewReader(client), peer: p}
	done := make(chan error, 1)
	go func() { _, err := ReadModbusTCP(r); done <- err }()
	time.Sleep(60 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("idle read timed out: %v", err)
	default:
	}
	if _, err := server.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || err == io.EOF {
			t.Fatal("incomplete frame not timed out", err)
		}
	case <-time.After(time.Second):
		t.Fatal("partial frame hung")
	}
}
