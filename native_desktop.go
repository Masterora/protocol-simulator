package main

// Wails v2 owns one native window per process. Editor processes share the
// parent's engine over authenticated loopback RPC; they never create sessions.
import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"gytest-go/internal/core"
)

type NativeContext struct {
	Generation uint64                  `json:"generation,omitempty"`
	Position   *nativePosition         `json:"position,omitempty"`
	Kind       string                  `json:"kind"`
	Channel    string                  `json:"channel"`
	Device     string                  `json:"device"`
	Role       string                  `json:"role"`
	Edit       bool                    `json:"edit"`
	Point      string                  `json:"point"`
	PointKind  string                  `json:"pointKind"`
	Ask        json.RawMessage         `json:"ask,omitempty"`
	Activate   int                     `json:"activate"`
	Draft      json.RawMessage         `json:"draft,omitempty"`
	Control    *core.Control104Request `json:"control,omitempty"`
}
type nativePosition struct {
	X int `json:"x"`
	Y int `json:"y"`
}
type nativeResult struct {
	Choice string `json:"choice"`
	Value  string `json:"value"`
}
type nativeSession struct {
	context NativeContext
	cmd     *exec.Cmd
	result  chan nativeResult
	ready   bool
	layout  nativeLayout
}
type nativeLayout struct {
	ViewportWidth  int `json:"viewportWidth"`
	ViewportHeight int `json:"viewportHeight"`
	DocumentWidth  int `json:"documentWidth"`
	DialogWidth    int `json:"dialogWidth"`
	Controls       int `json:"controls"`
}
type nativeWindows struct {
	mu             sync.Mutex
	app            *App
	server         *http.Server
	address, token string
	sessions       map[string]*nativeSession
	selection      NativeContext
	closed         bool
	drafts         map[string]json.RawMessage
}
type nativeRemote struct {
	address, token, session string
	kind                    string
	exiting                 atomic.Bool
}
type nativeRequest struct {
	Session string            `json:"session"`
	Method  string            `json:"method"`
	Args    []json.RawMessage `json:"args"`
}
type nativeReply struct {
	Value json.RawMessage `json:"value"`
	Error string          `json:"error,omitempty"`
}

func nonce() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

func newNativeWindows(a *App) (*nativeWindows, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	n := &nativeWindows{app: a, address: "http://" + listener.Addr().String(), token: nonce(), sessions: make(map[string]*nativeSession), drafts: make(map[string]json.RawMessage)}
	n.server = &http.Server{Handler: http.HandlerFunc(n.serve), ReadHeaderTimeout: 3 * time.Second}
	go n.server.Serve(listener)
	return n, nil
}

func (n *nativeWindows) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || r.URL.Path != "/rpc" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+n.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var q nativeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&q); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	n.mu.Lock()
	s := n.sessions[q.Session]
	n.mu.Unlock()
	if s == nil {
		http.Error(w, "window closed", 410)
		return
	}
	var value any
	var err error
	switch q.Method {
	case "NativeWindowReady":
		var layout nativeLayout
		if len(q.Args) == 1 {
			if err = json.Unmarshal(q.Args[0], &layout); err != nil {
				break
			}
		}
		n.mu.Lock()
		s.ready = true
		s.layout = layout
		n.mu.Unlock()
	case "SaveNativeDraft":
		if s.context.Kind != "device-dialog" || s.context.Edit || len(q.Args) != 1 {
			err = errors.New("invalid draft")
		} else {
			n.mu.Lock()
			n.drafts[s.context.Role] = append(json.RawMessage(nil), q.Args[0]...)
			n.mu.Unlock()
		}
	case "GetNativeContext":
		n.mu.Lock()
		c := s.context
		if (c.Kind == "channel-dialog" || c.Kind == "device-dialog" && !c.Edit) && n.selection.Role == c.Role {
			c.Channel = n.selection.Channel
		}
		n.mu.Unlock()
		value = c
		if c.Kind == "auto-data-dialog" {
			var view core.AutoDataSnapshot
			view, err = n.app.engine.GetAutoData(c.Channel, c.Device)
			if err == nil && c.Generation != 0 && c.Generation != view.Generation {
				err = errors.New("工程已更换，请重新打开自动数据窗口")
			}
		}
		if isAutoWindow(c.Kind) {
			mode := "yk"
			if c.Kind == "auto-yt-dialog" {
				mode = "yt"
			}
			err = n.app.engine.ValidateAutoControlWindow(c.Channel, c.Device, mode)
		}
	case "ResolveNativeAsk":
		var result nativeResult
		if len(q.Args) != 1 {
			err = errors.New("invalid result")
		} else if err = json.Unmarshal(q.Args[0], &result); err == nil {
			var options struct {
				Choices [][2]string `json:"choices"`
			}
			_ = json.Unmarshal(s.context.Ask, &options)
			allowed := result.Choice == "cancel"
			for _, choice := range options.Choices {
				allowed = allowed || choice[0] == result.Choice
			}
			if s.context.Kind != "confirm-dialog" || !allowed || len(result.Value) > 1000 {
				err = errors.New("invalid choice")
			} else {
				select {
				case s.result <- result:
				default:
				}
			}
		}
	default:
		value, err = invokeNative(n.app, q.Method, q.Args)
	}
	reply := nativeReply{}
	if err != nil {
		reply.Error = err.Error()
	} else {
		reply.Value, _ = json.Marshal(value)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}

