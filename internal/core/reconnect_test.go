package core

import (
	"net"
	"testing"
	"time"
)

func waitConnected(t *testing.T, e *Engine, id string) {
	t.Helper()
	eventually(t, func() bool {
		for _, s := range e.Snapshot().States {
			if s.ID == id {
				return s.Status == "connected" && s.Peers > 0
			}
		}
		return false
	})
}

func TestTCPClientWaitsReconnectsAndStops(t *testing.T) {
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserve.Addr().String()
	reserve.Close()
	e := NewEngine()
	defer e.StopAll()
	id, err := e.UpsertChannel(Channel{Name: "waiting", Protocol: "raw", Role: "client", Transport: "tcp-client", Address: address, TimeoutMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { s := e.Snapshot().States[0]; return s.Status == "connecting" && s.LastError != "" })
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(4 * time.Second))
	first, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	waitConnected(t, e, id)
	first.Close()
	eventually(t, func() bool { return e.Snapshot().States[0].Status == "connecting" })
	eventually(t, func() bool {
		for _, m := range e.Snapshot().Messages {
			if m.Peer == address && m.Summary == "对端连接已断开；等待自动重连" {
				return true
			}
		}
		return false
	})
	second, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	waitConnected(t, e, id)
	if e.Snapshot().States[0].LastError != "" {
		t.Fatal("stale error after reconnect")
	}
	e.StopAll()
	listener.(*net.TCPListener).SetDeadline(time.Now().Add(1200 * time.Millisecond))
	if c, err := listener.Accept(); err == nil {
		c.Close()
		t.Fatal("stopped channel reconnected")
	}
	if e.Snapshot().States[0].Status != "stopped" {
		t.Fatal("stale session changed stopped state")
	}
}

func TestStopCancelsPendingTCPDial(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "pending", Protocol: "raw", Role: "client", Transport: "tcp-client", Address: "192.0.2.1:2425", TimeoutMS: 30000})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	e.StopAll()
	if time.Since(start) > 2*time.Second {
		t.Fatal("pending dial blocked start/stop")
	}
	if e.Snapshot().States[0].Status != "stopped" {
		t.Fatal("channel not stopped")
	}
}
