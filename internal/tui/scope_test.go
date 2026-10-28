package tui

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/rivo/uniseg"
	"gytest-go/internal/core"
	"gytest-go/internal/observe"
)

func testObserver(t *testing.T) (string, *core.Engine) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix observer")
	}
	dir, err := os.MkdirTemp("", "prs-scope-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	e := core.NewEngine()
	for i := 0; i < 2; i++ {
		c := core.Channel{ID: fmt.Sprintf("ch-%d", i), Name: fmt.Sprintf("通道 %d", i), Protocol: "modbus-tcp", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:15020", Unit: 1, Devices: []core.Device{{ID: "device-1", Name: "装置一", Unit: 1, CommonAddress: 1, Points: []core.Point{{Address: 1, Kind: "holding", Name: fmt.Sprintf("电压-%d", i), Value: 220}}}, {ID: "device-2", Name: "装置二", Unit: 2, CommonAddress: 2, Points: []core.Point{{Address: 2, Kind: "holding", Name: fmt.Sprintf("温度-%d", i), Value: 32}}}}}
		if _, err = e.UpsertChannel(c); err != nil {
			t.Fatal(err)
		}
	}
	server, err := observe.Start(e, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return dir, e
}
func pollModel(m *model, dir string) {
	m.apply(fetch(context.Background(), request{dir: dir, socket: m.socket, channel: m.channel, device: m.device, kind: m.pointKind, filter: m.filter, offset: m.pointOffset, after: m.after, logAfter: m.logAfter}))
}
func TestDirectResourceKeysResolveWholeScopeWithoutEnter(t *testing.T) {
	dir, _ := testObserver(t)
	for _, keys := range [][]string{{"2"}, {"3"}, {"4"}, {"5"}, {"6"}, {"7"}, {"tab"}, {":", "p", "t", "enter"}} {
		m := model{}
		// Reproduces pressing 2 before the first discovery response arrives.
		for _, key := range keys {
			m.key(key)
		}
		pollModel(&m, dir)
		if m.socket == "" || m.channel != "ch-0" || m.device != "device-1" || !m.connected {
			t.Fatalf("%v left empty scope: %+v", keys, m)
		}
		if len(m.detail.View.Channels) != 2 || len(m.detail.View.Devices) != 2 || len(m.detail.View.Points) != 1 {
			t.Fatalf("%v: %+v", keys, m.detail.View)
		}
		if (m.view == 1 || m.view == 2 || m.view == 3) && len(m.rows()) == 0 {
			t.Fatalf("%v empty resource view", keys)
		}
	}
}
func TestHighlightedProjectChannelAndDeviceAreUsedByDirectJumps(t *testing.T) {
	dir, _ := testObserver(t)
	m := model{}
	pollModel(&m, dir)
	// Jump from the second highlighted channel directly to Points.
	m.key("2")
	m.key("down")
	m.key("4")
	pollModel(&m, dir)
	if m.channel != "ch-1" || !strings.Contains(m.rows()[0].text, "电压-1") {
		t.Fatal(m.channel, m.rows())
	}
	m.key("3")
	m.key("down")
	m.key("4")
	pollModel(&m, dir)
	if m.device != "device-2" || !strings.Contains(m.rows()[0].text, "温度-1") {
		t.Fatal(m.device, m.rows())
	}
	// Switching project clears cached device/point scope and uses its metadata.
	other := instance{socket: "second", info: observe.Info{View: core.Inspection{Name: "另一个工程", Channels: []core.InspectChannel{{ID: "second-channel"}}}}}
	m.instances = append(m.instances, other)
	m.selectView(0)
	m.cursor = 1
	m.key("4")
	if m.socket != "second" || m.channel != "second-channel" || m.device != "" || len(m.detail.View.Points) != 0 {
		t.Fatal(m.socket, m.channel, m.device)
	}
}
func TestDetailIdentitySurvivesHistoryEvictionAndHelpOwnsFocus(t *testing.T) {
	m := model{view: 5, messages: []core.Message{{Sequence: 1, Direction: "RX", Hex: "680407000000", Summary: "first"}}}
	m.key("enter")
	before := strings.Join(m.detailLines, "\n")
	m.messages = []core.Message{{Sequence: 2, Direction: "TX", Summary: "replacement"}}
	if !strings.Contains(before, "0000  68 04 07 00 00 00") {
		t.Fatal(before)
	}
	m.refreshDetail()
	after := strings.Join(m.detailLines, "\n")
	if !strings.Contains(after, "已移出历史缓冲区") || strings.Contains(after, "replacement") || strings.Contains(after, "68 04") {
		t.Fatal(after)
	}
	m.key("esc")
	m.key("?")
	view := m.view
	m.key("4")
	m.key("down")
	if m.view != view || !m.help {
		t.Fatal("help changed underlying resource")
	}
	m.key("esc")
	if m.help {
		t.Fatal("help did not close")
	}
}
func TestResourceFramesFitTerminalCellsAndExposeContext(t *testing.T) {
	dir, _ := testObserver(t)
	m := model{}
	pollModel(&m, dir)
	for _, width := range []int{60, 80, 110, 150} {
		for _, height := range []int{12, 24, 35} {
			for v := 0; v < 7; v++ {
				m.selectView(v)
				rendered := m.render(width, height)
				// Strip only renderer-owned escape sequences; user controls are sanitized.
				plain := stripStyles(rendered)
				lines := strings.Split(strings.TrimPrefix(plain, "\x1b[H"), "\r\n")
				if len(lines) != height {
					t.Fatalf("%dx%d: %d lines", width, height, len(lines))
				}
				for _, line := range lines {
					if n := uniseg.StringWidth(line); n > width-1 {
						t.Fatalf("%dx%d: %d cells %q", width, height, n, line)
					}
				}
				if !strings.Contains(plain, "PRS") || !strings.Contains(plain, "┌") || !strings.Contains(plain, "└") {
					t.Fatal(plain)
				}
			}
		}
	}
}
func stripStyles(s string) string {
	for _, style := range []string{reset, cyan, purple, muted, white, green, yellow, red, selected, active, "\x1b[K"} {
		s = strings.ReplaceAll(s, style, "")
	}
	return s
}
func TestAutoSelectionSkipsUnavailableProjectAndPointDetailsUseTypedAddress(t *testing.T) {
	m := model{}
	m.apply(update{socket: "healthy", channel: "ch", device: "d", instances: []instance{{socket: "unavailable", err: fmt.Errorf("offline")}, {socket: "healthy", info: observe.Info{View: core.Inspection{Name: "工程"}}}}, detail: observe.Info{View: core.Inspection{Channels: []core.InspectChannel{{ID: "ch"}}, Devices: []core.InspectDevice{{ID: "d"}}}}})
	if m.cursor != 1 || m.socket != "healthy" {
		t.Fatal(m.cursor, m.socket)
	}
	m.view = 3
	m.cursor = 1
	m.detail.View.Points = []core.Point{{Address: 0, Kind: "coil", Value: 1}, {Address: 0, Kind: "holding", Value: 99}}
	m.openDetail()
	data := strings.Join(m.detailLines, "\n")
	if !strings.Contains(data, "保持寄存器") || !strings.Contains(data, "99") {
		t.Fatal(data)
	}
}

func TestEscapeAtProjectOverviewStaysAtRoot(t *testing.T) {
	m := model{}
	m.key("esc")
	if m.view != 0 {
		t.Fatal("Esc left project overview", m.view)
	}
}
