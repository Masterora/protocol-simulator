package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrPointConfigurationChanged = errors.New("工程配置已变化，请刷新后重试")

type ManualPointRequest struct {
	Channel  string   `json:"channel"`
	Device   string   `json:"device"`
	Kind     string   `json:"kind"`
	Address  uint32   `json:"address"`
	Metadata uint64   `json:"metadata"`
	Action   string   `json:"action"`
	Value    *float64 `json:"value,omitempty"`
	Quality  *byte    `json:"quality,omitempty"`
}
type ManualPointResult struct {
	Point     *Point `json:"point,omitempty"`
	Applied   bool   `json:"applied"`
	Mode      string `json:"mode"`
	Receivers int    `json:"receivers"`
	Error     string `json:"error,omitempty"`
}

// OperatePoint serializes with automatic generation and reporting. Patch only
// supplied fields; a quality edit must not restore an older automatic value.
// It never writes the project file or changes a data job's lifecycle.
func (e *Engine) OperatePoint(ctx context.Context, req ManualPointRequest) (result ManualPointResult, err error) {
	if req.Channel == "" || req.Device == "" || req.Kind == "" {
		return result, errors.New("必须指定通道、装置和点类型")
	}
	if req.Action != "set" && req.Action != "report" {
		return result, errors.New("不支持的点位操作")
	}
	if req.Action == "set" && req.Value == nil && req.Quality == nil || req.Action == "report" && (req.Value != nil || req.Quality != nil) {
		return result, errors.New("设值须提供数值或品质；立即上报不接受修改字段")
	}
	e.op.Lock()
	defer e.op.Unlock()
	if err = ctx.Err(); err != nil {
		return result, err
	}
	e.mu.Lock()
	if req.Metadata == 0 || req.Metadata != e.viewMetadata {
		e.mu.Unlock()
		return result, ErrPointConfigurationChanged
	}
	c, d, err := e.dataTargetLocked(req.Channel, req.Device)
	if err != nil {
		e.mu.Unlock()
		return result, err
	}
	allowed := c.Protocol == "iec104" && (req.Kind == "yx" || req.Kind == "yc") || strings.HasPrefix(c.Protocol, "modbus-") && (req.Kind == "coil" || req.Kind == "discrete" || req.Kind == "holding" || req.Kind == "input")
	if !allowed {
		e.mu.Unlock()
		return result, errors.New("只支持子站遥信、遥测或 Modbus 数据点，不下发遥控、遥调")
	}
	if req.Action == "report" && c.Protocol != "iec104" {
		e.mu.Unlock()
		return result, errors.New("Modbus 由主站读取，不支持主动上报")
	}
	index := -1
	for i, p := range d.Points {
		if p.Kind == req.Kind && p.Address == req.Address {
			index = i
			break
		}
	}
	if index < 0 {
		e.mu.Unlock()
		return result, errors.New("所选点位已不存在，请刷新后重试")
	}
	point := deviceValidationPoints(c.Protocol, *d, []Point{d.Points[index]})[0]
	reports := []Point{point}
	if req.Action == "set" {
		if req.Value != nil {
			point.Value = *req.Value
		}
		if req.Quality != nil {
			point.Quality = *req.Quality
		}
		point = deviceValidationPoints(c.Protocol, *d, []Point{point})[0]
		if err = validatePoints([]Point{point}, c.Protocol); err != nil {
			e.mu.Unlock()
			return result, err
		}
		now := time.Now()
		point.Updated = now.Format("15:04:05.000")
		if timeTagged(point.WireType) {
			point.Timestamp = now.Format(time.RFC3339Nano)
			point.TimeInvalid = false
		}
		reports = []Point{point}
		if req.Value != nil && c.Protocol == "iec104" {
			reports, err = applyTelemetryRelations(*d, reports)
			if err != nil {
				e.mu.Unlock()
				return result, err
			}
		}
		for _, update := range reports {
			for i, p := range d.Points {
				if p.Kind == update.Kind && p.Address == update.Address {
					d.Points[i] = update
					break
				}
			}
		}
		c.syncLegacy()
		e.markPointsLocked(c.ID, d.ID, reports)
		result.Applied = true
	}
	result.Point = &point
	protocol, ca := c.Protocol, d.CommonAddress
	session := e.running[c.ID]
	e.mu.Unlock()
	if protocol != "iec104" {
		result.Mode = "polled"
		return result, nil
	}
	result.Mode = "offline"
	if session == nil || len(readyDataPeers(session)) == 0 {
		if req.Action == "report" {
			return result, errors.New("没有已激活的 IEC104 连接，未上报")
		}
		return result, nil
	}
	result.Mode = "reported"
	var failures []error
	for _, peer := range readyDataPeers(session) {
		if sendErr := peer.publishPointsContext(ctx, Device{CommonAddress: ca}, reports, 3, "手动点位上报"); sendErr != nil {
			failures = append(failures, sendErr)
		} else {
			result.Receivers++
		}
	}
	if len(failures) > 0 {
		return result, fmt.Errorf("已提交 %d 个连接，上报失败：%w", result.Receivers, errors.Join(failures...))
	}
	if result.Receivers == 0 {
		result.Mode = "offline"
		return result, errors.New("连接已断开，未上报")
	}
	return result, nil
}
