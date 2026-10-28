// Package observe exposes bounded inspection and explicit data-point operations over an owner-only Unix socket.
package observe

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"gytest-go/internal/core"
)

type Log struct {
	Sequence uint64    `json:"sequence"`
	Time     time.Time `json:"time"`
	Level    string    `json:"level"`
	Message  string    `json:"message"`
	Fields   string    `json:"fields"`
}
type Info struct {
	PointOperations bool            `json:"pointOperations,omitempty"`
	PID             int             `json:"pid"`
	Started         time.Time       `json:"started"`
	View            core.Inspection `json:"view"`
	Logs            []Log           `json:"logs,omitempty"`
	LogSequence     uint64          `json:"logSequence"`
	LogMissed       uint64          `json:"logMissed"`
}
type Server struct {
	operationLogger func(string, ...any)
	engine          *core.Engine
	mu              sync.Mutex
	logs            []Log
	sequence        uint64
	started         time.Time
	socket          string
	listener        net.Listener
	http            *http.Server
}

func RuntimeDir() string {
	if dir := os.Getenv("PRS_RUNTIME_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "protocol-simulator")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("protocol-simulator-%d", os.Getuid()))
}
func Start(engine *core.Engine, dir string) (*Server, error) {
	if runtime.GOOS == "windows" {
		return nil, fmt.Errorf("local observation is supported on Unix servers")
	}
	if dir == "" {
		dir = RuntimeDir()
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
		return nil, fmt.Errorf("runtime directory must be a real directory without group/world write permission: %s", dir)
	}
	// The kernel enforces owner-only access to every socket, including under
	// systemd's shared runtime directory. Never remove stale or foreign sockets.
	id := make([]byte, 6)
	if _, err = rand.Read(id); err != nil {
		return nil, err
	}
	socket := filepath.Join(dir, fmt.Sprintf("%d-%s.sock", os.Getpid(), hex.EncodeToString(id)))
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(socket, 0600); err != nil {
		listener.Close()
		return nil, err
	}
	s := &Server{engine: engine, started: time.Now().UTC(), socket: socket, listener: listener}
	s.http = &http.Server{Handler: http.HandlerFunc(s.serve), ReadHeaderTimeout: time.Second, ReadTimeout: 2 * time.Second, WriteTimeout: 2 * time.Second, IdleTimeout: 2 * time.Second, MaxHeaderBytes: 4096}
	go s.http.Serve(&limitedListener{Listener: listener, slots: make(chan struct{}, 16)})
	return s, nil
}
func (s *Server) Socket() string { return s.socket }
func (s *Server) Close()         { s.http.Close(); s.listener.Close() }
func (s *Server) Log(level, message string, fields ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++
	data := fmt.Sprintf("%v", fields)
	if len(data) > 4096 {
		data = data[:4096]
	}
	s.logs = append(s.logs, Log{s.sequence, time.Now().UTC(), level, message, data})
	if len(s.logs) > 500 {
		copy(s.logs, s.logs[len(s.logs)-500:])
		s.logs = s.logs[:500]
	}
}
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/v1/point" {
		s.operatePoint(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "read-only endpoint", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path != "/v1/info" && r.URL.Path != "/v1/view" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	after, _ := strconv.ParseUint(q.Get("after"), 10, 64)
	filter := q.Get("filter")
	if len(filter) > 256 {
		http.Error(w, "filter too long", 400)
		return
	}
	request := core.InspectRequest{}
	if r.URL.Path == "/v1/view" {
		request = core.InspectRequest{Channel: q.Get("channel"), Device: q.Get("device"), Filter: filter, Offset: offset, Kind: q.Get("kind"), Order: q.Get("order"), After: after, Messages: true}
	}
	result := Info{PointOperations: true, PID: os.Getpid(), Started: s.started, View: s.engine.Inspect(request)}
	if r.URL.Path == "/v1/view" {
		logAfter, _ := strconv.ParseUint(q.Get("logAfter"), 10, 64)
		s.mu.Lock()
		result.LogSequence = s.sequence
		for i := max(0, len(s.logs)-100); i < len(s.logs); i++ {
			entry := s.logs[i]
			if entry.Sequence <= logAfter {
				continue
			}
			if len(result.Logs) == 0 && logAfter > 0 && entry.Sequence > logAfter+1 {
				result.LogMissed = entry.Sequence - logAfter - 1
			}
			result.Logs = append(result.Logs, entry)
		}
		s.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

type limitedListener struct {
	net.Listener
	slots chan struct{}
}

func (l *limitedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	select {
	case l.slots <- struct{}{}:
		return &limitedConn{Conn: c, release: func() { <-l.slots }}, nil
	default:
		c.Close()
		return nil, temporaryError{}
	}
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "observation client limit reached" }
func (temporaryError) Timeout() bool   { return false }
func (temporaryError) Temporary() bool { return true }

type limitedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitedConn) Close() error { err := c.Conn.Close(); c.once.Do(c.release); return err }

// Fetch owns no engine lifecycle. Closing it cannot stop a project.
func Fetch(ctx context.Context, socket, path string) (Info, error) {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 700 * time.Millisecond}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix"+path, nil)
	if err != nil {
		return Info{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return Info{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return Info{}, fmt.Errorf("observer status %d", response.StatusCode)
	}
	var info Info
	err = json.NewDecoder(http.MaxBytesReader(nil, response.Body, 4<<20)).Decode(&info)
	return info, err
}
func Discover(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sockets []string
	for _, entry := range entries {
		if len(sockets) >= 64 {
			break
		}
		pid, parseErr := strconv.Atoi(strings.SplitN(entry.Name(), "-", 2)[0])
		if parseErr == nil && processExists(pid) && strings.HasSuffix(entry.Name(), ".sock") && entry.Type()&os.ModeSocket != 0 {
			sockets = append(sockets, filepath.Join(dir, entry.Name()))
		}
	}
	return sockets, nil
}
