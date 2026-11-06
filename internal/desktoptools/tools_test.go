package desktoptools

import (
	"gytest-go/internal/core"
	"strings"
	"testing"
)

func TestNumbersChecksumsAndCodecs(t *testing.T) {
	cases := []struct {
		request Request
		want    string
	}{
		{Request{Kind: "number", Operation: "integer", Input: "0xFFFFFFFFFFFFFFFF"}, "有符号十进制：-1"},
		{Request{Kind: "number", Operation: "float32-tohex", Input: "1", Order: "big"}, "3F 80 00 00"},
		{Request{Kind: "number", Operation: "hex-tofloat32", Input: "00 00 80 3F"}, "1"},
		{Request{Kind: "encoding", Operation: "encode", Input: "中文", Encoding: "gbk"}, "D6 D0 CE C4"},
		{Request{Kind: "encoding", Operation: "decode", Input: "2D 4E 87 65", Encoding: "utf16le"}, "中文"},
		{Request{Kind: "string", Operation: "base64-decode", Input: "5Lit5paH"}, "中文"},
		{Request{Kind: "string", Operation: "reverse-bytes", Input: "00 01 FE"}, "FE 01 00"},
	}
	for _, c := range cases {
		r, err := Convert(c.request)
		if err != nil || !strings.Contains(r.Text, c.want) {
			t.Fatal(c.request, r, err)
		}
	}
	for _, r := range []Request{{Kind: "encoding", Operation: "decode", Input: "FF", Encoding: "utf8"}, {Kind: "number", Operation: "hex-tofloat32", Input: "00"}, {Kind: "number", Input: "18446744073709551616"}} {
		if _, err := Convert(r); err == nil {
			t.Fatal("invalid input accepted", r)
		}
	}
}
func TestTimestampFixedUTCPlusEight(t *testing.T) {
	result, err := Convert(Request{Kind: "time", Operation: "date", Input: "1900-01-01 08:00:00", Zone: "+08:00"})
	if err != nil || !strings.Contains(result.Text, "1900-01-01T08:00:00+08:00") || !strings.Contains(result.Text, "-2208988800") {
		t.Fatal(result, err)
	}
	result, err = Convert(Request{Kind: "time", Operation: "unix-seconds", Input: "-2208988800", Zone: "+08:00"})
	if err != nil || !strings.Contains(result.Text, "1900-01-01T08:00:00+08:00") {
		t.Fatal(result, err)
	}
}

