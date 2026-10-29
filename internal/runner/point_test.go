package runner

import (
	"bytes"
	"context"
	"gytest-go/internal/core"
	"gytest-go/internal/observe"
	"os"
	"runtime"
	"strconv"
	"testing"
)

func TestPointCommandTargetsRunningProcessAndLeavesSavedConfigurationUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("point commands attach through Unix-only observation sockets")
	}
	path := projectFile(t, "modbus-tcp", unusedAddress(t), true)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	project, err := core.ReadProject(path)
	if err != nil {
		t.Fatal(err)
	}
	engine := core.NewEngine()
	if err = engine.ReplaceProject(project, path); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "prs-point-cli-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	server, err := observe.Start(engine, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	args := []string{"point", "set", "--pid", strconv.Itoa(os.Getpid()), "--runtime-dir", dir, "--channel", "server", "--device", "device", "--kind", "holding", "--address", "0x0", "--value", "0", "--quality", "128", "--json"}
	var out, stderr bytes.Buffer
	if code := Execute(context.Background(), args, &out, &stderr, "test"); code != 0 {
		t.Fatal(code, out.String(), stderr.String())
	}
	point := engine.Snapshot().Project.Channels[0].Devices[0].Points[0]
	if point.Value != 0 || point.Quality != 128 {
		t.Fatal(point)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("manual operation modified project file")
	}
}

func TestPointCommandRejectsIncompleteAmbiguousAndInvalidInput(t *testing.T) {
	base := []string{"point", "set", "--pid", "123", "--channel", "c", "--device", "d", "--kind", "yc", "--address", "0"}
	cases := [][]string{{"point"}, {"point", "set"}, append(append([]string{}, base...), "--value", "NaN"), append(append([]string{}, base...), "--quality", "256"), {"point", "report", "--pid", "123", "--channel", "c", "--device", "d", "--kind", "yc", "--address", "0", "--value", ""}}
	for _, args := range cases {
		var out, stderr bytes.Buffer
		if code := Execute(context.Background(), args, &out, &stderr, "test"); code != 2 {
			t.Fatal(args, code, stderr.String())
		}
	}
}
