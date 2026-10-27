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

func TestTimedReportsSendCurrentValuesOver104WithoutChangingPoints(t *testing.T) {
	e, client, server := dataLoopback(t, "iec104")
	id := server.Devices[0].ID
	stamp := time.Now().Add(-time.Hour).Truncate(time.Millisecond).Format(time.RFC3339Nano)
	points := []Point{{Kind: "yx", Address: 1, WireType: 30, Value: 1, Quality: 16, Timestamp: stamp, TimeInvalid: true, Updated: "previous"}, {Kind: "yx", Address: 2, Value: 0}, {Kind: "yc", Address: 10, WireType: 13, Value: 220.5, Quality: 64, Updated: "previous"}, {Kind: "yc", Address: 11, Value: 999}}
	if err := e.SetDevicePoints(server.ID, id, points); err != nil {
		t.Fatal(err)
	}
	before, _ := e.Device(server.ID, id)
	e.ClearMessages()
	for _, category := range []string{"yx", "yc"} {
		address, period := uint32(1), 200
		if category == "yc" {
			address, period = 10, 400
		}
		config := AutoDataConfig{StopOnDisconnect: true, Rules: []AutoDataRule{{Enabled: true, Kind: category, Start: address, End: address, IntervalMS: period, Mode: "report"}}}
		if _, err := e.SaveDataTest(server.ID, id, category+"-report", config); err != nil {
			t.Fatal(err)
		}
		v, err := e.StartDataTest(server.ID, id, category+"-report")
		if err != nil {
			t.Fatal(err)
		}
		if v.Updates != 0 {
			t.Fatal("定时上报未等待首个周期")
		}
	}
	frames := func() map[string][]Point {
		reports := map[string][]Point{}
		for _, m := range e.Snapshot().Messages {
			if m.ChannelID != server.ID || m.Direction != "TX" || !strings.Contains(m.Summary, "定时上报") {
				continue
			}
			b, _ := hex.DecodeString(m.Hex)
			if len(b) <= 9 || b[2]&1 != 0 || b[8] != 3 {
				t.Fatalf("定时上报未沿用主动上送编码: %x", b)
			}
			decoded, err := Decode104Points(b[6:], server.CommonAddress)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range decoded {
				reports[p.Kind] = append(reports[p.Kind], p)
			}
		}
		return reports
	}
	eventually(t, func() bool { v := frames(); return len(v["yx"]) >= 2 && len(v["yc"]) >= 2 })
	for _, p := range frames()["yx"] {
		if p.Address != 1 || p.Value != 1 || p.Quality != 16 || !p.TimeInvalid {
			t.Fatalf("遥信上报范围、值或品质发生变化: %+v", p)
		}
	}
	for _, p := range frames()["yc"] {
		if p.Address != 10 || p.Value != 220.5 || p.Quality != 64 {
			t.Fatalf("遥测上报范围、值或品质发生变化: %+v", p)
		}
	}
	after, _ := e.Device(server.ID, id)
	if !reflect.DeepEqual(before.Points, after.Points) {
		t.Fatalf("定时上报修改了点值或时标: before=%+v after=%+v", before.Points, after.Points)
	}
	eventually(t, func() bool {
		d, _ := e.Device(client.ID, client.Devices[0].ID)
		for _, p := range d.Points {
			if p.Kind == "yc" && p.Address == 10 && p.Value == 220.5 && p.Quality == 64 {
				return true
			}
		}
		return false
	})
	paused, err := e.SetDataTestState(server.ID, id, "yx-report", "pause")
	if err != nil {
		t.Fatal(err)
	}
	yc, _ := e.GetDataTest(server.ID, id, "yc-report")
	eventually(t, func() bool { v, _ := e.GetDataTest(server.ID, id, "yc-report"); return v.Updates > yc.Updates })
	yx, _ := e.GetDataTest(server.ID, id, "yx-report")
	if yx.Updates != paused.Updates || yx.Status != "paused" {
		t.Fatal("遥信暂停未生效或受遥测任务影响")
	}
	if _, err = e.SetDataTestState(server.ID, id, "yx-report", "resume"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { v, _ := e.GetDataTest(server.ID, id, "yx-report"); return v.Updates > paused.Updates })
}

