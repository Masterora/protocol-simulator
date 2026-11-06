package tui

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

type choicePicker struct {
	title  string
	values []string
	cursor int
	apply  func(string)
}

func choiceLabel(key, value string) string {
	switch key {
	case "kind", "pointKind":
		return translated(value, kindLabels)
	case "role":
		return translated(value, roleLabels)
	case "mode":
		return translated(value, modeLabels)
	case "protocol":
		return translated(value, map[string]string{"iec104": "IEC104", "modbus-tcp": "Modbus TCP", "modbus-rtu": "Modbus RTU", "raw": "RAW 原始数据"})
	case "transport":
		return translated(value, transportLabels)
	case "enabled", "stopOnDisconnect":
		return translated(value, map[string]string{"true": "是", "false": "否"})
	case "parity":
		return translated(value, map[string]string{"N": "无校验", "E": "偶校验", "O": "奇校验"})
	case "cause":
		return translated(value, map[string]string{"0": "自动", "1": "周期扫描（1）", "2": "背景扫描（2）", "3": "突发（3）"})
	case "dataType":
		return translated(value, map[string]string{"": "自动", "uint16": "无符号16位整数", "int16": "有符号16位整数", "uint32": "无符号32位整数", "int32": "有符号32位整数", "float32": "32位浮点数"})
	case "byteOrder":
		if value == "" {
			return "自动"
		}
	case "wireType":
		if label, ok := wireTypeLabels[value]; ok {
			return label
		}
		return "未知报文类型（" + value + "）"
	case "quality":
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 || n > 255 {
			return value
		}
		if n == 0 {
			return "正常（0）"
		}
		names := []string{}
		for _, flag := range []struct {
			bit   int
			label string
		}{{128, "无效"}, {64, "未刷新"}, {32, "替代"}, {16, "封锁"}, {1, "溢出"}} {
			if n&flag.bit != 0 {
				names = append(names, flag.label)
			}
		}
		if n&14 != 0 {
			names = append(names, fmt.Sprintf("保留位0x%X", n&14))
		}
		return strings.Join(names, "/") + fmt.Sprintf("（0x%02X）", n)
	}
	return value
}

var wireTypeLabels = map[string]string{"0": "自动（沿用装置配置）", "1": "单点遥信（1）", "2": "带短时标单点遥信（2）", "3": "双点遥信（3）", "5": "步位置信息（5）", "9": "归一化遥测（9）", "11": "标度化遥测（11）", "13": "短浮点遥测（13）", "15": "累计量（15）", "21": "不带品质归一化遥测（21）", "30": "带时标单点遥信（30）", "31": "带时标双点遥信（31）", "32": "带时标步位置（32）", "34": "带时标归一化遥测（34）", "35": "带时标标度化遥测（35）", "36": "带时标短浮点遥测（36）", "37": "带时标累计量（37）", "45": "单点遥控（45）", "46": "双点遥控（46）", "47": "步调节命令（47）", "48": "归一化遥调（48）", "49": "标度化遥调（49）", "50": "短浮点遥调（50）", "58": "带时标单点遥控（58）", "59": "带时标双点遥控（59）", "60": "带时标步调节（60）", "61": "带时标归一化遥调（61）", "62": "带时标标度化遥调（62）", "63": "带时标短浮点遥调（63）"}

