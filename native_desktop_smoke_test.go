package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"gytest-go/internal/core"
)

// Opt-in integration test: starts the real desktop executable and waits for its
// WebView frontend to finish rendering and call the authenticated parent bridge.
func TestNativeWindowSmoke(t *testing.T) {
	binary := os.Getenv("GYLAB_NATIVE_SMOKE_BINARY")
	if binary == "" {
		t.Skip("requires built desktop executable")
	}
	built, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("verified_binary_sha256=%x", sha256.Sum256(built))
	a := NewApp()
	n, err := newNativeWindows(a)
	if err != nil {
		t.Fatal(err)
	}
	a.native = n
	t.Cleanup(n.close)
	id, err := a.engine.UpsertChannel(core.Channel{Name: "native smoke", Protocol: "raw", Role: "client", Transport: "tcp-client", Address: "127.0.0.1:2404"})
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"channel-dialog", "device-dialog", "manager-dialog", "point-dialog", "confirm-dialog", "control-dialog", "control-step", "control-setpoint", "auto-yk-dialog", "auto-yt-dialog", "auto-bulk-dialog", "auto-data-dialog", "events-dialog", "device-client-modbus", "device-server-iec104", "device-server-modbus", "data-test-yx-modbus", "remote-yk-iec104", "remote-yt-iec104", "remote-yk-modbus", "remote-yt-modbus", "telemetry-values-yx", "telemetry-values-yc", "auto-yk-modbus", "auto-yt-modbus"} {
		scenario := kind
		if scenario == "control-step" || scenario == "control-setpoint" {
			kind = "control-dialog"
		}
		if strings.HasPrefix(scenario, "remote-") {
			kind = "remote-config-dialog"
		}
		if strings.HasPrefix(scenario, "telemetry-values-") {
			kind = "telemetry-values-dialog"
		}
		if scenario == "auto-yk-modbus" {
			kind = "auto-yk-dialog"
		}
		if scenario == "auto-yt-modbus" {
			kind = "auto-yt-dialog"
		}
		if strings.HasPrefix(kind, "device-") && kind != "device-dialog" {
			kind = "device-dialog"
		}
		if strings.HasPrefix(scenario, "data-") {
			kind = "auto-data-dialog"
		}
		// Point editor needs a protocol device, while other editors may use RAW.
		deviceID := ""
		if kind == "point-dialog" || kind == "control-dialog" || isAutoWindow(kind) {
			devices := a.engine.Snapshot().Project.Channels[0].Devices
			if len(devices) > 0 {
				deviceID = devices[0].ID
			} else {
				deviceID, err = a.engine.UpsertDevice(id, "iec104", core.Device{Name: "device", Unit: 1, CommonAddress: 1}, false)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if isAutoWindow(kind) {
			mode := "yk"
			if kind == "auto-yt-dialog" {
				mode = "yt"
			}
			if err = a.engine.SetDevicePoints(id, deviceID, []core.Point{{Kind: "yk", Address: 100}, {Kind: "yt", Address: 101}}); err != nil {
				t.Fatal(err)
			}
			if _, err = a.engine.PrepareAutoControl(id, deviceID, mode, core.AutoControlSeed{Addresses: "101", Value: "10", ValueType: 50}); err != nil {
				t.Fatal(err)
			}
		}
		sessionID := nonce()
		c := NativeContext{Kind: kind, Role: "client", Channel: id, Device: deviceID, Edit: kind == "channel-dialog"}
		if strings.HasPrefix(scenario, "device-") && scenario != "device-dialog" {
			role, group := "client", "modbus"
			if strings.Contains(scenario, "server") {
				role = "server"
			}
			if strings.Contains(scenario, "iec104") {
				group = "iec104"
			}
			channelID, e := a.engine.UpsertChannel(core.Channel{Name: scenario, Protocol: "raw", Role: role, Transport: "tcp-client", Address: "127.0.0.1:2404"})
			if e != nil {
				t.Fatal(e)
			}
			c.Role, c.Channel = role, channelID
			c.Draft = []byte(fmt.Sprintf(`{"window":{"group":%q,"version":%q},"panels":[]}`, group, map[string]string{"iec104": "标准版", "modbus": "TCP版"}[group]))
		}
		if kind == "auto-data-dialog" {
			protocol := "iec104"
			points := []core.Point{{Kind: "yc", Address: 10}, {Kind: "yx", Address: 1}}
			if strings.HasSuffix(scenario, "modbus") {
				protocol = "modbus-tcp"
				points = []core.Point{{Kind: "holding", Address: 10}, {Kind: "discrete", Address: 1}}
			}
			if strings.HasPrefix(scenario, "data-") {
				if strings.Contains(scenario, "-yx-") {
					c.PointKind = "yx"
				} else {
					c.PointKind = "yc"
				}
			}
			if strings.HasPrefix(scenario, "data-report-") {
				c.Point = "report"
			}
			serverID, e := a.engine.UpsertChannel(core.Channel{Name: "data server", Protocol: protocol, Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Unit: 1, CommonAddress: 1, Points: points})
			if e != nil {
				t.Fatal(e)
			}
			var d core.Device
			for _, ch := range a.engine.Snapshot().Project.Channels {
				if ch.ID == serverID {
					d = ch.Devices[0]
				}
			}
			c.Role = "server"
			c.Channel = serverID
			c.Device = d.ID
		}
		if kind == "remote-config-dialog" || kind == "telemetry-values-dialog" || strings.HasPrefix(scenario, "auto-") && strings.HasSuffix(scenario, "modbus") {
			protocol, role := "iec104", "server"
			points := []core.Point{{Kind: "yx", Address: 1}, {Kind: "yc", Address: 10}, {Kind: "yk", Address: 100}, {Kind: "yt", Address: 200}}
			if strings.HasSuffix(scenario, "modbus") {
				protocol = "modbus-tcp"
				points = []core.Point{{Kind: "discrete", Tab: "yx", Address: 1}, {Kind: "holding", Tab: "yc", Address: 10}, {Kind: "coil", Tab: "yk", Address: 100}, {Kind: "holding", Tab: "yt", Address: 200}}
			}
			if strings.HasPrefix(scenario, "auto-") {
				role = "client"
			}
			transport, address := "tcp-server", "127.0.0.1:0"
			if role == "client" {
				transport, address = "tcp-client", "127.0.0.1:15020"
			}
			chID, err := a.engine.UpsertChannel(core.Channel{Name: scenario, Protocol: protocol, Role: role, Transport: transport, Address: address, Unit: 1, CommonAddress: 1, Points: points})
			if err != nil {
				t.Fatal(err)
			}
			var dev core.Device
			for _, ch := range a.engine.Snapshot().Project.Channels {
				if ch.ID == chID {
					dev = ch.Devices[0]
				}
			}
			c.Channel, c.Device, c.Role, c.Edit = chID, dev.ID, role, true
			if kind == "remote-config-dialog" {
				category := "yk"
				source, target := core.PointKey{Kind: points[2].Kind, Address: 100}, core.PointKey{Kind: points[0].Kind, Address: 1}
				if strings.Contains(scenario, "-yt-") {
					category = "yt"
					source, target = core.PointKey{Kind: points[3].Kind, Address: 200}, core.PointKey{Kind: points[1].Kind, Address: 10}
				}
				c.PointKind = category
				if err = a.engine.SaveControlSimulation(chID, dev.ID, category, core.ControlSimulation{Mode: "normal", Mappings: []core.ControlMapping{{Source: source, Target: target}}}); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "telemetry-values-dialog" {
				c.PointKind = "yx"
				if strings.HasSuffix(scenario, "yc") {
					c.PointKind = "yc"
				}
			}
			if isAutoWindow(kind) {
				mode, fc := "yk", byte(5)
				if kind == "auto-yt-dialog" {
					mode, fc = "yt", 6
				}
				if _, err = a.engine.PrepareAutoControl(chID, dev.ID, mode, core.AutoControlSeed{Addresses: "200", Value: "42", ValueType: 49, Function: fc}); err != nil {
					t.Fatal(err)
				}
			}
		}
		if kind == "control-dialog" {
			c.Point = "yk"
			c.Control = &core.Control104Request{Type: 45, Address: 24577, Value: 1}
			if scenario == "control-step" {
				c.Point = "step"
				c.Control = &core.Control104Request{Type: 47, Address: 25089, Value: 2}
			}
			if scenario == "control-setpoint" {
				c.Point = "setpoint"
				c.Control = &core.Control104Request{Type: 50, Address: 25089, Value: 0}
			}
		}
		if kind == "confirm-dialog" {
			c.Ask = []byte(`{"title":"原生窗口验证","input":"name","choices":[["cancel","取消"],["ok","确认"]]}`)
		}
		cmd := exec.Command(binary, "--gylab-native-window="+sessionID)
		cmd.Env = append(os.Environ(), "GYLAB_NATIVE_ADDRESS="+n.address, "GYLAB_NATIVE_TOKEN="+n.token)
		session := &nativeSession{context: c, cmd: cmd, result: make(chan nativeResult, 1)}
		n.mu.Lock()
		n.sessions[sessionID] = session
		n.mu.Unlock()
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		deadline := time.Now().Add(25 * time.Second)
		ready := false
		for time.Now().Before(deadline) {
			n.mu.Lock()
			ready = session.ready
			n.mu.Unlock()
			if ready {
				break
			}
			select {
			case exit := <-exited:
				t.Fatalf("%s exited before frontend readiness: %v", kind, exit)
			default:
			}
			time.Sleep(100 * time.Millisecond)
		}
		if ready && runtime.GOOS == "darwin" {
			// Inspect the running AppKit process, rather than trusting a static
			// plist: Wails resets the activation policy during application launch.
			script := fmt.Sprintf(`ObjC.import('AppKit'); $.NSRunningApplication.runningApplicationWithProcessIdentifier(%d).activationPolicy`, cmd.Process.Pid)
			policy, policyErr := exec.Command("osascript", "-l", "JavaScript", "-e", script).CombinedOutput()
			if policyErr != nil || strings.TrimSpace(string(policy)) != "1" {
				_ = cmd.Process.Kill()
				t.Fatalf("%s editor still has a Dock icon: policy=%s error=%v", scenario, policy, policyErr)
			}
			t.Logf("%s: accessory activation policy verified (no Dock icon)", scenario)
		}
		_ = cmd.Process.Kill()
		select {
		case <-exited:
		case <-time.After(3 * time.Second):
			t.Fatal("test window did not close")
		}
		n.mu.Lock()
		delete(n.sessions, sessionID)
		n.mu.Unlock()
		if !ready {
			t.Fatalf("%s native WebView did not finish rendering", kind)
		}
		layout := session.layout
		if kind == "auto-data-dialog" && (layout.DataRuleRows != 0 || !layout.DataAddSymbol) {
			t.Fatalf("%s must open without rules and expose a symbol add button: %+v", scenario, layout)
		}
		if kind == "remote-config-dialog" && c.PointKind == "yt" && layout.ViewportHeight > 440 {
			t.Fatalf("%s binding editor not compact: %+v", scenario, layout)
		}
		if kind == "control-dialog" && c.Point != "yk" {
			if layout.ViewportHeight > 440 || layout.ContentBottom > layout.ViewportHeight {
				t.Fatalf("%s is tall or clips actions: %+v", scenario, layout)
			}
		}
		if kind == "auto-yt-dialog" && layout.ViewportHeight > 300 {
			t.Fatalf("%s not compact: %+v", scenario, layout)
		}
		if layout.SaveButtonBottom > layout.ViewportHeight {
			t.Fatalf("%s save action clipped: %+v", scenario, layout)
		}
		if kind == "device-dialog" && (layout.WrappedLabels != 0 || layout.MinInputWidth < 80 || layout.ViewportWidth < 640 || layout.InputAlignmentDelta > 1) {
			t.Fatalf("%s device fields wrap or are too narrow: %+v", scenario, layout)
		}
		if kind == "device-dialog" && layout.DevicePointFields != 8 {
			t.Fatalf("%s did not retain four first-point/count pairs: %+v", scenario, layout)
		}
		if kind == "telemetry-values-dialog" && !layout.QualityCheckboxLeft {
			t.Fatalf("%s quality checkbox is not left of its label: %+v", scenario, layout)
		}
		if kind == "auto-data-dialog" || kind == "auto-yk-dialog" || kind == "auto-yt-dialog" {
			if layout.EditorSaveButtons != 1 || layout.EditorRuntimeButtons != 0 {
				t.Fatalf("%s editor must only save: %+v", scenario, layout)
			}
		}
		if layout.ViewportWidth < 180 || layout.ViewportHeight < 100 || layout.Controls == 0 || layout.DialogWidth < 150 {
			t.Fatalf("%s native editor has empty or invalid layout: %+v", kind, layout)
		}
		if layout.DocumentWidth > layout.ViewportWidth+2 || layout.DialogWidth > layout.ViewportWidth+2 {
			t.Fatalf("%s native editor overflows horizontally: %+v", kind, layout)
		}
		t.Logf("%s: visible layout verified %+v", scenario, layout)
		t.Logf("%s: native process and frontend bridge ready", scenario)
	}
}

// Closing an editor must release its WebView process while the owning engine's
// transport and automatic data worker continue. This uses a real close button.
func TestNativeEditorCloseReleasesProcess(t *testing.T) {
	binary := os.Getenv("GYLAB_NATIVE_SMOKE_BINARY")
	if binary == "" {
		t.Skip("requires built desktop executable")
	}
	built, readErr := os.ReadFile(binary)
	if readErr != nil {
		t.Fatal(readErr)
	}
	t.Logf("verified_binary_sha256=%x", sha256.Sum256(built))
	ax := os.Getenv("GYLAB_AX_HELPER")
	if ax == "" {
		t.Skip("requires accessibility close-button helper")
	}
	a := NewApp()
	n, err := newNativeWindows(a)
	if err != nil {
		t.Fatal(err)
	}
	a.native = n
	t.Cleanup(n.close)
	t.Cleanup(a.engine.StopAll)
	channelID, err := a.engine.UpsertChannel(core.Channel{Name: "resource verification", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Devices: []core.Device{{ID: "d", Name: "resource device", Unit: 1, CommonAddress: 1, Points: []core.Point{{Kind: "holding", Address: 1}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.engine.SaveDataTest(channelID, "d", "telemetry", core.AutoDataConfig{Rules: []core.AutoDataRule{{Enabled: true, Kind: "holding", Points: "1", Mode: "increment", Max: 1000, Step: 1, IntervalMS: 200}}}); err != nil {
		t.Fatal(err)
	}
	if err = a.engine.Start(channelID); err != nil {
		t.Fatal(err)
	}
	if _, err = a.engine.StartDataTest(channelID, "d", "telemetry"); err != nil {
		t.Fatal(err)
	}
	initial, _ := a.engine.GetDataTest(channelID, "d", "telemetry")
	for i := 0; i < 3; i++ {
		id := nonce()
		cmd := exec.Command(binary, "--gylab-native-window="+id)
		cmd.Env = append(os.Environ(), "GYLAB_NATIVE_ADDRESS="+n.address, "GYLAB_NATIVE_TOKEN="+n.token)
		session := &nativeSession{context: NativeContext{Kind: "auto-data-dialog", Role: "server", Channel: channelID, Device: "d", PointKind: "telemetry", Edit: true}, cmd: cmd, result: make(chan nativeResult, 1)}
		n.mu.Lock()
		n.sessions[id] = session
		n.mu.Unlock()
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		t.Cleanup(func() { _ = cmd.Process.Kill() })
		ready := false
		for end := time.Now().Add(20 * time.Second); time.Now().Before(end); {
			n.mu.Lock()
			ready = session.ready
			n.mu.Unlock()
			if ready {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if !ready {
			t.Fatal("editor did not render")
		}
		rss, _ := exec.Command("ps", "-o", "rss=", "-p", fmt.Sprint(cmd.Process.Pid)).Output()
		activation := fmt.Sprintf(`tell application "System Events" to set frontmost of first process whose unix id is %d to true`, cmd.Process.Pid)
		if out, e := exec.Command("osascript", "-e", activation).CombinedOutput(); e != nil {
			t.Fatalf("activate: %v %s", e, out)
		}
		time.Sleep(200 * time.Millisecond)
		var out []byte
		var e error
		for end := time.Now().Add(6 * time.Second); time.Now().Before(end); {
			role, label := "AXButton", "取消"
			if i == 1 {
				role, label = "AXCloseButton", ""
			}
			out, e = exec.Command(ax, fmt.Sprint(cmd.Process.Pid), role, label).CombinedOutput()
			if e == nil && strings.Contains(string(out), "press") {
				break
			}
			time.Sleep(300 * time.Millisecond)
		}
		if e != nil || !strings.Contains(string(out), "press") {
			t.Fatalf("close button unavailable: %v %s", e, out)
		}
		select {
		case e = <-exited:
			if e != nil {
				t.Fatalf("editor close failed: %v", e)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("closed editor retained its process")
		}
		n.mu.Lock()
		delete(n.sessions, id)
		remaining := len(n.sessions)
		n.mu.Unlock()
		view, e := a.engine.GetDataTest(channelID, "d", "telemetry")
		if e != nil || view.Status != "running" || view.Updates <= initial.Updates {
			t.Fatalf("editor close interrupted worker: %+v %v", view, e)
		}
		t.Logf("cycle=%d editor_rss_kib=%s close_exit=true retained_editor_processes=%d worker_updates=%d", i+1, strings.TrimSpace(string(rss)), remaining, view.Updates)
	}
}
