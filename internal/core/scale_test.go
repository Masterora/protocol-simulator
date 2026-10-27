package core

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Scale fixtures reach the existing per-device limit without changing product
// defaults. Different kinds deliberately reuse addresses: kind is part of a key.
func scaleIECPoints(perKind int, mixed bool) []Point {
	out := make([]Point, 0, 4*perKind)
	for i := 0; i < perKind; i++ {
		for k, kind := range []string{"yx", "yc", "yk", "yt"} {
			p := Point{Kind: kind, Tab: kind, Address: uint32(k*20000 + i*2), Name: fmt.Sprintf("%s-%05d-点", kind, i), Value: float64(i % 2), Quality: 16}
			if kind == "yc" {
				p.WireType = 13
				p.Value = float64(i%100) + .25
				if mixed {
					p.WireType = []byte{9, 11, 13, 21, 34, 35, 36}[i%7]
					if p.WireType == 9 || p.WireType == 21 || p.WireType == 34 {
						p.Value = .25
					}
					if p.WireType == 11 || p.WireType == 35 {
						p.Value = float64(i % 100)
					}
					if p.WireType == 21 {
						p.Quality = 0
					}
				}
			}
			if kind == "yx" {
				p.WireType = 1
				if mixed && i%2 == 1 {
					p.WireType = 30
				}
			}
			if timeTagged(p.WireType) {
				p.Timestamp = "2026-10-26T10:00:00+08:00"
			}
			if kind == "yk" || kind == "yt" {
				p.Quality = 0
				p.Value = 0
			}
			out = append(out, p)
		}
	}
	return out
}
func scaleChannel(id, protocol, role string, points []Point) Channel {
	return Channel{ID: id, Name: id, Protocol: protocol, Role: role, Transport: map[string]string{"server": "tcp-server", "client": "tcp-client"}[role], Address: map[string]string{"server": "127.0.0.1:0", "client": "127.0.0.1:1"}[role], TimeoutMS: 3000, Devices: []Device{{ID: "device", Name: "device", Unit: 1, CommonAddress: 1, Points: points}}}
}
func scaleWait(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	until := time.Now().Add(timeout)
	for time.Now().Before(until) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("scale condition timed out after", timeout)
}
func scalePointEqual(a, b Point) bool {
	if a.Kind != b.Kind || a.Address != b.Address || a.WireType != b.WireType || a.Quality != b.Quality || math.Abs(a.Value-b.Value) > 1e-5 {
		return false
	}
	if a.Timestamp != "" {
		x, e := time.Parse(time.RFC3339Nano, a.Timestamp)
		y, f := time.Parse(time.RFC3339Nano, b.Timestamp)
		return e == nil && f == nil && x.UnixMilli() == y.UnixMilli()
	}
	return true
}
func TestScaleMaximumDeviceBatchAndView(t *testing.T) {
	e := NewEngine()
	defer e.StopAll()
	points := scaleIECPoints(2500, false)
	c := scaleChannel("maximum", "iec104", "server", points)
	id, err := e.UpsertChannel(c)
	if err != nil {
		t.Fatal(err)
	}
	v := e.View(ViewRequest{Channel: id, Device: "device"})
	if len(v.Points) != 10000 {
		t.Fatal("maximum table truncated", len(v.Points))
	}
	start := time.Now()
	q := byte(0x90)
	result, err := e.SetTelemetryRange(id, "device", TelemetryRangeEdit{Category: "yc", Points: "20000-24998", Mode: "set", Value: 220.5, Quality: &q})
	if err != nil || result.Points != 2500 {
		t.Fatal(result, err)
	}
	next := e.View(viewRequest(v))
	if next.FullPoints || len(next.Points) != 2500 {
		t.Fatal("incorrect incremental batch", next.FullPoints, len(next.Points))
	}
	d, _ := e.Device(id, "device")
	for _, p := range d.Points {
		if p.Kind == "yc" {
			if p.Value != 220.5 || p.Quality != q {
				t.Fatal("batch lost point", p)
			}
		} else if p.Value != points[int(p.Address%20000)/2*4+map[string]int{"yx": 0, "yk": 2, "yt": 3}[p.Kind]].Value {
			t.Fatal("unselected kind changed", p)
		}
	}
	before := e.Snapshot()
	if _, err = e.SetTelemetryRange(id, "device", TelemetryRangeEdit{Category: "yc", Points: "20000-24998", Mode: "set", Value: math.Inf(1)}); err == nil {
		t.Fatal("invalid large batch accepted")
	}
	after := e.Snapshot()
	if !reflect.DeepEqual(before.Project, after.Project) {
		t.Fatal("invalid batch changed data")
	}
	extra := append(append([]Point{}, points...), Point{Kind: "yc", Address: 0xffffff, WireType: 13})
	if err = e.SetDevicePoints(id, "device", extra); err == nil {
		t.Fatal("10001st point accepted")
	}
	if err = e.SetDevicePoints(id, "device", []Point{{Kind: "yc", Address: 0x1000000}}); err == nil {
		t.Fatal("24-bit overflow accepted")
	}
	t.Logf("10000 points, 2500-point batch and delta verified in %s", time.Since(start))
}

