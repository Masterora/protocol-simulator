package core

import (
	"errors"
	"fmt"
	"reflect"
)

// Only these options and rows are saved. Runtime counters and pause state are not.
type AutoControlConfiguration struct {
	Options AutoControlOptions `json:"options"`
	Rows    []AutoControlRow   `json:"rows"`
}

func autoRecipe(v AutoControlSnapshot) AutoControlConfiguration {
	rows := append([]AutoControlRow{}, v.Rows...)
	for i := range rows {
		rows[i].Selection = ""
		rows[i].Execution = ""
		rows[i].FeedbackResult = ""
		rows[i].Executed = ""
	}
	return AutoControlConfiguration{Options: v.Options, Rows: rows}
}
func validateAutoRecipe(mode string, recipe AutoControlConfiguration) error {
	if mode != "yk" && mode != "yt" {
		return errors.New("自动操作类型无效")
	}
	if len(recipe.Rows) > 10000 {
		return errors.New("最多10000个点位")
	}
	if err := validateAutoOptions(recipe.Options); err != nil {
		return err
	}
	for i, r := range recipe.Rows {
		if err := validateAutoRow(mode, r, recipe.Options); err != nil {
			return fmt.Errorf("第%d行：%w", i+1, err)
		}
	}
	return nil
}
func (e *Engine) savedAutoJob(channel, device, mode string) (*autoControlJob, error) {
	cfg, d, err := e.autoTarget(channel, device)
	if err != nil {
		return nil, err
	}
	key := autoKey(channel, d.ID, mode)
	e.autoMu.Lock()
	defer e.autoMu.Unlock()
	if j := e.autoJobs[key]; j != nil && j.snapshot().Protocol == cfg.Protocol {
		return j, nil
	}
	if d.Configuration == nil {
		return nil, errors.New("请先保存自动操作配置")
	}
	recipe, ok := d.Configuration.AutoTests[mode]
	if !ok {
		return nil, errors.New("请先保存自动操作配置")
	}
	if err := validateAutoRecipe(mode, recipe); err != nil {
		return nil, err
	}
	j := &autoControlJob{view: AutoControlSnapshot{Saved: true, Protocol: cfg.Protocol, ID: newID(), Channel: channel, Device: d.ID, Mode: mode, Status: "idle", Rows: append([]AutoControlRow{}, recipe.Rows...), Options: recipe.Options, Errors: []string{}, Current: -1, Generation: 1}, wake: make(chan struct{}, 1)}
	if e.autoJobs == nil {
		e.autoJobs = map[string]*autoControlJob{}
	}
	e.autoJobs[key] = j
	return j, nil
}
func (e *Engine) PrepareAutoControlDraft(channel, device, mode string, seed AutoControlSeed) (AutoControlSnapshot, error) {
	cfg, d, err := e.autoTarget(channel, device)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	view, err := e.GetAutoControl(channel, device, mode)
	if err != nil {
		if mode == "yt" && seed.Addresses == "" {
			view = AutoControlSnapshot{ID: newID(), Protocol: cfg.Protocol, Channel: channel, Device: device, Mode: mode, Status: "idle", Current: -1, Rows: []AutoControlRow{}, Errors: []string{}, Options: AutoControlOptions{IntervalMS: 200, TimeoutMS: 10000, AutoPause: true, ValueType: 48}, Generation: 1}
			e.autoMu.Lock()
			if e.autoJobs == nil {
				e.autoJobs = map[string]*autoControlJob{}
			}
			e.autoJobs[autoKey(channel, device, mode)] = &autoControlJob{view: view, wake: make(chan struct{}, 1)}
			e.autoMu.Unlock()
		} else {
			view, err = e.PrepareAutoControl(channel, device, mode, seed)
			if err != nil {
				return view, err
			}
		}
	}

	view.Protocol = cfg.Protocol
	old := map[uint32]AutoControlRow{}
	for _, r := range view.Rows {
		old[r.Address] = r
	}
	if mode == "yk" {
		view.Rows = []AutoControlRow{}
		for _, p := range d.Points {
			if controlCategory(dataPointCategory(cfg.Protocol, p)) != "yk" {
				continue
			}
			r, ok := old[p.Address]
			if !ok {
				r = AutoControlRow{Address: p.Address, Enabled: true, Action: "合闸后分闸", Count: 1}
			}
			r.Name = p.Name
			r.Feedback = p.Feedback
			r.FeedbackName = "未关联"
			if p.Feedback != "" {
				a, _, failure := parseFeedback(p.Feedback)
				if failure == nil {
					for _, target := range d.Points {
						if target.Address == a && dataPointCategory(cfg.Protocol, target) == "yx" {
							r.FeedbackName = target.Name
							if r.FeedbackName == "" {
								r.FeedbackName = fmt.Sprintf("遥信%d", a)
							}
							break
						}
					}
				}
			}
			view.Rows = append(view.Rows, r)
		}
	} else if seed.Addresses != "" {
		addresses, failure := ParseAutoAddresses(seed.Addresses)
		if failure != nil {
			return AutoControlSnapshot{}, failure
		}
		view.Rows = []AutoControlRow{}
		for _, a := range addresses {
			r, ok := old[a]
			if !ok {
				r = AutoControlRow{Address: a, Name: fmt.Sprintf("遥调%d", a), Value: seed.Value, Enabled: true, Count: 1}
			}
			if seed.ReplaceValues {
				r.Value = seed.Value
			}
			view.Rows = append(view.Rows, r)
		}
	}
	if mode == "yt" && seed.ValueType != 0 {
		view.Options.ValueType = seed.ValueType
	}
	if cfg.Protocol != "iec104" {
		view.Options.Direct = true
		view.Options.Double = false
		view.Options.Function = seed.Function
		if view.Options.Function == 0 {
			view.Options.Function = 5
			if mode == "yt" {
				view.Options.Function = 6
			}
		}
	}
	view.ID = newID()
	view.Status = "draft"
	view.Generation++
	view.Revision++
	view.Total = 0
	view.Success = 0
	view.Failure = 0
	view.Current = -1
	view.Errors = []string{}
	view.LastError = ""
	e.autoMu.Lock()
	if e.autoDrafts == nil {
		e.autoDrafts = map[string]*autoControlJob{}
	}
	e.autoDrafts[autoKey(channel, device, mode)] = &autoControlJob{view: view, wake: make(chan struct{}, 1)}
	e.autoMu.Unlock()
	return view, nil
}
func (e *Engine) autoDraftJob(channel, device, mode string) (*autoControlJob, error) {
	if _, _, err := e.autoTarget(channel, device); err != nil {
		return nil, err
	}
	key := autoKey(channel, device, mode)
	e.autoMu.Lock()
	j := e.autoDrafts[key]
	e.autoMu.Unlock()
	if j != nil {
		return j, nil
	}
	view, err := e.GetAutoControl(channel, device, mode)
	if err != nil {
		return nil, err
	}
	view.Status = "draft"
	view.Rows = append([]AutoControlRow{}, view.Rows...)
	e.autoMu.Lock()
	defer e.autoMu.Unlock()
	if e.autoDrafts == nil {
		e.autoDrafts = map[string]*autoControlJob{}
	}
	if existing := e.autoDrafts[key]; existing != nil {
		return existing, nil
	}
	j = &autoControlJob{view: view, wake: make(chan struct{}, 1)}
	e.autoDrafts[key] = j
	return j, nil
}
func (e *Engine) GetAutoControlDraft(channel, device, mode string) (AutoControlSnapshot, error) {
	draft, err := e.autoDraftJob(channel, device, mode)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	view := draft.snapshot()
	e.autoMu.Lock()
	runtime := e.autoJobs[autoKey(channel, device, mode)]
	e.autoMu.Unlock()
	if runtime != nil {
		live := runtime.snapshot()
		view.Total = live.Total
		view.Success = live.Success
		view.Failure = live.Failure
		view.Errors = live.Errors
		view.LastError = live.LastError
		view.Revision += live.Revision
		for i := range view.Rows {
			if i < len(live.Rows) && view.Rows[i].Address == live.Rows[i].Address {
				view.Rows[i].Selection = live.Rows[i].Selection
				view.Rows[i].Execution = live.Rows[i].Execution
				view.Rows[i].FeedbackResult = live.Rows[i].FeedbackResult
				view.Rows[i].Executed = live.Rows[i].Executed
			}
		}
	}
	return view, nil
}

