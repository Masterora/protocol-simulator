package projectfile

import (
	"encoding/json"
	"reflect"
	"testing"

	"gytest-go/internal/core"
)

func TestPointBindingsPersistAndRejectInvalidTargetsWithoutChangingOtherBindings(t *testing.T) {
	for _, protocol := range []string{"iec104", "modbus-tcp", "modbus-rtu"} {
		t.Run(protocol, func(t *testing.T) {
			points := []core.Point{{Kind: "yk", Address: 100}, {Kind: "yk", Address: 101}, {Kind: "yt", Address: 200}, {Kind: "yx", Address: 1}, {Kind: "yx", Address: 2}, {Kind: "yc", Address: 10}}
			if protocol != "iec104" {
				for i := range points {
					points[i].Tab = points[i].Kind
					switch points[i].Kind {
					case "yk":
						points[i].Kind = "coil"
					case "yx":
						points[i].Kind = "discrete"
					default:
						points[i].Kind = "holding"
					}
				}
			}
			key := func(i int) core.PointKey { return core.PointKey{Kind: points[i].Kind, Address: points[i].Address} }
			c := core.Channel{ID: "c", Name: "C", Protocol: protocol, Role: "server", Transport: "tcp-server", Address: "127.0.0.1:2404", Devices: []core.Device{{ID: "d", Name: "D", Unit: 1, CommonAddress: 1, Points: points}}}
			c.Defaults()
			c.Devices[0].Points[1].Feedback = "2"
			original := core.Project{Version: 2, Name: "P", Channels: []core.Channel{c}}
			source := key(0)
			request := Request{Resource: "binding", Action: "edit", Channel: "c", Device: "d", Point: &source}
			mappings := []core.ControlMapping{{Source: source, Target: key(3)}, {Source: source, Target: key(4), Invert: true}}
			request.Fields = Fields(map[string]any{"mappings": mappings})
			next, _, err := Apply(original, request)
			if err != nil {
				t.Fatal(err)
			}
			d := next.Channels[0].Devices[0]
			cfg := core.ControlConfiguration(d, "yk")
			if len(cfg.Mappings) != 3 || len(core.ControlConfiguration(original.Channels[0].Devices[0], "yk").Mappings) != 1 {
				t.Fatal("sibling legacy binding lost or original mutated", cfg)
			}
			v, err := ReadView(Document{Project: next}, request)
			if err != nil || len(v.BindingTargets) != 2 {
				t.Fatal(v, err)
			}
			var read []core.ControlMapping
			json.Unmarshal(v.Value["mappings"], &read)
			if !reflect.DeepEqual(read, mappings) {
				t.Fatal(read)
			}
			for _, bad := range []any{nil, []core.ControlMapping{{Source: key(1), Target: key(3)}}, []core.ControlMapping{{Source: source, Target: key(5)}}, append(mappings, mappings[0])} {
				request.Fields = Fields(map[string]any{"mappings": bad})
				if _, _, err = Apply(next, request); err == nil {
					t.Fatal("accepted invalid binding", bad)
				}
			}
			request.Fields = Fields(map[string]any{"mappings": []core.ControlMapping{}})
			cleared, _, err := Apply(next, request)
			if err != nil || len(core.ControlConfiguration(cleared.Channels[0].Devices[0], "yk").Mappings) != 1 {
				t.Fatal("clear removed sibling or resurrected legacy", err)
			}
			source = key(2)
			request.Fields = Fields(map[string]any{"mappings": []core.ControlMapping{{Source: source, Target: key(5)}}})
			if _, _, err = Apply(next, request); err != nil {
				t.Fatal(err)
			}
			request.Fields = Fields(map[string]any{"mappings": []core.ControlMapping{{Source: source, Target: key(5), Invert: true}}})
			if _, _, err = Apply(next, request); err == nil {
				t.Fatal("YT inverse accepted")
			}
		})
	}
}

func TestPointEditBindingsAreAtomicWithOrdinaryFields(t *testing.T) {
	source := core.PointKey{Kind: "yk", Address: 100}
	c := core.Channel{ID: "c", Name: "C", Protocol: "iec104", Role: "server", Transport: "tcp-server", Address: "127.0.0.1:2404", Devices: []core.Device{{ID: "d", Name: "D", CommonAddress: 1, Points: []core.Point{{Kind: "yk", Address: 100, Name: "original"}, {Kind: "yx", Address: 1}}}}}
	c.Defaults()
	original := core.Project{Version: 2, Name: "P", Channels: []core.Channel{c}}
	request := Request{Resource: "point", Action: "edit", Channel: "c", Device: "d", Point: &source, Fields: Fields(map[string]any{"name": "edited", "mappings": []core.ControlMapping{{Source: source, Target: core.PointKey{Kind: "yx", Address: 1}, Invert: true}}})}
	next, _, err := Apply(original, request)
	if err != nil {
		t.Fatal(err)
	}
	view, err := ReadView(Document{Project: next}, request)
	if err != nil || len(view.BindingTargets) != 1 || view.Value["mappings"] == nil || string(view.Value["name"]) != `"edited"` {
		t.Fatal(view, err)
	}
	if original.Channels[0].Devices[0].Points[0].Name != "original" || original.Channels[0].Devices[0].Configuration != nil {
		t.Fatal("input mutated")
	}
	request.Fields["mappings"] = Fields(map[string]any{"mappings": []core.ControlMapping{{Source: source, Target: core.PointKey{Kind: "yx", Address: 999}}}})["mappings"]
	if _, _, err = Apply(original, request); err == nil {
		t.Fatal("invalid combined edit accepted")
	}
	if original.Channels[0].Devices[0].Points[0].Name != "original" {
		t.Fatal("invalid binding partially edited point")
	}
	for _, input := range []string{`null`, `[] true`, `[{"source":{"kind":"yk","address":100},"target":{"kind":"yx","address":1},"unexpected":true}]`} {
		if _, err = ParseField("point", "mappings", input); err == nil {
			t.Fatal("invalid JSON accepted", input)
		}
	}
}
