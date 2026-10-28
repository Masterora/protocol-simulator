package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rivo/uniseg"
	"gytest-go/internal/core"
	"gytest-go/internal/observe"
)

func TestTerminalTextCannotInjectControlsAndRespectsCellWidths(t *testing.T) {
	text := "工程\x1b[2J\r\n\u009b\u202e危险"
	safe := sanitize(text)
	if strings.ContainsAny(safe, "\x1b\r\n\u009b\u202e") {
		t.Fatal(safe)
	}
	for width := 0; width < 20; width++ {
		value := fit(text, width)
		if !utf8.ValidString(value) || uniseg.StringWidth(value) != width {
			t.Fatal(width, value)
		}
	}
	for _, line := range wrap(text, 5) {
		if uniseg.StringWidth(line) > 5 {
			t.Fatal(line)
		}
	}
}
func TestNavigationSearchDetailPauseAndQuitNeverOwnRuntime(t *testing.T) {
	m := model{instances: []instance{{socket: "one", info: observe.Info{PID: 1, View: core.Inspection{Name: "工程一"}}}, {socket: "two", info: observe.Info{PID: 2, View: core.Inspection{Name: "工程二"}}}}}
	for _, key := range []string{"/", "工", "程", "二", "enter"} {
		if m.key(key) {
			t.Fatal("search quit interface")
		}
	}
	if len(m.rows()) != 1 {
		t.Fatal(m.rows())
	}
	m.key("enter")
	if m.socket != "two" || m.view != 1 {
		t.Fatal(m)
	}
	m.detail = observe.Info{View: core.Inspection{Channels: []core.InspectChannel{{ID: "channel", Name: "通道"}}, Devices: []core.InspectDevice{{ID: "device", Name: "装置"}}, Points: []core.Point{{Address: 1, Kind: "yc", Name: "电压", Value: 220.5}}, PointMatches: 1}}
	m.channel, m.device = "channel", "device"
	m.key("enter")
	if m.channel != "channel" || m.view != 2 {
		t.Fatal(m)
	}
	m.key("enter")
	if m.device != "device" || m.view != 3 {
		t.Fatal(m)
	}
	m.key("enter")
	if !m.detailOpen {
		t.Fatal("point detail unavailable")
	}
	m.key("esc")
	m.key("p")
	if !m.paused {
		t.Fatal("display not paused")
	}
	m.key(":")
	for _, key := range []string{"l", "o", "g", "s", "enter"} {
		m.key(key)
	}
	if m.view != 6 {
		t.Fatal(m)
	}
	m.key("?")
	if !strings.Contains(m.render(90, 24), "工程继续运行") {
		t.Fatal(m.render(90, 24))
	}
	if !m.key("q") || !m.key("ctrl-c") {
		t.Fatal("quit unavailable")
	}
}
func TestOutdatedDetailCannotOverwriteCurrentSelection(t *testing.T) {
	m := model{socket: "socket", channel: "new", device: "device"}
	m.apply(update{socket: "socket", request: request{channel: "old", device: "device"}, detail: observe.Info{View: core.Inspection{Name: "stale"}}})
	if m.detail.View.Name == "stale" {
		t.Fatal("stale response applied")
	}
}
