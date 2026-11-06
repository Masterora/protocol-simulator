package core

import (
	"errors"
	"reflect"
)

type DeviceTarget struct {
	Channel string
	Device  string
}
type ServerConfigEffect struct {
	StoppedChannels []string
	ResetDevices    []DeviceTarget
}
type ServerPointHint struct {
	Channel, Device string
	Old, Next       *PointKey
	Fields          map[string]bool
}

func TelemetryConfiguration(d Device) AutoDataConfig {
	cfg := dataConfiguration(&d, "telemetry")
	if cfg == nil {
		return AutoDataConfig{Rules: []AutoDataRule{}, StopOnDisconnect: true}
	}
	return *cfg
}
func ValidateServerPointReferences(c Channel, d Device) error { return validateRemoteReferences(c, d) }
func ServerPointReferenced(d Device, key PointKey) bool {
	configs := []*AutoDataConfig{d.AutoData}
	for _, cfg := range d.DataTests {
		configs = append(configs, cfg)
	}
	for _, cfg := range configs {
		if cfg == nil {
			continue
		}
		for _, rule := range cfg.Rules {
			if rule.Kind != key.Kind {
				continue
			}
			spans, err := selectionSpans(rule.Points, rule.Start, rule.End, 16777215)
			if err == nil && containsPoint(spans, key.Address) {
				return true
			}
		}
	}
	return false
}
func connectionDefinition(c Channel) Channel {
	c.Name = ""
	c.Devices = nil
	c.Points = nil
	c.Unit = 0
	c.CommonAddress = 0
	return c
}
func dataDefinition(d Device) Device {
	d.Name = ""
	d.Points = append([]Point{}, d.Points...)
	for i := range d.Points {
		p := &d.Points[i]
		p.Name = ""
		p.Value = 0
		p.Quality = 0
		p.Updated = ""
		p.Timestamp = ""
		p.TimeInvalid = false
	}
	return d
}
func pointMap(points []Point) map[PointKey]Point {
	out := map[PointKey]Point{}
	for _, p := range points {
		out[PointKey{p.Kind, p.Address}] = p
	}
	return out
}
func channelMap(channels []Channel) map[string]Channel {
	out := map[string]Channel{}
	for _, c := range channels {
		out[c.ID] = c
	}
	return out
}
func deviceMap(devices []Device) map[string]Device {
	out := map[string]Device{}
	for _, d := range devices {
		out[d.ID] = d
	}
	return out
}

// ApplyServerConfiguration commits validated file data before touching runtime.
// Data-table/rule edits retain transports; connection changes leave only their
// affected channels stopped for an explicit reconnect. Unedited live values survive.
func (e *Engine) ApplyServerConfiguration(previous, next Project, hint ServerPointHint, commit func() error) (ServerConfigEffect, error) {
	var effect ServerConfigEffect
	normalized, err := NormalizeProject(next)
	if err != nil {
		return effect, err
	}
	next = normalized
	before, after := channelMap(previous.Channels), channelMap(next.Channels)
	for id, c := range after {
		old, exists := before[id]
		if !exists || !reflect.DeepEqual(connectionDefinition(old), connectionDefinition(c)) {
			effect.StoppedChannels = append(effect.StoppedChannels, id)
		}
		oldDevices := deviceMap(old.Devices)
		newDevices := deviceMap(c.Devices)
		for oldID := range oldDevices {
			if _, ok := newDevices[oldID]; !ok {
				effect.ResetDevices = append(effect.ResetDevices, DeviceTarget{id, oldID})
			}
		}
		for _, d := range c.Devices {
			bindingOnly := false
			if hint.Channel == id && hint.Device == d.ID && hint.Fields["mappings"] {
				bindingOnly = true
				for field := range hint.Fields {
					if field != "mappings" && field != "name" && field != "value" && field != "quality" {
						bindingOnly = false
					}
				}
			}
			if previous, ok := oldDevices[d.ID]; !ok || !bindingOnly && !reflect.DeepEqual(dataDefinition(previous), dataDefinition(d)) {
				effect.ResetDevices = append(effect.ResetDevices, DeviceTarget{id, d.ID})
			}
		}
	}
	for id := range before {
		if _, ok := after[id]; !ok {
			effect.StoppedChannels = append(effect.StoppedChannels, id)
		}
	}
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	if e.path == "" {
		e.mu.Unlock()
		return effect, errors.New("工程没有配置文件路径")
	}
	if err = commit(); err != nil {
		e.mu.Unlock()
		return effect, err
	}
	e.mu.Unlock()
	for _, id := range effect.StoppedChannels {
		e.stopLocked(id)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	liveChannels := channelMap(e.project.Channels)
	for ci := range next.Channels {
		c := &next.Channels[ci]
		oldC := before[c.ID]
		liveC := liveChannels[c.ID]
		oldDevices, liveDevices := deviceMap(oldC.Devices), deviceMap(liveC.Devices)
		for di := range c.Devices {
			d := &c.Devices[di]
			oldPoints, livePoints := pointMap(oldDevices[d.ID].Points), pointMap(liveDevices[d.ID].Points)
			for pi := range d.Points {
				p := &d.Points[pi]
				key := PointKey{p.Kind, p.Address}
				originalKey := key
				force := map[string]bool{}
				if hint.Channel == c.ID && hint.Device == d.ID && hint.Old != nil && hint.Next != nil && *hint.Next == key {
					originalKey = *hint.Old
					force = hint.Fields
				}
				old, known := oldPoints[originalKey]
				live, exists := livePoints[originalKey]
				if !known || !exists {
					continue
				}
				if p.Value == old.Value && !force["value"] {
					p.Value = live.Value
				}
				if p.Quality == old.Quality && !force["quality"] {
					p.Quality = live.Quality
				}
				if p.Updated == old.Updated {
					p.Updated = live.Updated
				}
				if p.Timestamp == old.Timestamp {
					p.Timestamp = live.Timestamp
				}
				if p.TimeInvalid == old.TimeInvalid {
					p.TimeInvalid = live.TimeInvalid
				}
			}
		}
		c.syncLegacy()
	}
	for _, target := range effect.ResetDevices {
		e.cancelDataLocked(target.Channel, target.Device)
		for key, job := range e.dataJobs {
			if job.view.Channel == target.Channel && job.view.Device == target.Device {
				delete(e.dataJobs, key)
			}
		}
	}
	for _, id := range effect.StoppedChannels {
		delete(e.states, id)
	}
	e.project = next
	e.dirty = false
	e.markConfigurationLocked()
	return effect, nil
}

func ServerRuleIncludes(protocol string, rule AutoDataRule, p Point) bool {
	if rule.Kind != p.Kind || !dataScopeMatches("telemetry", protocol, p) {
		return false
	}
	spans, err := selectionSpans(rule.Points, rule.Start, rule.End, 16777215)
	return err == nil && containsPoint(spans, p.Address)
}
