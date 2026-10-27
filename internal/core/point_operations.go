package core

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type PointOperationResult struct {
	Points    int  `json:"points"`
	Requests  int  `json:"requests"`
	Broadcast bool `json:"broadcast"`
}

func (e *Engine) ReadDeviceModbusPoints(channel, device string, fc byte, selection string) (PointOperationResult, error) {
	return e.modbusPointOperation(channel, device, fc, selection, 0, false)
}
func (e *Engine) WriteDeviceModbusPoints(channel, device string, fc byte, selection string, value uint16) (PointOperationResult, error) {
	return e.modbusPointOperation(channel, device, fc, selection, value, true)
}

func (e *Engine) modbusPointOperation(channel, device string, fc byte, selection string, writeValue uint16, write bool) (PointOperationResult, error) {
	points, err := ParsePointSelection(selection, 65535)
	if err != nil {
		return PointOperationResult{}, err
	}
	s, err := e.getSession(channel)
	if err != nil {
		return PointOperationResult{}, err
	}
	if s.cfg.Role != "client" || !strings.HasPrefix(s.cfg.Protocol, "modbus") {
		return PointOperationResult{}, errors.New("此操作需要Modbus主站")
	}
	d, err := e.Device(channel, device)
	if err != nil {
		return PointOperationResult{}, err
	}
	if !write && (fc < 1 || fc > 4) || write && fc != 5 && fc != 6 && fc != 15 && fc != 16 {
		return PointOperationResult{}, errors.New("点位操作功能码无效")
	}
	if s.cfg.Protocol == "modbus-rtu" && d.Unit == 0 && !write {
		return PointOperationResult{}, errors.New("广播装置不能读取点位")
	}
	physical := append([]uint32{}, points...)
	if !write && fc >= 3 {
		kind := "holding"
		if fc == 4 {
			kind = "input"
		}
		for _, point := range points {
			if p, ok := findRemotePoint(d.Points, PointKey{kind, point}); ok && registerWidth(p) == 2 {
				if point == 65535 {
					return PointOperationResult{}, errors.New("双寄存器点位越界")
				}
				physical = append(physical, point+1)
			}
		}
	}
	sort.Slice(physical, func(i, j int) bool { return physical[i] < physical[j] })
	spans := []pointSpan{}
	for _, n := range physical {
		if len(spans) > 0 && n <= spans[len(spans)-1].last+1 {
			if n > spans[len(spans)-1].last {
				spans[len(spans)-1].last = n
			}
		} else {
			spans = append(spans, pointSpan{n, n})
		}
	}
	pdus := [][]byte{}
	for _, span := range spans {
		for next := span.first; next <= span.last; {
			max := uint32(125)
			if fc <= 2 {
				max = 2000
			}
			if fc == 5 || fc == 6 {
				max = 1
			}
			if fc == 15 {
				max = 1968
			}
			if fc == 16 {
				max = 123
			}
			count := min(max, span.last-next+1)
			// Do not split a configured two-register value between reads.
			if !write && fc >= 3 && count > 1 && next+count <= span.last {
				kind := "holding"
				if fc == 4 {
					kind = "input"
				}
				if p, ok := findRemotePoint(d.Points, PointKey{kind, next + count - 1}); ok && registerWidth(p) == 2 {
					count--
				}
			}
			var pdu []byte
			if !write {
				pdu, err = ReadPDU(fc, uint16(next), uint16(count))
			} else if fc == 5 || fc == 6 {
				pdu, err = WritePDU(fc, uint16(next), writeValue)
			} else {
				values := make([]uint16, count)
				for i := range values {
					values[i] = writeValue
				}
				pdu, err = WriteMultiplePDU(fc, uint16(next), values)
			}
			if err != nil {
				return PointOperationResult{}, err
			}
			pdus = append(pdus, pdu)
			next += count
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	owner := "points/" + newID()
	release, err := e.reserveAuto(ctx, cancel, s, owner)
	if err != nil {
		return PointOperationResult{}, err
	}
	defer release()
	result := PointOperationResult{Points: len(points), Broadcast: s.cfg.Protocol == "modbus-rtu" && d.Unit == 0}
	timeout := s.cfg.TimeoutMS
	if value(d, "txt_overTime", value(d, "txtTimeOut", "")) != "" {
		timeout = int(seconds(d, "txt_overTime", number(d, "txtTimeOut", 3)).Milliseconds())
	}
	for _, pdu := range pdus {
		if ctx.Err() != nil {
			return result, errors.New("点位操作已停止")
		}
		current, lookupErr := e.getSession(channel)
		if lookupErr != nil || current != s {
			return result, errors.New("通道已停止或更换")
		}
		if result.Broadcast {
			err = s.forPeers(func(p *peer) error {
				p.mu.Lock()
				defer p.mu.Unlock()
				if p.batchOwner != owner || p.closed {
					return errors.New("连接已结束")
				}
				return p.writeLocked(encodeDeviceRTU(d.Unit, pdu, d), "点位广播写入")
			})
		} else {
			err = e.commandModbusContext(ctx, channel, device, pdu, timeout, owner)
		}
		if err != nil {
			return result, fmt.Errorf("已完成%d组，第%d组失败：%w", result.Requests, result.Requests+1, err)
		}
		result.Requests++
	}
	return result, nil
}

type pointSpan struct{ first, last uint32 }

func parsePointSpans(text string, max uint32) ([]pointSpan, error) {
	if len(text) > 160000 || strings.TrimSpace(text) == "" {
		return nil, errors.New("请填写点位")
	}
	spans := []pointSpan{}
	for _, token := range strings.Split(strings.ReplaceAll(text, "，", ","), ",") {
		parts := strings.Split(strings.TrimSpace(token), "-")
		if len(parts) > 2 {
			return nil, errors.New("点位使用逗号分隔，范围使用-")
		}
		var values [2]uint32
		for i, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				return nil, errors.New("点位不能为空")
			}
			for _, c := range part {
				if c < '0' || c > '9' {
					return nil, errors.New("点位应为十进制整数，多个点位用，分隔，范围用-")
				}
			}
			n, err := strconv.ParseUint(part, 10, 32)
			if err != nil || n > uint64(max) {
				return nil, fmt.Errorf("点位应为0–%d整数", max)
			}
			values[i] = uint32(n)
		}
		if len(parts) == 1 {
			values[1] = values[0]
		}
		if values[1] < values[0] {
			return nil, errors.New("范围末点位不能小于首点位")
		}
		spans = append(spans, pointSpan{values[0], values[1]})
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].first < spans[j].first })
	out := spans[:0]
	for _, s := range spans {
		if len(out) > 0 && uint64(s.first) <= uint64(out[len(out)-1].last)+1 {
			if s.last > out[len(out)-1].last {
				out[len(out)-1].last = s.last
			}
		} else {
			out = append(out, s)
		}
	}
	count := uint64(0)
	for _, s := range out {
		count += uint64(s.last) - uint64(s.first) + 1
	}
	if count > 10000 {
		return nil, errors.New("一次最多10000个点位")
	}
	return out, nil
}
func selectionSpans(text string, start, end, max uint32) ([]pointSpan, error) {
	if text != "" {
		return parsePointSpans(text, max)
	}
	if start > end || end > max {
		return nil, errors.New("点位范围无效")
	}
	return []pointSpan{{start, end}}, nil
}
func containsPoint(spans []pointSpan, point uint32) bool {
	i := sort.Search(len(spans), func(i int) bool { return spans[i].last >= point })
	return i < len(spans) && spans[i].first <= point
}
func overlappingPoints(a, b []pointSpan) bool {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i].first <= b[j].last && b[j].first <= a[i].last {
			return true
		}
		if a[i].last < b[j].last {
			i++
		} else {
			j++
		}
	}
	return false
}
func ParsePointSelection(text string, max uint32) ([]uint32, error) {
	spans, err := parsePointSpans(text, max)
	if err != nil {
		return nil, err
	}
	out := []uint32{}
	for _, s := range spans {
		for n := s.first; ; n++ {
			out = append(out, n)
			if n == s.last {
				break
			}
		}
	}
	return out, nil
}
func rangePointNumbers(protocol string, c *DeviceConfiguration, r PointRange) ([]uint32, error) {
	max := uint32(0xffffff)
	if strings.HasPrefix(protocol, "modbus") {
		max = 65535
	}
	if r.Selection != "" {
		points, err := ParsePointSelection(r.Selection, max)
		if err != nil {
			return nil, err
		}
		if r.Count != len(points) || len(points) > 0 && r.Address != points[0] {
			return nil, errors.New("点位选择与点表数量不一致")
		}
		return points, nil
	}
	width := 1
	if strings.HasPrefix(protocol, "modbus") {
		format, _ := registerFormat(Device{Configuration: c}, r.Tab)
		width = registerWidth(Point{DataType: format})
	}
	if r.Count < 0 || r.Count > 10000 || uint64(r.Address) > uint64(max) || uint64(r.Address)+uint64(r.Count)*uint64(width) > uint64(max)+1 {
		return nil, fmt.Errorf("%s点位超出0–%d", r.Name, max)
	}
	points := make([]uint32, r.Count)
	for i := range points {
		points[i] = r.Address + uint32(i*width)
	}
	return points, nil
}
