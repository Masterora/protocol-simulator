package core

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
)

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
		return errors.New("多寄存器地址超出范围")
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
