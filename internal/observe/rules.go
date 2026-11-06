package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"gytest-go/internal/core"
)

type RuleRequest struct {
	Index    *int   `json:"index,omitempty"`
	Channel  string `json:"channel"`
	Device   string `json:"device"`
	Action   string `json:"action"`
	Revision string `json:"revision,omitempty"`
}
type RuleResult struct {
	View  *core.AutoDataSnapshot `json:"view,omitempty"`
	Error string                 `json:"error,omitempty"`
}
type RuleHandler func(context.Context, RuleRequest) (RuleResult, error)

func ApplyRules(engine *core.Engine, req RuleRequest) (RuleResult, error) {
	if req.Channel == "" || req.Device == "" {
		return RuleResult{}, errors.New("请选择通道和装置")
	}
	current := engine.Inspect(core.InspectRequest{Channel: req.Channel, Device: req.Device}).Rules
	if current == nil {
		return RuleResult{}, errors.New("请选择有效装置并配置自动数据规则")
	}
	if req.Index != nil {
		view, err := engine.ControlDataRule(req.Channel, req.Device, current.Category, *req.Index, req.Action)
		return RuleResult{View: &view}, err
	}
	if req.Action == "stop" && current.Status == "stopped" {
		return RuleResult{View: current}, nil
	}
	var view core.AutoDataSnapshot
	var err error
	switch req.Action {
	case "start":
		if current.Category == "" {
			view, err = engine.StartAutoData(req.Channel, req.Device)
		} else {
			view, err = engine.StartDataTest(req.Channel, req.Device, current.Category)
		}
	case "pause", "resume", "stop":
		if current.Category == "" {
			view, err = engine.SetAutoDataState(req.Channel, req.Device, req.Action)
		} else {
			view, err = engine.SetDataTestState(req.Channel, req.Device, current.Category, req.Action)
		}
	default:
		return RuleResult{}, errors.New("无效规则任务操作")
	}
	if err != nil {
		return RuleResult{}, err
	}
	return RuleResult{View: &view}, nil
}
func (s *Server) SetRuleHandler(handler RuleHandler) {
	s.mu.Lock()
	s.ruleHandler = handler
	s.mu.Unlock()
}
func (s *Server) operateRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var req RuleRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(RuleResult{Error: "请求格式无效：" + err.Error()})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(RuleResult{Error: "请求只能包含一个 JSON 对象"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
	defer cancel()
	s.mu.Lock()
	handler, logger := s.ruleHandler, s.operationLogger
	s.mu.Unlock()
	var result RuleResult
	var err error
	if handler != nil {
		result, err = handler(ctx, req)
	} else {
		result, err = ApplyRules(s.engine, req)
	}
	if err == nil && result.View == nil {
		err = errors.New("规则操作未返回任务状态")
	}
	if err != nil {
		result.Error = err.Error()
		w.WriteHeader(http.StatusBadRequest)
	} else {
		fields := []any{"channel", req.Channel, "device", req.Device, "action", req.Action, "status", result.View.Status}
		if req.Index != nil {
			fields = []any{"channel", req.Channel, "device", req.Device, "index", *req.Index, "action", req.Action, "status", core.RuleRuntimeStatus(result.View, *req.Index)}
		}
		if logger != nil {
			logger("rule_state", fields...)
		} else {
			s.Log("INFO", "rule_state", fields...)
		}
	}
	json.NewEncoder(w).Encode(result)
}

// ControlRules sends one explicit operation; it never retries a state change.
func ControlRules(ctx context.Context, socket string, request RuleRequest) (RuleResult, error) {
	var result RuleResult
	body, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/rules", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode == 404 || response.StatusCode == 405 {
		return result, errors.New("请升级该工程服务后再控制规则任务")
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return result, err
	}
	if result.Error != "" {
		return result, errors.New(result.Error)
	}
	if response.StatusCode != http.StatusOK || result.View == nil {
		return result, fmt.Errorf("规则操作返回无效状态 %d", response.StatusCode)
	}
	return result, nil
}