func TestScaleIEC104FullInterrogationAndControlMappings(t *testing.T) {
	start := time.Now()
	e := NewEngine()
	defer e.StopAll()
	points := scaleIECPoints(2500, true)
	server := scaleChannel("scale-server", "iec104", "server", points)
	yk, yt := ControlSimulation{Mode: "normal"}, ControlSimulation{Mode: "normal"}
	for i := 0; i < 2500; i++ {
		yk.Mappings = append(yk.Mappings, ControlMapping{Source: PointKey{"yk", uint32(40000 + i*2)}, Target: PointKey{"yx", uint32(i * 2)}, Invert: i%2 == 1})
		yt.Mappings = append(yt.Mappings, ControlMapping{Source: PointKey{"yt", uint32(60000 + i*2)}, Target: PointKey{"yc", uint32(20000 + i*2)}})
	}
	server.Devices[0].Configuration = &DeviceConfiguration{Version: "标准版", Controls: map[string]ControlSimulation{"yk": yk, "yt": yt}}
	if _, err := e.UpsertChannel(server); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(server.ID); err != nil {
		t.Fatal(err)
	}
	addr := e.Snapshot().States[0].Address
	seed := append([]Point{}, points...)
	for i := range seed {
		seed[i].Value = 0
		seed[i].Quality = 0
		seed[i].Timestamp = ""
		seed[i].WireType = 0
	}
	client := scaleChannel("scale-client", "iec104", "client", seed)
	client.Address = addr
	if _, err := e.UpsertChannel(client); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(client.ID); err != nil {
		t.Fatal(err)
	}
	scaleWait(t, 10*time.Second, func() bool {
		v := e.View(ViewRequest{Channel: client.ID})
		for _, s := range v.States {
			if s.ID == client.ID {
				return s.ReadyPeers > 0
			}
		}
		return false
	})
	if err := e.InterrogateDevice(client.ID, "device"); err != nil {
		t.Fatal(err)
	}
	scaleWait(t, 20*time.Second, func() bool {
		d, _ := e.Device(client.ID, "device")
		if len(d.Points) != 10000 {
			return false
		}
		for i, p := range points {
			if p.Kind == "yx" || p.Kind == "yc" {
				expected := p
				switch expected.WireType {
				case 30:
					expected.WireType = 1
				case 34:
					expected.WireType = 9
				case 35:
					expected.WireType = 11
				case 36:
					expected.WireType = 13
				}
				expected.Timestamp = ""
				if !scalePointEqual(expected, d.Points[i]) {
					return false
				}
			}
		}
		return true
	})
	gi := time.Since(start)
	commands := 0
	// Exercise the whole command address distribution, including the final point,
	// with both selected and direct commands; other formats have full suite vectors.
	for n := 0; n < 128; n++ {
		i := n * 2499 / 127
		for _, kind := range []string{"yk", "yt"} {
			typ, value, addr := byte(45), float64(1), uint32(40000+i*2)
			target := PointKey{"yx", uint32(i * 2)}
			want := float64(1)
			if i%2 == 1 {
				want = 0
			}
			if kind == "yt" {
				typ = 50
				addr = uint32(60000 + i*2)
				target = PointKey{"yc", uint32(20000 + i*2)}
				value = float64(123)
				if p := points[i*4+1]; p.WireType == 9 || p.WireType == 21 || p.WireType == 34 {
					value = .5
				} else if p.WireType == 13 || p.WireType == 36 {
					value = 220.5
				}
				want = value
			}
			req := Control104Request{Type: typ, Address: addr, Value: value, Operation: "execute", Direct: n%2 == 0, TimeoutMS: 3000}
			if !req.Direct {
				req.Operation = "select"
				if _, err := e.Command104Device(client.ID, "device", req); err != nil {
					t.Fatal(n, kind, err)
				}
				commands++
				req.Operation = "execute"
			}
			if _, err := e.Command104Device(client.ID, "device", req); err != nil {
				t.Fatal(n, kind, err)
			}
			commands++
			d, _ := e.Device(server.ID, "device")
			got, ok := findRemotePoint(d.Points, target)
			if !ok || got.Value != want {
				t.Fatal("mapping failed", n, kind, got, want)
			}
		}
	}
	// The first and last mappings must survive save/load along with every point.
	path := filepath.Join(t.TempDir(), "large-four-remote.json")
	if err := e.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded := NewEngine()
	defer loaded.StopAll()
	if err := loaded.Load(path); err != nil {
		t.Fatal(err)
	}
	d, _ := loaded.Device(server.ID, "device")
	if len(d.Points) != 10000 || !reflect.DeepEqual(d.Configuration.Controls, server.Devices[0].Configuration.Controls) {
		t.Fatal("large recipe not persisted")
	}
	e.Stop(client.ID)
	if err := e.Start(client.ID); err != nil {
		t.Fatal(err)
	}
	scaleWait(t, 10*time.Second, func() bool {
		v := e.View(ViewRequest{Channel: client.ID})
		for _, s := range v.States {
			if s.ID == client.ID {
				return s.ReadyPeers > 0
			}
		}
		return false
	})
	if err := e.InterrogateDevice(client.ID, "device"); err != nil {
		t.Fatal(err)
	}
	t.Logf("10000 points/device, 5000 telemetry GI with 9/11/13/21/30/34/35/36 types, 5000 mappings, %d command PDUs, reconnect and roundtrip; GI=%s total=%s", commands, gi, time.Since(start))
}

