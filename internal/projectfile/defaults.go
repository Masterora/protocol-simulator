package projectfile

import (
	"errors"
	"fmt"
	"gytest-go/internal/core"
	"net"
	"strconv"
	"strings"
)

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
		if c.Protocol == "raw" {
			return v, errors.New("RAW 通道不包含装置")
		}
		if c.Protocol == "iec104" && len(c.Devices) > 0 {
			return v, errors.New("IEC104 每通道一个装置，请新增通道")
		}
		units, addresses := map[int]bool{}, map[int]bool{}
		for _, d := range c.Devices {
			units[int(d.Unit)] = true
			addresses[int(d.CommonAddress)] = true
		}
		unit, ca := 1, 1
		for units[unit] {
			unit++
		}
		for addresses[ca] {
			ca++
		}
		kind, start := "yc", 1
		if strings.HasPrefix(c.Protocol, "modbus-") {
			kind, start = "holding", 0
		}
		v.Value = Fields(map[string]any{"name": fmt.Sprintf("装置%d", len(c.Devices)+1), "unit": unit, "commonAddress": ca, "pointCount": 50, "pointKind": kind, "pointStart": start})
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
