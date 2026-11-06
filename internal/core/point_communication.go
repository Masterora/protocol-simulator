package core

import (
	"encoding/binary"
	"time"
)

// Communication times belong to this engine's runtime, never to the saved
// project or an IEC104 source/event timestamp.
type pointCommunicationKey struct {
	channel, device string
	point           PointKey
}

func (e *Engine) communicationPointLocked(channel, device string, point Point) Point {
	point.CommunicationAt = e.pointCommunications[pointCommunicationKey{channel, device, PointKey{point.Kind, point.Address}}]
	return point
}

func (e *Engine) prunePointCommunicationsLocked() {
	if len(e.pointCommunications) == 0 {
		return
	}
	valid := make(map[pointCommunicationKey]bool)
	for _, c := range e.project.Channels {
		for _, d := range c.Devices {
			for _, p := range d.Points {
				valid[pointCommunicationKey{c.ID, d.ID, PointKey{p.Kind, p.Address}}] = true
			}
		}
	}
	for key := range e.pointCommunications {
		if !valid[key] {
			delete(e.pointCommunications, key)
		}
	}
}

// Called after a full write, or after a received frame has been decoded. The
// session identity prevents late frames from updating a replacement project.
func (e *Engine) recordPointCommunication(s *session, device string, keys map[PointKey]bool) {
	if len(keys) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.running[s.cfg.ID] != s {
		return
	}
	for ci := range e.project.Channels {
		c := &e.project.Channels[ci]
		if c.ID != s.cfg.ID {
			continue
		}
		d, err := deviceIn(c, device)
		if err != nil {
			return
		}
		stamp := time.Now().Format(time.RFC3339Nano)
		if e.pointCommunications == nil {
			e.pointCommunications = make(map[pointCommunicationKey]string)
		}
		var changed []Point
		for _, p := range d.Points {
			key := PointKey{p.Kind, p.Address}
			if keys[key] {
				e.pointCommunications[pointCommunicationKey{c.ID, d.ID, key}] = stamp
				changed = append(changed, p)
			}
		}
		e.markPointsLocked(c.ID, d.ID, changed)
		return
	}
}

func (p *peer) record104Communication(asdu []byte) {
	if len(asdu) < 6 {
		return
	}
	ca := binary.LittleEndian.Uint16(asdu[4:])
	for _, d := range p.s.e.devices(p.s.cfg.ID) {
		if d.CommonAddress != ca {
			continue
		}
		keys := make(map[PointKey]bool)
		if controlKind(asdu[0]) != "" {
			r, err := decodeControl104(asdu)
			if err != nil {
				return
			}
			kind := controlKind(r.Type)
			keys[PointKey{kind, r.Address}] = true
			if kind == "step" {
				keys[PointKey{"yt", r.Address}] = true
			}
		} else {
			points, err := Decode104Points(asdu, ca)
			if err != nil {
				return
			}
			for _, point := range points {
				keys[PointKey{point.Kind, point.Address}] = true
			}
		}
		p.s.e.recordPointCommunication(p.s, d.ID, keys)
		return
	}
}

func (p *peer) recordModbusCommunication(device string, request []byte) {
	if len(request) < 5 {
		return
	}
	kind := map[byte]string{1: "coil", 2: "discrete", 3: "holding", 4: "input", 5: "coil", 6: "holding", 15: "coil", 16: "holding"}[request[0]]
	if kind == "" {
		return
	}
	start := uint32(binary.BigEndian.Uint16(request[1:3]))
	count := uint32(binary.BigEndian.Uint16(request[3:5]))
	if request[0] == 5 || request[0] == 6 {
		count = 1
	}
	if count == 0 || start+count > 65536 {
		return
	}
	d, err := p.s.e.Device(p.s.cfg.ID, device)
	if err != nil {
		return
	}
	keys := make(map[PointKey]bool)
	for _, point := range d.Points {
		width := uint32(1)
		if kind == "holding" || kind == "input" {
			width = uint32(registerWidth(point))
		}
		if point.Kind == kind && point.Address >= start && point.Address+width <= start+count {
			keys[PointKey{point.Kind, point.Address}] = true
		}
	}
	p.s.e.recordPointCommunication(p.s, device, keys)
}
