package core

import (
	"io"
	"reflect"
	"testing"
)

func TestInterrogationBatchesInterleavedPointTable(t *testing.T) {
	for _, sq := range []string{"非顺序SQ0", "顺序SQ1"} {
		t.Run(sq, func(t *testing.T) {
			points := []Point{}
			for i := 249; i >= 0; i-- {
				points = append(points,
					Point{Kind: "yx", Address: uint32(3 + i), Value: float64(i % 2), WireType: 30, Quality: 16, Timestamp: "2026-10-26T12:00:00+08:00"},
					Point{Kind: "yc", Address: uint32(17016 + i), Value: float64(i) + .125, WireType: 13, Quality: 64},
					Point{Kind: "yk", Address: uint32(24578 + i)},
					Point{Kind: "yt", Address: uint32(26114 + i)})
			}
			c := probeChannel(points)
			c.Devices[0].Configuration = &DeviceConfiguration{Values: map[string]string{"sendWindow": "1", "cbVSQ": sq}}
			e := NewEngine()
			e.project = Project{Version: 2, Name: "interleaved", Channels: []Channel{c}}
			s := newSession(e, c)
			e.running[c.ID] = s
			sink := &controlSink{}
			p := &peer{s: s, conn: sink, transfer: true, done: make(chan struct{})}
			s.peers[p] = true
			if err := p.handleASDU(interrogation(1, 6), false); err != nil {
				t.Fatal(err)
			}
			for len(p.unacked) > 0 {
				p.mu.Lock()
				err := p.ackLocked(p.tx)
				p.mu.Unlock()
				if err != nil {
					t.Fatal(err)
				}
			}
			var frames [][]byte
			var reported []Point
			for {
				b, err := Read104(sink)
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				frames = append(frames, b)
				if b[6] == 100 {
					continue
				}
				if b[8] != 20 {
					t.Fatalf("unexpected interrogation cause: %x", b)
				}
				decoded, err := Decode104Points(b[6:], 1)
				if err != nil {
					t.Fatal(err)
				}
				reported = append(reported, decoded...)
			}
			if len(frames) != 36 || len(reported) != 500 {
				t.Fatalf("interleaved 1000-point table: frames=%d, telemetry=%d; want 36 frames, 500 values", len(frames), len(reported))
			}
			if frames[0][6] != 100 || frames[0][8] != 7 || frames[len(frames)-1][6] != 100 || frames[len(frames)-1][8] != 10 {
				t.Fatal("interrogation acknowledgement/end order changed")
			}
			for i, point := range reported {
				kind, base, quality, typ, value := "yx", 3, byte(16), byte(1), float64(i%2)
				if i >= 250 {
					kind, base, quality, typ, value = "yc", 17016, 64, 13, float64(i-250)+.125
				}
				if point.Kind != kind || point.Address != uint32(base+i%250) || point.Quality != quality || point.WireType != typ || point.Value != value || point.Timestamp != "" {
					t.Fatalf("point %d: %+v", i, point)
				}
			}
			if !reflect.DeepEqual(e.project.Channels[0].Devices[0].Points, points) {
				t.Fatal("snapshot report reordered or modified the stored point table")
			}
		})
	}
}
