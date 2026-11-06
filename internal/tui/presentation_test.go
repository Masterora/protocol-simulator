package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/rivo/uniseg"
	"gytest-go/internal/core"
	"gytest-go/internal/observe"
)

func TestChannelRolesStayVisibleAndAlignedInNarrowTerminals(t *testing.T) {
	m := model{view: 1, connected: true, discovered: true}
	m.detail.View.Channels = []core.InspectChannel{
		{ID: "master", Name: "通道甲", Protocol: "iec104", Role: "client", Address: "0.0.0.0:2406"},
		{ID: "slave", Name: "通道乙", Protocol: "iec104", Role: "server", Address: "127.0.0.1:2407"},
	}
	for _, width := range []int{60, 80, 110, 150} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			lines := strings.Split(stripStyles(m.render(width, 24)), "\r\n")
			var header string
			for _, line := range lines {
				if strings.Contains(line, "角色") && strings.Contains(line, "协议") {
					header = line
					break
				}
			}
			if header == "" {
				t.Fatal("role header missing", lines)
			}
			position := uniseg.StringWidth(header[:strings.Index(header, "角色")])
			for _, role := range []string{"主站", "子站"} {
				found := false
				for _, line := range lines {
					if i := strings.Index(line, role); i >= 0 {
						found = true
						if uniseg.StringWidth(line[:i]) != position {
							t.Fatal("role is not aligned with its header", header, line)
						}
					}
				}
				if !found {
					t.Fatal("role missing", role, lines)
				}
			}
		})
	}
	m.filter = "主站"
	if rows := m.rows(); len(rows) != 1 || rows[0].id != "master" {
		t.Fatal("Chinese role search did not find the master", rows)
	}
}

func TestSelectedChannelRoleIsVisibleAcrossResourceViewsAndRefreshes(t *testing.T) {
	m := model{channel: "c", connected: true, discovered: true}
	m.detail.View.Channels = []core.InspectChannel{{ID: "c", Name: "测试通道", Role: "client"}}
	for view := 1; view < len(viewLabels); view++ {
		m.view = view
		for _, role := range []string{"client", "server"} {
			m.detail.View.Channels[0].Role = role
			if text := stripStyles(m.render(80, 24)); !strings.Contains(text, "测试通道 · "+roleLabels[role]) {
				t.Fatal("selected channel role missing after refresh", view, role, text)
			}
		}
	}
}

func TestRemovedKeysDoNotNavigateButRemainSearchableText(t *testing.T) {
	m := model{view: 3, cursor: 1, detail: observe.Info{View: core.Inspection{Points: []core.Point{{Kind: "yc", Address: 1}, {Kind: "yc", Address: 2}}}}}
	for _, key := range []string{"d", "j", "k"} {
		m.key(key)
	}
	if m.cursor != 1 || m.detailOpen {
		t.Fatal("removed shortcuts still active", m.cursor, m.detailOpen)
	}
	m.key("up")
	if m.cursor != 0 {
		t.Fatal("up unavailable")
	}
	m.key("down")
	m.key("enter")
	if !m.detailOpen {
		t.Fatal("Enter did not open detail")
	}
	for _, key := range []string{"d", "j", "k"} {
		m.key(key)
	}
	if m.detailScroll != 0 {
		t.Fatal("removed detail scroll shortcuts still active")
	}
	m.key("down")
	if m.detailScroll != 1 {
		t.Fatal("down did not scroll detail")
	}
	m.key("enter")
	m.key("?")
	for _, key := range []string{"j", "k"} {
		m.key(key)
	}
	if m.detailScroll != 0 {
		t.Fatal("removed help scroll shortcuts still active")
	}
	m.key("esc")
	m.key("/")
	for _, key := range []string{"d", "j", "k"} {
		m.key(key)
	}
	if m.input != "djk" {
		t.Fatal("ordinary search characters were removed", m.input)
	}
}

func TestCurrentPageAndCategoryUseColorWithoutBrackets(t *testing.T) {
	m := model{view: 3, pointKind: "yc"}
	m.detail.View.PointKinds = map[string]int{"yx": 21, "yc": 61, "yk": 10, "yt": 2}
	for _, width := range []int{60, 90, 150} {
		raw := m.render(width, 24)
		plain := strings.Split(stripStyles(raw), "\r\n")
		if strings.ContainsAny(plain[3], "[]") || !strings.HasPrefix(plain[4], "┌ 点位 ") {
			t.Fatal(plain[3], plain[4])
		}
		for _, category := range []string{"遥信", "遥测", "遥控", "遥调"} {
			if strings.Count(plain[4], category) != 1 || strings.Contains(plain[5], category) {
				t.Fatal("category must appear once in the table title", plain[4], plain[5])
			}
		}
		if !strings.Contains(raw, active+" 4:点位 ") || !strings.Contains(raw, active+" 遥测 ") {
			t.Fatal("selected navigation lacks color", raw)
		}
		if strings.Contains(raw, "回车") || strings.Contains(raw, "[d]") || strings.Contains(raw, "j/k") {
			t.Fatal(raw)
		}
	}
}

