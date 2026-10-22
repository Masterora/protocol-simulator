package main

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"gytest-go/internal/core"
)

func TestNewProjectOpensIsolatedWorkspaceAndPreservesLiveOriginal(t *testing.T) {
	for _, role := range []string{"client", "server"} {
		t.Run(role, func(t *testing.T) {
			original := NewApp()
			t.Cleanup(original.engine.StopAll)
			id, err := original.engine.UpsertChannel(core.Channel{Name: "old", Protocol: "raw", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0"})
			if err != nil {
				t.Fatal(err)
			}
			if err = original.engine.Start(id); err != nil {
				t.Fatal(err)
			}
			address := original.engine.Snapshot().States[0].Address
			if err = original.engine.Save(filepath.Join(t.TempDir(), "original.json")); err != nil {
				t.Fatal(err)
			}
			before := original.engine.Snapshot()
			var child *App
			original.launchProject = func(c projectWindowConfig) error {
				child = NewApp()
				t.Cleanup(child.engine.StopAll)
				return child.initializeProjectWindow(c)
			}
			if err = original.NewProject("工程2", role); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(original.engine.Snapshot(), before) {
				t.Fatal("new project mutated original engine")
			}
			got := child.engine.Snapshot()
			if got.Project.Name != "工程2" || got.Path != "" || got.Dirty || len(got.Project.Channels) != 1 || got.States[0].Status != "stopped" {
				t.Fatalf("unexpected new workspace: %+v", got)
			}
			expected := core.NewEngine()
			if err = expected.EnsureWorkspaceChannel(role); err != nil {
				t.Fatal(err)
			}
			channel, want := got.Project.Channels[0], expected.Snapshot().Project.Channels[0]
			channel.ID, want.ID = "", ""
			if !reflect.DeepEqual(channel, want) {
				t.Fatal("child defaults differ from startup")
			}
			child.shutdown(context.Background())
			conn, err := net.DialTimeout("tcp", address, time.Second)
			if err != nil {
				t.Fatalf("closing child stopped original listener: %v", err)
			}
			conn.Close()
		})
	}
}

func TestProjectWindowLifecycleDoesNotOwnOtherProjects(t *testing.T) {
	original, child := NewApp(), NewApp()
	t.Cleanup(child.engine.StopAll)
	if err := child.initializeProjectWindow(projectWindowConfig{Name: "工程2", Role: "server"}); err != nil {
		t.Fatal(err)
	}
	config := child.engine.Snapshot().Project.Channels[0]
	config.Address = "127.0.0.1:0"
	if err := child.engine.UpdateTransport(config); err != nil {
		t.Fatal(err)
	}
	if err := child.engine.Start(config.ID); err != nil {
		t.Fatal(err)
	}
	address := child.engine.Snapshot().States[0].Address
	original.shutdown(context.Background())
	conn, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatalf("closing original affected child: %v", err)
	}
	conn.Close()
}

func TestNewProjectInvalidModeAndLaunchFailurePreserveOriginal(t *testing.T) {
	a := NewApp()
	if err := a.engine.EnsureWorkspaceChannel("client"); err != nil {
		t.Fatal(err)
	}
	before := a.engine.Snapshot()
	a.launchProject = func(projectWindowConfig) error { return errors.New("cannot start process") }
	for _, role := range []string{"invalid", "client"} {
		if err := a.NewProject("工程2", role); err == nil {
			t.Fatal("failed launch accepted")
		}
		if !reflect.DeepEqual(a.engine.Snapshot(), before) {
			t.Fatal("failed launch replaced existing data")
		}
	}
}

func TestProjectCommandUsesIndependentMainAndRoundTripsNames(t *testing.T) {
	config := projectWindowConfig{Name: "工程2 中文 '$() \"quoted\"", Role: "server"}
	cmd, err := projectCommand(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmd.Args) != 2 {
		t.Fatalf("unexpected process args: %v", cmd.Args)
	}
	got, err := projectConfigFromArgs(cmd.Args[1:])
	if err != nil || got == nil || *got != config {
		t.Fatalf("startup arguments changed: %+v %v", got, err)
	}
	app := NewApp()
	if err = app.initializeProjectWindow(*got); err != nil {
		t.Fatal(err)
	}
	context, err := app.GetNativeContext()
	if err != nil || context.Kind != "" || context.Role != "server" || app.remote != nil {
		t.Fatalf("not an independent server workspace: %+v %v", context, err)
	}
	for _, args := range [][]string{{projectWindowArg + "bad-base64"}, {cmd.Args[1], cmd.Args[1]}} {
		if _, err = projectConfigFromArgs(args); err == nil {
			t.Fatal("invalid startup args accepted")
		}
	}
}
