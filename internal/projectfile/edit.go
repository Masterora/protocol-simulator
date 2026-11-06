package projectfile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gytest-go/internal/core"
)

type Request struct {
	Action   string                     `json:"action"`
	Resource string                     `json:"resource"`
	Revision string                     `json:"revision,omitempty"`
	Channel  string                     `json:"channel,omitempty"`
	Device   string                     `json:"device,omitempty"`
	Point    *core.PointKey             `json:"point,omitempty"`
	Index    *int                       `json:"index,omitempty"`
	Fields   map[string]json.RawMessage `json:"fields,omitempty"`
}
type View struct {
	BindingTargets []core.Point               `json:"bindingTargets,omitempty"`
	LocalAddresses []string                   `json:"localAddresses,omitempty"`
	Path           string                     `json:"path"`
	Revision       string                     `json:"revision"`
	Resource       string                     `json:"resource"`
	Protocol       string                     `json:"protocol,omitempty"`
	Value          map[string]json.RawMessage `json:"value"`
}
type Result struct {
	Deleted         bool           `json:"deleted,omitempty"`
	Saved           bool           `json:"saved"`
	Applied         bool           `json:"applied"`
	Path            string         `json:"path,omitempty"`
	Revision        string         `json:"revision,omitempty"`
	Channel         string         `json:"channel,omitempty"`
	Device          string         `json:"device,omitempty"`
	Point           *core.PointKey `json:"point,omitempty"`
	Index           *int           `json:"index,omitempty"`
	StoppedChannels []string       `json:"stoppedChannels,omitempty"`
	Message         string         `json:"message,omitempty"`
	Error           string         `json:"error,omitempty"`
}

func Object(v any) map[string]json.RawMessage {
	data, _ := json.Marshal(v)
	var obj map[string]json.RawMessage
	json.Unmarshal(data, &obj)
	return obj
}
func Fields(values map[string]any) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, v := range values {
		out[k], _ = json.Marshal(v)
	}
	return out
}

var kinds = map[string]map[string]string{
	"project": {"name": "string"},
	"channel": {"id": "string", "name": "string", "protocol": "string", "role": "string", "transport": "string", "address": "string", "localAddress": "string", "baud": "integer", "parity": "string", "dataBits": "integer", "stopBits": "integer", "timeoutMs": "integer"},
	"device":  {"id": "string", "name": "string", "unit": "integer", "commonAddress": "integer", "pointCount": "integer", "pointKind": "string", "pointStart": "integer"},
	"point":   {"name": "string", "kind": "string", "address": "integer", "value": "number", "quality": "integer", "wireType": "integer", "dataType": "string", "byteOrder": "string", "tab": "string", "feedback": "string", "count": "integer", "mappings": "mappings"},
	"rule":    {"enabled": "boolean", "kind": "string", "points": "string", "start": "integer", "end": "integer", "intervalMs": "integer", "mode": "string", "min": "number", "max": "number", "step": "number", "cause": "integer", "stopOnDisconnect": "boolean"},
	"binding": {"mappings": "mappings"},
}

