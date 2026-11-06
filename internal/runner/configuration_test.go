//go:build !windows

package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
	"gytest-go/internal/projectfile"
)

func TestLiveConfigurationEditsPersistWithoutBreakingUnrelatedTraffic(t *testing.T) {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "prscfg-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := projectFile(t, "modbus-tcp", unusedAddress(t), true)
	p, _ := core.ReadProject(path)
	other := p.Channels[0]
	other.ID = "steady"
	other.Address = unusedAddress(t)
	p.Channels = append(p.Channels, other)
	writeProject(t, path, p)
	out, _ := startRunner(t, path, "--runtime-dir", dir, "--watch=false")
	waitLog(t, out, `"msg":"running"`, 1)
	socket, _, err := findProjectSocket(ctx, dir, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	conn := connectEventually(t, p.Channels[0].Address)
	defer conn.Close()
	steady := connectEventually(t, other.Address)
	defer steady.Close()
	edit := func(resource, action string, fields map[string]any) projectfile.Result {
		t.Helper()
		info, err := observe.Fetch(ctx, socket, "/v1/info")
		if err != nil {
			t.Fatal(err)
		}
		req := projectfile.Request{Resource: resource, Action: action, Channel: "server", Device: "device", Revision: info.ConfigurationRevision, Fields: projectfile.Fields(fields)}
		if resource == "point" && action == "edit" {
			req.Point = &core.PointKey{Kind: "holding", Address: 0}
		}
		if resource == "rule" && action == "edit" {
			i := 0
			req.Index = &i
		}
		_, result, err := observe.Configure(ctx, socket, req)
		if err != nil || !result.Applied {
			t.Fatal(result, err)
		}
		if resource != "channel" || (action != "start" && action != "stop") {
			if !result.Saved {
				t.Fatal(result)
			}
		}
		if readHolding(t, steady) != 42 {
			t.Fatal("unrelated channel disturbed")
		}
		return result
	}
	originalInfo, _ := observe.Fetch(ctx, socket, "/v1/info")
	edit("channel", "edit", map[string]any{"name": "renamed"})
	if readHolding(t, conn) != 42 {
		t.Fatal("name edit reconnected channel")
	}
	_, _, err = observe.Configure(ctx, socket, projectfile.Request{Resource: "project", Action: "edit", Revision: originalInfo.ConfigurationRevision, Fields: projectfile.Fields(map[string]any{"name": "stale"})})
	if !errors.Is(err, projectfile.ErrConflict) {
		t.Fatalf("stale writer accepted: %v", err)
	}
	edit("point", "add", map[string]any{"kind": "holding", "address": 1, "value": 17, "name": "added", "quality": 128})
	edit("device", "add", map[string]any{"name": "second", "unit": 2, "pointCount": 3})
	edit("point", "edit", map[string]any{"name": "metadata preserved", "dataType": "uint16", "byteOrder": "AB"})
	if readHolding(t, conn) != 42 {
		t.Fatal("point/device edit broke existing connection")
	}
	// Existing rules were not requested at startup. Editing/enabling one explicitly starts it.
	edit("rule", "edit", map[string]any{"enabled": true, "min": 310, "max": 320, "intervalMs": 200})
	deadline := time.Now().Add(3 * time.Second)
	var live uint16
	for time.Now().Before(deadline) {
		live = readHolding(t, conn)
		if live >= 310 && live <= 320 {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if live < 310 || live > 320 {
		t.Fatal("edited rule failed to run", live)
	}
	edit("channel", "edit", map[string]any{"name": "rule running"})
	saved, err := core.ReadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Channels[0].Devices[0].Points[0].Value != 42 {
		t.Fatal("automatic runtime value leaked into file")
	}
	if live = readHolding(t, conn); live < 310 || live > 320 {
		t.Fatal("metadata edit reset live value", live)
	}
	// A quality-only CLI save must retain both the stored value and the latest live value.
	var stdout, stderr bytes.Buffer
	code := Execute(ctx, []string{"point", "set", "--pid", strconv.Itoa(os.Getpid()), "--runtime-dir", dir, "--channel", "server", "--device", "device", "--kind", "holding", "--address", "0", "--quality", "128", "--json"}, &stdout, &stderr, "test")
	var result core.ManualPointResult
	if code != 0 || json.Unmarshal(stdout.Bytes(), &result) != nil || !result.Saved {
		t.Fatal(code, stdout.String(), stderr.String())
	}
	saved, _ = core.ReadProject(path)
	if saved.Channels[0].Devices[0].Points[0].Value != 42 || saved.Channels[0].Devices[0].Points[0].Quality != 128 {
		t.Fatal("partial patch overwrote other file fields")
	}
	if live = readHolding(t, conn); live < 310 || live > 320 {
		t.Fatal("quality-only patch reset live value", live)
	}
	// Invalid edits and a file removed underneath the process cannot report success or mutate runtime.
	before, _ := os.ReadFile(path)
	info, _ := observe.Fetch(ctx, socket, "/v1/info")
	_, r, err := observe.Configure(ctx, socket, projectfile.Request{Action: "edit", Resource: "point", Channel: "server", Device: "device", Point: &core.PointKey{Kind: "holding", Address: 0}, Revision: info.ConfigurationRevision, Fields: projectfile.Fields(map[string]any{"quality": 256})})
	after, _ := os.ReadFile(path)
	if err == nil || r.Saved || !bytes.Equal(before, after) {
		t.Fatal("invalid edit modified file", r, err)
	}
	if err = os.Rename(path, path+".saved"); err != nil {
		t.Fatal(err)
	}
	_, r, err = observe.Configure(ctx, socket, projectfile.Request{Action: "edit", Resource: "project", Revision: info.ConfigurationRevision, Fields: projectfile.Fields(map[string]any{"name": "cannot save"})})
	if err == nil || r.Saved {
		t.Fatal("failed save reported success")
	}
	if err = os.Rename(path+".saved", path); err != nil {
		t.Fatal(err)
	}
	if readHolding(t, steady) != 42 {
		t.Fatal("failed save broke other channel")
	}
	// A transport change saves first and releases just that channel until manually started.
	replacement := unusedAddress(t)
	r = edit("channel", "edit", map[string]any{"address": replacement})
	if len(r.StoppedChannels) != 1 || r.StoppedChannels[0] != "server" {
		t.Fatal(r)
	}
	listener, err := net.Listen("tcp", replacement)
	if err != nil {
		t.Fatal("changed channel started without consent", err)
	}
	listener.Close()
	edit("channel", "start", nil)
	replacementConn := connectEventually(t, replacement)
	defer replacementConn.Close()
	if value := readHolding(t, replacementConn); value < 310 || value > 320 {
		t.Fatal(value)
	}
	// Reload from disk preserves every accepted definition, including newly added resources.
	saved, err = core.ReadProject(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Channels[0].Devices) != 2 || len(saved.Channels[0].Devices[0].Points) != 2 || saved.Channels[0].Name != "rule running" {
		t.Fatal(saved)
	}
}
