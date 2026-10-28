package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"gytest-go/internal/observe"
)

func TestLocalizedDetailsPreserveIdentifiersUserNamesAndExactNumbers(t *testing.T) {
	original := map[string]any{"id": "device-running", "name": "Custom running name", "status": "connected", "kind": "holding", "enabled": true, "sequence": uint64(9007199254740993)}
	text := strings.Join(detailJSONLines(original), "\n")
	var shown map[string]any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if err := decoder.Decode(&shown); err != nil {
		t.Fatal(err)
	}
	if shown["状态"] != "已连接" || shown["类型"] != "保持寄存器" || shown["启用"] != "是" {
		t.Fatal(shown)
	}
	if shown["标识"] != original["id"] || shown["名称"] != original["name"] || shown["序号"].(json.Number).String() != "9007199254740993" {
		t.Fatal(shown)
	}
	if original["status"] != "connected" || original["kind"] != "holding" {
		t.Fatal("Display modified source data")
	}
}
func TestChineseResourceCommandsAndLogDisplayKeepRawRecords(t *testing.T) {
	m := model{}
	for _, key := range []string{":", "点", "位", "enter"} {
		m.key(key)
	}
	if m.view != 3 {
		t.Fatal(m.view)
	}
	entry := observe.Log{Level: "INFO", Message: "channel_state", Fields: "[channel ch-1 status connected peers 1 ready_peers 1 error ]"}
	m.view = 6
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