func TestTimedReportConfigurationIsSeparateFromVariationAndPersistsStopped(t *testing.T) {
	e, _, server := dataLoopback(t, "iec104")
	id := server.Devices[0].ID
	report := AutoDataConfig{StopOnDisconnect: true, Rules: []AutoDataRule{{Enabled: true, Kind: "yc", Start: 16385, End: 16386, IntervalMS: 400, Mode: "report"}}}
	change := AutoDataConfig{Rules: []AutoDataRule{{Enabled: true, Kind: "yc", Start: 16385, End: 16386, IntervalMS: 200, Mode: "increment", Max: 100, Step: 1}}}
	if _, err := e.SaveDataTest(server.ID, id, "yc", change); err != nil {
		t.Fatal(err)
	}
	if _, err := e.SaveDataTest(server.ID, id, "yc-report", report); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartDataTest(server.ID, id, "yc"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartDataTest(server.ID, id, "yc-report"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		a, _ := e.GetDataTest(server.ID, id, "yc")
		b, _ := e.GetDataTest(server.ID, id, "yc-report")
		return a.Updates > 0 && b.Updates > 0
	})
	if _, err := e.SetDataTestState(server.ID, id, "yc-report", "stop"); err != nil {
		t.Fatal(err)
	}
	before, _ := e.GetDataTest(server.ID, id, "yc")
	eventually(t, func() bool { v, _ := e.GetDataTest(server.ID, id, "yc"); return v.Updates > before.Updates })
	if _, err := e.SaveDataTest(server.ID, id, "yc-report", change); err == nil {
		t.Fatal("定时上报接受了变化规则")
	}
	if _, err := e.SaveDataTest(server.ID, id, "yc", report); err == nil {
		t.Fatal("定时上报覆盖了变化测试")
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err := e.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded := NewEngine()
	t.Cleanup(loaded.StopAll)
	if err := loaded.Load(path); err != nil {
		t.Fatal(err)
	}
	for scope, want := range map[string]AutoDataConfig{"yc": change, "yc-report": report} {
		v, err := loaded.GetDataTest(server.ID, id, scope)
		if err != nil || v.Status != "stopped" || !reflect.DeepEqual(v.Config, want) {
			t.Fatalf("上报配置未独立保存或打开工程自动启动: %+v %v", v, err)
		}
	}
	modbus := NewEngine()
	cid, err := modbus.UpsertChannel(Channel{Name: "modbus", Role: "server", Protocol: "modbus-tcp", Transport: "tcp-server", Address: "127.0.0.1:0", Unit: 1, Points: []Point{{Kind: "holding", Address: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	d := modbus.Snapshot().Project.Channels[0].Devices[0]
	if _, err = modbus.SaveDataTest(cid, d.ID, "yc-report", report); err == nil {
		t.Fatal("Modbus开启了不支持的主动上报")
	}
}

func TestTimedReportsBatchTheDefaultFiftyPointRange(t *testing.T) {
	e, _, server := dataLoopback(t, "iec104")
	id := server.Devices[0].ID
	points := make([]Point, 50)
	for i := range points {
		points[i] = Point{Kind: "yx", Address: uint32(i + 1), Value: float64(i % 2)}
	}
	if err := e.SetDevicePoints(server.ID, id, points); err != nil {
		t.Fatal(err)
	}
	config := AutoDataConfig{Rules: []AutoDataRule{{Enabled: true, Kind: "yx", Start: 1, End: 50, IntervalMS: 200, Mode: "report"}}}
	if _, err := e.SaveDataTest(server.ID, id, "yx-report", config); err != nil {
		t.Fatal(err)
	}
	e.ClearMessages()
	if _, err := e.StartDataTest(server.ID, id, "yx-report"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { v, _ := e.GetDataTest(server.ID, id, "yx-report"); return v.Updates >= 50 })
	if _, err := e.SetDataTestState(server.ID, id, "yx-report", "stop"); err != nil {
		t.Fatal(err)
	}
	seen := map[uint32]bool{}
	frames := 0
	for _, m := range e.Snapshot().Messages {
		if m.ChannelID != server.ID || m.Direction != "TX" || !strings.Contains(m.Summary, "定时上报") {
			continue
		}
		b, _ := hex.DecodeString(m.Hex)
		decoded, err := Decode104Points(b[6:], server.CommonAddress)
		if err != nil {
			t.Fatal(err)
		}
		frames++
		for _, p := range decoded {
			seen[p.Address] = true
			if p.Value != points[p.Address-1].Value {
				t.Fatal("上报的默认点值发生变化")
			}
		}
	}
	if len(seen) != 50 || frames >= 50 {
		t.Fatalf("默认50点范围未完整批量上报: points=%d frames=%d", len(seen), frames)
	}
}

func TestUnifiedReportingMigratesSavesAndRunsMixedRules(t *testing.T) {
	e, c, s := dataLoopback(t, "iec104")
	id := s.Devices[0].ID
	if err := e.SetDevicePoints(s.ID, id, []Point{{Kind: "yx", Address: 1}, {Kind: "yc", Address: 10, WireType: 13}}); err != nil {
		t.Fatal(err)
	}
	old := map[string]AutoDataConfig{
		"yx":        {Rules: []AutoDataRule{{Enabled: true, Kind: "yx", Points: "1", Mode: "toggle", Max: 1, Step: 1, IntervalMS: 200}}, StopOnDisconnect: true},
		"yc":        {Rules: []AutoDataRule{{Enabled: true, Kind: "yc", Points: "10", Mode: "increment", Max: 10, Step: 1, IntervalMS: 200}}, StopOnDisconnect: true},
		"yc-report": {Rules: []AutoDataRule{{Enabled: true, Kind: "yc", Points: "10", Mode: "report", IntervalMS: 300}}, StopOnDisconnect: true},
	}
	for _, scope := range []string{"yx", "yc", "yc-report"} {
		if _, err := e.SaveDataTest(s.ID, id, scope, old[scope]); err != nil {
			t.Fatal(err)
		}
	}
	view, err := e.GetDataTest(s.ID, id, "telemetry")
	if err != nil || len(view.Config.Rules) != 3 {
		t.Fatalf("migration: %+v %v", view, err)
	}
	view.Config.Rules[1].Step = -1
	if _, err = e.ConfigureAutoData(s.ID, id, "telemetry", view.Config); err == nil {
		t.Fatal("invalid unified save accepted")
	}
	if e.running[s.ID] == nil {
		t.Fatal("invalid save disconnected channel")
	}
	view.Config.Rules[1].Step = 1
	if _, err = e.ConfigureAutoData(s.ID, id, "telemetry", view.Config); err != nil {
		t.Fatal(err)
	}
	if e.running[s.ID] != nil {
		t.Fatal("configuration change did not disconnect")
	}
	d, _ := e.Device(s.ID, id)
	if len(d.DataTests) != 1 || len(d.DataTests["telemetry"].Rules) != 3 {
		t.Fatalf("legacy scopes retained: %+v", d.DataTests)
	}
	file := filepath.Join(t.TempDir(), "unified.json")
	if err = e.Save(file); err != nil {
		t.Fatal(err)
	}
	e.StopAll()
	if err = e.Load(file); err != nil {
		t.Fatal(err)
	}
	if _, err = e.GetDataTest(s.ID, id, "telemetry"); err != nil {
		t.Fatal(err)
	}
	if err = e.Start(s.ID); err != nil {
		t.Fatal(err)
	}
	client := findChannel(e.Snapshot().Project, "client")
	for _, state := range e.Snapshot().States {
		if state.ID == s.ID {
			client.Address = state.Address
		}
	}
	if _, err = e.UpsertChannel(client); err != nil {
		t.Fatal(err)
	}
	if err = e.Start(c.ID); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		for _, state := range e.Snapshot().States {
			if state.ID == s.ID {
				return state.ReadyPeers > 0
			}
		}
		return false
	})
	if _, err = e.StartDataTest(s.ID, id, "telemetry"); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		d, err := e.Device(c.ID, c.Devices[0].ID)
		if err != nil {
			return false
		}
		yc, ycOK := findRemotePoint(d.Points, PointKey{"yc", 10})
		yx, yxOK := findRemotePoint(d.Points, PointKey{"yx", 1})
		view, _ := e.GetDataTest(s.ID, id, "telemetry")
		return ycOK && yxOK && yc.Value > 0 && yx.Value == 1 && view.Cycles[2] > 0
	})
	if _, err = e.StartDataTest(s.ID, id, "yc"); err == nil {
		t.Fatal("legacy task started alongside unified task")
	}
	if _, err = e.SetDataTestState(s.ID, id, "telemetry", "stop"); err != nil {
		t.Fatal(err)
	}
	v, _ := e.GetDataTest(s.ID, id, "telemetry")
	if v.Status != "stopped" {
		t.Fatal("unified stop failed")
	}
}

func TestStepAdjustmentUsesBoundMeasurementCurrentValue(t *testing.T) {
	e, c, s := autoFixture(t, []Point{{Kind: "yt", Address: 101, Value: 0}, {Kind: "yc", Address: 10, Value: 42.5, WireType: 13}})
	cfg := ControlSimulation{Mode: "normal", Mappings: []ControlMapping{{Source: PointKey{"yt", 101}, Target: PointKey{"yc", 10}}}}
	if err := e.SaveControlSimulation(s.ID, s.Devices[0].ID, "yt", cfg); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		direction float64
		want      float64
	}{{2, 43.5}, {1, 42.5}} {
		if _, err := e.Command104Device(c.ID, c.Devices[0].ID, Control104Request{Type: 47, Address: 101, Value: tc.direction, Operation: "execute", Direct: true, TimeoutMS: 1000}); err != nil {
			t.Fatal(err)
		}
		eventually(t, func() bool { return pointAt(t, e, c, "yc", 10).Value == tc.want })
	}
}

