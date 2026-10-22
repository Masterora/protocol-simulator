package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

type AutoDataRule struct {
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
	if len(c.Rules) > 64 {
		return errors.New("最多配置64组自动数据")
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
		if !valid || r.Start > r.End || r.End > limit {
			return fmt.Errorf("第%d组：数据类型或点位范围无效", i+1)
		}
		if r.IntervalMS < 200 || r.IntervalMS > 3600000 {
			return fmt.Errorf("第%d组：周期应为200–3600000毫秒", i+1)
		}
		for _, v := range []float64{r.Min, r.Max, r.Step} {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > math.MaxFloat32 {
				return fmt.Errorf("第%d组：数值范围无效", i+1)
			}
		}
		if r.Mode != "fixed" && r.Mode != "increment" && r.Mode != "random" && r.Mode != "toggle" {
			return errors.New("未知数值变化方式")
		}
		if r.Mode != "fixed" && r.Min > r.Max {
			return fmt.Errorf("第%d组：下限不能大于上限", i+1)
		}
		if r.Mode == "increment" && r.Step <= 0 {
			return errors.New("递增步长必须大于0")
		}
		if r.Mode == "toggle" && !dataBit(r.Kind) {
			return errors.New("翻转仅适用于开关量")
		}
		if dataBit(r.Kind) && (r.Min < 0 || r.Min > 1 || r.Max < 0 || r.Max > 1) {
			return errors.New("开关量取值只能为0或1")
		}
		for _, point := range points {
			if point.Kind != r.Kind || point.Address < r.Start || point.Address > r.End {
				continue
			}
			for _, v := range []float64{r.Min, r.Max} {
				candidate := point
				candidate.Value = v
				if err := validatePoints([]Point{candidate}, protocol); err != nil {
					return fmt.Errorf("第%d组：%w", i+1, err)
				}
			}
			if r.Mode == "increment" && point.DataType != "float32" && point.Kind != "yc" && math.Trunc(r.Step) != r.Step {
				return errors.New("整数点步长必须为整数")
			}
		}

		for k := 0; k < i; k++ {
			other := c.Rules[k]
			if other.Enabled && other.Kind == r.Kind && other.Start <= r.End && r.Start <= other.End {
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
func (e *Engine) dataSnapshotLocked(channel, device string, d *Device) AutoDataSnapshot {
	v := AutoDataSnapshot{Channel: channel, Device: device, Status: "stopped", Config: AutoDataConfig{Rules: []AutoDataRule{}, StopOnDisconnect: true}, Cycles: []uint64{}}
	if d.AutoData != nil {
		v.Config = *cloneAutoData(d.AutoData)
	}
	if j := e.dataJobs[autoKey(channel, device, "data")]; j != nil {
		v = j.view
		v.Config = *cloneAutoData(&v.Config)
		v.Cycles = append([]uint64{}, v.Cycles...)
	}
	v.Generation = e.dataGeneration
	return v
}
func (e *Engine) GetAutoData(channel, device string) (AutoDataSnapshot, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, d, err := e.dataTargetLocked(channel, device)
	if err != nil {
		return AutoDataSnapshot{}, err
	}
	return e.dataSnapshotLocked(channel, device, d), nil
}
func (e *Engine) SaveAutoData(channel, device string, config AutoDataConfig) (AutoDataSnapshot, error) {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	c, d, err := e.dataTargetLocked(channel, device)
	if err != nil {
		return AutoDataSnapshot{}, err
	}
	if j := e.dataJobs[autoKey(channel, device, "data")]; j != nil && (j.view.Status == "running" || j.view.Status == "paused") {
		return AutoDataSnapshot{}, errors.New("请先停止自动数据，再修改规则")
	}
	if err = validateAutoData(c.Protocol, c.Role, &config, d.Points...); err != nil {
		return AutoDataSnapshot{}, err
	}
	d.AutoData = cloneAutoData(&config)
	delete(e.dataJobs, autoKey(channel, device, "data"))
	e.dirty = true
	return e.dataSnapshotLocked(channel, device, d), nil
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
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	c, d, err := e.dataTargetLocked(channel, device)
	if err != nil {
		e.mu.Unlock()
		return AutoDataSnapshot{}, err
	}
	key := autoKey(channel, device, "data")
	if old := e.dataJobs[key]; old != nil && (old.view.Status == "running" || old.view.Status == "paused") {
		e.mu.Unlock()
		return AutoDataSnapshot{}, errors.New("自动数据任务已经启动")
	}
	config := cloneAutoData(d.AutoData)
	if config == nil {
		e.mu.Unlock()
		return AutoDataSnapshot{}, errors.New("请先配置自动数据规则")
	}
	if err = validateAutoData(c.Protocol, c.Role, config, d.Points...); err != nil {
		e.mu.Unlock()
		return AutoDataSnapshot{}, err
	}
	count := 0
	for i, r := range config.Rules {
		if !r.Enabled {
			continue
		}
		matched := false
		for _, p := range d.Points {
			if p.Kind == r.Kind && p.Address >= r.Start && p.Address <= r.End {
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
	j := &autoDataJob{view: AutoDataSnapshot{Channel: channel, Device: device, Status: "running", Config: *config, Cycles: make([]uint64, len(config.Rules))}, ctx: ctx, cancel: cancel, pending: pending, cancelPending: cancelPending, next: make([]time.Time, len(config.Rules))}
	for i, r := range config.Rules {
		j.next[i] = time.Now().Add(time.Duration(r.IntervalMS) * time.Millisecond)
	}
	e.mu.Lock()
	if e.dataJobs == nil {
		e.dataJobs = map[string]*autoDataJob{}
	}
	e.dataJobs[key] = j
	v := e.dataSnapshotLocked(channel, device, d)
	e.mu.Unlock()
	go e.runAutoData(j)
	return v, nil
}
func (e *Engine) SetAutoDataState(channel, device, action string) (AutoDataSnapshot, error) {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	_, d, err := e.dataTargetLocked(channel, device)
	if err != nil {
		return AutoDataSnapshot{}, err
	}
	j := e.dataJobs[autoKey(channel, device, "data")]
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
	return e.dataSnapshotLocked(channel, device, d), nil
}
func (e *Engine) finishDataLocked(j *autoDataJob, message string) {
	j.cancelPending()
	j.cancel()
	j.view.Status = "stopped"
	j.view.LastError = message
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
func nextDataValue(p Point, r AutoDataRule) float64 {
	switch r.Mode {
	case "fixed":
		return r.Min
	case "toggle":
		if p.Value == 0 {
			return 1
		}
		return 0
	case "random":
		if r.Kind == "yc" || p.DataType == "float32" {
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
	updates := []Point{}
	for i, r := range j.view.Config.Rules {
		if !r.Enabled || now.Before(j.next[i]) {
			continue
		}
		j.next[i] = now.Add(time.Duration(r.IntervalMS) * time.Millisecond)
		matched := 0
		for k := range d.Points {
			p := &d.Points[k]
			if p.Kind != r.Kind || p.Address < r.Start || p.Address > r.End {
				continue
			}
			p.Value = nextDataValue(*p, r)
			p.Updated = now.Format("15:04:05.000")
			if timeTagged(p.WireType) {
				p.Timestamp = now.Format(time.RFC3339Nano)
				p.TimeInvalid = false
			}
			updates = append(updates, *p)
			matched++
		}
		if matched == 0 {
			e.finishDataLocked(j, fmt.Sprintf("第%d组点位已不存在", i+1))
			e.mu.Unlock()
			return
		}
		j.view.Cycles[i]++
	}
	if len(updates) > 0 {
		c.syncLegacy()
		j.view.Updates += uint64(len(updates))
		e.dirty = true
	}
	pending := j.pending
	e.mu.Unlock()
	for _, p := range peers {
		for _, point := range updates {
			if err = p.sendIContext(pending, PointASDU(point, ca, 3), "自动上送"); err != nil {
				e.mu.Lock()
				e.finishDataLocked(j, err.Error())
				e.mu.Unlock()
				return
			}
		}
	}
}
