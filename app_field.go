package main

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"gytest-go/internal/core"
)

func (a *App) OpenServerFrames() ([]core.Message, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "打开服务器报文日志"})
	if err != nil || path == "" {
		return nil, err
	}
	return readServerFrames(path)
}

func readServerFrames(path string) ([]core.Message, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 32<<20 {
		return nil, errors.New("请选择不超过 32 MiB 的报文日志文件")
	}
	frames := make([]core.Message, 0)
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 1<<20)
	for line := 1; scan.Scan(); line++ {
		raw := strings.TrimSpace(scan.Text())
		if raw == "" {
			continue
		}
		var frame core.Message
		if err := json.Unmarshal([]byte(raw), &frame); err != nil {
			return nil, fmt.Errorf("第 %d 行不是有效的报文 JSON：%w", line, err)
		}
		_, err := hex.DecodeString(frame.Hex)
		if err != nil || frame.Hex == "" || (frame.Direction != "RX" && frame.Direction != "TX") {
			return nil, fmt.Errorf("第 %d 行的原始报文或收发方向无效", line)
		}
		frames = append(frames, frame)
	}
	return frames, scan.Err()
}

func (a *App) CheckServerDeployment(serverIP string, startRules bool) ([]string, error) {
	snapshot := a.engine.Snapshot()
	if err := core.NewEngine().ReplaceProject(snapshot.Project, snapshot.Path); err != nil {
		return nil, err
	}
	serverIP = strings.TrimSpace(serverIP)
	if serverIP != "" && net.ParseIP(serverIP) == nil {
		return nil, errors.New("请输入服务器网卡 IP")
	}
	results := []string{"工程配置校验通过"}
	if len(snapshot.Project.Channels) == 0 {
		return append(results, "当前工程没有通道"), nil
	}
	listeners := map[string][]string{}
	serials := map[string]bool{}
	for _, c := range snapshot.Project.Channels {
		name := c.Name
		switch c.Transport {
		case "serial":
			results = append(results, fmt.Sprintf("%s · 确认服务器串口路径及访问权限 %s", name, c.Address))
			if serials[c.Address] {
				results = append(results, name+" · 工程内重复使用串口 "+c.Address)
			}
			serials[c.Address] = true
		case "tcp-server":
			host, port, err := net.SplitHostPort(c.Address)
			if err != nil {
				return nil, err
			}
			results = append(results, fmt.Sprintf("%s · 监听 %s，确认服务器端口可用", name, c.Address))
			ip := net.ParseIP(host)
			wildcard := host == "" || ip != nil && ip.IsUnspecified()
			if wildcard || serverIP != "" && !net.ParseIP(serverIP).Equal(ip) {
				results = append(results, name+" · 核对监听 IP 与服务器网卡地址")
			}
			if ip != nil && ip.IsLoopback() {
				results = append(results, name+" · 回环监听仅供服务器本机连接")
			}
			for _, previous := range listeners[port] {
				p := net.ParseIP(previous)
				if previous == host || wildcard || previous == "" || p != nil && p.IsUnspecified() {
					results = append(results, name+" · 工程内监听端口冲突 "+port)
					break
				}
			}
			listeners[port] = append(listeners[port], host)
		case "tcp-client":
			results = append(results, fmt.Sprintf("%s · 目标 %s，确认服务器到目标可达", name, c.Address))
			if c.LocalAddress != "" {
				results = append(results, name+" · 核对本地绑定 "+c.LocalAddress)
			}
		}
		if c.Role != "server" {
			continue
		}
		for _, d := range c.Devices {
			for _, rule := range core.TelemetryConfiguration(d).Rules {
				if rule.Enabled {
					state := "需要启用 --start-data-rules"
					if startRules {
						state = "启动时授权自动数据规则"
					}
					results = append(results, name+" / "+d.Name+" · "+state)
					break
				}
			}
		}
	}
	return results, nil
}

var sshTarget = regexp.MustCompile(`^(?:[A-Za-z0-9_][A-Za-z0-9_.-]*@)?(?:[A-Za-z0-9_][A-Za-z0-9_.-]*|\[[A-Fa-f0-9:]+\])$`)

// ListSSHHosts offers explicit aliases from the user's OpenSSH config.
func (a *App) ListSSHHosts() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(home, ".ssh", "config"))
	hosts := []string{}
	if errors.Is(err, os.ErrNotExist) {
		return hosts, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	seen := make(map[string]bool)
	lines := bufio.NewScanner(file)
	for lines.Scan() {
		line, _, _ := strings.Cut(lines.Text(), "#")
		line = strings.Replace(strings.TrimSpace(line), "=", " ", 1)
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "Host") {
			continue
		}
		for _, host := range fields[1:] {
			host = strings.Trim(host, `"'`)
			key := strings.ToLower(host)
			if len(host) <= 255 && !strings.Contains(host, "@") && sshTarget.MatchString(host) && !seen[key] {
				seen[key] = true
				hosts = append(hosts, host)
			}
		}
	}
	sort.Strings(hosts)
	return hosts, lines.Err()
}

func (a *App) InspectServerSSH(target string) (json.RawMessage, error) {
	target = strings.TrimSpace(target)
	if len(target) > 255 || !sshTarget.MatchString(target) {
		return nil, errors.New("请输入 SSH 主机别名或 user@host")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=yes", "-o", "ConnectTimeout=5", target, `if command -v prs >/dev/null 2>&1; then exec prs inspect; elif [ -x "$HOME/.local/bin/prs" ]; then exec "$HOME/.local/bin/prs" inspect; else exec "$HOME/.local/share/protocol-simulator/bin/prs" inspect; fi`)
	data, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("SSH 查看超时")
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("SSH 查看失败：%s", strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, err
	}
	if !json.Valid(data) {
		return nil, errors.New("服务器没有返回有效的工程状态")
	}
	return json.RawMessage(data), nil
}
