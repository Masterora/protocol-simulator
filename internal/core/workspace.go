package core

import "errors"

// EnsureWorkspaceChannel prepares the selected station without connecting it.
// Existing channels, including those loaded from a project, are left intact.
func (e *Engine) EnsureWorkspaceChannel(role string) error {
	if role != "client" && role != "server" {
		return errors.New("无效工作模式")
	}
	e.op.Lock()
	defer e.op.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, c := range e.project.Channels {
		if c.Role == role {
			return nil
		}
	}
	if len(e.project.Channels) >= 64 {
		return errors.New("最多支持 64 个通道")
	}
	c := Channel{ID: newID(), Name: "通道1", Protocol: "raw", Role: role, Transport: "tcp-client", Address: "127.0.0.1:2404"}
	if role == "server" {
		c.Transport, c.Address = "tcp-server", "0.0.0.0:2404"
	}
	c.Defaults()
	if err := c.Validate(); err != nil {
		return err
	}
	if len(e.project.Channels) > 0 || e.path != "" {
		e.dirty = true
	}
	e.project.Channels = append(e.project.Channels, c)
	return nil
}