func TestAllViewHeadersAlignWithFirstDataColumn(t *testing.T) {
	m := model{socket: "s", channel: "c", device: "dev", discovered: true, connected: true}
	m.instances = []instance{{socket: "s", info: observe.Info{PID: 123456, View: core.Inspection{Name: "工程"}}}}
	m.detail = observe.Info{View: core.Inspection{
		Channels: []core.InspectChannel{{ID: "c", Name: "通道甲", Protocol: "iec104", Address: "127.0.0.1:2406"}},
		Devices:  []core.InspectDevice{{ID: "dev", Name: "装置甲", Points: 1}},
		Points:   []core.Point{{Kind: "yc", Address: 17026, Name: "电压é", Value: 42}},
		Rules:    &core.AutoDataSnapshot{Config: core.AutoDataConfig{Rules: []core.AutoDataRule{{Kind: "yc", Points: "17026"}}}},
	}}
	m.messages = []core.Message{{Sequence: 1, ChannelID: "c", Direction: "RX", Time: "05:49:36.001", Hex: "680407000000"}}
	m.logs = []observe.Log{{Sequence: 1, Time: time.Date(2026, 10, 28, 5, 49, 36, 0, time.UTC), Level: "INFO", Message: "running"}}
	for view := 0; view < 7; view++ {
		for _, width := range []int{60, 80, 110, 150} {
			m.view = view
			lines := strings.Split(stripStyles(m.render(width, 24)), "\r\n")
			top := 0
			for i, line := range lines {
				if strings.Contains(line, "┌") {
					top = i
					break
				}
			}
			cols, indices := m.columns(width - 5 - m.numberWidth(len(m.rows())))
			header := cols[indices[0]].title
			value := m.rows()[0].cells[indices[0]]
			r, _ := utf8.DecodeRuneInString(value)
			hi := strings.Index(lines[top+1], header)
			prefix := len("│› ") + m.numberWidth(len(m.rows()))
			di := strings.IndexRune(lines[top+2][prefix:], r)
			if di >= 0 {
				di += prefix
			}
			if hi < 0 || di < 0 {
				t.Fatal(view, width, lines)
			}
			hc, dc := uniseg.StringWidth(lines[top+1][:hi]), uniseg.StringWidth(lines[top+2][:di])
			if hc != dc || hc != 3+m.numberWidth(len(m.rows())) {
				t.Fatalf("view %d width %d: header at %d, data at %d", view, width, hc, dc)
			}
		}
	}
}

func TestDevicePageKeepsIdentifiersVisibleAndAddressesNearNames(t *testing.T) {
	m := model{view: 2, channel: "c", connected: true, discovered: true, socket: "s"}
	m.detail.View.Devices = []core.InspectDevice{{ID: "prs-four-remote-1030", Name: "四遥测试装置", Unit: 247, CommonAddress: 65535, Points: 94}}
	for _, protocol := range []string{"iec104", "modbus-tcp", "modbus-rtu"} {
		m.detail.View.Channels = []core.InspectChannel{{ID: "c", Protocol: protocol}}
		for _, width := range []int{60, 80, 110, 150} {
			lines := strings.Split(stripStyles(m.render(width, 24)), "\r\n")
			var header, row string
			for i, line := range lines {
				if strings.Contains(line, "┌ 装置 ") {
					header, row = lines[i+1], lines[i+2]
					break
				}
			}
			if !strings.Contains(row, "prs-four-remote-1030") || !strings.Contains(row, "四遥测试装置") {
				t.Fatal(protocol, width, header, row)
			}
			address := "站号"
			if protocol == "iec104" {
				address = "公共地址"
				if strings.Contains(header, "站号") {
					t.Fatal(header)
				}
			} else if strings.Contains(header, "公共地址") {
				t.Fatal(header)
			}
			index := strings.Index(header, address)
			if index < 0 || uniseg.StringWidth(header[:index]) > 30 {
				t.Fatal("address separated from name by excessive whitespace", width, header)
			}
		}
	}
	m.detail.View.Devices[0].Name = strings.Repeat("中文名称", 8)
	if frame := stripStyles(m.render(150, 24)); !strings.Contains(frame, m.detail.View.Devices[0].Name) {
		t.Fatal("wide terminal truncated a name that fits", frame)
	}
}

