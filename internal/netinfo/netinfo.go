// Package netinfo discovers addresses the panel is reachable on, for the
// first-run banner and the setup screen (homelab: usually a bare LAN IP).
package netinfo

import (
	"net"
	"strings"
)

// Address is one reachable IP with a hint about where it comes from.
type Address struct {
	IP        string `json:"ip"`
	Interface string `json:"interface"`
	// Kind is "lan", "tailscale" or "other".
	Kind string `json:"kind"`
}

// tailscaleCGNAT is the 100.64.0.0/10 range Tailscale hands out.
var tailscaleCGNAT = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// LocalAddresses lists IPv4 addresses on interfaces that are up and not loopback.
func LocalAddresses() ([]Address, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []Address
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil || ip4.IsLinkLocalUnicast() {
				continue
			}
			out = append(out, Address{IP: ip4.String(), Interface: iface.Name, Kind: classify(iface.Name, ip4)})
		}
	}
	return out, nil
}

func classify(ifaceName string, ip net.IP) string {
	if strings.HasPrefix(ifaceName, "tailscale") || tailscaleCGNAT.Contains(ip) {
		return "tailscale"
	}
	if ip.IsPrivate() {
		return "lan"
	}
	return "other"
}

// URLs renders http(s)://ip:port for each address. listen is the configured
// listen address; when it binds a specific IP only that URL is returned.
func URLs(listen string, tls bool, addrs []Address) []string {
	scheme := "http"
	if tls {
		scheme = "https"
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return nil
	}
	if host != "" && host != "0.0.0.0" && host != "::" {
		return []string{scheme + "://" + net.JoinHostPort(host, port)}
	}
	urls := make([]string, 0, len(addrs)+1)
	for _, a := range addrs {
		urls = append(urls, scheme+"://"+net.JoinHostPort(a.IP, port))
	}
	if len(urls) == 0 {
		urls = append(urls, scheme+"://"+net.JoinHostPort("127.0.0.1", port))
	}
	return urls
}