// Only editor operations are reachable over IPC. File dialogs and application
// lifecycle methods always run in the owning desktop window.
var nativeMethods = map[string]bool{
	"GetAutoData": true, "SaveAutoData": true, "StartAutoData": true, "SetAutoDataState": true,
	"ReadClockDevice": true, "ClearEvents": true, "ClockSyncDevice": true, "CounterInterrogateDevice": true, "GetSnapshot": true, "SerialPorts": true, "LocalIPs": true, "UpsertChannel": true, "UpsertDevice": true,
	"PatchDevicePoint": true, "StartChannel": true, "StopChannel": true, "StopAll": true,
	"SetPointValue": true, "EditPointCell": true,
	"AskNative": true, "UpdateTransport": true, "RenameChannel": true,
	"Command104Device":   true,
	"PrepareAutoControl": true, "GetAutoControl": true, "UpdateAutoOptions": true, "UpdateAutoRows": true,
	"ApplyAutoBulk": true, "StartAutoControl": true, "PauseAutoControl": true, "ResumeAutoControl": true, "StopAutoControl": true,
	"OpenAutoBulk":     true,
	"SetAutoValueType": true,
}

func invokeNative(a *App, name string, args []json.RawMessage) (value any, err error) {
	if !nativeMethods[name] {
		return nil, errors.New("unsupported editor operation")
	}
	defer func() {
		if recover() != nil {
			value = nil
			err = errors.New("invalid editor request")
		}
	}()
	method := reflect.ValueOf(a).MethodByName(name)
	t := method.Type()
	if len(args) != t.NumIn() {
		return nil, errors.New("invalid argument count")
	}
	input := make([]reflect.Value, len(args))
	for i, arg := range args {
		p := reflect.New(t.In(i))
		if err = json.Unmarshal(arg, p.Interface()); err != nil {
			return nil, err
		}
		input[i] = p.Elem()
	}
	output := method.Call(input)
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	for _, v := range output {
		if v.Type().Implements(errorType) {
			if !v.IsNil() {
				err = v.Interface().(error)
			}
		} else {
			value = v.Interface()
		}
	}
	return
}

func (r *nativeRemote) call(method string, args []json.RawMessage) (json.RawMessage, error) {
	body, err := json.Marshal(nativeRequest{r.session, method, args})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest("POST", r.address+"/rpc", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+r.token)
	request.Header.Set("Content-Type", "application/json")
	// AskNative waits for a user decision. Other calls have a bounded timeout.
	client := &http.Client{Timeout: 10 * time.Second}
	if method == "Command104Device" {
		client.Timeout = 75 * time.Second
	}
	if method == "AskNative" {
		client.Timeout = 0
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("主窗口已关闭")
	}
	var reply nativeReply
	if err = json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&reply); err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, errors.New(reply.Error)
	}
	return reply.Value, nil
}

