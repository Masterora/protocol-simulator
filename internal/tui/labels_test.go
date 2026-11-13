package tui

import (
	"strings"
	"testing"

	"gytest-go/internal/observe"
)

func TestLocalizedDetailsPreserveIdentifiersUserNamesAndExactNumbers(t *testing.T) {
	original := map[string]any{"id": "device-running", "name": "Custom running name", "status": "connected", "kind": "holding", "enabled": true, "sequence": uint64(9007199254740993)}
	text := strings.Join(formatDetailLines(original), "\n")
	for _, line := range []string{"状态：已连接", "类型：保持寄存器", "启用：是", "标识：device-running", "名称：Custom running name", "序号：9007199254740993"} {
		if !strings.Contains(text, line) {
			t.Fatal(text)
		}
	}
	if !strings.HasPrefix(text, "名称：") || strings.ContainsAny(text, "{}") {
		t.Fatal("expected a readable list of fields", text)
	}
	if original["status"] != "connected" || original["kind"] != "holding" {
		t.Fatal("Display modified source data")
	}
}
func TestLocalizedLogDisplayKeepsRawRecords(t *testing.T) {
	m := model{view: 6}
	entry := observe.Log{Level: "INFO", Message: "channel_state", Fields: "[channel ch-1 status connected peers 1 ready_peers 1 error ]"}
	m.logs = []observe.Log{entry}
	rows := m.rows()
	if len(rows) != 1 || !strings.Contains(rows[0].text, "已连接") || !strings.Contains(rows[0].text, "通道状态变化") {
		t.Fatal(rows)
	}
	m.openDetail()
	text := strings.Join(m.detailLines, "\n")
	if !strings.Contains(text, entry.Fields) || !strings.Contains(text, "原始字段") || m.logs[0] != entry {
		t.Fatal(text)
	}
}
