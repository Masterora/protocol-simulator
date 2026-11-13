package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync/atomic"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
	"gytest-go/internal/projectfile"
)

type configurationJob struct {
	ctx     context.Context
	request *projectfile.Request
	point   *core.ManualPointRequest
	rule    *observe.RuleRequest
	reply   chan configurationReply
}
type configurationReply struct {
	view   projectfile.View
	result projectfile.Result
	point  core.ManualPointResult
	rule   observe.RuleResult
	err    error
}
type configurationState struct {
	deleted                               bool
	ctx                                   context.Context
	engine                                *core.Engine
	sink                                  *logSink
	opt                                   options
	project                               core.Project
	channels                              []core.Channel
	plans                                 []dataPlan
	doc                                   projectfile.Document
	watcher                               *projectWatcher
	stopped, manualChannels, explicitData map[string]bool
	jobs                                  chan configurationJob
	revision                              atomic.Value
}

func newConfigurationState(ctx context.Context, engine *core.Engine, p core.Project, channels []core.Channel, opt options, sink *logSink) (*configurationState, error) {
	doc, err := projectfile.Read(opt.project)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(doc.Project, p) {
		return nil, errors.New("启动期间工程文件发生变化，请重试")
	}
	s := &configurationState{ctx: ctx, engine: engine, sink: sink, opt: opt, project: p, channels: channels, doc: doc, watcher: newProjectWatcher(opt.project), stopped: map[string]bool{}, manualChannels: map[string]bool{}, explicitData: map[string]bool{}, jobs: make(chan configurationJob, 4)}
	s.revision.Store(doc.Revision)
	if err = s.rebuildPlans(nil, nil); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *configurationState) acceptWatch() {
	info, err := os.Stat(s.opt.project)
	if err == nil {
		s.watcher.accepted = info
		s.watcher.pending = info
	}
}
func (s *configurationState) publish(doc projectfile.Document) {
	s.doc = doc
	s.project = doc.Project
	s.revision.Store(doc.Revision)
	s.acceptWatch()
}
func (s *configurationState) request(ctx context.Context, req projectfile.Request) (projectfile.View, projectfile.Result, error) {
	reply := make(chan configurationReply, 1)
	job := configurationJob{ctx: ctx, request: &req, reply: reply}
	select {
	case s.jobs <- job:
	case <-ctx.Done():
		return projectfile.View{}, projectfile.Result{}, ctx.Err()
	case <-s.ctx.Done():
		return projectfile.View{}, projectfile.Result{}, errors.New("工程服务已退出")
	}
	select {
	case result := <-reply:
		return result.view, result.result, result.err
	case <-ctx.Done():
		return projectfile.View{}, projectfile.Result{}, ctx.Err()
	case <-s.ctx.Done():
		return projectfile.View{}, projectfile.Result{}, errors.New("工程服务已退出")
	}
}
func (s *configurationState) pointRequest(ctx context.Context, req core.ManualPointRequest) (core.ManualPointResult, error) {
	reply := make(chan configurationReply, 1)
	job := configurationJob{ctx: ctx, point: &req, reply: reply}
	select {
	case s.jobs <- job:
	case <-ctx.Done():
		return core.ManualPointResult{}, ctx.Err()
	case <-s.ctx.Done():
		return core.ManualPointResult{}, errors.New("工程服务已退出")
	}
	select {
	case result := <-reply:
		return result.point, result.err
	case <-ctx.Done():
		return core.ManualPointResult{}, ctx.Err()
	case <-s.ctx.Done():
		return core.ManualPointResult{}, errors.New("工程服务已退出")
	}
}
func (s *configurationState) handle(job configurationJob) {
	reply := configurationReply{}
	if err := job.ctx.Err(); err != nil {
		reply.err = err
	} else if job.rule != nil {
		reply.rule, reply.err = s.controlRules(*job.rule)
	} else if job.point != nil {
		reply.point, reply.err = s.persistPoint(job.ctx, *job.point)
	} else {
		reply.view, reply.result, reply.err = s.configure(job.ctx, *job.request)
	}
	job.reply <- reply
}
func (s *configurationState) selected() ([]core.Channel, error) {
	desired, err := selectChannels(s.project.Channels, s.opt.channels)
	if err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	for _, c := range desired {
		wanted[c.ID] = true
	}
	for id := range s.manualChannels {
		wanted[id] = true
	}
	var selected []core.Channel
	for _, c := range s.project.Channels {
		if wanted[c.ID] && !s.stopped[c.ID] {
			selected = append(selected, c)
		}
	}
	return selected, nil
}
func dataKey(channel, device string) string { return channel + "/" + device }
func (s *configurationState) rebuildPlans(resetDevices, resetChannels map[string]bool) error {
	var err error
	s.channels, err = s.selected()
	if err != nil {
		return err
	}
	plans, err := dataPlans(s.engine, s.channels)
	if err != nil {
		return err
	}
	var accepted []dataPlan
	for _, plan := range plans {
		if !s.opt.startData && !s.explicitData[dataKey(plan.channel, plan.device)] {
			continue
		}
		if !resetDevices[dataKey(plan.channel, plan.device)] && !resetChannels[plan.channel] {
			for _, old := range s.plans {
				if old.channel == plan.channel && old.device == plan.device && old.scope == plan.scope {
					plan.started = old.started
				}
			}
		}
		accepted = append(accepted, plan)
	}
	s.plans = accepted
	return nil
}
func (s *configurationState) persistPoint(ctx context.Context, req core.ManualPointRequest) (core.ManualPointResult, error) {
	if req.Action != "set" || req.Persist == nil || !*req.Persist {
		return s.engine.OperatePoint(ctx, req)
	}
	if req.Revision == "" || req.Revision != s.doc.Revision {
		return core.ManualPointResult{}, projectfile.ErrConflict
	}
	fields := map[string]json.RawMessage{}
	if req.Value != nil {
		fields["value"], _ = json.Marshal(*req.Value)
	}
	if req.Quality != nil {
		fields["quality"], _ = json.Marshal(*req.Quality)
	}
	edit := projectfile.Request{Resource: "point", Action: "edit", Revision: req.Revision, Channel: req.Channel, Device: req.Device, Point: &core.PointKey{Kind: req.Kind, Address: req.Address}, Fields: fields}
	next, _, err := projectfile.Apply(s.doc.Project, edit)
	if err != nil {
		return core.ManualPointResult{}, err
	}
	result, err := s.engine.OperatePointWithCommit(ctx, req, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		revision, err := projectfile.Commit(s.doc.Path, s.doc.Revision, next)
		if err != nil {
			return err
		}
		s.publish(projectfile.Document{Project: next, Revision: revision, Path: s.doc.Path})
		return nil
	})
	if result.Saved {
		result.Path = s.doc.Path
		result.Revision = s.doc.Revision
	}
	return result, err
}
func (s *configurationState) configure(ctx context.Context, req projectfile.Request) (projectfile.View, projectfile.Result, error) {
	var view projectfile.View
	var result projectfile.Result
	if err := ctx.Err(); err != nil {
		return view, result, err
	}
	current, err := projectfile.Read(s.doc.Path)
	if err != nil {
		return view, result, err
	}
	if current.Revision != s.doc.Revision {
		return view, result, projectfile.ErrConflict
	}
	if req.Resource == "point-table" {
		return s.configurePointTable(ctx, req)
	}
	if req.Action == "read" {
		view, err = projectfile.ReadView(s.doc, req)
		return view, result, err
	}
	if req.Action == "defaults" {
		view, err = projectfile.DefaultView(s.doc, req)
		return view, result, err
	}
	if req.Revision == "" || req.Revision != s.doc.Revision {
		return view, result, projectfile.ErrConflict
	}
	if req.Action == "delete" && req.Resource == "project" {
		if len(req.Fields) > 0 || req.Channel != "" || req.Device != "" || req.Point != nil || req.Index != nil {
			return view, result, errors.New("删除工程不接受通道、装置、点位、规则或字段")
		}
		if err = projectfile.Remove(s.doc.Path, req.Revision); err != nil {
			return view, result, err
		}
		s.engine.StopAll()
		s.deleted = true
		result = projectfile.Result{Saved: true, Applied: true, Deleted: true, Path: s.doc.Path, Message: "工程已永久删除并停止，无法恢复"}
		s.sink.info("project_deleted", "path", s.doc.Path)
		return view, result, nil
	}
	if req.Action == "start" || req.Action == "stop" {
		if req.Resource != "channel" {
			return view, result, errors.New("启动或停止只适用于通道")
		}
		found := false
		for _, c := range s.project.Channels {
			found = found || c.ID == req.Channel
		}
		if !found {
			return view, result, errors.New("通道不存在")
		}
		if req.Action == "start" {
			if err = s.engine.Start(req.Channel); err != nil {
				return view, result, err
			}
			delete(s.stopped, req.Channel)
			s.manualChannels[req.Channel] = true
		} else {
			for _, state := range s.engine.ChannelStates() {
				if state.ID == req.Channel && state.Status != "stopped" {
					s.engine.Stop(req.Channel)
					break
				}
			}
			s.stopped[req.Channel] = true
		}
		err = s.rebuildPlans(nil, map[string]bool{req.Channel: true})
		result = projectfile.Result{Applied: true, Path: s.doc.Path, Revision: s.doc.Revision, Channel: req.Channel, Message: map[string]string{"start": "已启动通道", "stop": "已停止通道"}[req.Action]}
		return view, result, err
	}
	next, result, err := projectfile.Apply(s.doc.Project, req)
	if err != nil {
		return view, result, err
	}
	hint := core.ServerPointHint{Channel: req.Channel, Device: req.Device, Old: req.Point, Next: result.Point, Fields: map[string]bool{}}
	for key := range req.Fields {
		hint.Fields[key] = true
	}
	effect, err := s.engine.ApplyServerConfiguration(s.doc.Project, next, hint, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		revision, err := projectfile.Commit(s.doc.Path, s.doc.Revision, next)
		if err != nil {
			return err
		}
		s.publish(projectfile.Document{Project: next, Revision: revision, Path: s.doc.Path})
		return nil
	})
	if err != nil {
		return view, result, err
	}
	result.Saved, result.Applied, result.Path, result.Revision = true, true, s.doc.Path, s.doc.Revision
	result.StoppedChannels = effect.StoppedChannels
	result.Message = "配置已保存并生效"
	if req.Action == "delete" {
		result.Deleted = true
		result.Message = "已永久删除并保存，无法恢复"
	}
	for _, id := range effect.StoppedChannels {
		s.stopped[id] = true
	}
	resets := map[string]bool{}
	for _, d := range effect.ResetDevices {
		resets[dataKey(d.Channel, d.Device)] = true
	}
	if req.Action == "delete" && req.Resource == "device" {
		delete(s.explicitData, dataKey(req.Channel, req.Device))
	}
	if req.Action == "delete" && req.Resource == "channel" {
		delete(s.manualChannels, req.Channel)
		delete(s.stopped, req.Channel)
		for key := range s.explicitData {
			if strings.HasPrefix(key, req.Channel+"/") {
				delete(s.explicitData, key)
			}
		}
		if s.opt.channels != "all" && s.opt.channels != "none" {
			kept := []string{}
			for _, id := range strings.Split(s.opt.channels, ",") {
				if strings.TrimSpace(id) != req.Channel {
					kept = append(kept, id)
				}
			}
			s.opt.channels = strings.Join(kept, ",")
			if s.opt.channels == "" {
				s.opt.channels = "none"
			}
		}
	}
	if req.Resource == "rule" {
		s.explicitData[dataKey(req.Channel, req.Device)] = true
	}
	if err = s.rebuildPlans(resets, nil); err != nil {
		result.Message = "配置已保存；自动规则计划更新失败：" + err.Error()
		return view, result, err
	}
	if len(effect.StoppedChannels) > 0 && req.Action != "delete" {
		result.Message = "配置已保存；新增或通信参数变更的通道保持停止，请手动连接"
	}
	// Full point-definition edits use the existing safe data-point operation for
	// any explicitly changed value/quality. Never send control/setpoint commands.
	if req.Resource == "point" && req.Action == "edit" && (hint.Fields["value"] || hint.Fields["quality"]) {
		info := s.engine.Inspect(core.InspectRequest{})
		var c core.Channel
		for _, candidate := range next.Channels {
			if candidate.ID == req.Channel {
				c = candidate
			}
		}
		dataPoint := c.Role == "server" && (c.Protocol == "iec104" && (result.Point.Kind == "yx" || result.Point.Kind == "yc") || c.Protocol == "modbus-tcp" || c.Protocol == "modbus-rtu")
		if dataPoint {
			pointReq := core.ManualPointRequest{Channel: req.Channel, Device: req.Device, Kind: result.Point.Kind, Address: result.Point.Address, Metadata: info.Metadata, Action: "set"}
			if raw, ok := req.Fields["value"]; ok {
				var value float64
				json.Unmarshal(raw, &value)
				pointReq.Value = &value
			}
			if raw, ok := req.Fields["quality"]; ok {
				var quality byte
				json.Unmarshal(raw, &quality)
				pointReq.Quality = &quality
			}
			pointResult, sendErr := s.engine.OperatePoint(ctx, pointReq)
			if sendErr != nil {
				result.Message = "配置已保存并修改运行值；上报未完成：" + sendErr.Error()
			} else if pointResult.Mode == "offline" {
				result.Message = "配置已保存并修改运行值；连接未激活，未上报"
			} else if pointResult.Mode == "reported" {
				result.Message = fmt.Sprintf("配置已保存，已上报 %d 个连接", pointResult.Receivers)
			}
		}
	}
	s.sink.info("configuration_saved", "resource", req.Resource, "action", req.Action, "path", s.doc.Path, "revision", s.doc.Revision)
	return view, result, nil
}

func (s *configurationState) selectCandidate(p core.Project) ([]core.Channel, error) {
	old := s.project
	s.project = p
	selected, err := s.selected()
	s.project = old
	return selected, err
}
