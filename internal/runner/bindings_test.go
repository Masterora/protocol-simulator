//go:build !windows

package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"
	"time"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
	"gytest-go/internal/projectfile"
)

func TestPersistedBindingsDriveWireFeedbackAndRetainRunningRules(t *testing.T) {
	ctx := context.Background()
	path := projectFile(t, "modbus-tcp", unusedAddress(t), true)
	p, _ := core.ReadProject(path)
	p.Channels[0].Devices[0].Points = append(p.Channels[0].Devices[0].Points, core.Point{Kind: "coil", Address: 100}, core.Point{Kind: "discrete", Address: 1, Name: "合闸状态"}, core.Point{Kind: "discrete", Address: 2, Name: "分闸状态"}, core.Point{Kind: "holding", Address: 1, Name: "同编号的另一类型"})
	steady := p.Channels[0]
	steady.ID = "steady"
	steady.Address = unusedAddress(t)
	p.Channels = append(p.Channels, steady)
	writeProject(t, path, p)
	dir, err := os.MkdirTemp("", "prsbind-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	out, _ := startRunner(t, path, "--runtime-dir", dir, "--start-data-rules=true", "--watch=false")
	waitLog(t, out, `"msg":"running"`, 1)
	socket, _, err := findProjectSocket(ctx, dir, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	conn := connectEventually(t, p.Channels[0].Address)
	defer conn.Close()
	other := connectEventually(t, steady.Address)
	defer other.Close()
	scope := "/v1/view?channel=server&device=device&kind=coil&order=address"
	var before observe.Info
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		before, err = observe.Fetch(ctx, socket, scope)
		if err == nil && before.View.Rules != nil && len(before.View.Rules.Cycles) > 0 && before.View.Rules.Cycles[0] >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if before.View.Rules == nil || len(before.View.Rules.Cycles) == 0 || before.View.Rules.Cycles[0] < 2 {
		t.Fatal("rule did not run")
	}
	source := core.PointKey{Kind: "coil", Address: 100}
	req := projectfile.Request{Resource: "point", Action: "read", Channel: "server", Device: "device", Point: &source}
	view, _, err := observe.Configure(ctx, socket, req)
	if err != nil || len(view.BindingTargets) != 2 {
		t.Fatal(view, err)
	}
	req.Action, req.Revision = "edit", view.Revision
	req.Fields = projectfile.Fields(map[string]any{"name": "edited with binding", "mappings": []core.ControlMapping{{Source: source, Target: core.PointKey{Kind: "discrete", Address: 1}}, {Source: source, Target: core.PointKey{Kind: "discrete", Address: 2}, Invert: true}}})
	_, result, err := observe.Configure(ctx, socket, req)
	if err != nil || !result.Saved || !result.Applied || len(result.StoppedChannels) > 0 {
		t.Fatal(result, err)
	}
	after, err := observe.Fetch(ctx, socket, scope)
	if err != nil || len(after.View.PointBindings["coil:100"]) != 2 || after.View.Rules.Cycles[0] < before.View.Rules.Cycles[0] || !reflect.DeepEqual(after.View.Rules.RuleStates, before.View.Rules.RuleStates) {
		t.Fatal("rule reset or bindings missing", after, err)
	}
	if !reflect.DeepEqual(after.View.BindingTargetNames, map[string]string{"discrete:1": "合闸状态", "discrete:2": "分闸状态"}) {
		t.Fatal("binding target names from the wrong point or scope", after.View.BindingTargetNames)
	}
	exchange := func(raw []byte) []byte {
		t.Helper()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := conn.Write(raw); err != nil {
			t.Fatal(err)
		}
		h := make([]byte, 7)
		if _, err := io.ReadFull(conn, h); err != nil {
			t.Fatal(err)
		}
		b := make([]byte, int(h[4])<<8|int(h[5])-1)
		if _, err := io.ReadFull(conn, b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	if response := exchange([]byte{0, 1, 0, 0, 0, 6, 1, 5, 0, 100, 255, 0}); !bytes.Equal(response, []byte{5, 0, 100, 255, 0}) {
		t.Fatal(response)
	}
	if response := exchange([]byte{0, 2, 0, 0, 0, 6, 1, 2, 0, 1, 0, 2}); !bytes.Equal(response, []byte{2, 1, 1}) {
		t.Fatal("binding wire feedback", response)
	}
	if readHolding(t, other) == 0 {
		t.Fatal("unrelated channel stopped")
	}
	saved, _ := os.ReadFile(path)
	if _, _, err = observe.Configure(ctx, socket, req); !errors.Is(err, projectfile.ErrConflict) {
		t.Fatal("stale binding saved", err)
	}
	req.Revision = result.Revision
	req.Fields = projectfile.Fields(map[string]any{"mappings": []core.ControlMapping{{Source: source, Target: core.PointKey{Kind: "discrete", Address: 999}}}})
	if _, _, err = observe.Configure(ctx, socket, req); err == nil {
		t.Fatal("invalid target saved")
	}
	current, _ := os.ReadFile(path)
	if !bytes.Equal(saved, current) {
		t.Fatal("rejected save changed file")
	}
	req.Fields = projectfile.Fields(map[string]any{"mappings": []core.ControlMapping{}})
	if _, _, err = observe.Configure(ctx, socket, req); err != nil {
		t.Fatal(err)
	}
	if response := exchange([]byte{0, 3, 0, 0, 0, 6, 1, 5, 0, 100, 0, 0}); response[0] != 5 {
		t.Fatal(response)
	}
	if response := exchange([]byte{0, 4, 0, 0, 0, 6, 1, 2, 0, 1, 0, 2}); !bytes.Equal(response, []byte{2, 1, 1}) {
		t.Fatal("cleared binding still affected target", response)
	}
}
