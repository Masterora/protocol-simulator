package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gytest-go/internal/core"
	"gytest-go/internal/projectfile"
	"io"
	"net"
	"net/http"
	"time"
)

type ConfigurationHandler func(context.Context, projectfile.Request) (projectfile.View, projectfile.Result, error)
type PointHandler func(context.Context, core.ManualPointRequest) (core.ManualPointResult, error)

func (s *Server) SetConfigurationHandler(handler ConfigurationHandler, point PointHandler, revision func() string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configurationHandler = handler
	s.pointHandler = point
	s.configurationRevision = revision
}
func (s *Server) configuration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", 405)
		return
	}
	http.NewResponseController(w).SetWriteDeadline(time.Now().Add(15 * time.Second))
	s.mu.Lock()
	handler := s.configurationHandler
	s.mu.Unlock()
	if handler == nil {
		http.Error(w, "Configuration editing unavailable", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	var request projectfile.Request
	limit := int64(1 << 20)
	if r.URL.Query().Get("tools") == "1" {
		limit = 32 << 20
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(configurationReply{Error: err.Error()})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(configurationReply{Error: "请求只能包含一个 JSON 对象"})
		return
	}
	view, result, err := handler(r.Context(), request)
	reply := configurationReply{View: view, Result: result}
	if err != nil {
		reply.Error = err.Error()
		if errors.Is(err, projectfile.ErrConflict) {
			w.WriteHeader(409)
		} else {
			w.WriteHeader(400)
		}
	}
	json.NewEncoder(w).Encode(reply)
}

type configurationReply struct {
	View   projectfile.View   `json:"view"`
	Result projectfile.Result `json:"result"`
	Error  string             `json:"error,omitempty"`
}

func Configure(ctx context.Context, socket string, request projectfile.Request) (projectfile.View, projectfile.Result, error) {
	var reply configurationReply
	body, err := json.Marshal(request)
	if err != nil {
		return reply.View, reply.Result, err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second}
	url := "http://unix/v1/configuration"
	limit := int64(1 << 20)
	if request.Resource == "point-table" {
		url += "?tools=1"
		limit = 32 << 20
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return reply.View, reply.Result, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return reply.View, reply.Result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 || resp.StatusCode == 405 {
		return reply.View, reply.Result, errors.New("运行中的工程不支持配置编辑，请升级该工程服务")
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, limit)).Decode(&reply); err != nil {
		return reply.View, reply.Result, err
	}
	if reply.Error != "" {
		if resp.StatusCode == 409 {
			return reply.View, reply.Result, fmt.Errorf("%w：%s", projectfile.ErrConflict, reply.Error)
		}
		return reply.View, reply.Result, errors.New(reply.Error)
	}
	if resp.StatusCode != 200 {
		return reply.View, reply.Result, fmt.Errorf("configuration operation status %d", resp.StatusCode)
	}
	return reply.View, reply.Result, nil
}
