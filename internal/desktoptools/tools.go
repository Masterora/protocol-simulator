// Package desktoptools contains deterministic, offline desktop utilities.
package desktoptools

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	textunicode "golang.org/x/text/encoding/unicode"
	"gytest-go/internal/core"
)

type Request struct {
	Kind        string      `json:"kind"`
	Operation   string      `json:"operation"`
	Input       string      `json:"input"`
	Encoding    string      `json:"encoding"`
	Order       string      `json:"order"`
	Zone        string      `json:"zone"`
	Transaction string      `json:"transaction"`
	Read        *ModbusRead `json:"read,omitempty"`
}
type Field struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type Result struct {
	Text   string  `json:"text"`
	Fields []Field `json:"fields"`
}

func hexBytes(s string) ([]byte, error) {
	parts := strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == ':' || r == ',' })
	if len(parts) == 0 {
		return nil, errors.New("请输入十六进制字节")
	}
	var joined strings.Builder
	joined.Grow(len(s))
	for _, part := range parts {
		if strings.HasPrefix(part, "0x") || strings.HasPrefix(part, "0X") {
			part = part[2:]
		}
		if len(part) == 0 || len(part)%2 != 0 {
			return nil, errors.New("十六进制应为完整字节对")
		}
		joined.WriteString(part)
	}
	b, err := hex.DecodeString(joined.String())
	if err != nil {
		return nil, errors.New("十六进制应为完整字节对")
	}
	return b, nil
}
func encodeHex(b []byte) string {
	parts := make([]string, len(b))
	for i, v := range b {
		parts[i] = fmt.Sprintf("%02X", v)
	}
	return strings.Join(parts, " ")
}
func codec(name string) (encoding.Encoding, error) {
	switch name {
	case "gbk":
		return simplifiedchinese.GBK, nil
	case "gb18030":
		return simplifiedchinese.GB18030, nil
	case "utf16le":
		return textunicode.UTF16(textunicode.LittleEndian, textunicode.IgnoreBOM), nil
	case "utf16be":
		return textunicode.UTF16(textunicode.BigEndian, textunicode.IgnoreBOM), nil
	default:
		return nil, errors.New("请选择UTF-8、GBK、GB18030、UTF-16LE或UTF-16BE")
	}
}
func Convert(r Request) (Result, error) {
	if len(r.Input) > 1<<20 {
		return Result{}, errors.New("输入不能超过1MB")
	}
	var fields []Field
	out := ""
	switch r.Kind {
	case "number":
		switch r.Operation {
		case "integer", "bit-mask", "":
			text := strings.TrimSpace(r.Input)
			base := 10
			if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") || strings.HasPrefix(text, "0b") || strings.HasPrefix(text, "0B") || strings.HasPrefix(text, "0o") || strings.HasPrefix(text, "0O") {
				base = 0
			}
			var n uint64
			if strings.HasPrefix(text, "-") {
				v, err := strconv.ParseInt(text, base, 64)
				if err != nil {
					return Result{}, errors.New("请输入64位整数，可使用0x、0b、0o前缀")
				}
				n = uint64(v)
			} else {
				v, err := strconv.ParseUint(text, base, 64)
				if err != nil {
					return Result{}, errors.New("请输入64位整数，可使用0x、0b、0o前缀")
				}
				n = v
			}
			if r.Operation == "bit-mask" {
				var positions []string
				for mask := n; mask != 0; mask &= mask - 1 {
					positions = append(positions, strconv.Itoa(bits.TrailingZeros64(mask)))
				}
				enabled := strings.Join(positions, ", ")
				if enabled == "" {
					enabled = "无"
				}
				fields = []Field{{"HEX", fmt.Sprintf("0x%X", n)}, {"置位数量", strconv.Itoa(len(positions))}, {"置位位号（最低位 0）", enabled}}
				break
			}
			fields = []Field{{"无符号十进制", strconv.FormatUint(n, 10)}, {"有符号十进制", strconv.FormatInt(int64(n), 10)}, {"十六进制", fmt.Sprintf("0x%X", n)}, {"二进制", "0b" + strconv.FormatUint(n, 2)}, {"八进制", "0o" + strconv.FormatUint(n, 8)}}
		case "register16", "register32", "register64":
			size, _ := strconv.Atoi(strings.TrimPrefix(r.Operation, "register"))
			var err error
			fields, err = registerFields(r, size/8)
			if err != nil {
				return Result{}, err
			}
		case "float32-tohex", "float64-tohex":
			n, err := strconv.ParseFloat(strings.TrimSpace(r.Input), 64)
			if err != nil {
				return Result{}, errors.New("请输入浮点数")
			}
			var b []byte
			if r.Operation == "float32-tohex" {
				if !math.IsInf(n, 0) && math.IsInf(float64(float32(n)), 0) {
					return Result{}, errors.New("数值超出Float32范围")
				}
				b = make([]byte, 4)
				binary.BigEndian.PutUint32(b, math.Float32bits(float32(n)))
			} else {
				b = make([]byte, 8)
				binary.BigEndian.PutUint64(b, math.Float64bits(n))
			}
			if err := registerOrder(b, r.Order); err != nil {
				return Result{}, err
			}
			out = encodeHex(b)
		case "hex-tofloat32", "hex-tofloat64":
			b, err := hexBytes(r.Input)
			if err != nil {
				return Result{}, err
			}
			size := 4
			if r.Operation == "hex-tofloat64" {
				size = 8
			}
			if len(b) != size {
				return Result{}, fmt.Errorf("需要%d个字节", size)
			}
			if err := registerOrder(b, r.Order); err != nil {
				return Result{}, err
			}
			var n float64
			if size == 4 {
				n = float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
			} else {
				n = math.Float64frombits(binary.BigEndian.Uint64(b))
			}
			out = strconv.FormatFloat(n, 'g', -1, 64)
		default:
			return Result{}, errors.New("未知数值转换")
		}
	case "network":
		var err error
		fields, err = subnetFields(r.Input)
		if err != nil {
			return Result{}, err
		}
	case "modbus":
		var err error
		fields, out, err = modbusTool(r)
		if err != nil {
			return Result{}, err
		}
	case "encoding":
		if r.Operation == "encode" {
			b := []byte(r.Input)
			if r.Encoding != "utf8" && r.Encoding != "" {
				c, err := codec(r.Encoding)
				if err != nil {
					return Result{}, err
				}
				b, err = c.NewEncoder().Bytes(b)
				if err != nil {
					return Result{}, fmt.Errorf("编码失败：%w", err)
				}
			}
			out = encodeHex(b)
		} else if r.Operation == "decode" {
			b, err := hexBytes(r.Input)
			if err != nil {
				return Result{}, err
			}
			if r.Encoding != "utf8" && r.Encoding != "" {
				c, e := codec(r.Encoding)
				if e != nil {
					return Result{}, e
				}
				b, err = c.NewDecoder().Bytes(b)
				if err != nil {
					return Result{}, fmt.Errorf("解码失败：%w", err)
				}
			} else if !utf8.Valid(b) {
				return Result{}, errors.New("字节不是有效UTF-8")
			}
			out = string(b)
		} else {
			return Result{}, errors.New("请选择编码或解码")
		}
	case "string":
		switch r.Operation {
		case "base64-encode":
			out = base64.StdEncoding.EncodeToString([]byte(r.Input))
		case "base64-decode":
			b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(r.Input))
			if err != nil {
				return Result{}, errors.New("Base64内容无效")
			}
			if !utf8.Valid(b) {
				return Result{}, errors.New("解码结果不是UTF-8文本，请使用编码工具处理原始字节")
			}
			out = string(b)
		case "url-encode":
			out = url.QueryEscape(r.Input)
		case "url-decode":
			s, err := url.QueryUnescape(r.Input)
			if err != nil {
				return Result{}, err
			}
			out = s
		case "json-compact":
			var compact bytes.Buffer
			if err := json.Compact(&compact, []byte(r.Input)); err != nil {
				return Result{}, err
			}
			out = compact.String()
		case "hex-format", "hexdump", "hex-to-base64", "base64-to-hex":
			if r.Operation == "base64-to-hex" {
				b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(r.Input))
				if err != nil {
					return Result{}, errors.New("Base64内容无效")
				}
				out = encodeHex(b)
				break
			}
			b, err := hexBytes(r.Input)
			if err != nil {
				return Result{}, err
			}
			switch r.Operation {
			case "hex-format":
				out = encodeHex(b)
			case "hexdump":
				if len(b) > 32<<10 {
					return Result{}, errors.New("HEX转储最多支持32KB字节")
				}
				out = hex.Dump(b)
			case "hex-to-base64":
				out = base64.StdEncoding.EncodeToString(b)
			}
		case "json-format":
			var v any
			d := json.NewDecoder(strings.NewReader(r.Input))
			d.UseNumber()
			if err := d.Decode(&v); err != nil {
				return Result{}, err
			}
			if d.Decode(new(any)) != io.EOF {
				return Result{}, errors.New("JSON包含多余内容")
			}
			b, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				return Result{}, err
			}
			out = string(b)
		case "escape":
			out = strconv.Quote(r.Input)
		case "unescape":
			s, err := strconv.Unquote(strings.TrimSpace(r.Input))
			if err != nil {
				return Result{}, err
			}
			out = s
		case "reverse-bytes":
			b, err := hexBytes(r.Input)
			if err != nil {
				return Result{}, err
			}
			for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
				b[i], b[j] = b[j], b[i]
			}
			out = encodeHex(b)
		default:
			return Result{}, errors.New("请选择字符串转换方式")
		}
	case "time":
		loc := time.Local
		switch r.Zone {
		case "", "local":
		case "UTC":
			loc = time.UTC
		case "+08:00":
			loc = time.FixedZone("UTC+08:00", 8*60*60)
		default:
			var err error
			loc, err = time.LoadLocation(r.Zone)
			if err != nil {
				return Result{}, errors.New("时区无效")
			}
		}
		var t time.Time
		switch r.Operation {
		case "now":
			t = time.Now().In(loc)
		case "unix-seconds", "unix-ms":
			n, err := strconv.ParseInt(strings.TrimSpace(r.Input), 10, 64)
			if err != nil {
				return Result{}, errors.New("时间戳应为整数")
			}
			if r.Operation == "unix-ms" {
				t = time.UnixMilli(n).In(loc)
			} else {
				if n < -62135596800 || n > 253402300799 {
					return Result{}, errors.New("秒时间戳超出0001–9999年范围")
				}
				t = time.Unix(n, 0).In(loc)
			}
		case "cp56":
			b, err := hexBytes(r.Input)
			if err != nil {
				return Result{}, err
			}
			if len(b) != 7 {
				return Result{}, errors.New("CP56Time2a需要7字节")
			}
			if b[2]&0x80 != 0 {
				return Result{}, errors.New("CP56时间无效位已置位")
			}
			ms := int(binary.LittleEndian.Uint16(b))
			minute, hour, day, month, year := int(b[2]&63), int(b[3]&31), int(b[4]&31), int(b[5]&15), 2000+int(b[6]&127)
			if ms >= 60000 || minute > 59 || hour > 23 || day < 1 || month < 1 || month > 12 || year > 2099 {
				return Result{}, errors.New("CP56字段越界")
			}
			t = time.Date(year, time.Month(month), day, hour, minute, ms/1000, (ms%1000)*1000000, loc)
			if t.Day() != day || int(t.Month()) != month {
				return Result{}, errors.New("CP56日期无效")
			}
		case "date":
			text := strings.TrimSpace(r.Input)
			var err error
			for _, format := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05", "2006-01-02"} {
				t, err = time.ParseInLocation(format, text, loc)
				if err == nil {
					break
				}
			}
			if err != nil {
				return Result{}, errors.New("请输入YYYY-MM-DD HH:mm:ss或RFC3339时间")
			}
			t = t.In(loc)
		default:
			return Result{}, errors.New("请选择时间转换方式")
		}
		if t.Year() < 1 || t.Year() > 9999 {
			return Result{}, errors.New("时间超出0001–9999年范围")
		}
		fields = []Field{{"日期时间", t.Format(time.RFC3339Nano)}, {"Unix秒", strconv.FormatInt(t.Unix(), 10)}, {"Unix毫秒", strconv.FormatInt(t.UnixMilli(), 10)}, {"时区", loc.String()}}
		if t.Year() >= 2000 && t.Year() <= 2099 {
			b := make([]byte, 7)
			binary.LittleEndian.PutUint16(b, uint16(t.Second()*1000+t.Nanosecond()/1000000))
			b[2], b[3] = byte(t.Minute()), byte(t.Hour())
			wd := int(t.Weekday())
			if wd == 0 {
				wd = 7
			}
			b[4], b[5], b[6] = byte(t.Day()|wd<<5), byte(t.Month()), byte(t.Year()-2000)
			fields = append(fields, Field{"CP56Time2a", encodeHex(b)})
		}
	case "iec104":
		b, err := hexBytes(r.Input)
		if err != nil {
			return Result{}, err
		}
		if len(b) < 6 || b[0] != 0x68 || int(b[1])+2 != len(b) || b[1] < 4 || b[1] > 253 {
			return Result{}, errors.New("不是完整IEC104 APDU：检查68起始符和长度")
		}
		fields = []Field{{"APDU字节数", strconv.Itoa(len(b))}}
		if b[2]&1 == 0 {
			if len(b) < 12 {
				return Result{}, errors.New("I帧缺少完整ASDU头")
			}
			fields = append(fields, Field{"帧格式", "I帧"}, Field{"发送序号", fmt.Sprint(binary.LittleEndian.Uint16(b[2:]) >> 1)}, Field{"接收序号", fmt.Sprint(binary.LittleEndian.Uint16(b[4:]) >> 1)}, Field{"类型标识", fmt.Sprint(b[6])}, Field{"信息体数量", fmt.Sprint(b[7] & 127)}, Field{"连续地址SQ", fmt.Sprint(b[7] >> 7)}, Field{"传送原因", fmt.Sprint(b[8] & 63)}, Field{"否定确认", fmt.Sprint(b[8]&64 != 0)}, Field{"测试标志", fmt.Sprint(b[8]&128 != 0)}, Field{"源发地址", fmt.Sprint(b[9])}, Field{"公共单元地址", fmt.Sprint(binary.LittleEndian.Uint16(b[10:]))})
			if len(b) >= 15 {
				fields = append(fields, Field{"首信息体地址", fmt.Sprint(uint32(b[12]) | uint32(b[13])<<8 | uint32(b[14])<<16)}, Field{"信息体原始字节", encodeHex(b[12:])})
			}
			if b[4]&1 != 0 {
				return Result{}, errors.New("I帧接收序号保留位无效")
			}
			asdu := b[6:]
			ca := binary.LittleEndian.Uint16(b[10:])
			points, err := core.Decode104Points(asdu, ca)
			if err != nil {
				return Result{}, err
			}
			names := map[string]string{"yx": "遥信", "yc": "遥测", "mc": "累计量", "position": "步位置"}
			for i, p := range points {
				value := strconv.FormatFloat(p.Value, 'g', -1, 64) + fmt.Sprintf("；品质0x%02X", p.Quality)
				if p.Timestamp != "" {
					value += "；时标" + p.Timestamp
					if p.TimeInvalid {
						value += "（无效）"
					}
				}
				fields = append(fields, Field{fmt.Sprintf("信息体%d · %s · 地址%d", i+1, names[p.Kind], p.Address), value})
			}
			if b[6] >= 45 && b[6] <= 50 || b[6] >= 58 && b[6] <= 63 {
				command, err := core.Decode104Control(asdu)
				if err != nil {
					return Result{}, err
				}
				fields = append(fields, Field{"控制操作", map[string]string{"select": "选择", "execute": "执行", "cancel": "撤销"}[command.Operation]}, Field{"控制点位", fmt.Sprint(command.Address)}, Field{"控制值", strconv.FormatFloat(command.Value, 'g', -1, 64)})
				if command.Timestamp != "" {
					fields = append(fields, Field{"控制时标", command.Timestamp})
				}
			}
			switch b[6] {
			case 100, 101, 105:
				if len(asdu) != 10 || asdu[1] != 1 {
					return Result{}, errors.New("系统命令信息体数量或长度无效")
				}
				label := map[byte]string{100: "总召唤限定词", 101: "累计量召唤限定词", 105: "复位限定词"}[b[6]]
				fields = append(fields, Field{label, fmt.Sprint(asdu[9])})
			case 102:
				if len(asdu) != 9 || asdu[1] != 1 {
					return Result{}, errors.New("读命令信息体数量或长度无效")
				}
			case 103:
				if len(asdu) != 16 || asdu[1] != 1 {
					return Result{}, errors.New("校时命令信息体数量或长度无效")
				}
				stamp, err := Convert(Request{Kind: "time", Operation: "cp56", Input: encodeHex(asdu[9:]), Zone: "local"})
				if err != nil {
					return Result{}, err
				}
				fields = append(fields, Field{"校时时间", stamp.Fields[0].Value})
			}

		} else if b[2]&3 == 1 {
			if len(b) != 6 || b[2] != 1 || b[3] != 0 || b[4]&1 != 0 {
				return Result{}, errors.New("S帧控制域或长度无效")
			}
			fields = append(fields, Field{"帧格式", "S帧"}, Field{"接收序号", fmt.Sprint(binary.LittleEndian.Uint16(b[4:]) >> 1)})
		} else {
			names := map[byte]string{7: "STARTDT act", 11: "STARTDT con", 19: "STOPDT act", 35: "STOPDT con", 67: "TESTFR act", 131: "TESTFR con"}
			name, ok := names[b[2]]
			if !ok || len(b) != 6 || b[3] != 0 || b[4] != 0 || b[5] != 0 {
				return Result{}, errors.New("U帧控制域或长度无效")
			}
			fields = append(fields, Field{"帧格式", "U帧"}, Field{"命令", name})
		}
	default:
		return Result{}, errors.New("工具类型无效")
	}
	if fields == nil {
		fields = []Field{}
	}
	if out == "" && len(fields) > 0 {
		lines := make([]string, len(fields))
		for i, v := range fields {
			lines[i] = v.Name + "：" + v.Value
		}
		out = strings.Join(lines, "\n")
	}
	return Result{Text: out, Fields: fields}, nil
}
