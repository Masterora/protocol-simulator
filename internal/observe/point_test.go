package observe

import (
	"context"
	"gytest-go/internal/core"
	"net"
	"net/http"
	"strings"
	"testing"
)

func manualObserver(t *testing.T) (*Server, *core.Engine) {
	t.Helper()
	e := core.NewEngine()
	_, err := e.UpsertChannel(core.Channel{ID: "c", Name: "c", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Unit: 1, Points: []core.Point{{Kind: "holding", Address: 0, Value: 42}}})
	if err != nil {
		t.Fatal(err)
	}
	s, err := Start(e, runtimeDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, e
}

func TestPointEndpointRejectsMalformedAndOversizedBodiesWithoutMutations(t *testing.T) {
	s, e := manualObserver(t)
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", s.Socket())
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	for _, body := range []string{"null", `{"unknown":true}`, `{} {}`, `{"channel":"c","device":"device-1","kind":"holding","address":0,"metadata":1,"action":"set","quality":256}`, strings.Repeat(" ", 5000)} {
		r, err := client.Post("http://unix/v1/point", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode < 400 || r.StatusCode >= 500 {
			t.Fatal(r.StatusCode, body[:min(80, len(body))])
		}
	}
	if e.Snapshot().Project.Channels[0].Devices[0].Points[0].Value != 42 {
		t.Fatal("malformed input mutated point")
	}
}
func TestPointEndpointAppliesAtomicPatchAndIsolatesIndependentEngines(t *testing.T) {
	s, e := manualObserver(t)
	_, other := manualObserver(t)
	info, err := Fetch(context.Background(), s.Socket(), "/v1/info")
	if err != nil || !info.PointOperations || info.View.Metadata == 0 {
		t.Fatal(info, err)
	}
	value, q := 77.0, byte(128)
	req := core.ManualPointRequest{Channel: "c", Device: "device-1", Kind: "holding", Address: 0, Metadata: info.View.Metadata, Action: "set", Value: &value, Quality: &q}
	r, err := Operate(context.Background(), s.Socket(), req)
	if err != nil || !r.Applied || r.Mode != "polled" {
		t.Fatal(r, err)
	}
	if other.Snapshot().Project.Channels[0].Devices[0].Points[0].Value != 42 {
		t.Fatal("other project changed")
	}
	if err = e.RenameChannel("c", "renamed"); err != nil {
		t.Fatal(err)
	}
	value = 99
	r, err = Operate(context.Background(), s.Socket(), req)
	if err == nil || r.Applied || !strings.Contains(err.Error(), "配置已变化") {
		t.Fatal(r, err)
	}
	if e.Snapshot().Project.Channels[0].Devices[0].Points[0].Value != 77 {
		t.Fatal("stale write applied")
	}
}
