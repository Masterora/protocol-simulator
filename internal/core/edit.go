package core

import (
	"errors"
	"strings"
	"time"
)

func (e *Engine) ClearChannelMessages(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	kept := e.messages[:0]
	for _, m := range e.messages {
		if m.ChannelID != id {
			kept = append(kept, m)
		}
	}
	e.messages = kept
	e.messageEpoch++
}

func (e *Engine) RenameChannel(id, name string) error {
	if strings.TrimSpace(name) == "" || len([]rune(name)) > 100 {
		return errors.New("名称应为 1–100 字")
	}
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.project.Channels {
		if e.project.Channels[i].ID == id {
			e.project.Channels[i].Name = strings.TrimSpace(name)
			e.dirty = true
			e.markConfigurationLocked()
			return nil
		}
	}
	return errors.New("通道不存在")
}

// PatchPoint applies only edited fields to the latest live point, under the same
// lock as protocol updates. Editing a description never replaces other live values.
func (e *Engine) PatchPoint(id string, key *PointKey, point Point, fields []string) error {
	return e.PatchDevicePoint(id, "", key, point, fields)
}
func (e *Engine) PatchDevicePoint(id, deviceID string, key *PointKey, point Point, fields []string) error {
	return e.patchDevicePoint(id, deviceID, key, point, fields, false)
}
func (e *Engine) patchDevicePoint(id, deviceID string, key *PointKey, point Point, fields []string, disconnect bool) error {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID != id {
			continue
		}
		d, err := deviceIn(c, deviceID)
		if err != nil {
			return err
		}
		points := append([]Point{}, d.Points...)
		editedIndex := -1
		if key == nil {
			points = append(points, point)
		} else {
			index := -1
			for j, p := range points {
				if p.Kind == key.Kind && p.Address == key.Address {
					index = j
					break
				}
			}
			if index < 0 {
				return errors.New("此点已被删除或替换，请重新编辑")
			}
			editedIndex = index
			p := points[index]
			for _, field := range fields {
				switch field {
				case "address":
					p.Address = point.Address
				case "kind":
					p.Kind = point.Kind
					p.Tab = ""
				case "name":
					p.Name = point.Name
				case "value":
					p.Value = point.Value
					p.Updated = time.Now().Format("15:04:05.000")
				case "quality":
					p.Quality = point.Quality
				case "feedback":
					p.Feedback = point.Feedback
				default:
					return errors.New("不支持的点表字段")
				}
			}
			points[index] = p
		}
		if err := validateDevicePointTable(*c, *d, points); err != nil {
			return err
		}
		candidate := *d
		candidate.Points = points
		if err := validateRemoteReferences(*c, candidate); err != nil {
			return err
		}
		if disconnect {
			structural := key == nil
			for _, field := range fields {
				if field == "address" || field == "kind" || field == "feedback" {
					structural = true
				}
			}
			if structural {
				e.disconnectConfigurationLocked(id)
				// Receiving a final frame while stopping must not roll other
				// points, or untouched fields of this point, back to the draft.
				for j, latest := range d.Points {
					if j == editedIndex {
						for _, field := range fields {
							switch field {
							case "address":
								latest.Address = points[j].Address
							case "kind":
								latest.Kind, latest.Tab = points[j].Kind, points[j].Tab
							case "name":
								latest.Name = points[j].Name
							case "value":
								latest.Value, latest.Updated = points[j].Value, points[j].Updated
							case "quality":
								latest.Quality = points[j].Quality
							case "feedback":
								latest.Feedback = points[j].Feedback
							}
						}
						points[j] = latest
					} else {
						points[j] = latest
					}
				}
			}
		}
		d.Points = points
		for _, field := range fields {
			if field == "address" || field == "kind" {
				e.cancelDataLocked(id, d.ID)
				break
			}
		}
		c.syncLegacy()
		e.dirty = true
		e.markConfigurationLocked()
		return nil
	}
	return errors.New("通道不存在")
}

func (e *Engine) DeletePoint(id string, key PointKey) error { return e.DeleteDevicePoint(id, "", key) }
func (e *Engine) DeleteDevicePoint(id, deviceID string, key PointKey) error {
	return e.deleteDevicePoint(id, deviceID, key, false)
}
func (e *Engine) deleteDevicePoint(id, deviceID string, key PointKey, disconnect bool) error {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID != id {
			continue
		}
		d, err := deviceIn(c, deviceID)
		if err != nil {
			return err
		}
		for j, p := range d.Points {
			if p.Kind == key.Kind && p.Address == key.Address {
				next := append([]Point{}, d.Points[:j]...)
				next = append(next, d.Points[j+1:]...)
				candidate := *d
				candidate.Points = next
				if err := validateRemoteReferences(*c, candidate); err != nil {
					return err
				}
				if disconnect {
					e.disconnectConfigurationLocked(id)
					next = append([]Point{}, d.Points[:j]...)
					next = append(next, d.Points[j+1:]...)
				}
				e.cancelDataLocked(id, d.ID)
				d.Points = next
				c.syncLegacy()
				e.dirty = true
				e.markConfigurationLocked()
				return nil
			}
		}
		return errors.New("此点已被删除")
	}
	return errors.New("通道不存在")
}

// Validate before stopping any active transport. Cancelled dialogs and malformed
// project files must leave the current project and its connections untouched.
func (e *Engine) ReplaceProjectAndStop(p Project, path string) error {
	if err := validateProject(&p); err != nil {
		return err
	}
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	ids := make([]string, 0, len(e.running))
	for id := range e.running {
		ids = append(ids, id)
	}
	e.mu.Unlock()
	for _, id := range ids {
		e.stopLocked(id)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.project = cloneProject(p)
	e.markConfigurationLocked()
	e.messageEpoch++
	e.eventEpoch++
	e.cancelDataLocked("", "")
	e.dataJobs = nil
	e.dataGeneration++
	e.resetAutoControls()
	e.path = path
	e.dirty = false
	e.states = map[string]ChannelState{}
	e.messages = []Message{}
	e.events = []PointEvent{}
	return nil
}

func (e *Engine) LoadReplacing(path string) error {
	staged := NewEngine()
	if err := staged.Load(path); err != nil {
		return err
	}
	return e.ReplaceProjectAndStop(staged.Snapshot().Project, path)
}
