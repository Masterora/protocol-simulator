package core

import (
	"errors"
	"fmt"
	"reflect"
)

// ReloadProject is the headless server's transactional configuration boundary.
// previous is the last accepted file configuration, never a live value snapshot.
// Unchanged channels retain their sessions, point values and data jobs.
func (e *Engine) ReloadProject(previous, next Project, selected []Channel) (changed []string, restored bool, result error) {
	if err := validateProject(&next); err != nil {
		return nil, true, err
	}
	before, after := map[string]Channel{}, map[string]Channel{}
	for _, c := range previous.Channels {
		before[c.ID] = c
	}
	for _, c := range next.Channels {
		after[c.ID] = c
	}
	affected := map[string]bool{}
	for id, c := range before {
		n, ok := after[id]
		if !ok || !reflect.DeepEqual(c, n) {
			affected[id] = true
		}
	}
	for id := range after {
		if _, ok := before[id]; !ok {
			affected[id] = true
		}
	}
	for id := range affected {
		changed = append(changed, id)
	}
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	old := cloneProject(e.project)
	running := map[string]bool{}
	for id := range affected {
		running[id] = e.running[id] != nil
	}
	e.mu.Unlock()
	for id := range affected {
		e.stopLocked(id)
	}
	install := func(p Project) {
		p = cloneProject(p)
		e.mu.Lock()
		defer e.mu.Unlock()
		// Use the live values of unchanged channels, including edits from peers.
		for i, c := range p.Channels {
			if !affected[c.ID] {
				for _, current := range e.project.Channels {
					if current.ID == c.ID {
						p.Channels[i] = current
						break
					}
				}
			}
		}
		e.project = p
		for key, job := range e.dataJobs {
			if affected[job.view.Channel] {
				delete(e.dataJobs, key)
			}
		}
		for id := range affected {
			delete(e.states, id)
		}
		e.markConfigurationLocked()
		e.dirty = false
	}
	install(next)
	for _, c := range selected {
		if !affected[c.ID] {
			continue
		}
		if err := e.startLocked(c.ID); err != nil {
			result = fmt.Errorf("start reloaded channel %s: %w", c.ID, err)
			break
		}
	}
	if result == nil {
		return changed, false, nil
	}
	for id := range affected {
		e.stopLocked(id)
	}
	install(old)
	restored = true
	for id, active := range running {
		if active {
			if err := e.startLocked(id); err != nil {
				restored = false
				result = errors.Join(result, fmt.Errorf("restore channel %s: %w", id, err))
			}
		}
	}
	return changed, restored, result
}
