package core

import (
	"encoding/binary"
	"errors"
	"time"
)

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
