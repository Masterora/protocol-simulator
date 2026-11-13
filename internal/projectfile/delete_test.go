package projectfile

import (
	"errors"
	"gytest-go/internal/core"
	"os"
	"path/filepath"
	"testing"
)

func deletionProject(t *testing.T) (core.Project, string, string) {
	t.Helper()
	c := DefaultChannel("通道1")
	project := core.Project{Version: 2, Name: "Delete test", Channels: []core.Channel{c}}
	p, r, err := Apply(project, Request{Action: "add", Resource: "device", Channel: c.ID, Fields: Fields(map[string]any{"pointCount": 2})})
	if err != nil {
		t.Fatal(err)
	}
	return p, c.ID, r.Device
}
func TestDeleteResourcesRejectReferencesAndPreserveUnchangedSource(t *testing.T) {
	p, c, d := deletionProject(t)
	p, _, err := Apply(p, Request{Resource: "rule", Action: "add", Channel: c, Device: d, Fields: Fields(map[string]any{"kind": "yc", "points": "1", "enabled": true})})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Apply(p, Request{Resource: "point", Action: "delete", Channel: c, Device: d, Point: &core.PointKey{Kind: "yc", Address: 1}})
	if err == nil || len(p.Channels[0].Devices[0].Points) != 2 {
		t.Fatal("referenced point deleted or source changed", err)
	}
	zero := 0
	next, r, err := Apply(p, Request{Resource: "rule", Action: "delete", Channel: c, Device: d, Index: &zero})
	if err != nil || len(RuleConfiguration(next.Channels[0].Devices[0]).Rules) != 0 || r.Index == nil {
		t.Fatal(r, err)
	}
	if len(RuleConfiguration(p.Channels[0].Devices[0]).Rules) != 1 {
		t.Fatal("source rule modified")
	}
	next, _, err = Apply(next, Request{Resource: "point", Action: "delete", Channel: c, Device: d, Point: &core.PointKey{Kind: "yc", Address: 1}})
	if err != nil || len(next.Channels[0].Devices[0].Points) != 1 {
		t.Fatal(err)
	}
	next, _, err = Apply(next, Request{Resource: "device", Action: "delete", Channel: c, Device: d})
	if err != nil || len(next.Channels[0].Devices) != 0 {
		t.Fatal(err)
	}
	next, _, err = Apply(next, Request{Resource: "channel", Action: "delete", Channel: c})
	if err != nil || len(next.Channels) != 0 {
		t.Fatal(err)
	}
	if len(p.Channels) != 1 || len(p.Channels[0].Devices) != 1 || len(p.Channels[0].Devices[0].Points) != 2 {
		t.Fatal("source mutated")
	}
}
func TestPermanentProjectRemovalRequiresFreshRevisionAndRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project.json")
	doc, err := Create(path, "Delete")
	if err != nil {
		t.Fatal(err)
	}
	if err = Remove(path, "stale"); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err = Remove(path, doc.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}
