//go:build !windows

package instance

import (
	"os/exec"
	"syscall"
)

func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
