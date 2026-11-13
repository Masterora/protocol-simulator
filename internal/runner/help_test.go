package runner

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCommandHelpUsesChineseSectionsAndRelevantOptions(t *testing.T) {
	run := func(args ...string) string {
		t.Helper()
		var out, stderr bytes.Buffer
		code := Execute(context.Background(), args, &out, &stderr, "test")
		if code != 0 || stderr.Len() != 0 {
			t.Fatalf("%v: exit %d, stderr %q", args, code, stderr.String())
		}
		help := out.String()
		for _, unwanted := range []string{"Usage:", " (default ", "<int>", "<string>"} {
			if strings.Contains(help, unwanted) {
				t.Fatalf("%v: unexpected text %q in %q", args, unwanted, help)
			}
		}
		return help
	}
	root := run()
	if root != run("--help") || root != run("help") || !strings.Contains(root, "\n命令\n") {
		t.Fatal("root help entry points differ")
	}
	for _, name := range []string{"run", "validate", "tui", "ports", "version"} {
		if run(name, "--help") != run("help", name) {
			t.Fatalf("%s help entry points differ", name)
		}
	}
	for resource, actions := range resourceActions {
		group := run("help", resource)
		if !strings.Contains(group, "\n操作\n") || group != run(resource, "--help") || group != run(resource) {
			t.Fatalf("%s group help is incomplete", resource)
		}
		for _, action := range actions {
			help := run("help", resource, action.name)
			if help != run(resource, action.name, "--help") || !strings.Contains(help, "\n选项\n") {
				t.Fatalf("%s %s help is incomplete", resource, action.name)
			}
		}
	}
	if report := run("point", "report", "--help"); strings.Contains(report, "--value") || strings.Contains(report, "--quality") {
		t.Fatalf("report help lists options it rejects: %s", report)
	}
	if stop := run("channel", "stop", "--help"); strings.Contains(stop, "--file") || strings.Contains(stop, "--set") {
		t.Fatalf("stop help lists offline editing options: %s", stop)
	}
}