func TestIndependentDataTests104ReportAndStopSeparately(t *testing.T) {
	e, client, server := dataLoopback(t, "iec104")
	id := server.Devices[0].ID
	if err := e.SetDevicePoints(server.ID, id, []Point{{Kind: "yx", Address: 1}, {Kind: "yc", Address: 10}}); err != nil {
		t.Fatal(err)
	}
	e.ClearMessages()
	if err := e.PublishDeviceCategory(server.ID, id, "yx"); err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, m := range e.Snapshot().Messages {
		b, _ := hex.DecodeString(m.Hex)
		if m.ChannelID == server.ID && m.Direction == "TX" && len(b) > 9 && b[2]&1 == 0 && b[8] == 3 {
			if b[6] != 1 {
				t.Fatalf("遥信上送包含其它分类: %x", b)
			}
			seen = true
		}
	}
	if !seen {
		t.Fatal("遥信没有上送")
	}
	yx := AutoDataConfig{StopOnDisconnect: true, Rules: []AutoDataRule{{Enabled: true, Kind: "yx", Start: 1, End: 1, IntervalMS: 200, Mode: "toggle", Max: 1, Step: 1}}}
	yc := AutoDataConfig{StopOnDisconnect: false, Rules: []AutoDataRule{{Enabled: true, Kind: "yc", Start: 10, End: 10, IntervalMS: 200, Mode: "fixed", Min: 12.5, Max: 12.5, Step: 1}}}
	for category, config := range map[string]AutoDataConfig{"yx": yx, "yc": yc} {
		if _, err := e.SaveDataTest(server.ID, id, category, config); err != nil {
			t.Fatal(err)
		}
		if _, err := e.StartDataTest(server.ID, id, category); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, func() bool {
		v, _ := e.GetDataTest(server.ID, id, "yx")
		w, _ := e.GetDataTest(server.ID, id, "yc")
		return v.Updates > 0 && w.Updates > 0
	})
	eventually(t, func() bool {
		d, _ := e.Device(client.ID, client.Devices[0].ID)
		for _, p := range d.Points {
			if p.Kind == "yc" && p.Address == 10 && p.Value == 12.5 {
				return true
			}
		}
		return false
	})
	stopped, err := e.SetDataTestState(server.ID, id, "yx", "stop")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := e.GetDataTest(server.ID, id, "yc")
	yx.Rules[0].IntervalMS = 400
	if _, err = e.SaveDataTest(server.ID, id, "yx", yx); err != nil {
		t.Fatalf("遥测运行时无法单独保存遥信: %v", err)
	}
	eventually(t, func() bool { v, _ := e.GetDataTest(server.ID, id, "yc"); return v.Updates > before.Updates })
	if j := e.dataJobs[dataJobKey(server.ID, id, "yx")]; j != nil && j.view.Updates != stopped.Updates {
		t.Fatal("已停止的遥信继续更新")
	}
	after, _ := e.GetDataTest(server.ID, id, "yc")
	if after.Status != "running" || !reflect.DeepEqual(after.Config, yc) {
		t.Fatal("遥信保存修改了遥测配置或运行状态")
	}
	if _, err = e.SaveDataTest(server.ID, id, "yx", yc); err == nil {
		t.Fatal("遥信接受了遥测规则")
	}
	if _, err = e.StartAutoData(server.ID, id); err == nil {
		t.Fatal("统一任务与独立任务同时操作点值")
	}
}