func compactTableModel() model {
	m := model{socket: "s", channel: "c", device: "dev", discovered: true, connected: true, pointKind: "yc"}
	m.detail = observe.Info{PID: 2151230, View: core.Inspection{Name: "工程2", Path: "/home/ut/.config/protocol-simulator/工程2.json",
		Channels: []core.InspectChannel{{ID: "c", Name: "四遥测试通道", Protocol: "iec104", Role: "server", Address: "10.20.12.198:2411", State: core.ChannelState{Status: "connected", Peers: 1, RX: 1234, TX: 5678}}},
		Devices:  []core.InspectDevice{{ID: "prs-four-remote-1030", Name: "四遥测试装置", CommonAddress: 1, Points: 12}},
		Points:   []core.Point{{Kind: "yc", Address: 16385, Name: "电压 é", Value: 221.5, Quality: 128}},
		Rules:    &core.AutoDataSnapshot{Config: core.AutoDataConfig{Rules: []core.AutoDataRule{{Enabled: true, Kind: "yc", Mode: "increment", IntervalMS: 2000, Points: "16385-16390"}}}},
	}}
	m.instances = []instance{{socket: "s", info: m.detail}}
	m.messages = []core.Message{{Sequence: 1, ChannelID: "c", Direction: "RX", Time: "05:49:36.001", Peer: "10.20.12.59:38100", Summary: "总召唤", Hex: "680407000000"}}
	m.logs = []observe.Log{{Sequence: 1, Time: time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC), Level: "INFO", Message: "running", Fields: "[project 工程2]"}}
	return m
}

func TestAllTableColumnsAlignAndDoNotWasteSpaceWhenContentFits(t *testing.T) {
	m := compactTableModel()
	for view := 0; view < 7; view++ {
		m.view = view
		for _, width := range []int{60, 80, 110, 150} {
			lines := strings.Split(stripStyles(m.render(width, 24)), "\r\n")
			top := m.tableTop()
			header, data := lines[top+1], lines[top+2]
			cols, indices := m.columns(width - 5 - m.numberWidth(len(m.rows())))
			cells := m.rows()[0].cells
			natural := 0
			gap := tableGap(width - 5 - m.numberWidth(len(m.rows())))
			for _, i := range indices {
				natural += max(uniseg.StringWidth(cols[i].title), uniseg.StringWidth(cells[i])) + gap
			}
			if natural > width-5-m.numberWidth(len(m.rows())) {
				continue // Insufficient space: truncation is expected, covered below.
			}
			byteOffset, position := len("│› ")+m.numberWidth(len(m.rows())), 3+m.numberWidth(len(m.rows()))
			for _, i := range indices {
				h := strings.Index(header, cols[i].title)
				d := strings.Index(data[byteOffset:], cells[i])
				if h < 0 || d < 0 {
					t.Fatalf("view %d width %d: content that fits was clipped: %s / %s", view, width, header, data)
				}
				d += byteOffset
				if uniseg.StringWidth(header[:h]) != position || uniseg.StringWidth(data[:d]) != position {
					t.Fatalf("view %d width %d: column %s misaligned or padded excessively: %s / %s", view, width, cols[i].title, header, data)
				}
				byteOffset = d + len(cells[i])
				position += max(uniseg.StringWidth(cols[i].title), uniseg.StringWidth(cells[i])) + gap
			}
		}
	}
}

func TestEightyColumnTablesKeepUsefulColumnsAndFullShortValues(t *testing.T) {
	m := compactTableModel()
	for view, expected := range map[int][]string{
		0: {"状态", "工程2.json"},
		1: {"10.20.12.198:2411"},
		3: {"十六进制", "0x4001", "电压 é"},
		4: {"状态", "已停止", "递增", "16385-16390"},
		5: {"对端", "原始报文", "680407000000"},
	} {
		m.view = view
		frame := stripStyles(m.render(80, 24))
		for _, value := range expected {
			if !strings.Contains(frame, value) {
				t.Fatal("useful content hidden at 80 columns", view, value, frame)
			}
		}
	}
}

