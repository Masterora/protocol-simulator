package desktoptools

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"strconv"
	"strings"

	"gytest-go/internal/core"
)

func lrc(data []byte) byte {
	var sum byte
	for _, b := range data {
		sum += b
	}
	return -sum
}

// Register permutations are their own inverse. The first and last words
// exchange places in CDAB, while BADC swaps the bytes within each word.
func registerOrder(data []byte, order string) error {
	switch order {
	case "big":
	case "", "little":
		for i, j := 0, len(data)-1; i < j; i, j = i+1, j-1 {
			data[i], data[j] = data[j], data[i]
		}
	case "word-swap", "byte-swap":
		if len(data) != 4 {
			return errors.New("CDAB / BADC 字序只适用于4字节")
		}
		if order == "word-swap" {
			data[0], data[1], data[2], data[3] = data[2], data[3], data[0], data[1]
		} else {
			data[0], data[1], data[2], data[3] = data[1], data[0], data[3], data[2]
		}
	default:
		return errors.New("字节序无效")
	}
	return nil
}

func registerFields(r Request, size int) ([]Field, error) {
	raw, err := hexBytes(r.Input)
	if err != nil {
		return nil, err
	}
	if len(raw) != size {
		return nil, fmt.Errorf("需要%d个字节", size)
	}
	input := encodeHex(raw)
	if err := registerOrder(raw, r.Order); err != nil {
		return nil, err
	}
	var unsigned uint64
	var signed int64
	var floating string
	switch size {
	case 2:
		n := binary.BigEndian.Uint16(raw)
		unsigned, signed = uint64(n), int64(int16(n))
	case 4:
		n := binary.BigEndian.Uint32(raw)
		unsigned, signed = uint64(n), int64(int32(n))
		floating = strconv.FormatFloat(float64(math.Float32frombits(n)), 'g', -1, 32)
	case 8:
		unsigned = binary.BigEndian.Uint64(raw)
		signed = int64(unsigned)
		floating = strconv.FormatFloat(math.Float64frombits(unsigned), 'g', -1, 64)
	}
	fields := []Field{{"输入字节", input}, {"标准字节序", encodeHex(raw)}, {"无符号整数", strconv.FormatUint(unsigned, 10)}, {"有符号整数", strconv.FormatInt(signed, 10)}}
	if floating != "" {
		fields = append(fields, Field{fmt.Sprintf("Float%d", size*8), floating})
	}
	return fields, nil
}

func subnetFields(input string) ([]Field, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(input))
	if err != nil || !prefix.Addr().Is4() {
		return nil, errors.New("请输入 IPv4/CIDR，例如 192.168.1.25/24")
	}
	bits := prefix.Bits()
	first := binary.BigEndian.Uint32(prefix.Masked().Addr().AsSlice())
	mask := ^uint32(0) << (32 - bits)
	last := first | ^mask
	ip := func(n uint32) string {
		return netip.AddrFrom4([4]byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}).String()
	}
	lo, hi, count := first, last, uint64(last)-uint64(first)+1
	broadcast := "—"
	if bits <= 30 {
		lo, hi, count, broadcast = first+1, last-1, count-2, ip(last)
	}
	return []Field{{"网络", prefix.Masked().String()}, {"子网掩码", ip(mask)}, {"反掩码", ip(^mask)}, {"广播地址", broadcast}, {"可用地址", ip(lo) + " – " + ip(hi)}, {"可用数量", strconv.FormatUint(count, 10)}}, nil
}

var modbusFunctions = map[byte]string{1: "读线圈", 2: "读离散输入", 3: "读保持寄存器", 4: "读输入寄存器", 5: "写单个线圈", 6: "写单个寄存器", 15: "写多个线圈", 16: "写多个寄存器", 22: "屏蔽写寄存器", 23: "读写多个寄存器"}
var modbusExceptions = map[byte]string{1: "非法功能", 2: "非法数据地址", 3: "非法数据值", 4: "设备故障", 5: "确认", 6: "设备忙", 8: "存储校验错误", 10: "网关路径不可用", 11: "网关目标未响应"}

