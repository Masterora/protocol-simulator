package runner

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"gytest-go/internal/core"
	"gytest-go/internal/observe"
)

type rotatingLog struct {
	path        string
	limit, size int64
	files       int
	file        *os.File
}

func regularFile(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("log path must be a regular file: %s", path)
	}
	return nil
}

func openFrames(path, project string, limit int64, files int) (*rotatingLog, error) {
	if limit < 1 || files < 1 || files > 10 {
		return nil, errors.New("invalid frame log limits")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	source, err := os.Stat(project)
	if err != nil {
		return nil, err
	}
	for i := 0; i < files; i++ {
		candidate := path
		if i > 0 {
			candidate = fmt.Sprintf("%s.%d", path, i)
		}
		if err = regularFile(candidate); err != nil {
			return nil, err
		}
		if info, e := os.Stat(candidate); e == nil && os.SameFile(info, source) {
			return nil, errors.New("frame logs must not overwrite the project")
		}
		absolute, _ := filepath.Abs(project)
		if filepath.Clean(candidate) == absolute {
			return nil, errors.New("frame logs must not overwrite the project")
		}
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	w := &rotatingLog{path: path, limit: limit, files: files}
	err = w.open()
	return w, err
}
func (w *rotatingLog) open() error {
	if err := regularFile(w.path); err != nil {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	w.file, w.size = file, info.Size()
	return nil
}
func (w *rotatingLog) Write(data []byte) error {
	if int64(len(data)) > w.limit {
		return errors.New("frame log record exceeds file size limit")
	}
	if w.size+int64(len(data)) > w.limit {
		if err := w.file.Close(); err != nil {
			return err
		}
		for i := w.files - 1; i >= 0; i-- {
			old := w.path
			if i > 0 {
				old = fmt.Sprintf("%s.%d", w.path, i)
			}
			if err := regularFile(old); err != nil {
				return err
			}
			if i == w.files-1 {
				if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
					return err
				}
			} else {
				next := fmt.Sprintf("%s.%d", w.path, i+1)
				if err := os.Rename(old, next); err != nil && !os.IsNotExist(err) {
					return err
				}
			}
		}
		if err := w.open(); err != nil {
			return err
		}
	}
	n, err := w.file.Write(data)
	w.size += int64(n)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}

type logRecord struct {
	level   slog.Level
	message string
	values  []any
	frame   *core.Message
}
type logSink struct {
	queue    chan logRecord
	done     chan struct{}
	errors   chan error
	dropped  atomic.Uint64
	mu       sync.Mutex
	closed   bool
	observer *observe.Server
}

type checkedWriter struct {
	writer io.Writer
	errors chan error
}

func (w checkedWriter) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		select {
		case w.errors <- fmt.Errorf("runtime log: %w", err):
		default:
		}
	}
	return n, err
}

func newLogSink(out io.Writer, format string, frames *rotatingLog) *logSink {
	s := &logSink{queue: make(chan logRecord, 1024), done: make(chan struct{}), errors: make(chan error, 1)}
	checked := checkedWriter{out, s.errors}
	var handler slog.Handler = slog.NewTextHandler(checked, nil)
	if format == "json" {
		handler = slog.NewJSONHandler(checked, nil)
	}
	logger := slog.New(handler)
	go func() {
		defer close(s.done)
		if frames != nil {
			defer func() { frames.file.Close() }()
		}
		reportDrops := func() {
			if n := s.dropped.Swap(0); n > 0 {
				logger.Warn("log_records_dropped", "count", n)
			}
		}
		for record := range s.queue {
			reportDrops()
			if record.frame == nil {
				logger.Log(nil, record.level, record.message, record.values...)
				continue
			}
			if frames == nil {
				continue
			}
			data, err := json.Marshal(struct {
				Time time.Time `json:"time"`
				core.Message
			}{time.Now().UTC(), *record.frame})
			if err == nil {
				err = frames.Write(append(data, '\n'))
			}
			if err != nil {
				select {
				case s.errors <- fmt.Errorf("frame log: %w", err):
				default:
				}
				return
			}
		}
		reportDrops()
	}()
	return s
}
func (s *logSink) emit(record logRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.observer != nil && record.frame == nil {
		s.observer.Log(record.level.String(), record.message, record.values...)
	}
	select {
	case s.queue <- record:
	default:
		s.dropped.Add(1)
	}
}
func (s *logSink) info(message string, values ...any) {
	s.emit(logRecord{message: message, values: values})
}
func (s *logSink) close() error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.queue)
	}
	s.mu.Unlock()
	select {
	case <-s.done:
		select {
		case err := <-s.errors:
			return err
		default:
			return nil
		}
	case <-time.After(2 * time.Second):
		return errors.New("log flush timed out")
	}
}
