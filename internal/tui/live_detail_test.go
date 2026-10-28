package tui

import (
	"fmt"
	"strings"
	"testing"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
)

func applyDetail(m *model, v core.Inspection) {
	m.apply(update{request: request{socket: m.socket, channel: m.channel, device: m.device, kind: m.pointKind}, socket: m.socket, channel: m.channel, device: m.device, kind: m.pointKind, detail: observe.Info{View: v}})
}
func detailText(m *model) string { return strings.Join(m.detailLines, "\n") }
func TestLivePointDetailTracksTypedAddressUpdatesAndRecoversFromDisconnect(t *testing.T) {
	m := model{view: 3, socket: "socket", channel: "ch", device: "dev", connected: true, discovered: true, cursor: 1}
	m.detail.View.Points = []core.Point{{Kind: "coil", Address: 0, Value: 1}, {Kind: "holding", Address: 0, Value: 42}}
	m.openDetail()
	m.detailScroll = 2
	applyDetail(&m, core.Inspection{Points: []core.Point{{Kind: "holding", Address: 0, Value: 99, Quality: 128}, {Kind: "coil", Address: 0, Value: 7}}})
	text := detailText(&m)
	if !strings.Contains(text, `"数值": 99`) || !strings.Contains(text, `"品质": 128`) || m.detailScroll != 2 {
		t.Fatal(text, m.detailScroll)
	}
	m.apply(update{request: request{socket: m.socket, channel: m.channel, device: m.device, kind: m.pointKind}, err: fmt.Errorf("disconnected")})
	if !strings.Contains(detailText(&m), "观测连接中断") || strings.Contains(detailText(&m), `"数值"`) {
		t.Fatal(detailText(&m))
	}
	applyDetail(&m, core.Inspection{Points: []core.Point{{Kind: "holding", Address: 0, Value: 103}}})
	if !strings.Contains(detailText(&m), `"数值": 103`) {
		t.Fatal(detailText(&m))
	}
	applyDetail(&m, core.Inspection{Points: []core.Point{{Kind: "coil", Address: 0, Value: 17}}})
	if !strings.Contains(detailText(&m), "所选点位") || strings.Contains(detailText(&m), `"数值": 17`) {
		t.Fatal(detailText(&m))
	}
}
func TestLiveRuleDetailUpdatesCyclesSettingsAndPreservesIdentityWhenReordered(t *testing.T) {
	target := core.AutoDataRule{Kind: "yc", Points: "1", IntervalMS: 1000, Mode: "fixed", Enabled: true}
	other := core.AutoDataRule{Kind: "yc", Points: "2", IntervalMS: 1000, Mode: "fixed", Enabled: true}
	m := model{view: 4, socket: "socket", channel: "ch", device: "dev", connected: true, discovered: true}
	m.detail.View.Rules = &core.AutoDataSnapshot{Status: "running", Config: core.AutoDataConfig{Rules: []core.AutoDataRule{target, other}}, Cycles: []uint64{1, 2}}
	m.openDetail()
	target.IntervalMS = 2000
	target.Mode = "random"
	applyDetail(&m, core.Inspection{Rules: &core.AutoDataSnapshot{Status: "paused", Config: core.AutoDataConfig{Rules: []core.AutoDataRule{other, target}}, Cycles: []uint64{80, 7}}})
	text := detailText(&m)
	for _, want := range []string{`"执行次数": 7`, `"周期（毫秒）": 2000`, `"状态": "已暂停"`, `"模式": "随机"`} {
		if !strings.Contains(text, want) {
			t.Fatal(text, want)
		}
	}
	applyDetail(&m, core.Inspection{Rules: &core.AutoDataSnapshot{Config: core.AutoDataConfig{Rules: []core.AutoDataRule{other}}, Cycles: []uint64{90}}})
	if !strings.Contains(detailText(&m), "所选规则已移除") || strings.Contains(detailText(&m), `"执行次数": 90`) {
		t.Fatal(detailText(&m))
	}
}
func TestDuplicateRuleDetailsRemainLiveUntilTheirConfigurationBecomesAmbiguous(t *testing.T) {
	r := core.AutoDataRule{Kind: "yc", Points: "1"}
	m := model{view: 4, cursor: 1}
	m.detail.View.Rules = &core.AutoDataSnapshot{Config: core.AutoDataConfig{Rules: []core.AutoDataRule{r, r}}, Cycles: []uint64{1, 2}}
	m.openDetail()
	m.detail.View.Rules.Cycles[1] = 3
	m.refreshDetail()
	if !strings.Contains(detailText(&m), `"执行次数": 3`) {
		t.Fatal(detailText(&m))
	}
	m.detail.View.Rules.Config.Rules[0].Mode = "fixed"
	m.refreshDetail()
	if !strings.Contains(detailText(&m), "请重新选择") {
		t.Fatal(detailText(&m))
	}
	m.detail.View.Rules.Config.Rules = []core.AutoDataRule{r}
	m.refreshDetail()
	if !strings.Contains(detailText(&m), "请重新选择") {
		t.Fatal(detailText(&m))
	}
}

func TestLiveDetailsDoNotKeepOldValuesWhenDiscoveryReturnsNoProjects(t *testing.T) {
	m := model{view: 3, socket: "socket", channel: "ch", device: "dev", connected: true, discovered: true}
	m.detail.View.Points = []core.Point{{Kind: "yc", Address: 1, Value: 99}}
	m.openDetail()
	m.apply(update{request: request{socket: m.socket, channel: m.channel, device: m.device, kind: m.pointKind}})
	if m.connected || !strings.Contains(detailText(&m), "观测连接中断") || strings.Contains(detailText(&m), `"数值"`) {
		t.Fatal(detailText(&m))
	}
}
