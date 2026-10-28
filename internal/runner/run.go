package runner

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
)

type dataPlan struct {
	channel, device, scope, protocol string
	started                          bool
}

func run(ctx context.Context, engine *core.Engine, project core.Project, channels []core.Channel, opt options, sink *logSink) (result error) {
	observer, observerErr := observe.Start(engine, opt.runtimeDir)
	if observerErr != nil {
		sink.emit(logRecord{level: slog.LevelWarn, message: "observer_unavailable", values: []any{"error", observerErr.Error()}})
	} else {
		sink.mu.Lock()
		sink.observer = observer
		sink.mu.Unlock()
		defer observer.Close()
		observer.SetOperationLogger(sink.info)
		sink.info("observer_ready", "socket", observer.Socket())
	}
	subscription := engine.SubscribeMessages(1024, opt.frames != "")
	var reportedDrops uint64
	defer subscription.Close()
	defer func() {
		if result != nil {
			sink.emit(logRecord{level: slog.LevelError, message: "runtime_failure", values: []any{"error", result.Error()}})
		}
		engine.StopAll()
		// StopAll has joined protocol workers; drain their final messages.
		subscription.Close()
		for message := range subscription.Messages() {
			forwardMessage(sink, message)
		}
		if n := subscription.Dropped(); n > reportedDrops {
			sink.emit(logRecord{level: slog.LevelWarn, message: "protocol_log_records_dropped", values: []any{"count", n - reportedDrops, "total", n}})
		}
		sink.info("stopped", "project", project.Name)
	}()
	var plans []dataPlan
	var err error
	if opt.startData {
		plans, err = dataPlans(engine, channels)
		if err != nil {
			return err
		}
	}
	watcher := newProjectWatcher(opt.project)

	sink.info("starting", "project", project.Name, "channels", len(channels), "data_jobs", len(plans))
	for _, c := range channels {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if err := engine.Start(c.ID); err != nil {
			return fmt.Errorf("start channel %s: %w", c.ID, err)
		}
	}
	sink.info("running", "project", project.Name)
	previous := map[string]core.ChannelState{}
	startedJobs := 0
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-sink.errors:
			return err
		case message := <-subscription.Messages():
			forwardMessage(sink, message)
		case <-tick.C:
			if opt.watch {
				if candidate, changed, watchErr := watcher.poll(); changed {
					if watchErr != nil {
						sink.emit(logRecord{level: slog.LevelError, message: "reload_rejected", values: []any{"error", watchErr.Error()}})
					} else {
						selected, selectErr := selectChannels(candidate.Channels, opt.channels)
						if selectErr != nil {
							sink.emit(logRecord{level: slog.LevelError, message: "reload_rejected", values: []any{"error", selectErr.Error()}})
						} else {
							ids, restored, reloadErr := engine.ReloadProject(project, candidate, selected)
							if reloadErr != nil && !restored {
								return reloadErr
							}
							affected := map[string]bool{}
							for _, id := range ids {
								affected[id] = true
								delete(previous, id)
							}
							oldPlans := plans
							if reloadErr == nil {
								project = candidate
								channels = selected
								sink.info("reloaded", "project", project.Name, "changed_channels", ids)
							} else {
								sink.emit(logRecord{level: slog.LevelError, message: "reload_rolled_back", values: []any{"error", reloadErr.Error()}})
							}
							if opt.startData {
								plans, err = dataPlans(engine, channels)
								if err != nil {
									return err
								}
								for i := range plans {
									if !affected[plans[i].channel] {
										for _, old := range oldPlans {
											if old.channel == plans[i].channel && old.device == plans[i].device && old.scope == plans[i].scope {
												plans[i].started = old.started
											}
										}
									}
								}
							}
						}
					}
				}
			}
			if n := subscription.Dropped(); n > reportedDrops {
				sink.emit(logRecord{level: slog.LevelWarn, message: "protocol_log_records_dropped", values: []any{"count", n - reportedDrops, "total", n}})
				reportedDrops = n
			}
			states := map[string]core.ChannelState{}
			for _, state := range engine.ChannelStates() {
				states[state.ID] = state
				old := previous[state.ID]
				if old.Status != state.Status || old.Peers != state.Peers || old.ReadyPeers != state.ReadyPeers || old.LastError != state.LastError {
					sink.info("channel_state", "channel", state.ID, "status", state.Status, "peers", state.Peers, "ready_peers", state.ReadyPeers, "error", state.LastError)
					previous[state.ID] = state
				}
			}
			for i := range plans {
				p := &plans[i]
				if p.started || p.protocol == "iec104" && states[p.channel].ReadyPeers == 0 {
					continue
				}
				var err error
				if p.scope == "" {
					_, err = engine.StartAutoData(p.channel, p.device)
				} else {
					_, err = engine.StartDataTest(p.channel, p.device, p.scope)
				}
				if err != nil {
					if p.protocol == "iec104" {
						ready := false
						for _, state := range engine.ChannelStates() {
							if state.ID == p.channel {
								ready = state.ReadyPeers > 0
							}
						}
						if !ready {
							continue
						}
					}
					return fmt.Errorf("start data rules %s/%s: %w", p.channel, p.device, err)
				}
				p.started = true
				startedJobs++
				sink.info("data_rules_started", "channel", p.channel, "device", p.device, "started", startedJobs)
			}
		}
	}
}
func forwardMessage(sink *logSink, message core.Message) {
	if message.Direction == "RX" || message.Direction == "TX" {
		sink.emit(logRecord{frame: &message})
		return
	}
	sink.info("protocol", "sequence", message.Sequence, "channel", message.ChannelID, "peer", message.Peer, "direction", message.Direction, "summary", message.Summary)
}

func dataPlans(engine *core.Engine, channels []core.Channel) ([]dataPlan, error) {
	var plans []dataPlan
	for _, c := range channels {
		if c.Role != "server" || c.Protocol == "raw" {
			continue
		}
		for _, d := range c.Devices {
			scope := "telemetry"
			var view core.AutoDataSnapshot
			var err error
			if len(d.DataTests) == 0 && d.AutoData != nil {
				scope = ""
				view, err = engine.GetAutoData(c.ID, d.ID)
			} else {
				view, err = engine.GetDataTest(c.ID, d.ID, scope)
			}
			if err != nil {
				return nil, err
			}
			for _, r := range view.Config.Rules {
				if r.Enabled {
					plans = append(plans, dataPlan{channel: c.ID, device: d.ID, scope: scope, protocol: c.Protocol})
					break
				}
			}
		}
	}
	return plans, nil
}
