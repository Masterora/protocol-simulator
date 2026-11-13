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
	for _, key := range []string{"d", "j", "k", "f", "l", ":", "g", "delete", "f1"} {
		m.key(key)
	}
	if m.cursor != 1 || m.detailOpen || m.searching || m.help || m.view != 3 {
		t.Fatal("unlisted shortcuts still active", m)
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

func TestDeviceShortcutsRespectTheSelectedChannelsCapacity(t *testing.T) {
	m := deletableModel()
	m.view = 2
	m.detail.View.Channels[0].Devices = 1
	m.detail.View.Devices = []core.InspectDevice{{ID: "d", Name: "装置"}}
	hints := m.actionHints(130)
	if strings.Contains(hints, "[n]") || !strings.Contains(hints, "[e]") || !strings.Contains(hints, "[x]") {
		t.Fatal(hints)
	}
	m.key("n")
	if m.pendingConfiguration != nil {
		t.Fatal("IEC104 with an existing device offered another")
	}
	m.detail.View.Channels[0].Protocol = "modbus-tcp"
	if !strings.Contains(m.actionHints(130), "[n]") {
		t.Fatal("Modbus channel cannot add devices")
	}
}

func TestCurrentPageAndCategoryUseColorWithoutBrackets(t *testing.T) {
	m := model{view: 3, pointKind: "yc"}
	m.detail.View.PointKinds = map[string]int{"yx": 21, "yc": 61, "yk": 10, "yt": 2}
	for _, width := range []int{60, 90, 150} {
		raw := m.render(width, 24)
		plain := strings.Split(stripStyles(raw), "\r\n")
		if strings.ContainsAny(plain[3], "[]") || !strings.HasPrefix(plain[5], "┌ 点位 ") {
			t.Fatal(plain[3], plain[5])
		}
		for _, category := range []string{"遥信", "遥测", "遥控", "遥调"} {
			if strings.Count(plain[5], category) != 1 || strings.Contains(plain[6], category) {
				t.Fatal("category must appear once in the table title", plain[5], plain[6])
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

func TestPagingUsesVisibleRowsAndKeepsSelectionInView(t *testing.T) {
	m := compactTableModel()
	m.view = 6
	for i := 2; i <= 80; i++ {
		m.logs = append(m.logs, observe.Log{Sequence: uint64(i), Level: "INFO", Message: "running"})
	}
	m.render(80, 12)
	m.key("pagedown")
	if m.cursor != 3 {
		t.Fatal("small table skipped visible rows", m.cursor)
	}
	m.render(80, 12)
	first := m.listStart
	if first != 3 {
		t.Fatal("PageDown did not advance the viewport", first)
	}
	m.key("down")
	m.key("up")
	m.render(80, 12)
	if m.listStart != first {
		t.Fatal("viewport shifted while the selection was visible", m.listStart)
	}
	m.render(110, 24)
	m.key("pagedown")
	if m.cursor != 18 {
		t.Fatal("page size did not follow the terminal resize", m.cursor)
	}
	m.key("?")
	m.render(80, 12)
	m.key("pagedown")
	if m.detailScroll != 4 {
		t.Fatal("help skipped content in a short terminal", m.detailScroll)
	}
}

func TestPointPageAndFilterClearPreviousActionTargets(t *testing.T) {
	m := compactTableModel()
	m.view = 3
	m.detail.View.PointMatches = 101
	m.cursor = 99
	m.key("pagedown")
	if m.pointOffset != 100 || !m.pointLoading || len(m.rows()) != 0 {
		t.Fatal("old points were still actionable after paging")
	}
	m.key("/")
	m.input = "不存在"
	m.key("enter")
	if m.pointOffset != 0 || !m.pointLoading || m.emptyMessage() != "正在读取点位…" {
		t.Fatal("point search did not request a fresh page")
	}
	m.pointLoading = false
	if !strings.Contains(m.emptyMessage(), "没有匹配结果") {
		t.Fatal("zero search results show the wrong empty state")
	}
}

func TestPointTransferShortcutsMatchCapabilitiesAtMinimumWidth(t *testing.T) {
	m := editableModel()
	m.detail.ToolOperations = true
	hints := m.actionHints(149)
	if !strings.Contains(hints, "[m]导入 [o]导出") || strings.Index(hints, "[o]导出") > strings.Index(hints, "[↑↓]选择") {
		t.Fatal("point transfer should be grouped before navigation", hints)
	}
	for _, capable := range []bool{true, false} {
		m.detail.ToolOperations = capable
		lines := strings.Split(stripStyles(m.render(60, 12)), "\r\n")
		if !strings.Contains(lines[4], "[o]导出") || strings.Contains(lines[4], "[m]导入") != capable {
			t.Fatal(lines[4])
		}
		m.key("i")
		if !m.detailOpen || m.configurationEditor != nil {
			t.Fatal("detail shortcut opened point transfer")
		}
		m.key("esc")

		m.key("M")
		if (m.configurationEditor != nil) != capable {
			t.Fatal("import key and hint disagree")
		}
		if capable {
			m.key("esc")
		}
		m.key("O")
		if m.configurationEditor == nil || m.configurationEditor.title != "导出点表" {
			t.Fatal("export key did not open form")
		}
		m.key("esc")
	}
}

func TestPagingHintAppearsOnceOnlyWhenTheListOverflows(t *testing.T) {
	m := editableModel()
	for i := 11; i <= 13; i++ {
		m.detail.View.Points = append(m.detail.View.Points, core.Point{Kind: "yc", Address: uint32(i)})
	}
	for _, size := range [][3]int{{60, 12, 1}, {150, 12, 1}, {60, 13, 0}} {
		if screen := stripStyles(m.render(size[0], size[1])); strings.Count(screen, "[PgUp/PgDn]") != size[2] {
			t.Fatal(size, screen)
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
			cols, indices := m.columns(width-5-m.numberWidth(len(m.rows())), m.rows())
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
			cols, indices := m.columns(width-5-m.numberWidth(len(m.rows())), m.rows())
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
				if strings.Contains(lines[6], retired) {
					t.Fatal("statistic or numbered-column heading remains", view, retired, lines[6])
				}
			}
			if strings.ContainsAny(lines[5]+lines[len(lines)-2], "0123456789") || !strings.HasPrefix(lines[7], "│› 1  ") || !strings.HasPrefix(lines[6], "│     ") {
				t.Fatal("number must only be in the blank-headed first column", view, width, lines)
			}
		}
	}
	m.view, m.pointOffset, m.cursor = 3, 100, 1
	m.detail.View.Points = []core.Point{{Kind: "yc", Address: 16385, Name: "甲"}, {Kind: "yc", Address: 16386, Name: "乙"}}
	lines := strings.Split(stripStyles(m.render(80, 12)), "\r\n")
	if !strings.HasPrefix(lines[7], "│  101 ") || !strings.HasPrefix(lines[8], "│› 102 ") {
		t.Fatal("point numbering must continue across pages", lines)
	}
	point, ok := m.selectedPoint()
	if !ok || point.Address != 16386 {
		t.Fatal("display ordinal changed point identity", point)
	}
	m.view, m.cursor, m.filter = 4, 0, "停用"
	m.detail.View.Rules.Config.Rules = append(m.detail.View.Rules.Config.Rules, core.AutoDataRule{Kind: "yc", Enabled: false, Points: "16386"})
	lines = strings.Split(stripStyles(m.render(80, 12)), "\r\n")
	if m.rows()[0].id != "1" || !strings.HasPrefix(lines[7], "│› 1  ") {
		t.Fatal("filtered rule must renumber visually and retain its actual identity", lines)
	}
}

func TestFullFrameAtMinimumTerminalSize(t *testing.T) {
	m := compactTableModel()
	m.view = 5
	payload := strings.Repeat("0123456789ABCDEF", 32) + "ABCD"
	m.messages[0].Hex = payload
	screen := stripStyles(m.render(60, 12))
	if !strings.Contains(screen, "[Enter]详情") {
		t.Fatal(screen)
	}
	if !strings.Contains(screen, "[PgUp/PgDn]翻页") {
		t.Fatal("a single long frame must be pageable", screen)
	}
	var visibleHex strings.Builder
	for next := 1; next < len(m.frameRows); {
		lines := strings.Split(screen, "\r\n")
		for i := m.frameTop; i < min(len(m.frameRows), m.frameTop+m.tableRows(12)); i++ {
			if i != next {
				continue
			}
			line := lines[m.tableTop()+2+i-m.frameTop]
			visibleHex.WriteString(strings.TrimSpace(strings.Trim(line, "│")))
			next++
		}
		if next == len(m.frameRows) {
			break
		}
		top := m.frameTop
		m.key("pagedown")
		screen = stripStyles(m.render(60, 12))
		if m.frameTop <= top {
			t.Fatal("paging stalled within a long frame")
		}
	}
	if visibleHex.String() != payload {
		t.Fatalf("wrapped list lost or changed bytes: %s", visibleHex.String())
	}
	if !strings.Contains(screen, "[c]复制报文") {
		t.Fatal(screen)
	}
	m.key("c")
	if m.pendingClipboard != payload {
		t.Fatal("copy must use the full selected frame, including bytes off screen")
	}
	m.key("enter")
	var hexText strings.Builder
	for _, line := range m.detailLines[1:] {
		if line == "" {
			break
		}
		_, bytes, _ := strings.Cut(line, "  ")
		hexText.WriteString(strings.ReplaceAll(bytes, " ", ""))
		if len(line) > 49 {
			t.Fatalf("hex line too wide: %q", line)
		}
	}
	if hexText.String() != payload {
		t.Fatal("frame payload lost or changed")
	}
	screen = stripStyles(m.render(60, 12))
	if !strings.Contains(screen, "0000  01 23") {
		t.Fatal(screen)
	}
}