func (e *Engine) UpdateAutoDraftOptions(channel, device, mode string, o AutoControlOptions) (AutoControlSnapshot, error) {
	if err := validateAutoOptions(o); err != nil {
		return AutoControlSnapshot{}, err
	}
	j, err := e.autoDraftJob(channel, device, mode)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.view.Protocol != "iec104" {
		o.Function = j.view.Options.Function
		o.Direct = true
		o.Double = false
	}
	if mode == "yt" {
		o.ValueType = j.view.Options.ValueType
	}
	j.view.Options = o
	j.view.Revision++
	return j.cloneLocked(), nil
}
func (e *Engine) SetAutoDraftValueType(channel, device string, typ byte) error {
	if typ < 48 || typ > 50 {
		return errors.New("遥调类型无效")
	}
	j, err := e.autoDraftJob(channel, device, "yt")
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	options := j.view.Options
	options.ValueType = typ
	if err := validateAutoRecipe("yt", AutoControlConfiguration{Options: options, Rows: j.view.Rows}); err != nil {
		return err
	}
	j.view.Options = options
	j.view.Revision++
	return nil
}
func (e *Engine) UpdateAutoDraftRows(channel, device, mode string, edits []AutoControlRowEdit) (AutoControlSnapshot, error) {
	j, err := e.autoDraftJob(channel, device, mode)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	rows := append([]AutoControlRow{}, j.view.Rows...)
	if len(edits) > 10000 {
		return AutoControlSnapshot{}, errors.New("修改行数超出范围")
	}
	for _, edit := range edits {
		if edit.Generation != 0 && edit.Generation != j.view.Generation {
			return AutoControlSnapshot{}, errors.New("配置表已更新，请重新编辑")
		}
		if edit.Index < 0 || edit.Index >= len(rows) {
			return AutoControlSnapshot{}, errors.New("配置行不存在")
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
func (e *Engine) ApplyAutoDraftBulk(channel, device string, b AutoControlBulk) (AutoControlSnapshot, error) {
	j, err := e.autoDraftJob(channel, device, "yk")
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
func (e *Engine) SaveAutoControlDraft(channel, device, mode string) (AutoControlSnapshot, error) {
	e.op.Lock()
	defer e.op.Unlock()
	draft, err := e.GetAutoControlDraft(channel, device, mode)
	if err != nil {
		return AutoControlSnapshot{}, err
	}
	recipe := autoRecipe(draft)
	if err = validateAutoRecipe(mode, recipe); err != nil {
		return AutoControlSnapshot{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var cfg *Channel
	var d *Device
	for i := range e.project.Channels {
		if e.project.Channels[i].ID == channel {
			cfg = &e.project.Channels[i]
			d, err = deviceIn(cfg, device)
			break
		}
	}
	if cfg == nil || d == nil || cfg.Role != "client" || cfg.Protocol != draft.Protocol {
		return AutoControlSnapshot{}, errors.New("通道或规约已改变，请重新打开配置")
	}
	unchanged := d.Configuration != nil && reflect.DeepEqual(d.Configuration.AutoTests[mode], recipe)
	if !unchanged {
		e.disconnectConfigurationLocked(channel)
	}
	if d.Configuration == nil {
		version := "标准版"
		if cfg.Protocol == "modbus-tcp" {
			version = "TCP版"
		} else if cfg.Protocol == "modbus-rtu" {
			version = "标准版(RTU)"
		}
		d.Configuration = &DeviceConfiguration{Version: version, Values: map[string]string{}}
	}
	if d.Configuration.AutoTests == nil {
		d.Configuration.AutoTests = map[string]AutoControlConfiguration{}
	}
	d.Configuration.AutoTests[mode] = recipe
	if !unchanged {
		e.dirty = true
		e.markConfigurationLocked()
	}
	cfg.syncLegacy()
	if unchanged {
		e.autoMu.Lock()
		j := e.autoJobs[autoKey(channel, device, mode)]
		e.autoMu.Unlock()
		if j != nil {
			return j.snapshot(), nil
		}
	}
	v := draft
	v.Saved = true
	v.Status = "idle"
	v.ID = newID()
	v.Rows = append([]AutoControlRow{}, recipe.Rows...)
	v.Options = recipe.Options
	v.Generation++
	v.Revision++
	v.Total = 0
	v.Success = 0
	v.Failure = 0
	v.Current = -1
	v.Errors = []string{}
	v.LastError = ""
	// A canceled worker may still be finishing. Give the saved recipe a new job.
	j := &autoControlJob{view: v, wake: make(chan struct{}, 1)}
	e.autoMu.Lock()
	if e.autoJobs == nil {
		e.autoJobs = map[string]*autoControlJob{}
	}
	e.autoJobs[autoKey(channel, device, mode)] = j
	e.autoMu.Unlock()
	result := j.snapshot()

	if job, lookup := e.autoDraftJobUnlocked(channel, device, mode); lookup {
		job.mu.Lock()
		job.view = result
		job.view.Status = "draft"
		job.mu.Unlock()
	}
	return result, nil
}
func (e *Engine) autoDraftJobUnlocked(channel, device, mode string) (*autoControlJob, bool) {
	e.autoMu.Lock()
	defer e.autoMu.Unlock()
	j := e.autoDrafts[autoKey(channel, device, mode)]
	return j, j != nil
}
func (e *Engine) StartSavedAutoControl(channel, device, mode string) (AutoControlSnapshot, error) {
	view, err := e.GetAutoControl(channel, device, mode)
	if err != nil {
		return view, err
	}
	if !view.Saved {
		return view, errors.New("请先保存自动操作配置")
	}
	cfg, d, err := e.autoTarget(channel, device)
	if err != nil {
		return view, err
	}
	if cfg.Protocol != view.Protocol {
		return view, errors.New("规约已更改，请重新保存配置")
	}
	if mode == "yk" {
		for _, r := range view.Rows {
			if !r.Enabled {
				continue
			}
			found := false
			for _, p := range d.Points {
				found = found || p.Address == r.Address && controlCategory(dataPointCategory(cfg.Protocol, p)) == "yk"
			}
			if !found {
				return view, errors.New("点表已更改，请重新保存自动遥控配置")
			}
		}
	}
	return e.StartAutoControl(channel, device, mode)
}

// Discarding an editor draft never stops its separately owned runtime job.
func (e *Engine) DiscardAutoControlDraft(channel, device, mode string) {
	e.autoMu.Lock()
	delete(e.autoDrafts, autoKey(channel, device, mode))
	e.autoMu.Unlock()
}
