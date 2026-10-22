package main

import (
	"crypto/sha256"
	"os"
	"os/exec"
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
	for _, kind := range []string{"channel-dialog", "device-dialog", "manager-dialog", "point-dialog", "confirm-dialog", "control-dialog", "auto-yk-dialog", "auto-yt-dialog", "auto-bulk-dialog", "auto-data-dialog", "events-dialog"} {
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
		if kind == "auto-data-dialog" {
			serverID, e := a.engine.UpsertChannel(core.Channel{Name: "data server", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Unit: 1, CommonAddress: 1, Points: []core.Point{{Kind: "yc", Address: 10}, {Kind: "yx", Address: 1}}})
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
		if kind == "control-dialog" {
			c.Point = "yk"
			c.Control = &core.Control104Request{Type: 45, Address: 24577, Value: 1}
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
		if layout.ViewportWidth < 180 || layout.ViewportHeight < 100 || layout.Controls == 0 || layout.DialogWidth < 150 {
			t.Fatalf("%s native editor has empty or invalid layout: %+v", kind, layout)
		}
		if layout.DocumentWidth > layout.ViewportWidth+2 || layout.DialogWidth > layout.ViewportWidth+2 {
			t.Fatalf("%s native editor overflows horizontally: %+v", kind, layout)
		}
		t.Logf("%s: visible layout verified %+v", kind, layout)
		t.Logf("%s: native process and frontend bridge ready", kind)
	}
}
