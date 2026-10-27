package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

func WriteMultiplePDU(fc byte, address uint16, values []uint16) ([]byte, error) {
	max := 123
	if fc == 15 {
		max = 1968
	} else if fc != 16 {
		return nil, errors.New("批量写入功能码应为15或16")
	}
	if len(values) < 1 || len(values) > max || int(address)+len(values) > 65536 {
		return nil, errors.New("批量写入范围无效")
	}
	n := len(values) * 2
	if fc == 15 {
		n = (len(values) + 7) / 8
	}
	pdu := make([]byte, 6+n)
	pdu[0] = fc
	binary.BigEndian.PutUint16(pdu[1:], address)
	binary.BigEndian.PutUint16(pdu[3:], uint16(len(values)))
	pdu[5] = byte(n)
	for i, v := range values {
		if fc == 15 {
			if v > 1 {
				return nil, errors.New("线圈值应为0或1")
			}
			pdu[6+i/8] |= byte(v) << uint(i%8)
		} else {
			binary.BigEndian.PutUint16(pdu[6+i*2:], v)
		}
	}
	return pdu, nil
}
func decodeModbusReply(request, reply []byte) ([]Point, error) {
	if len(request) == 0 {
		return nil, errors.New("请求为空")
	}
	fc := request[0]
	if fc != 15 && fc != 16 {
		return DecodeModbusResponse(request, reply)
	}
	if len(reply) == 2 && reply[0] == fc|128 {
		return nil, fmt.Errorf("Modbus异常码 %02X", reply[1])
	}
	if len(request) < 6 || len(reply) != 5 || !bytes.Equal(request[:5], reply) {
		return nil, errors.New("批量写入确认与请求不匹配")
	}
	qty := int(binary.BigEndian.Uint16(request[3:]))
	addr := uint32(binary.BigEndian.Uint16(request[1:]))
	out := make([]Point, qty)
	for i := range out {
		out[i] = Point{Kind: "holding", Address: addr + uint32(i)}
		if fc == 15 {
			out[i].Kind = "coil"
			out[i].Value = float64(request[6+i/8] >> uint(i%8) & 1)
		} else {
			out[i].Value = float64(binary.BigEndian.Uint16(request[6+i*2:]))
		}
	}
	return out, nil
}
func (e *Engine) WriteDeviceModbusBatch(channel, device string, fc byte, address uint16, values []uint16) error {
	s, err := e.getSession(channel)
	if err != nil {
		return err
	}
	d, err := e.Device(channel, device)
	if err != nil {
		return err
	}
	pdu, err := WriteMultiplePDU(fc, address, values)
	if err != nil {
		return err
	}
	return s.requestDeviceModbus(d, pdu)
}

// Automation counts a command only after a matching normal response. The RTU
// stream is single-flight; an uncertain timed-out write requires reconnection.
func (e *Engine) commandModbusContext(ctx context.Context, channel, device string, pdu []byte, timeout int, owner string) error {
	s, err := e.getSession(channel)
	if err != nil {
		return err
	}
	d, err := e.Device(channel, device)
	if err != nil {
		return err
	}
	if d.Unit == 0 && s.cfg.Protocol == "modbus-rtu" {
		return errors.New("自动控制不支持无应答的广播点位")
	}
	return s.forPeers(func(p *peer) error {
		p.mu.Lock()
		if p.controlUncertain {
			p.mu.Unlock()
			return errControlUncertain
		}
		if p.closed || p.batchOwner != owner || p.controlUncertain || len(p.pending) > 0 {
			p.mu.Unlock()
			return errors.New("通道存在待应答请求或控制结果未知")
		}
		for {
			p.transaction++
			if _, ok := p.pending[p.transaction]; !ok {
				break
			}
		}
		tid := p.transaction
		frame := EncodeTCP(tid, d.Unit, pdu)
		if s.cfg.Protocol == "modbus-rtu" {
			tid = 0
			frame = encodeDeviceRTU(d.Unit, pdu, d)
		}
		result := make(chan error, 1)
		p.pending[tid] = pendingRequest{deviceID: d.ID, unit: d.Unit, pdu: append([]byte{}, pdu...), until: time.Now().Add(time.Duration(timeout) * time.Millisecond), result: result}
		summary := "自动控制 · " + d.Name
		if strings.HasPrefix(owner, "points/") {
			summary = "点位操作 · " + d.Name
		}
		err := p.writeLocked(frame, summary)
		if err != nil {
			delete(p.pending, tid)
			if s.cfg.Protocol == "modbus-rtu" {
				p.controlUncertain = true
			}
		}
		p.mu.Unlock()
		if err != nil {
			return err
		}
		select {
		case err = <-result:
			return err
		case <-p.done:
			return errors.New("连接已断开，控制结果未知")
		case <-ctx.Done():
			p.mu.Lock()
			delete(p.pending, tid)
			if s.cfg.Protocol == "modbus-rtu" {
				p.controlUncertain = true
			}
			p.mu.Unlock()
			return ctx.Err()
		}
	})
}

