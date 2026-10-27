package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

type AutoControlSeed struct {
	ReplaceValues bool   `json:"replaceValues,omitempty"`
	Addresses     string `json:"addresses"`
	Value         string `json:"value"`
	ValueType     byte   `json:"valueType"`
	Function      byte   `json:"function,omitempty"`
}
type AutoControlOptions struct {
	Function      byte `json:"function,omitempty"`
	IntervalMS    int  `json:"intervalMs"`
	TimeoutMS     int  `json:"timeoutMs"`
	Double        bool `json:"double"`
	Cycle         bool `json:"cycle"`
	Direct        bool `json:"direct"`
	CheckFeedback bool `json:"checkFeedback"`
	AutoPause     bool `json:"autoPause"`
	ValueType     byte `json:"valueType"`
}
type AutoControlRow struct {
	Address        uint32 `json:"address"`
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	Action         string `json:"action"`
	Count          int    `json:"count"`
	Value          string `json:"value"`
	Feedback       string `json:"feedback"`
	FeedbackName   string `json:"feedbackName"`
	Selection      string `json:"selection"`
	Execution      string `json:"execution"`
	FeedbackResult string `json:"feedbackResult"`
	Executed       string `json:"executed"`
}
type AutoControlSnapshot struct {
	Saved      bool               `json:"saved"`
	Protocol   string             `json:"protocol"`
	ID         string             `json:"id"`
	Channel    string             `json:"channel"`
	Device     string             `json:"device"`
	Mode       string             `json:"mode"`
	Status     string             `json:"status"`
	Rows       []AutoControlRow   `json:"rows"`
	Options    AutoControlOptions `json:"options"`
	Current    int                `json:"current"`
	Total      int                `json:"total"`
	Success    int                `json:"success"`
	Failure    int                `json:"failure"`
	Errors     []string           `json:"errors"`
	LastError  string             `json:"lastError"`
	Revision   uint64             `json:"revision"`
	Run        uint64             `json:"run"`
	Generation uint64             `json:"generation"`
}
type AutoControlRowEdit struct {
	Index      int     `json:"index"`
	Generation uint64  `json:"generation,omitempty"`
	Enabled    *bool   `json:"enabled,omitempty"`
	Action     *string `json:"action,omitempty"`
	Count      *int    `json:"count,omitempty"`
	Value      *string `json:"value,omitempty"`
}
type AutoControlBulk struct {
	ChangeEnabled bool   `json:"changeEnabled"`
	Enabled       bool   `json:"enabled"`
	ChangeAction  bool   `json:"changeAction"`
	Action        string `json:"action"`
	ChangeCount   bool   `json:"changeCount"`
	Count         int    `json:"count"`
}
type autoControlJob struct {
	mu     sync.Mutex
	view   AutoControlSnapshot
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{}
}

func autoActive(status string) bool {
	return status == "starting" || status == "running" || status == "paused" || status == "stopping"
}
func (j *autoControlJob) snapshot() AutoControlSnapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.cloneLocked()
}
func (j *autoControlJob) cloneLocked() AutoControlSnapshot {
	v := j.view
	v.Rows = append([]AutoControlRow{}, v.Rows...)
	v.Errors = append([]string{}, v.Errors...)
	return v
}
func (j *autoControlJob) change(fn func(*AutoControlSnapshot)) {
	j.mu.Lock()
	fn(&j.view)
	j.view.Revision++
	j.mu.Unlock()
}
func (j *autoControlJob) notify() {
	select {
	case j.wake <- struct{}{}:
	default:
	}
}
func (j *autoControlJob) row(i int) AutoControlRow {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.view.Rows[i]
}
func (j *autoControlJob) options() AutoControlOptions {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.view.Options
}
func autoKey(channel, device, mode string) string { return channel + "/" + device + "/" + mode }
func (e *Engine) autoJob(channel, device, mode string) (*autoControlJob, error) {
	e.autoMu.Lock()
	defer e.autoMu.Unlock()
	j := e.autoJobs[autoKey(channel, device, mode)]
	if j == nil {
		return nil, errors.New("请先打开自动操作窗口")
	}
	return j, nil
}
func (e *Engine) autoTarget(channel, device string) (Channel, Device, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.project.Channels {
		if c.ID != channel {
			continue
		}
		if (c.Protocol != "iec104" && c.Protocol != "modbus-tcp" && c.Protocol != "modbus-rtu") || c.Role != "client" {
			return Channel{}, Device{}, errors.New("自动遥控／遥调需要 IEC104 或 Modbus 主站")
		}
		d, err := deviceIn(&c, device)
		if err != nil {
			return Channel{}, Device{}, err
		}
		copy := *d
		copy.Points = append([]Point{}, d.Points...)
		copy.Configuration = cloneConfiguration(d.Configuration)
		return c, copy, nil
	}
	return Channel{}, Device{}, errors.New("通道不存在")
}
func parseFeedback(text string) (uint32, bool, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "！", "!"))
	inverse := strings.HasPrefix(text, "!")
	text = strings.TrimPrefix(text, "!")
	n, err := strconv.ParseUint(text, 10, 24)
	if err != nil {
		return 0, false, errors.New("遥信关联应为点位，或 !点位（反向）")
	}
	return uint32(n), inverse, nil
}