func (a *App) CallNative(method string, args []json.RawMessage) (json.RawMessage, error) {
	if a.remote == nil {
		return nil, errors.New("not an editor window")
	}
	return a.remote.call(method, args)
}
func (a *App) GetNativeContext() (NativeContext, error) {
	if a.remote == nil {
		return NativeContext{Role: a.workspaceRole}, nil
	}
	raw, err := a.remote.call("GetNativeContext", nil)
	if err != nil {
		return NativeContext{}, err
	}
	var c NativeContext
	err = json.Unmarshal(raw, &c)
	return c, err
}
func (a *App) CloseNativeWindow() {
	if a.remote != nil {
		if reusableNativeWindow(a.remote.kind) {
			runtime.WindowHide(a.ctx)
			return
		}
		a.QuitNativeWindow()
	}
}
func isAutoWindow(kind string) bool {
	return kind == "auto-yk-dialog" || kind == "auto-yt-dialog" || kind == "auto-bulk-dialog"
}
func reusableNativeWindow(kind string) bool {
	return isAutoWindow(kind) || kind == "auto-data-dialog" || kind == "channel-dialog" || kind == "device-dialog"
}
func (a *App) QuitNativeWindow() {
	if a.remote != nil {
		a.remote.exiting.Store(true)
		runtime.Quit(a.ctx)
	}
}
func (a *App) ActivateNativeWindow() {
	if a.remote != nil {
		if reusableNativeWindow(a.remote.kind) && !isAutoWindow(a.remote.kind) {
			if c, err := a.GetNativeContext(); err == nil && c.Position != nil {
				runtime.WindowSetPosition(a.ctx, c.Position.X, c.Position.Y)
			}
		}
		runtime.WindowUnminimise(a.ctx)
		runtime.WindowShow(a.ctx)
	}
}

// Native activation must keep working after a window has been hidden for a
// long time; WebView background timers may be throttled by Chromium.
func (a *App) watchNativeWindow(ctx context.Context) {
	watch, cancel := context.WithCancel(ctx)
	a.nativeWatchCancel = cancel
	go func() {
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		activation := 0
		for {
			select {
			case <-watch.Done():
				return
			case <-ticker.C:
				c, err := a.GetNativeContext()
				if err != nil {
					a.QuitNativeWindow()
					return
				}
				if c.Activate != activation {
					activation = c.Activate
					a.ActivateNativeWindow()
				}
			}
		}
	}()
}
func (a *App) FitAutoWindow(clientWidth, clientHeight int) {
	if a.remote == nil || clientWidth < 100 || clientHeight < 100 {
		return
	}
	width, height := 0, 0
	switch a.remote.kind {
	case "auto-yk-dialog":
		width, height = 925, 402
	case "auto-yt-dialog":
		width, height = 516, 386
	default:
		return
	}
	outerWidth, outerHeight := runtime.WindowGetSize(a.ctx)
	outerWidth += width - clientWidth
	outerHeight += height - clientHeight
	if outerWidth < 100 || outerHeight < 100 || outerWidth > 4000 || outerHeight > 4000 {
		return
	}
	runtime.WindowSetMinSize(a.ctx, outerWidth, outerHeight)
	runtime.WindowSetSize(a.ctx, outerWidth, outerHeight)
}
func (a *App) FitConfigWindow(clientWidth, clientHeight, targetWidth, targetHeight int) {
	if a.remote == nil || (a.remote.kind != "channel-dialog" && a.remote.kind != "device-dialog") || clientWidth < 100 || clientHeight < 50 || targetWidth < 280 || targetWidth > 600 || targetHeight < 110 || targetHeight > 1000 {
		return
	}
	w, h := runtime.WindowGetSize(a.ctx)
	w, h = w+targetWidth-clientWidth, h+targetHeight-clientHeight
	if w < 280 || h < 110 || w > 1000 || h > 1500 {
		return
	}
	runtime.WindowSetMinSize(a.ctx, 280, 110)
	runtime.WindowSetSize(a.ctx, w, h)
}
func (a *App) SetNativeSelection(c NativeContext) {
	if a.native != nil {
		a.native.mu.Lock()
		a.native.selection = c
		a.native.mu.Unlock()
	}
}

