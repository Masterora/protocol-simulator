package tui

import (
	"container/heap"
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/rivo/uniseg"
	"gytest-go/internal/core"
	"gytest-go/internal/observe"
)

func pointKinds(protocol string, counts map[string]int) []string {
	kinds := []string{"yx", "yc", "yk", "yt"}
	if strings.HasPrefix(protocol, "modbus") {
		kinds = []string{"holding", "input", "coil", "discrete"}
	} else if protocol == "raw" {
		kinds = []string{"raw"}
	}
	var extra []string
	for kind := range counts {
		found := false
		for _, k := range kinds {
			found = found || kind == k
		}
		if !found {
			extra = append(extra, kind)
		}
	}
	sort.Strings(extra)
	return append(kinds, extra...)
}

func (m *model) pointKinds() []string {
	return pointKinds(m.pointProtocol(), m.detail.View.PointKinds)
}
func (m *model) pointProtocol() string {
	for _, c := range m.detail.View.Channels {
		if c.ID == m.channel {
			return c.Protocol
		}
	}
	return ""
}
func (m *model) switchPointKind(step int) {
	if m.view != 3 {
		return
	}
	kinds := m.pointKinds()
	index := 0
	for i, k := range kinds {
		if k == m.pointKind {
			index = i
			break
		}
	}
	m.pointKind = kinds[(index+step+len(kinds))%len(kinds)]
	m.pointOffset, m.cursor = 0, 0
	m.filter = ""
	// A paused viewer can change scope, but must not display the previous kind's data.
	m.detail.View.Points = nil
	m.detail.View.PointMatches = 0
}

func pointProtocol(info observe.Info, channel string) string {
	for _, c := range info.View.Channels {
		if c.ID == channel {
			return c.Protocol
		}
	}
	return ""
}
func selectedPointKind(protocol, requested string, counts map[string]int) string {
	kinds := pointKinds(protocol, counts)
	if requested != "" {
		for _, kind := range kinds {
			if kind == requested {
				return requested
			}
		}
	}
	for _, kind := range kinds {
		if counts[kind] > 0 {
			return kind
		}
	}
	return kinds[0]
}

// pointPage uses the bounded, ordered query when available. Old running engines
// are read through their existing 100-point pages, without restarting them.
func pointPage(ctx context.Context, socket string, query url.Values, info observe.Info) (observe.Info, string, error) {
	protocol := pointProtocol(info, query.Get("channel"))
	requested := query.Get("kind")
	if info.View.PointOrder == "address" {
		kind := selectedPointKind(protocol, requested, info.View.PointKinds)
		if kind != requested && query.Get("device") != "" {
			query.Set("kind", kind)
			query.Set("offset", "0")
			info2, err := observe.Fetch(ctx, socket, "/v1/view?"+query.Encode())
			return info2, kind, err
		}
		return info, kind, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return legacyPointPage(query, info, func(offset int) (observe.Info, error) {
		q := url.Values{}
		for key, values := range query {
			q[key] = append([]string(nil), values...)
		}
		q.Set("filter", "")
		q.Set("offset", fmt.Sprint(offset))
		return observe.Fetch(ctx, socket, "/v1/view?"+q.Encode())
	})
}

func pointLess(a, b core.Point) bool {
	if a.Address != b.Address {
		return a.Address < b.Address
	}
	return a.Kind < b.Kind
}

type pointHeap []core.Point

func (p pointHeap) Len() int           { return len(p) }
func (p pointHeap) Less(i, j int) bool { return pointLess(p[j], p[i]) }
func (p pointHeap) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p *pointHeap) Push(v any)        { *p = append(*p, v.(core.Point)) }
func (p *pointHeap) Pop() any          { a := *p; v := a[len(a)-1]; *p = a[:len(a)-1]; return v }

func legacyPointPage(query url.Values, info observe.Info, fetchPage func(int) (observe.Info, error)) (observe.Info, string, error) {
	protocol := pointProtocol(info, query.Get("channel"))
	requested := query.Get("kind")
	offset := 0
	fmt.Sscan(query.Get("offset"), &offset)
	offset = max(0, offset)
	filter := strings.ToLower(query.Get("filter"))
	first := info
	var err error
	if filter != "" || offset != 0 {
		first, err = fetchPage(0)
		if err != nil {
			return info, "", err
		}
	}
	counts := map[string]int{}
	kind := requested
	points := &pointHeap{}
	matches := 0
	total := first.View.PointMatches
	limit := min(offset, total) + 100
	for page, scan := first, 0; ; scan += 100 {
		if scan > 0 {
			page, err = fetchPage(scan)
			if err != nil {
				return info, "", err
			}
		}
		for _, p := range page.View.Points {
			counts[p.Kind]++
			if requested == "" {
				chosen := selectedPointKind(protocol, "", counts)
				if chosen != kind {
					kind = chosen
					*points = nil
					matches = 0
				}
			}
			if p.Kind != kind {
				continue
			}
			if filter != "" && !strings.Contains(strings.ToLower(fmt.Sprintf("%d %x %s %s %g %d", p.Address, p.Address, p.Kind, p.Name, p.Value, p.Quality)), filter) {
				continue
			}
			matches++
			if points.Len() < limit {
				heap.Push(points, p)
			} else if pointLess(p, (*points)[0]) {
				(*points)[0] = p
				heap.Fix(points, 0)
			}
		}
		if scan+len(page.View.Points) >= total {
			break
		}
		if len(page.View.Points) == 0 {
			return info, "", fmt.Errorf("点位分页发生变化，请等待重新读取")
		}
	}
	if kind == "" {
		kind = selectedPointKind(protocol, requested, counts)
	}
	if resolved := selectedPointKind(protocol, requested, counts); resolved != kind {
		q := url.Values{}
		for key, values := range query {
			q[key] = append([]string(nil), values...)
		}
		q.Set("kind", resolved)
		q.Set("offset", "0")
		return legacyPointPage(q, first, fetchPage)
	}
	sort.Slice(*points, func(i, j int) bool { return pointLess((*points)[i], (*points)[j]) })
	start := min(offset, points.Len())
	info.View.Points = append([]core.Point(nil), (*points)[start:min(start+100, points.Len())]...)
	info.View.PointMatches, info.View.PointKinds = matches, counts
	return info, kind, nil
}

func (m *model) pointNavigation(width int) string {
	var labels []string
	selectedIndex := -1
	for _, kind := range m.pointKinds() {
		if kind == m.pointKind {
			selectedIndex = len(labels)
		}
		labels = append(labels, fmt.Sprintf("%s %d", translated(kind, kindLabels), m.detail.View.PointKinds[kind]))
	}
	if uniseg.StringWidth(" "+strings.Join(labels, "  ")+" ") > width {
		return navigation([]string{fmt.Sprintf("%s %d", translated(m.pointKind, kindLabels), m.detail.View.PointKinds[m.pointKind]), "←/→ 切换分类"}, 0, width)
	}
	return navigation(labels, selectedIndex, width)
}
