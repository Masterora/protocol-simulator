package core

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestCRCAndRTUVector(t *testing.T) {
	request := []byte{1, 3, 0, 0, 0, 10}
	if crc := CRC16(request); crc != 0xcdc5 {
		t.Fatalf("CRC=%04x", crc)
	}
	frame := EncodeRTU(1, request[1:])
	want := []byte{1, 3, 0, 0, 0, 10, 0xc5, 0xcd}
	if !bytes.Equal(frame, want) {
		t.Fatalf("frame=%x", frame)
	}
	got, err := ReadModbusRTU(bytes.NewReader(frame), true)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%x %v", got, err)
	}
	frame[3] ^= 1
	if _, err = ReadModbusRTU(bytes.NewReader(frame), true); err == nil {
		t.Fatal("corrupt CRC accepted")
	}
}

type shortReader struct{ io.Reader }

func (r shortReader) Read(b []byte) (int, error) {
	if len(b) > 2 {
		b = b[:2]
	}
	return r.Reader.Read(b)
}
func TestStreamFraming(t *testing.T) {
	a := EncodeTCP(3, 1, []byte{3, 2, 0, 9})
	b := EncodeTCP(4, 1, []byte{6, 0, 1, 0, 8})
	reader := shortReader{bytes.NewReader(append(a, b...))}
	for _, want := range [][]byte{a, b} {
		got, err := ReadModbusTCP(reader)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("got %x %v", got, err)
		}
	}
	x := append(UFrame(StartAct), IFrame(1, 2, interrogation(1, 6))...)
	r := shortReader{bytes.NewReader(x)}
	for _, want := range [][]byte{UFrame(StartAct), IFrame(1, 2, interrogation(1, 6))} {
		got, err := Read104(r)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("104=%x %v", got, err)
		}
	}
	if _, err := ReadModbusTCP(bytes.NewReader([]byte{0, 0, 0, 0, 0xff, 0xff, 1})); err == nil {
		t.Fatal("unbounded length accepted")
	}
	if _, err := Read104(bytes.NewReader([]byte{0x68, 3})); err == nil {
		t.Fatal("short frame accepted")
	}
}

func TestModbusReadWriteAndExceptions(t *testing.T) {
	points := []Point{{Address: 0, Kind: "holding", Value: 220}, {Address: 1, Kind: "holding", Value: 12}, {Address: 0, Kind: "coil", Value: 1}}
	req, _ := ReadPDU(3, 0, 2)
	response, updates := ModbusReply(req, points)
	if updates != nil || !bytes.Equal(response, []byte{3, 4, 0, 220, 0, 12}) {
		t.Fatalf("response=%x", response)
	}
	decoded, err := DecodeModbusResponse(req, response)
	if err != nil || len(decoded) != 2 || decoded[0].Value != 220 {
		t.Fatalf("%v %v", decoded, err)
	}
	write, _ := WritePDU(5, 0, 0)
	response, updates = ModbusReply(write, points)
	if len(updates) != 1 || updates[0].Value != 0 || !bytes.Equal(write, response) {
		t.Fatal("single coil write failed")
	}
	response, updates = ModbusReply([]byte{16, 0, 0, 0, 2, 4, 0, 1, 0, 2}, points)
	if len(updates) != 2 || updates[1].Value != 2 || !bytes.Equal(response, []byte{16, 0, 0, 0, 2}) {
		t.Fatalf("multiwrite=%x %+v", response, updates)
	}
	response, updates = ModbusReply([]byte{16, 0, 1, 0, 2, 4, 0, 1, 0, 2}, points)
	if !bytes.Equal(response, []byte{0x90, 2}) || updates != nil {
		t.Fatal("out-of-map write was not atomic")
	}
	response, _ = ModbusReply([]byte{16, 0, 0, 0, 2, 1, 0}, points)
	if !bytes.Equal(response, []byte{0x90, 3}) {
		t.Fatal("malformed byte count accepted")
	}
	if _, err = ReadPDU(3, 65535, 2); err == nil {
		t.Fatal("address overflow accepted")
	}
	if _, err = ReadPDU(3, 0, 126); err == nil {
		t.Fatal("register count overflow accepted")
	}
	if _, err = DecodeModbusResponse(req, []byte{0x83, 2}); err == nil {
		t.Fatal("exception lost")
	}
	if _, err = DecodeModbusResponse(req, []byte{3, 2, 0, 1}); err == nil {
		t.Fatal("response count mismatch accepted")
	}
}

