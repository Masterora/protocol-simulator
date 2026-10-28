package tui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rivo/uniseg"
	"gytest-go/internal/core"
	"gytest-go/internal/observe"
)

func TestRemovedKeysDoNotNavigateButRemainSearchableText(t *testing.T) {
	m := model{view: 3, cursor: 1, detail: observe.Info{View: core.Inspection{Points: []core.Point{{Kind: "yc", Address: 1}, {Kind: "yc", Address: 2}}}}}
	for _, key := range []string{"d", "j", "k"} {
		m.key(key)
	}
	if m.cursor != 1 || m.detailOpen {
		t.Fatal("removed shortcuts still active", m.cursor, m.detailOpen)
	}
	m.key("up")
	if m.cursor != 0 {
		t.Fatal("up unavailable")
	}
	m.key("down")
	m.key("enter")
	if !m.detailOpen {
		t.Fatal("Enter did not open detail")
	}
	for _, key := range []string{"d", "j", "k"} {
		m.key(key)
	}
	if m.detailScroll != 0 {
		t.Fatal("removed detail scroll shortcuts still active")
	}
	m.key("down")
	if m.detailScroll != 1 {
		t.Fatal("down did not scroll detail")
	}
	m.key("enter")
	m.key("?")
	for _, key := range []string{"j", "k"} {
		m.key(key)
	}
	if m.detailScroll != 0 {
		t.Fatal("removed help scroll shortcuts still active")
	}
	m.key("esc")
	m.key("/")
	for _, key := range []string{"d", "j", "k"} {
		m.key(key)
	}
	if m.input != "djk" {
		t.Fatal("ordinary search characters were removed", m.input)
	}
}

func TestCurrentPageAndCategoryUseColorWithoutBrackets(t *testing.T) {
	m := model{view: 3, pointKind: "yc"}
	m.detail.View.PointKinds = map[string]int{"yx": 21, "yc": 61, "yk": 10, "yt": 2}
	for _, width := range []int{60, 90, 150} {
		raw := m.render(width, 24)
		plain := strings.Split(stripStyles(raw), "\r\n")
		if strings.ContainsAny(plain[3]+plain[4], "[]") {
			t.Fatal(plain[3], plain[4])
		}
		if !strings.Contains(raw, active+" 4:点位 ") || !strings.Contains(raw, active+" 遥测 61 ") {
			t.Fatal("selected navigation lacks color", raw)
		}
		if strings.Contains(raw, "回车") || strings.Contains(raw, "[d]") || strings.Contains(raw, "j/k") {
			t.Fatal(raw)
		}
	}
}

func TestAllViewHeadersAlignWithFirstDataColumn(t *testing.T) {
	m := model{socket: "s", channel: "c", device: "dev", discovered: true, connected: true}
	m.instances = []instance{{socket: "s", info: observe.Info{PID: 123456, View: core.Inspection{Name: "工程"}}}}
	m.detail = observe.Info{View: core.Inspection{
		Channels: []core.InspectChannel{{ID: "c", Name: "通道甲", Protocol: "iec104", Address: "127.0.0.1:2406"}},
		Devices:  []core.InspectDevice{{ID: "dev", Name: "装置甲", Points: 1}},
		Points:   []core.Point{{Kind: "yc", Address: 17026, Name: "电压é", Value: 42}},
		Rules:    &core.AutoDataSnapshot{Config: core.AutoDataConfig{Rules: []core.AutoDataRule{{Kind: "yc", Points: "17026"}}}},
	}}
	m.messages = []core.Message{{Sequence: 1, ChannelID: "c", Direction: "RX", Time: "05:49:36.001", Hex: "680407000000"}}
	m.logs = []observe.Log{{Sequence: 1, Time: time.Date(2026, 10, 28, 5, 49, 36, 0, time.UTC), Level: "INFO", Message: "running"}}
	for view := 0; view < 7; view++ {
		for _, width := range []int{60, 80, 110, 150} {
			m.view = view
			lines := strings.Split(stripStyles(m.render(width, 24)), "\r\n")
			top := 0
			for i, line := range lines {
				if strings.Contains(line, "┌") {
					top = i
					break
				}
			}
			cols, indices := m.columns(width - 5)
			header := cols[indices[0]].title
			value := m.rows()[0].cells[indices[0]]
			r, _ := utf8.DecodeRuneInString(value)
			hi := strings.Index(lines[top+1], header)
			di := strings.IndexRune(lines[top+2], r)
			if hi < 0 || di < 0 {
				t.Fatal(view, width, lines)
			}
			hc, dc := uniseg.StringWidth(lines[top+1][:hi]), uniseg.StringWidth(lines[top+2][:di])
			if hc != dc || hc != 3 {
				t.Fatalf("view %d width %d: header at %d, data at %d", view, width, hc, dc)
			}
		}
	}
}