func TestTimeAndIEC104Analysis(t *testing.T) {
	r, err := Convert(Request{Kind: "time", Operation: "date", Input: "2026-11-03T00:00:01.123Z", Zone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	var cp string
	for _, f := range r.Fields {
		if f.Name == "CP56Time2a" {
			cp = f.Value
		}
	}
	decoded, err := Convert(Request{Kind: "time", Operation: "cp56", Input: cp, Zone: "UTC"})
	if err != nil || !strings.Contains(decoded.Text, "2026-11-03T00:00:01.123Z") {
		t.Fatal(decoded, err)
	}
	for _, input := range []string{"68 04 07 00 00 00", "68 04 01 00 02 00", "68 0E 00 00 00 00 64 01 06 00 01 00 00 00 00 14"} {
		if _, err := Convert(Request{Kind: "iec104", Input: input}); err != nil {
			t.Fatal(input, err)
		}
	}
	if _, err = Convert(Request{Kind: "iec104", Input: "68 05 07 00 00 00"}); err == nil {
		t.Fatal("wrong APDU length accepted")
	}
	if _, err = Convert(Request{Kind: "time", Operation: "cp56", Input: "00 00 80 00 01 01 1A"}); err == nil {
		t.Fatal("invalid CP56 accepted")
	}
}

func TestIEC104AnalysisIncludesValuesAndControlSemantics(t *testing.T) {
	asdu := core.PointASDU(core.Point{Kind: "yc", Address: 0x4001, Value: 23.5, WireType: 13}, 1, 3)
	frame := core.IFrame(4, 5, asdu)
	view, err := Convert(Request{Kind: "iec104", Input: encodeHex(frame)})
	if err != nil || !strings.Contains(view.Text, "23.5") || !strings.Contains(view.Text, "地址16385") {
		t.Fatal(view, err)
	}
	command, err := core.EncodeControl104(1, core.Control104Request{Type: 45, Address: 6001, Value: 1, Operation: "select"})
	if err != nil {
		t.Fatal(err)
	}
	view, err = Convert(Request{Kind: "iec104", Input: encodeHex(core.IFrame(0, 0, command))})
	if err != nil || !strings.Contains(view.Text, "控制操作：选择") || !strings.Contains(view.Text, "控制点位：6001") {
		t.Fatal(view, err)
	}
	frame[1]--
	if _, err = Convert(Request{Kind: "iec104", Input: encodeHex(frame)}); err == nil {
		t.Fatal("malformed frame accepted")
	}
}

func TestModbusToolBuildsAndValidatesCommonFrames(t *testing.T) {
	cases := []struct {
		format, frame, response string
	}{
		{"rtu", "01 03 00 00 00 01 84 0A", "01 03 02 00 2A 39 9B"},
		{"tcp", "00 01 00 00 00 06 01 03 00 00 00 01", "00 01 00 00 00 05 01 03 02 00 2A"},
		{"ascii", ":010300000001FB\r\n", ":010302002AD0"},
	}
	for _, c := range cases {
		built, err := Convert(Request{Kind: "modbus", Encoding: c.format, Operation: "build", Input: "01 03 00 00 00 01", Transaction: "1"})
		if err != nil || built.Text != c.frame {
			t.Fatal(c.format, built, err)
		}
		parsed, err := Convert(Request{Kind: "modbus", Encoding: c.format, Operation: "request", Input: c.frame})
		if err != nil || !strings.Contains(parsed.Text, "读取数量：1") {
			t.Fatal(c.format, parsed, err)
		}
		parsed, err = Convert(Request{Kind: "modbus", Encoding: c.format, Operation: "response", Input: c.response})
		if err != nil || !strings.Contains(parsed.Text, "寄存器值：42") {
			t.Fatal(c.format, parsed, err)
		}
	}
	for _, c := range []struct{ format, input string }{
		{"rtu", "01 03 00 00 00 01 84 00"},
		{"ascii", ":010300000001FA"},
		{"tcp", "00 01 00 00 00 07 01 03 00 00 00 01"},
		{"tcp", "00 01 00 00 00 06 01 03 FF FF 00 02"},
	} {
		if _, err := Convert(Request{Kind: "modbus", Encoding: c.format, Operation: "request", Input: c.input}); err == nil {
			t.Fatal("invalid frame accepted", c)
		}
	}
	r, err := Convert(Request{Kind: "modbus", Encoding: "tcp", Operation: "response", Input: "00 01 00 00 00 03 01 83 02"})
	if err != nil || !strings.Contains(r.Text, "非法数据地址") {
		t.Fatal(r, err)
	}
}

func TestGenerateModbusReadUsesZeroBasedAddressAndValidatesRange(t *testing.T) {
	r := Request{Kind: "modbus", Encoding: "tcp", Operation: "read", Transaction: "42",
		Read: &ModbusRead{Unit: "1", Function: "3", Address: "0", Quantity: "1"}}
	out, err := Convert(r)
	if err != nil || out.Text != "00 2A 00 00 00 06 01 03 00 00 00 01" {
		t.Fatal(out, err)
	}
	r.Read.Address, r.Read.Quantity = "65535", "2"
	if _, err := Convert(r); err == nil {
		t.Fatal("read crossing the register boundary was accepted")
	}
}

func TestIndustrialUtilitiesKnownVectors(t *testing.T) {
	for _, c := range []struct{ order, bytes string }{
		{"big", "3F 80 00 00"},
		{"little", "00 00 80 3F"},
		{"word-swap", "00 00 3F 80"},
		{"byte-swap", "80 3F 00 00"},
	} {
		r, err := Convert(Request{Kind: "number", Operation: "register32", Order: c.order, Input: c.bytes})
		if err != nil || !strings.Contains(r.Text, "Float32：1") || !strings.Contains(r.Text, "标准字节序：3F 80 00 00") {
			t.Fatal(c, r, err)
		}
	}
	cases := []struct {
		request Request
		want    string
	}{
		{Request{Kind: "string", Operation: "base64-to-hex", Input: "/wAB"}, "FF 00 01"},
		{Request{Kind: "string", Operation: "hexdump", Input: "48 65 6C 6C 6F"}, "|Hello|"},
		{Request{Kind: "network", Input: "192.168.1.25/24"}, "可用地址：192.168.1.1 – 192.168.1.254"},
		{Request{Kind: "network", Input: "192.168.1.25/31"}, "可用地址：192.168.1.24 – 192.168.1.25"},
		{Request{Kind: "network", Input: "192.168.1.25/32"}, "可用数量：1"},
	}
	for _, c := range cases {
		r, err := Convert(c.request)
		if err != nil || !strings.Contains(r.Text, c.want) {
			t.Fatal(c.request, r, err)
		}
	}
}

func TestBitMask(t *testing.T) {
	mask, err := Convert(Request{Kind: "number", Operation: "bit-mask", Input: "0x80000000000000A5"})
	if err != nil || len(mask.Fields) != 3 || mask.Fields[1].Value != "5" || mask.Fields[2].Value != "0, 2, 5, 7, 63" {
		t.Fatal(mask, err)
	}
}
