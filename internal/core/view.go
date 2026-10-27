package core

import (
	"errors"
	"time"
)

// View is the desktop's incremental read model. Full Snapshot remains available
// for exports and diagnostics, but polling never copies unrelated point tables.
type ViewRequest struct {
	Role          string `json:"role"`
	Channel       string `json:"channel"`
	Device        string `json:"device"`
	Metadata      uint64 `json:"metadata"`
	PointIdentity string `json:"pointIdentity"`
	Points        uint64 `json:"points"`
	Messages      uint64 `json:"messages"`
	MessageEpoch  uint64 `json:"messageEpoch"`
	Events        uint64 `json:"events"`
	EventEpoch    uint64 `json:"eventEpoch"`
}
type View struct {
	Metadata        uint64         `json:"metadata"`
	Project         *Project       `json:"project,omitempty"`
	Name            string         `json:"name"`
	Path            string         `json:"path"`
	Dirty           bool           `json:"dirty"`
	Generation      uint64         `json:"generation"`
	States          []ChannelState `json:"states"`
	Channel         string         `json:"channel"`
	Device          string         `json:"device"`
	PointIdentity   string         `json:"pointIdentity"`
	PointVersion    uint64         `json:"pointVersion"`
	FullPoints      bool           `json:"fullPoints"`
	Points          []Point        `json:"points,omitempty"`
	FirstMessage    uint64         `json:"firstMessage"`
	FirstEvent      uint64         `json:"firstEvent"`
	MessageEpoch    uint64         `json:"messageEpoch"`
	MessageSequence uint64         `json:"messageSequence"`
	ResetMessages   bool           `json:"resetMessages"`
	Messages        []Message      `json:"messages,omitempty"`
	EventEpoch      uint64         `json:"eventEpoch"`
	EventSequence   uint64         `json:"eventSequence"`
	ResetEvents     bool           `json:"resetEvents"`
	Events          []PointEvent   `json:"events,omitempty"`
}
type changedPoint struct {
	version uint64
	point   Point
}
type pointRevision struct {
	version uint64
	changed map[PointKey]changedPoint
}

