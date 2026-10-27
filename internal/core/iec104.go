package core

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

const (
	StartAct = byte(0x07)
	StartCon = byte(0x0b)
	StopAct  = byte(0x13)
	StopCon  = byte(0x23)
	TestAct  = byte(0x43)
	TestCon  = byte(0x83)
)

func UFrame(control byte) []byte { return []byte{0x68, 4, control, 0, 0, 0} }
func SFrame(receive uint16) []byte {
	b := []byte{0x68, 4, 1, 0, 0, 0}
	binary.LittleEndian.PutUint16(b[4:], (receive&0x7fff)<<1)
	return b
}
func IFrame(send, receive uint16, asdu []byte) []byte {
	b := make([]byte, 6+len(asdu))
	b[0] = 0x68
	b[1] = byte(len(b) - 2)
	binary.LittleEndian.PutUint16(b[2:], (send&0x7fff)<<1)
	binary.LittleEndian.PutUint16(b[4:], (receive&0x7fff)<<1)
	copy(b[6:], asdu)
	return b
}

func Read104(r io.Reader) ([]byte, error) {
	b := make([]byte, 2)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	if b[0] != 0x68 || b[1] < 4 || b[1] > 253 {
		return nil, errors.New("104 帧起始符或长度无效")
	}
	tail := make([]byte, int(b[1]))
	_, err := io.ReadFull(r, tail)
	return append(b, tail...), err
}

func ASDU(typ, vsq, cot byte, ca uint16, payload []byte) []byte {
	b := []byte{typ, vsq, cot, 0, byte(ca), byte(ca >> 8)}
	return append(b, payload...)
}
func interrogation(ca uint16, cot byte) []byte { return ASDU(100, 1, cot, ca, []byte{0, 0, 0, 20}) }
func ioa(address uint32) []byte {
	return []byte{byte(address), byte(address >> 8), byte(address >> 16)}
}

// CP56Time2a uses local calendar fields; the invalid-time flag is retained.
func encodeCP56(t time.Time) []byte {
	b := make([]byte, 7)
	binary.LittleEndian.PutUint16(b, uint16(t.Second()*1000+t.Nanosecond()/1000000))
	b[2] = byte(t.Minute())
	b[3] = byte(t.Hour())
	b[4] = byte(t.Day())
	b[5] = byte(t.Month())
	b[6] = byte(t.Year() % 100)
	return b
}
func decodeCP56(b []byte) (string, bool, error) {
	if len(b) < 7 {
		return "", false, errors.New("CP56 时间不足")
	}
	ms := int(binary.LittleEndian.Uint16(b))
	min, hour, day, month, year := int(b[2]&63), int(b[3]&31), int(b[4]&31), int(b[5]&15), 2000+int(b[6]&127)
	if ms >= 60000 || min > 59 || hour > 23 || day < 1 || month < 1 || month > 12 || year > 2099 {
		return "", false, errors.New("CP56 时间字段无效")
	}
	t := time.Date(year, time.Month(month), day, hour, min, ms/1000, (ms%1000)*1000000, time.Local)
	if t.Day() != day {
		return "", false, errors.New("CP56 日期无效")
	}
	return t.Format("2006-01-02T15:04:05.000Z07:00"), b[2]&128 != 0, nil
}
func configured104Type(d Device, kind string) (byte, error) {
	if kind == "yx" {
		if value(d, "cbYX_Type", "单点") == "双点" {
			if flag(d, "isHasTime", false) {
				return 31, nil
			}
			return 3, nil
		}
		if flag(d, "isHasTime", false) {
			return 30, nil
		}
		return 1, nil
	}
	if kind == "mc" {
		return 15, nil
	}
	if kind == "position" {
		return 5, nil
	}
	if kind != "yc" {
		return 0, nil
	}
	s := value(d, "cbYC_Type", "带品质短浮点(type:13)")
	for _, n := range []byte{9, 11, 13, 21, 34, 35, 36} {
		if strings.Contains(s, fmt.Sprintf("type:%d)", n)) || s == fmt.Sprint(n) {
			return n, nil
		}
	}
	return 0, errors.New("遥测类型无效")
}

// Imported projects may omit the per-point type while retaining device settings.
func deviceValidationPoints(protocol string, d Device, points []Point) []Point {
	if protocol != "iec104" || d.Configuration == nil {
		return points
	}
	resolved := append([]Point{}, points...)
	for i := range resolved {
		if resolved[i].WireType == 0 {
			resolved[i].WireType, _ = configured104Type(d, resolved[i].Kind)
		}
	}
	return resolved
}

