package core

import "net"

// LocalIPv4 lists explicit addresses on active interfaces. Loopback remains a
// usable fallback and is ordered after addresses reachable from the network.
func LocalIPv4() []string {
	result := []string{}
	seen := map[string]bool{"127.0.0.1": true}
	interfaces, _ := net.Interfaces()
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, _ := iface.Addrs()
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil || ip.To4() == nil || !ip.IsGlobalUnicast() || seen[ip.String()] {
				continue
			}
			seen[ip.String()] = true
			result = append(result, ip.String())
		}
	}
	return append(result, "127.0.0.1")
}

func DefaultListenIP() string { return LocalIPv4()[0] }