// Resolve the register image and all mapped inputs under one lock. Timed test
// updates cannot be overwritten by a stale partial-register write snapshot.
func (e *Engine) applyModbusCommand(channel, device string, pdu []byte) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID != channel {
			continue
		}
		d, err := deviceIn(c, device)
		if err != nil {
			return nil, err
		}
		reply, updates := ModbusDeviceReply(pdu, *d)
		if len(updates) == 0 {
			return reply, nil
		}
		mapped, err := mappedControlPoints(c.Protocol, *d, updates)
		if err != nil {
			return nil, err
		}
		updates = append(updates, mapped...)
		for _, u := range updates {
			for k := range d.Points {
				if d.Points[k].Kind == u.Kind && d.Points[k].Address == u.Address {
					u.Updated = time.Now().Format("15:04:05.000")
					d.Points[k] = u
					break
				}
			}
		}
		c.syncLegacy()
		e.dirty = true
		e.markPointsLocked(channel, d.ID, updates)
		return reply, nil
	}
	return nil, errors.New("通道不存在")
}

func registerFormat(d Device, tab string) (string, string) {
	prefix := "yc"
	if tab == "mc" {
		prefix = "mc"
	}
	if tab != "yc" && tab != "mc" {
		return "", ""
	}
	// Client/server designer uses different names for the same wire settings.
	key := "cb_" + prefix + "Type"
	fallback := value(d, "cb_"+prefix+"Format", "0")
	typ := choice(Device{Configuration: &DeviceConfiguration{Values: map[string]string{key: value(d, key, fallback)}}}, key, 0)
	kinds := []string{"uint16", "int16", "uint32", "int32", "float32"}
	if tab == "mc" {
		kinds = []string{"uint32", "int32", "float32"}
	}
	if typ < 0 || typ >= len(kinds) {
		return "invalid", ""
	}
	order := choice(d, "cb_"+prefix+"FunWay", 0)
	orders := []string{"AB", "BA"}
	if tab == "mc" || typ >= 2 {
		orders = []string{"ABCD", "BADC", "CDAB", "DCBA"}
	}
	if order < 0 || order >= len(orders) {
		return "invalid", ""
	}
	return kinds[typ], orders[order]
}
func registerWidth(p Point) int {
	if p.DataType == "uint32" || p.DataType == "int32" || p.DataType == "float32" {
		return 2
	}
	return 1
}
func validateRegisterPoint(p Point) error {
	if p.Kind != "holding" && p.Kind != "input" {
		return nil
	}
	if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
		return errors.New("寄存器值必须有限")
	}
	lo, hi, integer := 0.0, 65535.0, true
	switch p.DataType {
	case "", "uint16":
	case "int16":
		lo, hi = -32768, 32767
	case "uint32":
		hi = 4294967295
	case "int32":
		lo, hi = -2147483648, 2147483647
	case "float32":
		lo, hi, integer = -math.MaxFloat32, math.MaxFloat32, false
	default:
		return errors.New("寄存器数据类型无效")
	}
	if p.Value < lo || p.Value > hi || integer && math.Trunc(p.Value) != p.Value {
		return errors.New("寄存器数值超出数据类型范围")
	}
	if uint64(p.Address)+uint64(registerWidth(p)) > 65536 {
		return errors.New("多寄存器点位超出范围")
	}
	allowed := "|AB|BA|"
	if registerWidth(p) == 2 {
		allowed = "|ABCD|BADC|CDAB|DCBA|"
	}
	if p.ByteOrder != "" && !strings.Contains(allowed, "|"+p.ByteOrder+"|") {
		return errors.New("寄存器字节序无效")
	}
	return nil
}
func shuffle(b []byte, order string) []byte {
	if order == "" {
		return b
	}
	s := "AB"
	if len(b) == 4 {
		s = "ABCD"
	}
	out := make([]byte, len(b))
	for i, c := range order {
		j := strings.IndexRune(s, c)
		if i < len(out) && j >= 0 {
			out[i] = b[j]
		}
	}
	return out
}
func unshuffle(b []byte, order string) []byte {
	if order == "" {
		return b
	}
	s := "AB"
	if len(b) == 4 {
		s = "ABCD"
	}
	out := make([]byte, len(b))
	for i, c := range order {
		j := strings.IndexRune(s, c)
		if i < len(b) && j >= 0 {
			out[j] = b[i]
		}
	}
	return out
}
func registerBytes(p Point) []byte {
	b := make([]byte, registerWidth(p)*2)
	if len(b) == 2 {
		binary.BigEndian.PutUint16(b, uint16(int64(p.Value)))
	} else {
		v := uint32(int64(p.Value))
		if p.DataType == "float32" {
			v = math.Float32bits(float32(p.Value))
		}
		binary.BigEndian.PutUint32(b, v)
	}
	return shuffle(b, p.ByteOrder)
}
func registerValue(p Point, b []byte) float64 {
	b = unshuffle(b, p.ByteOrder)
	switch p.DataType {
	case "int16":
		return float64(int16(binary.BigEndian.Uint16(b)))
	case "int32":
		return float64(int32(binary.BigEndian.Uint32(b)))
	case "uint32":
		return float64(binary.BigEndian.Uint32(b))
	case "float32":
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
	}
	return float64(binary.BigEndian.Uint16(b))
}
func expandedRegisters(points []Point) []Point {
	out := []Point{}
	for _, p := range points {
		if p.Kind != "holding" && p.Kind != "input" {
			out = append(out, p)
			continue
		}
		b := registerBytes(p)
		for i := 0; i < len(b); i += 2 {
			out = append(out, Point{Kind: p.Kind, Address: p.Address + uint32(i/2), Value: float64(binary.BigEndian.Uint16(b[i:]))})
		}
	}
	return out
}
func collapseRegisters(d Device, wire []Point, partial bool) ([]Point, error) {
	lookup := map[PointKey]Point{}
	for _, p := range wire {
		lookup[PointKey{p.Kind, p.Address}] = p
	}
	out := []Point{}
	consumed := map[PointKey]bool{}
	for _, p := range d.Points {
		if p.Kind != "holding" && p.Kind != "input" {
			continue
		}
		b := registerBytes(p)
		found, complete := false, true
		for i := 0; i < len(b); i += 2 {
			k := PointKey{p.Kind, p.Address + uint32(i/2)}
			if r, ok := lookup[k]; ok {
				found = true
				binary.BigEndian.PutUint16(b[i:], uint16(r.Value))
				consumed[k] = true
			} else {
				complete = false
			}
		}
		if found && (complete || partial) {
			p.Value = registerValue(p, b)
			if err := validateRegisterPoint(p); err != nil {
				return nil, err
			}
			out = append(out, p)
		}
	}
	for _, p := range wire {
		if !consumed[PointKey{p.Kind, p.Address}] {
			out = append(out, p)
		}
	}
	return out, nil
}
func ModbusDeviceReply(pdu []byte, d Device) ([]byte, []Point) {
	reply, raw := ModbusReply(pdu, expandedRegisters(d.Points))
	if len(raw) == 0 {
		return bitOrderPDU(reply, d, true), nil
	}
	updates, err := collapseRegisters(d, raw, true)
	if err != nil {
		return []byte{pdu[0] | 128, 3}, nil
	}
	return reply, updates
}