func Test104DataDecode(t *testing.T) {
	a := ASDU(1, 0x83, 20, 1, []byte{1, 0, 0, 1, 0, 1})
	points, err := Decode104Points(a, 1)
	if err != nil || len(points) != 3 || points[2].Address != 3 || points[1].Value != 0 {
		t.Fatalf("%+v %v", points, err)
	}
	want := Point{Address: 16385, Kind: "yc", Value: 220.5, Quality: 0x80}
	got, err := Decode104Points(PointASDU(want, 1, 3), 1)
	if err != nil || got[0].Address != want.Address || got[0].Value != 220.5 || got[0].Quality != 0x80 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err = Decode104Points(a, 2); err == nil {
		t.Fatal("wrong common address accepted")
	}
	if _, err = Decode104Points(a[:len(a)-1], 1); err == nil {
		t.Fatal("truncated points accepted")
	}
	frame := IFrame(32767, 32767, interrogation(1, 6))
	if binary.LittleEndian.Uint16(frame[2:]) != 65534 {
		t.Fatal("sequence encoding")
	}
}

func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		if fn() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("condition timed out")
		case <-tick.C:
		}
	}
}
func findChannel(p Project, role string) Channel {
	for _, c := range p.Channels {
		if c.Role == role {
			return c
		}
	}
	return Channel{}
}

func TestEngineModbusLoopback(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	id, err := e.Demo("modbus-tcp")
	if err != nil {
		t.Fatal(err)
	}
	waitConnected(t, e, id)
	if err = e.ReadModbus(id, 3, 0, 2); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		c := findChannel(e.Snapshot().Project, "client")
		return len(c.Points) == 2 && c.Points[0].Value == 220
	})
	if err = e.WriteModbus(id, 6, 0, 321); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		snap := e.Snapshot()
		client := findChannel(snap.Project, "client")
		server := findChannel(snap.Project, "server")
		return client.Points[0].Value == 321 && server.Points[1].Value == 321
	})
	e.StopAll()
	for _, s := range e.Snapshot().States {
		if s.Status != "stopped" {
			t.Fatalf("not stopped: %+v", s)
		}
	}
}

func TestEngine104LoopbackAndWindow(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	id, err := e.Demo("iec104")
	if err != nil {
		t.Fatal(err)
	}
	server := findChannel(e.Snapshot().Project, "server")
	points := make([]Point, 100)
	for i := range points {
		points[i] = Point{Address: uint32(16385 + i), Kind: "yc", Name: "test", Value: float64(i) + .5}
	}
	if err = e.SetPoints(server.ID, points); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return e.Interrogate(id) == nil })
	eventually(t, func() bool {
		c := findChannel(e.Snapshot().Project, "client")
		return len(c.Points) == 100 && c.Points[99].Value == 99.5
	})
	points[0].Value = 777
	if err = e.SetPoints(server.ID, points); err != nil {
		t.Fatal(err)
	}
	if err = e.Publish(server.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { c := findChannel(e.Snapshot().Project, "client"); return c.Points[0].Value == 777 })
	for _, m := range e.Snapshot().Messages {
		if m.Direction == "ERR" {
			t.Fatalf("protocol error: %s", m.Summary)
		}
	}
}

