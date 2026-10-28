package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFrameRotationBoundedAndProjectProtected(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project.json")
	os.WriteFile(project, []byte(`{"project":true}`), 0600)
	if _, err := openFrames(project, project, 128, 3); err == nil {
		t.Fatal("project accepted as log")
	}
	path := filepath.Join(dir, "frames.jsonl")
	log, err := openFrames(path, project, 128, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if err = log.Write([]byte("{\"hex\":\"0123456789abcdef0123456789abcdef\"}\n")); err != nil {
			t.Fatal(err)
		}
	}
	log.file.Close()
	matches, _ := filepath.Glob(path + "*")
	if len(matches) != 3 {
		t.Fatal(matches)
	}
	for _, p := range matches {
		data, _ := os.ReadFile(p)
		if len(data) > 128 {
			t.Fatal("size exceeded")
		}
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
			if !json.Valid(line) {
				t.Fatal(string(line))
			}
		}
	}
	data, _ := os.ReadFile(project)
	if string(data) != `{"project":true}` {
		t.Fatal("project changed")
	}
}

type blockedWriter struct {
	entered chan struct{}
	release chan struct{}
	out     bytes.Buffer
}

func (w *blockedWriter) Write(data []byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}
	<-w.release
	return w.out.Write(data)
}
func TestSlowLogConsumerDropsWithoutBlockingProducers(t *testing.T) {
	w := &blockedWriter{entered: make(chan struct{}, 1), release: make(chan struct{})}
	sink := newLogSink(w, "json", nil)
	sink.info("first")
	<-w.entered
	start := time.Now()
	for i := 0; i < 2000; i++ {
		sink.info("pressure", "i", i)
	}
	if time.Since(start) > time.Second || sink.dropped.Load() == 0 {
		t.Fatal("log consumer blocked producers")
	}
	close(w.release)
	if err := sink.close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.out.String(), "log_records_dropped") {
		t.Fatal("loss not reported")
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk unavailable") }
func TestRuntimeLogWriteFailureIsReported(t *testing.T) {
	sink := newLogSink(failingWriter{}, "json", nil)
	sink.info("start")
	select {
	case <-sink.errors:
	case <-time.After(time.Second):
		t.Fatal("write failure ignored")
	}
	if err := sink.close(); err != nil {
		t.Fatal(err)
	}
}
