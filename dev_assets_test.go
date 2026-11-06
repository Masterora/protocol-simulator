//go:build devassets

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDevelopmentAssetsRevisionsAndBoundaries(t *testing.T) {
	root := t.TempDir()
	write := func(n, v string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, n), []byte(v), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("index.html", "<html><head></head><body></body></html>")
	write("app.js", "let a=1;")
	write("app.css", "body{color:red}")
	write("secret.txt", "private")
	t.Setenv("PRS_DEV_ASSETS", root)
	assets, err := developmentAssets(fstest.MapFS{})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := fs.ReadDir(assets, ".")
	if err != nil || len(entries) != 3 {
		t.Fatal(entries, err)
	}
	data, err := fs.ReadFile(assets, "index.html")
	if err != nil || !strings.Contains(string(data), "GetDevelopmentState") || !strings.Contains(string(data), `"enabled":true`) {
		t.Fatalf("injection absent: %s %v", data, err)
	}
	first, _ := NewApp().GetDevelopmentState()
	write("app.css", "body{color:tan}")
	second, _ := NewApp().GetDevelopmentState()
	if first.Code != second.Code || first.Styles == second.Styles {
		t.Fatal("CSS should not reload code")
	}
	write("app.js", "let a=2;")
	third, _ := NewApp().GetDevelopmentState()
	if third.Code == second.Code || third.Styles != second.Styles {
		t.Fatal("JS change was not detected")
	}
	for _, name := range []string{"../secret.txt", "secret.txt", "/index.html", "nested/app.js"} {
		if _, err := fs.ReadFile(assets, name); err == nil {
			t.Fatal("invalid resource exposed", name)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.js")
	os.WriteFile(outside, []byte("secret"), 0600)
	if err := os.Symlink(outside, filepath.Join(root, "link.js")); err == nil {
		if _, err = fs.ReadFile(assets, "link.js"); err == nil {
			t.Fatal("symlink exposed outside file")
		}
	}
	t.Setenv("PRS_DEV_ASSETS", "frontend")
	if _, err = developmentAssets(fstest.MapFS{}); err == nil {
		t.Fatal("relative path accepted")
	}
}
