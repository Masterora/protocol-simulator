package main

import (
	"context"
	"errors"
	"fmt"
	"gytest-go/internal/core"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDirtyApplicationClosesWithoutConfirmationAndStopsChannels(t *testing.T) {
	a := NewApp()
	id, err := a.engine.UpsertChannel(core.Channel{Name: "waiting", Protocol: "raw", Role: "client", Transport: "tcp-client", Address: "192.0.2.1:2425", TimeoutMS: 30000})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.engine.Start(id); err != nil {
		t.Fatal(err)
	}
	defer a.engine.StopAll()
	if !a.engine.Snapshot().Dirty {
		t.Fatal("fixture must be unsaved")
	}
	if a.beforeClose(context.Background()) {
		t.Fatal("main close was intercepted")
	}
	a.shutdown(context.Background())
	for _, s := range a.engine.Snapshot().States {
		if s.Status != "stopped" {
			t.Fatal("shutdown left an active channel")
		}
	}
}

func TestProjectSaveFilename(t *testing.T) {
	for _, item := range []struct{ name, want string }{
		{"工程1", "工程1.json"},
		{"现场联调", "现场联调.json"},
		{"工程2.json", "工程2.json"},
		{"现场/测试:1", "现场_测试_1.json"},
		{"", "工程1.json"},
	} {
		if got := projectSaveFilename(item.name); got != item.want {
			t.Errorf("%q: got %q want %q", item.name, got, item.want)
		}
	}
}

func TestSaveExistingProjectKeepsItsChosenPath(t *testing.T) {
	a := NewApp()
	if err := a.engine.EnsureWorkspaceChannel("client"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "custom-name.json")
	if err := a.engine.Save(path); err != nil {
		t.Fatal(err)
	}
	id := a.engine.Snapshot().Project.Channels[0].ID
	if err := a.engine.RenameChannel(id, "updated channel"); err != nil {
		t.Fatal(err)
	}
	got, err := a.SaveProject(false)
	if err != nil || got != path {
		t.Fatalf("save changed existing path: %q %v", got, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "updated channel") {
		t.Fatalf("existing file was not updated: %v", err)
	}
}

func TestAutomaticExampleLoadsWithoutStartingConnections(t *testing.T) {
	e := core.NewEngine()
	defer e.StopAll()
	if err := e.Load("examples/automatic-loopback.json"); err != nil {
		t.Fatal(err)
	}
	snapshot := e.Snapshot()
	if len(snapshot.Project.Channels) != 2 {
		t.Fatal("invalid example channels")
	}
	for _, state := range snapshot.States {
		if state.Status != "stopped" {
			t.Fatal("loading the example started a connection")
		}
	}
	for _, channel := range snapshot.Project.Channels {
		if channel.Role == "client" {
			v, err := e.PrepareAutoControl(channel.ID, channel.Devices[0].ID, "yk", core.AutoControlSeed{})
			if err != nil || len(v.Rows) != 2 || v.Rows[0].FeedbackName == "未关联" {
				t.Fatalf("example does not demonstrate the original automatic table: %+v %v", v, err)
			}
		}
	}
}

func TestApplicationUpdateDecisions(t *testing.T) {
	cases := []struct {
		name, current, release, repo, want, err string
		status, repoStatus                      int
	}{
		{name: "new patch", current: "1.0.0", release: `{"tag_name":"v1.0.1"}`, want: "available"},
		{name: "numeric minor", current: "1.2.9", release: `{"tag_name":"1.10.0"}`, want: "available"},
		{name: "new major", current: "1.99.99", release: `{"tag_name":"2.0.0"}`, want: "available"},
		{name: "equal", current: "1.0.0", release: `{"tag_name":"v1.0.0"}`, want: "current"},
		{name: "older", current: "2.0.0", release: `{"tag_name":"1.99.99"}`, want: "current"},
		{name: "build metadata", current: "1.0.0+local", release: `{"tag_name":"v1.0.0+release"}`, want: "current"},
		{name: "no release", current: "1.0.0", status: 404, repo: `{"full_name":"Masterora/protocol-simulator","private":false}`, want: "unpublished"},
		{name: "missing repository", current: "1.0.0", status: 404, repoStatus: 404, err: "无法访问更新仓库"},
		{name: "wrong repository", current: "1.0.0", status: 404, repo: `{"full_name":"other/project"}`, err: "无法访问更新仓库"},
		{name: "private repository", current: "1.0.0", status: 404, repo: `{"full_name":"Masterora/protocol-simulator","private":true}`, err: "无法访问更新仓库"},
		{name: "rate limit", current: "1.0.0", status: 403, err: "请求过于频繁"},
		{name: "too many requests", current: "1.0.0", status: 429, err: "请求过于频繁"},
		{name: "service error", current: "1.0.0", status: 503, err: "HTTP 503"},
		{name: "invalid JSON", current: "1.0.0", release: `{`, err: "更新信息无法读取"},
		{name: "missing tag", current: "1.0.0", release: `{}`, err: "更新版本号无效"},
		{name: "prerelease", current: "1.0.0", release: `{"tag_name":"v2.0.0","prerelease":true}`, err: "正式发布版本"},
		{name: "draft", current: "1.0.0", release: `{"tag_name":"v2.0.0","draft":true}`, err: "正式发布版本"},
		{name: "malformed version", current: "1.0.0", release: `{"tag_name":"next"}`, err: "更新版本号无效"},
		{name: "overflow", current: "1.0.0", release: `{"tag_name":"18446744073709551616.0.0"}`, err: "更新版本号无效"},
		{name: "invalid local", current: "", err: "当前应用版本号无效"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("User-Agent") != "protocol-simulator/"+tt.current || r.Header.Get("Authorization") != "" {
					t.Errorf("unexpected request headers or method")
				}
				status, body := tt.status, tt.release
				if r.URL.Path == "/repo" {
					status, body = tt.repoStatus, tt.repo
				} else if r.URL.Path != "/repo/releases/latest" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				if status == 0 {
					status = 200
				}
				w.WriteHeader(status)
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			result, err := checkApplicationUpdate(context.Background(), server.Client(), server.URL+"/repo", tt.current)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("got %+v, %v; want error %q", result, err, tt.err)
				}
			} else if err != nil || result.Status != tt.want || result.CurrentVersion != tt.current {
				t.Fatalf("got %+v, %v; want %s", result, err, tt.want)
			}
			wantRequests := 1
			if tt.status == 404 {
				wantRequests = 2
			}
			if tt.name == "invalid local" {
				wantRequests = 0
			}
			if requests != wantRequests {
				t.Fatalf("requests=%d, want %d", requests, wantRequests)
			}
			if tt.want == "available" || tt.want == "current" {
				if result.LatestVersion == "" {
					t.Fatal("latest version missing")
				}
			}
		})
	}
}

func TestApplicationUpdateCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := checkApplicationUpdate(ctx, server.Client(), server.URL, "1.0.0")
	if err == nil || !strings.Contains(err.Error(), "检查网络") {
		t.Fatalf("cancellation returned %v", err)
	}
}
func TestApplicationUpdateVersionAndWindow(t *testing.T) {
	if _, err := versionNumbers(applicationVersion()); err != nil {
		t.Fatalf("packaged application version invalid: %v", err)
	}
	for _, v := range []string{"1", "1.0", "1.0.0-beta.1", "1.00.0", "-1.0.0", "1.0.0/other", "1.0.0+"} {
		if _, err := versionNumbers(v); err == nil {
			t.Errorf("accepted invalid stable version %q", v)
		}
	}
	if err := (&App{}).OpenUpdatePage(); err == nil {
		t.Fatal("opening browser without main window should fail")
	}
}

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