func windowTitle(c NativeContext) string {
	switch c.Kind {
	case "auto-data-dialog":
		return "自动数据"
	case "auto-yk-dialog":
		return "自动遥控窗口"
	case "auto-yt-dialog":
		return "自动遥调窗口"
	case "auto-bulk-dialog":
		return "批量修改"
	case "control-dialog":
		if c.Point == "yk" {
			return "遥控操作"
		}
		return "遥调操作"
	case "channel-dialog":
		return "通信配置窗口"
	case "device-dialog":
		if c.Edit {
			return "查看/修改装置配置"
		}
		if c.Role == "server" {
			return "子站增加装置"
		}
		return "主站增加装置"
	case "events-dialog":
		return "SOE / COS 事件记录"
	case "point-dialog":
		return "信息体配置"
	case "manager-dialog":
		return "网络通道管理"
	case "confirm-dialog":
		var v struct {
			Title string `json:"title"`
		}
		_ = json.Unmarshal(c.Ask, &v)
		return v.Title
	}
	return "protocol-simulator"
}
func (n *nativeWindows) open(c NativeContext) (*nativeSession, error) {
	switch c.Kind {
	case "channel-dialog", "device-dialog", "point-dialog", "manager-dialog", "confirm-dialog", "control-dialog", "auto-yk-dialog", "auto-yt-dialog", "auto-bulk-dialog", "auto-data-dialog", "events-dialog":
	default:
		return nil, errors.New("无效窗口")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return nil, errors.New("应用已关闭")
	}
	for _, s := range n.sessions {
		v := s.context
		if c.Kind == "auto-data-dialog" && c.Generation != v.Generation {
			continue
		}
		if c.Kind != "confirm-dialog" && v.Kind == c.Kind && v.Role == c.Role && v.Edit == c.Edit && (c.Kind == "channel-dialog" || !c.Edit && c.Kind != "auto-data-dialog" || v.Channel == c.Channel && v.Device == c.Device) && v.Point == c.Point {
			s.context.Position = c.Position
			if c.Kind == "channel-dialog" {
				s.context.Channel = c.Channel
			}
			s.context.Activate++
			return s, nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	id := nonce()
	if c.Kind == "device-dialog" && !c.Edit {
		c.Draft = n.drafts[c.Role]
	}
	cmd := exec.Command(exe, "--gylab-native-window="+id)
	env := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GYLAB_NATIVE_") {
			env = append(env, e)
		}
	}
	cmd.Env = append(env, "GYLAB_NATIVE_ADDRESS="+n.address, "GYLAB_NATIVE_TOKEN="+n.token)
	s := &nativeSession{context: c, cmd: cmd, result: make(chan nativeResult, 1)}
	n.sessions[id] = s
	if err = cmd.Start(); err != nil {
		delete(n.sessions, id)
		return nil, fmt.Errorf("打开窗口: %w", err)
	}
	go func() {
		_ = cmd.Wait()
		n.mu.Lock()
		delete(n.sessions, id)
		n.mu.Unlock()
		select {
		case s.result <- nativeResult{Choice: "cancel"}:
		default:
		}
	}()
	return s, nil
}
func (a *App) OpenNativeWindow(c NativeContext) error {
	if a.native == nil {
		return errors.New("窗口服务未启动")
	}
	if c.Kind == "auto-data-dialog" {
		view, err := a.engine.GetAutoData(c.Channel, c.Device)
		if err != nil {
			return err
		}
		c.Generation = view.Generation
	}
	if a.ctx != nil && (c.Kind == "channel-dialog" || c.Kind == "device-dialog") {
		x, y := runtime.WindowGetPosition(a.ctx)
		c.Position = &nativePosition{X: x + 100, Y: y + 30}
	}
	_, err := a.native.open(c)
	return err
}
func (a *App) AskNative(options json.RawMessage) (nativeResult, error) {
	var v struct {
		Choices [][2]string `json:"choices"`
	}
	if err := json.Unmarshal(options, &v); err != nil {
		return nativeResult{}, err
	}
	if a.native == nil {
		return nativeResult{}, errors.New("窗口服务未启动")
	}
	s, err := a.native.open(NativeContext{Kind: "confirm-dialog", Ask: options})
	if err != nil {
		return nativeResult{}, err
	}
	return <-s.result, nil
}
func (n *nativeWindows) close() {
	n.mu.Lock()
	n.closed = true
	for _, s := range n.sessions {
		select {
		case s.result <- nativeResult{Choice: "cancel"}:
		default:
		}
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	}
	n.mu.Unlock()
	_ = n.server.Close()
}