func (e *Engine) markConfigurationLocked() {
	e.viewClock++
	e.viewMetadata = e.viewClock
	e.viewPoints = nil
}
func (e *Engine) markPointsLocked(channel, device string, points []Point) {
	if len(points) == 0 {
		return
	}
	e.viewClock++
	if e.viewPoints == nil {
		e.viewPoints = map[string]*pointRevision{}
	}
	key := channel + "/" + device
	v := e.viewPoints[key]
	if v == nil {
		v = &pointRevision{changed: map[PointKey]changedPoint{}}
		e.viewPoints[key] = v
	}
	v.version = e.viewClock
	for _, p := range points {
		v.changed[PointKey{p.Kind, p.Address}] = changedPoint{e.viewClock, p}
	}
}
func (e *Engine) View(request ViewRequest) View {
	e.mu.Lock()
	v := View{Metadata: e.viewMetadata, Name: e.project.Name, Path: e.path, Dirty: e.dirty, Generation: e.dataGeneration, MessageEpoch: e.messageEpoch, MessageSequence: e.sequence, EventEpoch: e.eventEpoch, EventSequence: e.eventSequence}
	reset := request.Metadata != v.Metadata
	if reset {
		project := Project{Version: e.project.Version, Name: e.project.Name, Channels: make([]Channel, len(e.project.Channels))}
		for i, c := range e.project.Channels {
			c.Points = nil
			c.TransportDraft = cloneTransportDraft(c.TransportDraft)
			c.Devices = append([]Device{}, c.Devices...)
			for j := range c.Devices {
				d := &c.Devices[j]
				d.Points = nil
				d.Configuration = cloneConfiguration(d.Configuration)
				d.AutoData = cloneAutoData(d.AutoData)
				d.DataTests = cloneDataTests(d.DataTests)
			}
			project.Channels[i] = c
		}
		v.Project = &project
	}
	transfers := map[string]*session{}
	var selected *Channel
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		state, ok := e.states[c.ID]
		if !ok {
			state = ChannelState{ID: c.ID, Status: "stopped", Address: c.Address}
		}
		v.States = append(v.States, state)
		if c.Protocol == "iec104" {
			transfers[c.ID] = e.running[c.ID]
		}
		if (request.Role == "" || c.Role == request.Role) && (selected == nil || c.ID == request.Channel) {
			if selected == nil || c.ID == request.Channel {
				selected = c
			}
		}
	}
	if selected != nil {
		v.Channel = selected.ID
		if d, err := deviceIn(selected, request.Device); err == nil {
			v.Device = d.ID
		} else if len(selected.Devices) > 0 {
			v.Device = selected.Devices[0].ID
		}
		if d, err := deviceIn(selected, v.Device); err == nil {
			v.PointIdentity = v.Channel + "/" + v.Device
			change := e.viewPoints[v.PointIdentity]
			v.PointVersion = e.viewMetadata
			if change != nil {
				v.PointVersion = change.version
			}
			v.FullPoints = reset || request.PointIdentity != v.PointIdentity || request.Points == 0 || request.Points > v.PointVersion
			if v.FullPoints {
				v.Points = append([]Point{}, d.Points...)
			} else if change != nil && request.Points < change.version {
				for _, c := range change.changed {
					if c.version > request.Points {
						v.Points = append(v.Points, c.point)
					}
				}
			}
		}
	}
	if len(e.messages) > 0 {
		v.FirstMessage = e.messages[0].Sequence
	}
	if len(e.events) > 0 {
		v.FirstEvent = e.events[0].Sequence
	}
	v.ResetMessages = request.MessageEpoch != e.messageEpoch || request.Messages > e.sequence
	if len(e.messages) > 0 && request.Messages+1 < e.messages[0].Sequence {
		v.ResetMessages = true
	}
	for _, m := range e.messages {
		if v.ResetMessages || m.Sequence > request.Messages {
			v.Messages = append(v.Messages, m)
		}
	}
	v.ResetEvents = request.EventEpoch != e.eventEpoch || request.Events > e.eventSequence
	if len(e.events) > 0 && request.Events+1 < e.events[0].Sequence {
		v.ResetEvents = true
	}
	for _, event := range e.events {
		if v.ResetEvents || event.Sequence > request.Events {
			v.Events = append(v.Events, event)
		}
	}
	e.mu.Unlock()
	for i := range v.States {
		state := &v.States[i]
		if transport, iec104 := transfers[state.ID]; iec104 {
			if transport != nil {
				state.ReadyPeers = len(readyDataPeers(transport))
			}
		} else {
			state.ReadyPeers = state.Peers
		}
	}
	return v
}

type PointEvent struct {
	Sequence    uint64  `json:"sequence"`
	ChannelID   string  `json:"channelId"`
	DeviceID    string  `json:"deviceId"`
	Category    string  `json:"category"`
	Address     uint32  `json:"address"`
	Kind        string  `json:"kind"`
	Value       float64 `json:"value"`
	Quality     byte    `json:"quality"`
	Timestamp   string  `json:"timestamp"`
	Received    string  `json:"received"`
	TimeInvalid bool    `json:"timeInvalid"`
}

func (e *Engine) recordEvents(channel, device string, points []Point, cause byte) {
	if cause != 3 && cause != 11 && cause != 12 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, p := range points {
		if p.Kind != "yx" && p.Kind != "coil" && p.Kind != "discrete" {
			continue
		}
		category := "COS"
		if p.Timestamp != "" {
			category = "SOE"
		}
		e.eventSequence++
		e.events = append(e.events, PointEvent{e.eventSequence, channel, device, category, p.Address, p.Kind, p.Value, p.Quality, p.Timestamp, time.Now().Format(time.RFC3339Nano), p.TimeInvalid})
	}
	if len(e.events) > 5000 {
		e.events = append([]PointEvent{}, e.events[len(e.events)-5000:]...)
	}
}
func (e *Engine) ClearEvents(channel, category string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	kept := e.events[:0]
	for _, v := range e.events {
		if (channel != "" && v.ChannelID != channel) || (category != "" && v.Category != category) {
			kept = append(kept, v)
		}
	}
	e.events = kept
	e.eventEpoch++
}

