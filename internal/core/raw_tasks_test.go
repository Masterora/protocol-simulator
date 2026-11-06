package core

import (
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func rawFixture(t *testing.T) (*Engine, string, net.Conn) {
	t.Helper()
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "raw", Protocol: "raw", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", TimeoutMS: 200})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.StopAll)
	var address string
	for _, s := range e.ChannelStates() {
		if s.ID == id {
			address = s.Address
		}
	}
	c, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		for _, s := range e.ChannelStates() {
			if s.ID == id && s.Peers == 1 {
				return e, id, c
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("peer not ready")
	return nil, "", nil
}
func readRaw(t *testing.T, c net.Conn, n int) []byte {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(2 * time.Second))
	b := make([]byte, n)
	if _, err := io.ReadFull(c, b); err != nil {
		t.Fatal(err)
	}
	return b
}
func TestRawRepliesReassembleAndOnlyReplyToRequester(t *testing.T) {
	e, id, c := rawFixture(t)
	cfg := defaultRawTools()
	cfg.Framing = "fixed"
	cfg.FrameLength = 4
	cfg.Replies = []RawReplyRule{{Enabled: true, Offset: 1, Match: "AA BB", Response: "01 02", Format: "hex"}}
	if err := e.SaveRawTools(id, cfg); err != nil {
		t.Fatal(err)
	}
	var address string
	for _, s := range e.ChannelStates() {
		if s.ID == id {
			address = s.Address
		}
	}
	other, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err = e.StartRawTask(id, "reply"); err != nil {
		t.Fatal(err)
	}
	c.Write([]byte{0, 0xaa})
	time.Sleep(10 * time.Millisecond)
	c.Write([]byte{0xbb, 0xff, 0, 0xaa, 0xbb, 0xff})
	if got := readRaw(t, c, 4); string(got) != string([]byte{1, 2, 1, 2}) {
		t.Fatal(got)
	}
	other.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, err = other.Read(make([]byte, 1)); err == nil {
		t.Fatal("reply was broadcast to another peer")
	}
	if err = e.SaveRawTools(id, cfg); err == nil {
		t.Fatal("running config changed")
	}
	if err = e.StopRawTask(id, "reply"); err != nil {
		t.Fatal(err)
	}
	v, _ := e.GetRawTools(id)
	if v.Reply.Running || v.Reply.Sent != 2 {
		t.Fatal(v)
	}
	if err = e.SendHex(id, "FF", false); err != nil {
		t.Fatal("stopping task stopped channel", err)
	}
}
func TestRawSendOrderCyclesPersistenceAndExplicitStart(t *testing.T) {
	e, id, c := rawFixture(t)
	cfg := defaultRawTools()
	cfg.Rows = []RawSendRow{{Enabled: true, Data: "AA", Format: "hex"}, {Enabled: true, Data: "B", Format: "text"}}
	cfg.Selection = "2,1"
	cfg.Cycles = 2
	cfg.IntervalMS = 10
	if err := e.SaveRawTools(id, cfg); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("saving implicitly started sender")
	}
	if err := e.StartRawTask(id, "send"); err != nil {
		t.Fatal(err)
	}
	if got := readRaw(t, c, 4); string(got) != string([]byte{'B', 0xaa, 'B', 0xaa}) {
		t.Fatal(got)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		v, _ := e.GetRawTools(id)
		if !v.Send.Running {
			if v.Send.Cycles != 2 || v.Send.Sent != 4 {
				t.Fatal(v)
			}
			break
		}
		time.Sleep(time.Millisecond)
	}
	file := filepath.Join(t.TempDir(), "project.json")
	if err := e.Save(file); err != nil {
		t.Fatal(err)
	}
	fresh := NewEngine()
	if err := fresh.Load(file); err != nil {
		t.Fatal(err)
	}
	v, err := fresh.GetRawTools(id)
	if err != nil || v.Send.Running || v.Reply.Running || v.Config.Selection != "2,1" {
		t.Fatal(v, err)
	}
	cfg.Cycles = 0
	if err = e.SaveRawTools(id, cfg); err != nil {
		t.Fatal(err)
	}
	if err = e.StartRawTask(id, "send"); err != nil {
		t.Fatal(err)
	}
	readRaw(t, c, 1)
	e.Stop(id)
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		v, _ = e.GetRawTools(id)
		if !v.Send.Running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("channel stop left sender running")
}
func TestRawTaskValidationAndProtocolIsolation(t *testing.T) {
	e, id, _ := rawFixture(t)
	cfg := defaultRawTools()
	cfg.Rows = []RawSendRow{{Enabled: true, Data: "0", Format: "hex"}}
	if err := e.SaveRawTools(id, cfg); err == nil {
		t.Fatal("invalid payload accepted")
	}
	cfg.Rows[0].Data = "01"
	cfg.Selection = "1-999"
	if err := e.SaveRawTools(id, cfg); err == nil {
		t.Fatal("selection out of range accepted")
	}
	cfg = defaultRawTools()
	cfg.Framing = "delimiter"
	cfg.Delimiter = ""
	if err := e.SaveRawTools(id, cfg); err == nil {
		t.Fatal("empty delimiter accepted")
	}
	other, err := e.UpsertChannel(Channel{Name: "protocol", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.SaveRawTools(other, defaultRawTools()); err == nil {
		t.Fatal("protocol auto response could be overridden")
	}
}

func TestRawIdleDelimiterAndPromptCancellation(t *testing.T) {
	for _, mode := range []string{"idle", "delimiter"} {
		t.Run(mode, func(t *testing.T) {
			e, id, c := rawFixture(t)
			cfg := defaultRawTools()
			cfg.Framing = mode
			cfg.Delimiter = "0D0A"
			cfg.Replies = []RawReplyRule{{Enabled: true, Match: "AA BB", Response: "55", Format: "hex"}}
			if err := e.SaveRawTools(id, cfg); err != nil {
				t.Fatal(err)
			}
			if err := e.StartRawTask(id, "reply"); err != nil {
				t.Fatal(err)
			}
			c.Write([]byte{0xaa})
			time.Sleep(time.Millisecond)
			suffix := []byte{0xbb}
			count := 1
			if mode == "delimiter" {
				suffix = append(suffix, 0x0d, 0x0a, 0xaa, 0xbb, 0x0d, 0x0a)
				count = 2
			}
			c.Write(suffix)
			got := readRaw(t, c, count)
			for _, v := range got {
				if v != 0x55 {
					t.Fatal(got)
				}
			}
		})
	}
	e, id, c := rawFixture(t)
	cfg := defaultRawTools()
	cfg.Framing = "fixed"
	cfg.FrameLength = 2
	cfg.Replies = []RawReplyRule{{Enabled: true, Match: "AA BB", Response: "55", Format: "hex", DelayMS: 1000}}
	if err := e.SaveRawTools(id, cfg); err != nil {
		t.Fatal(err)
	}
	if err := e.StartRawTask(id, "reply"); err != nil {
		t.Fatal(err)
	}
	c.Write([]byte{0xaa, 0xbb})
	time.Sleep(20 * time.Millisecond)
	start := time.Now()
	if err := e.StopRawTask(id, "reply"); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("stopping a delayed reply was not prompt")
	}
	if err := e.SendHex(id, "FE", false); err != nil {
		t.Fatal(err)
	}
	if got := readRaw(t, c, 1); got[0] != 0xfe {
		t.Fatal("stopped job emitted its delayed reply", got)
	}
	low, err := rawPayload("01 03 00 00 00 01", "hex", "crc16")
	if err != nil {
		t.Fatal(err)
	}
	high, err := rawPayload("01 03 00 00 00 01", "hex", "crc16-high")
	if err != nil || low[6] != high[7] || low[7] != high[6] {
		t.Fatal(low, high, err)
	}
}

func TestRawReplyRejectsOverflowingOffset(t *testing.T) {
	cfg := defaultRawTools()
	cfg.Replies = []RawReplyRule{{Enabled: true, Offset: int(^uint(0) >> 1), Match: "01", Response: "02", Format: "hex"}}
	if err := validateRawTools(cfg); err == nil {
		t.Fatal("overflowing match offset accepted")
	}
}
