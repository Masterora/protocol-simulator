package observe

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"gytest-go/internal/core"
)

func TestRuleOperationsControlOnlyTargetEngineAndRejectInvalidRequests(t *testing.T) {
	s, engine := manualObserver(t)
	_, other := manualObserver(t)
	t.Cleanup(engine.StopAll)
	_, err := engine.SaveAutoData("c", "device-1", core.AutoDataConfig{Rules: []core.AutoDataRule{{Enabled: true, Kind: "holding", Points: "0", IntervalMS: 200, Mode: "increment", Min: 42, Max: 100, Step: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.Start("c"); err != nil {
		t.Fatal(err)
	}
	info, err := Fetch(context.Background(), s.Socket(), "/v1/info")
	if err != nil || !info.RuleOperations {
		t.Fatal(info, err)
	}
	request := RuleRequest{Channel: "c", Device: "device-1"}
	control := func(action, status string) {
		t.Helper()
		request.Action = action
		result, err := ControlRules(context.Background(), s.Socket(), request)
		if err != nil || result.View == nil || result.View.Status != status {
			t.Fatal(result, err)
		}
	}
	control("start", "running")
	deadline := time.Now().Add(3 * time.Second)
	for {
		v, _ := engine.GetAutoData("c", "device-1")
		if v.Cycles[0] > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("rule did not run")
		}
		time.Sleep(10 * time.Millisecond)
	}
	control("pause", "paused")
	before, _ := engine.GetAutoData("c", "device-1")
	time.Sleep(300 * time.Millisecond)
	after, _ := engine.GetAutoData("c", "device-1")
	if after.Cycles[0] != before.Cycles[0] {
		t.Fatal("paused task kept running")
	}
	control("resume", "running")
	control("stop", "stopped")
	control("start", "running")
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", s.Socket())
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	for _, body := range []string{"null", `{"unknown":true}`, `{} {}`, `{"channel":"c","device":"device-1","action":"control"}`, strings.Repeat(" ", 5000)} {
		r, err := client.Post("http://unix/v1/rules", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 400 {
			t.Fatal(r.StatusCode, body[:min(50, len(body))])
		}
	}
	r, err := client.Get("http://unix/v1/rules")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 405 {
		t.Fatal(r.StatusCode)
	}
	v, _ := engine.GetAutoData("c", "device-1")
	if v.Status != "running" {
		t.Fatal("invalid requests changed task", v)
	}
	if other.Snapshot().Project.Channels[0].Devices[0].Points[0].Value != 42 {
		t.Fatal("independent engine changed")
	}
}
