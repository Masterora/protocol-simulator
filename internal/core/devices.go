package core

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"
)

func deviceIn(c *Channel, id string) (*Device, error) {
	for i := range c.Devices {
		if id == "" || c.Devices[i].ID == id {
			return &c.Devices[i], nil
		}
	}
	return nil, errors.New("装置不存在，请重新选择")
}
func (e *Engine) Device(channelID, deviceID string) (Device, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.project.Channels {
		if e.project.Channels[i].ID == channelID {
			d, err := deviceIn(&e.project.Channels[i], deviceID)
			if err != nil {
				return Device{}, err
			}
			copy := *d
			copy.Points = append([]Point{}, d.Points...)
			copy.Configuration = cloneConfiguration(d.Configuration)
			copy.AutoData = cloneAutoData(d.AutoData)
			return copy, nil
		}
	}
	return Device{}, errors.New("通道不存在")
}
func (e *Engine) devices(channelID string) []Device {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.project.Channels {
		if c.ID == channelID {
			list := append([]Device{}, c.Devices...)
			for i := range list {
				list[i].Points = nil
				list[i].AutoData = cloneAutoData(list[i].AutoData)
			}
			return list
		}
	}
	return nil
}

// Devices on a channel share a protocol and transport. Address-only routing is
// ambiguous when two devices share an address, so validation rejects duplicates.
func (e *Engine) UpsertDevice(channelID, protocol string, d Device, replace bool) (string, error) {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.project.Channels {
		existing := &e.project.Channels[i]
		if existing.ID != channelID {
			continue
		}
		c := cloneProject(Project{Channels: []Channel{*existing}}).Channels[0]
		replace104 := protocol == "iec104" && c.Protocol == "iec104" && d.ID == "" && len(c.Devices) > 0
		if replace104 {
			if !replace {
				return "", errors.New("再次增加 IEC104 装置需要确认替换原装置")
			}
			c.Devices = []Device{}
		}
		if protocol != c.Protocol {
			if len(c.Devices) > 0 && !replace {
				return "", errors.New("切换规约需要确认移除原有装置")
			}
			c.Protocol = protocol
			c.Devices = []Device{}

		}
		if protocol == "raw" {
			return "", errors.New("请为装置选择规约")
		}
		d.Name = strings.TrimSpace(d.Name)
		d.Configuration = cloneConfiguration(d.Configuration)
		d.AutoData = cloneAutoData(d.AutoData)
		if d.ID == "" {
			d.ID = newID()
			points, err := configuredPoints(protocol, d.Configuration, nil)
			if err != nil {
				return "", err
			}
			d.Points = points
			c.Devices = append(c.Devices, d)
		} else {
			old, err := deviceIn(&c, d.ID)
			if err != nil {
				return "", err
			}
			if d.AutoData == nil {
				d.AutoData = cloneAutoData(old.AutoData)
			}
			if d.Configuration == nil {
				d.Configuration = cloneConfiguration(old.Configuration)
				d.Points = old.Points
			} else {
				var points []Point
				var err error
				if old.Configuration != nil && reflect.DeepEqual(d.Configuration.Ranges, old.Configuration.Ranges) || old.Configuration == nil && rangesDescribeExisting(d.Configuration.Ranges, old.Points) {
					points = append([]Point{}, old.Points...)
					for j := range points {
						if points[j].Tab == "" {
							for _, r := range d.Configuration.Ranges {
								if r.Kind == points[j].Kind {
									points[j].Tab = r.Tab
									break
								}
							}
						}
						if protocol == "iec104" {
							points[j].WireType, _ = configured104Type(d, points[j].Kind)
						} else {
							points[j].DataType, points[j].ByteOrder = registerFormat(d, points[j].Tab)
						}
					}
				} else {
					points, err = configuredPoints(protocol, d.Configuration, old.Points)
				}
				if err != nil {
					return "", err
				}
				d.Points = points
			}
			*old = d
		}
		c.syncLegacy()
		if err := c.Validate(); err != nil {
			return "", fmt.Errorf("装置配置无效（请同时检查通信配置）：%w", err)
		}
		configurationChanged := d.ID != "" && !reflect.DeepEqual(existing.Devices, c.Devices)
		if (replace104 || existing.Protocol != c.Protocol || configurationChanged) && e.running[channelID] != nil {
			e.mu.Unlock()
			e.stopLocked(channelID)
			e.mu.Lock()
		}
		if existing.Protocol != c.Protocol || replace104 {
			delete(e.states, channelID)
		}
		*existing = c
		e.cancelDataLocked(channelID, d.ID)
		e.project.Version = 2
		e.dirty = true
		return d.ID, nil
	}
	return "", errors.New("通道不存在")
}
func (e *Engine) DeleteDevice(channelID, deviceID string) error {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID != channelID {
			continue
		}
		for j, d := range c.Devices {
			if d.ID == deviceID {
				e.cancelDataLocked(channelID, deviceID)
				last := len(c.Devices) == 1
				if last && e.running[channelID] != nil {
					e.mu.Unlock()
					e.stopLocked(channelID)
					e.mu.Lock()
				}
				c.Devices = append(c.Devices[:j], c.Devices[j+1:]...)
				if last {
					c.Protocol = "raw"
				}
				c.syncLegacy()
				e.dirty = true
				return nil
			}
		}
		return errors.New("装置不存在")
	}
	return errors.New("通道不存在")
}
func (e *Engine) RenameDevice(channelID, deviceID, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 100 {
		return errors.New("名称应为 1–100 字")
	}
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID != channelID {
			continue
		}
		d, err := deviceIn(c, deviceID)
		if err != nil {
			return err
		}
		d.Name = name
		e.dirty = true
		return nil
	}
	return errors.New("通道不存在")
}
func (e *Engine) SetDevicePoints(channelID, deviceID string, points []Point) error {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID != channelID {
			continue
		}
		d, err := deviceIn(c, deviceID)
		if err != nil {
			return err
		}
		if err = validatePoints(points, c.Protocol); err != nil {
			return err
		}
		e.cancelDataLocked(channelID, d.ID)
		d.Points = append([]Point{}, points...)
		c.syncLegacy()
		e.dirty = true
		return nil
	}
	return errors.New("通道不存在")
}
func (e *Engine) applyDevicePoints(channelID, deviceID string, updates []Point) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID != channelID {
			continue
		}
		d, err := deviceIn(c, deviceID)
		if err != nil {
			return
		}
		for _, u := range updates {
			u.Updated = time.Now().Format("15:04:05.000")
			if u.Timestamp != "" {
				u.Updated = u.Timestamp
			}
			found := false
			for j := range d.Points {
				p := &d.Points[j]
				if p.Address == u.Address && p.Kind == u.Kind {
					p.Value = u.Value
					p.Quality = u.Quality
					p.Updated = u.Updated
					p.Timestamp = u.Timestamp
					p.TimeInvalid = u.TimeInvalid
					if u.WireType != 0 {
						p.WireType = u.WireType
					}
					found = true
					break
				}
			}
			if !found && len(d.Points) < 10000 {
				u.Name = fmt.Sprintf("点 %d", u.Address)
				d.Points = append(d.Points, u)
			}
		}
		c.syncLegacy()

		return
	}
}
func (e *Engine) ReadDeviceModbus(channelID, deviceID string, fc byte, start, count uint16) error {
	s, err := e.getSession(channelID)
	if err != nil {
		return err
	}
	d, err := e.Device(channelID, deviceID)
	if err != nil {
		return err
	}
	pdu, err := ReadPDU(fc, start, count)
	if err != nil {
		return err
	}
	return s.requestDeviceModbus(d, pdu)
}
func (e *Engine) WriteDeviceModbus(channelID, deviceID string, fc byte, address, value uint16) error {
	s, err := e.getSession(channelID)
	if err != nil {
		return err
	}
	d, err := e.Device(channelID, deviceID)
	if err != nil {
		return err
	}
	pdu, err := WritePDU(fc, address, value)
	if err != nil {
		return err
	}
	return s.requestDeviceModbus(d, pdu)
}
func (e *Engine) InterrogateDevice(channelID, deviceID string) error {
	s, err := e.getSession(channelID)
	if err != nil {
		return err
	}
	d, err := e.Device(channelID, deviceID)
	if err != nil {
		return err
	}
	if s.cfg.Protocol != "iec104" || s.cfg.Role != "client" {
		return errors.New("总召唤仅适用于 104 主站")
	}
	return s.forPeers(func(p *peer) error { return p.sendI(interrogation(d.CommonAddress, 6), "总召唤 · "+d.Name) })
}
func (e *Engine) PublishDevice(channelID, deviceID string) error {
	s, err := e.getSession(channelID)
	if err != nil {
		return err
	}
	d, err := e.Device(channelID, deviceID)
	if err != nil {
		return err
	}
	if s.cfg.Protocol != "iec104" || s.cfg.Role != "server" {
		return errors.New("主动上送仅适用于 104 子站")
	}
	return s.forPeers(func(p *peer) error {
		points := []Point{}
		for _, point := range d.Points {
			if point.Kind == "yx" || point.Kind == "yc" || point.Kind == "mc" || point.Kind == "position" {
				points = append(points, point)
			}
		}
		return p.publishPoints(d, points, 3)
	})
}
