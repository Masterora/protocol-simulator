package core

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type PointValueResult struct {
	Mode      string `json:"mode"`
	Receivers int    `json:"receivers"`
}

// Commit a single simulated input without replacing its other live fields or
// broadcasting the rest of the device. Offline edits remain available to polls.
func (e *Engine) SetPointValue(channel, device string, key PointKey, value float64) (PointValueResult, error) {
	return e.editPointCell(channel, device, key, "value", Point{Value: value}, true)
}

func (e *Engine) EditPointCell(channel, device string, key PointKey, field string, patch Point) (PointValueResult, error) {
	return e.editPointCell(channel, device, key, field, patch, false)
}

func (e *Engine) editPointCell(channel, device string, key PointKey, field string, patch Point, telemetryOnly bool) (PointValueResult, error) {
	if field != "address" && field != "name" && field != "value" && field != "quality" {
		return PointValueResult{}, errors.New("不支持的点表字段")
	}
	if field == "name" && len([]rune(patch.Name)) > 200 {
		return PointValueResult{}, errors.New("描述最多200字")
	}
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	var point Point
	var ca uint16
	protocol, role := "", ""
	found := false
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID != channel {
			continue
		}
		if telemetryOnly && c.Role != "server" {
			e.mu.Unlock()
			return PointValueResult{}, errors.New("请在子站编辑模拟遥信、遥测值")
		}
		protocol, role = c.Protocol, c.Role
		allowed := protocol == "iec104" && (key.Kind == "yx" || key.Kind == "yc") || strings.HasPrefix(protocol, "modbus-") && (key.Kind == "coil" || key.Kind == "discrete" || key.Kind == "holding" || key.Kind == "input")
		if telemetryOnly && !allowed {
			e.mu.Unlock()
			return PointValueResult{}, errors.New("此点不支持直接编辑遥信、遥测值")
		}
		d, err := deviceIn(c, device)
		if err != nil {
			e.mu.Unlock()
			return PointValueResult{}, err
		}
		for j, original := range d.Points {
			if original.Kind != key.Kind || original.Address != key.Address {
				continue
			}
			point = original
			switch field {
			case "address":
				if point.Tab == "" && d.Configuration != nil {
					for _, r := range d.Configuration.Ranges {
						if r.Kind == key.Kind && key.Address >= r.Address && uint64(key.Address) < uint64(r.Address)+uint64(r.Count) {
							point.Tab = r.Tab
							break
						}
					}
				}
				point.Address = patch.Address
			case "name":
				point.Name = patch.Name
			case "quality":
				point.Quality = patch.Quality
			case "value":
				point.Value = patch.Value
			}
			if field == "address" {
				for k, other := range d.Points {
					if k != j && other.Kind == point.Kind && other.Address == point.Address {
						e.mu.Unlock()
						return PointValueResult{}, errors.New("点表地址重复")
					}
				}
			}
			if err := validatePoints([]Point{point}, protocol); err != nil {
				e.mu.Unlock()
				return PointValueResult{}, err
			}
			candidate := append([]Point{}, d.Points...)
			candidate[j] = point
			if err := validatePoints(candidate, protocol); err != nil {
				e.mu.Unlock()
				return PointValueResult{}, err
			}
			point.Updated = time.Now().Format("15:04:05.000")
			if timeTagged(point.WireType) {
				point.Timestamp = time.Now().Format(time.RFC3339Nano)
				point.TimeInvalid = false
			}
			if field == "address" {
				e.cancelDataLocked(channel, d.ID)
			}
			d.Points[j] = point
			ca = d.CommonAddress
			c.syncLegacy()
			e.dirty = true
			found = true
			break
		}
		break
	}
	s := e.running[channel]
	e.mu.Unlock()
	if !found {
		return PointValueResult{}, errors.New("通道、装置或点位已不存在，请刷新后重试")
	}
	if role != "server" || (field != "value" && field != "quality") || (protocol == "iec104" && key.Kind != "yx" && key.Kind != "yc") {
		return PointValueResult{Mode: "stored"}, nil
	}
	if protocol != "iec104" {
		return PointValueResult{Mode: "polled"}, nil
	}
	if s == nil {
		return PointValueResult{Mode: "offline"}, nil
	}
	peers := readyDataPeers(s)
	if len(peers) == 0 {
		return PointValueResult{Mode: "offline"}, nil
	}
	result := PointValueResult{Mode: "reported"}
	var failures []error
	for _, p := range peers {
		if err := p.sendI(PointASDU(point, ca, 3), "手动变位上送"); err != nil {
			failures = append(failures, err)
		} else {
			result.Receivers++
		}
	}
	if len(failures) > 0 {
		return result, fmt.Errorf("数值已保存，已提交 %d 个连接，上送失败：%w", result.Receivers, errors.Join(failures...))
	}
	return result, nil
}