func TestScaleLargeProjectPersistence(t *testing.T) {
	if os.Getenv("GYLAB_SCALE_PROJECT") == "" {
		t.Skip("explicit large-project acceptance")
	}
	start := time.Now()
	e := NewEngine()
	defer e.StopAll()
	p := Project{Name: "scale", Version: 2, Channels: []Channel{}}
	channels, devices := 64, 1
	protocol := "iec104"
	if os.Getenv("GYLAB_SCALE_PROJECT") == "modbus" {
		channels, devices, protocol = 2, 127, "modbus-tcp"
	}
	points := scaleIECPoints(2500, false)
	for i := 0; i < channels; i++ {
		c := scaleChannel(fmt.Sprintf("scale-%02d", i), protocol, "server", nil)
		c.Devices = nil
		for j := 0; j < devices; j++ {
			d := Device{ID: fmt.Sprintf("device-%03d", j), Name: fmt.Sprintf("device-%03d", j), Unit: byte(j + 1), CommonAddress: 1, Points: append([]Point{}, points...)}
			if protocol != "iec104" {
				for k := range d.Points {
					q := &d.Points[k]
					q.WireType = 0
					q.Timestamp = ""
					q.Quality = 0
					q.Kind = map[string]string{"yx": "discrete", "yc": "holding", "yk": "coil", "yt": "holding"}[q.Kind]
					q.Value = math.Trunc(q.Value)
					q.Address = uint32(k / 4)
					if q.Tab == "yt" {
						q.Address += 10000
					}
				}
			}
			c.Devices = append(c.Devices, d)
		}
		p.Channels = append(p.Channels, c)
	}
	if err := e.ReplaceProject(p, ""); err != nil {
		t.Fatal(err)
	}
	fixture := time.Since(start)
	v := e.View(ViewRequest{Channel: p.Channels[len(p.Channels)-1].ID, Device: p.Channels[len(p.Channels)-1].Devices[devices-1].ID})
	if len(v.Points) != 10000 {
		t.Fatal("selected device incomplete")
	}
	idle := e.View(viewRequest(v))
	b, err := json.Marshal(idle)
	if err != nil || len(b) > 20000 || len(idle.Points) != 0 {
		t.Fatal("idle view copied unrelated point data", len(b), err)
	}
	path := filepath.Join(t.TempDir(), "large-project.json")
	if out := os.Getenv("GYLAB_SCALE_OUTPUT"); out != "" {
		if err := os.MkdirAll(out, 0755); err != nil {
			t.Fatal(err)
		}
		path = filepath.Join(out, protocol+"-large.json")
	}
	beforeSave := time.Now()
	if err := e.Save(path); err != nil {
		t.Fatal("large project save", channels*devices*10000, "points:", err)
	}
	saved := time.Since(beforeSave)
	loaded := NewEngine()
	defer loaded.StopAll()
	beforeLoad := time.Now()
	if err := loaded.Load(path); err != nil {
		t.Fatal(err)
	}
	load := time.Since(beforeLoad)
	count := 0
	for ci, c := range loaded.project.Channels {
		for di, d := range c.Devices {
			count += len(d.Points)
			if !reflect.DeepEqual(d.Points, p.Channels[ci].Devices[di].Points) {
				t.Fatalf("roundtrip changed points channel=%d device=%d", ci, di)
			}
		}
	}
	if count != channels*devices*10000 {
		t.Fatal("roundtrip lost points", count)
	}
	last, _ := loaded.Device(v.Channel, v.Device)
	if len(last.Points) != 10000 || !reflect.DeepEqual(last.Points, p.Channels[channels-1].Devices[devices-1].Points) {
		t.Fatal("last device changed during persistence")
	}
	stat, _ := os.Stat(path)
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	t.Logf("SCALE project=%s channels=%d devices=%d points=%d bytes=%d fixture=%s save=%s load=%s idleJSON=%d heapMiB=%d", protocol, channels, channels*devices, count, stat.Size(), fixture, saved, load, len(b), m.HeapAlloc>>20)

	if protocol == "modbus-tcp" {
		hashFile := func() string {
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			h := sha256.New()
			if _, err := io.Copy(h, f); err != nil {
				t.Fatal(err)
			}
			return fmt.Sprintf("%x", h.Sum(nil))
		}
		original := hashFile()
		for i := range p.Channels {
			d := p.Channels[i].Devices[0]
			d.ID = "over-limit"
			d.Unit = 128
			p.Channels[i].Devices = append(p.Channels[i].Devices, d)
		}
		if err := e.ReplaceProject(p, ""); err != nil {
			t.Fatal(err)
		}
		if err := e.Save(path); err == nil || !strings.Contains(err.Error(), "512MB") {
			t.Fatal("oversized save accepted or wrong error", err)
		}
		if hashFile() != original {
			t.Fatal("oversized save replaced original project")
		}
		t.Log("SCALE 2560000-point save exceeds 512MiB; rejected without replacing the saved 2540000-point file")
	}
}

