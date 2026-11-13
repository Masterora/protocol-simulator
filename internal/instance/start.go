// Package instance starts an independent server without tying it to a TUI lifetime.
package instance

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"gytest-go/internal/observe"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Started struct {
	PID     int          `json:"pid"`
	Socket  string       `json:"socket"`
	LogPath string       `json:"logPath"`
	Info    observe.Info `json:"-"`
}

func Start(ctx context.Context, path, dir string) (Started, error) {
	var result Started
	if err := ctx.Err(); err != nil {
		return result, err
	}
	binary, err := os.Executable()
	if err != nil {
		return result, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return result, err
	}
	if dir == "" {
		dir = observe.RuntimeDir()
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return result, err
	}
	state := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(state) {
		home, err := os.UserHomeDir()
		if err != nil {
			return result, err
		}
		state = filepath.Join(home, ".local", "state")
	}
	logs := filepath.Join(state, "protocol-simulator", "server-logs")
	if err = os.MkdirAll(logs, 0700); err != nil {
		return result, err
	}
	hash := sha256.Sum256([]byte(absolute))
	logPath := filepath.Join(logs, fmt.Sprintf("%x.log", hash[:8]))
	log, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return result, err
	}
	defer log.Close()
	cmd := exec.Command(binary, "run", absolute, "--channels", "none", "--start-data-rules", "--runtime-dir", dir, "--log-format", "json")
	cmd.Stdout, cmd.Stderr = log, log
	detach(cmd)
	if err = cmd.Start(); err != nil {
		return result, err
	}
	result = Started{PID: cmd.Process.Pid, LogPath: logPath}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("工程已保存，后台启动结果未确认，请查看 %s：%w", logPath, ctx.Err())
		case err := <-done:
			return result, fmt.Errorf("工程已保存，服务启动失败（%v），请查看 %s", err, logPath)
		case <-deadline.C:
			return result, errors.New("工程已保存，后台启动尚未确认，请查看 " + logPath)
		case <-tick.C:
			sockets, _ := observe.Discover(dir)
			for _, socket := range sockets {
				if !strings.HasPrefix(filepath.Base(socket), fmt.Sprint(result.PID)+"-") {
					continue
				}
				info, err := observe.Fetch(ctx, socket, "/v1/info")
				if err == nil && info.PID == result.PID && info.View.Path == absolute && info.ConfigurationOperations {
					result.Socket, result.Info = socket, info
					return result, nil
				}
			}
		}
	}
}
