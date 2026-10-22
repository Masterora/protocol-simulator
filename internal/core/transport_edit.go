package core

import "errors"

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
	if changed || !hasSession {
		delete(e.states, config.ID)
	}
	e.mu.Unlock()
	return nil
}