func modbusPDU(data []byte, response bool) ([]Field, error) {
	fc := data[0]
	name := modbusFunctions[fc&0x7f]
	if name == "" {
		name = "其他功能"
	}
	fields := []Field{{"功能码", fmt.Sprintf("0x%02X %s", fc, name)}}
	if len(data) > 1 {
		fields = append(fields, Field{"数据区", encodeHex(data[1:])})
	}
	invalid := errors.New("功能码对应的数据长度、数量或取值无效")
	if fc&0x80 != 0 {
		if !response || len(data) != 2 {
			return nil, invalid
		}
		return append(fields, Field{"异常码", strings.TrimSpace(fmt.Sprintf("0x%02X %s", data[1], modbusExceptions[data[1]]))}), nil
	}
	add := func(name string, n uint16) { fields = append(fields, Field{name, strconv.Itoa(int(n))}) }
	word := func(offset int) uint16 { return binary.BigEndian.Uint16(data[offset:]) }
	switch fc {
	case 1, 2, 3, 4:
		if response {
			if len(data) < 3 || int(data[1]) != len(data)-2 || (fc >= 3 && data[1]%2 != 0) {
				return nil, invalid
			}
			add("数据字节数", uint16(data[1]))
			if fc >= 3 {
				values := make([]string, 0, data[1]/2)
				for i := 2; i < len(data); i += 2 {
					values = append(values, strconv.Itoa(int(word(i))))
				}
				fields = append(fields, Field{"寄存器值", strings.Join(values, "、")})
			}
		} else {
			limit := uint16(125)
			if fc <= 2 {
				limit = 2000
			}
			if len(data) != 5 || word(3) == 0 || word(3) > limit || uint32(word(1))+uint32(word(3)) > 65536 {
				return nil, invalid
			}
			add("起始点位", word(1))
			add("读取数量", word(3))
		}
	case 5, 6:
		if len(data) != 5 || (fc == 5 && word(3) != 0 && word(3) != 0xff00) {
			return nil, invalid
		}
		add("点位", word(1))
		if fc == 5 {
			state := "断开（OFF）"
			if word(3) == 0xff00 {
				state = "接通（ON）"
			}
			fields = append(fields, Field{"线圈值", state})
		} else {
			add("寄存器值", word(3))
		}
	case 15, 16:
		if len(data) < 5 {
			return nil, invalid
		}
		n := word(3)
		limit, bytes := uint16(123), int(n)*2
		if fc == 15 {
			limit, bytes = 1968, (int(n)+7)/8
		}
		if n == 0 || n > limit || uint32(word(1))+uint32(n) > 65536 ||
			(response && len(data) != 5) || (!response && (len(data) != 6+bytes || int(data[5]) != bytes)) {
			return nil, invalid
		}
		add("起始点位", word(1))
		add("写入数量", n)
	case 22:
		if len(data) != 7 {
			return nil, invalid
		}
		add("点位", word(1))
		fields = append(fields, Field{"AND 掩码", fmt.Sprintf("0x%04X", word(3))}, Field{"OR 掩码", fmt.Sprintf("0x%04X", word(5))})
	case 23:
		if response {
			if len(data) < 4 || int(data[1]) != len(data)-2 || data[1]%2 != 0 {
				return nil, invalid
			}
			add("返回寄存器数", uint16(data[1]/2))
		} else {
			if len(data) < 12 {
				return nil, invalid
			}
			read, write := word(3), word(7)
			if read == 0 || read > 125 || write == 0 || write > 121 ||
				uint32(word(1))+uint32(read) > 65536 || uint32(word(5))+uint32(write) > 65536 ||
				int(data[9]) != int(write)*2 || len(data) != 10+int(data[9]) {
				return nil, invalid
			}
			add("读取起始点位", word(1))
			add("读取数量", read)
			add("写入起始点位", word(5))
			add("写入数量", write)
		}
	}
	return fields, nil
}

type ModbusRead struct {
	Unit     string `json:"unit"`
	Function string `json:"function"`
	Address  string `json:"address"`
	Quantity string `json:"quantity"`
}

func (r *ModbusRead) payload() ([]byte, error) {
	if r == nil {
		return nil, errors.New("请填写读取报文参数")
	}
	data := make([]byte, 6)
	for i, param := range []struct {
		label string
		value string
		bits  int
	}{{"站号", r.Unit, 8}, {"功能码", r.Function, 8}, {"起始点位", r.Address, 16}, {"数量", r.Quantity, 16}} {
		n, err := strconv.ParseUint(strings.TrimSpace(param.value), 10, param.bits)
		if err != nil {
			return nil, fmt.Errorf("%s须为0–%d的十进制整数", param.label, (1<<param.bits)-1)
		}
		if i < 2 {
			data[i] = byte(n)
		} else {
			binary.BigEndian.PutUint16(data[(i-1)*2:], uint16(n))
		}
	}
	if data[0] == 0 || data[1] < 1 || data[1] > 4 {
		return nil, errors.New("读取报文需要非零站号，功能码须为01–04")
	}
	if _, err := modbusPDU(data[1:], false); err != nil {
		return nil, err
	}
	return data, nil
}

