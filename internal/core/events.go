package core

import "time"

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
}