func FieldType(resource, key string) string { return kinds[resource][key] }
func ParseField(resource, key, text string) (json.RawMessage, error) {
	var value any
	switch FieldType(resource, key) {
	case "string":
		value = text
	case "integer":
		base := 10
		if strings.HasPrefix(strings.ToLower(text), "0x") {
			base = 0
		}
		n, err := strconv.ParseInt(text, base, 64)
		if err != nil {
			return nil, fmt.Errorf("%s 应为整数", key)
		}
		value = n
	case "number":
		n, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil, fmt.Errorf("%s 应为有限数字", key)
		}
		value = n
	case "boolean":
		if text != "true" && text != "false" {
			return nil, fmt.Errorf("%s 应为 true 或 false", key)
		}
		value = text == "true"
	case "mappings":
		mappings, err := parseMappings(json.RawMessage(text))
		if err != nil {
			return nil, err
		}
		value = mappings
	default:
		return nil, fmt.Errorf("不支持字段 %s", key)
	}
	return json.Marshal(value)
}
func patch(resource string, target any, fields map[string]json.RawMessage, virtual ...string) error {
	skip := map[string]bool{}
	for _, key := range virtual {
		skip[key] = true
	}
	obj := Object(target)
	for key, v := range fields {
		if FieldType(resource, key) == "" {
			return fmt.Errorf("不支持字段 %s", key)
		}
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fmt.Errorf("%s 不能为 null", key)
		}
		if !skip[key] {
			obj[key] = v
		}
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
func channel(p *core.Project, id string) (*core.Channel, error) {
	for i := range p.Channels {
		if p.Channels[i].ID == id {
			return &p.Channels[i], nil
		}
	}
	return nil, errors.New("通道不存在")
}
func device(c *core.Channel, id string) (*core.Device, error) {
	for i := range c.Devices {
		if c.Devices[i].ID == id {
			return &c.Devices[i], nil
		}
	}
	return nil, errors.New("装置不存在")
}
func point(d *core.Device, key *core.PointKey) (*core.Point, error) {
	if key == nil {
		return nil, errors.New("必须指定点类型和地址")
	}
	for i := range d.Points {
		p := &d.Points[i]
		if p.Kind == key.Kind && p.Address == key.Address {
			return p, nil
		}
	}
	return nil, errors.New("点位不存在")
}
func RuleConfiguration(d core.Device) core.AutoDataConfig { return core.TelemetryConfiguration(d) }
func parseMappings(raw json.RawMessage) ([]core.ControlMapping, error) {
	var mappings []core.ControlMapping
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if !json.Valid(raw) || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, errors.New("绑定应为 JSON 数组，清空请使用 []")
	}
	if err := decoder.Decode(&mappings); err != nil {
		return nil, err
	}
	return mappings, nil
}
func applyPointBindings(c core.Channel, d *core.Device, source core.PointKey, raw json.RawMessage) error {
	mappings, err := parseMappings(raw)
	if err != nil {
		return err
	}
	return core.SetPointBindings(c, d, source, mappings)
}
func ReadView(doc Document, req Request) (View, error) {
	v := View{Path: doc.Path, Revision: doc.Revision, Resource: req.Resource}
	if req.Resource == "channel" {
		v.LocalAddresses = core.LocalIPv4()
	}
	if req.Resource == "project" {
		v.Value = Fields(map[string]any{"name": doc.Project.Name})
		return v, nil
	}
	c, err := channel(&doc.Project, req.Channel)
	if err != nil {
		return v, err
	}
	v.Protocol = c.Protocol
	switch req.Resource {
	case "channel":
		copy := *c
		copy.Devices = nil
		copy.Points = nil
		v.Value = Object(copy)
	case "device", "point", "rule", "binding":
		d, err := device(c, req.Device)
		if err != nil {
			return v, err
		}
		switch req.Resource {
		case "binding":
			if req.Point == nil {
				return v, errors.New("请选择控制点位")
			}
			category, targets, err := core.PointBindingOptions(*c, *d, *req.Point)
			if err != nil {
				return v, err
			}
			v.BindingTargets = targets
			mappings := []core.ControlMapping{}
			for _, m := range core.ControlConfiguration(*d, category).Mappings {
				if m.Source == *req.Point {
					mappings = append(mappings, m)
				}
			}
			v.Value = Fields(map[string]any{"mappings": mappings, "category": category})
		case "device":
			v.Value = Fields(map[string]any{"id": d.ID, "name": d.Name, "unit": d.Unit, "commonAddress": d.CommonAddress})
		case "point":
			p, err := point(d, req.Point)
			if err != nil {
				return v, err
			}
			v.Value = Object(p)
			if category, targets, err := core.PointBindingOptions(*c, *d, *req.Point); err == nil {
				v.BindingTargets = targets
				mappings := []core.ControlMapping{}
				for _, m := range core.ControlConfiguration(*d, category).Mappings {
					if m.Source == *req.Point {
						mappings = append(mappings, m)
					}
				}
				v.Value["mappings"], _ = json.Marshal(mappings)
				v.Value["category"], _ = json.Marshal(category)
			}
		case "rule":
			cfg := RuleConfiguration(*d)
			if req.Index == nil || *req.Index < 0 || *req.Index >= len(cfg.Rules) {
				return v, errors.New("上报规则已变化，请刷新")
			}
			v.Value = Object(cfg.Rules[*req.Index])
			v.Value["stopOnDisconnect"], _ = json.Marshal(cfg.StopOnDisconnect)
		}
	default:
		return v, errors.New("不支持的配置资源")
	}
	return v, nil
}
func intField(fields map[string]json.RawMessage, key string, fallback int) (int, error) {
	if b, ok := fields[key]; ok {
		var n int
		if err := json.Unmarshal(b, &n); err != nil {
			return 0, fmt.Errorf("%s 应为整数", key)
		}
		return n, nil
	}
	return fallback, nil
}
func stringField(fields map[string]json.RawMessage, key, fallback string) (string, error) {
	if b, ok := fields[key]; ok {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return "", err
		}
		return s, nil
	}
	return fallback, nil
}

