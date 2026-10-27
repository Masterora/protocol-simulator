package core

import (
	"errors"
)

// All point-table mutations must obey the same device layout and format rules
// as project loading. Validate a detached candidate before disconnect/commit.
func validateDevicePointTable(c Channel, d Device, points []Point) error {
	d.Points = points
	c.Devices = []Device{d}
	c.syncLegacy()
	return c.Validate()
}

// Configuration saves validate first, then disconnect the affected channel.
// Protocol operations keep using the live setters so simulated values can report.
// Caller holds Engine.op and Engine.mu; stopLocked acquires Engine.mu itself.
func (e *Engine) disconnectConfigurationLocked(channel string) {
	if e.running[channel] == nil {
		return
	}
	e.mu.Unlock()
	e.stopLocked(channel)
	e.mu.Lock()
}

func (e *Engine) ConfigureAutoData(channel, device, category string, c AutoDataConfig) (AutoDataSnapshot, error) {
	return e.saveAutoData(channel, device, category, c, true)
}

func (e *Engine) ConfigureControlSimulation(channel, device, category string, c ControlSimulation) error {
	return e.saveControlSimulation(channel, device, category, c, true)
}

func (e *Engine) ConfigureTelemetryRelations(channel, device string, rows []TelemetryRelation) error {
	return e.saveTelemetryRelations(channel, device, rows, true)
}

func (e *Engine) ConfigurePointCell(channel, device string, key PointKey, field string, p Point) (PointValueResult, error) {
	return e.editPointCell(channel, device, key, field, p, false, true)
}

func (e *Engine) ConfigureDevicePoint(channel, device string, key *PointKey, p Point, fields []string) error {
	return e.patchDevicePoint(channel, device, key, p, fields, true)
}

func (e *Engine) ConfigureDevicePoints(channel, device string, points []Point) error {
	return e.setDevicePoints(channel, device, points, true)
}

func (e *Engine) ConfigureDeleteDevicePoint(channel, device string, key PointKey) error {
	return e.deleteDevicePoint(channel, device, key, true)
}

func (e *Engine) ConfigureDeleteDevice(channel, device string) error {
	return e.deleteDevice(channel, device, true)
}

func cloneTransportDraft(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copy := make(map[string]string, len(values))
	for k, v := range values {
		copy[k] = v
	}
	return copy
}

func mergeTransport(current *Channel, config Channel) {
	current.Transport = config.Transport
	current.Address = config.Address
	current.LocalAddress = config.LocalAddress
	current.Baud = config.Baud
	current.DataBits = config.DataBits
	current.Parity = config.Parity
	current.StopBits = config.StopBits
	current.TransportDraft = cloneTransportDraft(config.TransportDraft)
	current.Defaults()
}
func sameTransport(a, b Channel) bool {
	return a.Transport == b.Transport && a.Address == b.Address && a.LocalAddress == b.LocalAddress && a.Baud == b.Baud && a.DataBits == b.DataBits && a.Parity == b.Parity && a.StopBits == b.StopBits
}

// Validate before touching the live session. Merge communication fields again
// after stopping so late protocol updates cannot be overwritten by editor data.
// Changed settings leave the channel stopped until the user connects manually.
func (e *Engine) UpdateTransport(config Channel) error {
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	index := -1
	for i, c := range e.project.Channels {
		if c.ID == config.ID {
			index = i
			break
		}
	}
	if index < 0 {
		e.mu.Unlock()
		return errors.New("通道已被移除")
	}
	before := e.project.Channels[index]
	candidate := cloneProject(Project{Channels: []Channel{before}}).Channels[0]
	mergeTransport(&candidate, config)
	if err := candidate.Validate(); err != nil {
		e.mu.Unlock()
		return err
	}
	hasSession := e.running[config.ID] != nil
	changed := !sameTransport(before, candidate)
	if changed && hasSession {
		e.mu.Unlock()
		e.stopLocked(config.ID)
		e.mu.Lock()
	}
	mergeTransport(&e.project.Channels[index], candidate)
	e.dirty = true
	e.markConfigurationLocked()
	if changed || !hasSession {
		delete(e.states, config.ID)
	}
	e.mu.Unlock()
	return nil
}

// EnsureWorkspaceChannel prepares the selected station without connecting it.
// Existing channels, including those loaded from a project, are left intact.
func (e *Engine) EnsureWorkspaceChannel(role string) error {
	if role != "client" && role != "server" {
		return errors.New("无效工作模式")
	}
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.project.Channels {
		if c.Role == role {
			return nil
		}
	}
	if len(e.project.Channels) >= 64 {
		return errors.New("最多支持 64 个通道")
	}
	c := Channel{ID: newID(), Name: "通道1", Protocol: "raw", Role: role, Transport: "tcp-client", Address: "127.0.0.1:2404"}
	if role == "server" {
		c.Transport, c.Address = "tcp-server", "0.0.0.0:2404"
	}
	c.Defaults()
	if err := c.Validate(); err != nil {
		return err
	}
	if len(e.project.Channels) > 0 || e.path != "" {
		e.dirty = true
	}
	e.project.Channels = append(e.project.Channels, c)
	e.markConfigurationLocked()
	return nil
}