func BenchmarkScaleBatchSet10000(b *testing.B) {
	e := NewEngine()
	c := scaleChannel("b", "iec104", "server", make([]Point, 10000))
	for i := range c.Devices[0].Points {
		c.Devices[0].Points[i] = Point{Kind: "yc", Address: uint32(i), WireType: 13}
	}
	if _, err := e.UpsertChannel(c); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.SetTelemetryRange(c.ID, "device", TelemetryRangeEdit{Category: "yc", Points: "0-9999", Mode: "set", Value: float64(i)}); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkScaleReceive10000(b *testing.B) {
	e := NewEngine()
	points := make([]Point, 10000)
	for i := range points {
		points[i] = Point{Kind: "yc", Address: uint32(i), WireType: 13, Value: 1}
	}
	c := scaleChannel("b", "iec104", "client", points)
	if _, err := e.UpsertChannel(c); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.applyDevicePoints(c.ID, "device", points)
	}
}

// Prevent accidental dense expansion of sparse 24-bit and Modbus boundaries.
func TestScaleSparseAndBoundarySelections(t *testing.T) {
	for _, max := range []uint32{65535, 0xffffff} {
		v, err := ParsePointSelection(fmt.Sprintf("0，2-9998，%d", max), max)
		if err != nil || len(v) != 9999 || v[len(v)-1] != max {
			t.Fatal(len(v), err)
		}
		if _, err = ParsePointSelection("0-10000", max); err == nil {
			t.Fatal("10001-point expansion accepted")
		}
	}
	points := []Point{{Kind: "yc", Address: 0, WireType: 13}, {Kind: "yc", Address: 0xffffff, WireType: 13}}
	e := NewEngine()
	c := scaleChannel("boundaries", "iec104", "server", points)
	if _, err := e.UpsertChannel(c); err != nil {
		t.Fatal(err)
	}
	if _, err := e.SetTelemetryRange(c.ID, "device", TelemetryRangeEdit{Category: "yc", Points: "0，16777215", Mode: "set", Value: -220.5}); err != nil {
		t.Fatal(err)
	}
	d, _ := e.Device(c.ID, "device")
	if len(d.Points) != 2 || d.Points[0].Value != -220.5 || d.Points[1].Value != -220.5 {
		t.Fatal(d.Points)
	}
	if strings.Contains(fmt.Sprint(d.Points), "NaN") {
		t.Fatal("invalid values")
	}
}

