package core

import "sync/atomic"

// MessageSubscription observes future messages without blocking protocol work.
// The existing desktop message buffer and counters keep their original behavior.
type MessageSubscription struct {
	engine   *Engine
	messages chan Message
	frames   bool
	dropped  atomic.Uint64
}

func (e *Engine) SubscribeMessages(capacity int, frames bool) *MessageSubscription {
	if capacity < 1 || capacity > 65536 {
		panic("message subscription capacity must be 1..65536")
	}
	s := &MessageSubscription{engine: e, messages: make(chan Message, capacity), frames: frames}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.subscribers == nil {
		e.subscribers = make(map[*MessageSubscription]struct{})
	}
	e.subscribers[s] = struct{}{}
	return s
}

func (s *MessageSubscription) Messages() <-chan Message { return s.messages }
func (s *MessageSubscription) Dropped() uint64          { return s.dropped.Load() }
func (s *MessageSubscription) Close() {
	s.engine.mu.Lock()
	defer s.engine.mu.Unlock()
	if _, exists := s.engine.subscribers[s]; exists {
		delete(s.engine.subscribers, s)
		close(s.messages)
	}
}

// ChannelStates reads runtime state without copying large project point tables.
func (e *Engine) ChannelStates() []ChannelState {
	e.mu.Lock()
	states := make([]ChannelState, 0, len(e.project.Channels))
	transfers := make(map[string]*session)
	for _, c := range e.project.Channels {
		state, exists := e.states[c.ID]
		if !exists {
			state = ChannelState{ID: c.ID, Status: "stopped", Address: c.Address}
		}
		states = append(states, state)
		if c.Protocol == "iec104" {
			transfers[c.ID] = e.running[c.ID]
		} else {
			states[len(states)-1].ReadyPeers = state.Peers
		}
	}
	e.mu.Unlock()
	for i := range states {
		if transfer := transfers[states[i].ID]; transfer != nil {
			states[i].ReadyPeers = len(readyDataPeers(transfer))
		}
	}
	return states
}
