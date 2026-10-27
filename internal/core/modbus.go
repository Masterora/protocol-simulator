package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

func CRC16(data []byte) uint16 {
	crc := uint16(0xffff)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xa001
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

func EncodeRTU(unit byte, pdu []byte) []byte {
	b := append([]byte{unit}, pdu...)
	crc := CRC16(b)
	return append(b, byte(crc), byte(crc>>8))
}

func EncodeTCP(transaction uint16, unit byte, pdu []byte) []byte {
	b := make([]byte, 7+len(pdu))
	binary.BigEndian.PutUint16(b, transaction)
	binary.BigEndian.PutUint16(b[4:], uint16(len(pdu)+1))
	b[6] = unit
	copy(b[7:], pdu)
	return b
}

func ReadModbusTCP(r io.Reader) ([]byte, error) {
	h := make([]byte, 7)
	if _, err := io.ReadFull(r, h); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(h[4:]))
	if binary.BigEndian.Uint16(h[2:]) != 0 || n < 2 || n > 254 {
		return nil, errors.New("无效的 Modbus TCP 头")
	}
	b := make([]byte, 6+n)
	copy(b, h)
	_, err := io.ReadFull(r, b[7:])
	return b, err
}

func ReadModbusRTU(r io.Reader, server bool) ([]byte, error) { return readModbusRTU(r, server, nil) }
func readModbusRTU(r io.Reader, server bool, highCRC func(byte) bool) ([]byte, error) {
	b := make([]byte, 3)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	n := 8
	if b[1]&0x80 != 0 {
		n = 5
	} else if !server && b[1] >= 1 && b[1] <= 4 {
		n = 5 + int(b[2])
	} else if server && (b[1] == 15 || b[1] == 16) {
		h := make([]byte, 4)
		if _, err := io.ReadFull(r, h); err != nil {
			return nil, err
		}
		b = append(b, h...)
		n = 9 + int(b[6])
	}
	if n < 5 || n > 256 {
		return nil, errors.New("RTU 帧长度无效")
	}
	tail := make([]byte, n-len(b))
	if _, err := io.ReadFull(r, tail); err != nil {
		return nil, err
	}
	b = append(b, tail...)
	actual := binary.LittleEndian.Uint16(b[len(b)-2:])
	if highCRC != nil && highCRC(b[0]) {
		actual = binary.BigEndian.Uint16(b[len(b)-2:])
	}
	if CRC16(b[:len(b)-2]) != actual {
		return nil, errors.New("RTU CRC 校验失败")
	}
	return b, nil
}

