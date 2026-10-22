package core

import (
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestOnlineCommunicationEditsStopUntilManualConnect(t *testing.T) {
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	_ = first.(*net.TCPListener).SetDeadline(time.Now().Add(3 * time.Second))
	_ = second.(*net.TCPListener).SetDeadline(time.Now().Add(3 * time.Second))
	e := NewEngine()
	defer e.StopAll()
	id, err := e.UpsertChannel(Channel{Name: "online", Protocol: "raw", Role: "client", Transport: "tcp-client", Address: first.Addr().String(), TimeoutMS: 300})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	old, err := first.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	config := e.Snapshot().Project.Channels[0]
	config.Address = second.Addr().String()
	if err = e.UpdateTransport(config); err != nil {
		t.Fatal(err)
	}
	if e.Snapshot().States[0].Status != "stopped" || e.running[id] != nil {
		t.Fatal("edited channel restarted automatically")
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	current, err := second.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	waitConnected(t, e, id)
	if err = e.SendHex(id, "0102", false); err != nil {
		t.Fatal(err)
	}
	_ = current.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 2)
	n, err := io.ReadFull(current, buf)
	if err != nil || n != 2 || buf[1] != 2 {
		t.Fatalf("new endpoint not used: %x %v", buf, err)
	}
	_ = old.SetReadDeadline(time.Now().Add(time.Second))
	if _, err = old.Read(buf); err == nil {
		t.Fatal("old connection survived")
	}
	invalid := config
	invalid.Address = "invalid"
	if err = e.UpdateTransport(invalid); err == nil {
		t.Fatal("invalid edit accepted")
	}
	if e.Snapshot().Project.Channels[0].Address != config.Address {
		t.Fatal("invalid edit changed live endpoint")
	}
	if err = e.SendHex(id, "03", false); err != nil {
		t.Fatal("invalid edit stopped old connection:", err)
	}
}
func TestOnlineServerEditsStopListenerAndKeepDeviceData(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	id, err := e.UpsertChannel(Channel{Name: "server", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Unit: 1, Points: []Point{{Kind: "holding", Address: 5, Value: 123}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	stale := e.Snapshot().Project.Channels[0]
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserve.Addr().String()
	reserve.Close()
	stale.Address = address
	if err = e.SetDevicePoints(id, stale.Devices[0].ID, []Point{{Kind: "holding", Address: 5, Value: 456}}); err != nil {
		t.Fatal(err)
	}
	if err = e.UpdateTransport(stale); err != nil {
		t.Fatal(err)
	}
	if e.Snapshot().States[0].Status != "stopped" || e.running[id] != nil {
		t.Fatal("server restarted automatically")
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	c := e.Snapshot().Project.Channels[0]
	if c.Devices[0].Points[0].Value != 456 {
		t.Fatal("online update restored stale point data")
	}
}
func TestCommunicationEditsStopRetryUntilManualConnect(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	id, err := e.UpsertChannel(Channel{Name: "retry", Protocol: "raw", Role: "client", Transport: "tcp-client", Address: listener.Addr().String(), TimeoutMS: 100})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	reserve, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := reserve.Addr().String()
	reserve.Close()
	config := e.Snapshot().Project.Channels[0]
	config.Address = address
	if err = e.UpdateTransport(config); err != nil {
		t.Fatal(err)
	}
	if e.Snapshot().Project.Channels[0].Address != address || e.Snapshot().States[0].Status != "stopped" || e.running[id] != nil {
		t.Fatal("failed endpoint not preserved")
	}
	available, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer available.Close()
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	waitConnected(t, e, id)
}

func TestTransportDraftSurvivesProjectRoundTripWithoutSharingMaps(t *testing.T) {
	e := NewEngine()
	if err := e.EnsureWorkspaceChannel("client"); err != nil {
		t.Fatal(err)
	}
	c := e.Snapshot().Project.Channels[0]
	c.TransportDraft = map[string]string{"cbComList": "COM18", "txtServerPort": "1503", "txtClientIP": "192.0.2.4"}
	if err := e.UpdateTransport(c); err != nil {
		t.Fatal(err)
	}
	c.TransportDraft["cbComList"] = "incorrect"
	snapshot := e.Snapshot()
	if snapshot.Project.Channels[0].TransportDraft["cbComList"] != "COM18" {
		t.Fatal("input draft aliases engine")
	}
	snapshot.Project.Channels[0].TransportDraft["cbComList"] = "snapshot mutation"
	path := filepath.Join(t.TempDir(), "project.json")
	if err := e.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded := NewEngine()
	if err := loaded.Load(path); err != nil {
		t.Fatal(err)
	}
	got := loaded.Snapshot().Project.Channels[0].TransportDraft
	if got["cbComList"] != "COM18" || got["txtServerPort"] != "1503" {
		t.Fatalf("draft lost on save: %+v", got)
	}
}

func TestTransportEditorPreservesConcurrentDeviceAndPointChanges(t *testing.T) {
	e := NewEngine()
	id, err := e.UpsertChannel(Channel{Name: "before", Protocol: "raw", Role: "client", Transport: "tcp-client", Address: "127.0.0.1:2404"})
	if err != nil {
		t.Fatal(err)
	}
	stale := e.Snapshot().Project.Channels[0]
	deviceID, err := e.UpsertDevice(id, "modbus-tcp", Device{Name: "new device", Unit: 1, CommonAddress: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = e.SetDevicePoints(id, deviceID, []Point{{Kind: "holding", Address: 1, Value: 77}}); err != nil {
		t.Fatal(err)
	}
	if err = e.RenameChannel(id, "after"); err != nil {
		t.Fatal(err)
	}
	stale.Address = "127.0.0.1:1502"
	if err = e.UpdateTransport(stale); err != nil {
		t.Fatal(err)
	}
	c := e.Snapshot().Project.Channels[0]
	if c.Name != "after" || c.Protocol != "modbus-tcp" || len(c.Devices) != 1 || len(c.Devices[0].Points) != 1 || c.Devices[0].Points[0].Value != 77 || c.Address != stale.Address {
		t.Fatalf("stale editor overwrote live data: %+v", c)
	}
	if err = e.DeleteChannel(id); err != nil {
		t.Fatal(err)
	}
	if err = e.UpdateTransport(stale); err == nil {
		t.Fatal("transport editor recreated deleted channel")
	}
}

func TestServerCommunicationEditSavesUnavailableBindWithoutStarting(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	id, err := e.UpsertChannel(Channel{Name: "server", Protocol: "raw", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(id); err != nil {
		t.Fatal(err)
	}
	config := e.Snapshot().Project.Channels[0]
	config.Address = "192.0.2.99:2425"
	if err = e.UpdateTransport(config); err != nil {
		t.Fatal(err)
	}
	if e.Snapshot().Project.Channels[0].Address != config.Address || e.Snapshot().States[0].Status != "stopped" || e.running[id] != nil {
		t.Fatal("save attempted to bind or discarded settings")
	}
	// Saving from an error state must not silently retry the connection either.
	e.mu.Lock()
	e.states[id] = ChannelState{ID: id, Status: "error", LastError: "previous bind error"}
	e.mu.Unlock()
	if err = e.UpdateTransport(config); err != nil {
		t.Fatal(err)
	}
	if e.Snapshot().States[0].Status != "stopped" {
		t.Fatal("save restarted failed channel")
	}
}