func TestAllTablesFitWithEmptyOrLongMixedWidthRows(t *testing.T) {
	for _, populated := range []bool{false, true} {
		m := compactTableModel()
		if populated {
			m.detail.View.Name = strings.Repeat("工程 é", 40)
			m.detail.View.Channels[0].Name = strings.Repeat("通道 é", 40)
			m.detail.View.Devices[0].Name = strings.Repeat("装置 é", 40)
			m.detail.View.Points[0].Name = strings.Repeat("测量 é", 40)
			m.messages[0].Summary = strings.Repeat("报文 é", 40)
			m.messages[0].Hex = strings.Repeat("68", 254)
			m.logs[0].Message = strings.Repeat("日志 é", 40)
		} else {
			m.instances, m.messages, m.logs = nil, nil, nil
			m.detail.View = core.Inspection{}
		}
		for view := 0; view < 7; view++ {
			m.view = view
			for _, width := range []int{60, 80, 110, 150} {
				for _, height := range []int{12, 24} {
					frame := stripStyles(m.render(width, height))
					lines := strings.Split(frame, "\r\n")
					if !utf8.ValidString(frame) || len(lines) != height {
						t.Fatal("invalid terminal frame", view, width, height)
					}
					for _, line := range lines {
						if uniseg.StringWidth(strings.TrimPrefix(line, "\x1b[H")) != width-1 {
							t.Fatal("wrapped terminal row", view, width, height, line)
						}
					}
				}
			}
		}
	}
}

func TestCompactTablesPreserveFilenameAndPeerPort(t *testing.T) {
	m := compactTableModel()
	other := m.detail
	other.PID = 2151231
	other.View.Name = "网关通道"
	other.View.Path = "/home/ut/.config/protocol-simulator/gateway.json"
	m.instances = append(m.instances, instance{socket: "other", info: other})
	for _, width := range []int{60, 80} {
		m.view = 0
		frame := stripStyles(m.render(width, 24))
		for _, name := range []string{"工程2.json", "gateway.json"} {
			if !strings.Contains(frame, name) {
				t.Fatal("shared directory prefix hid filename", width, name, frame)
			}
		}
		m.view = 5
		m.messages[0].Peer = "10.20.12.59:58718"
		m.messages[0].Summary = "接收到包含多个点位的数据报文"
		m.messages[0].Hex = strings.Repeat("680E9E0964000101030001", 10)
		frame = stripStyles(m.render(width, 24))
		if !strings.Contains(frame, m.messages[0].Peer) {
			t.Fatal("long report hid peer port", width, frame)
		}
	}
}

func TestUnlabelledNumbersReplaceCountsWithoutChangingResourceIdentity(t *testing.T) {
	m := compactTableModel()
	for view := 0; view < 7; view++ {
		m.view = view
		for _, width := range []int{60, 80, 110, 150} {
			lines := strings.Split(stripStyles(m.render(width, 24)), "\r\n")
			for _, retired := range []string{"通道数", "连接数", "接收量", "发送量", "点位数", "执行次数", "序号"} {
				if strings.Contains(lines[5], retired) {
					t.Fatal("statistic or numbered-column heading remains", view, retired, lines[5])
				}
			}
			if strings.ContainsAny(lines[4]+lines[len(lines)-3], "0123456789") || !strings.HasPrefix(lines[6], "│› 1  ") || !strings.HasPrefix(lines[5], "│     ") {
				t.Fatal("number must only be in the blank-headed first column", view, width, lines)
			}
		}
	}
	m.view, m.pointOffset, m.cursor = 3, 100, 1
	m.detail.View.Points = []core.Point{{Kind: "yc", Address: 16385, Name: "甲"}, {Kind: "yc", Address: 16386, Name: "乙"}}
	lines := strings.Split(stripStyles(m.render(80, 12)), "\r\n")
	if !strings.HasPrefix(lines[6], "│  101 ") || !strings.HasPrefix(lines[7], "│› 102 ") {
		t.Fatal("point numbering must continue across pages", lines)
	}
	point, ok := m.selectedPoint()
	if !ok || point.Address != 16386 {
		t.Fatal("display ordinal changed point identity", point)
	}
	m.view, m.cursor, m.filter = 4, 0, "停用"
	m.detail.View.Rules.Config.Rules = append(m.detail.View.Rules.Config.Rules, core.AutoDataRule{Kind: "yc", Enabled: false, Points: "16386"})
	lines = strings.Split(stripStyles(m.render(80, 12)), "\r\n")
	if m.rows()[0].id != "1" || !strings.HasPrefix(lines[6], "│› 1  ") {
		t.Fatal("filtered rule must renumber visually and retain its actual identity", lines)
	}
}
