package core

import (
	"io"
	"net"
	"os"
	"time"

	"go.bug.st/serial"
)

// No idle read timeout: a disconnected client waits until stopped. Once the
// first byte arrives, partial frames must complete within the configured time.
type packetReader struct {
	io.Reader
	peer    *peer
	started bool
}

func (r *packetReader) Read(b []byte) (int, error) {
	n, err := r.Reader.Read(b)
	if n > 0 && !r.started {
		r.started = true
		timeout := time.Duration(r.peer.s.cfg.TimeoutMS) * time.Millisecond
		for _, d := range r.peer.s.e.devices(r.peer.s.cfg.ID) {
			if r.peer.s.cfg.Protocol == "iec104" || d.Unit == b[0] {
				key := "txt_overTime"
				if r.peer.s.cfg.Role == "server" {
					key = "txtTimeOut"
				}
				if v := number(d, key, 0); v > 0 {
					timeout = time.Duration(v) * time.Second
				}
				break
			}
		}
		if c, ok := r.peer.conn.(net.Conn); ok {
			_ = c.SetReadDeadline(time.Now().Add(timeout))
		}
		if c, ok := r.peer.conn.(interface{ SetReadTimeout(time.Duration) error }); ok {
			_ = c.SetReadTimeout(timeout)
		}
	}
	if n == 0 && err == nil && r.started {
		return 0, os.ErrDeadlineExceeded
	}
	return n, err
}
func (p *peer) clearReadDeadline() {
	if c, ok := p.conn.(net.Conn); ok {
		_ = c.SetReadDeadline(time.Time{})
	}
	if c, ok := p.conn.(interface{ SetReadTimeout(time.Duration) error }); ok {
		_ = c.SetReadTimeout(serial.NoTimeout)
	}
}