func Test104SequenceRejection(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	id, err := e.UpsertChannel(Channel{Name: "test", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", CommonAddress: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp", e.Snapshot().States[0].Address)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	_, _ = c.Write(UFrame(StartAct))
	b, err := Read104(c)
	if err != nil || !bytes.Equal(b, UFrame(StartCon)) {
		t.Fatalf("%x %v", b, err)
	}
	_, _ = c.Write(IFrame(1, 0, interrogation(1, 6)))
	if _, err = Read104(c); err == nil {
		t.Fatal("out-of-sequence I-frame accepted")
	}
}

func TestProjectRoundTripAndGuards(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "测试 <&>", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Unit: 1, Points: []Point{{Address: 1, Kind: "holding", Name: "A,汉字", Value: 123}}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "工程.json")
	if err = e.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded := NewEngine()
	if err = loaded.Load(path); err != nil {
		t.Fatal(err)
	}
	snap := loaded.Snapshot()
	if snap.Project.Channels[0].Points[0].Name != "A,汉字" || snap.Dirty {
		t.Fatal("roundtrip failed")
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	defer e.StopAll()
	if _, err = e.UpsertChannel(e.Snapshot().Project.Channels[0]); err == nil {
		t.Fatal("active config edit accepted")
	}
	if err = e.Load(path); err == nil {
		t.Fatal("active project replace accepted")
	}
	if err = e.SetPoints(id, []Point{{Address: 1, Kind: "holding"}, {Address: 1, Kind: "holding"}}); err == nil {
		t.Fatal("duplicate point accepted")
	}
}

func TestRawTCPAndUDPLoopback(t *testing.T) {
	for _, transport := range []string{"tcp-client", "udp"} {
		t.Run(transport, func(t *testing.T) {
			e := NewEngine()
			defer e.StopAll()
			var address string
			finished := make(chan error, 1)
			if transport == "tcp-client" {
				l, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
				address = l.Addr().String()
				go func() {
					c, err := l.Accept()
					if err != nil {
						finished <- err
						return
					}
					defer c.Close()
					_ = c.SetDeadline(time.Now().Add(3 * time.Second))
					b := make([]byte, 2)
					_, err = io.ReadFull(c, b)
					if err == nil {
						_, err = c.Write(b)
					}
					finished <- err
				}()
			} else {
				c, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				address = c.LocalAddr().String()
				go func() {
					_ = c.SetDeadline(time.Now().Add(3 * time.Second))
					b := make([]byte, 20)
					n, from, err := c.ReadFromUDP(b)
					if err == nil {
						_, err = c.WriteToUDP(b[:n], from)
					}
					finished <- err
				}()
			}
			id, err := e.UpsertChannel(Channel{Name: "RAW test", Protocol: "raw", Role: "client", Transport: transport, Address: address})
			if err != nil {
				t.Fatal(err)
			}
			if err = e.Start(id); err != nil {
				t.Fatal(err)
			}
			waitConnected(t, e, id)
			if err = e.SendHex(id, "CA FE", false); err != nil {
				t.Fatal(err)
			}
			eventually(t, func() bool {
				for _, m := range e.Snapshot().Messages {
					if m.Direction == "RX" && m.Hex == "CAFE" {
						return true
					}
				}
				return false
			})
			if err = <-finished; err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRTUBroadcastWriteDoesNotWaitForResponse(t *testing.T) {
	e := NewEngine()
	_, err := e.UpsertChannel(Channel{ID: "broadcast", Name: "broadcast", Protocol: "modbus-rtu", Role: "client", Transport: "serial", Address: "COM1", Unit: 0, TimeoutMS: 3000})
	if err != nil {
		t.Fatal(err)
	}
	s := newSession(e, e.Snapshot().Project.Channels[0])
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	p := &peer{s: s, conn: a, pending: map[uint16]pendingRequest{}, done: make(chan struct{})}
	s.peers[p] = true
	received := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 8)
		_ = b.SetReadDeadline(time.Now().Add(time.Second))
		_, _ = io.ReadFull(b, buf)
		received <- buf
	}()
	request, _ := WritePDU(6, 1, 9)
	if err := s.requestModbus(request); err != nil {
		t.Fatal(err)
	}
	want := EncodeRTU(0, request)
	if got := <-received; !bytes.Equal(got, want) {
		t.Fatalf("%x", got)
	}
	if len(p.pending) != 0 {
		t.Fatal("broadcast should not wait")
	}
	read, _ := ReadPDU(3, 0, 1)
	if err := s.requestModbus(read); err == nil {
		t.Fatal("broadcast read accepted")
	}
}
