package projectfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"gytest-go/internal/core"
	"net"
	"strconv"
	"strings"
)

const DefaultDevicePointCount = 50

func DevicePointKinds(protocol string) []string {
	if strings.HasPrefix(protocol, "modbus-") {
		return []string{"discrete", "holding", "coil", "input"}
	}
	return []string{"yx", "yc", "yk", "yt", "mc", "position", "step"}
}

func DevicePointStart(protocol, kind string) uint32 {
	if strings.HasPrefix(protocol, "modbus-") {
		switch kind {
		case "discrete":
			return 1
		case "holding":
			return 1024
		case "coil":
			return 2560
		}
		return 0
	}
	switch kind {
	case "yx":
		return 1
	case "yc":
		return 16385
	case "yk":
		return 24577
	case "step":
		return 24833
	case "yt":
		return 25089
	case "mc":
		return 25601
	case "position":
		return 4097
	}
	return 0
}

func initialDevicePoints(protocol string, raw json.RawMessage) ([]core.Point, error) {
	var selections map[string]string
	if err := json.Unmarshal(raw, &selections); err != nil || selections == nil {
		return nil, errors.New("初始点表应为点位类型和范围组成的 JSON 对象")
	}
	kinds := DevicePointKinds(protocol)
	maxAddress := uint32(0xffffff)
	if strings.HasPrefix(protocol, "modbus-") {
		maxAddress = 65535
	}
	points := []core.Point{}
	for _, kind := range kinds {
		selection := strings.TrimSpace(selections[kind])
		delete(selections, kind)
		if selection == "" {
			continue
		}
		addresses, err := core.ParsePointSelection(selection, maxAddress)
		if err != nil {
			return nil, fmt.Errorf("初始点表（%s）：%w", kind, err)
		}
		if len(points)+len(addresses) > 10000 {
			return nil, errors.New("各类型合计最多 10000 个初始点位")
		}
		for _, address := range addresses {
			points = append(points, core.Point{Kind: kind, Address: address, Name: fmt.Sprintf("点位%d", len(points)+1)})
		}
	}
	if len(selections) != 0 {
		return nil, errors.New("初始点表含当前协议不支持的点位类型")
	}
	return points, nil
}

func defaultDevice(c *core.Channel) (core.Device, error) {
	if c.Protocol == "iec104" && len(c.Devices) > 0 {
		return core.Device{}, errors.New("此 IEC104 通道已有装置。编辑现有装置，或新增通道后再添加装置")
	}
	names, units := map[string]bool{}, map[uint8]bool{}
	for _, d := range c.Devices {
		names[d.Name], units[d.Unit] = true, true
	}
	n := 1
	for names[fmt.Sprintf("装置%d", n)] {
		n++
	}
	limit := 255
	if c.Protocol == "modbus-rtu" {
		limit = 247
	}
	unit := 1
	for unit <= limit && units[uint8(unit)] {
		unit++
	}
	if unit > limit {
		if c.Protocol == "modbus-rtu" && c.Role == "server" || units[0] {
			return core.Device{}, errors.New("通道已无可用站号，请选择其他通道")
		}
		unit = 0
	}
	return core.Device{Name: fmt.Sprintf("装置%d", n), Unit: uint8(unit), CommonAddress: 1, Points: []core.Point{}}, nil
}

func DefaultView(doc Document, req Request) (View, error) {
	v := View{Path: doc.Path, Revision: doc.Revision, Resource: req.Resource}
	if req.Resource == "channel" {
		v.LocalAddresses = core.LocalIPv4()
		names, ports := map[string]bool{}, map[int]bool{}
		for _, c := range doc.Project.Channels {
			names[c.Name] = true
			_, port, _ := net.SplitHostPort(c.Address)
			n, _ := strconv.Atoi(port)
			ports[n] = true
		}
		n := 1
		for names[fmt.Sprintf("通道%d", n)] {
			n++
		}
		port := 2404
		for ports[port] && port < 65535 {
			port++
		}
		c := DefaultChannel(fmt.Sprintf("通道%d", n))
		host, _, _ := net.SplitHostPort(c.Address)
		c.Address = net.JoinHostPort(host, strconv.Itoa(port))
		c.Points = nil
		c.Devices = nil
		v.Value = Object(c)
		return v, nil
	}
	c, err := channel(&doc.Project, req.Channel)
	if err != nil {
		return v, err
	}
	v.Protocol = c.Protocol
	if req.Resource == "device" {
		d, err := defaultDevice(c)
		if err != nil {
			return v, err
		}
		kind := "yc"
		if strings.HasPrefix(c.Protocol, "modbus-") {
			kind = "holding"
		}
		start := DevicePointStart(c.Protocol, kind)
		initialPoints := map[string]string{}
		for _, pointKind := range DevicePointKinds(c.Protocol) {
			first := DevicePointStart(c.Protocol, pointKind)
			initialPoints[pointKind] = fmt.Sprintf("%d-%d", first, first+DefaultDevicePointCount-1)
		}
		v.Value = Fields(map[string]any{"name": d.Name, "unit": d.Unit, "commonAddress": d.CommonAddress, "pointCount": DefaultDevicePointCount, "pointKind": kind, "pointStart": start,
			"initialPoints": initialPoints})
		return v, nil
	}
	d, err := device(c, req.Device)
	if err != nil {
		return v, err
	}
	switch req.Resource {
	case "point":
		kind, start := "yc", 1
		if strings.HasPrefix(c.Protocol, "modbus-") {
			kind, start = "holding", 0
		}
		if req.Point != nil && req.Point.Kind != "" {
			kind = req.Point.Kind
		}
		used := map[uint32]bool{}
		for _, p := range d.Points {
			if p.Kind == kind {
				used[p.Address] = true
			}
		}
		for used[uint32(start)] {
			start++
		}
		v.Value = Object(core.Point{Kind: kind, Address: uint32(start), Name: "点位"})
		v.Value["count"] = []byte("1")
	case "rule":
		rule := core.AutoDataRule{Kind: "yc", Points: "1", IntervalMS: 1000, Mode: "report", Step: 1, Cause: 3}
		if strings.HasPrefix(c.Protocol, "modbus-") {
			rule.Kind, rule.Points, rule.Mode = "holding", "0", "fixed"
		}
		for _, p := range d.Points {
			candidate := rule
			candidate.Kind = p.Kind
			candidate.Points = fmt.Sprint(p.Address)
			if core.ServerRuleIncludes(c.Protocol, candidate, p) {
				rule = candidate
				break
			}
		}
		v.Value = Object(rule)
		v.Value["stopOnDisconnect"], _ = jsonMarshalBool(RuleConfiguration(*d).StopOnDisconnect)
	default:
		return v, errors.New("不支持新增此类资源")
	}
	return v, nil
}
func jsonMarshalBool(value bool) ([]byte, error) {
	if value {
		return []byte("true"), nil
	}
	return []byte("false"), nil
}
