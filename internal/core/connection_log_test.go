package core

import (
	"net"
	"testing"
	"time"
)

func TestConnectionLogsDistinguishPeerDisconnectFromChannelStop(t *testing.T) {
	e := NewEngine()
	t.Cleanup(e.StopAll)
	id, err := e.UpsertChannel(Channel{Name: "gateway", Protocol: "raw", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Start(id); err != nil {
		t.Fatal(err)
	}
	connect := func() net.Conn {
		t.Helper()
		conn, err := net.DialTimeout("tcp", e.Snapshot().States[0].Address, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		return conn
	}
	waitLog := func(peer, summary string) {
		t.Helper()
		eventually(t, func() bool {
			for _, m := range e.Snapshot().Messages {
				if m.ChannelID == id && m.Peer == peer && m.Direction == "SYS" && m.Summary == summary {
					return true
				}
			}
			return false
		})
	}
	first, second := connect(), connect()
	eventually(t, func() bool { return e.Snapshot().States[0].Peers == 2 })
	first.Close()
	waitLog(first.LocalAddr().String(), "对端连接已断开；通道仍有 1 个连接")
	if state := e.Snapshot().States[0]; state.Status != "connected" || state.Peers != 1 {
		t.Fatalf("one peer disconnect stopped the channel: %+v", state)
	}
	if _, err := second.Write([]byte("still connected")); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		for _, m := range e.Snapshot().Messages {
			if m.Peer == second.LocalAddr().String() && m.Direction == "RX" {
				return true
			}
		}
		return false
	})
	second.Close()
	waitLog(second.LocalAddr().String(), "对端连接已断开；通道继续监听")
	if state := e.Snapshot().States[0]; state.Status != "listening" || state.Peers != 0 {
		t.Fatalf("last peer disconnect stopped the listener: %+v", state)
	}
	third := connect()
	eventually(t, func() bool { return e.Snapshot().States[0].Peers == 1 })
	e.Stop(id)
	waitLog(third.LocalAddr().String(), "对端连接已关闭（通道停止）")
	waitLog("", "通道已停止")
}
