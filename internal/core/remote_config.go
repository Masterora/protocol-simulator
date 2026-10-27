package core

import (
	"errors"
	"fmt"
	"reflect"
	"time"
)

// Control simulation is scoped to one of the two command categories. General
// interrogation/clock policies remain separate from these command policies.
type ControlSimulation struct {
	Mode     string           `json:"mode"`
	DelayMS  int              `json:"delayMs"`
	Mappings []ControlMapping `json:"mappings"`
}
type ControlMapping struct {
	Source PointKey `json:"source"`
	Target PointKey `json:"target"`
	Invert bool     `json:"invert"`
}

func controlCategory(kind string) string {
	if kind == "step" {
		return "yt"
	}
	return kind
}
func simulationFor(d Device, category string) ControlSimulation {
	if d.Configuration != nil {
		if c, ok := d.Configuration.Controls[category]; ok {
			c.Mappings = append([]ControlMapping{}, c.Mappings...)
			return c
		}
	}
	return ControlSimulation{Mode: value(d, "responseMode", "normal"), DelayMS: number(d, "responseDelayMs", 0), Mappings: []ControlMapping{}}
}
func findRemotePoint(points []Point, key PointKey) (Point, bool) {
	for _, p := range points {
		if p.Kind == key.Kind && p.Address == key.Address {
			return p, true
		}
	}
	return Point{}, false
}
func validateControlSimulation(protocol, role string, d Device, category string, cfg ControlSimulation) error {
	if role != "server" || category != "yk" && category != "yt" {
		return errors.New("控制配置仅适用于子站遥控或遥调")
	}
	if cfg.Mode != "normal" && cfg.Mode != "deny" && cfg.Mode != "silent" {
		return errors.New("应答策略无效")
	}
	if cfg.DelayMS < 0 || cfg.DelayMS > 60000 || len(cfg.Mappings) > 10000 {
		return errors.New("应答延迟应为0–60000毫秒，最多10000条映射")
	}
	seen := map[[2]PointKey]bool{}
	for i, m := range cfg.Mappings {
		s, ok := findRemotePoint(d.Points, m.Source)
		if !ok || controlCategory(dataPointCategory(protocol, s)) != category {
			return fmt.Errorf("第%d条：源点不属于当前控制点表", i+1)
		}
		t, ok := findRemotePoint(d.Points, m.Target)
		expected := "yc"
		if category == "yk" {
			expected = "yx"
		}
		if !ok || dataPointCategory(protocol, t) != expected {
			return fmt.Errorf("第%d条：映射目标应为已配置的%s点", i+1, expected)
		}
		if category == "yt" && m.Invert {
			return errors.New("遥调映射不支持反向开关量")
		}
		pair := [2]PointKey{m.Source, m.Target}
		if seen[pair] {
			return errors.New("映射重复")
		}
		seen[pair] = true
	}
	return nil
}
func (e *Engine) GetControlSimulation(channel, device, category string) (ControlSimulation, error) {
	d, err := e.Device(channel, device)
	if err != nil {
		return ControlSimulation{}, err
	}
	if category != "yk" && category != "yt" {
		return ControlSimulation{}, errors.New("控制分类无效")
	}
	cfg := simulationFor(d, category)
	for _, source := range d.Points {
		if category != "yk" || source.Feedback == "" || source.Kind != "yk" && source.Tab != "yk" && source.Kind != "coil" {
			continue
		}
		key := PointKey{source.Kind, source.Address}
		explicit := false
		for _, m := range cfg.Mappings {
			explicit = explicit || m.Source == key
		}
		if explicit {
			continue
		}
		addr, inverse, err := parseFeedback(source.Feedback)
		if err != nil {
			continue
		}
		protocol := "modbus-tcp"
		if source.Kind == "yk" {
			protocol = "iec104"
		}
		for _, target := range d.Points {
			if target.Address == addr && dataPointCategory(protocol, target) == "yx" {
				cfg.Mappings = append(cfg.Mappings, ControlMapping{Source: key, Target: PointKey{target.Kind, target.Address}, Invert: inverse})
				break
			}
		}
	}
	return cfg, nil
}
func (e *Engine) SaveControlSimulation(channel, device, category string, cfg ControlSimulation) error {
	return e.saveControlSimulation(channel, device, category, cfg, false)
}
func (e *Engine) saveControlSimulation(channel, device, category string, cfg ControlSimulation, disconnect bool) error {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID != channel {
			continue
		}
		d, err := deviceIn(c, device)
		if err != nil {
			return err
		}
		if err = validateControlSimulation(c.Protocol, c.Role, *d, category, cfg); err != nil {
			return err
		}
		if disconnect {
			old := simulationFor(*d, category)
			sameMappings := reflect.DeepEqual(old.Mappings, cfg.Mappings) || len(old.Mappings) == 0 && len(cfg.Mappings) == 0
			legacy := false
			if category == "yk" {
				for _, p := range d.Points {
					legacy = legacy || p.Feedback != "" && controlCategory(dataPointCategory(c.Protocol, p)) == category
				}
			}
			if old.Mode == cfg.Mode && old.DelayMS == cfg.DelayMS && sameMappings && !legacy {
				return nil
			}
			e.disconnectConfigurationLocked(channel)
		}
		if d.Configuration == nil {
			d.Configuration = &DeviceConfiguration{Version: "标准版", Values: map[string]string{}}
			if c.Protocol == "modbus-tcp" {
				d.Configuration.Version = "TCP版"
			} else if c.Protocol == "modbus-rtu" {
				d.Configuration.Version = "标准版(RTU)"
			}
		}
		if d.Configuration.Controls == nil {
			d.Configuration.Controls = map[string]ControlSimulation{}
		}
		cfg.Mappings = append([]ControlMapping{}, cfg.Mappings...)
		d.Configuration.Controls[category] = cfg
		if category == "yk" {
			for k := range d.Points {
				if controlCategory(dataPointCategory(c.Protocol, d.Points[k])) == category {
					d.Points[k].Feedback = ""
				}
			}
			c.syncLegacy()
		}
		e.dirty = true
		e.markConfigurationLocked()
		return nil
	}
	return errors.New("通道不存在")
}

