package runner

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"testing"
	"time"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
)

func TestStopCancelsPendingAutomaticLaunchAndRevisionProtectsRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only rule task control")
	}
	path := projectFile(t, "iec104", unusedAddress(t), true)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "prs-rule-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	out, _ := startRunner(t, path, "--runtime-dir", dir, "--start-data-rules")
	waitLog(t, out, `"msg":"running"`, 1)
	socket, _, err := findProjectSocket(context.Background(), dir, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	info, err := observe.Fetch(context.Background(), socket, "/v1/info")
	if err != nil {
		t.Fatal(err)
	}
	req := observe.RuleRequest{Channel: "server", Device: "device", Action: "stop", Revision: info.ConfigurationRevision}
	result, err := observe.ControlRules(context.Background(), socket, req)
	if err != nil || result.View.Status != "stopped" {
		t.Fatal(result, err)
	}
	conn := connectEventually(t, info.View.Channels[0].Address)
	defer conn.Close()
	if _, err = conn.Write([]byte{0x68, 4, 7, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	view, err := observe.Fetch(context.Background(), socket, "/v1/view?channel=server&device=device")
	if err != nil || view.View.Rules.Status != "stopped" {
		t.Fatal(view, err)
	}
	req.Action = "start"
	req.Revision = "stale"
	if _, err = observe.ControlRules(context.Background(), socket, req); err == nil {
		t.Fatal("stale start accepted")
	}
	req.Revision = info.ConfigurationRevision
	result, err = observe.ControlRules(context.Background(), socket, req)
	if err != nil || result.View.Status != "running" {
		t.Fatal(result, err)
	}
	time.Sleep(350 * time.Millisecond)
	if _, err = observe.Fetch(context.Background(), socket, "/v1/info"); err != nil {
		t.Fatal("manual start collided with automatic launcher", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("runtime task controls wrote configuration")
	}
}

func TestIndividualRuleRequestPreservesSiblingAndSavedConfiguration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix sockets")
	}
	path := projectFile(t, "modbus-tcp", unusedAddress(t), true)
	p, err := core.ReadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	d := &p.Channels[0].Devices[0]
	cfg := core.TelemetryConfiguration(*d)
	d.Points = append(d.Points, core.Point{Kind: "holding", Address: 1})
	second := cfg.Rules[0]
	second.Points = "1"
	cfg.Rules = append(cfg.Rules, second)
	d.DataTests = map[string]*core.AutoDataConfig{"telemetry": &cfg}
	d.AutoData = nil
	writeProject(t, path, p)
	before, _ := os.ReadFile(path)
	dir, err := os.MkdirTemp("", "prsrule-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	out, _ := startRunner(t, path, "--runtime-dir", dir, "--start-data-rules")
	waitLog(t, out, `"msg":"running"`, 1)
	socket, _, err := findProjectSocket(context.Background(), dir, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	info, err := observe.Fetch(context.Background(), socket, "/v1/info")
	if err != nil || !info.RuleItemOperations {
		t.Fatal(info, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		view, fetchErr := observe.Fetch(context.Background(), socket, "/v1/view?channel=server&device=device")
		if fetchErr == nil && view.View.Rules != nil && view.View.Rules.Status == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic rules did not start", view, fetchErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	index := 0
	req := observe.RuleRequest{Channel: "server", Device: "device", Action: "stop", Revision: info.ConfigurationRevision, Index: &index}
	result, err := observe.ControlRules(context.Background(), socket, req)
	if err != nil || result.View.RuleStates[0] != "stopped" || result.View.RuleStates[1] != "running" {
		t.Fatal(result, err)
	}
	initial := result.View.Cycles[1]
	time.Sleep(450 * time.Millisecond)
	view, err := observe.Fetch(context.Background(), socket, "/v1/view?channel=server&device=device")
	if err != nil || view.View.Rules.Cycles[1] <= initial || view.View.Rules.Cycles[0] != result.View.Cycles[0] {
		t.Fatal(view, err)
	}
	req.Action = "start"
	req.Revision = "stale"
	if _, err = observe.ControlRules(context.Background(), socket, req); err == nil {
		t.Fatal("stale indexed request accepted")
	}
	req.Revision = info.ConfigurationRevision
	result, err = observe.ControlRules(context.Background(), socket, req)
	if err != nil || result.View.RuleStates[0] != "running" || result.View.RuleStates[1] != "running" {
		t.Fatal(result, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("runtime toggle edited saved configuration")
	}
}
