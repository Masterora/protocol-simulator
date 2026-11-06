//go:build !devassets

package main

import (
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestReleaseNeverUsesDevelopmentAssets(t *testing.T) {
	t.Setenv("PRS_DEV_ASSETS", t.TempDir())
	source := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("embedded")}}
	assets, err := developmentAssets(source)
	if err != nil {
		t.Fatal(err)
	}
	b, err := fs.ReadFile(assets, "index.html")
	if err != nil || string(b) != "embedded" {
		t.Fatal(string(b), err)
	}
	state, err := NewApp().GetDevelopmentState()
	if err != nil || state.Enabled || developmentProjectLaunch() {
		t.Fatal("development enabled in release", state, err)
	}
}
