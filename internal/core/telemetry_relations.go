package core

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"time"
)

// GYTest CustomizedFunc.YxYcRelation has these five strategies: YC summation,
// YX→YX, YX→YC, YC→YX and YC→YC. YC conditions include both interval ends.
type TelemetryRelation struct {
	Enabled    bool     `json:"enabled"`
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Source     uint32   `json:"source"`
	Min        float64  `json:"min"`
	Max        float64  `json:"max"`
	Targets    []uint32 `json:"targets"`
	Value      float64  `json:"value"`
	SumSources []uint32 `json:"sumSources,omitempty"`
}

func cloneRelations(rows []TelemetryRelation) []TelemetryRelation {
	if rows == nil {
		return nil
	}
	out := append([]TelemetryRelation{}, rows...)
	for i := range out {
		if out[i].Targets != nil {
			out[i].Targets = append([]uint32{}, out[i].Targets...)
		}
		if out[i].SumSources != nil {
			out[i].SumSources = append([]uint32{}, out[i].SumSources...)
		}
	}
	return out
}
func relationKinds(r TelemetryRelation) (string, string) {
	switch r.Type {
	case "yx-yx":
		return "yx", "yx"
	case "yx-yc":
		return "yx", "yc"
	case "yc-yx":
		return "yc", "yx"
	case "yc-yc", "sum":
		return "yc", "yc"
	}
	return "", ""
}
func validateRelations(protocol, role string, d Device, rows []TelemetryRelation) error {
	if len(rows) == 0 {
		return nil
	}
	if protocol != "iec104" || role != "server" {
		return errors.New("动态关联仅适用于104子站")
	}
	if len(rows) > 256 {
		return errors.New("最多256组动态关联")
	}
	edges := map[PointKey][]PointKey{}
	total := 0
	for i, r := range rows {
		if !r.Enabled {
			continue
		}
		from, to := relationKinds(r)
		if from == "" || len(r.Targets) == 0 || len(r.Targets) > 1000 {
			return fmt.Errorf("第%d组：关联类型或目标无效", i+1)
		}
		for _, v := range []float64{r.Min, r.Max, r.Value} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return errors.New("关联数值必须有限")
			}
		}
		if r.Min > r.Max {
			return errors.New("关联区间下限不能大于上限")
		}
		sources := []uint32{r.Source}
		if r.Type == "sum" {
			sources = r.SumSources
			if len(sources) < 1 || len(sources) > 1000 {
				return errors.New("求和应有1–1000个源遥测")
			}
		}
		if from == "yx" && (r.Min != r.Max || r.Min != 0 && r.Min != 1) {
			return errors.New("遥信触发状态应为0或1")
		}
		for _, a := range sources {
			if _, ok := findRemotePoint(d.Points, PointKey{from, a}); !ok {
				return fmt.Errorf("第%d组：源点%d不存在", i+1, a)
			}
		}
		for _, a := range r.Targets {
			key := PointKey{to, a}
			p, ok := findRemotePoint(d.Points, key)
			if !ok {
				return fmt.Errorf("第%d组：目标点%d不存在", i+1, a)
			}
			p = deviceValidationPoints(protocol, d, []Point{p})[0]
			if r.Type != "sum" {
				p.Value = r.Value
				if to == "yx" && (p.WireType == 3 || p.WireType == 31) {
					p.Value++
				}
				if err := validatePoints(deviceValidationPoints(protocol, d, []Point{p}), protocol); err != nil {
					return err
				}
			}
			for _, src := range sources {
				edges[PointKey{from, src}] = append(edges[PointKey{from, src}], key)
				total++
			}
		}
		if total > 10000 {
			return errors.New("关联连线最多10000条")
		}
	}
	state := map[PointKey]byte{}
	var visit func(PointKey) bool
	visit = func(k PointKey) bool {
		if state[k] == 1 {
			return false
		}
		if state[k] == 2 {
			return true
		}
		state[k] = 1
		for _, n := range edges[k] {
			if !visit(n) {
				return false
			}
		}
		state[k] = 2
		return true
	}
	for k := range edges {
		if !visit(k) {
			return errors.New("存在循环联动，请移除相互关联")
		}
	}
	return nil
}

