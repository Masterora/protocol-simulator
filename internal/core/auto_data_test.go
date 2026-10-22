package core

import (
	"encoding/hex"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func dataLoopback(t *testing.T, protocol string) (*Engine, Channel, Channel) {
	t.Helper()
	e := NewEngine()
	t.Cleanup(e.StopAll)
	if _, err := e.Demo(protocol); err != nil {
		t.Fatal(err)
	}
	s := e.Snapshot()
	client, server := findChannel(s.Project, "client"), findChannel(s.Project, "server")
	waitConnected(t, e, client.ID)
	if protocol == "iec104" {
		eventually(t, func() bool {
			session, _ := e.getSession(server.ID)
			return session != nil && len(readyDataPeers(session)) > 0
		})
	}
	return e, client, server
}
func saveStartData(t *testing.T, e *Engine, s Channel, config AutoDataConfig) {
	t.Helper()
	if _, err := e.SaveAutoData(s.ID, s.Devices[0].ID, config); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartAutoData(s.ID, s.Devices[0].ID); err != nil {
		t.Fatal(err)
	}
}
func TestAutoData104MultipleTypesIntervalsRangeAndPauseResume(t *testing.T) {
	e, c, s := dataLoopback(t, "iec104")
	points := []Point{{Kind: "yc", Address: 10}, {Kind: "yc", Address: 11}, {Kind: "yc", Address: 12, Value: 77}, {Kind: "yx", Address: 1}}
	if err := e.SetDevicePoints(s.ID, s.Devices[0].ID, points); err != nil {
		t.Fatal(err)
	}
	config := AutoDataConfig{StopOnDisconnect: true, Rules: []AutoDataRule{
		{Enabled: true, Kind: "yc", Start: 10, End: 11, IntervalMS: 200, Mode: "fixed", Min: 12.5, Max: 12.5, Step: 1},
		{Enabled: true, Kind: "yx", Start: 1, End: 1, IntervalMS: 400, Mode: "toggle", Min: 0, Max: 1, Step: 1},
	}}
	saveStartData(t, e, s, config)
	initial, _ := e.GetAutoData(s.ID, s.Devices[0].ID)
	if initial.Updates != 0 {
		t.Fatal("first update must wait for its interval")
	}
	eventually(t, func() bool { v, _ := e.GetAutoData(s.ID, s.Devices[0].ID); return v.Cycles[0] >= 3 && v.Cycles[1] >= 1 })
	eventually(t, func() bool {
		d, _ := e.Device(c.ID, c.Devices[0].ID)
		seenYC, seenYX := false, false
		for _, p := range d.Points {
			seenYC = seenYC || p.Kind == "yc" && p.Address == 10 && p.Value == 12.5
			seenYX = seenYX || p.Kind == "yx" && p.Address == 1
		}
		return seenYC && seenYX
	})
	d, _ := e.Device(s.ID, s.Devices[0].ID)
	if d.Points[2].Value != 77 {
		t.Fatal("out-of-range point changed")
	}
	types := map[byte]bool{}
	for _, m := range e.Snapshot().Messages {
		if m.ChannelID != s.ID || m.Direction != "TX" {
			continue
		}
		b, _ := hex.DecodeString(m.Hex)
		if len(b) > 9 && b[2]&1 == 0 && b[8] == 3 {
			types[b[6]] = true
		}
	}
	if !types[1] || !types[13] {
		t.Fatalf("expected spontaneous YX and float YC frames: %+v", types)
	}
	v, err := e.SetAutoDataState(s.ID, s.Devices[0].ID, "pause")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(450 * time.Millisecond)
	paused, _ := e.GetAutoData(s.ID, s.Devices[0].ID)
	if paused.Updates != v.Updates || !reflect.DeepEqual(paused.Cycles, v.Cycles) {
		t.Fatal("paused task advanced")
	}
	if _, err = e.SaveAutoData(s.ID, s.Devices[0].ID, config); err == nil {
		t.Fatal("paused configuration was editable")
	}
	if _, err = e.SetAutoDataState(s.ID, s.Devices[0].ID, "resume"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { n, _ := e.GetAutoData(s.ID, s.Devices[0].ID); return n.Updates > paused.Updates })
	stopped, err := e.SetAutoDataState(s.ID, s.Devices[0].ID, "stop")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(250 * time.Millisecond)
	n, _ := e.GetAutoData(s.ID, s.Devices[0].ID)
	if n.Updates != stopped.Updates || n.Status != "stopped" {
		t.Fatal("stopped task advanced")
	}
}
func TestAutoDataModbusValuesAreReadableByMaster(t *testing.T) {
	e, c, s := dataLoopback(t, "modbus-tcp")
	saveStartData(t, e, s, AutoDataConfig{Rules: []AutoDataRule{
		{Enabled: true, Kind: "holding", Start: 0, End: 0, IntervalMS: 200, Mode: "fixed", Min: 321, Max: 321, Step: 1},
		{Enabled: true, Kind: "coil", Start: 0, End: 0, IntervalMS: 200, Mode: "fixed", Min: 0, Max: 0, Step: 1},
	}})
	eventually(t, func() bool { v, _ := e.GetAutoData(s.ID, s.Devices[0].ID); return v.Updates >= 2 })
	if err := e.ReadModbus(c.ID, 3, 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := e.ReadModbus(c.ID, 1, 0, 1); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		d, _ := e.Device(c.ID, c.Devices[0].ID)
		holding, coil := false, false
		for _, p := range d.Points {
			holding = holding || p.Kind == "holding" && p.Address == 0 && p.Value == 321
			coil = coil || p.Kind == "coil" && p.Address == 0 && p.Value == 0
		}
		return holding && coil
	})
}
func TestAutoDataValidationAndNoImplicitStart(t *testing.T) {
	e, _, s := dataLoopback(t, "modbus-tcp")
	rule := AutoDataRule{Enabled: true, Kind: "holding", Start: 0, End: 1, IntervalMS: 200, Mode: "increment", Min: 0, Max: 10, Step: 1}
	cases := []AutoDataRule{rule, rule, rule, rule, rule, rule}
	cases[0].IntervalMS = 199
	cases[1].End = 65536
	cases[2].Min = 11
	cases[3].Step = 0
	cases[4].Max = math.Inf(1)
	cases[5].Min = .5
	for i, r := range cases {
		if _, err := e.SaveAutoData(s.ID, s.Devices[0].ID, AutoDataConfig{Rules: []AutoDataRule{r}}); err == nil {
			t.Fatalf("invalid rule %d accepted", i)
		}
	}
	if _, err := e.SaveAutoData(s.ID, s.Devices[0].ID, AutoDataConfig{Rules: []AutoDataRule{rule, rule}}); err == nil {
		t.Fatal("overlap accepted")
	}
	if _, err := e.SaveAutoData(s.ID, s.Devices[0].ID, AutoDataConfig{Rules: []AutoDataRule{rule}}); err != nil {
		t.Fatal(err)
	}
	v, _ := e.GetAutoData(s.ID, s.Devices[0].ID)
	if v.Status != "stopped" || v.Updates != 0 {
		t.Fatal("saving started task")
	}
	e.Stop(s.ID)
	if _, err := e.StartAutoData(s.ID, s.Devices[0].ID); err == nil {
		t.Fatal("started stopped transport")
	}
}
func TestAutoDataRulesPersistWithoutResumingOnLoad(t *testing.T) {
	e, _, s := dataLoopback(t, "modbus-tcp")
	config := AutoDataConfig{Rules: []AutoDataRule{{Enabled: true, Kind: "holding", Start: 0, End: 1, IntervalMS: 200, Mode: "increment", Min: 0, Max: 10, Step: 1}}}
	saveStartData(t, e, s, config)
	path := filepath.Join(t.TempDir(), "data.json")
	if err := e.Save(path); err != nil {
		t.Fatal(err)
	}
	other := NewEngine()
	defer other.StopAll()
	if err := other.Load(path); err != nil {
		t.Fatal(err)
	}
	v, err := other.GetAutoData(s.ID, s.Devices[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != "stopped" || !reflect.DeepEqual(v.Config, config) {
		t.Fatalf("wrong restored state: %+v", v)
	}
	v.Config.Rules[0].Max = 999
	again, _ := other.GetAutoData(s.ID, s.Devices[0].ID)
	if again.Config.Rules[0].Max != 10 {
		t.Fatal("snapshot aliases rules")
	}
}
func TestAutoDataConnectionLossStopsOrWaitsAsConfigured(t *testing.T) {
	for _, stop := range []bool{true, false} {
		t.Run(map[bool]string{true: "stop", false: "wait"}[stop], func(t *testing.T) {
			e, c, s := dataLoopback(t, "iec104")
			saveStartData(t, e, s, AutoDataConfig{StopOnDisconnect: stop, Rules: []AutoDataRule{{Enabled: true, Kind: "yc", Start: 16385, End: 16386, IntervalMS: 200, Mode: "increment", Min: 0, Max: 300, Step: 1}}})
			e.Stop(c.ID)
			if stop {
				eventually(t, func() bool {
					v, _ := e.GetAutoData(s.ID, s.Devices[0].ID)
					return v.Status == "stopped" && strings.Contains(v.LastError, "通信中断")
				})
			} else {
				time.Sleep(250 * time.Millisecond)
				v, _ := e.GetAutoData(s.ID, s.Devices[0].ID)
				if v.Status != "running" {
					t.Fatal(v)
				}
				if err := e.Start(c.ID); err != nil {
					t.Fatal(err)
				}
				eventually(t, func() bool { v, _ := e.GetAutoData(s.ID, s.Devices[0].ID); return v.Updates > 0 })
			}
		})
	}
}
func TestAutoDataLifecycleCancelsDeletedDeviceAndReplacedProject(t *testing.T) {
	e, _, s := dataLoopback(t, "modbus-tcp")
	config := AutoDataConfig{Rules: []AutoDataRule{{Enabled: true, Kind: "holding", Start: 0, End: 1, IntervalMS: 200, Mode: "increment", Min: 0, Max: 1000, Step: 1}}}
	saveStartData(t, e, s, config)
	e.mu.Lock()
	job := e.dataJobs[autoKey(s.ID, s.Devices[0].ID, "data")]
	e.mu.Unlock()
	if err := e.DeleteDevice(s.ID, s.Devices[0].ID); err != nil {
		t.Fatal(err)
	}
	if job.ctx.Err() == nil {
		t.Fatal("deleted device job survives")
	}
	if _, err := e.GetAutoData(s.ID, s.Devices[0].ID); err == nil {
		t.Fatal("deleted target still available")
	}
	if err := e.ReplaceProjectAndStop(Project{Version: 1, Name: "new"}, ""); err != nil {
		t.Fatal(err)
	}
	if len(e.dataJobs) != 0 {
		t.Fatal("project retained jobs")
	}
}
func TestAutoDataNumericModesStayInBounds(t *testing.T) {
	r := AutoDataRule{Kind: "yc", Mode: "increment", Min: -1, Max: 1, Step: .5}
	if nextDataValue(Point{Value: 1}, r) != -1 || nextDataValue(Point{Value: 0}, r) != .5 {
		t.Fatal("increment wrap")
	}
	r.Mode = "random"
	for i := 0; i < 1000; i++ {
		v := nextDataValue(Point{}, r)
		if v < -1 || v > 1 {
			t.Fatal(v)
		}
	}
	r = AutoDataRule{Kind: "holding", Mode: "random", Min: 5, Max: 8}
	for i := 0; i < 1000; i++ {
		v := nextDataValue(Point{}, r)
		if v < 5 || v > 8 || math.Trunc(v) != v {
			t.Fatal(v)
		}
	}
}
