package tui

import (
	"strings"
	"testing"
)

func TestEndpointEntryKeepsSeparatorsAndSupportsBothInputStyles(t *testing.T) {
	for text, want := range map[string]string{
		"1921681001001000":     "192.168.100.100:1000",
		"192.168.100.100:1000": "192.168.100.100:1000",
		"1.1.1.1:1":            "1.1.1.1:1",
	} {
		var parts endpointParts
		var cursor endpointCursor
		replace := true
		for _, key := range text {
			if !cursor.key(&parts, &replace, string(key)) {
				t.Fatal("key ignored", key)
			}
		}
		if got, err := parts.checked(); err != nil || got != want {
			t.Fatalf("%s -> %q, %v", text, got, err)
		}
	}
	parts, _ := parseEndpoint("192.168.100.100:1000")
	var cursor endpointCursor
	replace := true
	cursor.key(&parts, &replace, "backspace")
	if got := parts.string(); got != "192.168.100.100:100" {
		t.Fatal("first backspace must delete one digit", got)
	}
	cursor.key(&parts, &replace, "ctrl-u")
	if got := strings.TrimRight(strings.ReplaceAll(cursor.render(parts, true), inputCaret, ""), " "); got != "   .   .   .   :" {
		t.Fatal("mask missing after clear", got)
	}
	cursor.key(&parts, &replace, ".")
	cursor.key(&parts, &replace, ":")
	cursor.key(&parts, &replace, "9")
	if parts[0] != "" || parts[4] != "9" {
		t.Fatal("separators did not move focus", parts)
	}
}

func TestEndpointCaretDoesNotMoveTheSeparators(t *testing.T) {
	parts, _ := parseEndpoint("192.1.1.1:2")
	cursor := endpointCursor{part: 0, at: 3, active: true}
	got := strings.ReplaceAll(cursor.render(parts, true), inputCaret, "")
	if got != "192.1  .1  .1  :2    " {
		t.Fatal(got)
	}
}
