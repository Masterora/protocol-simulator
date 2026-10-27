package core

import (
	"errors"
	"fmt"
	"strings"
)

type PointRange struct {
	Selection string `json:"selection,omitempty"`
	Tab       string `json:"tab"`
	Kind      string `json:"kind"`
	Address   uint32 `json:"address"`
	Count     int    `json:"count"`
	Name      string `json:"name"`
}
type DeviceConfiguration struct {
	AutoTests map[string]AutoControlConfiguration `json:"autoTests,omitempty"`
	Relations []TelemetryRelation                 `json:"relations,omitempty"`
	Controls  map[string]ControlSimulation        `json:"controls,omitempty"`
	Version   string                              `json:"version"`
	Values    map[string]string                   `json:"values"`
	Ranges    []PointRange                        `json:"ranges"`
}

func cloneConfiguration(c *DeviceConfiguration) *DeviceConfiguration {
	if c == nil {
		return nil
	}
	v := *c
	v.Relations = cloneRelations(c.Relations)
	v.Ranges = append([]PointRange{}, c.Ranges...)
	v.Values = map[string]string{}
	if c.AutoTests != nil {
		v.AutoTests = map[string]AutoControlConfiguration{}
		for mode, recipe := range c.AutoTests {
			recipe.Rows = append([]AutoControlRow{}, recipe.Rows...)
			v.AutoTests[mode] = recipe
		}
	}
	for k, s := range c.Values {
		v.Values[k] = s
	}
	if c.Controls != nil {
		v.Controls = map[string]ControlSimulation{}
		for k, s := range c.Controls {
			s.Mappings = append([]ControlMapping{}, s.Mappings...)
			v.Controls[k] = s
		}
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
	occupied := map[PointKey]string{}
	for _, r := range c.Ranges {
		if tabs[r.Tab] || !strings.Contains("|yx|yc|mc|yk|jdq|yt|step|position|", "|"+r.Tab+"|") {
			return errors.New("信息体类型重复或无效")
		}
		tabs[r.Tab] = true
		max := uint64(0xffffff)
		if strings.HasPrefix(protocol, "modbus") {
			max = 65535
		}
		width := 1
		if strings.HasPrefix(protocol, "modbus") {
			format, _ := registerFormat(Device{Configuration: c}, r.Tab)
			width = registerWidth(Point{DataType: format})
		}
		points, err := rangePointNumbers(protocol, c, r)
		if err != nil {
			return err
		}
		for _, point := range points {
			if uint64(point)+uint64(width) > max+1 {
				return fmt.Errorf("%s点位超出%d", r.Name, max)
			}
			for offset := 0; offset < width; offset++ {
				key := PointKey{r.Kind, point + uint32(offset)}
				if old, ok := occupied[key]; ok {
					return fmt.Errorf("%s与%s点位范围重叠", r.Name, old)
				}
				occupied[key] = r.Name
			}
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
		points, _ := rangePointNumbers(protocol, c, r)
		for n, pointNumber := range points {
			format, order := registerFormat(Device{Configuration: c}, r.Tab)
			key := PointKey{Kind: r.Kind, Address: pointNumber}
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
		var selected []pointSpan
		if r.Selection != "" {
			var err error
			selected, err = parsePointSpans(r.Selection, 0xffffff)
			if err != nil {
				return false
			}
		}
		count := 0
		first := uint32(0)
		for _, p := range points {
			if p.Kind == r.Kind && (p.Tab == "" || p.Tab == r.Tab) {
				if selected != nil && !containsPoint(selected, p.Address) {
					return false
				}
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
