package core

import (
	"errors"
	"fmt"
)

func layout104(d Device) (int, int, int) {
	cot := number(d, "txtCOT", number(d, "textEdit2", 2))
	ca := number(d, "txtASDU_Byte", number(d, "textEdit3", 2))
	ioa := number(d, "txtMess_Byte", 3)
	if value(d, "txt_DeviceAddr", "") != "" {
		ioa = number(d, "txtAdd", 3)
	}
	return cot, ca, ioa
}
func littleValue(b []byte) uint32 {
	var n uint32
	for i, v := range b {
		n |= uint32(v) << uint(8*i)
	}
	return n
}
func putLittle(n uint32, size int) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = byte(n >> uint(8*i))
	}
	return b
}
func asduWidth(typ byte) int {
	if n := point104Width(typ); n > 0 {
		return n
	}
	if n := controlWidth(typ); n > 0 {
		if typ >= 58 && typ <= 63 {
			n += 7
		}
		return n
	}
	switch typ {
	case 100, 101:
		return 1
	case 103:
		return 7
	}
	return 0
}

// Internally ASDUs use COT2/CA2/IOA3. Only the peer boundary adapts configured
// field widths, keeping control matching and point metadata independent of layout.
func convertASDU(a []byte, fromCOT, fromCA, fromIOA, toCOT, toCA, toIOA int) ([]byte, error) {
	head := 2 + fromCOT + fromCA
	if len(a) < head {
		return nil, errors.New("ASDU 头不足")
	}
	cot := littleValue(a[2 : 2+fromCOT])
	ca := littleValue(a[2+fromCOT : head])
	if ca >= 1<<uint(toCA*8) || cot >= 1<<uint(toCOT*8) {
		return nil, errors.New("ASDU 地址或传送原因超出配置字节数")
	}
	out := append([]byte{a[0], a[1]}, putLittle(cot, toCOT)...)
	out = append(out, putLittle(ca, toCA)...)
	n := int(a[1] & 127)
	seq := a[1]&128 != 0
	width := asduWidth(a[0])
	if width == 0 && n == 1 {
		width = len(a) - head - fromIOA
	}
	if n == 0 || width < 0 {
		return nil, fmt.Errorf("ASDU 类型 %d 的布局未知", a[0])
	}
	offset := head
	for i := 0; i < n; i++ {
		if !seq || i == 0 {
			if len(a) < offset+fromIOA {
				return nil, errors.New("信息体地址不足")
			}
			address := littleValue(a[offset : offset+fromIOA])
			if address >= 1<<uint(toIOA*8) {
				return nil, errors.New("信息体地址超出配置字节数")
			}
			out = append(out, putLittle(address, toIOA)...)
			offset += fromIOA
		}
		if len(a) < offset+width {
			return nil, errors.New("信息体不足")
		}
		out = append(out, a[offset:offset+width]...)
		offset += width
	}
	if offset != len(a) {
		return nil, errors.New("ASDU 布局长度不符")
	}
	return out, nil
}
func (p *peer) wireASDU(a []byte) ([]byte, error) {
	c, ca, ioa := layout104(p.device104())
	if c == 2 && ca == 2 && ioa == 3 {
		return a, nil
	}
	return convertASDU(a, 2, 2, 3, c, ca, ioa)
}
func (p *peer) canonicalASDU(a []byte) ([]byte, error) {
	c, ca, ioa := layout104(p.device104())
	if c == 2 && ca == 2 && ioa == 3 {
		return a, nil
	}
	return convertASDU(a, c, ca, ioa, 2, 2, 3)
}
