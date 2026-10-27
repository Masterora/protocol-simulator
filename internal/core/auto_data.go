package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"sort"
	"time"
)

type AutoDataRule struct {
	Points     string  `json:"points,omitempty"`
	Cause      byte    `json:"cause,omitempty"`
	Enabled    bool    `json:"enabled"`
	Kind       string  `json:"kind"`
	Start      uint32  `json:"start"`
	End        uint32  `json:"end"`
	IntervalMS int     `json:"intervalMs"`
	Mode       string  `json:"mode"`
	Min        float64 `json:"min"`
	Max        float64 `json:"max"`
	Step       float64 `json:"step"`
}
type AutoDataConfig struct {
	Rules            []AutoDataRule `json:"rules"`
	StopOnDisconnect bool           `json:"stopOnDisconnect"`
}
type AutoDataSnapshot struct {
	Category   string         `json:"category,omitempty"`
	Generation uint64         `json:"generation"`
	Channel    string         `json:"channel"`
	Device     string         `json:"device"`
	Status     string         `json:"status"`
	Config     AutoDataConfig `json:"config"`
	Cycles     []uint64       `json:"cycles"`
	Updates    uint64         `json:"updates"`
	LastError  string         `json:"lastError"`
}
type autoDataJob struct {
	view          AutoDataSnapshot // Protected by Engine.mu.
	ctx           context.Context
	cancel        context.CancelFunc
	pending       context.Context
	cancelPending context.CancelFunc
	next          []time.Time
}