func modbusTool(r Request) ([]Field, string, error) {
	format := r.Encoding
	operation := r.Operation
	if format != "rtu" && format != "tcp" && format != "ascii" {
		return nil, "", errors.New("请选择 RTU、TCP 或 ASCII")
	}
	if operation != "build" && operation != "read" && operation != "request" && operation != "response" {
		return nil, "", errors.New("请选择解析请求、解析响应或封装报文")
	}
	input := strings.TrimSpace(r.Input)
	if len(input) > 4096 {
		return nil, "", errors.New("一次处理一帧 Modbus 报文")
	}
	if format == "ascii" && operation != "build" && operation != "read" {
		if !strings.HasPrefix(input, ":") {
			return nil, "", errors.New("Modbus ASCII 帧须以冒号开头")
		}
		input = input[1:]
	}
	var data []byte
	var err error
	if operation == "read" {
		data, err = r.Read.payload()
	} else {
		data, err = hexBytes(input)
	}
	if err != nil {
		return nil, "", err
	}
	fields := []Field{{"协议", "Modbus " + strings.ToUpper(format)}}
	if operation == "build" || operation == "read" {
		if len(data) < 2 || len(data) > 254 || data[1] == 0 {
			return nil, "", errors.New("输入站号、功能码与数据，最多254字节")
		}
		unit, pdu := data[0], data[1:]
		if format != "tcp" && unit > 247 {
			return nil, "", errors.New("串行站号须为0–247")
		}
		switch format {
		case "rtu":
			return nil, encodeHex(core.EncodeRTU(unit, pdu)), nil
		case "ascii":
			return nil, ":" + strings.ReplaceAll(encodeHex(append(data, lrc(data))), " ", "") + "\r\n", nil
		default:
			tid := uint64(1)
			if strings.TrimSpace(r.Transaction) != "" {
				tid, err = strconv.ParseUint(strings.TrimSpace(r.Transaction), 10, 16)
				if err != nil {
					return nil, "", errors.New("事务编号应为0–65535的十进制整数")
				}
			}
			return nil, encodeHex(core.EncodeTCP(uint16(tid), unit, pdu)), nil
		}
	}
	switch format {
	case "rtu":
		if len(data) < 4 || len(data) > 256 {
			return nil, "", errors.New("RTU帧长度无效")
		}
		payload := data[:len(data)-2]
		wire := binary.LittleEndian.Uint16(data[len(data)-2:])
		expected := core.CRC16(payload)
		if wire != expected {
			return nil, "", fmt.Errorf("CRC不匹配，应为 %02X %02X", byte(expected), byte(expected>>8))
		}
		fields = append(fields, Field{"CRC", encodeHex(data[len(data)-2:]) + " · 正确"})
		data = payload
	case "ascii":
		if len(data) < 3 || len(data) > 255 || lrc(data) != 0 {
			return nil, "", errors.New("ASCII帧长度或LRC校验无效")
		}
		fields = append(fields, Field{"LRC", fmt.Sprintf("%02X · 正确", data[len(data)-1])})
		data = data[:len(data)-1]
	case "tcp":
		if len(data) < 8 || len(data) > 260 || binary.BigEndian.Uint16(data[2:]) != 0 || int(binary.BigEndian.Uint16(data[4:])) != len(data)-6 {
			return nil, "", errors.New("MBAP协议编号或长度无效")
		}
		fields = append(fields, Field{"事务编号", strconv.Itoa(int(binary.BigEndian.Uint16(data)))}, Field{"MBAP长度", strconv.Itoa(len(data) - 6)})
		data = data[6:]
	}
	if data[1] == 0 || (format != "tcp" && (data[0] > 247 || operation == "response" && data[0] == 0)) {
		return nil, "", errors.New("站号或功能码无效")
	}
	fields = append(fields, Field{"站号", strconv.Itoa(int(data[0]))})
	pdu, err := modbusPDU(data[1:], operation == "response")
	return append(fields, pdu...), "", err
}