func TestScaleProjectSizeGuardPreservesState(t *testing.T) {
	e := NewEngine()
	c := scaleChannel("guard", "iec104", "server", []Point{{Kind: "yc", Address: 1, WireType: 13, Value: 7}})
	if _, err := e.UpsertChannel(c); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "too-large.json")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(maxProjectFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	before := e.Snapshot()
	if err = e.LoadReplacing(path); err == nil || !strings.Contains(err.Error(), "512MB") {
		t.Fatal("size limit not enforced", err)
	}
	if !reflect.DeepEqual(before, e.Snapshot()) {
		t.Fatal("oversized load changed current project")
	}
	for i, invalid := range []string{`{"version":2,"name":"bad","channels":[],"unknown":true}`, `{"version":2,"name":"bad","channels":[]} {}`, `{"version":2,`} {
		p := filepath.Join(t.TempDir(), fmt.Sprint(i)+".json")
		os.WriteFile(p, []byte(invalid), 0600)
		if err = e.LoadReplacing(p); err == nil {
			t.Fatal("invalid file accepted", i)
		}
		if !reflect.DeepEqual(before, e.Snapshot()) {
			t.Fatal("invalid load changed current project", i)
		}
	}
}

func scaleModbusDevice(unit byte, count int, mappings bool) Device {
	d := Device{ID: fmt.Sprintf("unit-%03d", unit), Name: fmt.Sprintf("unit-%03d", unit), Unit: unit, CommonAddress: 1}
	for i := 0; i < count; i++ {
		for group := 0; group < 5; group++ {
			p := Point{Address: uint32(i), Name: fmt.Sprintf("u%d-g%d-p%d", unit, group, i)}
			switch group {
			case 0:
				p.Kind = "discrete"
				p.Tab = "yx"
				p.Value = float64(i % 2)
			case 1:
				p.Kind = "coil"
				p.Tab = "yk"
				p.Value = float64(i % 2)
			case 2:
				p.Kind = "holding"
				p.Tab = "yc"
				p.Value = float64(int(unit)*1000 + i)
			case 3:
				p.Kind = "holding"
				p.Tab = "yt"
				p.Address += 10000
			case 4:
				p.Kind = "input"
				p.Tab = "yc"
				p.Value = float64(int(unit)*100 + i)
			}
			d.Points = append(d.Points, p)
		}
	}
	if mappings {
		yk, yt := ControlSimulation{Mode: "normal"}, ControlSimulation{Mode: "normal"}
		for i := 0; i < count; i++ {
			yk.Mappings = append(yk.Mappings, ControlMapping{Source: PointKey{"coil", uint32(i)}, Target: PointKey{"discrete", uint32(i)}})
			yt.Mappings = append(yt.Mappings, ControlMapping{Source: PointKey{"holding", uint32(10000 + i)}, Target: PointKey{"holding", uint32(i)}})
		}
		d.Configuration = &DeviceConfiguration{Version: "TCP版", Controls: map[string]ControlSimulation{"yk": yk, "yt": yt}}
	}
	return d
}
func TestScaleModbusFullTablesMultiUnitAndBatchMappings(t *testing.T) {
	units := 2
	if os.Getenv("GYLAB_SCALE_NETWORK") != "" {
		units = 16
	}
	scaleModbusTables(t, "modbus-tcp", units)
}
func TestScaleSerialFullTablesAndMappings(t *testing.T) {
	if os.Getenv("GYLAB_SERIAL_A") == "" || os.Getenv("GYLAB_SERIAL_B") == "" {
		t.Skip("requires disposable PTYs")
	}
	scaleModbusTables(t, "modbus-rtu", 2)
}
func scaleModbusTables(t *testing.T, protocol string, units int) {
	t.Helper()
	start := time.Now()
	e := NewEngine()
	defer e.StopAll()
	server := scaleChannel("mb-server", protocol, "server", nil)
	if protocol == "modbus-rtu" {
		server.Transport = "serial"
		server.Address = os.Getenv("GYLAB_SERIAL_A")
	}
	server.Devices = nil
	for u := 1; u <= units; u++ {
		d := scaleModbusDevice(byte(u), 2000, u == units)
		if protocol == "modbus-rtu" && d.Configuration != nil {
			d.Configuration.Version = "标准版(RTU)"
		}
		server.Devices = append(server.Devices, d)
	}
	if _, err := e.UpsertChannel(server); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(server.ID); err != nil {
		t.Fatal(err)
	}
	client := server
	client.ID, client.Name, client.Role, client.Transport = "mb-client", "mb-client", "client", "tcp-client"
	client.Address = e.View(ViewRequest{}).States[0].Address
	if protocol == "modbus-rtu" {
		client.Transport = "serial"
		client.Address = os.Getenv("GYLAB_SERIAL_B")
	}
	client.Devices = append([]Device{}, server.Devices...)
	for i := range client.Devices {
		client.Devices[i].Configuration = nil
		client.Devices[i].Points = append([]Point{}, client.Devices[i].Points...)
		for j := range client.Devices[i].Points {
			client.Devices[i].Points[j].Value = 0
		}
	}
	if _, err := e.UpsertChannel(client); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(client.ID); err != nil {
		t.Fatal(err)
	}
	scaleWait(t, 10*time.Second, func() bool {
		for _, s := range e.View(ViewRequest{Channel: client.ID}).States {
			if s.ID == client.ID {
				return s.Peers == 1
			}
		}
		return false
	})
	requests := 0
	for _, d := range server.Devices {
		for _, fc := range []byte{1, 2, 3, 4} {
			selection := "0-1999"
			if fc == 3 {
				selection += "，10000-11999"
			}
			v, err := e.ReadDeviceModbusPoints(client.ID, d.ID, fc, selection)
			if err != nil {
				t.Fatal(d.ID, fc, err)
			}
			requests += v.Requests
		}
		actual, _ := e.Device(client.ID, d.ID)
		for i, p := range d.Points {
			if p.Kind != actual.Points[i].Kind || p.Address != actual.Points[i].Address || p.Value != actual.Points[i].Value {
				t.Fatal("unit/full table mismatch", d.ID, p, actual.Points[i])
			}
		}
	}
	target := server.Devices[units-1].ID
	r, err := e.WriteDeviceModbusPoints(client.ID, target, 15, "0-1999", 1)
	if err != nil || r.Requests != 2 {
		t.Fatal("large coil segmentation", r, err)
	}
	requests += r.Requests
	r, err = e.WriteDeviceModbusPoints(client.ID, target, 16, "10000-11999", 77)
	if err != nil || r.Requests != 17 {
		t.Fatal("large register segmentation", r, err)
	}
	requests += r.Requests
	got, _ := e.Device(server.ID, target)
	for _, p := range got.Points {
		if p.Kind == "discrete" && p.Value != 1 || p.Kind == "holding" && p.Tab == "yc" && p.Value != 77 {
			t.Fatal("large batch mapping lost target", p)
		}
	}
	other, _ := e.Device(server.ID, server.Devices[0].ID)
	if !reflect.DeepEqual(other.Points, server.Devices[0].Points) {
		t.Fatal("batch write crossed unit boundary")
	}
	before, _ := e.Device(server.ID, target)
	if _, err = e.WriteDeviceModbusPoints(client.ID, target, 16, "65535-65536", 9); err == nil {
		t.Fatal("register overflow accepted")
	}
	after, _ := e.Device(server.ID, target)
	if !reflect.DeepEqual(before.Points, after.Points) {
		t.Fatal("invalid batch changed data")
	}
	t.Logf("SCALE "+protocol+" units=%d pointsPerDevice=10000 totalServerPoints=%d full-read requests and segmented writes=%d mappedCoils=2000 mappedRegisters=2000 elapsed=%s", units, units*10000, requests, time.Since(start))
}