// GYTest appends range expansions after individual entries in its input list.
// Preserve that ordering and duplicates; reject invalid or unbounded expansions.
func ParseAutoAddresses(text string) ([]uint32, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("请输入遥调点位")
	}
	if _, err := parsePointSpans(text, 0xffffff); err != nil {
		return nil, err
	}
	singles, ranges := []uint32{}, [][2]uint32{}
	count := 0
	for _, part := range strings.Split(strings.ReplaceAll(text, "，", ","), ",") {
		part = strings.TrimSpace(part)
		parts := strings.Split(part, "-")
		if len(parts) > 2 || part == "" {
			return nil, errors.New("遥调点位应为数字、范围和英文逗号")
		}
		first, err := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 24)
		if err != nil {
			return nil, errors.New("遥调点位超出 24 位范围")
		}
		if len(parts) == 1 {
			singles = append(singles, uint32(first))
			count++
		} else {
			last, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 24)
			if err != nil || last < first {
				return nil, errors.New("范围后面的点位不得小于前面的点位")
			}
			if last-first+1 > 10000 {
				return nil, errors.New("一次最多 10000 个遥调点")
			}
			count += int(last - first + 1)
			ranges = append(ranges, [2]uint32{uint32(first), uint32(last)})
		}
		if count > 10000 {
			return nil, errors.New("一次最多 10000 个遥调点")
		}
	}
	for _, r := range ranges {
		for n := r[0]; n <= r[1]; n++ {
			singles = append(singles, n)
		}
	}
	return singles, nil
}
func parseAutoValue(text string) (float64, error) {
	text = strings.TrimSpace(text)
	if strings.HasSuffix(strings.ToUpper(text), "H") {
		n, err := strconv.ParseUint(text[:len(text)-1], 16, 32)
		if err != nil {
			return 0, errors.New("遥调十六进制数值无效")
		}
		return float64(int32(n)), nil
	}
	n, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, errors.New("遥调数值无效")
	}
	return n, nil
}
func validateAutoOptions(o AutoControlOptions) error {
	if o.IntervalMS < 0 || o.IntervalMS > 3600000 {
		return errors.New("操作间隔应为 0–3600000 毫秒")
	}
	if o.TimeoutMS < 100 || o.TimeoutMS > 60000 {
		return errors.New("超时判断应为 100–60000 毫秒")
	}
	if o.ValueType != 48 && o.ValueType != 49 && o.ValueType != 50 {
		return errors.New("遥调类型无效")
	}
	return nil
}
func validateAutoRow(mode string, r AutoControlRow, o AutoControlOptions) error {
	if r.Count < 1 || r.Count > 1000000 {
		return errors.New("遥控次数应为 1–1000000 整数")
	}
	if o.Function != 0 {
		if r.Address > 65535 || o.Function != 5 && o.Function != 6 || mode == "yt" && o.Function != 6 {
			return errors.New("Modbus自动控制点位或功能码无效")
		}
		if mode == "yt" {
			v, err := parseAutoValue(r.Value)
			if err != nil {
				return err
			}
			if v < 0 || v > 65535 || math.Trunc(v) != v {
				return errors.New("Modbus遥调值应为0–65535整数")
			}
			return nil
		}
	}
	if mode == "yk" {
		switch r.Action {
		case "合闸后分闸", "分闸后合闸", "合闸", "分闸":
		default:
			return errors.New("遥控类型无效")
		}
		return nil
	}
	value, err := parseAutoValue(r.Value)
	if err != nil {
		return err
	}
	_, err = EncodeControl104(1, Control104Request{Type: o.ValueType, Address: r.Address, Value: value, Operation: "execute"})
	return err
}
func (e *Engine) PrepareAutoControl(channel, device, mode string, seed AutoControlSeed) (AutoControlSnapshot, error) {
	if mode != "yk" && mode != "yt" {
		return AutoControlSnapshot{}, errors.New("自动操作类型无效")
	}
	channelConfig, d, err := e.autoTarget(channel, device)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	e.autoMu.Lock()
	if e.autoJobs == nil {
		e.autoJobs = map[string]*autoControlJob{}
	}
	key := autoKey(channel, d.ID, mode)
	j := e.autoJobs[key]
	if j == nil {
		interval := 2
		if mode == "yt" {
			interval = 200
		}
		j = &autoControlJob{view: AutoControlSnapshot{ID: newID(), Protocol: channelConfig.Protocol, Channel: channel, Device: d.ID, Mode: mode, Status: "idle", Current: -1, Rows: []AutoControlRow{}, Errors: []string{}, Options: AutoControlOptions{IntervalMS: interval, TimeoutMS: 10000, AutoPause: true, ValueType: 48}}, wake: make(chan struct{}, 1)}
		e.autoJobs[key] = j
	}
	e.autoMu.Unlock()
	j.mu.Lock()
	defer j.mu.Unlock()
	if autoActive(j.view.Status) {
		return j.cloneLocked(), nil
	}
	rows := []AutoControlRow{}
	if mode == "yk" {
		for _, p := range d.Points {
			if controlCategory(dataPointCategory(channelConfig.Protocol, p)) != "yk" {
				continue
			}
			r := AutoControlRow{Address: p.Address, Name: p.Name, Enabled: true, Action: "合闸后分闸", Count: 1, Feedback: p.Feedback, FeedbackName: "未关联"}
			if p.Feedback != "" {
				address, _, failure := parseFeedback(p.Feedback)
				if failure == nil {
					for _, yx := range d.Points {
						if dataPointCategory(channelConfig.Protocol, yx) == "yx" && yx.Address == address {
							r.FeedbackName = yx.Name
							if r.FeedbackName == "" {
								r.FeedbackName = fmt.Sprintf("遥信%d", address)
							}
							break
						}
					}
				}
			}
			rows = append(rows, r)
		}
	} else {
		addresses, failure := ParseAutoAddresses(seed.Addresses)
		if failure != nil {
			return AutoControlSnapshot{}, failure
		}
		if seed.ValueType != 0 {
			j.view.Options.ValueType = seed.ValueType
		}
		for _, address := range addresses {
			rows = append(rows, AutoControlRow{Address: address, Name: fmt.Sprintf("遥调%d", address), Value: seed.Value, Enabled: true, Count: 1, Executed: "否"})
		}
	}
	if channelConfig.Protocol != "iec104" {
		j.view.Options.Direct = true
		j.view.Options.Double = false
		j.view.Options.Function = seed.Function
		if j.view.Options.Function == 0 {
			j.view.Options.Function = 5
			if mode == "yt" {
				j.view.Options.Function = 6
			}
		}
	}
	j.view.Rows = rows
	j.view.Status = "idle"
	j.view.Generation++
	j.view.Current = -1
	j.view.LastError = ""
	j.view.Revision++
	return j.cloneLocked(), nil
}
func (e *Engine) GetAutoControl(channel, device, mode string) (AutoControlSnapshot, error) {
	j, err := e.savedAutoJob(channel, device, mode)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	return j.snapshot(), nil
}
func (e *Engine) ValidateAutoControlWindow(channel, device, mode string) error {
	if _, err := e.autoJob(channel, device, mode); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.project.Channels {
		if c.ID == channel && (c.Protocol == "iec104" || c.Protocol == "modbus-tcp" || c.Protocol == "modbus-rtu") && c.Role == "client" {
			_, err := deviceIn(&c, device)
			return err
		}
	}
	return errors.New("原通道或装置已不存在")
}
func (e *Engine) SetAutoValueType(channel, device string, typ byte) error {
	if typ < 48 || typ > 50 {
		return errors.New("遥调类型无效")
	}
	e.autoMu.Lock()
	j := e.autoJobs[autoKey(channel, device, "yt")]
	e.autoMu.Unlock()
	if j != nil {
		j.change(func(v *AutoControlSnapshot) { v.Options.ValueType = typ })
	}
	return nil
}
func (e *Engine) UpdateAutoOptions(channel, device, mode string, o AutoControlOptions) (AutoControlSnapshot, error) {
	if err := validateAutoOptions(o); err != nil {
		return AutoControlSnapshot{}, err
	}
	j, err := e.autoJob(channel, device, mode)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	j.change(func(v *AutoControlSnapshot) {
		if v.Protocol != "" && v.Protocol != "iec104" {
			o.Function = v.Options.Function
			o.Direct = true
			o.Double = false
		}
		if mode == "yt" {
			o.ValueType = v.Options.ValueType
		}
		v.Options = o
	})
	j.notify()
	return j.snapshot(), nil
}
func (e *Engine) UpdateAutoRows(channel, device, mode string, edits []AutoControlRowEdit) (AutoControlSnapshot, error) {
	j, err := e.autoJob(channel, device, mode)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(edits) > 10000 {
		return AutoControlSnapshot{}, errors.New("修改行数超出范围")
	}
	rows := append([]AutoControlRow{}, j.view.Rows...)
	for _, edit := range edits {
		if edit.Generation != 0 && edit.Generation != j.view.Generation {
			return AutoControlSnapshot{}, errors.New("操作表已更新，请重新编辑")
		}
		if edit.Index < 0 || edit.Index >= len(rows) {
			return AutoControlSnapshot{}, errors.New("自动操作行已不存在")
		}
		r := &rows[edit.Index]
		if edit.Enabled != nil {
			r.Enabled = *edit.Enabled
		}
		if edit.Action != nil {
			r.Action = *edit.Action
		}
		if edit.Count != nil {
			r.Count = *edit.Count
		}
		if edit.Value != nil {
			r.Value = *edit.Value
		}
		if err := validateAutoRow(mode, *r, j.view.Options); err != nil {
			return AutoControlSnapshot{}, err
		}
	}
	j.view.Rows = rows
	j.view.Revision++
	return j.cloneLocked(), nil
}
func (e *Engine) ApplyAutoBulk(channel, device string, b AutoControlBulk) (AutoControlSnapshot, error) {
	j, err := e.autoJob(channel, device, "yk")
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	rows := append([]AutoControlRow{}, j.view.Rows...)
	for i := range rows {
		if b.ChangeEnabled {
			rows[i].Enabled = b.Enabled
		}
		if b.ChangeAction {
			rows[i].Action = b.Action
		}
		if b.ChangeCount {
			rows[i].Count = b.Count
		}
		if err := validateAutoRow("yk", rows[i], j.view.Options); err != nil {
			return AutoControlSnapshot{}, err
		}
	}
	j.view.Rows = rows
	j.view.Revision++
	return j.cloneLocked(), nil
}

