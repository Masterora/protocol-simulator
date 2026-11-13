package observe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"gytest-go/internal/core"
)

type MasterRequest struct {
	Action    string
	Channel   string
	Device    string
	Selection string
	Function  byte
	Value     *uint16
}

func (s *Server) master(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	var req MasterRequest
	if err := decoder.Decode(&req); err != nil || decoder.Decode(new(any)) != io.EOF || req.Channel == "" || req.Device == "" {
		http.Error(w, "无效的主站操作参数", 400)
		return
	}
	// Modbus batches can take longer than the observation server's read-only deadline.
	if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Second)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		http.Error(w, "无法设置主站操作超时", http.StatusInternalServerError)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	var result core.PointOperationResult
	var err error
	switch {
	case req.Action == "interrogate" && req.Selection == "" && req.Function == 0 && req.Value == nil:
		err = s.engine.InterrogateDevice(req.Channel, req.Device)
	case req.Action == "read" && req.Selection != "" && req.Value == nil:
		result, err = s.engine.OperateDeviceModbusPoints(ctx, req.Channel, req.Device, req.Function, req.Selection, nil)
	case req.Action == "write" && req.Selection != "" && req.Value != nil:
		if (req.Function == 5 || req.Function == 15) && *req.Value > 1 {
			http.Error(w, "线圈值必须为 0 或 1", http.StatusBadRequest)
			return
		}
		result, err = s.engine.OperateDeviceModbusPoints(ctx, req.Channel, req.Device, req.Function, req.Selection, req.Value)
	default:
		http.Error(w, "无效的主站操作参数", 400)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func OperateMaster(ctx context.Context, socket string, req MasterRequest) (core.PointOperationResult, error) {
	var result core.PointOperationResult
	body, err := json.Marshal(req)
	if err != nil {
		return result, err
	}
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/master", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	response, err := client.Do(request)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		if response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotFound {
			return result, errors.New("运行中的服务需要升级才能使用主站操作")
		}
		return result, errors.New(string(bytes.TrimSpace(message)))
	}
	err = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result)
	return result, err
}
