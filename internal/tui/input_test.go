package tui

import (
	"strings"
	"testing"

	"github.com/rivo/uniseg"
)

func TestTextCaretEditsAtPositionAndStaysVisible(t *testing.T) {
	var cursor textCursor
	value, replace := "甲e\u0301乙", true
	cursor.key(&value, &replace, "left", 64)
	if got := cursor.render(value, 10); got != "甲e\u0301"+inputCaret+"乙" {
		t.Fatal(got)
	}
	cursor.key(&value, &replace, "backspace", 64)
	cursor.key(&value, &replace, "新", 64)
	cursor.key(&value, &replace, "delete", 64)
	if value != "甲新" {
		t.Fatal(value)
	}
	cursor.key(&value, &replace, "home", 64)
	cursor.key(&value, &replace, "先", 64)
	if value != "先甲新" {
		t.Fatal(value)
	}
	if got := cursor.render(value, 4); !strings.Contains(got, inputCaret) || uniseg.StringWidth(got) > 4 {
		t.Fatal(got)
	}
}

func TestTerminalFramePlacesBlinkingCaretUsingCellWidth(t *testing.T) {
	frame := "\x1b[Hfirst\r\n" + selected + "遥测 " + inputCaret + "123" + reset
	visible := terminalFrame(frame, true)
	if !strings.HasSuffix(visible, "\x1b[2;6H\x1b[?25h") || strings.Contains(visible, inputCaret) {
		t.Fatal(visible)
	}
	if hidden := terminalFrame(frame, false); !strings.HasSuffix(hidden, "\x1b[?25l") || strings.Contains(hidden, inputCaret) {
		t.Fatal(hidden)
	}
}
