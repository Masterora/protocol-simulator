package core

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func value(d Device, key, fallback string) string {
	if d.Configuration != nil {
		if v, ok := d.Configuration.Values[key]; ok {
			return v
		}
	}
	return fallback
}
func number(d Device, key string, fallback int) int {
	v, err := strconv.Atoi(value(d, key, strconv.Itoa(fallback)))
	if err != nil {
		return fallback
	}
	return v
}
func flag(d Device, key string, fallback bool) bool {
	return value(d, key, strconv.FormatBool(fallback)) == "true"
}
func choice(d Device, key string, fallback int) int {
	s := value(d, key, strconv.Itoa(fallback))
	if i := strings.Index(s, ":"); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
func seconds(d Device, key string, fallback int) time.Duration {
	return time.Duration(number(d, key, fallback)) * time.Second
}

func validateSettings(protocol string, c *DeviceConfiguration) error {
	if c == nil {
		return nil
	}
	d := Device{Configuration: c}
	bounds := map[string][2]int{
		"txtTotal_Time": {0, 86400}, "txtMC_Time": {0, 1440}, "txtSyn_Time": {0, 1440}, "txtUTest_Time": {1, 86400},
		"text_replySICount": {1, 32767}, "txt_replyIFrame": {1, 32767}, "sendWindow": {1, 32767}, "ackDelayMs": {10, 60000},
		"retryNum": {0, 10}, "retryInterval": {1, 60}, "responseDelayMs": {0, 60000}, "txtSendInterval": {20, 3600000},
		"txt_overTime": {1, 60}, "txtTimeOut": {1, 60}, "SynTime": {0, 86400}, "eventCount": {1, 125},
		"txt_FrameOverTime": {1, 60000},
	}
	for k, b := range bounds {
		if !settingEnabled(d, k) {
			continue
		}
		if s, ok := c.Values[k]; ok {
			n, err := strconv.Atoi(s)
			if err != nil || n < b[0] || n > b[1] {
				return fmt.Errorf("%s应为 %d–%d 整数", settingLabel(k), b[0], b[1])
			}
		}
	}
	for _, k := range []string{"TimeAddr", "txtEventAddr"} {
		if !settingEnabled(d, k) {
			continue
		}
		if s, ok := c.Values[k]; ok {
			if _, err := parseAddress(s, 65535); err != nil {
				return fmt.Errorf("%s：%w", settingLabel(k), err)
			}
		}
	}
	if v := value(d, "responseMode", "normal"); v != "normal" && v != "deny" && v != "silent" {
		return fmt.Errorf("应答策略无效")
	}
	for _, key := range []string{"txtASDU_Byte", "textEdit3", "txtMess_Byte", "txtCOT", "textEdit2"} {
		if s, ok := c.Values[key]; ok {
			if _, err := strconv.Atoi(s); err != nil {
				return fmt.Errorf("%s 字节数无效", key)
			}
		}
	}
	for key, max := range map[string]int{"cb_ycType": 4, "cb_ycFormat": 4, "cb_mcType": 2, "cb_mcFormat": 2, "cb_ycFunWay": 3, "cb_mcFunWay": 3, "cb_yxtwoWay": 3, "cb_yxTwoFun": 3, "cb_check": 1} {
		if protocol == "modbus-tcp" && key == "cb_check" {
			continue
		}
		if s, ok := c.Values[key]; ok {
			if i := strings.Index(s, ":"); i >= 0 {
				s = s[:i]
			}
			n, err := strconv.Atoi(s)
			if err != nil || n < 0 || n > max {
				return fmt.Errorf("%s选项无效", settingLabel(key))
			}
		}
	}
	if protocol == "iec104" {
		if value(d, "txt_DeviceAddr", "") != "" {
			if _, err := strconv.Atoi(value(d, "txtAdd", "3")); err != nil {
				return fmt.Errorf("信息点位字节数无效")
			}
		}

		cot, ca, ioa := layout104(d)
		if cot < 1 || cot > 2 || ca < 1 || ca > 2 || ioa < 1 || ioa > 3 {
			return fmt.Errorf("COT/ASDU/信息点位字节数无效")
		}
		if !strings.Contains("|单点|双点|", "|"+value(d, "cbYX_Type", "单点")+"|") {
			return fmt.Errorf("遥信类型无效")
		}
		if _, err := configured104Type(d, "yc"); err != nil {
			return err
		}
	}
	if strings.HasPrefix(protocol, "modbus") {
		for _, tab := range []string{"yc", "mc"} {
			format, _ := registerFormat(d, tab)
			if format == "invalid" {
				return fmt.Errorf("%s 数据格式无效", tab)
			}
		}
		if flag(d, "clockEnabled", false) || flag(d, "cbTime", false) {
			a, err := parseAddress(value(d, "TimeAddr", "1000H"), 65532)
			if err != nil {
				return err
			}
			for _, r := range c.Ranges {
				format, _ := registerFormat(d, r.Tab)
				width := registerWidth(Point{DataType: format})
				points, err := rangePointNumbers(protocol, c, r)
				if err != nil {
					return err
				}
				for _, point := range points {
					if (r.Kind == "holding" || r.Kind == "input") && uint64(point) < uint64(a)+4 && uint64(point)+uint64(width) > uint64(a) {
						return fmt.Errorf("校时寄存器与点表范围重叠")
					}
				}
			}
		}
	}
	return nil
}
func parseAddress(s string, max uint64) (uint32, error) {
	base := 10
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		base = 16
		s = s[2:]
	} else if strings.HasSuffix(strings.ToUpper(s), "H") {
		base = 16
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseUint(s, base, 32)
	if err != nil || n > max {
		return 0, fmt.Errorf("点位无效")
	}
	return uint32(n), nil
}

func settingEnabled(d Device, key string) bool {
	switch key {
	case "sendWindow":
		return !flag(d, "ck_IsDataSentWaitS", false)
	case "txtSendInterval":
		return flag(d, "pollEnabled", true)
	case "txtMC_Time":
		return flag(d, "mcEnable", true)
	case "txtSyn_Time":
		return flag(d, "timeEnable", true)
	case "retryNum", "retryInterval":
		return flag(d, "cbxRetry", true)
	case "txtEventAddr", "eventCount":
		return flag(d, "cb_ReadEvent", true)
	case "SynTime":
		return flag(d, "cbTime", true)
	case "TimeAddr":
		return flag(d, "clockEnabled", false) || flag(d, "cbTime", false) || value(d, "clockEnabled", "") == ""
	case "responseDelayMs":
		return value(d, "responseMode", "normal") != "silent"
	}
	return true
}
func settingLabel(key string) string {
	labels := map[string]string{"cb_ycType": "遥测数据类型", "cb_ycFormat": "遥测数据类型", "cb_mcType": "脉冲数据类型", "cb_mcFormat": "脉冲数据类型", "cb_ycFunWay": "遥测字节序", "cb_mcFunWay": "脉冲字节序", "cb_yxtwoWay": "遥信格式", "cb_yxTwoFun": "遥信格式", "cb_check": "校验码格式", "txtMC_Time": "脉冲召唤间隔", "txtSyn_Time": "周期校时间隔", "retryNum": "超时重发次数", "retryInterval": "超时重发间隔", "txtTotal_Time": "总召间隔", "txtUTest_Time": "U测试间隔", "text_replySICount": "累计接收I帧数量", "txt_replyIFrame": "累计接收I帧数量", "sendWindow": "未确认发送窗口", "ackDelayMs": "S帧最长等待", "responseDelayMs": "应答延迟", "txtSendInterval": "采集间隔", "txt_overTime": "应答超时", "txtTimeOut": "报文超时", "SynTime": "校时间隔", "eventCount": "事件读取寄存器数", "TimeAddr": "校时首点位", "txtEventAddr": "事件读取首点位", "txt_FrameOverTime": "分帧超时"}
	if s := labels[key]; s != "" {
		return s
	}
	return key
}

func sendWindow104(d Device) int {
	if flag(d, "ck_IsDataSentWaitS", false) {
		return 1
	}
	return number(d, "sendWindow", 12)
}