// Return the final values of each affected point exactly once. Acyclic rules
// may cascade; unchanged values do not retrigger or create duplicate reports.
func applyTelemetryRelations(d Device, updates []Point) ([]Point, error) {
	if d.Configuration == nil || len(d.Configuration.Relations) == 0 {
		return updates, nil
	}
	d.Points = deviceValidationPoints("iec104", d, d.Points)
	updates = deviceValidationPoints("iec104", d, updates)
	current := map[PointKey]Point{}
	for _, p := range d.Points {
		current[PointKey{p.Kind, p.Address}] = p
	}
	queue := []PointKey{}
	order := []PointKey{}
	changed := map[PointKey]bool{}
	set := func(p Point) {
		key := PointKey{p.Kind, p.Address}
		old, ok := current[key]
		current[key] = p
		if !changed[key] {
			order = append(order, key)
			changed[key] = true
		}
		if !ok || old.Value != p.Value {
			queue = append(queue, key)
		}
	}
	for _, p := range updates {
		set(p)
	}
	steps := 0
	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]
		source := current[key]
		steps++
		if steps > 20000 {
			return nil, errors.New("关联更新次数超出限制")
		}
		for _, r := range d.Configuration.Relations {
			if !r.Enabled {
				continue
			}
			from, to := relationKinds(r)
			if key.Kind != from {
				continue
			}
			v := r.Value
			if r.Type == "sum" {
				found := false
				v = 0
				for _, a := range r.SumSources {
					found = found || a == key.Address
					p, ok := current[PointKey{"yc", a}]
					if !ok {
						return nil, errors.New("求和源点已不存在")
					}
					v += p.Value
				}
				if !found {
					continue
				}
			} else {
				state := source.Value
				if from == "yx" && (source.WireType == 3 || source.WireType == 31) {
					if source.Value != 1 && source.Value != 2 {
						continue
					}
					state--
				}
				if key.Address != r.Source || state < r.Min || state > r.Max {
					continue
				}
			}
			for _, a := range r.Targets {
				target, ok := current[PointKey{to, a}]
				if !ok {
					return nil, errors.New("联动目标已不存在")
				}
				value := v
				if to == "yx" && (target.WireType == 3 || target.WireType == 31) {
					value++
				}
				if target.Value == value {
					continue
				}
				target.Value = value
				target.Updated = time.Now().Format("15:04:05.000")
				if timeTagged(target.WireType) {
					target.Timestamp = time.Now().Format(time.RFC3339Nano)
					target.TimeInvalid = false
				}
				if err := validatePoints(deviceValidationPoints("iec104", d, []Point{target}), "iec104"); err != nil {
					return nil, fmt.Errorf("动态关联：%w", err)
				}
				set(target)
			}
		}
	}
	out := make([]Point, 0, len(order))
	for _, k := range order {
		out = append(out, current[k])
	}
	return out, nil
}
func (e *Engine) GetTelemetryRelations(channel, device string) ([]TelemetryRelation, error) {
	d, err := e.Device(channel, device)
	if err != nil {
		return nil, err
	}
	if d.Configuration == nil {
		return []TelemetryRelation{}, nil
	}
	return cloneRelations(d.Configuration.Relations), nil
}
func (e *Engine) SaveTelemetryRelations(channel, device string, rows []TelemetryRelation) error {
	return e.saveTelemetryRelations(channel, device, rows, false)
}
func (e *Engine) saveTelemetryRelations(channel, device string, rows []TelemetryRelation, disconnect bool) error {
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
		if err = validateRelations(c.Protocol, c.Role, *d, rows); err != nil {
			return err
		}
		if disconnect {
			if d.Configuration == nil && len(rows) == 0 || d.Configuration != nil && (reflect.DeepEqual(d.Configuration.Relations, rows) || len(d.Configuration.Relations) == 0 && len(rows) == 0) {
				return nil
			}
			e.disconnectConfigurationLocked(channel)
		}
		if d.Configuration == nil {
			d.Configuration = &DeviceConfiguration{Version: "标准版", Values: map[string]string{}}
		}
		d.Configuration.Relations = cloneRelations(rows)
		e.dirty = true
		e.markConfigurationLocked()
		return nil
	}
	return errors.New("通道不存在")
}