func BenchmarkScale104LinkSettings10000(b *testing.B) {
	e := NewEngine()
	c := scaleChannel("link", "iec104", "server", scaleIECPoints(2500, false))
	c.Devices[0].Configuration = &DeviceConfiguration{Version: "标准版", Values: map[string]string{"sendWindow": "12", "txtMess_Byte": "3"}}
	if _, err := e.UpsertChannel(c); err != nil {
		b.Fatal(err)
	}
	p := &peer{s: &session{e: e, cfg: c}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if p.device104().CommonAddress != 1 {
			b.Fatal("settings changed")
		}
	}
}

func TestScaleLinkSettingsAreDetachedAndLightweight(t *testing.T) {
	e := NewEngine()
	c := scaleChannel("link", "iec104", "server", scaleIECPoints(2500, false))
	c.Devices[0].Configuration = &DeviceConfiguration{Version: "标准版", Values: map[string]string{"sendWindow": "12"}, Ranges: []PointRange{{Kind: "yx", Tab: "yx", Count: 2500}}}
	if _, err := e.UpsertChannel(c); err != nil {
		t.Fatal(err)
	}
	p := &peer{s: &session{e: e, cfg: c}}
	d := p.device104()
	if len(d.Points) != 0 || len(d.Configuration.Ranges) != 0 || d.Configuration.Values["sendWindow"] != "12" {
		t.Fatal("link settings copied table or lost value")
	}
	d.Configuration.Values["sendWindow"] = "1"
	if p.device104().Configuration.Values["sendWindow"] != "12" {
		t.Fatal("settings alias live configuration")
	}
}

