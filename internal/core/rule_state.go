package core

import (
	"context"
	"errors"
	"time"
)

// RuleRuntimeStatus supports both individual states and legacy task snapshots.
func RuleRuntimeStatus(view *AutoDataSnapshot, index int) string {
	if view == nil || index < 0 || index >= len(view.Config.Rules) {
		return "stopped"
	}
	state := view.Status
	if index < len(view.RuleStates) {
		state = view.RuleStates[index]
	} else if !view.Config.Rules[index].Enabled {
		return "disabled"
	}
	if state == "stopped" && view.LastError != "" {
		return "error"
	}
	if state == "" {
		return "stopped"
	}
	return state
}

// ControlDataRule changes only one rule's runtime state, without editing its
// saved configuration or restarting the channel or the other rules' timers.
func (e *Engine) ControlDataRule(channel, device, category string, index int, action string) (AutoDataSnapshot, error) {
	if category != "" && dataTestCategory(category) == "" {
		return AutoDataSnapshot{}, errors.New("无效规则分类")
	}
	if action != "start" && action != "resume" && action != "stop" {
		return AutoDataSnapshot{}, errors.New("无效单条规则操作")
	}
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	c, d, err := e.dataTargetLocked(channel, device)
	if err != nil {
		e.mu.Unlock()
		return AutoDataSnapshot{}, err
	}
	config := dataConfiguration(d, category)
	if config == nil || index < 0 || index >= len(config.Rules) {
		e.mu.Unlock()
		return AutoDataSnapshot{}, errors.New("规则已变化，请刷新后重试")
	}
	j := e.dataJobs[dataJobKey(channel, device, category)]
	if j == nil || j.view.Status == "stopped" {
		if action == "stop" {
			view := e.dataSnapshotLocked(channel, device, d, category)
			e.mu.Unlock()
			return view, nil
		}
		e.mu.Unlock()
		return e.startAutoDataLocked(channel, device, category, &index)
	}
	if action != "stop" {
		validation := cloneAutoData(config)
		for i := range validation.Rules {
			validation.Rules[i].Enabled = i == index
		}
		if err := validateDataTest(c.Protocol, c.Role, category, validation, deviceValidationPoints(c.Protocol, *d, d.Points)...); err != nil {
			e.mu.Unlock()
			return AutoDataSnapshot{}, err
		}
		matched := false
		rule := config.Rules[index]
		spans, _ := selectionSpans(rule.Points, rule.Start, rule.End, 0xffffff)
		for _, p := range d.Points {
			if dataScopeMatches(category, c.Protocol, p) && p.Kind == rule.Kind && containsPoint(spans, p.Address) {
				matched = true
				break
			}
		}
		if !matched {
			e.mu.Unlock()
			return AutoDataSnapshot{}, errors.New("规则范围内没有已配置点位")
		}
		if j.view.Status == "paused" {
			j.pending, j.cancelPending = context.WithCancel(j.ctx)
		}
		j.view.Status = "running"
		j.view.RuleStates[index] = "running"
		j.next[index] = time.Now().Add(time.Duration(config.Rules[index].IntervalMS) * time.Millisecond)
	} else {
		j.view.RuleStates[index] = "stopped"
		running, paused := false, false
		for _, state := range j.view.RuleStates {
			running = running || state == "running"
			paused = paused || state == "paused"
		}
		if !running && !paused {
			e.finishDataLocked(j, "")
		} else if !running {
			j.view.Status = "paused"
		}
	}
	view := e.dataSnapshotLocked(channel, device, d, category)
	e.mu.Unlock()
	return view, nil
}
