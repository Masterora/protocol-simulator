//go:build !windows

package observe

import "golang.org/x/sys/unix"

func processExists(pid int) bool { return pid > 0 && unix.Kill(pid, 0) != unix.ESRCH }
