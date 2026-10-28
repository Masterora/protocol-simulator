//go:build !windows

package tui

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
)

func readKeys(ctx context.Context, in *os.File, keys chan<- byte) {
	defer close(keys)
	descriptors := []unix.PollFd{{Fd: int32(in.Fd()), Events: unix.POLLIN}}
	for ctx.Err() == nil {
		n, err := unix.Poll(descriptors, 100)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return
		}
		if n == 0 {
			continue
		}
		if descriptors[0].Revents&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			return
		}
		var buffer [64]byte
		count, err := unix.Read(int(in.Fd()), buffer[:])
		if err != nil || count == 0 {
			return
		}
		for _, b := range buffer[:count] {
			select {
			case keys <- b:
			case <-ctx.Done():
				return
			}
		}
	}
}
