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
	"unicode/utf8"

	"github.com/rivo/uniseg"
)

type configurationOperation struct {
	transfer        *pointTransfer
	socket          string
	request         projectfile.Request
	create          bool
	path, name, dir string
}
type configurationResponse struct {
	preview   *pointTransferPreview
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

const (
	initialStart = "initial-start:"
	initialCount = "initial-count:"
)

type configurationEditor struct {
	submit            func(*configurationEditor) error
	submitLabel       string
	operation         configurationOperation
	fields            []configurationField
	values, original  []string
	replace           []bool
	focus             int
	caret             textCursor
	endpointCaret     endpointCursor
	initialTable      int
	start             int
	visibleRows       int
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
			{key: "name", label: "通道名称"}, {"protocol", "协议", []string{"iec104", "modbus-tcp", "modbus-rtu"}}, {"role", "角色", []string{"server", "client"}}, {"transport", "连接方式", []string{"tcp-server", "tcp-client", "serial"}}, {key: "address", label: "通信地址"}, {key: "localAddress", label: "本地绑定"}, {key: "timeoutMs", label: "超时(ms)"}, {"baud", "波特率", []string{"1200", "2400", "4800", "9600", "19200", "38400", "57600", "115200", "230400", "460800", "921600"}}, {"parity", "校验位", []string{"N", "E", "O"}}, {"dataBits", "数据位", []string{"5", "6", "7", "8"}}, {"stopBits", "停止位", []string{"1", "2"}},
		}
	case "device":
		fields := []configurationField{{key: "name", label: "装置名称"}}
		if protocol == "iec104" {
			fields = append(fields, configurationField{key: "commonAddress", label: "公共地址"})
		} else {
			fields = append(fields, configurationField{key: "unit", label: "站号"})
		}
		if action == "add" {
			kinds := projectfile.DevicePointKinds(protocol)
			fields = append(fields, configurationField{key: "pointCount", label: "初始点数"}, configurationField{"pointKind", "初始类型", kinds}, configurationField{key: "pointStart", label: "起始点位"})
		}
		return fields
	case "point":
		kinds := projectfile.DevicePointKinds(protocol)
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
	if resource == "device" {
		if view.Protocol != "" {
			editor.title += " · " + choiceLabel("protocol", view.Protocol)
		}
		if formAction == "add" && len(view.Value["initialPoints"]) != 0 {
			editor.fields = configurationFields(resource, "edit", view.Protocol)
			editor.initialTable = len(editor.fields)
			for _, kind := range projectfile.DevicePointKinds(view.Protocol) {
				label := kindLabels[kind]
				editor.fields = append(editor.fields,
					configurationField{key: initialStart + kind, label: label + "起始点位"},
					configurationField{key: initialCount + kind, label: label + "点数"},
				)
			}
		}
	}
	for _, field := range editor.fields {
		value := displayField(resource, field, view.Value[field.key])
		if resource == "channel" && formAction == "add" && field.key == "address" {
			value = ""
		}
		if kind, ok := strings.CutPrefix(field.key, initialStart); ok {
			value = strconv.FormatUint(uint64(projectfile.DevicePointStart(view.Protocol, kind)), 10)
		}
		if strings.HasPrefix(field.key, initialCount) {
			value = strconv.Itoa(projectfile.DefaultDevicePointCount)
		}
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
	if action == "add" && m.view >= 2 && operation.request.Channel == "" {
		m.setOperationNotice("请先在通道页选中通道")
		return operation, false
	}
	if action == "add" && m.view >= 3 && operation.request.Device == "" {
		m.setOperationNotice("请先在装置页选中装置")
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
func toolEditor(title, submitLabel string, fields []configurationField, values []string, submit func(*configurationEditor) error) *configurationEditor {
	replace := make([]bool, len(values))
	for i := range replace {
		replace[i] = true
	}
	return &configurationEditor{title: title, submitLabel: submitLabel, fields: fields, values: values, replace: replace, submit: submit}
}

func (e *configurationEditor) acceptsText(index int) bool {
	key := e.fields[index].key
	return key == "baud" || key == "address" || len(e.fieldChoices(index)) == 0
}

func (e *configurationEditor) focusField(index int) {
	e.focus = index
	e.replace[index] = true
	e.caret = textCursor{}
	e.endpointCaret = endpointCursor{}
}

func (e *configurationEditor) endpoint(i int) (endpointParts, bool) {
	key := e.fields[i].key
	if e.operation.request.Resource != "channel" || (key != "address" && key != "localAddress") || e.value("transport") == "serial" {
		return endpointParts{}, false
	}
	return parseEndpoint(e.values[i])
}

func (e *configurationEditor) rowForField(field int) int {
	if e.operation.request.Resource == "channel" {
		row := 0
		for i := range field {
			if e.fieldVisible(i) {
				row++
			}
		}
		return row
	}
	if e.initialTable > 0 && field >= e.initialTable {
		return e.initialTable + (field-e.initialTable)/2
	}
	return field
}

func (e *configurationEditor) fieldForRow(row int) int {
	if e.operation.request.Resource == "channel" {
		for i := range e.fields {
			if e.fieldVisible(i) {
				if row == 0 {
					return i
				}
				row--
			}
		}
	}
	if e.initialTable > 0 && row >= e.initialTable {
		return e.initialTable + (row-e.initialTable)*2
	}
	return row
}

func (e *configurationEditor) rowCount() int {
	if e.operation.request.Resource == "channel" {
		return e.rowForField(len(e.fields))
	}
	return e.rowForField(len(e.fields)-1) + 1
}

func (e *configurationEditor) fieldVisible(index int) bool {
	if e.operation.request.Resource == "channel" {
		switch e.fields[index].key {
		case "localAddress":
			return e.value("transport") == "tcp-client"
		case "baud", "parity", "dataBits", "stopBits":
			return e.value("transport") == "serial"
		}
	}
	return true
}

func (e *configurationEditor) focusRow(row int) {
	field := e.fieldForRow(row)
	if e.initialTable > 0 && e.focus >= e.initialTable && field >= e.initialTable {
		field += (e.focus - e.initialTable) % 2
	}
	e.focusField(field)
}

func (e *configurationEditor) initialSelections() (map[string]string, error) {
	var selections map[string]string
	for i, field := range e.fields {
		kind, ok := strings.CutPrefix(field.key, initialCount)
		if !ok {
			continue
		}
		if selections == nil {
			selections = map[string]string{}
		}
		count, err := strconv.Atoi(strings.TrimSpace(e.values[i]))
		if err != nil || count < 0 || count > 10000 {
			e.focusField(i)
			return nil, fmt.Errorf("%s应为 0–10000 的整数", field.label)
		}
		if count == 0 {
			continue
		}
		text := strings.TrimSpace(e.values[i-1])
		base := 10
		if strings.HasPrefix(strings.ToLower(text), "0x") {
			base = 0
		}
		start, err := strconv.ParseUint(text, base, 24)
		if err != nil {
			e.focusField(i - 1)
			return nil, fmt.Errorf("%s应为有效的非负整数", e.fields[i-1].label)
		}
		selections[kind] = fmt.Sprintf("%d-%d", start, start+uint64(count)-1)
	}
	return selections, nil
}

func (m *model) configurationKey(key string) {
	editor := m.configurationEditor
	if editor == nil {
		return
	}
	i := editor.focus
	editable := editor.acceptsText(i)
	if !editable && (key == "ctrl-u" || key == "backspace" || key == "delete" || utf8.RuneCountInString(key) == 1 && key != " ") {
		editor.error = "此项使用 ←/→ 切换，空格打开选项"
		return
	}
	if key == " " && len(editor.fieldChoices(i)) > 0 {
		m.openChoices(editor.fieldLabel(editor.fields[i]), editor.fields[i].key, editor.values[i], editor.fieldChoices(i), func(v string) { editor.setChoice(i, v) })
		return
	}
	if parts, ok := editor.endpoint(i); ok {
		if editor.endpointCaret.key(&parts, &editor.replace[i], key) {
			editor.values[i] = parts.string()
			editor.error = ""
			return
		}
	}
	switch key {
	case "esc":
		m.configurationEditor = nil
		return
	case "tab":
		next := (i + 1) % len(editor.fields)
		for !editor.fieldVisible(next) {
			next = (next + 1) % len(editor.fields)
		}
		editor.focusField(next)
		return
	case "shift-tab":
		previous := (i + len(editor.fields) - 1) % len(editor.fields)
		for !editor.fieldVisible(previous) {
			previous = (previous + len(editor.fields) - 1) % len(editor.fields)
		}
		editor.focusField(previous)
		return
	case "up", "down":
		total := editor.rowCount()
		step := 1
		if key == "up" {
			step = -1
		}
		editor.focusRow((editor.rowForField(i) + total + step) % total)
		return
	case "pagedown":
		step := max(1, editor.visibleRows)
		editor.focusRow(min(editor.rowCount()-1, editor.rowForField(i)+step))
		editor.start += step
		return
	case "pageup":
		step := max(1, editor.visibleRows)
		editor.focusRow(max(0, editor.rowForField(i)-step))
		editor.start = max(0, editor.start-step)
		return
	case "left", "right":
		if editable {
			editor.caret.key(&editor.values[i], &editor.replace[i], key, 4096)
			return
		}
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
	case "enter":
		if editor.submit != nil {
			if err := editor.submit(editor); err != nil {
				editor.error = err.Error()
			}
			return
		}
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
			initialPoints, err := editor.initialSelections()
			if err != nil {
				editor.error = err.Error()
				return
			}
			for j, field := range editor.fields {
				if !editor.fieldVisible(j) || strings.HasPrefix(field.key, initialStart) || strings.HasPrefix(field.key, initialCount) {
					continue
				}
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
				value := editor.values[j]
				if parts, ok := editor.endpoint(j); ok && (value != "" || field.key == "address") {
					value, err = parts.checked()
					if err != nil {
						editor.error = editor.fieldLabel(field) + "：" + err.Error()
						editor.focusField(j)
						return
					}
				}
				raw, err := projectfile.ParseField(operation.request.Resource, field.key, value)
				if err != nil {
					editor.error = editor.fieldLabel(field) + "：" + err.Error()
					editor.focusField(j)
					return
				}
				operation.request.Fields[field.key] = raw
			}
			if initialPoints != nil {
				operation.request.Fields["initialPoints"], _ = json.Marshal(initialPoints)
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
		if !editable || !editor.caret.key(&editor.values[i], &editor.replace[i], key, 4096) {
			return
		}
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
	if operation.transfer != nil {
		performPointTransfer(ctx, *operation.transfer, &reply)
		return reply
	}
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
			reply.result.Message = "空工程已创建，进入通道页按 n 新增"
		}
		return reply
	}
	reply.view, reply.result, reply.err = observe.Configure(ctx, operation.socket, operation.request)
	return reply
}
func (m *model) applyConfiguration(reply configurationResponse) {
	m.operationBusy = false
	if reply.operation.transfer != nil || reply.operation.request.Resource == "point-table" {
		m.applyPointTransfer(reply)
		return
	}
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
	}
	return field.label
}

const (
	initialTypeWidth  = 12
	initialStartWidth = 16
	initialCountWidth = 10
)

func initialPointHeading() string {
	return "  " + fit("类型", initialTypeWidth) + "  " + fit("起始点位", initialStartWidth) + "  点数"
}

func (e *configurationEditor) displayValue(i, width int) string {
	if parts, ok := e.endpoint(i); ok {
		return e.endpointCaret.render(parts, i == e.focus)
	}
	value := e.values[i]
	if i == e.focus && e.acceptsText(i) {
		return e.caret.render(value, width)
	}
	if len(e.fieldChoices(i)) > 0 && !e.acceptsText(i) {
		value = "‹ " + choiceLabel(e.fields[i].key, value) + " ›"
	}
	if value == "" {
		value = "（空）"
	}
	return clipped(value, width, false)
}

func (m *model) configurationBody(width, height int) []string {
	editor := m.configurationEditor
	if editor == nil {
		return nil
	}
	var lines []string
	contentWidth := min(width-7, 108) - 4
	capacity := m.overlayRows(height)
	if editor.error != "" {
		errorLines := wrap("错误 "+editor.error, contentWidth)
		limit := min(len(errorLines), max(1, capacity-2))
		lines = append(lines, errorLines[:limit]...)
		if limit < len(errorLines) {
			lines[limit-1] = clipped(lines[limit-1]+errorLines[limit], contentWidth, false)
		}
	}
	var hint string
	switch {
	case editor.bindingsAvailable && len(editor.bindingTargets) == 0:
		hint = "无可绑定点位。请先新增同装置的遥信或遥测。"
	case editor.bindingsAvailable && editor.focus >= editor.bindingStart:
		hint = "填点位 301-302 或 301,311，留空解除绑定。"
	case editor.fields[editor.focus].key == "baud":
		hint = "可选常用波特率，也可输入自定义值。"
	case editor.initialTable > 0 && capacity >= 6:
		hint = "点数为 0 时跳过该类型。"
	case editor.operation.create:
		hint = "新工程从空白开始，通道在通道页添加。"
	case editor.operation.request.Resource == "rule" && editor.fields[editor.focus].key == "enabled":
		hint = "仅启用的规则运行。Modbus 由主站轮询读取。"
	}
	rows := capacity - len(lines)
	if hint != "" && rows > 2 {
		rows--
	} else {
		hint = ""
	}
	if editor.initialTable > 0 && rows > 1 {
		rows--
	}
	editor.visibleRows = rows
	total := editor.rowCount()
	focus := editor.rowForField(editor.focus)
	editor.start = visibleStart(editor.start, focus, rows, total)
	end := min(total, editor.start+rows)
	labelWidth := 0
	baseFields := editor.fields
	if editor.initialTable > 0 {
		baseFields = baseFields[:editor.initialTable]
	}
	for _, field := range baseFields {
		labelWidth = max(labelWidth, uniseg.StringWidth(editor.fieldLabel(field)))
	}
	labelWidth = min(labelWidth, contentWidth/2)
	for row := editor.start; row < end; row++ {
		prefix := "  "
		if row == focus {
			prefix = "› "
		}
		i := editor.fieldForRow(row)
		if editor.initialTable > 0 && i >= editor.initialTable {
			if row == max(editor.start, editor.initialTable) {
				lines = append(lines, initialPointHeading())
			}
			kind := strings.TrimPrefix(editor.fields[i].key, initialStart)
			start := fit(editor.displayValue(i, initialStartWidth), initialStartWidth)
			count := editor.displayValue(i+1, initialCountWidth)
			lines = append(lines, prefix+fit(kindLabels[kind], initialTypeWidth)+"  "+start+"  "+count)
			continue
		}
		valueWidth := contentWidth - labelWidth - 4
		lines = append(lines, prefix+fit(editor.fieldLabel(editor.fields[i]), labelWidth)+"  "+editor.displayValue(i, valueWidth))
	}
	if hint != "" {
		lines = append(lines, clipped(hint, contentWidth, false))
	}
	return lines
}
