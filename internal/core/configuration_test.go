package core

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func configurationState(e *Engine, id string) string {
	for _, state := range e.Snapshot().States {
		if state.ID == id {
			return state.Status
		}
	}
	return ""
}

func TestConfigurationSaveValidatesBeforeDisconnectAndLeavesOnlineValuesConnected(t *testing.T) {
	e, _, s := dataLoopback(t, "iec104")
	did := s.Devices[0].ID
	if _, err := e.SetPointValue(s.ID, did, PointKey{"yc", 16385}, 12.5); err != nil {
		t.Fatal(err)
	}
	if _, err := e.SetTelemetryRange(s.ID, did, TelemetryRangeEdit{Category: "yc", Points: "16385-16386", Mode: "set", Value: 25}); err != nil {
		t.Fatal(err)
	}
	if configurationState(e, s.ID) != "connected" {
		t.Fatal("online value changes disconnected transport")
	}
	config := AutoDataConfig{Rules: []AutoDataRule{{Enabled: true, Kind: "yc", Points: "16385-16386", Mode: "increment", Min: 0, Max: 100, Step: 1, IntervalMS: 200}}}
	bad := *cloneAutoData(&config)
	bad.Rules[0].IntervalMS = 1
	if _, err := e.ConfigureAutoData(s.ID, did, "yc", bad); err == nil {
		t.Fatal("invalid rule accepted")
	}
	if configurationState(e, s.ID) != "connected" {
		t.Fatal("invalid configuration disconnected transport")
	}
	if _, err := e.SaveDataTest(s.ID, did, "yc", config); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartDataTest(s.ID, did, "yc"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ConfigureAutoData(s.ID, did, "yc", config); err != nil {
		t.Fatal(err)
	}
	if v, _ := e.GetDataTest(s.ID, did, "yc"); v.Status != "running" || configurationState(e, s.ID) != "connected" {
		t.Fatal("saving unchanged rules disturbed running task")
	}
	config.Rules[0].IntervalMS = 400
	if _, err := e.ConfigureAutoData(s.ID, did, "yc", config); err != nil {
		t.Fatal(err)
	}
	if configurationState(e, s.ID) != "stopped" {
		t.Fatal("changed configuration did not disconnect")
	}
	if v, _ := e.GetDataTest(s.ID, did, "yc"); v.Status == "running" || v.Status == "paused" || v.Config.Rules[0].IntervalMS != 400 {
		t.Fatalf("save resumed or failed to update task: %+v", v)
	}
	if _, err := e.StartDataTest(s.ID, did, "yc"); err == nil {
		t.Fatal("task ran before manual reconnect")
	}
}

func TestMappingAndPointConfigurationDisconnectAfterValidation(t *testing.T) {
	for _, kind := range []string{"remote", "relation", "point"} {
		t.Run(kind, func(t *testing.T) {
			e, _, s := autoFixture(t, []Point{{Kind: "yx", Address: 1}, {Kind: "yc", Address: 10}, {Kind: "yk", Address: 100}})
			did := s.Devices[0].ID
			var err error
			switch kind {
			case "remote":
				err = e.ConfigureControlSimulation(s.ID, did, "yk", ControlSimulation{Mode: "normal", Mappings: []ControlMapping{{Source: PointKey{"yk", 100}, Target: PointKey{"yx", 999}}}})
			case "relation":
				err = e.ConfigureTelemetryRelations(s.ID, did, []TelemetryRelation{{Enabled: true, Type: "yx-yc", Source: 1, Targets: []uint32{999}, Min: 1, Max: 1, Value: 5}})
			case "point":
				_, err = e.ConfigurePointCell(s.ID, did, PointKey{"yc", 10}, "address", Point{Address: 0xffffff + 1})
			}
			if err == nil || configurationState(e, s.ID) != "connected" {
				t.Fatalf("invalid config affected connection: %v", err)
			}
			switch kind {
			case "remote":
				err = e.ConfigureControlSimulation(s.ID, did, "yk", ControlSimulation{Mode: "normal", Mappings: []ControlMapping{{Source: PointKey{"yk", 100}, Target: PointKey{"yx", 1}}}})
			case "relation":
				err = e.ConfigureTelemetryRelations(s.ID, did, []TelemetryRelation{{Enabled: true, Type: "yx-yc", Source: 1, Targets: []uint32{10}, Min: 1, Max: 1, Value: 5}})
			case "point":
				_, err = e.ConfigurePointCell(s.ID, did, PointKey{"yc", 10}, "address", Point{Address: 11})
			}
			if err != nil || configurationState(e, s.ID) != "stopped" {
				t.Fatalf("valid config did not disconnect: %v", err)
			}
		})
	}
}

func TestAutomaticConfigurationDraftIsIsolatedAndSaveRequiresManualReconnect(t *testing.T) {
	e, c, _ := autoFixture(t, []Point{{Kind: "yk", Address: 100}, {Kind: "yt", Address: 101}})
	did := c.Devices[0].ID
	draft, err := e.PrepareAutoControlDraft(c.ID, did, "yk", AutoControlSeed{})
	if err != nil {
		t.Fatal(err)
	}
	count := 2
	if _, err = e.UpdateAutoDraftRows(c.ID, did, "yk", []AutoControlRowEdit{{Index: 0, Generation: draft.Generation, Count: &count}}); err != nil {
		t.Fatal(err)
	}
	options := draft.Options
	options.Cycle, options.IntervalMS, options.TimeoutMS = true, 20, 1000
	if _, err = e.UpdateAutoDraftOptions(c.ID, did, "yk", options); err != nil {
		t.Fatal(err)
	}
	live, _ := e.GetAutoControl(c.ID, did, "yk")
	if live.Saved || live.Rows[0].Count != 1 || live.Options.Cycle || configurationState(e, c.ID) != "connected" {
		t.Fatal("draft edits changed runtime settings or connection")
	}
	if _, err = e.StartSavedAutoControl(c.ID, did, "yk"); err == nil {
		t.Fatal("unsaved draft was executable")
	}
	saved, err := e.SaveAutoControlDraft(c.ID, did, "yk")
	if err != nil || !saved.Saved || saved.Rows[0].Count != 2 || configurationState(e, c.ID) != "stopped" {
		t.Fatalf("save did not commit and disconnect: %+v %v", saved, err)
	}
	if _, err = e.StartSavedAutoControl(c.ID, did, "yk"); err == nil {
		t.Fatal("start succeeded before manual connect")
	}
	if err = e.Start(c.ID); err != nil {
		t.Fatal(err)
	}
	waitConnected(t, e, c.ID)
	eventually(t, func() bool { _, err = e.StartSavedAutoControl(c.ID, did, "yk"); return err == nil })
	eventually(t, func() bool { v, _ := e.GetAutoControl(c.ID, did, "yk"); return v.Total >= 2 })
	draft, err = e.PrepareAutoControlDraft(c.ID, did, "yk", AutoControlSeed{})
	if err != nil {
		t.Fatal(err)
	}
	count = 3
	if _, err = e.UpdateAutoDraftRows(c.ID, did, "yk", []AutoControlRowEdit{{Index: 0, Generation: draft.Generation, Count: &count}}); err != nil {
		t.Fatal(err)
	}
	live, _ = e.GetAutoControl(c.ID, did, "yk")
	if live.Status != "running" || live.Rows[0].Count != 2 || configurationState(e, c.ID) != "connected" {
		t.Fatal("editing configuration paused or changed active worker")
	}
	job, _ := e.autoJob(c.ID, did, "yk")
	oldDone := job.done
	saved, err = e.SaveAutoControlDraft(c.ID, did, "yk")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-oldDone:
	case <-time.After(time.Second):
		t.Fatal("old worker did not stop")
	}
	live, _ = e.GetAutoControl(c.ID, did, "yk")
	if live.ID != saved.ID || live.Status != "idle" || live.Total != 0 || live.Rows[0].Count != 3 || configurationState(e, c.ID) != "stopped" {
		t.Fatalf("old worker overwrote saved configuration: %+v", live)
	}
	path := filepath.Join(t.TempDir(), "saved-auto.json")
	if err = e.Save(path); err != nil {
		t.Fatal(err)
	}
	restored := NewEngine()
	t.Cleanup(restored.StopAll)
	if err = restored.Load(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := restored.GetAutoControl(c.ID, did, "yk")
	if err != nil || !loaded.Saved || loaded.Status != "idle" || loaded.Rows[0].Count != 3 || len(restored.running) != 0 {
		t.Fatalf("saved recipe was lost or resumed: %+v %v", loaded, err)
	}
	if !reflect.DeepEqual(autoRecipe(loaded), autoRecipe(saved)) {
		t.Fatal("saved options changed after project reload")
	}
}

func TestSavedAutomaticAdjustmentPausesWithoutSendingTheNextPoint(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yt", Address: 1}, {Kind: "yt", Address: 2}})
	did := c.Devices[0].ID
	draft, err := e.PrepareAutoControlDraft(c.ID, did, "yt", AutoControlSeed{Addresses: "1,2", Value: "42", ValueType: 50})
	if err != nil {
		t.Fatal(err)
	}
	o := draft.Options
	o.IntervalMS, o.Direct = 200, true
	if _, err = e.UpdateAutoDraftOptions(c.ID, did, "yt", o); err != nil {
		t.Fatal(err)
	}
	if _, err = e.SaveAutoControlDraft(c.ID, did, "yt"); err != nil {
		t.Fatal(err)
	}
	if err = e.Start(c.ID); err != nil {
		t.Fatal(err)
	}
	waitConnected(t, e, c.ID)
	eventually(t, func() bool { _, err = e.StartSavedAutoControl(c.ID, did, "yt"); return err == nil })
	eventually(t, func() bool { v, _ := e.GetAutoControl(c.ID, did, "yt"); return v.Success == 1 })
	if v, err := e.PauseAutoControl(c.ID, did, "yt"); err != nil || v.Status != "paused" {
		t.Fatalf("pause: %+v %v", v, err)
	}
	time.Sleep(300 * time.Millisecond)
	d, _ := e.Device(s.ID, s.Devices[0].ID)
	if d.Points[0].Value != 42 || d.Points[1].Value != 0 {
		t.Fatal("paused adjustment sent the next point")
	}
	if _, err := e.ResumeAutoControl(c.ID, did, "yt"); err != nil {
		t.Fatal(err)
	}
	autoWait(t, e, c, "yt", "completed")
	d, _ = e.Device(s.ID, s.Devices[0].ID)
	if d.Points[1].Value != 42 {
		t.Fatal("resumed adjustment did not finish")
	}
}

func TestPointDeletionConfigurationDisconnectsOnlyAfterReferenceValidation(t *testing.T) {
	e, _, s := autoFixture(t, []Point{{Kind: "yx", Address: 1}, {Kind: "yc", Address: 10}, {Kind: "yk", Address: 100}})
	did := s.Devices[0].ID
	if err := e.SaveControlSimulation(s.ID, did, "yk", ControlSimulation{Mode: "normal", Mappings: []ControlMapping{{Source: PointKey{"yk", 100}, Target: PointKey{"yx", 1}}}}); err != nil {
		t.Fatal(err)
	}
	if err := e.ConfigureDeleteDevicePoint(s.ID, did, PointKey{"yx", 1}); err == nil || configurationState(e, s.ID) != "connected" {
		t.Fatalf("referenced deletion: %v", err)
	}
	if err := e.ConfigureDeleteDevicePoint(s.ID, did, PointKey{"yc", 10}); err != nil || configurationState(e, s.ID) != "stopped" {
		t.Fatalf("valid deletion: %v", err)
	}
	d, _ := e.Device(s.ID, did)
	if len(d.Points) != 2 {
		t.Fatal("point was not deleted")
	}
}

func TestDiscardAutoDraftPreservesSavedRecipeAndConnection(t *testing.T) {
	e, c, _ := autoFixture(t, []Point{{Kind: "yk", Address: 100}})
	id := c.Devices[0].ID
	draft, err := e.PrepareAutoControlDraft(c.ID, id, "yk", AutoControlSeed{})
	if err != nil {
		t.Fatal(err)
	}
	count := 5
	if _, err = e.UpdateAutoDraftRows(c.ID, id, "yk", []AutoControlRowEdit{{Index: 0, Generation: draft.Generation, Count: &count}}); err != nil {
		t.Fatal(err)
	}
	e.DiscardAutoControlDraft(c.ID, id, "yk")
	reopened, err := e.GetAutoControlDraft(c.ID, id, "yk")
	if err != nil || reopened.Rows[0].Count == count || configurationState(e, c.ID) != "connected" {
		t.Fatalf("cancel leaked draft or stopped connection: %+v %v", reopened, err)
	}
}
func TestNormalizedDataSavePreservesActiveTask(t *testing.T) {
	e, _, c := dataLoopback(t, "iec104")
	id := c.Devices[0].ID
	config := AutoDataConfig{Rules: []AutoDataRule{{Enabled: true, Kind: "yc", Points: "16385-16386", Mode: "fixed", Min: 1, Max: 999, Step: 42, IntervalMS: 200}}}
	if _, err := e.SaveDataTest(c.ID, id, "telemetry", config); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartDataTest(c.ID, id, "telemetry"); err != nil {
		t.Fatal(err)
	}
	config.Rules[0].Points = "16385，16386"
	config.Rules[0].Max = 1
	config.Rules[0].Step = 1
	if _, err := e.ConfigureAutoData(c.ID, id, "telemetry", config); err != nil {
		t.Fatal(err)
	}
	v, _ := e.GetDataTest(c.ID, id, "telemetry")
	if v.Status != "running" || configurationState(e, c.ID) != "connected" {
		t.Fatal("equivalent config disturbed running task")
	}
}

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

func TestSaveRenamesProjectAndPersistsChosenFilename(t *testing.T) {
	e := NewEngine()
	if err := e.EnsureWorkspaceChannel("client"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	var firstPath string
	for i, filename := range []string{"现场联调.json", "工程2.v1.JSON"} {
		path := filepath.Join(root, filename)
		if err := e.Save(path); err != nil {
			t.Fatal(err)
		}
		want := []string{"现场联调", "工程2.v1"}[i]
		got := e.Snapshot()
		if got.Project.Name != want || got.Path != path || got.Dirty {
			t.Fatalf("save metadata not synchronized: %+v", got)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var project Project
		if err = json.Unmarshal(data, &project); err != nil || project.Name != want {
			t.Fatalf("persisted name: %q %v", project.Name, err)
		}
		loaded := NewEngine()
		if err = loaded.Load(path); err != nil || loaded.Snapshot().Project.Name != want {
			t.Fatalf("reopened name not preserved: %v", err)
		}
		if i == 0 {
			firstPath = path
		}
	}
	data, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	var original Project
	if err = json.Unmarshal(data, &original); err != nil || original.Name != "现场联调" {
		t.Fatal("save as changed the old file")
	}
}

func TestFailedSavePreservesNamePathAndDirtyState(t *testing.T) {
	e := NewEngine()
	if err := e.EnsureWorkspaceChannel("client"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := e.Save(filepath.Join(root, "原工程.json")); err != nil {
		t.Fatal(err)
	}
	if err := e.RenameChannel(e.Snapshot().Project.Channels[0].ID, "changed"); err != nil {
		t.Fatal(err)
	}
	before := e.Snapshot()
	for _, path := range []string{filepath.Join(root, "missing", "失败.json"), filepath.Join(root, ".json")} {
		if err := e.Save(path); err == nil {
			t.Fatal("invalid save unexpectedly succeeded")
		}
		if !reflect.DeepEqual(e.Snapshot(), before) {
			t.Fatal("failed save changed current project")
		}
	}
}