func TestIndependentDataTestsModbusRespectTabsAndPersistStopped(t *testing.T) {
	e, _, server := dataLoopback(t, "modbus-tcp")
	id := server.Devices[0].ID
	points := []Point{{Kind: "coil", Tab: "yx", Address: 11}, {Kind: "coil", Tab: "yk", Address: 12}, {Kind: "holding", Tab: "yc", Address: 10}, {Kind: "holding", Tab: "yt", Address: 20, Value: 888}}
	if err := e.SetDevicePoints(server.ID, id, points); err != nil {
		t.Fatal(err)
	}
	configs := map[string]AutoDataConfig{
		"yx": {StopOnDisconnect: true, Rules: []AutoDataRule{{Enabled: true, Kind: "coil", Start: 11, End: 12, IntervalMS: 200, Mode: "toggle", Max: 1, Step: 1}}},
		"yc": {Rules: []AutoDataRule{{Enabled: true, Kind: "holding", Start: 10, End: 20, IntervalMS: 200, Mode: "fixed", Min: 7, Max: 7, Step: 1}}},
	}
	for category, config := range configs {
		if _, err := e.SaveDataTest(server.ID, id, category, config); err != nil {
			t.Fatal(err)
		}
		if _, err := e.StartDataTest(server.ID, id, category); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, func() bool {
		yx, _ := e.GetDataTest(server.ID, id, "yx")
		yc, _ := e.GetDataTest(server.ID, id, "yc")
		return yx.Updates > 0 && yc.Updates > 0
	})
	d, _ := e.Device(server.ID, id)
	for _, p := range d.Points {
		if p.Tab == "yk" && p.Value != 0 || p.Tab == "yt" && p.Value != 888 {
			t.Fatalf("变位或变化修改了其它四遥点: %+v", p)
		}
	}
	for category := range configs {
		if _, err := e.SetDataTestState(server.ID, id, category, "stop"); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "independent.json")
	if err := e.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded := NewEngine()
	t.Cleanup(loaded.StopAll)
	if err := loaded.Load(path); err != nil {
		t.Fatal(err)
	}
	for category, config := range configs {
		v, err := loaded.GetDataTest(server.ID, id, category)
		if err != nil || v.Status != "stopped" || !reflect.DeepEqual(v.Config, config) {
			t.Fatalf("独立参数未持久化或恢复后自动启动: %+v %v", v, err)
		}
	}
	copy := loaded.Snapshot()
	copy.Project.Channels[0].Devices[0].DataTests["yx"].Rules[0].IntervalMS = 999
	v, _ := loaded.GetDataTest(server.ID, id, "yx")
	if v.Config.Rules[0].IntervalMS != 200 {
		t.Fatal("快照泄漏配置引用")
	}
	time.Sleep(225 * time.Millisecond)
	v, _ = loaded.GetDataTest(server.ID, id, "yc")
	if v.Updates != 0 {
		t.Fatal("打开工程后自动启动测试")
	}
}