func point104Width(typ byte) int {
	switch typ {
	case 1, 3:
		return 1
	case 5:
		return 2
	case 9, 11:
		return 3
	case 21:
		return 2
	case 13, 15:
		return 5
	case 30, 31:
		return 8
	case 32:
		return 9
	case 34, 35:
		return 10
	case 36, 37:
		return 12
	}
	return 0
}
func timeTagged(typ byte) bool { return typ >= 30 && typ <= 37 }
func PointASDU(p Point, ca uint16, cot byte) []byte {
	a, _ := pointsASDU([]Point{p}, ca, cot, false)
	return a
}
func pointsASDU(points []Point, ca uint16, cot byte, sequential bool) ([]byte, error) {
	if len(points) == 0 || len(points) > 127 {
		return nil, errors.New("信息体数量无效")
	}
	typ := points[0].WireType
	if typ == 0 {
		if points[0].Kind == "yx" {
			typ = 1
		} else if points[0].Kind == "mc" {
			typ = 15
		} else if points[0].Kind == "position" {
			typ = 5
		} else {
			typ = 13
		}
	}
	width := point104Width(typ)
	if width == 0 {
		return nil, errors.New("信息体类型无效")
	}
	payload := []byte{}
	vsq := byte(len(points))
	if sequential {
		vsq |= 128
	}
	for i, p := range points {
		if sequential && p.Address != points[0].Address+uint32(i) {
			return nil, errors.New("SQ1 点位必须连续")
		}
		if i == 0 || !sequential {
			payload = append(payload, ioa(p.Address)...)
		}
		b := make([]byte, width)
		switch typ {
		case 1, 30:
			if p.Value != 0 && p.Value != 1 {
				return nil, errors.New("单点值必须为0或1")
			}
			b[0] = byte(p.Value)&1 | p.Quality&0xf0
		case 3, 31:
			if p.Value < 0 || p.Value > 3 || math.Trunc(p.Value) != p.Value {
				return nil, errors.New("双点值必须为0–3整数")
			}
			b[0] = byte(p.Value)&3 | p.Quality&0xf0
		case 5, 32:
			if p.Value < -64 || p.Value > 63 || math.Trunc(p.Value) != p.Value {
				return nil, errors.New("步位置值必须为-64–63整数")
			}
			b[0] = byte(int8(p.Value)) & 0x7f
			b[1] = p.Quality
		case 9, 34:
			if p.Value < -1 || p.Value >= 1 {
				return nil, fmt.Errorf("点位 %d：归一化遥测范围为 -1 ≤ 值 < 1；较大数值请选择标度化或短浮点格式", p.Address)
			}
			binary.LittleEndian.PutUint16(b, uint16(int16(min(32767, math.Round(p.Value*32768)))))
			b[2] = p.Quality
		case 11, 35:
			if p.Value < -32768 || p.Value > 32767 || math.Trunc(p.Value) != p.Value {
				return nil, errors.New("标度遥测应为有符号16位整数")
			}
			binary.LittleEndian.PutUint16(b, uint16(int16(p.Value)))
			b[2] = p.Quality
		case 21:
			if p.Value < -1 || p.Value >= 1 {
				return nil, fmt.Errorf("点位 %d：归一化遥测范围为 -1 ≤ 值 < 1；较大数值请选择标度化或短浮点格式", p.Address)
			}
			binary.LittleEndian.PutUint16(b, uint16(int16(min(32767, math.Round(p.Value*32768)))))
		case 13, 36:
			binary.LittleEndian.PutUint32(b, math.Float32bits(float32(p.Value)))
			b[4] = p.Quality
		case 15, 37:
			if p.Value < -2147483648 || p.Value > 2147483647 || math.Trunc(p.Value) != p.Value {
				return nil, errors.New("累计值必须为有符号32位整数")
			}
			binary.LittleEndian.PutUint32(b, uint32(int32(p.Value)))
			b[4] = p.Quality
		}
		if timeTagged(typ) {
			t := time.Now()
			if p.Timestamp != "" {
				parsed, err := time.Parse(time.RFC3339Nano, p.Timestamp)
				if err != nil {
					return nil, err
				}
				t = parsed
			}
			copy(b[len(b)-7:], encodeCP56(t))
			if p.TimeInvalid {
				b[len(b)-5] |= 128
			}
		}
		payload = append(payload, b...)
	}
	if len(payload)+6 > 249 {
		return nil, errors.New("信息体超过104帧上限")
	}
	return ASDU(typ, vsq, cot, ca, payload), nil
}
func Decode104Points(a []byte, ca uint16) ([]Point, error) {
	if len(a) < 6 {
		return nil, errors.New("ASDU 长度不足")
	}
	if binary.LittleEndian.Uint16(a[4:]) != ca {
		return nil, errors.New("公共点位不匹配")
	}
	typ := a[0]
	width := point104Width(typ)
	if width == 0 {
		return nil, nil
	}
	n := int(a[1] & 127)
	seq := a[1]&128 != 0
	expected := 6 + n*(3+width)
	if seq {
		expected = 9 + n*width
	}
	if n == 0 || len(a) != expected {
		return nil, errors.New("ASDU 信息体数量与长度不匹配")
	}
	offset := 6
	var address uint32
	points := []Point{}
	for i := 0; i < n; i++ {
		if !seq || i == 0 {
			address = uint32(a[offset]) | uint32(a[offset+1])<<8 | uint32(a[offset+2])<<16
			offset += 3
		} else {
			address++
		}
		if address > 0xffffff {
			return nil, errors.New("点位溢出")
		}
		v := a[offset : offset+width]
		p := Point{Address: address, Kind: "yc", WireType: typ}
		switch typ {
		case 1, 30:
			p.Kind = "yx"
			p.Value = float64(v[0] & 1)
			p.Quality = v[0] & 0xf0
		case 3, 31:
			p.Kind = "yx"
			p.Value = float64(v[0] & 3)
			p.Quality = v[0] & 0xf0
		case 5, 32:
			p.Kind = "position"
			x := int8(v[0]<<1) >> 1
			p.Value = float64(x)
			p.Quality = v[1]
		case 9, 34:
			p.Value = float64(int16(binary.LittleEndian.Uint16(v))) / 32768
			p.Quality = v[2]
		case 11, 35:
			p.Value = float64(int16(binary.LittleEndian.Uint16(v)))
			p.Quality = v[2]
		case 21:
			p.Value = float64(int16(binary.LittleEndian.Uint16(v))) / 32768
		case 13, 36:
			p.Value = float64(math.Float32frombits(binary.LittleEndian.Uint32(v)))
			p.Quality = v[4]
		case 15, 37:
			p.Kind = "mc"
			p.Value = float64(int32(binary.LittleEndian.Uint32(v)))
			p.Quality = v[4]
		}
		if timeTagged(typ) {
			var err error
			p.Timestamp, p.TimeInvalid, err = decodeCP56(v[len(v)-7:])
			if err != nil {
				return nil, err
			}
		}
		if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) {
			return nil, errors.New("非有限遥测值")
		}
		points = append(points, p)
		offset += width
	}
	return points, nil
}
func (p *peer) publishPoints(d Device, points []Point, cot byte) error {
	return p.publishPointsContext(context.Background(), d, points, cot, "信息体上送 · "+d.Name)
}
func (p *peer) publishPointsContext(ctx context.Context, d Device, points []Point, cot byte, summary string) error {
	// Periodic and background scans carry the current untimed data. Event
	// timestamps remain stored for spontaneous reporting and SOE records.
	if cot == 1 || cot == 2 || cot == 20 {
		points = append([]Point{}, points...)
		for i := range points {
			switch points[i].WireType {
			case 30:
				points[i].WireType = 1
			case 31:
				points[i].WireType = 3
			case 32:
				points[i].WireType = 5
			case 34:
				points[i].WireType = 9
			case 35:
				points[i].WireType = 11
			case 36:
				points[i].WireType = 13
			case 37:
				points[i].WireType = 15
			}
		}
	}
	seq := value(d, "cbVSQ", "非顺序SQ0") == "顺序SQ1"
	for i := 0; i < len(points); {
		typ := points[i].WireType
		if typ == 0 {
			typ, _ = configured104Type(d, points[i].Kind)
		}
		points[i].WireType = typ
		j := i + 1
		// Keep each manually edited input as one ASDU; bulk publish packs compatible points.
		for j < len(points) && j-i < 15 {
			next := points[j].WireType
			if next == 0 {
				next, _ = configured104Type(d, points[j].Kind)
			}
			if next != typ || seq && points[j].Address != points[j-1].Address+1 {
				break
			}
			points[j].WireType = next
			j++
		}
		a, err := pointsASDU(points[i:j], d.CommonAddress, cot, seq)
		if err != nil {
			return err
		}
		if err = p.sendIContext(ctx, a, summary); err != nil {
			return err
		}
		i = j
	}
	return nil
}

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
		return nil, errors.New("ASDU 点位或传送原因超出配置字节数")
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
				return nil, errors.New("信息体点位不足")
			}
			address := littleValue(a[offset : offset+fromIOA])
			if address >= 1<<uint(toIOA*8) {
				return nil, errors.New("信息体点位超出配置字节数")
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
