package core

import "errors"

// Independent telemetry tests share the existing update/report worker. Their
// saved parameters and runtime state are scoped to the selected category.
func validDataCategory(category string) bool { return category == "yx" || category == "yc" }

func dataTestCategory(scope string) string {
	switch scope {
	case "telemetry":
		return "telemetry"
	case "yx", "yx-report":
		return "yx"
	case "yc", "yc-report":
		return "yc"
	}
	return ""
}
func reportingDataTest(scope string) bool { return scope == "yx-report" || scope == "yc-report" }

func dataScopeMatches(scope, protocol string, p Point) bool {
	if scope == "" {
		return true
	}
	category := dataPointCategory(protocol, p)
	if scope == "telemetry" {
		return validDataCategory(category)
	}
	return category == dataTestCategory(scope)
}

func dataJobKey(channel, device, category string) string {
	mode := "data"
	if category != "" {
		mode += ":" + category
	}
	return autoKey(channel, device, mode)
}

func cloneDataTests(tests map[string]*AutoDataConfig) map[string]*AutoDataConfig {
	if tests == nil {
		return nil
	}
	copy := make(map[string]*AutoDataConfig, len(tests))
	for category, config := range tests {
		copy[category] = cloneAutoData(config)
	}
	return copy
}

func dataPointCategory(protocol string, p Point) string {
	if p.Tab != "" {
		return p.Tab
	}
	if protocol == "iec104" {
		return p.Kind
	}
	return map[string]string{"discrete": "yx", "holding": "yc", "input": "yc", "coil": "yk"}[p.Kind]
}

func dataRuleCategory(kind string) string {
	if dataBit(kind) {
		return "yx"
	}
	return "yc"
}

func dataConfiguration(d *Device, category string) *AutoDataConfig {
	if category == "" {
		return cloneAutoData(d.AutoData)
	}
	if config, ok := d.DataTests[category]; ok {
		return cloneAutoData(config)
	}
	if category == "telemetry" {
		merged := &AutoDataConfig{Rules: []AutoDataRule{}, StopOnDisconnect: true}
		for _, scope := range []string{"yx", "yc", "yx-report", "yc-report"} {
			if old := dataConfiguration(d, scope); old != nil {
				merged.Rules = append(merged.Rules, old.Rules...)
				merged.StopOnDisconnect = merged.StopOnDisconnect && old.StopOnDisconnect
			}
		}
		return merged
	}
	if reportingDataTest(category) {
		return nil
	}
	// Older projects stored both telemetry tests together. Copy only the selected
	// category's rules until it has its own saved configuration.
	config := cloneAutoData(d.AutoData)
	if config == nil {
		return nil
	}
	rules := config.Rules[:0]
	for _, rule := range config.Rules {
		if dataRuleCategory(rule.Kind) == category {
			rules = append(rules, rule)
		}
	}
	config.Rules = rules
	return config
}

func validateDataTest(protocol, role, category string, config *AutoDataConfig, points ...Point) error {
	if category != "" && dataTestCategory(category) == "" {
		return errors.New("无效的独立测试分类")
	}
	if reportingDataTest(category) && (protocol != "iec104" || role != "server") {
		return errors.New("定时上报仅适用于104子站")
	}
	if config != nil && category != "" && category != "telemetry" {
		for _, rule := range config.Rules {
			if rule.Enabled && dataRuleCategory(rule.Kind) != dataTestCategory(category) {
				return errors.New("测试规则不属于当前四遥分类")
			}
			if rule.Enabled && (reportingDataTest(category) != (rule.Mode == "report")) {
				return errors.New("定时上报与自动变化应使用各自的配置")
			}
		}
	}
	if category != "" {
		selected := make([]Point, 0, len(points))
		for _, point := range points {
			if dataScopeMatches(category, protocol, point) {
				selected = append(selected, point)
			}
		}
		points = selected
	}
	return validateAutoData(protocol, role, config, points...)
}

func (e *Engine) GetDataTest(channel, device, category string) (AutoDataSnapshot, error) {
	if dataTestCategory(category) == "" {
		return AutoDataSnapshot{}, errors.New("无效的独立测试分类")
	}
	return e.getAutoData(channel, device, category)
}
func (e *Engine) SaveDataTest(channel, device, category string, config AutoDataConfig) (AutoDataSnapshot, error) {
	if dataTestCategory(category) == "" {
		return AutoDataSnapshot{}, errors.New("无效的独立测试分类")
	}
	return e.saveAutoData(channel, device, category, config, false)
}
func (e *Engine) StartDataTest(channel, device, category string) (AutoDataSnapshot, error) {
	if dataTestCategory(category) == "" {
		return AutoDataSnapshot{}, errors.New("无效的独立测试分类")
	}
	return e.startAutoData(channel, device, category)
}
func (e *Engine) SetDataTestState(channel, device, category, action string) (AutoDataSnapshot, error) {
	if dataTestCategory(category) == "" {
		return AutoDataSnapshot{}, errors.New("无效的独立测试分类")
	}
	return e.setAutoDataState(channel, device, category, action)
}
