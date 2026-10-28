package runner

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gytest-go/internal/core"
)

type safeBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *safeBuffer) text() string { b.mu.Lock(); defer b.mu.Unlock(); return b.Buffer.String() }
func unusedAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	l.Close()
	return address
}
func projectFile(t *testing.T, protocol, address string, canonical bool) string {
	t.Helper()
	c := core.Channel{ID: "server", Name: "server", Protocol: protocol, Role: "server", Transport: "tcp-server", Address: address, Unit: 1, Points: []core.Point{{Kind: "holding", Address: 0, Value: 42}}}
	if protocol == "iec104" {
		c.Points = []core.Point{{Kind: "yc", Address: 1, Value: 42, WireType: 13}}
	}
	c.Defaults()
	c.Devices[0].ID = "device"
	cfg := &core.AutoDataConfig{Rules: []core.AutoDataRule{{Enabled: true, Kind: c.Points[0].Kind, Points: fmt.Sprint(c.Points[0].Address), IntervalMS: 200, Mode: "increment", Min: 100, Max: 200, Step: 1}}, StopOnDisconnect: true}
	if canonical {
		c.Devices[0].DataTests = map[string]*core.AutoDataConfig{"telemetry": cfg}
	} else {
		c.Devices[0].AutoData = cfg
	}
	project := core.Project{Version: 1, Name: "CLI test", Channels: []core.Channel{c}}
	data, err := json.Marshal(project)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "project.json")
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func TestValidateDoesNotConnectOrChangeProject(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	path := projectFile(t, "modbus-tcp", listener.Addr().String(), true)
	before, _ := os.ReadFile(path)
	var out, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"validate", path, "--json"}, &out, &stderr, "test")
	after, _ := os.ReadFile(path)
	if code != 0 || !bytes.Equal(before, after) || !strings.Contains(out.String(), `"valid":true`) {
		t.Fatal(code, out.String(), stderr.String())
	}
}
func TestUsageAndPreflightFailures(t *testing.T) {
	path := projectFile(t, "modbus-tcp", unusedAddress(t), true)
	cases := [][]string{{"run", path, "--project", path}, {"wat"}, {"run"}, {"run", path, "--channels", "missing"}, {"run", path, "--channels", "server,server"}, {"run", path, "--log-format", "bad"}, {"run", path, "--frames", path}, {"run", path, "--frames-files", "0"}, {"run", path, "--channels", "server,missing"}}
	for _, args := range cases {
		var out, stderr bytes.Buffer
		if code := Execute(context.Background(), args, &out, &stderr, "test"); code != 2 {
			t.Fatal(args, code, stderr.String())
		}
	}
	var out, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"version"}, &out, &stderr, "13.27.4095"); code != 0 || out.String() != "prs 13.27.4095\n" {
		t.Fatal(code, out.String())
	}
}
func connectEventually(t *testing.T, address string) net.Conn {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			return conn
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("listener did not start", address)
	return nil
}
func readHolding(t *testing.T, conn net.Conn) uint16 {
	t.Helper()
	conn.SetDeadline(time.Now().Add(time.Second))
	if _, err := conn.Write([]byte{0, 42, 0, 0, 0, 6, 1, 3, 0, 0, 0, 1}); err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 7)
	if _, err := io.ReadFull(conn, header); err != nil {
		t.Fatal(err)
	}
	body := make([]byte, int(binary.BigEndian.Uint16(header[4:6]))-1)
	if _, err := io.ReadFull(conn, body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 4 || body[0] != 3 || body[1] != 2 {
		t.Fatal(body)
	}
	return binary.BigEndian.Uint16(body[2:])
}
func TestRunModbusExplicitRulesAndGracefulRelease(t *testing.T) {
	for _, startData := range []bool{false, true} {
		t.Run(fmt.Sprint(startData), func(t *testing.T) {
			address := unusedAddress(t)
			path := projectFile(t, "modbus-tcp", address, true)
			before, _ := os.ReadFile(path)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var out, stderr safeBuffer
			done := make(chan int, 1)
			args := []string{"run", path, "--log-format", "json"}
			if startData {
				args = append(args, "--start-data-rules")
			}
			go func() { done <- Execute(ctx, args, &out, &stderr, "test") }()
			conn := connectEventually(t, address)
			defer conn.Close()
			if startData {
				deadline := time.Now().Add(2 * time.Second)
				for readHolding(t, conn) == 42 {
					if time.Now().After(deadline) {
						t.Fatal("data rules did not start")
					}
					time.Sleep(50 * time.Millisecond)
				}
			} else {
				time.Sleep(450 * time.Millisecond)
				if value := readHolding(t, conn); value != 42 {
					t.Fatal("rules started implicitly", value)
				}
			}
			cancel()
			select {
			case code := <-done:
				if code != 0 {
					t.Fatal(code, stderr.text())
				}
			case <-time.After(3 * time.Second):
				t.Fatal("shutdown blocked")
			}
			l, err := net.Listen("tcp", address)
			if err != nil {
				t.Fatal("port not released", err)
			}
			l.Close()
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("project changed")
			}
			for _, line := range strings.Split(strings.TrimSpace(out.text()), "\n") {
				var row map[string]any
				if err := json.Unmarshal([]byte(line), &row); err != nil {
					t.Fatal(line, err)
				}
				if _, err := time.Parse(time.RFC3339Nano, row["time"].(string)); err != nil {
					t.Fatal(err)
				}
			}
			if !strings.Contains(out.text(), `"msg":"stopped"`) {
				t.Fatal(out.text())
			}
		})
	}
}
func TestFailedSecondChannelRollsBackFirst(t *testing.T) {
	address := unusedAddress(t)
	path := projectFile(t, "modbus-tcp", address, true)
	data, _ := os.ReadFile(path)
	var project core.Project
	json.Unmarshal(data, &project)
	second := project.Channels[0]
	second.ID = "collision"
	second.Name = "collision"
	project.Channels = append(project.Channels, second)
	data, _ = json.Marshal(project)
	os.WriteFile(path, data, 0600)
	var out, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"run", path}, &out, &stderr, "test"); code != 1 {
		t.Fatal(code, stderr.String())
	}
	l, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal("partial startup leaked listener", err)
	}
	l.Close()
}
func TestIEC104RulesWaitForActivationAndHonorDisconnectStop(t *testing.T) {
	address := unusedAddress(t)
	path := projectFile(t, "iec104", address, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var out, stderr safeBuffer
	done := make(chan int, 1)
	go func() {
		done <- Execute(ctx, []string{"run", path, "--start-data-rules", "--log-format", "json"}, &out, &stderr, "test")
	}()
	conn := connectEventually(t, address)
	conn.SetReadDeadline(time.Now().Add(350 * time.Millisecond))
	probe := make([]byte, 6)
	if _, err := conn.Read(probe); err == nil {
		t.Fatal("data sent before STARTDT", probe)
	}
	activate := func(c net.Conn) {
		c.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := c.Write([]byte{0x68, 4, 7, 0, 0, 0}); err != nil {
			t.Fatal(err)
		}
		b := make([]byte, 6)
		if _, err := io.ReadFull(c, b); err != nil || b[2] != 0x0b {
			t.Fatal(b, err)
		}
	}
	activate(conn)
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		t.Fatal("auto report missing", err)
	}
	body := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, body); err != nil || body[0]&1 != 0 {
		t.Fatal(body, err)
	}
	conn.Close()
	time.Sleep(600 * time.Millisecond)
	waitLog(t, &out, `"msg":"reloaded"`, 1)
	candidate, err := core.ReadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	candidate.Name = "renamed while stopped"
	writeProject(t, path, candidate)
	waitLog(t, &out, `"msg":"reloaded"`, 2)
	second := connectEventually(t, address)
	defer second.Close()
	activate(second)
	second.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err := second.Read(probe); err == nil {
		t.Fatal("disconnect-stopped task restarted", probe)
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatal(code, stderr.text())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown blocked")
	}
}