func ReadPDU(function byte, start, count uint16) ([]byte, error) {
	max := uint16(125)
	if function == 1 || function == 2 {
		max = 2000
	}
	if function < 1 || function > 4 || count == 0 || count > max || uint32(start)+uint32(count) > 65536 {
		return nil, errors.New("读功能码、起始点位或数量无效")
	}
	b := []byte{function, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(b[1:], start)
	binary.BigEndian.PutUint16(b[3:], count)
	return b, nil
}

func WritePDU(function byte, address, value uint16) ([]byte, error) {
	if function != 5 && function != 6 {
		return nil, errors.New("写入仅支持功能码 05/06")
	}
	if function == 5 {
		if value > 1 {
			return nil, errors.New("线圈写入值只能是 0 或 1")
		}
		if value == 1 {
			value = 0xff00
		}
	}
	b := []byte{function, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(b[1:], address)
	binary.BigEndian.PutUint16(b[3:], value)
	return b, nil
}

// ModbusReply validates the complete request before changing any points.
func ModbusReply(pdu []byte, points []Point) ([]byte, []Point) {
	if len(pdu) == 0 {
		return nil, nil
	}
	fc := pdu[0]
	exception := func(code byte) ([]byte, []Point) { return []byte{fc | 0x80, code}, nil }
	if fc != 1 && fc != 2 && fc != 3 && fc != 4 && fc != 5 && fc != 6 && fc != 15 && fc != 16 {
		return exception(1)
	}
	if len(pdu) < 5 {
		return exception(3)
	}
	start := uint32(binary.BigEndian.Uint16(pdu[1:]))
	qty := int(binary.BigEndian.Uint16(pdu[3:]))
	kind := map[byte]string{1: "coil", 2: "discrete", 3: "holding", 4: "input", 5: "coil", 6: "holding", 15: "coil", 16: "holding"}[fc]
	lookup := map[uint32]Point{}
	for _, p := range points {
		if p.Kind == kind {
			lookup[p.Address] = p
		}
	}
	if fc == 5 || fc == 6 {
		if len(pdu) != 5 {
			return exception(3)
		}
		p, ok := lookup[start]
		if !ok {
			return exception(2)
		}
		if fc == 5 {
			if qty != 0 && qty != 0xff00 {
				return exception(3)
			}
			p.Value = 0
			if qty != 0 {
				p.Value = 1
			}
		} else {
			p.Value = float64(qty)
		}
		return append([]byte{}, pdu...), []Point{p}
	}
	max := 125
	if fc <= 2 {
		max = 2000
	}
	if fc == 15 {
		max = 1968
	}
	if fc == 16 {
		max = 123
	}
	if qty < 1 || qty > max || start+uint32(qty) > 65536 {
		return exception(3)
	}
	if fc <= 4 && len(pdu) != 5 {
		return exception(3)
	}
	if fc == 15 || fc == 16 {
		expected := (qty + 7) / 8
		if fc == 16 {
			expected = qty * 2
		}
		if len(pdu) < 6 || int(pdu[5]) != expected || len(pdu) != 6+expected {
			return exception(3)
		}
	}
	for i := 0; i < qty; i++ {
		if _, ok := lookup[start+uint32(i)]; !ok {
			return exception(2)
		}
	}
	if fc <= 4 {
		bytes := qty * 2
		if fc <= 2 {
			bytes = (qty + 7) / 8
		}
		out := make([]byte, 2+bytes)
		out[0] = fc
		out[1] = byte(bytes)
		for i := 0; i < qty; i++ {
			v := lookup[start+uint32(i)].Value
			if fc <= 2 {
				if v != 0 {
					out[2+i/8] |= 1 << uint(i%8)
				}
			} else {
				binary.BigEndian.PutUint16(out[2+i*2:], uint16(v))
			}
		}
		return out, nil
	}
	changed := make([]Point, qty)
	for i := 0; i < qty; i++ {
		p := lookup[start+uint32(i)]
		if fc == 15 {
			p.Value = float64((pdu[6+i/8] >> uint(i%8)) & 1)
		} else {
			p.Value = float64(binary.BigEndian.Uint16(pdu[6+i*2:]))
		}
		changed[i] = p
	}
	return append([]byte{}, pdu[:5]...), changed
}

func DecodeModbusResponse(request, response []byte) ([]Point, error) {
	if len(request) != 5 || len(response) < 2 {
		return nil, errors.New("Modbus 应答长度不足")
	}
	fc := request[0]
	if response[0] == fc|0x80 {
		if len(response) != 2 {
			return nil, errors.New("异常应答长度错误")
		}
		return nil, fmt.Errorf("Modbus 异常码 %02X", response[1])
	}
	if response[0] != fc {
		return nil, errors.New("Modbus 应答功能码不匹配")
	}
	start := uint32(binary.BigEndian.Uint16(request[1:]))
	count := int(binary.BigEndian.Uint16(request[3:]))
	kind := map[byte]string{1: "coil", 2: "discrete", 3: "holding", 4: "input", 5: "coil", 6: "holding"}[fc]
	if fc == 5 || fc == 6 {
		if len(response) != 5 || string(response) != string(request) {
			return nil, errors.New("写入应答不匹配")
		}
		value := float64(count)
		if fc == 5 {
			value = 0
			if count == 0xff00 {
				value = 1
			}
		}
		return []Point{{Address: start, Kind: kind, Value: value}}, nil
	}
	expected := count * 2
	if fc <= 2 {
		expected = (count + 7) / 8
	}
	if int(response[1]) != expected || len(response) != 2+expected {
		return nil, errors.New("应答字节数与请求不匹配")
	}
	points := make([]Point, count)
	for i := 0; i < count; i++ {
		value := float64(0)
		if fc <= 2 {
			value = float64((response[2+i/8] >> uint(i%8)) & 1)
		} else {
			value = float64(binary.BigEndian.Uint16(response[2+i*2:]))
		}
		points[i] = Point{Address: start + uint32(i), Kind: kind, Value: value}
	}
	return points, nil
}
