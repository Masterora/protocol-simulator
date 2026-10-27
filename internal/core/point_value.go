package core

import (
	"context"
	"errors"
	"fmt"
	"math"
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
	return e.editPointCell(channel, device, key, "value", Point{Value: value}, true, false)
}

func (e *Engine) EditPointCell(channel, device string, key PointKey, field string, patch Point) (PointValueResult, error) {
	return e.editPointCell(channel, device, key, field, patch, false, false)
}

func (e *Engine) editPointCell(channel, device string, key PointKey, field string, patch Point, telemetryOnly, disconnect bool) (PointValueResult, error) {
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
	var reports []Point
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
						if r.Kind != key.Kind {
							continue
						}
						numbers, _ := rangePointNumbers(protocol, d.Configuration, r)
						matches := false
						for _, n := range numbers {
							matches = matches || n == key.Address
						}
						if matches {
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
						return PointValueResult{}, errors.New("点表点位重复")
					}
				}
			}
			point = deviceValidationPoints(protocol, *d, []Point{point})[0]
			if err := validatePoints([]Point{point}, protocol); err != nil {
				e.mu.Unlock()
				return PointValueResult{}, err
			}
			candidate := append([]Point{}, d.Points...)
			candidate[j] = point
			if err := validateDevicePointTable(*c, *d, candidate); err != nil {
				e.mu.Unlock()
				return PointValueResult{}, err
			}
			if field == "address" {
				dCopy := *d
				dCopy.Points = candidate
				if err := validateRemoteReferences(*c, dCopy); err != nil {
					e.mu.Unlock()
					return PointValueResult{}, err
				}
			}
			if disconnect && field == "address" && point.Address != original.Address {
				e.disconnectConfigurationLocked(channel)
				point = d.Points[j]
				point.Address = patch.Address
				if point.Tab == "" {
					point.Tab = candidate[j].Tab
				}
			}
			point.Updated = time.Now().Format("15:04:05.000")
			if timeTagged(point.WireType) {
				point.Timestamp = time.Now().Format(time.RFC3339Nano)
				point.TimeInvalid = false
			}
			if field == "address" {
				e.cancelDataLocked(channel, d.ID)
			}
			reports = []Point{point}
			if role == "server" && protocol == "iec104" && field == "value" {
				var err error
				reports, err = applyTelemetryRelations(*d, reports)
				if err != nil {
					e.mu.Unlock()
					return PointValueResult{}, err
				}
			}
			d.Points[j] = point
			for _, u := range reports {
				for k := range d.Points {
					if d.Points[k].Kind == u.Kind && d.Points[k].Address == u.Address {
						d.Points[k] = u
						break
					}
				}
			}
			ca = d.CommonAddress
			c.syncLegacy()
			e.dirty = true
			if field == "address" {
				e.markConfigurationLocked()
			} else {
				e.markPointsLocked(channel, d.ID, reports)
			}
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
		if err := p.publishPointsContext(context.Background(), Device{CommonAddress: ca}, reports, 3, "手动变位上送"); err != nil {
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

type TelemetryRangeEdit struct {
	Points   string  `json:"points,omitempty"`
	Category string  `json:"category"`
	Start    uint32  `json:"start"`
	End      uint32  `json:"end"`
	Mode     string  `json:"mode"`
	Value    float64 `json:"value"`
	Quality  *byte   `json:"quality,omitempty"`
}
type TelemetryRangeResult struct {
	Points    int `json:"points"`
	Receivers int `json:"receivers"`
}

func (e *Engine) SetTelemetryRange(channel, device string, r TelemetryRangeEdit) (TelemetryRangeResult, error) {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	c, d, err := e.dataTargetLocked(channel, device)
	if err != nil {
		e.mu.Unlock()
		return TelemetryRangeResult{}, err
	}
	limit := uint32(0xffffff)
	if c.Protocol != "iec104" {
		limit = 65535
	}
	selected, selectErr := selectionSpans(r.Points, r.Start, r.End, limit)
	if selectErr != nil {
		e.mu.Unlock()
		return TelemetryRangeResult{}, selectErr
	}
	if !validDataCategory(r.Category) || r.Mode != "set" && r.Mode != "flip" && r.Mode != "quality" && r.Mode != "raw16" || r.Mode == "flip" && r.Category != "yx" {
		e.mu.Unlock()
		return TelemetryRangeResult{}, errors.New("批量设置分类、范围或方式无效")
	}
	updates := []Point{}
	now := time.Now()
	for _, p := range d.Points {
		if dataPointCategory(c.Protocol, p) != r.Category || !containsPoint(selected, p.Address) {
			continue
		}
		if c.Protocol == "iec104" && p.WireType == 0 && d.Configuration != nil {
			p.WireType, _ = configured104Type(*d, p.Kind)
		}
		switch r.Mode {
		case "raw16":
			if r.Category != "yc" || math.Trunc(r.Value) != r.Value || r.Value < 0 || r.Value > 65535 {
				e.mu.Unlock()
				return TelemetryRangeResult{}, errors.New("无符号码值应为0–65535整数")
			}
			if c.Protocol == "iec104" {
				typ := p.WireType
				if typ == 0 {
					typ, _ = configured104Type(*d, p.Kind)
				}
				switch typ {
				case 9, 21, 34:
					p.Value = float64(int16(uint16(r.Value))) / 32768
				case 11, 35:
					p.Value = float64(int16(uint16(r.Value)))
				default:
					e.mu.Unlock()
					return TelemetryRangeResult{}, errors.New("无符号16位设值仅适用于归一化或标度化遥测")
				}
			} else {
				if registerWidth(p) != 1 {
					e.mu.Unlock()
					return TelemetryRangeResult{}, errors.New("请选择16位寄存器点")
				}
				p.Value = r.Value
				if p.DataType == "int16" {
					p.Value = float64(int16(uint16(r.Value)))
				}
			}
		case "set":
			p.Value = r.Value
		case "flip":
			if p.WireType == 3 || p.WireType == 31 {
				if p.Value != 1 && p.Value != 2 {
					e.mu.Unlock()
					return TelemetryRangeResult{}, errors.New("双点遥信应先设置为1或2")
				}
				p.Value = 3 - p.Value
			} else {
				p.Value = 1 - p.Value
			}
		}
		if r.Quality != nil {
			p.Quality = *r.Quality
		}
		p.Updated = now.Format("15:04:05.000")
		if timeTagged(p.WireType) {
			p.Timestamp = now.Format(time.RFC3339Nano)
			p.TimeInvalid = false
		}
		updates = append(updates, p)
	}
	if len(updates) == 0 {
		e.mu.Unlock()
		return TelemetryRangeResult{}, errors.New("范围内没有对应点位")
	}
	if err = validatePoints(updates, c.Protocol); err == nil && c.Protocol == "iec104" {
		updates, err = applyTelemetryRelations(*d, updates)
	}
	if err != nil {
		e.mu.Unlock()
		return TelemetryRangeResult{}, err
	}
	for _, u := range updates {
		for i := range d.Points {
			if d.Points[i].Kind == u.Kind && d.Points[i].Address == u.Address {
				d.Points[i] = u
				break
			}
		}
	}
	c.syncLegacy()
	e.dirty = true
	e.markPointsLocked(channel, d.ID, updates)
	protocol := c.Protocol
	reportDevice := *d
	reportDevice.Configuration = cloneConfiguration(d.Configuration)
	s := e.running[channel]
	e.mu.Unlock()
	result := TelemetryRangeResult{Points: len(updates)}
	if protocol != "iec104" || s == nil {
		return result, nil
	}
	for _, p := range readyDataPeers(s) {
		if err = p.publishPoints(reportDevice, updates, 3); err != nil {
			return result, fmt.Errorf("点值已保存，上报失败：%w", err)
		}
		result.Receivers++
	}
	return result, nil
}
