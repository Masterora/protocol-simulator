package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"gytest-go/internal/core"
)

func editorFixture(t *testing.T) (*App, *nativeRemote) {
	t.Helper()
	a := NewApp()
	n, err := newNativeWindows(a)
	if err != nil {
		t.Fatal(err)
	}
	a.native = n
	n.sessions["test-window"] = &nativeSession{context: NativeContext{Kind: "device-dialog", Role: "client", Channel: "a"}, result: make(chan nativeResult, 1)}
	t.Cleanup(n.close)
	return a, &nativeRemote{address: n.address, token: n.token, session: "test-window"}
}
func rawArgs(values ...any) []json.RawMessage {
	args := make([]json.RawMessage, len(values))
	for i, v := range values {
		args[i], _ = json.Marshal(v)
	}
	return args
}

func TestNativeEditorSharesParentEngine(t *testing.T) {
	a, remote := editorFixture(t)
	raw, err := remote.call("UpsertChannel", rawArgs(core.Channel{Name: "shared", Protocol: "raw", Role: "client", Transport: "tcp-client", Address: "127.0.0.1:2404"}))
	if err != nil {
		t.Fatal(err)
	}
	var id string
	if err = json.Unmarshal(raw, &id); err != nil || id == "" {
		t.Fatalf("invalid channel: %s %v", raw, err)
	}
	if len(a.engine.Snapshot().Project.Channels) != 1 {
		t.Fatal("child did not mutate parent engine")
	}
	raw, err = remote.call("GetSnapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot core.Snapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil || snapshot.Project.Channels[0].ID != id {
		t.Fatalf("inconsistent shared snapshot: %v", err)
	}
}
func TestNativeIndependentDataTestRunsAcrossEditorSessions(t *testing.T) {
	a, remote := editorFixture(t)
	_, err := a.engine.Demo("modbus-tcp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.engine.StopAll)
	var server core.Channel
	for _, c := range a.engine.Snapshot().Project.Channels {
		if c.Role == "server" {
			server = c
		}
	}
	config := core.AutoDataConfig{Rules: []core.AutoDataRule{{Enabled: true, Kind: "holding", Start: 0, End: 1, IntervalMS: 200, Mode: "fixed", Min: 123, Max: 123, Step: 1}}}
	if _, err = remote.call("SaveDataTest", rawArgs(server.ID, server.Devices[0].ID, "yc", config)); err != nil {
		t.Fatal(err)
	}
	if err = a.engine.Start(server.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = remote.call("StartDataTest", rawArgs(server.ID, server.Devices[0].ID, "yc")); err != nil {
		t.Fatal(err)
	}
	a.native.mu.Lock()
	delete(a.native.sessions, remote.session)
	a.native.sessions["reopen"] = &nativeSession{context: NativeContext{Kind: "auto-data-dialog", PointKind: "yc", Role: "server", Channel: server.ID, Device: server.Devices[0].ID}, result: make(chan nativeResult, 1)}
	a.native.mu.Unlock()
	reopened := &nativeRemote{address: remote.address, token: remote.token, session: "reopen"}
	raw, err := reopened.call("GetDataTest", rawArgs(server.ID, server.Devices[0].ID, "yc"))
	if err != nil {
		t.Fatal(err)
	}
	var view core.AutoDataSnapshot
	if err = json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	if view.Status != "running" {
		t.Fatal("closing editor stopped background task")
	}
	if _, err = reopened.call("SetDataTestState", rawArgs(server.ID, server.Devices[0].ID, "yc", "stop")); err != nil {
		t.Fatal(err)
	}
	a.native.mu.Lock()
	a.native.sessions["reopen"].context.Generation = view.Generation
	a.native.mu.Unlock()
	project := a.engine.Snapshot().Project
	if err = a.engine.ReplaceProjectAndStop(project, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.call("GetNativeContext", nil); err == nil {
		t.Fatal("old data window accepted replacement project with reused IDs")
	}
}
func TestNativeDataWindowReusesOneDeviceWindow(t *testing.T) {
	a, _ := editorFixture(t)
	existing := &nativeSession{context: NativeContext{Kind: "auto-data-dialog", Role: "server", Channel: "a", Device: "d", PointKind: "telemetry", Edit: true, Generation: 1}}
	a.native.sessions["unified"] = existing
	for _, category := range []string{"yx", "yc", "yx-report", "yc-report"} {
		context := existing.context
		context.PointKind = "tab:" + strings.TrimSuffix(category, "-report")
		if strings.HasSuffix(category, "-report") {
			context.Point = "report"
		}
		session, err := a.native.open(context)
		if err != nil || session != existing {
			t.Fatalf("did not reuse unified device window: %s %v", category, err)
		}
	}
}

func TestNativeIPCRejectsUnauthorizedAndInvalidCalls(t *testing.T) {
	a, remote := editorFixture(t)
	response, err := http.Post(a.native.address+"/rpc", "application/json", strings.NewReader(`{"method":"GetSnapshot"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal(response.Status)
	}
	for _, name := range []string{"shutdown", "OpenProject", "SaveProject", "OpenNativeWindow", "not-a-method"} {
		if _, err = remote.call(name, nil); err == nil {
			t.Fatalf("allowed %s", name)
		}
	}
	if _, err = remote.call("UpsertChannel", rawArgs("bad type")); err == nil {
		t.Fatal("accepted malformed arguments")
	}
	remote.session = "closed"
	if _, err = remote.call("GetSnapshot", nil); err == nil {
		t.Fatal("accepted closed window")
	}
}
func TestNativeAddFollowsSelectionAndEditKeepsIdentity(t *testing.T) {
	a, remote := editorFixture(t)
	a.SetNativeSelection(NativeContext{Role: "client", Channel: "b"})
	raw, err := remote.call("GetNativeContext", nil)
	if err != nil {
		t.Fatal(err)
	}
	var c NativeContext
	_ = json.Unmarshal(raw, &c)
	if c.Channel != "b" {
		t.Fatal("add must follow current selection")
	}
	a.native.mu.Lock()
	a.native.sessions[remote.session].context.Edit = true
	a.native.mu.Unlock()
	raw, err = remote.call("GetNativeContext", nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &c)
	if c.Channel != "a" {
		t.Fatal("edit must keep original identity")
	}
}
func TestNativeCommunicationReusesOneWindowAndFollowsStationSelection(t *testing.T) {
	a, remote := editorFixture(t)
	s := a.native.sessions[remote.session]
	s.context = NativeContext{Kind: "channel-dialog", Role: "client", Channel: "a", Edit: true}
	opened, err := a.native.open(NativeContext{Kind: "channel-dialog", Role: "client", Channel: "b", Edit: true})
	if err != nil {
		t.Fatal(err)
	}
	if opened != s || len(a.native.sessions) != 1 || s.context.Activate != 1 || s.context.Channel != "b" {
		t.Fatal("communication window not reused")
	}
	a.SetNativeSelection(NativeContext{Role: "client", Channel: "c"})
	raw, err := remote.call("GetNativeContext", nil)
	if err != nil {
		t.Fatal(err)
	}
	var c NativeContext
	if err = json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if c.Channel != "c" {
		t.Fatal("communication must follow selected channel")
	}
}
func TestNativeAskCancellationAndDraft(t *testing.T) {
	a, remote := editorFixture(t)
	draft := map[string]any{"window": map[string]string{"group": "modbus", "version": "TCP版"}}
	if _, err := remote.call("SaveNativeDraft", rawArgs(draft)); err != nil {
		t.Fatal(err)
	}
	if len(a.native.drafts["client"]) == 0 {
		t.Fatal("draft was not preserved")
	}
	if _, err := remote.call("ResolveNativeAsk", rawArgs(nativeResult{Choice: "ok"})); err == nil {
		t.Fatal("non-confirm window resolved a decision")
	}
	session := a.native.sessions[remote.session]
	session.context.Kind = "confirm-dialog"
	session.context.Ask = json.RawMessage(`{"choices":[["cancel","取消"],["ok","确认"]]}`)
	if _, err := remote.call("ResolveNativeAsk", rawArgs(nativeResult{Choice: "unknown"})); err == nil {
		t.Fatal("unknown choice accepted")
	}
	if _, err := remote.call("ResolveNativeAsk", rawArgs(nativeResult{Choice: "ok", Value: "name"})); err != nil {
		t.Fatal(err)
	}
	if result := <-session.result; result.Choice != "ok" || result.Value != "name" {
		t.Fatal(result)
	}
	// Closing a parent releases pending decisions rather than leaving IPC calls stuck.
	a.native.close()
	if result := <-session.result; result.Choice != "cancel" {
		t.Fatal(result)
	}
}

func TestNativeControlRPCCompletesAgainstSharedServer(t *testing.T) {
	a, remote := editorFixture(t)
	clientID, err := a.engine.Demo("iec104")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.engine.StopAll)
	var client, server core.Channel
	for _, c := range a.engine.Snapshot().Project.Channels {
		if c.ID == clientID {
			client = c
		} else {
			server = c
		}
	}
	if err = a.engine.SetDevicePoints(server.ID, server.Devices[0].ID, []core.Point{{Kind: "yk", Address: 5000, Value: 0}}); err != nil {
		t.Fatal(err)
	}
	request := core.Control104Request{Type: 45, Address: 5000, Value: 1, Operation: "select", TimeoutMS: 1000}
	var raw json.RawMessage
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, err = remote.call("Command104Device", rawArgs(client.ID, client.Devices[0].ID, request))
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	var result core.Control104Result
	if err = json.Unmarshal(raw, &result); err != nil || result.Stage != "selected" {
		t.Fatalf("RPC select: %s %v", raw, err)
	}
	request.Operation = "execute"
	raw, err = remote.call("Command104Device", rawArgs(client.ID, client.Devices[0].ID, request))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &result); err != nil || result.Stage != "completed" {
		t.Fatalf("RPC execute: %s %v", raw, err)
	}
	device, err := a.engine.Device(server.ID, server.Devices[0].ID)
	if err != nil || device.Points[0].Value != 1 {
		t.Fatalf("RPC did not update shared server: %+v %v", device, err)
	}
}

func TestNativeAutomaticJobPersistsAcrossWindowReopenAndStopsViaIPC(t *testing.T) {
	a, remote := editorFixture(t)
	clientID, err := a.engine.Demo("iec104")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.engine.StopAll)
	var client, server core.Channel
	for _, c := range a.engine.Snapshot().Project.Channels {
		if c.ID == clientID {
			client = c
		} else {
			server = c
		}
	}
	points := []core.Point{{Kind: "yk", Address: 100, Name: "control"}}
	for _, c := range []core.Channel{client, server} {
		if err = a.engine.SetDevicePoints(c.ID, c.Devices[0].ID, points); err != nil {
			t.Fatal(err)
		}
	}
	args := []any{client.ID, client.Devices[0].ID, "yk"}
	raw, err := remote.call("PrepareAutoControl", rawArgs(append(args, core.AutoControlSeed{})...))
	if err != nil {
		t.Fatal(err)
	}
	var view core.AutoControlSnapshot
	if err = json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	options := view.Options
	options.IntervalMS = 1000
	options.TimeoutMS = 1000
	if _, err = remote.call("UpdateAutoOptions", rawArgs(append(args, options)...)); err != nil {
		t.Fatal(err)
	}
	if _, err = remote.call("SaveAutoControlDraft", rawArgs(args...)); err != nil {
		t.Fatal(err)
	}
	if err = a.engine.Start(client.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, err = remote.call("StartAutoControl", rawArgs(args...))
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline) {
		raw, err = remote.call("GetAutoControl", rawArgs(args...))
		if err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(raw, &view)
		if view.Success == 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if view.Success != 1 {
		t.Fatal("first automatic operation did not complete")
	}
	originalID := view.ID
	a.native.mu.Lock()
	delete(a.native.sessions, remote.session)
	a.native.sessions["reopened"] = &nativeSession{context: NativeContext{Kind: "auto-yk-dialog", Channel: client.ID, Device: client.Devices[0].ID, Role: "client"}, result: make(chan nativeResult, 1)}
	a.native.mu.Unlock()
	reopened := &nativeRemote{address: remote.address, token: remote.token, session: "reopened"}
	if _, err = reopened.call("GetNativeContext", nil); err != nil {
		t.Fatal(err)
	}
	raw, err = reopened.call("GetAutoControl", rawArgs(args...))
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &view)
	if view.ID != originalID || view.Status != "running" || view.Success != 1 {
		t.Fatalf("window lifecycle reset worker: %+v", view)
	}
	if _, err = reopened.call("StartAutoControl", rawArgs(args...)); err == nil {
		t.Fatal("reopening created a duplicate worker")
	}
	if _, err = reopened.call("StopAutoControl", rawArgs(args...)); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		raw, err = reopened.call("GetAutoControl", rawArgs(args...))
		if err != nil {
			t.Fatal(err)
		}
		_ = json.Unmarshal(raw, &view)
		if view.Status == "stopped" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	d, _ := a.engine.Device(server.ID, server.Devices[0].ID)
	if view.Status != "stopped" || d.Points[0].Value != 1 {
		t.Fatalf("stop did not preserve the last completed operation: %+v %+v", view, d)
	}
}

// Exercise the real child-to-parent bridge, not a JavaScript mock that accepts
// every method name. The manager never needs arbitrary window-opening access.
func TestNativeManagerInlineEditsReachParentWithoutOpeningEditor(t *testing.T) {
	a, remote := editorFixture(t)
	id, err := a.engine.UpsertChannel(core.Channel{Name: "manager", Protocol: "raw", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	a.native.mu.Lock()
	a.native.sessions[remote.session].context = NativeContext{Kind: "manager-dialog", Role: "server", Channel: id}
	a.native.mu.Unlock()
	if err = a.engine.Start(id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.engine.StopAll)
	if _, err = remote.call("RenameChannel", rawArgs(id, "renamed")); err != nil {
		t.Fatal(err)
	}
	config := a.engine.Snapshot().Project.Channels[0]
	config.Address = "192.0.2.99:2425"
	if _, err = remote.call("UpdateTransport", rawArgs(config)); err != nil {
		t.Fatal(err)
	}
	got := a.engine.Snapshot()
	if got.Project.Channels[0].Name != "renamed" || got.Project.Channels[0].Address != config.Address || got.States[0].Status != "stopped" {
		t.Fatalf("inline changes lost: %+v", got)
	}
	if _, err = remote.call("OpenNativeWindow", rawArgs(NativeContext{Kind: "channel-dialog", Channel: id})); err == nil {
		t.Fatal("manager should not need generic window-opening access")
	}
}