// The first successful poll is a baseline. Subsequent value changes of actual
// telemetry inputs create COS records; command echoes and unchanged polls do not.
func (p *peer) recordModbusChanges(d Device, points []Point) {
	p.mu.Lock()
	if p.observed == nil {
		p.observed = map[string]map[PointKey]Point{}
	}
	if p.observed[d.ID] == nil {
		p.observed[d.ID] = map[PointKey]Point{}
	}
	previous := p.observed[d.ID]
	changes := []Point{}
	for _, u := range points {
		original, configured := findRemotePoint(d.Points, PointKey{u.Kind, u.Address})
		if !configured || dataPointCategory(p.s.cfg.Protocol, original) != "yx" {
			continue
		}
		key := PointKey{u.Kind, u.Address}
		old, seen := previous[key]
		if seen && old.Value != u.Value {
			changes = append(changes, u)
		}
		previous[key] = u
	}
	p.mu.Unlock()
	p.s.e.recordEvents(p.s.cfg.ID, d.ID, changes, 3)
}

// Explicitly opened runtime details read one bounded page, rather than polling
// the entire saved recipe in every main window.
type AutoControlDetails struct {
	Rows     []AutoControlRow `json:"rows"`
	Errors   []string         `json:"errors"`
	Offset   int              `json:"offset"`
	RowCount int              `json:"rowCount"`
	Current  int              `json:"current"`
}

func (e *Engine) AutoControlDetails(channel, device, mode string, offset int) (AutoControlDetails, error) {
	if _, err := e.AutoControlStatus(channel, device, mode); err != nil {
		return AutoControlDetails{}, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.autoMu.Lock()
	job := e.autoJobs[autoKey(channel, device, mode)]
	e.autoMu.Unlock()
	v := AutoControlDetails{Rows: []AutoControlRow{}, Errors: []string{}, Current: -1}
	if job == nil {
		return v, nil
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	v.RowCount = len(job.view.Rows)
	if offset < 0 {
		offset = 0
	}
	if offset >= v.RowCount {
		offset = max(0, (v.RowCount-1)/100*100)
	}
	v.Offset = offset
	v.Current = job.view.Current
	v.Rows = append(v.Rows, job.view.Rows[offset:min(offset+100, v.RowCount)]...)
	v.Errors = append(v.Errors, job.view.Errors[max(0, len(job.view.Errors)-20):]...)
	return v, nil
}

// Runtime polling needs counters and availability, not thousands of recipe rows.
type AutoControlStatus struct {
	Saved       bool   `json:"saved"`
	Status      string `json:"status"`
	EnabledRows int    `json:"enabledRows"`
	Total       int    `json:"total"`
	Success     int    `json:"success"`
	Failure     int    `json:"failure"`
	LastError   string `json:"lastError"`
}

func (e *Engine) AutoControlStatus(channel, device, mode string) (AutoControlStatus, error) {
	if mode != "yk" && mode != "yt" {
		return AutoControlStatus{}, errors.New("自动操作类型无效")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var target *Device
	for i := range e.project.Channels {
		c := &e.project.Channels[i]
		if c.ID == channel && c.Role == "client" {
			d, err := deviceIn(c, device)
			if err != nil {
				return AutoControlStatus{}, err
			}
			target = d
			break
		}
	}
	if target == nil {
		return AutoControlStatus{}, errors.New("通道或装置不存在")
	}
	e.autoMu.Lock()
	job := e.autoJobs[autoKey(channel, device, mode)]
	e.autoMu.Unlock()
	v := AutoControlStatus{Status: "idle"}
	if job != nil {
		job.mu.Lock()
		defer job.mu.Unlock()
		j := job.view
		v.Saved = j.Saved
		v.Status = j.Status
		v.Total = j.Total
		v.Success = j.Success
		v.Failure = j.Failure
		v.LastError = j.LastError
		for _, r := range j.Rows {
			if mode == "yt" || r.Enabled {
				v.EnabledRows++
			}
		}
		return v, nil
	}
	if target.Configuration != nil {
		if recipe, ok := target.Configuration.AutoTests[mode]; ok {
			v.Saved = true
			for _, r := range recipe.Rows {
				if mode == "yt" || r.Enabled {
					v.EnabledRows++
				}
			}
		}
	}
	return v, nil
}
