package runner

import (
	"context"
	"errors"

	"gytest-go/internal/observe"
)

func (s *configurationState) ruleRequest(ctx context.Context, req observe.RuleRequest) (observe.RuleResult, error) {
	reply := make(chan configurationReply, 1)
	select {
	case s.jobs <- configurationJob{ctx: ctx, rule: &req, reply: reply}:
	case <-ctx.Done():
		return observe.RuleResult{}, ctx.Err()
	case <-s.ctx.Done():
		return observe.RuleResult{}, errors.New("工程服务已退出")
	}
	select {
	case r := <-reply:
		return r.rule, r.err
	case <-ctx.Done():
		return observe.RuleResult{}, ctx.Err()
	case <-s.ctx.Done():
		return observe.RuleResult{}, errors.New("工程服务已退出")
	}
}
func (s *configurationState) controlRules(req observe.RuleRequest) (observe.RuleResult, error) {
	if req.Revision == "" || req.Revision != s.revision.Load().(string) {
		return observe.RuleResult{}, errors.New("工程配置已变化，请刷新后重试")
	}
	result, err := observe.ApplyRules(s.engine, req)
	if err != nil {
		return result, err
	}
	// Any explicit action supersedes this device's pending automatic launch.
	for i := range s.plans {
		if s.plans[i].channel == req.Channel && s.plans[i].device == req.Device {
			s.plans[i].started = true
		}
	}
	return result, nil
}
