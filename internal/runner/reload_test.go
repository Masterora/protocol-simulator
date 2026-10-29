package runner

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"gytest-go/internal/core"
)

func waitLog(t *testing.T, b *safeBuffer, fragment string, count int) {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Count(b.text(), fragment) >= count {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("missing %s (%d): %s", fragment, count, b.text())
}
func writeProject(t *testing.T, path string, p core.Project) {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".new", data, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
}
func startRunner(t *testing.T, path string, extra ...string) (*safeBuffer, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var out, stderr safeBuffer
	done := make(chan int, 1)
	args := append([]string{"run", path, "--log-format", "json"}, extra...)
	go func() { done <- Execute(ctx, args, &out, &stderr, "test") }()
	stop := func() {
		cancel()
		select {
		case code := <-done:
			if code != 0 {
				t.Error(code, stderr.text(), out.text())
			}
		case <-time.After(5 * time.Second):
			t.Error("runner failed to stop")
		}
	}
	t.Cleanup(stop)
	return &out, stop
}
func TestHotReloadKeepsOtherProjectAndUnchangedChannelConnected(t *testing.T) {
	path := projectFile(t, "modbus-tcp", unusedAddress(t), true)
	p, err := core.ReadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	a := p.Channels[0].Address
	other := p.Channels[0]
	other.ID = "unchanged"
	other.Address = unusedAddress(t)
	p.Channels = append(p.Channels, other)
	writeProject(t, path, p)
	p, _ = core.ReadProject(path)
	independent := projectFile(t, "modbus-tcp", unusedAddress(t), true)
	q, _ := core.ReadProject(independent)
	out, _ := startRunner(t, path)
	startRunner(t, independent)
	first := connectEventually(t, a)
	defer first.Close()
	steady := connectEventually(t, other.Address)
	defer steady.Close()
	separate := connectEventually(t, q.Channels[0].Address)
	defer separate.Close()
	assertSteady := func() {
		t.Helper()
		if readHolding(t, steady) != 42 || readHolding(t, separate) != 42 {
			t.Fatal("unrelated communication changed")
		}
	}
	waitLog(t, out, `"msg":"reloaded"`, 1)
	if err := os.WriteFile(path, []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	waitLog(t, out, `"msg":"reload_rejected"`, 1)
	if readHolding(t, first) != 42 {
		t.Fatal("invalid JSON disturbed active values")
	}
	assertSteady()
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	original := p.Channels[0].Address
	p.Channels[0].Address = occupied.Addr().String()
	writeProject(t, path, p)
	waitLog(t, out, `"msg":"reload_rolled_back"`, 1)
	restored := connectEventually(t, original)
	defer restored.Close()
	if readHolding(t, restored) != 42 {
		t.Fatal("rollback lost values")
	}
	assertSteady()
	p.Channels[0].Address = unusedAddress(t)
	p.Channels[0].Devices[0].Points[0].Value = 77
	writeProject(t, path, p)
	waitLog(t, out, `"msg":"reloaded"`, 2)
	updated := connectEventually(t, p.Channels[0].Address)
	defer updated.Close()
	if readHolding(t, updated) != 77 {
		t.Fatal("replacement configuration not applied")
	}
	assertSteady()
	// Removing a channel and adding a different one must not affect the survivor.
	p.Channels = p.Channels[1:]
	writeProject(t, path, p)
	waitLog(t, out, `"msg":"reloaded"`, 3)
	assertSteady()
	c := q.Channels[0]
	c.ID = "added"
	c.Address = unusedAddress(t)
	p.Channels = append(p.Channels, c)
	writeProject(t, path, p)
	waitLog(t, out, `"msg":"reloaded"`, 4)
	added := connectEventually(t, c.Address)
	defer added.Close()
	if readHolding(t, added) != 42 {
		t.Fatal("new channel not started")
	}
	assertSteady()
}
func TestHotReloadEmptyProjectCanAcquireFirstChannel(t *testing.T) {
	path := projectFile(t, "modbus-tcp", unusedAddress(t), true)
	p, _ := core.ReadProject(path)
	channels := p.Channels
	p.Channels = nil
	writeProject(t, path, p)
	out, _ := startRunner(t, path)
	waitLog(t, out, `"msg":"reloaded"`, 1)
	p.Channels = channels
	writeProject(t, path, p)
	waitLog(t, out, `"msg":"reloaded"`, 2)
	conn := connectEventually(t, channels[0].Address)
	defer conn.Close()
	if readHolding(t, conn) != 42 {
		t.Fatal("first channel not applied")
	}
	p.Channels = nil
	writeProject(t, path, p)
	waitLog(t, out, `"msg":"reloaded"`, 3)
	listener, err := net.Listen("tcp", channels[0].Address)
	if err != nil {
		t.Fatal("last channel not released", err)
	}
	listener.Close()
}

func TestHotReloadWatchCanBeDisabled(t *testing.T) {
	path := projectFile(t, "modbus-tcp", unusedAddress(t), true)
	p, _ := core.ReadProject(path)
	out, _ := startRunner(t, path, "--watch=false")
	conn := connectEventually(t, p.Channels[0].Address)
	defer conn.Close()
	os.WriteFile(path, []byte("{broken"), 0600)
	time.Sleep(time.Second)
	if readHolding(t, conn) != 42 || strings.Contains(out.text(), "reload_") {
		t.Fatal(out.text())
	}
}

func TestHotReloadExplicitDataRulesUseNewConfiguration(t *testing.T) {
	path := projectFile(t, "modbus-tcp", unusedAddress(t), true)
	p, _ := core.ReadProject(path)
	out, _ := startRunner(t, path, "--start-data-rules")
	conn := connectEventually(t, p.Channels[0].Address)
	defer conn.Close()
	waitLog(t, out, `"msg":"data_rules_started"`, 1)
	waitLog(t, out, `"msg":"reloaded"`, 1)
	cfg := p.Channels[0].Devices[0].DataTests["telemetry"]
	cfg.Rules[0].Min = 300
	cfg.Rules[0].Max = 400
	writeProject(t, path, p)
	waitLog(t, out, `"msg":"reloaded"`, 2)
	next := connectEventually(t, p.Channels[0].Address)
	defer next.Close()
	deadline := time.Now().Add(3 * time.Second)
	for readHolding(t, next) < 300 {
		if time.Now().After(deadline) {
			t.Fatal("new automatic rules did not start")
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func TestProjectWatcherFollowsDirectorySymlinkReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("atomic replacement of directory symlinks requires Unix rename semantics")
	}
	base := t.TempDir()
	path := projectFile(t, "modbus-tcp", unusedAddress(t), true)
	p, _ := core.ReadProject(path)
	for _, rev := range []string{"rev1", "rev2"} {
		if err := os.Mkdir(base+"/"+rev, 0700); err != nil {
			t.Fatal(err)
		}
		p.Name = rev
		writeProject(t, base+"/"+rev+"/project.json", p)
	}
	if err := os.Symlink(base+"/rev1", base+"/current"); err != nil {
		t.Skip("symlink unavailable", err)
	}
	w := newProjectWatcher(base + "/current/project.json")
	poll := func(want string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			p, changed, err := w.poll()
			if changed {
				if err != nil || p.Name != want {
					t.Fatal(p.Name, err)
				}
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("symlink update not observed")
	}
	poll("rev1")
	if err := os.Symlink(base+"/rev2", base+"/pending"); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(base+"/pending", base+"/current"); err != nil {
		t.Fatal(err)
	}
	poll("rev2")
}
