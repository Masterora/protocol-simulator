package observe

import (
	"context"
	"fmt"
	"testing"

	"gytest-go/internal/core"
)

func TestOrderedKindPagesAreNumericBoundedAndNeverReorderProject(t *testing.T) {
	engine := core.NewEngine()
	var points []core.Point
	for i := 204; i >= 0; i-- {
		points = append(points, core.Point{Kind: "yc", Address: uint32(i), Name: "shared"})
	}
	points = append(points, core.Point{Kind: "yx", Address: 1, Name: "shared"}, core.Point{Kind: "yk", Address: 1, Name: "shared"}, core.Point{Kind: "yt", Address: 1, Name: "shared"})
	_, err := engine.UpsertChannel(core.Channel{ID: "c", Name: "test", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:2404", Unit: 1, Points: points})
	if err != nil {
		t.Fatal(err)
	}
	server, err := Start(engine, runtimeDirectory(t))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	for _, offset := range []int{0, 100, 200} {
		info, err := Fetch(context.Background(), server.Socket(), fmt.Sprintf("/v1/view?channel=c&device=device-1&kind=yc&order=address&filter=shared&offset=%d", offset))
		if err != nil || info.View.PointOrder != "address" || info.View.PointMatches != 205 || info.View.PointTotal != 208 || info.View.PointKinds["yx"] != 1 {
			t.Fatal(info, err)
		}
		if len(info.View.Points) != min(100, 205-offset) {
			t.Fatal(info.View)
		}
		for i, p := range info.View.Points {
			if p.Kind != "yc" || p.Address != uint32(offset+i) {
				t.Fatal(p, offset, i)
			}
		}
	}
	info, err := Fetch(context.Background(), server.Socket(), "/v1/view?channel=c&device=device-1&kind=yc&order=address&offset=9223372036854775807")
	if err != nil || len(info.View.Points) != 0 {
		t.Fatal(info, err)
	}
	if engine.Snapshot().Project.Channels[0].Devices[0].Points[0].Address != 204 {
		t.Fatal("observation mutated project order")
	}
}
