package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gytest-go/internal/core"
	processinstance "gytest-go/internal/instance"
	"gytest-go/internal/observe"
	"gytest-go/internal/projectfile"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

type configurationOperation struct {
	socket          string
	request         projectfile.Request
	create          bool
	path, name, dir string
}
type configurationResponse struct {
	operation configurationOperation
	view      projectfile.View
	result    projectfile.Result
	started   *processinstance.Started
	err       error
}
type configurationField struct {
	key, label string
	choices    []string
}
type configurationEditor struct {
	operation         configurationOperation
	fields            []configurationField
	values, original  []string
	replace           []bool
	focus             int
	error             string
	title             string
	pathEdited        bool
	serialPorts       []string
	localAddresses    []string
	bindingTargets    []core.Point
	bindingInputs     []bindingInput
	bindingLimit      uint32
	bindingStart      int
	bindingsAvailable bool
}

func configurationFields(resource, action, protocol string) []configurationField {
	switch resource {
	case "new-project":
		return []configurationField{{key: "name", label: "工程名称"}, {key: "path", label: "配置文件"}}
	case "project":
		return []configurationField{{key: "name", label: "工程名称"}}
	case "channel":
		return []configurationField{
			{key: "name", label: "通道名称"}, {"protocol", "协议", []string{"iec104", "modbus-tcp", "modbus-rtu", "raw"}}, {"role", "角色", []string{"server", "client"}}, {"transport", "连接方式", []string{"tcp-server", "tcp-client", "serial", "udp"}}, {key: "address", label: "通信地址"}, {key: "localAddress", label: "本地绑定"}, {key: "timeoutMs", label: "超时(ms)"}, {"baud", "波特率", []string{"1200", "2400", "4800", "9600", "19200", "38400", "57600", "115200", "230400", "460800", "921600"}}, {"parity", "校验位", []string{"N", "E", "O"}}, {"dataBits", "数据位", []string{"5", "6", "7", "8"}}, {"stopBits", "停止位", []string{"1", "2"}},
		}
	case "device":
		fields := []configurationField{{key: "name", label: "装置名称"}}
		if protocol == "iec104" {
			fields = append(fields, configurationField{key: "commonAddress", label: "公共地址"})
		} else {
			fields = append(fields, configurationField{key: "unit", label: "站号"})
		}
		if action == "add" {
			kinds := []string{"yx", "yc", "yk", "yt", "mc", "position", "step"}
			if strings.HasPrefix(protocol, "modbus-") {
				kinds = []string{"holding", "input", "coil", "discrete"}
			}
			fields = append(fields, configurationField{key: "pointCount", label: "初始点数"}, configurationField{"pointKind", "初始类型", kinds}, configurationField{key: "pointStart", label: "起始点位"})
		}
		return fields
	case "point":
		kinds := []string{"yx", "yc", "yk", "yt", "mc", "position", "step"}
		if strings.HasPrefix(protocol, "modbus-") {
			kinds = []string{"holding", "input", "coil", "discrete"}
		}
		fields := []configurationField{{"kind", "点位类型", kinds}, {key: "address", label: "点位"}, {key: "name", label: "描述"}, {key: "value", label: "数值"}, {"quality", "品质", qualityChoices()}}
		if protocol == "iec104" {
			fields = append(fields, configurationField{"wireType", "报文类型", []string{"0"}})
		} else {
			fields = append(fields, configurationField{"dataType", "寄存器格式", []string{"", "uint16", "int16", "uint32", "int32", "float32"}}, configurationField{"byteOrder", "字节序", []string{"", "AB", "BA", "ABCD", "CDAB", "BADC", "DCBA"}})
		}
		if action == "add" {
			fields = append(fields, configurationField{key: "count", label: "连续点数"})
		}
		return fields
	case "rule":
		kinds := []string{"yx", "yc"}
		modes := []string{"report", "fixed", "increment", "random", "toggle", "sequence"}
		if strings.HasPrefix(protocol, "modbus-") {
			kinds = []string{"holding", "input", "coil", "discrete"}
			modes = modes[1:]
		}
		return []configurationField{{"enabled", "启用", []string{"false", "true"}}, {"kind", "点位类型", kinds}, {key: "points", label: "点位范围"}, {key: "intervalMs", label: "周期(ms)"}, {"mode", "变化/上报方式", modes}, {key: "min", label: "固定值/下限"}, {key: "max", label: "上限"}, {key: "step", label: "步长"}, {"cause", "104 上报原因", []string{"0", "3", "2", "1"}}, {"stopOnDisconnect", "断连停止规则", []string{"true", "false"}}}
	}
	return nil
}
func displayField(resource string, field configurationField, value json.RawMessage) string {
	if len(value) == 0 {
		switch projectfile.FieldType(resource, field.key) {
		case "integer", "number":
			return "0"
		case "boolean":
			return "false"
		}
		return ""
	}
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text
	}
	return string(value)
}
func newConfigurationEditor(operation configurationOperation, view projectfile.View) *configurationEditor {
	resource := operation.request.Resource
	formAction := operation.request.Action
	if formAction == "defaults" {
		formAction = "add"
	}
	editor := &configurationEditor{operation: operation, fields: configurationFields(resource, formAction, view.Protocol), localAddresses: append([]string{}, view.LocalAddresses...)}
	if resource == "channel" {
		editor.serialPorts, _ = core.SerialPorts()
	}
	label := map[string]string{"project": "工程", "channel": "通道", "device": "装置", "point": "点位", "rule": "自动上报规则", "new-project": "工程"}[resource]
	editor.title = "编辑" + label
	if formAction == "add" || operation.create {
		editor.title = "新增" + label
	}
	for _, field := range editor.fields {
		value := displayField(resource, field, view.Value[field.key])
		editor.values = append(editor.values, value)
		editor.original = append(editor.original, value)
		editor.replace = append(editor.replace, true)
	}
	if resource == "point" && formAction != "add" && view.Value["mappings"] != nil {
		editor.bindingsAvailable = true
		appendBindingFields(editor, view)
	}
	return editor
}
func (m *model) configurationTarget(action string) (configurationOperation, bool) {
	operation := configurationOperation{socket: m.socket, dir: m.runtimeDir, request: projectfile.Request{Action: action, Channel: m.channel, Device: m.device, Revision: m.detail.ConfigurationRevision}}
	resources := []string{"project", "channel", "device", "point", "rule"}
	if m.view < 0 || m.view >= len(resources) {
		m.setOperationNotice("请在工程、通道、装置、点位或规则页操作")
		return operation, false
	}
	operation.request.Resource = resources[m.view]
	if m.view == 0 {
		rows := m.rows()
		if len(rows) == 0 {
			return operation, false
		}
		id := rows[min(m.cursor, len(rows)-1)].id
		for _, item := range m.instances {
			if item.socket == id {
				operation.socket = id
				operation.request.Revision = item.info.ConfigurationRevision
				if !item.info.ConfigurationOperations {
					m.setOperationNotice("此工程服务需升级后才能保存配置")
					return operation, false
				}
				return operation, true
			}
		}
	}
	if !m.connected || !m.detail.ConfigurationOperations {
		m.setOperationNotice("请连接支持配置编辑的工程；旧服务需升级")
		return operation, false
	}
	rows := m.rows()
	if action == "edit" || action == "start" || action == "stop" || action == "delete" {
		if len(rows) == 0 {
			m.setOperationNotice("请先选择要编辑的资源")
			return operation, false
		}
		id := rows[min(m.cursor, len(rows)-1)].id
		switch m.view {
		case 1:
			operation.request.Channel = id
		case 2:
			operation.request.Device = id
		case 3:
			p, ok := m.selectedPoint()
			if !ok {
				return operation, false
			}
			operation.request.Point = &core.PointKey{Kind: p.Kind, Address: p.Address}
		case 4:
			i, err := strconv.Atoi(id)
			if err != nil {
				return operation, false
			}
			operation.request.Index = &i
		}
	} else if m.view == 3 {
		operation.request.Point = &core.PointKey{Kind: m.pointKind}
	}
	return operation, true
}
func (m *model) beginConfiguration(action string) {
	if m.view == 0 && action == "add" {
		dir, err := os.UserConfigDir()
		if err != nil {
			m.setOperationNotice(err.Error())
			return
		}
		dir = filepath.Join(dir, "protocol-simulator")
		n := 1
		for {
			if _, err = os.Stat(filepath.Join(dir, fmt.Sprintf("工程%d.json", n))); errors.Is(err, os.ErrNotExist) {
				break
			}
			n++
			if n > 10000 {
				m.setOperationNotice("请使用 CLI 指定新工程路径")
				return
			}
		}
		name := fmt.Sprintf("工程%d", n)
		operation := configurationOperation{create: true, dir: m.runtimeDir, request: projectfile.Request{Resource: "new-project", Action: "add"}}
		m.configurationEditor = newConfigurationEditor(operation, projectfile.View{Value: projectfile.Fields(map[string]any{"name": name, "path": filepath.Join(dir, name+".json")})})
		return
	}
	operation, ok := m.configurationTarget(action)
	if !ok {
		return
	}
	operation.request.Action = "read"
	if action == "add" {
		operation.request.Action = "defaults"
	}
	m.pendingConfiguration = &operation
	m.operationBusy = true
}
func (m *model) toggleChannel() {
	operation, ok := m.configurationTarget("start")
	if !ok || m.view != 1 {
		return
	}
	for _, c := range m.detail.View.Channels {
		if c.ID == operation.request.Channel && c.State.Status != "stopped" && c.State.Status != "error" {
			operation.request.Action = "stop"
		}
	}
	m.pendingConfiguration = &operation
	m.operationBusy = true
}
func (m *model) configurationKey(key string) {
	editor := m.configurationEditor
	if editor == nil {
		return
	}
	i := editor.focus
	if len(editor.fieldChoices(i)) > 0 && editor.fields[i].key != "baud" && editor.fields[i].key != "address" && (key == "ctrl-u" || key == "backspace" || utf8.RuneCountInString(key) == 1 && key != " ") {
		editor.error = "此项使用 ←/→ 切换，空格打开选项"
		return
	}
	if key == " " && len(editor.fieldChoices(i)) > 0 {
		m.openChoices(editor.fieldLabel(editor.fields[i]), editor.fields[i].key, editor.values[i], editor.fieldChoices(i), func(v string) { editor.setChoice(i, v) })
		return
	}
	switch key {
	case "esc":
		m.configurationEditor = nil
		return
	case "tab", "down":
		editor.focus = (i + 1) % len(editor.fields)
		editor.replace[editor.focus] = true
		return
	case "up", "shift-tab":
		editor.focus = (i + len(editor.fields) - 1) % len(editor.fields)
		editor.replace[editor.focus] = true
		return
	case "pagedown":
		editor.focus = min(len(editor.fields)-1, i+5)
		editor.replace[editor.focus] = true
		return
	case "pageup":
		editor.focus = max(0, i-5)
		editor.replace[editor.focus] = true
		return
	case "left", "right":
		choices := editor.fieldChoices(i)
		if len(choices) == 0 {
			return
		}
		index := -1
		for j, v := range choices {
			if v == editor.values[i] {
				index = j
				break
			}
		}
		step := 1
		if key == "left" {
			step = -1
		}
		if index < 0 {
			index = 0
			step = 0
		}
		editor.setChoice(i, choices[(index+len(choices)+step)%len(choices)])
	case "ctrl-u":
		editor.values[i] = ""
		editor.replace[i] = false
	case "backspace":
		if editor.replace[i] {
			editor.values[i] = ""
		} else {
			runes := []rune(editor.values[i])
			if len(runes) > 0 {
				editor.values[i] = string(runes[:len(runes)-1])
			}
		}
		editor.replace[i] = false
	case "enter":
		operation := editor.operation
		if operation.create {
			operation.name, operation.path = strings.TrimSpace(editor.values[0]), strings.TrimSpace(editor.values[1])
			if operation.name == "" || operation.path == "" {
				editor.error = "请填写工程名称和配置文件路径"
				return
			}
		} else {
			operation.request.Action = "edit"
			if editor.operation.request.Action == "defaults" {
				operation.request.Action = "add"
			}
			operation.request.Fields = map[string]json.RawMessage{}
			for j, field := range editor.fields {
				if operation.request.Action == "edit" && editor.values[j] == editor.original[j] {
					continue
				}
				if editor.bindingsAvailable && j >= editor.bindingStart {
					fields, err := editor.bindingFields()
					if err != nil {
						editor.error = err.Error()
						return
					}
					operation.request.Fields["mappings"] = fields["mappings"]
					continue
				}
				raw, err := projectfile.ParseField(operation.request.Resource, field.key, editor.values[j])
				if err != nil {
					editor.error = err.Error()
					editor.focus = j
					return
				}
				operation.request.Fields[field.key] = raw
			}
			if operation.request.Action == "edit" && len(operation.request.Fields) == 0 {
				m.configurationEditor = nil
				m.setOperationNotice("未修改配置")
				return
			}
		}
		m.pendingConfiguration = &operation
		m.operationBusy = true
		return
	default:
		r, _ := utf8.DecodeRuneInString(key)
		if utf8.RuneCountInString(key) != 1 || unicode.IsControl(r) {
			return
		}
		if editor.replace[i] {
			editor.values[i] = ""
			editor.replace[i] = false
		}
		if len(editor.values[i])+len(key) > 4096 {
			return
		}
		editor.values[i] += key
	}
	if editor.operation.create {
		if i == 1 {
			editor.pathEdited = true
		}
		if i == 0 && !editor.pathEdited {
			safe := strings.NewReplacer("/", "_", "\\", "_").Replace(editor.values[0])
			editor.values[1] = filepath.Join(filepath.Dir(editor.original[1]), safe+".json")
		}
	}
	editor.error = ""
}
func performConfiguration(ctx context.Context, operation configurationOperation) configurationResponse {
	reply := configurationResponse{operation: operation}
	if operation.create {
		doc, err := projectfile.Create(operation.path, operation.name)
		if err != nil {
			reply.err = err
			return reply
		}
		reply.result = projectfile.Result{Saved: true, Path: doc.Path, Revision: doc.Revision, Message: "新工程已保存"}
		child, err := processinstance.Start(ctx, doc.Path, operation.dir)
		reply.started = &child
		reply.err = err
		if err == nil {
			reply.result.Applied = true
			reply.result.Message = "已创建独立工程，通道保持停止"
		}
		return reply
	}
	reply.view, reply.result, reply.err = observe.Configure(ctx, operation.socket, operation.request)
	return reply
}
func (m *model) applyConfiguration(reply configurationResponse) {
	m.operationBusy = false
	if reply.err != nil {
		if reply.result.Saved {
			m.configurationEditor = nil
			m.setOperationNotice("配置已保存，后续操作未完成：" + reply.err.Error())
		} else if m.configurationEditor != nil {
			m.configurationEditor.error = reply.err.Error()
		} else {
			m.setOperationNotice(reply.err.Error())
		}
		return
	}
	if reply.operation.create && reply.started != nil {
		child := reply.started
		item := instance{socket: child.Socket, info: child.Info}
		m.instances = append(m.instances, item)
		m.bindProject(item)
		m.filter = ""
		m.view, m.cursor = 0, len(m.instances)-1
		m.configurationEditor = nil
		m.setOperationNotice(reply.result.Message)
		return
	}
	if reply.operation.request.Action == "read" || reply.operation.request.Action == "defaults" {
		reply.operation.request.Revision = reply.view.Revision
		m.configurationEditor = newConfigurationEditor(reply.operation, reply.view)
		return
	}
	m.configurationEditor = nil
	if reply.operation.request.Action == "delete" {
		if reply.operation.request.Resource == "project" {
			kept := m.instances[:0]
			for _, item := range m.instances {
				if item.socket != reply.operation.socket {
					kept = append(kept, item)
				}
			}
			m.instances = kept
			if m.socket == reply.operation.socket {
				m.socket, m.channel, m.device = "", "", ""
				m.detail = observe.Info{}
				m.connected = false
			}
			m.selectView(0)
		} else {
			m.cursor = 0
			m.pointOffset = 0
		}
	}
	m.setOperationNotice(reply.result.Message)
	if reply.operation.socket == m.socket {
		m.detail.ConfigurationRevision = reply.result.Revision
		if reply.result.Point != nil {
			m.pointKind = reply.result.Point.Kind
			m.pointOffset = 0
		}
	}
}
func (e *configurationEditor) fieldLabel(field configurationField) string {
	if e.operation.request.Resource == "channel" {
		if field.key == "address" {
			return addressLabel(e.value("transport"))
		}
		if field.key == "localAddress" && e.value("transport") == "udp" {
			return "监听地址"
		}
	}
	return field.label
}
func (m *model) configurationBody(width, height int) []string {
	editor := m.configurationEditor
	if editor == nil {
		return nil
	}
	rows := max(1, height-m.overlayTop(height)-9)
	start := max(0, editor.focus-rows+1)
	end := min(len(editor.fields), start+rows)
	var lines []string
	contentWidth := min(width-7, 108) - 4
	labelWidth := 0
	for _, field := range editor.fields {
		labelWidth = max(labelWidth, uniseg.StringWidth(editor.fieldLabel(field)))
	}
	labelWidth = min(labelWidth, contentWidth/2)
	if editor.error != "" {
		lines = append(lines, clipped(editor.error, contentWidth, false))
	}
	for i := start; i < end; i++ {
		prefix := "  "
		if i == editor.focus {
			prefix = "› "
		}
		value := editor.values[i]
		if len(editor.fieldChoices(i)) > 0 {
			value = "‹ " + choiceLabel(editor.fields[i].key, value) + " ›"
		}
		if value == "" {
			value = "（空）"
		}
		value = clipped(value, contentWidth-labelWidth-4, i == editor.focus)
		lines = append(lines, prefix+fit(editor.fieldLabel(editor.fields[i]), labelWidth)+"  "+value)
	}
	if len(editor.fieldChoices(editor.focus)) > 0 {
		lines = append(lines, "←/→ 切换 · 空格打开选项")
		if editor.fields[editor.focus].key == "baud" {
			lines = append(lines, "支持选择常用波特率，也可输入自定义值。")
		}
	}
	if editor.operation.request.Resource == "rule" {
		lines = append(lines, "仅启用的规则运行；Modbus 由主站轮询读取。")
	}
	if editor.bindingsAvailable && len(editor.bindingTargets) == 0 {
		lines = append(lines, "无可绑定点位，请先在同一装置新增遥信或遥测。")
	}
	if editor.bindingsAvailable && editor.focus >= editor.bindingStart {
		lines = append(lines, "输入点位（如 301-302 或 301,311），留空取消该方向的绑定。")
	}
	if editor.operation.create {
		lines = append(lines, "新工程独立运行；退出界面不会停止工程。")
	}
	if m.operationBusy {
		lines = append([]string{"正在保存，请等待实际结果…"}, lines...)
	}
	return lines
}
