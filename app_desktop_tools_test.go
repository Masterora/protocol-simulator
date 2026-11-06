package main

import (
	"encoding/base64"
	"io"
	"net"
	"testing"
	"time"

	"gytest-go/internal/core"
)

func TestDesktopPointPreviewAndReviewedSourceApply(t *testing.T) {
	a := NewApp()
	id, err := a.engine.UpsertChannel(core.Channel{Name: "points", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Points: []core.Point{{Kind: "yx", Address: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	data := base64.StdEncoding.EncodeToString([]byte("kind,address,name\nyx,2,开关\n"))
	p, err := a.PreviewPointTable(id, "", "points.csv", data, "", "replace-all")
	if err != nil || p.Total != 1 || p.Removed != 1 || p.Added != 1 {
		t.Fatal(p, err)
	}
	if err = a.ApplyPointTable(id, "", "points.csv", data, "", "replace-all", "different", p.Revision); err == nil {
		t.Fatal("unreviewed source accepted")
	}
	if err = a.ApplyPointTable(id, "", "points.csv", data, "", "replace-all", p.SourceSHA256, p.Revision); err != nil {
		t.Fatal(err)
	}
	v, _ := a.engine.PointTable(id, "")
	if len(v.Points) != 1 || v.Points[0].Address != 2 {
		t.Fatal(v)
	}
}
func TestRawTerminalLaunchIsIndependentAndStopped(t *testing.T) {
	a := NewApp()
	before := a.engine.Snapshot()
	var launched projectWindowConfig
	a.launchProject = func(c projectWindowConfig) error { launched = c; return nil }
	if err := a.OpenRawTerminal("client"); err != nil {
		t.Fatal(err)
	}
	if !launched.Raw || launched.Name != "报文调试" || len(a.engine.Snapshot().Project.Channels) != len(before.Project.Channels) {
		t.Fatal(launched)
	}
	p, err := projectForWindow(launched)
	if err != nil || len(p.Channels) != 1 || p.Channels[0].Protocol != "raw" {
		t.Fatal(p, err)
	}
	if _, err = projectForWindow(projectWindowConfig{Name: "raw", Role: "invalid", Raw: true}); err == nil {
		t.Fatal("invalid role accepted")
	}
}

func TestRawTextEncodesUTF8AndKeepsProtocolGuard(t *testing.T) {
	a := NewApp()
	if err := a.SendRawText("missing", "中文", false); err == nil {
		t.Fatal("text sender bypassed session validation")
	}
	id, err := a.engine.UpsertChannel(core.Channel{Name: "raw", Protocol: "raw", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.engine.Start(id); err != nil {
		t.Fatal(err)
	}
	defer a.engine.StopAll()
	var address string
	for _, s := range a.engine.ChannelStates() {
		if s.ID == id {
			address = s.Address
		}
	}
	c, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	end := time.Now().Add(time.Second)
	for time.Now().Before(end) {
		s := a.engine.ChannelStates()
		if s[0].Peers > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if err = a.SendRawText(id, "中文", false); err != nil {
		t.Fatal(err)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, len([]byte("中文")))
	if _, err = io.ReadFull(c, buf); err != nil || string(buf) != "中文" {
		t.Fatal(buf, err)
	}
}
