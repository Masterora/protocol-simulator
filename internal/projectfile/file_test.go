package projectfile

import (
	"bytes"
	"errors"
	"gytest-go/internal/core"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestCreateEditRoundTripPreservesDeviceMetadataAndRejectsStaleWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "工程.json")
	doc, err := Create(path, "自定义工程")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Create(path, "overwrite"); err == nil {
		t.Fatal("overwrote existing project")
	}
	c := doc.Project.Channels[0]
	next, result, err := Apply(doc.Project, Request{Resource: "device", Action: "add", Channel: c.ID, Fields: Fields(map[string]any{"pointCount": 3, "name": "装置甲"})})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := Commit(path, doc.Revision, next)
	if err != nil {
		t.Fatal(err)
	}
	if revision == doc.Revision {
		t.Fatal("revision did not change")
	}
	if _, err = Commit(path, doc.Revision, doc.Project); !errors.Is(err, ErrConflict) {
		t.Fatal("stale save accepted", err)
	}
	loaded, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	d := loaded.Project.Channels[0].Devices[0]
	if len(d.Points) != 3 || d.ID != result.Device {
		t.Fatal(d)
	}
	if len(d.DataTests) != 0 || d.AutoData != nil {
		t.Fatal("generated unsolicited data rules")
	}
	next, _, err = Apply(loaded.Project, Request{Resource: "point", Action: "edit", Channel: c.ID, Device: d.ID, Point: &core.PointKey{Kind: "yc", Address: 1}, Fields: Fields(map[string]any{"name": "电压", "wireType": 36, "value": 220.5})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Commit(path, loaded.Revision, next); err != nil {
		t.Fatal(err)
	}
	saved, _ := Read(path)
	point := saved.Project.Channels[0].Devices[0].Points[0]
	if point.WireType != 36 || point.Value != 220.5 || point.Name != "电压" {
		t.Fatal(point)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("permissions changed", info.Mode())
	}
}
func TestConcurrentRevisionCheckedWritersCannotLoseAcceptedUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.json")
	doc, err := Create(path, "p")
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	results := make(chan error, 2)
	for _, name := range []string{"one", "two"} {
		group.Add(1)
		go func(name string) {
			defer group.Done()
			next, _, err := Apply(doc.Project, Request{Resource: "project", Action: "edit", Fields: Fields(map[string]any{"name": name})})
			if err == nil {
				_, err = Commit(path, doc.Revision, next)
			}
			results <- err
		}(name)
	}
	group.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatal("concurrent writers both committed", accepted)
	}
	saved, err := Read(path)
	if err != nil || (saved.Project.Name != "one" && saved.Project.Name != "two") {
		t.Fatal(saved, err)
	}
}
func TestInvalidPointAndRuleEditsDoNotTouchOriginalFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.json")
	doc, _ := Create(path, "p")
	c := doc.Project.Channels[0]
	p, r, err := Apply(doc.Project, Request{Resource: "device", Action: "add", Channel: c.ID, Fields: Fields(map[string]any{"pointCount": 2})})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, fields := range []map[string]any{{"quality": 256}, {"value": "wrong"}, {"address": 2}, {"unknown": true}} {
		_, _, err = Apply(p, Request{Resource: "point", Action: "edit", Channel: c.ID, Device: r.Device, Point: &core.PointKey{Kind: "yc", Address: 1}, Fields: Fields(fields)})
		if err == nil {
			t.Fatal("invalid edit accepted", fields)
		}
	}
	_, _, err = Apply(p, Request{Resource: "rule", Action: "add", Channel: c.ID, Device: r.Device, Fields: Fields(map[string]any{"enabled": true, "points": "999", "kind": "yc"})})
	if err == nil {
		t.Fatal("enabled empty selection accepted")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(data, after) {
		t.Fatal("validation wrote project file")
	}
}
func TestPointAddressEditProtectsExistingAutomaticRuleReferences(t *testing.T) {
	doc, _ := Create(filepath.Join(t.TempDir(), "p.json"), "p")
	c := doc.Project.Channels[0]
	p, r, err := Apply(doc.Project, Request{Resource: "device", Action: "add", Channel: c.ID, Fields: Fields(map[string]any{"pointCount": 2})})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err = Apply(p, Request{Resource: "rule", Action: "add", Channel: c.ID, Device: r.Device, Fields: Fields(map[string]any{"points": "1-2", "enabled": true})})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = Apply(p, Request{Resource: "point", Action: "edit", Channel: c.ID, Device: r.Device, Point: &core.PointKey{Kind: "yc", Address: 1}, Fields: Fields(map[string]any{"address": 10})})
	if err == nil {
		t.Fatal("broke reporting rule reference")
	}
}