func highCRC(d Device) bool { return strings.HasPrefix(value(d, "cb_check", "1:BA"), "0") }
func encodeDeviceRTU(unit byte, pdu []byte, d Device) []byte {
	b := EncodeRTU(unit, pdu)
	if highCRC(d) {
		b[len(b)-2], b[len(b)-1] = b[len(b)-1], b[len(b)-2]
	}
	return b
}

// Modbus bit data is packed least-significant bit first. The AB option swaps
// adjacent bytes of each packed 16-bit block; a final unpaired byte is retained.
func bitOrderPDU(pdu []byte, d Device, server bool) []byte {
	if len(pdu) < 3 || pdu[0] > 2 {
		return pdu
	}
	key := "cb_yxTwoFun"
	if server {
		key = "cb_yxtwoWay"
	}
	mode := choice(d, key, 0)
	if mode != 1 && mode != 3 {
		return pdu
	}
	out := append([]byte{}, pdu...)
	for i := 2; i+1 < len(out); i += 2 {
		out[i], out[i+1] = out[i+1], out[i]
	}
	return out
}

// Time data is an explicit configurable register mapping, never an OS clock write.
func modbusTimeReply(pdu []byte, d Device, now time.Time) ([]byte, time.Time, bool) {
	if !flag(d, "clockEnabled", false) || len(pdu) < 5 {
		return nil, time.Time{}, false
	}
	address, err := parseAddress(value(d, "TimeAddr", "1000H"), 65535)
	if err != nil {
		return nil, time.Time{}, false
	}
	start := uint32(binary.BigEndian.Uint16(pdu[1:]))
	if start != address {
		return nil, time.Time{}, false
	}
	fc := pdu[0]
	qty := binary.BigEndian.Uint16(pdu[3:])
	if qty != 4 {
		return []byte{fc | 128, 3}, time.Time{}, true
	}
	if fc == 3 || fc == 4 {
		b := append([]byte{fc, 8}, encodeCP56(now)...)
		return append(b, 0), time.Time{}, true
	}
	if fc == 16 {
		if len(pdu) != 14 || pdu[5] != 8 || pdu[13] != 0 {
			return []byte{fc | 128, 3}, time.Time{}, true
		}
		stamp, invalid, err := decodeCP56(pdu[6:13])
		if err != nil || invalid {
			return []byte{fc | 128, 3}, time.Time{}, true
		}
		t, _ := time.Parse(time.RFC3339Nano, stamp)
		return append([]byte{}, pdu[:5]...), t, true
	}
	return nil, time.Time{}, false
}
func (e *Engine) ReadClockDevice(channel, device string) error {
	s, err := e.getSession(channel)
	if err != nil {
		return err
	}
	d, err := e.Device(channel, device)
	if err != nil {
		return err
	}
	if s.cfg.Role != "client" || (s.cfg.Protocol != "modbus-tcp" && s.cfg.Protocol != "modbus-rtu") {
		return errors.New("读时需要Modbus主站")
	}
	a, err := parseAddress(value(d, "TimeAddr", "1000H"), 65532)
	if err != nil {
		return err
	}
	pdu, _ := ReadPDU(3, uint16(a), 4)
	return s.requestDeviceModbusMode(d, pdu, "clock")
}
func (p *peer) modbusClock(d Device, pdu []byte) ([]byte, bool) {
	p.s.mu.Lock()
	offset := p.s.clockOffsets[d.ID]
	p.s.mu.Unlock()
	reply, t, handled := modbusTimeReply(pdu, d, time.Now().Add(offset))
	if !t.IsZero() {
		p.s.mu.Lock()
		if p.s.clockOffsets == nil {
			p.s.clockOffsets = map[string]time.Duration{}
		}
		p.s.clockOffsets[d.ID] = time.Until(t)
		p.s.mu.Unlock()
		p.s.e.log(p.s.cfg.ID, p.address, "SYS", nil, "校时寄存器已更新 · "+d.Name)
	}
	return reply, handled
}
