package core

import (
	"net"
	"testing"
)

func TestDefaultListenerUsesAnExplicitLocalAddress(t *testing.T) {
	addresses := LocalIPv4()
	if len(addresses) == 0 || addresses[len(addresses)-1] != "127.0.0.1" {
		t.Fatal("missing loopback fallback", addresses)
	}
	seen := map[string]bool{}
	for _, address := range addresses {
		ip := net.ParseIP(address)
		if ip == nil || ip.IsUnspecified() || seen[address] {
			t.Fatal("invalid interface choice", addresses)
		}
		seen[address] = true
	}
	e := NewEngine()
	if err := e.EnsureWorkspaceChannel("server"); err != nil {
		t.Fatal(err)
	}
	c := e.Snapshot().Project.Channels[0]
	host, _, err := net.SplitHostPort(c.Address)
	if err != nil || !seen[host] || host == "0.0.0.0" {
		t.Fatal(c.Address, err)
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort(host, "0"))
	if err != nil {
		t.Fatal("chosen address cannot bind on this host", err)
	}
	listener.Close()
}
