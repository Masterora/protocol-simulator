package tui

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
)

func TestLegacyPointPagesSortAcrossBoundariesAndKeepKindsSeparate(t *testing.T) {
	var points []core.Point
	for i := 274; i >= 0; i-- {
		points = append(points, core.Point{Kind: "yc", Address: uint32(i), Name: "needle"})
	}
	for i := 29; i >= 0; i-- {
		points = append(points, core.Point{Kind: "yx", Address: uint32(i), Name: "yc needle"})
	}
	for _, kind := range []string{"yk", "yt"} {
		points = append(points, core.Point{Kind: kind, Address: 1, Name: "needle"})
	}
	page := func(offset int) (observe.Info, error) {
		return observe.Info{View: core.Inspection{Channels: []core.InspectChannel{{ID: "c", Protocol: "iec104"}}, PointTotal: len(points), PointMatches: len(points), Points: points[offset:min(offset+100, len(points))]}}, nil
	}
	first, _ := page(0)
	for _, offset := range []int{0, 100, 200} {
		q := url.Values{"channel": {"c"}, "kind": {"yc"}, "filter": {"needle"}, "offset": {fmt.Sprint(offset)}}
		info, kind, err := legacyPointPage(q, first, page)
		if err != nil || kind != "yc" || info.View.PointMatches != 275 || info.View.PointKinds["yx"] != 30 {
			t.Fatal(info, kind, err)
		}
		want := min(100, 275-offset)
		if len(info.View.Points) != want {
			t.Fatal(len(info.View.Points), want)
		}
		for i, p := range info.View.Points {
			if p.Kind != "yc" || p.Address != uint32(offset+i) {
				t.Fatal(offset, i, p)
			}
		}
	}
	info, kind, err := legacyPointPage(url.Values{"channel": {"c"}}, first, page)
	if err != nil || kind != "yx" || len(info.View.Points) != 30 || info.View.Points[0].Address != 0 {
		t.Fatal(info, kind, err)
	}
	info, kind, err = legacyPointPage(url.Values{"channel": {"c"}, "kind": {"removed-kind"}, "offset": {"100"}}, first, page)
	if err != nil || kind != "yx" || len(info.View.Points) != 30 || info.View.Points[0].Address != 0 {
		t.Fatal(info, kind, err)
	}
	if points[0].Address != 274 {
		t.Fatal("viewer reordered source data")
	}
}

func TestPointKindSwitchResetsSearchPageAndRejectsPreviousKindResponse(t *testing.T) {
	m := model{view: 3, pointKind: "yx", pointOffset: 100, filter: "old", paused: true}
	m.detail.View.Points = []core.Point{{Kind: "yx", Address: 123}}
	m.switchPointKind(1)
	if m.pointKind != "yc" || m.pointOffset != 0 || m.filter != "" || len(m.rows()) != 0 || !m.paused {
		t.Fatal(m)
	}
	m.apply(update{request: request{kind: "yx"}, detail: observe.Info{View: core.Inspection{Points: []core.Point{{Kind: "yx", Address: 123}}}}})
	if len(m.rows()) != 0 {
		t.Fatal("late response from previous kind applied")
	}
	for _, want := range []string{"yk", "yt", "yx"} {
		m.switchPointKind(1)
		if m.pointKind != want {
			t.Fatal(m.pointKind, want)
		}
	}
}

func TestControlsAndStatusStayAtBottomInEveryViewAndDetail(t *testing.T) {
	m := model{discovered: true, connected: true, socket: "s", pointKind: "yc"}
	for view := 0; view < 7; view++ {
		for _, width := range []int{60, 90, 150} {
			for _, overlay := range []bool{false, true} {
				m.view, m.detailOpen = view, overlay
				m.detailTitle = "点位详情"
				m.detailLines = []string{"live"}
				plain := stripStyles(m.render(width, 24))
				lines := strings.Split(plain, "\r\n")
				table, action, status := -1, -1, -1
				for i, line := range lines {
					if strings.Contains(line, "┌") || strings.Contains(line, "╭") {
						table = i
					}
					if strings.Contains(line, "[q]") {
						action = i
					}
					if strings.Contains(line, "报文缺口") {
						status = i
					}
				}
				if table < 0 || action < 0 || status < 0 || action != len(lines)-2 || status != len(lines)-1 || action <= table {
					t.Fatal(view, width, overlay, plain)
				}
				if !strings.Contains(lines[len(lines)-3], "└") {
					t.Fatal("footer is not below table", plain)
				}
			}
		}
	}
}