func cloneAutoData(c *AutoDataConfig) *AutoDataConfig {
	if c == nil {
		return nil
	}
	v := *c
	v.Rules = append([]AutoDataRule{}, c.Rules...)
	return &v
}
func dataBit(kind string) bool { return kind == "yx" || kind == "coil" || kind == "discrete" }
func validateAutoData(protocol, role string, c *AutoDataConfig, points ...Point) error {
	if c == nil {
		return nil
	}
	if role != "server" || (protocol != "iec104" && protocol != "modbus-tcp" && protocol != "modbus-rtu") {
		return errors.New("自动数据仅适用于 IEC104 或 Modbus 子站")
	}
	if len(c.Rules) > 256 {
		return errors.New("最多配置256组自动数据")
	}
	for i, r := range c.Rules {
		if !r.Enabled {
			continue
		}
		valid := protocol == "iec104" && (r.Kind == "yx" || r.Kind == "yc") || protocol != "iec104" && (r.Kind == "coil" || r.Kind == "discrete" || r.Kind == "holding" || r.Kind == "input")
		limit := uint32(65535)
		if protocol == "iec104" {
			limit = 16777215
		}
		selected, selectErr := selectionSpans(r.Points, r.Start, r.End, limit)
		if !valid || selectErr != nil {
			return fmt.Errorf("第%d组：数据类型或点位范围无效", i+1)
		}
		if r.IntervalMS < 200 || r.IntervalMS > 3600000 {
			return fmt.Errorf("第%d组：周期应为200–3600000毫秒", i+1)
		}
		if r.Mode == "report" && r.Cause != 0 && r.Cause != 1 && r.Cause != 2 && r.Cause != 3 {
			return errors.New("上报原因应为周期、背景或突发")
		}
		if r.Mode == "report" && r.Cause == 1 && r.Kind != "yc" {
			return errors.New("周期扫描仅适用于遥测")
		}
		if r.Mode == "report" && protocol != "iec104" {
			return errors.New("定时上报仅适用于104子站")
		}
		if r.Mode != "report" {
			for _, v := range []float64{r.Min, r.Max, r.Step} {
				if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > math.MaxFloat32 {
					return fmt.Errorf("第%d组：数值范围无效", i+1)
				}
			}
			if r.Mode != "fixed" && r.Mode != "increment" && r.Mode != "random" && r.Mode != "toggle" && r.Mode != "sequence" {
				return errors.New("未知数值变化方式")
			}
			if r.Mode != "fixed" && r.Min > r.Max {
				return fmt.Errorf("第%d组：下限不能大于上限", i+1)
			}
			if r.Mode == "increment" && r.Step <= 0 {
				return errors.New("递增步长必须大于0")
			}
			if (r.Mode == "toggle" || r.Mode == "sequence") && !dataBit(r.Kind) {
				return errors.New("翻转仅适用于开关量")
			}
			// Double-point YX also carries states 2 and 3. Resolve the actual
			// per-point wire type below; coils and discrete inputs remain binary.
			if dataBit(r.Kind) {
				upper := float64(1)
				if protocol == "iec104" && r.Kind == "yx" {
					upper = 3
				}
				bounds := []float64{r.Min}
				if r.Mode != "fixed" {
					bounds = append(bounds, r.Max)
				}
				for _, v := range bounds {
					if v < 0 || v > upper || math.Trunc(v) != v {
						return errors.New("开关量取值应为有效状态整数")
					}
				}
			}
		}
		for _, point := range points {
			if r.Mode == "report" || point.Kind != r.Kind || !containsPoint(selected, point.Address) {
				continue
			}
			bounds := []float64{r.Min}
			if r.Mode != "fixed" {
				bounds = append(bounds, r.Max)
			}
			for _, v := range bounds {
				candidate := point
				candidate.Value = v
				if err := validatePoints([]Point{candidate}, protocol); err != nil {
					return fmt.Errorf("第%d组：%w", i+1, err)
				}
			}
			if r.Mode == "increment" && dataInteger(point) && math.Trunc(r.Step) != r.Step {
				return errors.New("整数点步长必须为整数")
			}
		}

		for k := 0; k < i; k++ {
			other := c.Rules[k]
			otherPoints, _ := selectionSpans(other.Points, other.Start, other.End, limit)
			if other.Enabled && (other.Mode == "report") == (r.Mode == "report") && other.Kind == r.Kind && overlappingPoints(otherPoints, selected) {
				return fmt.Errorf("第%d组与第%d组点位范围重叠", k+1, i+1)
			}
		}
	}
	return nil
}
func (e *Engine) dataTargetLocked(channel, device string) (*Channel, *Device, error) {
	if device == "" {
		return nil, nil, errors.New("请选择装置")
	}
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID == channel {
			d, err := deviceIn(c, device)
			if err != nil {
				return nil, nil, err
			}
			if c.Role != "server" || (c.Protocol != "iec104" && c.Protocol != "modbus-tcp" && c.Protocol != "modbus-rtu") {
				return nil, nil, errors.New("请选择 IEC104 或 Modbus 子站装置")
			}
			return c, d, nil
		}
	}
	return nil, nil, errors.New("通道不存在")
}
func (e *Engine) dataSnapshotLocked(channel, device string, d *Device, category string) AutoDataSnapshot {
	v := AutoDataSnapshot{Category: category, Channel: channel, Device: device, Status: "stopped", Config: AutoDataConfig{Rules: []AutoDataRule{}, StopOnDisconnect: true}, Cycles: []uint64{}}
	if config := dataConfiguration(d, category); config != nil {
		v.Config = *config
	}
	if j := e.dataJobs[dataJobKey(channel, device, category)]; j != nil {
		v = j.view
		v.Config = *cloneAutoData(&v.Config)
		v.Cycles = append([]uint64{}, v.Cycles...)
	}
	v.Generation = e.dataGeneration
	return v
}
func (e *Engine) GetAutoData(channel, device string) (AutoDataSnapshot, error) {
	return e.getAutoData(channel, device, "")
}
func (e *Engine) getAutoData(channel, device, category string) (AutoDataSnapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, d, err := e.dataTargetLocked(channel, device)
	if err != nil {
		return AutoDataSnapshot{}, err
	}
	return e.dataSnapshotLocked(channel, device, d, category), nil
}
func (e *Engine) SaveAutoData(channel, device string, config AutoDataConfig) (AutoDataSnapshot, error) {
	return e.saveAutoData(channel, device, "", config, false)
}
func (e *Engine) saveAutoData(channel, device, category string, config AutoDataConfig, disconnect bool) (AutoDataSnapshot, error) {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	c, d, err := e.dataTargetLocked(channel, device)
	if err != nil {
		return AutoDataSnapshot{}, err
	}
	if j := e.dataJobs[dataJobKey(channel, device, category)]; !disconnect && j != nil && (j.view.Status == "running" || j.view.Status == "paused") {
		return AutoDataSnapshot{}, errors.New("请先停止自动数据，再修改规则")
	}
	if err = validateDataTest(c.Protocol, c.Role, category, &config, deviceValidationPoints(c.Protocol, *d, d.Points)...); err != nil {
		return AutoDataSnapshot{}, err
	}
	if disconnect {
		if sameDataConfiguration(dataConfiguration(d, category), &config) && (category != "telemetry" || d.DataTests["telemetry"] != nil) {
			return e.dataSnapshotLocked(channel, device, d, category), nil
		}
		e.disconnectConfigurationLocked(channel)
	}
	if category == "" {
		d.AutoData = cloneAutoData(&config)
	} else {
		if d.DataTests == nil {
			d.DataTests = map[string]*AutoDataConfig{}
		}
		d.DataTests[category] = cloneAutoData(&config)
		if category == "telemetry" {
			d.AutoData = nil
			for _, old := range []string{"", "yx", "yc", "yx-report", "yc-report"} {
				delete(d.DataTests, old)
				if job := e.dataJobs[dataJobKey(channel, device, old)]; job != nil {
					e.finishDataLocked(job, "")
					delete(e.dataJobs, dataJobKey(channel, device, old))
				}
			}
		}
	}
	delete(e.dataJobs, dataJobKey(channel, device, category))
	e.dirty = true
	e.markConfigurationLocked()
	return e.dataSnapshotLocked(channel, device, d, category), nil
}
func readyDataPeers(s *session) []*peer {
	s.mu.Lock()
	peers := make([]*peer, 0, len(s.peers))
	for p := range s.peers {
		peers = append(peers, p)
	}
	s.mu.Unlock()
	ready := peers[:0]
	for _, p := range peers {
		p.mu.Lock()
		ok := !p.closed && p.transfer
		p.mu.Unlock()
		if ok {
			ready = append(ready, p)
		}
	}
	return ready
}
func (e *Engine) StartAutoData(channel, device string) (AutoDataSnapshot, error) {
	return e.startAutoData(channel, device, "")
}
func (e *Engine) startAutoData(channel, device, category string) (AutoDataSnapshot, error) {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	c, d, err := e.dataTargetLocked(channel, device)
	if err != nil {
		e.mu.Unlock()
		return AutoDataSnapshot{}, err
	}
	key := dataJobKey(channel, device, category)
	if old := e.dataJobs[key]; old != nil && (old.view.Status == "running" || old.view.Status == "paused") {
		e.mu.Unlock()
		return AutoDataSnapshot{}, errors.New("自动数据任务已经启动")
	}
	for _, job := range e.dataJobs {
		if job.view.Channel == channel && job.view.Device == device && (job.view.Category == "" || category == "" || job.view.Category == "telemetry" || category == "telemetry") && (job.view.Status == "running" || job.view.Status == "paused") {
			e.mu.Unlock()
			return AutoDataSnapshot{}, errors.New("请先停止统一自动数据任务或对应的独立测试")
		}
	}
	config := dataConfiguration(d, category)
	if config == nil {
		e.mu.Unlock()
		return AutoDataSnapshot{}, errors.New("请先配置自动数据规则")
	}
	if err = validateDataTest(c.Protocol, c.Role, category, config, deviceValidationPoints(c.Protocol, *d, d.Points)...); err != nil {
		e.mu.Unlock()
		return AutoDataSnapshot{}, err
	}
	count := 0
	for i, r := range config.Rules {
		selected, _ := selectionSpans(r.Points, r.Start, r.End, 0xffffff)
		if !r.Enabled {
			continue
		}
		matched := false
		for _, p := range d.Points {
			if dataScopeMatches(category, c.Protocol, p) && p.Kind == r.Kind && containsPoint(selected, p.Address) {
				matched = true
				break
			}
		}
		if !matched {
			e.mu.Unlock()
			return AutoDataSnapshot{}, fmt.Errorf("第%d组范围内没有已配置点位", i+1)
		}
		count++
	}
	if count == 0 {
		e.mu.Unlock()
		return AutoDataSnapshot{}, errors.New("请至少启用一组规则")
	}
	s := e.running[channel]
	protocol := c.Protocol
	e.mu.Unlock()
	if s == nil {
		return AutoDataSnapshot{}, errors.New("请先启动子站通道")
	}
	if protocol == "iec104" && len(readyDataPeers(s)) == 0 {
		return AutoDataSnapshot{}, errors.New("请等待主站连接并激活104数据传输")
	}
	ctx, cancel := context.WithCancel(context.Background())
	pending, cancelPending := context.WithCancel(ctx)
	j := &autoDataJob{view: AutoDataSnapshot{Category: category, Channel: channel, Device: device, Status: "running", Config: *config, Cycles: make([]uint64, len(config.Rules))}, ctx: ctx, cancel: cancel, pending: pending, cancelPending: cancelPending, next: make([]time.Time, len(config.Rules))}
	for i, r := range config.Rules {
		j.next[i] = time.Now().Add(time.Duration(r.IntervalMS) * time.Millisecond)
	}
	e.mu.Lock()
	if e.dataJobs == nil {
		e.dataJobs = map[string]*autoDataJob{}
	}
	e.dataJobs[key] = j
	v := e.dataSnapshotLocked(channel, device, d, category)
	e.mu.Unlock()
	go e.runAutoData(j)
	return v, nil
}
func (e *Engine) SetAutoDataState(channel, device, action string) (AutoDataSnapshot, error) {
	return e.setAutoDataState(channel, device, "", action)
}
func (e *Engine) setAutoDataState(channel, device, category, action string) (AutoDataSnapshot, error) {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	_, d, err := e.dataTargetLocked(channel, device)
	if err != nil {
		return AutoDataSnapshot{}, err
	}
	j := e.dataJobs[dataJobKey(channel, device, category)]
	if j == nil {
		return AutoDataSnapshot{}, errors.New("自动数据任务未启动")
	}
	switch action {
	case "pause":
		if j.view.Status != "running" {
			return AutoDataSnapshot{}, errors.New("任务没有运行")
		}
		j.cancelPending()
		j.view.Status = "paused"
	case "resume":
		if j.view.Status != "paused" {
			return AutoDataSnapshot{}, errors.New("任务未暂停")
		}
		j.pending, j.cancelPending = context.WithCancel(j.ctx)
		j.view.Status = "running"
		for i, r := range j.view.Config.Rules {
			j.next[i] = time.Now().Add(time.Duration(r.IntervalMS) * time.Millisecond)
		}
	case "stop":
		e.finishDataLocked(j, "")
	default:
		return AutoDataSnapshot{}, errors.New("未知自动数据操作")
	}
	return e.dataSnapshotLocked(channel, device, d, category), nil
}
func (e *Engine) finishDataLocked(j *autoDataJob, message string) {
	j.cancelPending()
	j.cancel()
	j.view.Status = "stopped"
	j.view.LastError = message
	if message != "" {
		e.logLocked(j.view.Channel, "", "ERR", nil, message)
	}
}