// Caller holds Engine.mu. Resolve/validate the entire mapping before committing
// the command, so an invalid target cannot leave a half-applied command behind.
func mappedControlPoints(protocol string, d Device, sources []Point) ([]Point, error) {
	result := []Point{}
	for _, s := range sources {
		category := controlCategory(dataPointCategory(protocol, s))
		if category != "yk" && category != "yt" {
			continue
		}
		mappings := simulationFor(d, category).Mappings
		explicit := false
		for _, m := range mappings {
			explicit = explicit || m.Source == (PointKey{s.Kind, s.Address})
		}
		if s.Feedback != "" && category == "yk" && !explicit {
			a, inverse, err := parseFeedback(s.Feedback)
			if err != nil {
				return nil, err
			}
			kind := "yx"
			if protocol != "iec104" {
				kind = "discrete"
			}
			for _, target := range d.Points {
				if target.Address == a && dataPointCategory(protocol, target) == "yx" {
					kind = target.Kind
					break
				}
			}
			mappings = append(mappings, ControlMapping{Source: PointKey{s.Kind, s.Address}, Target: PointKey{kind, a}, Invert: inverse})
		}
		for _, m := range mappings {
			if m.Source != (PointKey{s.Kind, s.Address}) {
				continue
			}
			t, ok := findRemotePoint(d.Points, m.Target)
			if !ok {
				return nil, errors.New("控制映射目标已不存在")
			}
			t = deviceValidationPoints(protocol, d, []Point{t})[0]
			v := s.Value
			if category == "yk" {
				on := v != 0
				if m.Invert {
					on = !on
				}
				v = 0
				if on {
					v = 1
				}
				if t.WireType == 3 || t.WireType == 31 {
					v++
				}
			} else if protocol == "iec104" && (s.WireType == 47 || s.WireType == 60) {
				// applyControl104 computes a unit change in the command point. Apply that
				// delta independently to each bound measurement's current value.
				original, ok := findRemotePoint(d.Points, PointKey{s.Kind, s.Address})
				if !ok {
					return nil, errors.New("遥调点位已不存在")
				}
				v = t.Value + s.Value - original.Value
			} else if protocol == "iec104" && (s.WireType == 48 || s.WireType == 61) && (t.WireType == 9 || t.WireType == 21 || t.WireType == 34) {
				v /= 32768
			}
			t.Value = v
			t.Updated = time.Now().Format("15:04:05.000")
			if timeTagged(t.WireType) {
				t.Timestamp = time.Now().Format(time.RFC3339Nano)
				t.TimeInvalid = false
			}
			if err := validatePoints(deviceValidationPoints(protocol, d, []Point{t}), protocol); err != nil {
				return nil, fmt.Errorf("控制映射：%w", err)
			}
			result = append(result, t)
		}
	}
	if protocol == "iec104" {
		return applyTelemetryRelations(d, result)
	}
	return result, nil
}

func validateRemoteReferences(c Channel, d Device) error {
	if d.Configuration == nil {
		return nil
	}
	for category, cfg := range d.Configuration.Controls {
		if err := validateControlSimulation(c.Protocol, c.Role, d, category, cfg); err != nil {
			return err
		}
	}
	return validateRelations(c.Protocol, c.Role, d, d.Configuration.Relations)
}