func TestIndependentDataTestsMigrateLegacyRulesWithoutOverwritingOtherCategory(t *testing.T) {
	e, _, server := dataLoopback(t, "iec104")
	id := server.Devices[0].ID
	legacy := AutoDataConfig{StopOnDisconnect: true, Rules: []AutoDataRule{{Enabled: true, Kind: "yx", Start: 1, End: 1, IntervalMS: 200, Mode: "toggle", Max: 1, Step: 1}, {Enabled: true, Kind: "yc", Start: 16385, End: 16386, IntervalMS: 400, Mode: "increment", Max: 10, Step: 1}}}
	if _, err := e.SaveAutoData(server.ID, id, legacy); err != nil {
		t.Fatal(err)
	}
	yx, _ := e.GetDataTest(server.ID, id, "yx")
	yc, _ := e.GetDataTest(server.ID, id, "yc")
	if len(yx.Config.Rules) != 1 || yx.Config.Rules[0].Kind != "yx" || len(yc.Config.Rules) != 1 || yc.Config.Rules[0].Kind != "yc" {
		t.Fatal("旧配置未按类型分开")
	}
	yx.Config.StopOnDisconnect = false
	if _, err := e.SaveDataTest(server.ID, id, "yx", yx.Config); err != nil {
		t.Fatal(err)
	}
	ycAfter, _ := e.GetDataTest(server.ID, id, "yc")
	old, _ := e.GetAutoData(server.ID, id)
	if !reflect.DeepEqual(ycAfter.Config, yc.Config) || !reflect.DeepEqual(old.Config, legacy) {
		t.Fatal("保存遥信覆盖旧配置或遥测参数")
	}
}
