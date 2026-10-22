package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"gytest-go/internal/core"
)

const projectWindowArg = "--protocol-project="

type projectWindowConfig struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

// Startup and independent project windows use the same stopped-channel defaults.
func projectForWindow(config projectWindowConfig) (core.Project, error) {
	if strings.TrimSpace(config.Name) == "" || len([]rune(config.Name)) > 100 {
		return core.Project{}, errors.New("工程名称应为 1–100 字")
	}
	staged := core.NewEngine()
	if err := staged.EnsureWorkspaceChannel(config.Role); err != nil {
		return core.Project{}, err
	}
	project := staged.Snapshot().Project
	project.Name = config.Name
	return project, nil
}
func (a *App) initializeProjectWindow(config projectWindowConfig) error {
	project, err := projectForWindow(config)
	if err != nil {
		return err
	}
	if err = a.engine.ReplaceProjectAndStop(project, ""); err != nil {
		return err
	}
	a.workspaceRole = config.Role
	return nil
}
func projectConfigFromArgs(args []string) (*projectWindowConfig, error) {
	var config *projectWindowConfig
	for _, arg := range args {
		if !strings.HasPrefix(arg, projectWindowArg) {
			continue
		}
		if config != nil {
			return nil, errors.New("重复的工程启动参数")
		}
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(arg, projectWindowArg))
		if err != nil {
			return nil, err
		}
		config = &projectWindowConfig{}
		if err = json.Unmarshal(raw, config); err != nil {
			return nil, err
		}
		if _, err = projectForWindow(*config); err != nil {
			return nil, err
		}
	}
	return config, nil
}
func projectCommand(config projectWindowConfig) (*exec.Cmd, error) {
	if _, err := projectForWindow(config); err != nil {
		return nil, err
	}
	binary, err := os.Executable()
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	// A new main process has its own engine and popup service. It is deliberately
	// not registered as an owned editor, so closing this project cannot kill it.
	arg := projectWindowArg + base64.RawURLEncoding.EncodeToString(data)
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(binary)))
	if goruntime.GOOS == "darwin" && strings.HasSuffix(bundle, ".app") && filepath.Base(filepath.Dir(binary)) == "MacOS" {
		// Launch Services registers and activates a separate app instance. A raw
		// fork/exec does not reliably expose a second main window to macOS.
		return exec.Command("open", "-n", "-a", bundle, "--args", arg), nil
	}
	return exec.Command(binary, arg), nil
}
func (a *App) NewProject(name, role string) error {
	config := projectWindowConfig{Name: name, Role: role}
	if _, err := projectForWindow(config); err != nil {
		return err
	}
	if a.launchProject != nil {
		return a.launchProject(config)
	}
	cmd, err := projectCommand(config)
	if err != nil {
		return err
	}
	if goruntime.GOOS == "darwin" && filepath.Base(cmd.Path) == "open" {
		return cmd.Run()
	}
	if err = cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func (a *App) updateMainWindowTitle(role string) {
	if a.ctx == nil || a.remote != nil {
		return
	}
	if role == "" {
		role = a.workspaceRole
	}
	title := "protocol-simulator模拟主站"
	if role == "server" {
		title = "protocol-simulator模拟子站"
	}
	runtime.WindowSetTitle(a.ctx, a.engine.Snapshot().Project.Name+" - "+title)
}
