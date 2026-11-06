package runner

import (
	"context"
	"gytest-go/internal/core"
	"gytest-go/internal/observe"
	"gytest-go/internal/projectfile"
	"os"

	"testing"
)

func TestDeletingExplicitlySelectedChannelKeepsOtherConnectionAndRunner(t *testing.T) {
	path := projectFile(t, "modbus-tcp", unusedAddress(t), false)
	p, _ := core.ReadProject(path)
	other := p.Channels[0]
	other.ID = "steady"
	other.Address = unusedAddress(t)
	p.Channels = append(p.Channels, other)
	writeProject(t, path, p)
	dir, err := os.MkdirTemp("", "prs-del-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	out, _ := startRunner(t, path, "--runtime-dir", dir, "--channels", "server,steady", "--watch=false")
	waitLog(t, out, `"msg":"running"`, 1)
	conn := connectEventually(t, other.Address)
	defer conn.Close()
	sock, info, err := findProjectSocket(context.Background(), dir, os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	_, r, err := observe.Configure(context.Background(), sock, projectfile.Request{Resource: "channel", Action: "delete", Channel: "server", Revision: info.ConfigurationRevision})
	if err != nil || !r.Deleted || !r.Saved {
		t.Fatal(r, err)
	}
	if readHolding(t, conn) != 42 {
		t.Fatal("other connection changed")
	}
	info, err = observe.Fetch(context.Background(), sock, "/v1/info")
	if err != nil || len(info.View.Channels) != 1 || info.View.Channels[0].ID != "steady" {
		t.Fatal(info, err)
	}
	p, err = core.ReadProject(path)
	if err != nil || len(p.Channels) != 1 {
		t.Fatal(p, err)
	}
}