// Apply changes a detached file configuration, never a live-value snapshot.
func Apply(previous core.Project, req Request) (core.Project, Result, error) {
	p, err := core.NormalizeProject(previous)
	result := Result{Channel: req.Channel, Device: req.Device, Point: req.Point, Index: req.Index}
	if err != nil {
		return p, result, err
	}
	fail := func(err error) (core.Project, Result, error) { return core.Project{}, result, err }
	if req.Action == "delete" {
		return deleteResource(p, req)
	}
	if req.Action != "add" && req.Action != "edit" {
		return fail(errors.New("仅支持新增或编辑配置"))
	}
	if req.Action == "edit" && len(req.Fields) == 0 {
		return fail(errors.New("没有要保存的字段"))
	}
	if req.Action == "edit" {
		if _, ok := req.Fields["id"]; ok {
			return fail(errors.New("已有通道或装置的 ID 不可修改"))
		}
	}
	if req.Resource == "project" {
		if req.Action != "edit" {
			return fail(errors.New("请使用 project new 新建独立工程"))
		}
		draft := struct {
			Name string `json:"name"`
		}{p.Name}
		if err = patch("project", &draft, req.Fields); err != nil {
			return fail(err)
		}
		draft.Name = strings.TrimSpace(draft.Name)
		if draft.Name == "" || len([]rune(draft.Name)) > 100 {
			return fail(errors.New("名称应为 1–100 字"))
		}
		p.Name = draft.Name
	} else if req.Resource == "channel" {
		var c *core.Channel
		if req.Action == "add" {
			p.Channels = append(p.Channels, DefaultChannel(fmt.Sprintf("通道%d", len(p.Channels)+1)))
			c = &p.Channels[len(p.Channels)-1]
		} else {
			c, err = channel(&p, req.Channel)
			if err != nil {
				return fail(err)
			}
		}
		if err = patch("channel", c, req.Fields); err != nil {
			return fail(err)
		}
		result.Channel = c.ID
	} else {
		c, e := channel(&p, req.Channel)
		if e != nil {
			return fail(e)
		}
		if req.Resource == "device" {
			var d *core.Device
			if req.Action == "add" {
				c.Devices = append(c.Devices, core.Device{ID: NewID(), Name: fmt.Sprintf("装置%d", len(c.Devices)+1), Unit: 1, CommonAddress: 1, Points: []core.Point{}})
				d = &c.Devices[len(c.Devices)-1]
			} else {
				d, err = device(c, req.Device)
				if err != nil {
					return fail(err)
				}
			}
			if err = patch("device", d, req.Fields, "pointCount", "pointKind", "pointStart"); err != nil {
				return fail(err)
			}
			if req.Action == "edit" {
				for _, k := range []string{"pointCount", "pointKind", "pointStart"} {
					if _, ok := req.Fields[k]; ok {
						return fail(errors.New("已有装置请在点位页新增点位，不重建原点表"))
					}
				}
			} else {
				n, e := intField(req.Fields, "pointCount", 50)
				if e != nil || n < 0 || n > 10000 {
					return fail(errors.New("点位数量应为 0–10000"))
				}
				kind, start := "yc", 1
				if strings.HasPrefix(c.Protocol, "modbus-") {
					kind, start = "holding", 0
				}
				kind, e = stringField(req.Fields, "pointKind", kind)
				if e != nil {
					return fail(e)
				}
				start, e = intField(req.Fields, "pointStart", start)
				limit := uint64(16777215)
				if strings.HasPrefix(c.Protocol, "modbus-") {
					limit = 65535
				}
				if e != nil || start < 0 || uint64(start) > limit || n > 0 && uint64(start)+uint64(n)-1 > limit {
					return fail(errors.New("起始地址无效"))
				}
				for i := 0; i < n; i++ {
					d.Points = append(d.Points, core.Point{Kind: kind, Address: uint32(start + i), Name: fmt.Sprintf("点位%d", i+1)})
				}
			}
			result.Device = d.ID
		} else {
			d, e := device(c, req.Device)
			if e != nil {
				return fail(e)
			}
			switch req.Resource {
			case "binding":
				if req.Action != "edit" || req.Point == nil || len(req.Fields) != 1 || req.Fields["mappings"] == nil {
					return fail(errors.New("绑定编辑需要指定点位与 mappings"))
				}
				if err = applyPointBindings(*c, d, *req.Point, req.Fields["mappings"]); err != nil {
					return fail(err)
				}
			case "point":
				if req.Action == "add" {
					if _, ok := req.Fields["mappings"]; ok {
						return fail(errors.New("请先新增点位，再在编辑中配置绑定"))
					}
					n, e := intField(req.Fields, "count", 1)
					if e != nil || n < 1 || n > 10000 {
						return fail(errors.New("点位数量应为 1–10000"))
					}
					draft := core.Point{Kind: "yc", Address: 1, Name: "点位"}
					if strings.HasPrefix(c.Protocol, "modbus-") {
						draft.Kind, draft.Address = "holding", 0
					}
					if err = patch("point", &draft, req.Fields, "count"); err != nil {
						return fail(err)
					}
					if uint64(draft.Address)+uint64(n)-1 > uint64(^uint32(0)) {
						return fail(errors.New("点地址超出范围"))
					}
					result.Point = &core.PointKey{Kind: draft.Kind, Address: draft.Address}
					for i := 0; i < n; i++ {
						copy := draft
						copy.Address += uint32(i)
						if n > 1 {
							copy.Name = fmt.Sprintf("%s%d", draft.Name, i+1)
						}
						d.Points = append(d.Points, copy)
					}
				} else {
					if _, ok := req.Fields["count"]; ok {
						return fail(errors.New("编辑不接受点位数量"))
					}
					old, e := point(d, req.Point)
					if e != nil {
						return fail(e)
					}
					original := *old
					draft := *old
					if err = patch("point", &draft, req.Fields, "mappings"); err != nil {
						return fail(err)
					}
					if original.Kind != draft.Kind || original.Address != draft.Address {
						if core.ServerPointReferenced(*d, core.PointKey{Kind: original.Kind, Address: original.Address}) {
							return fail(errors.New("此点被上报规则引用，请先修改规则范围再修改点地址或类型"))
						}
					}
					*old = draft
					result.Point = &core.PointKey{Kind: draft.Kind, Address: draft.Address}
					if raw, ok := req.Fields["mappings"]; ok {
						if *req.Point != *result.Point {
							return fail(errors.New("修改绑定时请保持点类型和地址不变；请分别保存"))
						}
						if err = applyPointBindings(*c, d, *result.Point, raw); err != nil {
							return fail(err)
						}
					}
				}
				if err = core.ValidateServerPointReferences(*c, *d); err != nil {
					return fail(err)
				}
			case "rule":
				cfg := RuleConfiguration(*d)
				if req.Action == "add" {
					cfg.Rules = append(cfg.Rules, core.AutoDataRule{Kind: "yc", Points: "1", IntervalMS: 1000, Mode: "report", Step: 1, Cause: 3})
					index := len(cfg.Rules) - 1
					req.Index = &index
					result.Index = &index
					if strings.HasPrefix(c.Protocol, "modbus-") {
						cfg.Rules[index].Kind = "holding"
						cfg.Rules[index].Mode = "fixed"
						cfg.Rules[index].Points = "0"
					}
				}
				if req.Index == nil || *req.Index < 0 || *req.Index >= len(cfg.Rules) {
					return fail(errors.New("上报规则已变化，请刷新"))
				}
				if err = patch("rule", &cfg.Rules[*req.Index], req.Fields, "stopOnDisconnect"); err != nil {
					return fail(err)
				}
				if raw, ok := req.Fields["stopOnDisconnect"]; ok {
					if err = json.Unmarshal(raw, &cfg.StopOnDisconnect); err != nil {
						return fail(err)
					}
				}
				if d.DataTests == nil {
					d.DataTests = map[string]*core.AutoDataConfig{}
				}
				for _, key := range []string{"yx", "yc", "yx-report", "yc-report"} {
					delete(d.DataTests, key)
				}
				d.AutoData = nil
				d.DataTests["telemetry"] = &cfg
			default:
				return fail(errors.New("不支持的配置资源"))
			}
		}
	}
	p.Version = 2
	if req.Resource == "rule" {
		c, _ := channel(&p, req.Channel)
		d, _ := device(c, req.Device)
		cfg := RuleConfiguration(*d)
		for i, rule := range cfg.Rules {
			if !rule.Enabled {
				continue
			}
			matched := false
			for _, point := range d.Points {
				matched = matched || core.ServerRuleIncludes(c.Protocol, rule, point)
			}
			if !matched {
				return fail(fmt.Errorf("第%d组范围内没有可用的遥测或遥信点位", i+1))
			}
		}
	}
	p, err = core.NormalizeProject(p)
	if err != nil {
		return fail(err)
	}
	return p, result, nil
}
