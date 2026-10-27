package core

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestSnapshotDistinguishes104TCPConnectionFromActivatedTransfer(t *testing.T) {
	e := NewEngine()
	t.Cleanup(e.StopAll)
	id, err := e.UpsertChannel(Channel{Name: "delayed activation", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Unit: 1, CommonAddress: 1, TimeoutMS: 30000})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", e.Snapshot().States[0].Address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	state := func() ChannelState {
		for _, s := range e.Snapshot().States {
			if s.ID == id {
				return s
			}
		}
		t.Fatal("missing channel")
		return ChannelState{}
	}
	eventually(t, func() bool { return state().Peers == 1 })
	if s := state(); s.Status != "connected" || s.ReadyPeers != 0 {
		t.Fatalf("TCP connection was treated as ready: %+v", s)
	}
	exchange := func(act, con byte) {
		t.Helper()
		if _, err = conn.Write(UFrame(act)); err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		reply, err := Read104(conn)
		if err != nil || !bytes.Equal(reply, UFrame(con)) {
			t.Fatalf("reply %x: %v", reply, err)
		}
	}
	exchange(StartAct, StartCon)
	eventually(t, func() bool { return state().ReadyPeers == 1 })
	exchange(StopAct, StopCon)
	eventually(t, func() bool { return state().ReadyPeers == 0 })
	if s := state(); s.Peers != 1 || s.Status != "connected" {
		t.Fatalf("STOPDT incorrectly closed TCP: %+v", s)
	}
}
