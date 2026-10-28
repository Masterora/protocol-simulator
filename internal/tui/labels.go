package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
)

func translated(value string, labels map[string]string) string {
	if label, ok := labels[value]; ok {
		return label
	}
	return value
}

var stateLabels = map[string]string{"running": "运行中", "stopped": "已停止", "paused": "已暂停", "listening": "监听中", "connecting": "连接中", "connected": "已连接", "disconnected": "已断开", "error": "异常", "waiting": "等待中", "": "未启动"}
var kindLabels = map[string]string{"yx": "遥信", "yc": "遥测", "yk": "遥控", "yt": "遥调", "mc": "累计量", "position": "步位置", "step": "步调节", "coil": "线圈", "discrete": "离散输入", "holding": "保持寄存器", "input": "输入寄存器", "raw": "原始数据"}
var modeLabels = map[string]string{"fixed": "固定", "random": "随机", "increment": "递增", "toggle": "翻转", "sequence": "序列", "report": "仅上报", "normal": "正常应答", "deny": "拒绝应答", "silent": "不应答"}
var levelLabels = map[string]string{"INFO": "信息", "WARN": "警告", "ERROR": "错误", "DEBUG": "调试", "TRACE": "跟踪"}
var directionLabels = map[string]string{"RX": "接收", "TX": "发送", "SYS": "系统"}
var eventLabels = map[string]string{"point_set": "手动修改点值", "point_report": "手动上报点位", "observer_ready": "观测接口已就绪", "observer_unavailable": "观测接口不可用", "starting": "工程正在启动", "running": "工程正在运行", "stopped": "工程已停止", "runtime_failure": "工程运行失败", "channel_state": "通道状态变化", "data_rules_started": "自动上报已启动", "protocol": "通信事件", "reloaded": "配置已热加载", "reload_rejected": "配置变更已拒绝", "reload_rolled_back": "配置变更已回退", "protocol_log_records_dropped": "部分报文日志已跳过", "log_records_dropped": "部分运行日志已跳过"}
var detailLabels = map[string]string{
	"pid": "进程号", "project": "工程", "configuration": "配置文件", "started": "启动时间", "channels": "通道列表", "id": "标识", "name": "名称", "protocol": "协议", "role": "角色", "address": "地址", "devices": "装置数", "state": "通道状态", "status": "状态", "tx": "发送量", "rx": "接收量", "peers": "连接数", "readyPeers": "已就绪连接数", "lastError": "最近错误", "unit": "站号", "commonAddress": "公共地址", "points": "点位", "kind": "类型", "value": "数值", "quality": "品质", "wireType": "报文类型", "dataType": "数据类型", "byteOrder": "字节序", "tab": "分类", "timestamp": "点位时间", "timeInvalid": "时间无效", "updated": "更新时间", "feedback": "反馈", "cycles": "执行次数", "rule": "规则", "enabled": "启用", "cause": "传送原因", "start": "起始地址", "end": "结束地址", "intervalMs": "周期（毫秒）", "mode": "模式", "min": "最小值", "max": "最大值", "step": "步长", "sequence": "序号", "time": "时间", "direction": "方向", "channel": "通道标识", "peer": "对端", "summary": "摘要", "level": "级别", "message": "事件", "fields": "字段", "rawFields": "原始字段", "eventCode": "事件标识",
}

func localizedDetail(value any, key string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, child := range v {
			out[translated(k, detailLabels)] = localizedDetail(child, k)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = localizedDetail(child, key)
		}
		return out
	case string:
		switch key {
		case "status":
			return translated(v, stateLabels)
		case "kind", "tab":
			return translated(v, kindLabels)
		case "mode":
			return translated(v, modeLabels)
		case "role":
			return translated(v, map[string]string{"server": "子站", "client": "主站"})
		case "direction":
			return translated(v, directionLabels)
		case "level":
			return translated(v, levelLabels)
		case "message":
			return translated(v, eventLabels)
		}
	case bool:
		if v {
			return "是"
		}
		return "否"
	case nil:
		return "无"
	}
	return value
}
func detailJSONLines(data any) []string {
	raw, err := json.Marshal(data)
	if err != nil {
		return []string{"无法显示详情：" + err.Error()}
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var v any
	if err = decoder.Decode(&v); err != nil {
		return []string{"无法读取详情：" + err.Error()}
	}
	raw, err = json.MarshalIndent(localizedDetail(v, ""), "", "  ")
	if err != nil {
		return []string{"无法显示详情：" + err.Error()}
	}
	return strings.Split(string(raw), "\n")
}
func connectionMessage(err error) string {
	if errors.Is(err, os.ErrNotExist) {
		return "观测连接不存在，请确认工程仍在运行"
	}
	if errors.Is(err, os.ErrPermission) {
		return "没有观测目录的访问权限，请使用工程所属用户"
	}
	var n net.Error
	if errors.As(err, &n) && n.Timeout() {
		return "读取工程超时，请检查运行进程"
	}
	return "无法读取工程：" + err.Error()
}

var channelLog = regexp.MustCompile(`^\[channel (\S+) status (\S+) peers (\d+) ready_peers (\d+) error(?: (.*))?\]$`)
var dataLog = regexp.MustCompile(`^\[channel (\S+) device (\S+) started (\d+)\]$`)
var protocolLog = regexp.MustCompile(`^\[sequence (\d+) channel (\S+) peer (.*?) direction (RX|TX|SYS) summary (.*)\]$`)

func logFields(event, raw string) string {
	switch event {
	case "channel_state":
		if m := channelLog.FindStringSubmatch(raw); m != nil {
			return fmt.Sprintf("通道 %s · %s · 连接 %s · 已就绪 %s%s", m[1], translated(m[2], stateLabels), m[3], m[4], func() string {
				if m[5] != "" {
					return " · 错误：" + m[5]
				}
				return ""
			}())
		}
	case "data_rules_started":
		if m := dataLog.FindStringSubmatch(raw); m != nil {
			return fmt.Sprintf("通道 %s · 装置 %s · 累计启动 %s 次", m[1], m[2], m[3])
		}
	case "protocol":
		if m := protocolLog.FindStringSubmatch(raw); m != nil {
			return fmt.Sprintf("%s · %s · %s", translated(m[4], directionLabels), m[3], m[5])
		}
	case "observer_ready":
		if strings.HasPrefix(raw, "[socket ") {
			return "观测接口 " + strings.TrimSuffix(strings.TrimPrefix(raw, "[socket "), "]")
		}
	case "running", "stopped":
		if strings.HasPrefix(raw, "[project ") {
			return "工程 " + strings.TrimSuffix(strings.TrimPrefix(raw, "[project "), "]")
		}
	}
	// Keep the exact, potentially unstructured diagnostic payload in row details.
	return "—"
}
