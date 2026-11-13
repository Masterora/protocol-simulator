package projectfile

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gytest-go/internal/core"
)

type PointTableImport struct {
	Rows []core.Point `json:"rows"`
	Mode string       `json:"mode"`
	Kind string       `json:"kind"`
}

type PointTableSummary struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Removed int `json:"removed"`
}

func PointTableCandidate(project core.Project, channelID, deviceID string, input PointTableImport) (core.Project, PointTableSummary, error) {
	var summary PointTableSummary
	next, err := core.NormalizeProject(project)
	if err != nil {
		return next, summary, err
	}
	c, err := channel(&next, channelID)
	if err != nil {
		return next, summary, err
	}
	d, err := device(c, deviceID)
	if err != nil {
		return next, summary, err
	}
	points, err := core.BuildPointTable(*c, *d, input.Rows, input.Mode, input.Kind)
	if err != nil {
		return next, summary, err
	}
	previous := make(map[core.PointKey]bool, len(d.Points))
	retained := make(map[core.PointKey]bool, len(points))
	for _, p := range points {
		retained[core.PointKey{Kind: p.Kind, Address: p.Address}] = true
	}
	for _, p := range d.Points {
		key := core.PointKey{Kind: p.Kind, Address: p.Address}
		previous[key] = true
		if !retained[key] {
			if core.ServerPointReferenced(*d, key) {
				return next, summary, fmt.Errorf("点位 %s %d 仍被自动规则引用", p.Kind, p.Address)
			}
			summary.Removed++
		}
	}
	for _, p := range input.Rows {
		if previous[core.PointKey{Kind: p.Kind, Address: p.Address}] {
			summary.Updated++
		} else {
			summary.Added++
		}
	}
	d.Points = points
	// The device list is authoritative, including an intentionally empty table.
	c.Points = nil
	next.Version = 2
	return next, summary, nil
}

func ToolPayload(fields map[string]json.RawMessage, key string, target any) error {
	raw := fields[key]
	if len(fields) != 1 || len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("缺少有效的 %s", key)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}
