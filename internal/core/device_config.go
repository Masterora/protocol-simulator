package core

import (
	"errors"
	"fmt"
	"strings"
)

type PointRange struct {
	Tab     string `json:"tab"`
	Kind    string `json:"kind"`
	Address uint32 `json:"address"`
	Count   int    `json:"count"`
	Name    string `json:"name"`
}
type DeviceConfiguration struct {
	Version string            `json:"version"`
	Values  map[string]string `json:"values"`
	Ranges  []PointRange      `json:"ranges"`
}

func cloneConfiguration(c *DeviceConfiguration) *DeviceConfiguration {
	if c == nil {
		return nil
	}
	v := *c
	v.Ranges = append([]PointRange{}, c.Ranges...)
	v.Values = map[string]string{}
	for k, s := range c.Values {
		v.Values[k] = s
	}
	return &v
}
func validateConfiguration(protocol string, c *DeviceConfiguration) error {
	if c == nil {
		return nil
	}
	expected := "标准版"
	if protocol == "modbus-tcp" {
		expected = "TCP版"
	} else if protocol == "modbus-rtu" {
		expected = "标准版(RTU)"
	}
	if c.Version != expected {
		return errors.New("当前尚未实现此规约版本")
	}
	if len(c.Values) > 100 || len(c.Ranges) > 9 {
		return errors.New("装置配置项超出范围")
	}
	if err := validateSettings(protocol, c); err != nil {
		return err
	}
	total := 0
	tabs := map[string]bool{}
	for _, r := range c.Ranges {
		if tabs[r.Tab] || !strings.Contains("|yx|yc|mc|yk|jdq|yt|step|position|", "|"+r.Tab+"|") {
			return errors.New("信息体类型重复或无效")
		}
		tabs[r.Tab] = true
		max := uint64(0xffffff)
		if strings.HasPrefix(protocol, "modbus") {
			max = 65535
		}
		if r.Count < 0 || r.Count > 10000 || uint64(r.Address) > max || uint64(r.Address)+uint64(r.Count) > max+1 {
			return errors.New("信息体首地址或个数超出范围")
		}
		total += r.Count
		if err := validatePoints([]Point{{Kind: r.Kind, Address: r.Address}}, protocol); err != nil {
			return err
		}
	}
	if total > 10000 {
		return errors.New("每个装置最多 10000 个点")
	}
	return nil
}
func configuredPoints(protocol string, c *DeviceConfiguration, existing []Point) ([]Point, error) {
	if err := validateConfiguration(protocol, c); err != nil {
		return nil, err
	}
	if c == nil {
		return append([]Point{}, existing...), nil
	}
	old := map[PointKey]Point{}
	for _, p := range existing {
		old[PointKey{Kind: p.Kind, Address: p.Address}] = p
	}
	seen := map[PointKey]bool{}
	result := []Point{}
	for _, r := range c.Ranges {
		for n := 0; n < r.Count; n++ {
			format, order := registerFormat(Device{Configuration: c}, r.Tab)
			width := 1
			if strings.HasPrefix(protocol, "modbus") {
				width = registerWidth(Point{DataType: format})
			}
			key := PointKey{Kind: r.Kind, Address: r.Address + uint32(n*width)}
			if seen[key] {
				continue
			}
			seen[key] = true
			p, ok := old[key]
			if !ok {
				p = Point{Address: key.Address, Kind: key.Kind, Name: fmt.Sprintf("%s%d", r.Name, n+1)}
			}
			p.Tab = r.Tab
			if protocol == "iec104" {
				typ, err := configured104Type(Device{Configuration: c}, r.Kind)
				if err != nil {
					return nil, err
				}
				p.WireType = typ
			} else {
				p.DataType, p.ByteOrder = format, order
			}
			result = append(result, p)
		}
	}
	for _, p := range existing {
		represented := false
		for _, r := range c.Ranges {
			if r.Kind == p.Kind {
				represented = true
				break
			}
		}
		if !represented {
			result = append(result, p)
		}
	}
	return result, validatePoints(result, protocol)
}

// Legacy/imported point lists can be sparse and include manual control points.
// A settings-only edit must not regenerate a dense table over that data.
func rangesDescribeExisting(ranges []PointRange, points []Point) bool {
	for _, r := range ranges {
		count := 0
		first := uint32(0)
		for _, p := range points {
			if p.Kind == r.Kind && (p.Tab == "" || p.Tab == r.Tab) {
				if count == 0 || p.Address < first {
					first = p.Address
				}
				count++
			}
		}
		if count != r.Count || count > 0 && first != r.Address {
			return false
		}
	}
	return true
}