func wireTypes(kind string) []string {
	switch kind {
	case "yx":
		return []string{"0", "1", "3", "30", "31"}
	case "yc":
		return []string{"0", "9", "11", "13", "21", "34", "35", "36"}
	case "mc":
		return []string{"0", "15", "37"}
	case "position":
		return []string{"0", "5", "32"}
	case "yk":
		return []string{"0", "45", "46", "58", "59"}
	case "yt":
		return []string{"0", "48", "49", "50", "61", "62", "63"}
	case "step":
		return []string{"0", "47", "60"}
	}
	return []string{"0"}
}
func containsChoice(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func qualityChoices() []string {
	values := []string{"0", "128", "64", "32", "16", "1"}
	for n := 1; n < 256; n++ {
		v := strconv.Itoa(n)
		if !containsChoice(values, v) {
			values = append(values, v)
		}
	}
	return values
}
func (e *configurationEditor) value(key string) string {
	for i, f := range e.fields {
		if f.key == key {
			return e.values[i]
		}
	}
	return ""
}
func (e *configurationEditor) fieldChoices(i int) []string {
	field := e.fields[i]
	switch field.key {
	case "address":
		if e.value("transport") == "serial" {
			return e.serialPorts
		}
		if e.value("transport") == "tcp-server" {
			_, port, err := net.SplitHostPort(e.value("address"))
			if err == nil {
				values := make([]string, 0, len(e.localAddresses))
				for _, host := range e.localAddresses {
					values = append(values, net.JoinHostPort(host, port))
				}
				return values
			}
		}
	case "value":
		switch e.value("kind") {
		case "yx":
			if e.value("wireType") == "3" || e.value("wireType") == "31" {
				return []string{"0", "1", "2", "3"}
			}
			return []string{"0", "1"}
		case "yk", "coil", "discrete":
			return []string{"0", "1"}
		}
	case "mode":
		if e.value("kind") != "yx" && e.value("kind") != "coil" && e.value("kind") != "discrete" {
			values := []string{}
			for _, v := range field.choices {
				if v != "toggle" && v != "sequence" {
					values = append(values, v)
				}
			}
			return values
		}
	case "wireType":
		return wireTypes(e.value("kind"))
	case "transport":
		switch e.value("protocol") {
		case "iec104", "modbus-tcp":
			return []string{"tcp-server", "tcp-client"}
		case "modbus-rtu":
			return []string{"serial", "tcp-server", "tcp-client"}
		}
	case "byteOrder":
		if e.value("dataType") == "" || e.value("dataType") == "uint16" || e.value("dataType") == "int16" {
			return []string{"", "AB", "BA"}
		}
		return []string{"", "ABCD", "CDAB", "BADC", "DCBA"}
	case "cause":
		if e.value("kind") == "yx" {
			return []string{"0", "3", "2"}
		}
	}
	return field.choices
}
func (e *configurationEditor) setChoice(i int, value string) {
	if e.values[i] == value {
		e.replace[i] = true
		e.error = ""
		return
	}
	e.values[i] = value
	e.replace[i] = true
	e.error = ""
	for j, f := range e.fields {
		if (e.fields[i].key == "protocol" && f.key == "transport") || (e.fields[i].key == "kind" && f.key == "wireType") || (e.fields[i].key == "kind" && (f.key == "cause" || f.key == "mode")) || (e.fields[i].key == "dataType" && f.key == "byteOrder") {
			choices := e.fieldChoices(j)
			if !containsChoice(choices, e.values[j]) {
				e.values[j] = choices[0]
			}
		}
	}
}
func (m *model) openChoices(title, key, current string, values []string, apply func(string)) {
	if len(values) == 0 {
		return
	}
	values = append([]string{}, values...)
	if key != "wireType" && !containsChoice(values, current) && current != "" {
		values = append(values, current)
	}
	p := &choicePicker{title: "选择" + title, values: values, apply: apply}
	for i, v := range values {
		if v == current {
			p.cursor = i
		}
	}
	// Labels are captured here so later polling never changes the open list.
	labels := make([]string, len(values))
	for i, v := range values {
		labels[i] = choiceLabel(key, v)
	}
	m.picker = p
	m.pickerLabels = labels
}
func (m *model) pickerKey(key string) {
	p := m.picker
	switch key {
	case "esc":
		m.picker = nil
		m.pickerLabels = nil
	case "up", "left":
		p.cursor = max(0, p.cursor-1)
	case "down", "right":
		p.cursor = min(len(p.values)-1, p.cursor+1)
	case "pageup":
		p.cursor = max(0, p.cursor-8)
	case "pagedown":
		p.cursor = min(len(p.values)-1, p.cursor+8)
	case "home":
		p.cursor = 0
	case "end":
		p.cursor = len(p.values) - 1
	case "enter", " ":
		p.apply(p.values[p.cursor])
		m.picker = nil
		m.pickerLabels = nil
	}
}
func (m *model) pickerBody(width, height int) []string {
	rows := max(1, height-m.overlayTop(height)-7)
	start := max(0, m.picker.cursor-rows+1)
	end := min(len(m.picker.values), start+rows)
	maxWidth := min(width-7, 108) - 4
	out := []string{}
	for i := start; i < end; i++ {
		prefix := "  "
		if i == m.picker.cursor {
			prefix = "› "
		}
		out = append(out, fit(prefix+m.pickerLabels[i], maxWidth))
	}
	return out
}
