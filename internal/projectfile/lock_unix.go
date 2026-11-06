//go:build !windows

package projectfile

import (
	"golang.org/x/sys/unix"
	"os"
	"syscall"
)

func lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, err
	}
	return func() { unix.Flock(int(f.Fd()), unix.LOCK_UN); f.Close() }, nil
}
func preserveOwner(f *os.File, info os.FileInfo) error {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return f.Chown(int(st.Uid), int(st.Gid))
	}
	return nil
}
