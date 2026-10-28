// Package netinfo discovers local IPv4 addresses.
package netinfo

import (
	"net"
	"sort"
)

// Addr is a usable local IPv4 address.
type Addr struct {
	Interface string `json:"interface"`
	IP        string `json:"ip"`
	Private   bool   `json:"private"`
}

// LocalIPv4 lists up, non-loopback, non-link-local IPv4 addresses, private
// (RFC1918) ones first. It is called on demand; nothing polls for changes.
func LocalIPv4() []Addr {
	var out []Addr
	ifs, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipn.IP.To4()
			if ip == nil || ip.IsLinkLocalUnicast() || ip.IsLoopback() {
				continue
			}
			out = append(out, Addr{Interface: ifc.Name, IP: ip.String(), Private: ip.IsPrivate()})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Private && !out[j].Private })
	return out
}