// Caller holds the engine mutation and state locks. Workers never wait for
// the mutation lock, so cancellation also works inside project replacement.
func (e *Engine) cancelDataLocked(channel, device string) {
	for _, j := range e.dataJobs {
		if (channel == "" || j.view.Channel == channel) && (device == "" || j.view.Device == device) {
			e.finishDataLocked(j, "")
		}
	}
}
func (e *Engine) runAutoData(j *autoDataJob) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-j.ctx.Done():
			return
		case now := <-ticker.C:
			if !e.op.TryLock() {
				continue
			}
			if j.ctx.Err() == nil {
				e.stepAutoData(j, now)
			}
			e.op.Unlock()
		}
	}
}
func dataInteger(p Point) bool {
	if p.Kind == "yc" {
		return p.WireType == 11 || p.WireType == 35 || p.WireType == 15 || p.WireType == 37
	}
	return p.DataType != "float32"
}

func nextDataValue(p Point, r AutoDataRule) float64 {
	switch r.Mode {
	case "report":
		return p.Value
	case "fixed":
		return r.Min
	case "toggle", "sequence":
		if p.WireType == 3 || p.WireType == 31 {
			if p.Value == 1 {
				return 2
			}
			return 1
		}
		if p.Value == 0 {
			return 1
		}
		return 0
	case "random":
		if !dataInteger(p) {
			return r.Min + rand.Float64()*(r.Max-r.Min)
		}
		return r.Min + float64(rand.Int64N(int64(r.Max-r.Min)+1))
	default:
		next := p.Value + r.Step
		if p.Value < r.Min || next > r.Max {
			return r.Min
		}
		return next
	}
}
func (e *Engine) stepAutoData(j *autoDataJob, now time.Time) {
	e.mu.Lock()
	if j.view.Status != "running" {
		e.mu.Unlock()
		return
	}
	c, d, err := e.dataTargetLocked(j.view.Channel, j.view.Device)
	s := e.running[j.view.Channel]
	if err != nil || s == nil {
		e.finishDataLocked(j, "通道或装置已停止")
		e.mu.Unlock()
		return
	}
	protocol, ca := c.Protocol, d.CommonAddress

	e.mu.Unlock()
	var peers []*peer
	if protocol == "iec104" {
		peers = readyDataPeers(s)
		if len(peers) == 0 {
			e.mu.Lock()
			if j.view.Config.StopOnDisconnect {
				e.finishDataLocked(j, "通信中断，自动数据已停止")
			}
			e.mu.Unlock()
			return
		}
	}
	e.mu.Lock()
	// Stop/pause/configuration replacement may have completed while peer locks
	// were being read. Never commit a cycle after its cancellation boundary.
	if j.view.Status != "running" || j.ctx.Err() != nil || j.pending.Err() != nil || e.running[j.view.Channel] != s {
		e.mu.Unlock()
		return
	}
	c, d, err = e.dataTargetLocked(j.view.Channel, j.view.Device)
	if err != nil {
		e.finishDataLocked(j, err.Error())
		e.mu.Unlock()
		return
	}
	actualDevice := d
	working := *d
	for i, rule := range j.view.Config.Rules {
		if rule.Enabled && rule.Mode != "report" && !now.Before(j.next[i]) {
			working.Points = append([]Point{}, d.Points...)
			break
		}
	}
	d = &working
	type dataReport struct {
		point     Point
		scheduled bool
		cause     byte
	}
	var previousPoints []Point
	hasRelations := protocol == "iec104" && d.Configuration != nil && len(d.Configuration.Relations) > 0
	updates := []dataReport{}
	changed := false
	for i, r := range j.view.Config.Rules {
		if !r.Enabled || now.Before(j.next[i]) {
			continue
		}
		if r.Mode != "report" && hasRelations && previousPoints == nil {
			previousPoints = append([]Point{}, d.Points...)
		}
		j.next[i] = now.Add(time.Duration(r.IntervalMS) * time.Millisecond)
		selected, _ := selectionSpans(r.Points, r.Start, r.End, 0xffffff)
		matched := 0
		indexes := []int{}
		for k, p := range d.Points {
			if dataScopeMatches(j.view.Category, protocol, p) && p.Kind == r.Kind && containsPoint(selected, p.Address) {
				indexes = append(indexes, k)
			}
		}
		if r.Mode == "sequence" && len(indexes) > 0 {
			sort.Slice(indexes, func(a, b int) bool { return d.Points[indexes[a]].Address < d.Points[indexes[b]].Address })
			indexes = []int{indexes[int(j.view.Cycles[i]%uint64(len(indexes)))]}
		}
		for _, k := range indexes {
			p := &d.Points[k]
			if !dataScopeMatches(j.view.Category, protocol, *p) || p.Kind != r.Kind || !containsPoint(selected, p.Address) {
				continue
			}
			if r.Mode != "report" && protocol == "iec104" && p.WireType == 0 && d.Configuration != nil {
				p.WireType, _ = configured104Type(*d, p.Kind)
			}
			if r.Mode != "report" {
				p.Value = nextDataValue(*p, r)
				p.Updated = now.Format("15:04:05.000")
				if timeTagged(p.WireType) {
					p.Timestamp = now.Format(time.RFC3339Nano)
					p.TimeInvalid = false
				}
				changed = true
			}
			updates = append(updates, dataReport{*p, r.Mode == "report", r.Cause})
			matched++
		}
		if matched == 0 {
			e.finishDataLocked(j, fmt.Sprintf("第%d组点位已不存在", i+1))
			e.mu.Unlock()
			return
		}
		j.view.Cycles[i]++
	}
	if changed && hasRelations {
		// The generator has already assigned its input values; restore their
		// pre-cycle values for change detection while keeping generated copies.
		generated := []Point{}
		for _, u := range updates {
			if !u.scheduled {
				generated = append(generated, u.point)
			}
		}
		before := *d
		before.Points = previousPoints
		related, relationErr := applyTelemetryRelations(before, generated)
		if relationErr != nil {
			d.Points = previousPoints
			c.syncLegacy()
			e.finishDataLocked(j, relationErr.Error())
			e.mu.Unlock()
			return
		}
		known := map[PointKey]int{}
		for i, u := range updates {
			known[PointKey{u.point.Kind, u.point.Address}] = i
		}
		for _, u := range related {
			if index, ok := known[PointKey{u.Kind, u.Address}]; ok {
				updates[index].point = u
			} else {
				updates = append(updates, dataReport{point: u})
			}
			for k := range d.Points {
				if d.Points[k].Kind == u.Kind && d.Points[k].Address == u.Address {
					d.Points[k] = u
					break
				}
			}
		}
	}
	if changed {
		values := make([]Point, 0, len(updates))
		current := make(map[PointKey]Point, len(d.Points))
		for _, point := range d.Points {
			current[PointKey{point.Kind, point.Address}] = point
		}
		seen := make(map[PointKey]bool, len(updates))
		for i := range updates {
			key := PointKey{updates[i].point.Kind, updates[i].point.Address}
			// A variation and a scheduled report may cover the same point in
			// this cycle. Validate each final value once, and report the final
			// value even when the report rule preceded its variation rule.
			updates[i].point = current[key]
			if !seen[key] {
				values = append(values, updates[i].point)
				seen[key] = true
			}
		}
		if err := validatePoints(deviceValidationPoints(protocol, *d, values), protocol); err != nil {
			e.finishDataLocked(j, "自动数据未应用："+err.Error())
			e.mu.Unlock()
			return
		}
		actualDevice.Points = d.Points
		c.syncLegacy()
		e.dirty = true
		e.markPointsLocked(j.view.Channel, d.ID, values)
	}
	if len(updates) > 0 {
		j.view.Updates += uint64(len(updates))
	}
	pending := j.pending
	var reportDevice Device
	if len(updates) > 0 {
		reportDevice = Device{ID: d.ID, Name: d.Name, CommonAddress: ca, Configuration: cloneConfiguration(d.Configuration)}
	}
	e.mu.Unlock()
	for _, p := range peers {
		for i := 0; i < len(updates); {
			if updates[i].scheduled {
				points := []Point{}
				cause := updates[i].cause
				if cause == 0 {
					cause = 3
				}
				for i < len(updates) && updates[i].scheduled && (updates[i].cause == cause || updates[i].cause == 0 && cause == 3) {
					points = append(points, updates[i].point)
					i++
				}
				// Reuse manual publish batching, type selection and SQ encoding.
				err = p.publishPointsContext(pending, reportDevice, points, cause, "定时上报 · "+reportDevice.Name)
			} else {
				// A variation cycle can change thousands of points. Pack compatible
				// spontaneous values just like manual/timed bulk reports so a slow
				// receiver's acknowledgement window does not accumulate one job
				// per point. Keep the original order and each point's metadata.
				points := []Point{}
				for i < len(updates) && !updates[i].scheduled {
					points = append(points, updates[i].point)
					i++
				}
				err = p.publishPointsContext(pending, reportDevice, points, 3, "自动上送")
			}
			if err != nil {
				e.mu.Lock()
				e.finishDataLocked(j, err.Error())
				e.mu.Unlock()
				return
			}
		}
	}
}

