package observe

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gytest-go/internal/core"
)

func runtimeDirectory(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix-only local observation")
	}
	dir, err := os.MkdirTemp("", "prs-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}
func TestReadOnlyObserverBoundsPagesHistoryAndLifecycle(t *testing.T) {
	dir := runtimeDirectory(t)
	engine := core.NewEngine()
	var points []core.Point
	for i := 0; i < 700; i++ {
		points = append(points, core.Point{Address: uint32(i), Kind: "holding", Name: fmt.Sprintf("point-%d", i), Value: 42})
	}
	channel := core.Channel{ID: "c", Name: "test", Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:15020", Unit: 1, Points: points}
	if _, err := engine.UpsertChannel(channel); err != nil {
		t.Fatal(err)
	}
	before := engine.Snapshot()
	server, err := Start(engine, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	info, err := os.Stat(server.Socket())
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal(info, err)
	}
	discovered, err := Discover(dir)
	if err != nil || len(discovered) != 1 {
		t.Fatal(discovered, err)
	}
	result, err := Fetch(context.Background(), server.Socket(), "/v1/view?channel=c&device=device-1&offset=100")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.View.Points) != 100 || result.View.PointTotal != 700 || result.View.Points[0].Address != 100 {
		t.Fatal(result.View)
	}
	result, err = Fetch(context.Background(), server.Socket(), "/v1/view?channel=c&device=device-1&filter=point-699")
	if err != nil || len(result.View.Points) != 1 || result.View.Points[0].Address != 699 {
		t.Fatal(result, err)
	}
	for i := 0; i < 1000; i++ {
		server.Log("INFO", "event", i)
	}
	result, err = Fetch(context.Background(), server.Socket(), "/v1/view?logAfter=1")
	if err != nil || len(result.Logs) > 100 || result.LogSequence != 1000 || result.LogMissed == 0 {
		t.Fatal(result, err)
	}
	conn, err := net.Dial("unix", server.Socket())
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(conn, "POST /v1/view HTTP/1.1\r\nHost: unix\r\nContent-Length: 0\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil || response.StatusCode != 405 {
		t.Fatal(response, err)
	}
	response.Body.Close()
	conn.Close()
	server.Close()
	if _, err = os.Stat(server.Socket()); !os.IsNotExist(err) {
		t.Fatal("observer socket not removed", err)
	}
	after := engine.Snapshot()
	if after.Project.Name != before.Project.Name || len(after.Project.Channels) != len(before.Project.Channels) || after.Project.Channels[0].Devices[0].Points[0].Value != 42 || after.Dirty != before.Dirty {
		t.Fatal("observer mutated engine")
	}
	if _, err = engine.UpsertChannel(core.Channel{ID: "other", Name: "other", Protocol: "raw", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:15021"}); err != nil {
		t.Fatal("closing observer affected engine", err)
	}
}
func TestObserverRejectsUnsafeDirectoriesAndPreservesForeignFiles(t *testing.T) {
	base := runtimeDirectory(t)
	unsafe := filepath.Join(base, "unsafe")
	if err := os.Mkdir(unsafe, 0777); err != nil {
		t.Fatal(err)
	}
	os.Chmod(unsafe, 0777)
	if server, err := Start(core.NewEngine(), unsafe); err == nil {
		server.Close()
		t.Fatal("unsafe directory accepted")
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(base, link); err != nil {
		t.Fatal(err)
	}
	if server, err := Start(core.NewEngine(), link); err == nil {
		server.Close()
		t.Fatal("symlink directory accepted")
	}
	foreign := filepath.Join(base, "foreign.sock")
	os.WriteFile(foreign, []byte("preserve"), 0600)
	a, err := Start(core.NewEngine(), base)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Start(core.NewEngine(), base)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	a.Close()
	if _, err = Fetch(context.Background(), b.Socket(), "/v1/info"); err != nil {
		t.Fatal("closing one instance interrupted another", err)
	}
	content, err := os.ReadFile(foreign)
	if err != nil || string(content) != "preserve" {
		t.Fatal(content, err)
	}
}
