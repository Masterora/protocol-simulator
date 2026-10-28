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

func (s *Server) operatePoint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", 405)
		return
	}
	var req core.ManualPointRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	w.Header().Set("Content-Type", "application/json")
	if err := decoder.Decode(&req); err != nil {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(core.ManualPointResult{Error: "请求格式无效：" + err.Error()})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(core.ManualPointResult{Error: "请求只能包含一个 JSON 对象"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
	defer cancel()
	result, err := s.engine.OperatePoint(ctx, req)
	if err != nil {
		result.Error = err.Error()
		if !result.Applied && result.Receivers == 0 {
			if errors.Is(err, core.ErrPointConfigurationChanged) {
				w.WriteHeader(409)
			} else {
				w.WriteHeader(400)
			}
		}
	}
	if result.Point != nil {
		message := "point_set"
		if req.Action == "report" {
			message = "point_report"
		}
		s.mu.Lock()
		logger := s.operationLogger
		s.mu.Unlock()
		fields := []any{"channel", req.Channel, "device", req.Device, "kind", req.Kind, "address", req.Address, "value", result.Point.Value, "quality", result.Point.Quality, "mode", result.Mode, "receivers", result.Receivers, "error", result.Error}
		if logger != nil {
			logger(message, fields...)
		} else {
			s.Log("INFO", message, fields...)
		}
	}
	json.NewEncoder(w).Encode(result)
}

func (s *Server) SetOperationLogger(logger func(string, ...any)) {
	s.mu.Lock()
	s.operationLogger = logger
	s.mu.Unlock()
}

// Operate makes one explicit request. Writes are never retried automatically.
func Operate(ctx context.Context, socket string, request core.ManualPointRequest) (core.ManualPointResult, error) {
	var result core.ManualPointResult
	body, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/point", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 || resp.StatusCode == 405 {
		return result, fmt.Errorf("运行中的工程仍是只读版本，请升级该工程的服务进程")
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result); err != nil {
		return result, err
	}
	if result.Error != "" {
		return result, errors.New(result.Error)
	}
	if resp.StatusCode != 200 {
		return result, fmt.Errorf("point operation status %d", resp.StatusCode)
	}
	return result, nil
}
