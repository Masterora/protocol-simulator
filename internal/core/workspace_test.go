package core

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func TestUDPUsesConfiguredLocalPort(t *testing.T) {
	remote, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer remote.Close()
	reserve, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	port := reserve.LocalAddr().(*net.UDPAddr).Port
	reserve.Close()
	e := NewEngine()
	defer e.StopAll()
	id, err := e.UpsertChannel(Channel{Name: "UDP", Protocol: "raw", Role: "client", Transport: "udp", Address: remote.LocalAddr().String(), LocalAddress: fmt.Sprintf("0.0.0.0:%d", port)})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	if err = e.SendHex(id, "010203", false); err != nil {
		t.Fatal(err)
	}
	if err = remote.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 16)
	n, from, err := remote.ReadFromUDP(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if from.Port != port || n != 3 || buffer[0] != 1 {
		t.Fatalf("UDP binding not used: %v %x", from, buffer[:n])
	}
}

func TestWorkspaceStartsWithOneStoppedChannelPerStation(t *testing.T) {
	for _, role := range []string{"client", "server"} {
		t.Run(role, func(t *testing.T) {
			e := NewEngine()
			for i := 0; i < 2; i++ {
				if err := e.EnsureWorkspaceChannel(role); err != nil {
					t.Fatal(err)
				}
			}
			s := e.Snapshot()
			if s.Project.Name != "工程1" || len(s.Project.Channels) != 1 || s.Project.Channels[0].Role != role || s.Project.Channels[0].Name != "通道1" || s.Dirty || s.States[0].Status != "stopped" || len(s.Project.Channels[0].Devices) != 0 {
				t.Fatalf("unexpected initial workspace: %+v", s)
			}
			id := s.Project.Channels[0].ID
			if err := e.RenameChannel(id, "custom"); err != nil {
				t.Fatal(err)
			}
			if err := e.EnsureWorkspaceChannel(role); err != nil {
				t.Fatal(err)
			}
			if got := e.Snapshot().Project.Channels; len(got) != 1 || got[0].ID != id || got[0].Name != "custom" {
				t.Fatalf("existing channel replaced: %+v", got)
			}
		})
	}
}

func TestCustomEndpointAndLocalBindingReachConfiguredListener(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err = l.(*net.TCPListener).SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	e := NewEngine()
	defer e.StopAll()
	if err = e.EnsureWorkspaceChannel("client"); err != nil {
		t.Fatal(err)
	}
	c := e.Snapshot().Project.Channels[0]
	c.Address = l.Addr().String()
	c.LocalAddress = "127.0.0.1:0"
	if err = e.UpdateTransport(c); err != nil {
		t.Fatal(err)
	}
	if err = e.Start(c.ID); err != nil {
		t.Fatal(err)
	}
	conn, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if conn.RemoteAddr().(*net.TCPAddr).IP.String() != "127.0.0.1" {
		t.Fatal(conn.RemoteAddr())
	}
	if got := e.Snapshot().Project.Channels[0]; got.Address != l.Addr().String() || got.LocalAddress != c.LocalAddress {
		t.Fatalf("endpoint not preserved: %+v", got)
	}
}
