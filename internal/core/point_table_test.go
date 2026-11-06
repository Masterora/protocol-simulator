package core

import (
	"reflect"
	"testing"
)

func TestPointImportValidatesBeforeDisconnectAndPreservesOtherChannels(t *testing.T) {
	e := NewEngine()
	makeChannel := func(name string) string {
		id, err := e.UpsertChannel(Channel{Name: name, Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:0", Points: []Point{{Kind: "yx", Address: 1, Name: "old"}, {Kind: "yc", Address: 2, Value: 42}}})
		if err != nil {
			t.Fatal(err)
		}
		if err = e.Start(id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := makeChannel("one")
	other := makeChannel("two")
	defer e.StopAll()
	v, err := e.PointTable(id, "")
	if err != nil {
		t.Fatal(err)
	}
	invalid := []Point{{Kind: "yc", Address: 0x1000000}}
	if _, err = e.CheckPointTable(id, "", v.Revision, invalid, "merge", ""); err == nil {
		t.Fatal("invalid point could be previewed")
	}
	if err = e.ImportPointTable(id, "", v.Revision, invalid, "replace-all", ""); err == nil {
		t.Fatal("invalid point accepted")
	}
	if err = e.ImportPointTable(id, "", v.Revision-1, []Point{{Kind: "yx", Address: 3}}, "merge", ""); err == nil {
		t.Fatal("stale preview accepted")
	}
	e.mu.Lock()
	first, second := e.running[id], e.running[other]
	e.mu.Unlock()
	if first == nil || second == nil {
		t.Fatal("failed preview stopped a channel")
	}
	rows := []Point{{Kind: "yx", Address: 3, Name: "new"}}
	removed, err := e.CheckPointTable(id, "", v.Revision, rows, "replace-kind", "yx")
	if err != nil || removed != 1 {
		t.Fatal(removed, err)
	}
	if err = e.ImportPointTable(id, "", v.Revision, rows, "replace-kind", "yx"); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	first, latestSecond := e.running[id], e.running[other]
	e.mu.Unlock()
	if first != nil || latestSecond != second {
		t.Fatal("import did not isolate target channel")
	}
	got, _ := e.PointTable(id, "")
	if !reflect.DeepEqual(got.Points, []Point{{Kind: "yc", Address: 2, Value: 42}, {Kind: "yx", Address: 3, Name: "new"}}) {
		t.Fatal(got)
	}
}
