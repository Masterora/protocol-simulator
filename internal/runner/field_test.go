package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
)

func TestFieldCommandsUseRunningProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix observation socket")
	}
	for _, protocol := range []string{"iec104", "modbus-tcp"} {
		t.Run(protocol, func(t *testing.T) {
			address := unusedAddress(t)
			path := projectFile(t, protocol, address, true)
			p, err := core.ReadProject(path)
			if err != nil {
				t.Fatal(err)
			}
			client := core.Channel{ID: "client", Name: "Client", Protocol: protocol, Role: "client", Transport: "tcp-client", Address: address, Devices: []core.Device{{ID: "remote", Name: "Remote", Unit: 1, CommonAddress: p.Channels[0].Devices[0].CommonAddress}}}
			p.Channels = append(p.Channels, client)
			engine := core.NewEngine()
			if err = engine.ReplaceProject(p, path); err != nil {
				t.Fatal(err)
			}
			defer engine.StopAll()
			for _, c := range p.Channels {
				if err := engine.Start(c.ID); err != nil {
					t.Fatal(err)
				}
			}
			dir, err := os.MkdirTemp("", "prs-field-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			observer, err := observe.Start(engine, dir)
			if err != nil {
				t.Fatal(err)
			}
			defer observer.Close()
			observer.Log("INFO", "field check")
			waitFor := func(ready func() bool) {
				t.Helper()
				deadline := time.Now().Add(4 * time.Second)
				for !ready() {
					if time.Now().After(deadline) {
						t.Fatal("loopback operation did not complete")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			waitFor(func() bool { return engine.Inspect(core.InspectRequest{}).Channels[1].State.ReadyPeers > 0 })
			run := func(args ...string) string {
				t.Helper()
				var out, stderr bytes.Buffer
				if code := Execute(context.Background(), append(args, "--runtime-dir", dir), &out, &stderr, "test"); code != 0 {
					t.Fatal(args, stderr.String())
				}
				return out.String()
			}
			common := []string{"--pid", strconv.Itoa(os.Getpid()), "--channel", "client", "--device", "remote"}
			if protocol == "iec104" {
				run(append([]string{"master", "interrogate"}, common...)...)
			} else {
				run(append(append([]string{"master", "read"}, common...), "--function", "3", "--points", "0")...)
			}
			waitFor(func() bool {
				d, _ := engine.Device("client", "remote")
				return len(d.Points) > 0 && d.Points[0].Value == 42
			})
			if protocol == "modbus-tcp" {
				run(append(append([]string{"master", "write"}, common...), "--function", "6", "--points", "0", "--value", "63")...)
				d, _ := engine.Device("server", "device")
				if d.Points[0].Value != 63 {
					t.Fatal(d.Points)
				}
			}
			var projects []inspectedProject
			if err := json.Unmarshal([]byte(run("inspect")), &projects); err != nil || len(projects) != 1 || len(projects[0].Devices) != 2 || len(projects[0].Info.Logs) == 0 {
				t.Fatalf("inspect: %+v %v", projects, err)
			}
		})
	}
}
