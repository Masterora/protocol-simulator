package core

import (
	"fmt"
	"sort"
	"strings"
)

// InspectRequest is read-only and deliberately bounded. It never copies an
// entire project's point tables to support a single terminal page.
type InspectRequest struct {
	Channel  string `json:"channel"`
	Device   string `json:"device"`
	Filter   string `json:"filter"`
	Offset   int    `json:"offset"`
	Kind     string `json:"kind"`
	Order    string `json:"order"`
	After    uint64 `json:"after"`
	Messages bool   `json:"messages"`
}
type InspectChannel struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Protocol string       `json:"protocol"`
	Role     string       `json:"role"`
	Address  string       `json:"address"`
	Devices  int          `json:"devices"`
	State    ChannelState `json:"state"`
}
type InspectDevice struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Unit          byte   `json:"unit"`
	CommonAddress uint16 `json:"commonAddress"`
	Points        int    `json:"points"`
}
type Inspection struct {
	Metadata     uint64            `json:"metadata"`
	Name         string            `json:"name"`
	Path         string            `json:"path"`
	Channels     []InspectChannel  `json:"channels"`
	Devices      []InspectDevice   `json:"devices"`
	Points       []Point           `json:"points"`
	PointTotal   int               `json:"pointTotal"`
	PointMatches int               `json:"pointMatches"`
	PointKinds   map[string]int    `json:"pointKinds,omitempty"`
	PointOrder   string            `json:"pointOrder,omitempty"`
	Rules        *AutoDataSnapshot `json:"rules,omitempty"`
	Messages     []Message         `json:"messages"`
	Sequence     uint64            `json:"sequence"`
	Missed       uint64            `json:"missed"`
}

func (e *Engine) Inspect(request InspectRequest) Inspection {
	e.mu.Lock()
	transfers := map[string]*session{}
	v := Inspection{Metadata: e.viewMetadata, Name: e.project.Name, Path: e.path, Sequence: e.sequence}
	if request.Order == "address" {
		v.PointOrder = "address"
	}
	filter := strings.ToLower(request.Filter)
	offset := max(0, request.Offset)
	for _, c := range e.project.Channels {
		state, ok := e.states[c.ID]
		if !ok {
			state = ChannelState{ID: c.ID, Address: c.Address, Status: "stopped"}
		}
		if c.Protocol == "iec104" {
			transfers[c.ID] = e.running[c.ID]
		} else {
			state.ReadyPeers = state.Peers
		}
		v.Channels = append(v.Channels, InspectChannel{c.ID, c.Name, c.Protocol, c.Role, c.Address, len(c.Devices), state})
		if c.ID != request.Channel {
			continue
		}
		for _, d := range c.Devices {
			v.Devices = append(v.Devices, InspectDevice{d.ID, d.Name, d.Unit, d.CommonAddress, len(d.Points)})
			if d.ID != request.Device {
				continue
			}
			v.PointTotal = len(d.Points)
			v.PointKinds = make(map[string]int)
			var matches []int
			for i, p := range d.Points {
				v.PointKinds[p.Kind]++
				if request.Kind != "" && p.Kind != request.Kind {
					continue
				}
				if filter != "" && !strings.Contains(strings.ToLower(fmt.Sprintf("%d %x %s %s %g %d", p.Address, p.Address, p.Kind, p.Name, p.Value, p.Quality)), filter) {
					continue
				}
				matches = append(matches, i)
			}
			if request.Order == "address" {
				// Sort indices only: never reorder project data or copy the full point table.
				sort.SliceStable(matches, func(i, j int) bool {
					a, b := d.Points[matches[i]], d.Points[matches[j]]
					if a.Address != b.Address {
						return a.Address < b.Address
					}
					return a.Kind < b.Kind
				})
			}
			v.PointMatches = len(matches)
			start := min(offset, len(matches))
			for _, i := range matches[start : start+min(100, len(matches)-start)] {
				v.Points = append(v.Points, d.Points[i])
			}

			category := "telemetry"
			if len(d.DataTests) == 0 && d.AutoData != nil {
				category = ""
			}
			config := dataConfiguration(&d, category)
			if config != nil {
				view := AutoDataSnapshot{Channel: c.ID, Device: d.ID, Category: category, Status: "stopped", Config: *config}
				if job := e.dataJobs[dataJobKey(c.ID, d.ID, category)]; job != nil {
					view = job.view
					view.Config = *config
					view.Cycles = append([]uint64{}, job.view.Cycles...)
				}
				v.Rules = &view
			}
		}
	}
	if request.Messages {
		start := max(0, len(e.messages)-100)
		for i := start; i < len(e.messages); i++ {
			m := e.messages[i]
			if m.Sequence <= request.After {
				continue
			}
			if len(v.Messages) == 0 && request.After > 0 && m.Sequence > request.After+1 {
				v.Missed = m.Sequence - request.After - 1
			}
			v.Messages = append(v.Messages, m)
		}
	}
	e.mu.Unlock()
	// Match Snapshot's lock order: protocol peers may log while holding their lock.
	for i := range v.Channels {
		if transport := transfers[v.Channels[i].ID]; transport != nil {
			v.Channels[i].State.ReadyPeers = len(readyDataPeers(transport))
		}
	}
	return v
}