func (e *Engine) reserveAuto(ctx context.Context, cancel context.CancelFunc, s *session, id string) (func(), error) {
	s.mu.Lock()
	peers := []*peer{}
	for p := range s.peers {
		peers = append(peers, p)
	}
	s.mu.Unlock()
	reserved := []*peer{}
	release := func() {
		for _, p := range reserved {
			p.mu.Lock()
			if p.batchOwner == id {
				p.batchOwner = ""
			}
			p.mu.Unlock()
		}
	}
	if len(peers) == 0 {
		return nil, errors.New("请先连接通道")
	}
	for _, p := range peers {
		p.mu.Lock()
		if p.closed || s.cfg.Protocol == "iec104" && !p.transfer || p.control != nil || len(p.pending) > 0 || p.batchOwner != "" || p.controlUncertain {
			p.mu.Unlock()
			release()
			return nil, errors.New("通道尚未激活、存在待应答命令或已有自动任务")
		}
		p.batchOwner = id
		reserved = append(reserved, p)
		p.mu.Unlock()
	}
	for _, p := range peers {
		go func(p *peer) {
			select {
			case <-ctx.Done():
			case <-p.done:
				cancel()
			}
		}(p)
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-s.done:
			cancel()
		}
	}()
	return release, nil
}
func (e *Engine) StartAutoControl(channel, device, mode string) (AutoControlSnapshot, error) {
	_, d, err := e.autoTarget(channel, device)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	s, err := e.getSession(channel)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	j, err := e.autoJob(channel, d.ID, mode)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	j.mu.Lock()
	if autoActive(j.view.Status) {
		j.mu.Unlock()
		return AutoControlSnapshot{}, errors.New("自动任务已经启动")
	}
	if err = validateAutoOptions(j.view.Options); err == nil {
		enabled := 0
		for _, r := range j.view.Rows {
			if mode == "yk" && !r.Enabled {
				continue
			}
			enabled++
			if err = validateAutoRow(mode, r, j.view.Options); err != nil {
				break
			}
		}
		if enabled == 0 && err == nil {
			err = errors.New("没有启用的操作点")
		}
	}
	if err != nil {
		j.mu.Unlock()
		return AutoControlSnapshot{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.ctx = ctx
	j.cancel = cancel
	j.done = make(chan struct{})
	j.view.Status = "starting"
	j.view.Revision++
	j.mu.Unlock()
	release, err := e.reserveAuto(ctx, cancel, s, j.view.ID)
	if err != nil {
		cancel()
		j.change(func(v *AutoControlSnapshot) { v.Status = "idle"; v.LastError = err.Error() })
		close(j.done)
		return AutoControlSnapshot{}, err
	}
	if ctx.Err() != nil {
		release()
		j.change(func(v *AutoControlSnapshot) { v.Status = "stopped" })
		close(j.done)
		return j.snapshot(), nil
	}
	j.change(func(v *AutoControlSnapshot) {
		v.Status = "running"
		v.Total = 0
		v.Success = 0
		v.Failure = 0
		v.Current = -1
		v.Run++
		v.LastError = ""
		for i := range v.Rows {
			v.Rows[i].Selection = ""
			v.Rows[i].Execution = ""
			v.Rows[i].FeedbackResult = ""
			v.Rows[i].Executed = "否"
		}
	})
	go func() { defer close(j.done); defer cancel(); e.runAutoControl(j, ctx, s, d, release) }()
	return j.snapshot(), nil
}
func (e *Engine) PauseAutoControl(channel, device, mode string) (AutoControlSnapshot, error) {
	j, err := e.autoJob(channel, device, mode)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	j.change(func(v *AutoControlSnapshot) {
		if v.Status == "running" {
			v.Status = "paused"
		}
	})
	return j.snapshot(), nil
}
func (e *Engine) ResumeAutoControl(channel, device, mode string) (AutoControlSnapshot, error) {
	j, err := e.autoJob(channel, device, mode)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	j.change(func(v *AutoControlSnapshot) {
		if v.Status == "paused" {
			v.Status = "running"
		}
	})
	j.notify()
	return j.snapshot(), nil
}
func (e *Engine) StopAutoControl(channel, device, mode string) (AutoControlSnapshot, error) {
	j, err := e.autoJob(channel, device, mode)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	j.stop()
	return j.snapshot(), nil
}
func (j *autoControlJob) stop() {
	j.mu.Lock()
	if autoActive(j.view.Status) {
		j.view.Status = "stopping"
		j.view.Revision++
		if j.cancel != nil {
			j.cancel()
		}
	}
	j.mu.Unlock()
	j.notify()
}
func (e *Engine) stopAutoControls(channel string) {
	e.autoMu.Lock()
	jobs := []*autoControlJob{}
	for _, j := range e.autoJobs {
		if j.view.Channel == channel {
			jobs = append(jobs, j)
		}
	}
	e.autoMu.Unlock()
	for _, j := range jobs {
		j.stop()
	}
}
func (e *Engine) resetAutoControls() {
	e.autoMu.Lock()
	for _, j := range e.autoJobs {
		j.stop()
	}
	e.autoJobs = map[string]*autoControlJob{}
	e.autoDrafts = map[string]*autoControlJob{}
	e.autoMu.Unlock()
}
func (j *autoControlJob) checkpoint(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		j.mu.Lock()
		paused := j.view.Status == "paused"
		j.mu.Unlock()
		if !paused {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-j.wake:
		}
	}
}
func autoDelay(ctx context.Context, ms int) error {
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func autoActions(action string) []bool {
	switch action {
	case "合闸后分闸":
		return []bool{true, false}
	case "分闸后合闸":
		return []bool{false, true}
	case "合闸":
		return []bool{true}
	default:
		return []bool{false}
	}
}
func (j *autoControlJob) failure(index int, stage, message string) {
	j.change(func(v *AutoControlSnapshot) {
		v.Failure++
		v.LastError = message
		cell, detail, _ := strings.Cut(message, "：")
		if strings.Contains(detail, "超时") {
			cell = strings.Replace(cell, "失败", "超时", 1)
		} else if strings.Contains(detail, "拒绝") {
			cell = strings.Replace(cell, "失败", "拒绝", 1)
		}
		if stage == "select" {
			v.Rows[index].Selection = cell
		} else if stage == "feedback" {
			v.Rows[index].FeedbackResult = "变位失败"
		} else {
			v.Rows[index].Execution = cell
		}
		v.Errors = append(v.Errors, time.Now().Format("<15:04:05.000>")+" "+fmt.Sprintf("%d:%s → %s", v.Rows[index].Address, v.Rows[index].Name, message))
		if len(v.Errors) > 3000 {
			v.Errors = append([]string{}, v.Errors[len(v.Errors)-2000:]...)
		}
		if v.Options.AutoPause && v.Mode == "yk" {
			v.Status = "paused"
		}
	})
}
func (e *Engine) runAutoControl(j *autoControlJob, ctx context.Context, origin *session, target Device, release func()) {
	failed := false
	defer func() {
		release()
		j.change(func(v *AutoControlSnapshot) {
			if ctx.Err() != nil || v.Status == "stopping" {
				v.Status = "stopped"
			} else if failed {
				v.Status = "error"
			} else {
				v.Status = "completed"
			}
		})
	}()
	view := j.snapshot()
	valid := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		session, err := e.getSession(view.Channel)
		if err != nil || session != origin {
			return errors.New("原通道已停止或重新连接")
		}
		d, err := e.Device(view.Channel, view.Device)
		if err != nil || d.CommonAddress != target.CommonAddress || d.Unit != target.Unit {
			return errors.New("原装置已移除或点位已改变")
		}
		return nil
	}
	command := func(r Control104Request) error {
		if err := valid(); err != nil {
			return err
		}
		if origin.cfg.Protocol != "iec104" {
			if r.Operation == "select" {
				return nil
			}
			fc := j.options().Function
			pdu, err := WritePDU(fc, uint16(r.Address), uint16(r.Value))
			if err != nil {
				return err
			}
			return e.commandModbusContext(ctx, view.Channel, view.Device, pdu, r.TimeoutMS, view.ID)
		}
		r.batchID = view.ID
		_, err := e.command104DeviceContext(ctx, view.Channel, view.Device, r)
		return err
	}
	if view.Mode == "yt" {
		for i := range view.Rows {
			if j.checkpoint(ctx) != nil {
				return
			}
			r := j.row(i)
			o := j.options()
			j.change(func(v *AutoControlSnapshot) { v.Current = i; v.Total++ })
			value, err := parseAutoValue(r.Value)
			if err == nil {
				err = command(Control104Request{Type: o.ValueType, Address: r.Address, Value: value, Operation: "execute", Direct: true, TimeoutMS: o.TimeoutMS})
			}
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				e.autoFailure(j, i, "execute", err.Error())
				failed = true
				return
			}
			j.change(func(v *AutoControlSnapshot) { v.Rows[i].Executed = "是"; v.Success++ })
			if autoDelay(ctx, o.IntervalMS) != nil {
				return
			}
		}
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		j.change(func(v *AutoControlSnapshot) {
			for i := range v.Rows {
				v.Rows[i].Selection = ""
				v.Rows[i].Execution = ""
				v.Rows[i].FeedbackResult = ""
			}
		})
		enabled := 0
		for i := range view.Rows {
			if !j.row(i).Enabled {
				continue
			}
			enabled++
			for count := 1; count <= j.row(i).Count; count++ {
				for _, on := range autoActions(j.row(i).Action) {
					if j.checkpoint(ctx) != nil {
						return
					}
					if err := valid(); err != nil {
						e.autoFailure(j, i, "execute", err.Error())
						failed = true
						return
					}
					r, o := j.row(i), j.options()
					label := "分闸"
					if on {
						label = "合闸"
					}
					prefix := fmt.Sprintf("第%d次%s", count, label)
					j.change(func(v *AutoControlSnapshot) { v.Current = i; v.Total++ })
					typ, value := byte(45), float64(0)
					if on {
						value = 1
					}
					if o.Double {
						typ = 46
						value++
					}
					request := Control104Request{Type: typ, Address: r.Address, Value: value, TimeoutMS: o.TimeoutMS, Operation: "select"}
					if !o.Direct && origin.cfg.Protocol == "iec104" {
						j.change(func(v *AutoControlSnapshot) { v.Rows[i].Selection = prefix + "选择中.." })
						err := command(request)
						if ctx.Err() != nil {
							return
						}
						if err != nil {
							e.autoFailure(j, i, "select", prefix+"选择失败："+err.Error())
							if errors.Is(err, errControlUncertain) {
								failed = true
								return
							}
							if autoDelay(ctx, max(20, o.IntervalMS)) != nil {
								return
							}
							continue
						}
						j.change(func(v *AutoControlSnapshot) { v.Rows[i].Selection = prefix + "选择成功" })
					}
					request.Operation = "execute"
					request.Direct = o.Direct
					j.change(func(v *AutoControlSnapshot) { v.Rows[i].Execution = prefix + "执行中.." })
					err := command(request)
					if ctx.Err() != nil {
						return
					}
					if err != nil {
						e.autoFailure(j, i, "execute", prefix+"执行失败："+err.Error())
						if errors.Is(err, errControlUncertain) {
							failed = true
							return
						}
						if autoDelay(ctx, max(20, o.IntervalMS)) != nil {
							return
						}
						continue
					}
					j.change(func(v *AutoControlSnapshot) { v.Rows[i].Execution = prefix + "执行成功" })
					if o.CheckFeedback && r.Feedback != "" && r.FeedbackName != "未关联" {
						if origin.cfg.Protocol != "iec104" {
							addr, _, _ := parseFeedback(r.Feedback)
							fc := byte(2)
							for _, point := range target.Points {
								if dataPointCategory(origin.cfg.Protocol, point) == "yx" && point.Address == addr && point.Kind == "coil" {
									fc = 1
								}
							}
							pdu, _ := ReadPDU(fc, uint16(addr), 1)
							if err = e.commandModbusContext(ctx, view.Channel, view.Device, pdu, o.TimeoutMS, view.ID); err != nil {
								e.autoFailure(j, i, "feedback", err.Error())
								continue
							}
						}
						j.change(func(v *AutoControlSnapshot) { v.Rows[i].FeedbackResult = "测试中" })
						address, inverse, _ := parseFeedback(r.Feedback)
						expected := on
						if inverse {
							expected = !expected
						}
						matched := false
						deadline := time.Now().Add(time.Duration(o.TimeoutMS) * time.Millisecond)
						for time.Now().Before(deadline) {
							if ctx.Err() != nil {
								return
							}
							d, lookup := e.Device(view.Channel, view.Device)
							if lookup != nil {
								break
							}
							for _, p := range d.Points {
								if dataPointCategory(origin.cfg.Protocol, p) == "yx" && p.Address == address && p.Quality&0x80 == 0 && (!(p.WireType == 3 || p.WireType == 31) || p.Value == 1 || p.Value == 2) && ((p.WireType == 3 || p.WireType == 31) && p.Value == 2 || p.WireType != 3 && p.WireType != 31 && p.Value != 0) == expected {
									matched = true
									break
								}
							}
							if matched {
								break
							}
							if autoDelay(ctx, 20) != nil {
								return
							}
						}
						if !matched {
							e.autoFailure(j, i, "feedback", prefix+"执行后：遥信点变位失败")
							if autoDelay(ctx, o.IntervalMS) != nil {
								return
							}
							continue
						}
						j.change(func(v *AutoControlSnapshot) { v.Rows[i].FeedbackResult = "变位成功" })
					}
					j.change(func(v *AutoControlSnapshot) { v.Success++ })
					if autoDelay(ctx, o.IntervalMS) != nil {
						return
					}
				}
			}
		}
		if !j.options().Cycle {
			return
		}
		if enabled == 0 {
			j.change(func(v *AutoControlSnapshot) { v.Status = "paused"; v.LastError = "没有启用的遥控点" })
			if j.checkpoint(ctx) != nil {
				return
			}
		}
	}
}

func (e *Engine) autoFailure(j *autoControlJob, index int, stage, message string) {
	j.failure(index, stage, message)
	j.mu.Lock()
	channel := j.view.Channel
	address := j.view.Rows[index].Address
	j.mu.Unlock()
	e.log(channel, "", "ERR", nil, fmt.Sprintf("点位 %d：%s", address, message))
}