func TestScaleAutomaticReportingRecovery(t *testing.T) {
	duration := 2 * time.Second
	if v := os.Getenv("GYLAB_SCALE_SOAK_DURATION"); v != "" {
		var err error
		duration, err = time.ParseDuration(v)
		if err != nil || duration < time.Second || duration > 10*time.Minute {
			t.Fatal("invalid scale soak duration")
		}
	}
	e := NewEngine()
	defer e.StopAll()
	points := scaleIECPoints(2500, false)
	server := scaleChannel("soak-server", "iec104", "server", points)
	if _, err := e.UpsertChannel(server); err != nil {
		t.Fatal(err)
	}
	config := AutoDataConfig{StopOnDisconnect: false, Rules: []AutoDataRule{
		{Enabled: true, Kind: "yx", Points: "0-4998", Mode: "toggle", Max: 1, IntervalMS: 1000},
		{Enabled: true, Kind: "yc", Points: "20000-24998", Mode: "increment", Min: 100, Max: 300, Step: 1, IntervalMS: 1000},
		{Enabled: true, Kind: "yc", Points: "20000-24998", Mode: "report", Cause: 2, IntervalMS: 5000},
	}}
	if _, err := e.SaveDataTest(server.ID, "device", "telemetry", config); err != nil {
		t.Fatal(err)
	}
	if err := e.Start(server.ID); err != nil {
		t.Fatal(err)
	}
	client := scaleChannel("soak-client", "iec104", "client", append([]Point{}, points...))
	client.Address = e.View(ViewRequest{}).States[0].Address
	if _, err := e.UpsertChannel(client); err != nil {
		t.Fatal(err)
	}
	ready := func() bool {
		for _, s := range e.View(ViewRequest{Channel: client.ID}).States {
			if s.ID == client.ID {
				return s.ReadyPeers > 0
			}
		}
		return false
	}
	if err := e.Start(client.ID); err != nil {
		t.Fatal(err)
	}
	scaleWait(t, 10*time.Second, ready)
	if _, err := e.StartDataTest(server.ID, "device", "telemetry"); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	reconnects, samples, maxPending := 0, 0, 0
	nextReconnect := start.Add(10 * time.Second)
	view := e.View(ViewRequest{Channel: server.ID, Device: "device"})
	model := map[PointKey]Point{}
	for _, p := range view.Points {
		model[PointKey{p.Kind, p.Address}] = p
	}
	for time.Since(start) < duration {
		time.Sleep(250 * time.Millisecond)
		samples++
		view = e.View(viewRequest(view))
		if view.FullPoints {
			clear(model)
		}
		for _, p := range view.Points {
			model[PointKey{p.Kind, p.Address}] = p
		}
		e.mu.Lock()
		s := e.running[server.ID]
		e.mu.Unlock()
		for _, p := range readyDataPeers(s) {
			p.mu.Lock()
			n := len(p.queue)
			p.mu.Unlock()
			if n > maxPending {
				maxPending = n
			}
		}
		if time.Now().After(nextReconnect) {
			e.Stop(client.ID)
			if err := e.Start(client.ID); err != nil {
				t.Fatal(err)
			}
			scaleWait(t, 10*time.Second, ready)
			reconnects++
			nextReconnect = time.Now().Add(10 * time.Second)
		}
		state, err := e.GetDataTest(server.ID, "device", "telemetry")
		if err != nil || state.LastError != "" {
			t.Fatal("large reporting failed", state.LastError, err)
		}
	}
	stopped, err := e.SetDataTestState(server.ID, "device", "telemetry", "stop")
	if err != nil || stopped.Updates < 5000 {
		t.Fatal("no full reporting cycle", stopped, err)
	}
	view = e.View(viewRequest(view))
	for _, p := range view.Points {
		model[PointKey{p.Kind, p.Address}] = p
	}
	expected, _ := e.Device(server.ID, "device")
	for _, p := range expected.Points {
		if !reflect.DeepEqual(p, model[PointKey{p.Kind, p.Address}]) {
			t.Fatal("incremental stream lost point", p)
		}
	}
	// A post-stop GI drains/refreshes all final values; verify every reported point.
	if err := e.InterrogateDevice(client.ID, "device"); err != nil {
		t.Fatal(err)
	}
	scaleWait(t, 20*time.Second, func() bool {
		actual, _ := e.Device(client.ID, "device")
		for i, p := range expected.Points {
			if (p.Kind == "yx" || p.Kind == "yc") && !scalePointEqual(p, actual.Points[i]) {
				return false
			}
		}
		return true
	})
	for _, p := range expected.Points {
		if (p.Kind == "yk" || p.Kind == "yt") && p.Value != 0 {
			t.Fatal("unselected controls mutated")
		}
	}
	if _, err := e.SetPointValue(server.ID, "device", PointKey{"yc", 24998}, 321.5); err != nil {
		t.Fatal(err)
	}
	scaleWait(t, 10*time.Second, func() bool {
		d, _ := e.Device(client.ID, "device")
		p, _ := findRemotePoint(d.Points, PointKey{"yc", 24998})
		return p.Value == 321.5
	})
	time.Sleep(250 * time.Millisecond)
	after, _ := e.GetDataTest(server.ID, "device", "telemetry")
	if after.Status != "stopped" || after.Updates != stopped.Updates {
		t.Fatal("stopped generator advanced")
	}
	e.mu.Lock()
	messages, events := len(e.messages), len(e.events)
	e.mu.Unlock()
	if messages > 2000 || events > 5000 {
		t.Fatal("unbounded history")
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	t.Logf("SCALE soak duration=%s samples=%d source=10000 telemetryPerCycle=5000 updates=%d reconnects=%d peakPending=%d messages=%d events=%d heapMiB=%d goroutines=%d", time.Since(start), samples, stopped.Updates, reconnects, maxPending, messages, events, m.HeapAlloc>>20, runtime.NumGoroutine())
}

func TestScaleConcurrentIEC104Channels(t *testing.T) {
	if os.Getenv("GYLAB_SCALE_NETWORK") == "" {
		t.Skip("explicit 64-channel network acceptance")
	}
	start := time.Now()
	baseline := runtime.NumGoroutine()
	e := NewEngine()
	defer e.StopAll()
	points := scaleIECPoints(2500, false)
	pairs := 32
	for n := 0; n < pairs; n++ {
		server := scaleChannel(fmt.Sprintf("server-%02d", n), "iec104", "server", points)
		if _, err := e.UpsertChannel(server); err != nil {
			t.Fatal(err)
		}
		if err := e.Start(server.ID); err != nil {
			t.Fatal(err)
		}
		client := scaleChannel(fmt.Sprintf("client-%02d", n), "iec104", "client", nil)
		client.Devices[0].Points = append([]Point{}, points...)
		for j := range client.Devices[0].Points {
			client.Devices[0].Points[j].Value = -1
			if client.Devices[0].Points[j].Kind != "yc" {
				client.Devices[0].Points[j].Value = 0
			}
		}
		for _, s := range e.View(ViewRequest{}).States {
			if s.ID == server.ID {
				client.Address = s.Address
			}
		}
		if _, err := e.UpsertChannel(client); err != nil {
			t.Fatal(err)
		}
		if err := e.Start(client.ID); err != nil {
			t.Fatal(err)
		}
	}
	scaleWait(t, 20*time.Second, func() bool {
		v := e.View(ViewRequest{})
		for _, s := range v.States {
			if s.ReadyPeers != 1 {
				return false
			}
		}
		return len(v.States) == 64
	})
	giStart := time.Now()
	for n := 0; n < pairs; n++ {
		if err := e.InterrogateDevice(fmt.Sprintf("client-%02d", n), "device"); err != nil {
			t.Fatal(err)
		}
	}
	scaleWait(t, 40*time.Second, func() bool {
		for n := 0; n < pairs; n++ {
			d, err := e.Device(fmt.Sprintf("client-%02d", n), "device")
			if err != nil {
				return false
			}
			for i, p := range d.Points {
				if (p.Kind == "yx" || p.Kind == "yc") && !scalePointEqual(points[i], p) {
					return false
				}
			}
		}
		return true
	})
	if _, err := e.UpsertChannel(scaleChannel("overflow", "iec104", "server", nil)); err == nil {
		t.Fatal("65th channel accepted")
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	activeG := runtime.NumGoroutine()
	gi := time.Since(giStart)
	e.StopAll()
	scaleWait(t, 10*time.Second, func() bool { return runtime.NumGoroutine() <= baseline+4 })
	for _, s := range e.View(ViewRequest{}).States {
		if s.Status != "stopped" || s.Peers != 0 {
			t.Fatal("stop leaked channel", s)
		}
	}
	t.Logf("SCALE concurrent channels=64 connectedPairs=32 configuredPoints=640000 GIvalidatedTelemetry=160000 GI=%s total=%s activeGoroutines=%d finalGoroutines=%d baselineGoroutines=%d heapMiB=%d", gi, time.Since(start), activeG, runtime.NumGoroutine(), baseline, m.HeapAlloc>>20)
}
