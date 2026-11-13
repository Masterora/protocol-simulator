package core

import "errors"

type PointTableView struct {
	Revision uint64  `json:"revision"`
	Protocol string  `json:"protocol"`
	Name     string  `json:"name"`
	Points   []Point `json:"points"`
}

func (e *Engine) PointTable(channel, device string) (PointTableView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.project.Channels {
		if c.ID == channel {
			d, err := deviceIn(&c, device)
			if err != nil {
				return PointTableView{}, err
			}
			return PointTableView{e.viewMetadata, c.Protocol, d.Name, append([]Point{}, d.Points...)}, nil
		}
	}
	return PointTableView{}, errors.New("通道不存在")
}
func BuildPointTable(c Channel, d Device, rows []Point, mode, kind string) ([]Point, error) {
	if len(rows) == 0 || len(rows) > 100000 {
		return nil, errors.New("导入点位数量应为1–100000")
	}
	var points []Point
	switch mode {
	case "merge":
		points = append([]Point{}, d.Points...)
	case "replace-kind":
		if kind == "" {
			return nil, errors.New("请选择替换的点位类型")
		}
		for _, p := range d.Points {
			if p.Kind != kind {
				points = append(points, p)
			}
		}
		for _, p := range rows {
			if p.Kind != kind {
				return nil, errors.New("导入表含其他类型，不能替换所选类型")
			}
		}
	case "replace-all":
	default:
		return nil, errors.New("请选择合并、替换所选类型或替换全部点表")
	}
	indices := map[PointKey]int{}
	for i, p := range points {
		indices[PointKey{p.Kind, p.Address}] = i
	}
	seen := map[PointKey]bool{}
	for _, p := range rows {
		key := PointKey{p.Kind, p.Address}
		if seen[key] {
			return nil, errors.New("导入点表含重复点位")
		}
		seen[key] = true
		if j, ok := indices[key]; ok {
			points[j] = p
		} else {
			indices[key] = len(points)
			points = append(points, p)
		}
	}
	if err := validateDevicePointTable(c, d, points); err != nil {
		return nil, err
	}
	candidate := d
	candidate.Points = points
	if err := validateRemoteReferences(c, candidate); err != nil {
		return nil, err
	}
	return points, nil
}
func (e *Engine) CheckPointTable(channel, device string, revision uint64, rows []Point, mode, kind string) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if revision != e.viewMetadata {
		return 0, errors.New("工程配置已变化，请重新预览点表")
	}
	for _, c := range e.project.Channels {
		if c.ID != channel {
			continue
		}
		d, err := deviceIn(&c, device)
		if err != nil {
			return 0, err
		}
		points, err := BuildPointTable(c, *d, rows, mode, kind)
		if err != nil {
			return 0, err
		}
		keys := map[PointKey]bool{}
		for _, p := range points {
			keys[PointKey{p.Kind, p.Address}] = true
		}
		removed := 0
		for _, p := range d.Points {
			if !keys[PointKey{p.Kind, p.Address}] {
				removed++
			}
		}
		return removed, nil
	}
	return 0, errors.New("通道不存在")
}

// Only a validated, revision-matching candidate may stop and change the target
// channel. The normal Save command atomically writes the resulting project.
func (e *Engine) ImportPointTable(channel, device string, revision uint64, rows []Point, mode, kind string) error {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if revision != e.viewMetadata {
		return errors.New("工程配置已变化，请重新预览点表")
	}
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID != channel {
			continue
		}
		d, err := deviceIn(c, device)
		if err != nil {
			return err
		}
		points, err := BuildPointTable(*c, *d, rows, mode, kind)
		if err != nil {
			return err
		}
		e.disconnectConfigurationLocked(channel)
		// In-flight automatic updates can finish while the channel stops.
		// Preserve the latest untouched points rather than rebasing them to
		// the earlier preview candidate.
		imported := map[PointKey]bool{}
		for _, p := range rows {
			imported[PointKey{p.Kind, p.Address}] = true
		}
		latest := map[PointKey]Point{}
		for _, p := range d.Points {
			latest[PointKey{p.Kind, p.Address}] = p
		}
		for i, p := range points {
			key := PointKey{p.Kind, p.Address}
			if !imported[key] {
				if current, ok := latest[key]; ok {
					points[i] = current
				}
			}
		}
		e.cancelDataLocked(channel, d.ID)
		d.Points = points
		c.syncLegacy()
		e.dirty = true
		e.markConfigurationLocked()
		return nil
	}
	return errors.New("通道不存在")
}
