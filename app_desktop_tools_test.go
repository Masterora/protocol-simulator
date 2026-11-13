package main

import (
	"encoding/base64"
	"testing"

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