// Compare effective rules, including equivalent mixed point expressions.
func sameDataConfiguration(a, b *AutoDataConfig) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.StopOnDisconnect != b.StopOnDisconnect || len(a.Rules) != len(b.Rules) {
		return false
	}
	for i, x := range a.Rules {
		y := b.Rules[i]
		if !x.Enabled && !y.Enabled {
			if !reflect.DeepEqual(x, y) {
				return false
			}
			continue
		}
		xs, xe := selectionSpans(x.Points, x.Start, x.End, 0xffffff)
		ys, ye := selectionSpans(y.Points, y.Start, y.End, 0xffffff)
		if xe != nil || ye != nil || !reflect.DeepEqual(xs, ys) {
			return false
		}
		x.Points = ""
		y.Points = ""
		x.Start = 0
		x.End = 0
		y.Start = 0
		y.End = 0
		if x.Mode == "fixed" {
			x.Max = x.Min
		}
		if y.Mode == "fixed" {
			y.Max = y.Min
		}
		if x.Mode != "increment" {
			x.Step = 0
		}
		if y.Mode != "increment" {
			y.Step = 0
		}
		if x.Mode == "report" {
			x.Min = 0
			x.Max = 0
			if x.Cause == 0 {
				x.Cause = 3
			}
		}
		if y.Mode == "report" {
			y.Min = 0
			y.Max = 0
			if y.Cause == 0 {
				y.Cause = 3
			}
		}
		if !reflect.DeepEqual(x, y) {
			return false
		}
	}
	return true
}
