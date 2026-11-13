package tui

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

const inputCaret = "\ue000"

var terminalControl = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]")

var terminalKeys = map[string]string{
	"\x1b[A": "up", "\x1b[B": "down", "\x1b[C": "right", "\x1b[D": "left",
	"\x1bOA": "up", "\x1bOB": "down", "\x1bOC": "right", "\x1bOD": "left",
	"\x1b[5~": "pageup", "\x1b[6~": "pagedown", "\x1b[3~": "delete",
	"\x1b[Z": "shift-tab", "\x1b[1;2Z": "shift-tab", "\x1b[9;2u": "shift-tab", "\x1b[27;2;9~": "shift-tab",
	"\x1b[H": "home", "\x1b[F": "end", "\x1bOH": "home", "\x1bOF": "end", "\x1b[1~": "home", "\x1b[4~": "end",
}

func terminalFrame(frame string, caretVisible bool) string {
	before, _, found := strings.Cut(frame, inputCaret)
	if !found {
		return frame + "\x1b[?25l"
	}
	frame = strings.ReplaceAll(frame, inputCaret, "")
	if !caretVisible {
		return frame + "\x1b[?25l"
	}
	row := strings.Count(before, "\r\n") + 1
	line := before[strings.LastIndex(before, "\n")+1:]
	column := uniseg.StringWidth(terminalControl.ReplaceAllString(line, "")) + 1
	return frame + fmt.Sprintf("\x1b[%d;%dH\x1b[?25h", row, column)
}

type textCursor struct {
	// The zero value places the caret at the end of the current field.
	tail int
}

func (c textCursor) index(text string) int {
	return len(text) - min(len(text), c.tail)
}

func (c *textCursor) key(text *string, replace *bool, key string, limit int) bool {
	s := *text
	i := c.index(s)
	left, right := i, i
	if key == "left" || key == "right" || key == "backspace" || key == "delete" {
		graphemes := uniseg.NewGraphemes(s)
		for graphemes.Next() {
			start, end := graphemes.Positions()
			if start < i {
				left = start
			}
			if end > i {
				right = end
				break
			}
		}
	}
	next := i
	switch key {
	case "left":
		next = left
	case "right":
		next = right
	case "home":
		next = 0
	case "end":
		next = len(s)
	case "ctrl-u":
		s, next = "", 0
	case "backspace", "delete":
		if *replace {
			s, next = "", 0
		} else if key == "backspace" {
			s, next = s[:left]+s[i:], left
		} else {
			s = s[:i] + s[right:]
		}
	default:
		r, size := utf8.DecodeRuneInString(key)
		if size == 0 || size != len(key) || unicode.IsControl(r) || key == inputCaret {
			return false
		}
		if *replace {
			s, i = "", 0
		}
		if len(s)+len(key) > limit {
			return false
		}
		s, next = s[:i]+key+s[i:], i+len(key)
	}
	changed := s != *text
	*text, *replace = s, false
	c.tail = len(s) - next
	return changed
}

func (c textCursor) render(text string, width int) string {
	if width < 1 {
		return ""
	}
	i := c.index(text)
	left, right := text[:i], text[i:]
	width--
	leftWidth := uniseg.StringWidth(left)
	rightWidth := uniseg.StringWidth(right)
	if leftWidth+rightWidth <= width {
		return left + inputCaret + right
	}
	leftBudget := min(leftWidth, max(width/2, width-rightWidth))
	rightPart := clipped(right, width-leftBudget, false)
	return clipped(left, width-uniseg.StringWidth(rightPart), true) + inputCaret + rightPart
}
