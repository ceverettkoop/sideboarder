package main

import (
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strings"
)

// Tailscale assigns node addresses from these ranges.
var (
	tailnetV4 = netip.MustParsePrefix("100.64.0.0/10")
	tailnetV6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

// tailscaleIP finds this machine's tailnet address: first by scanning the
// network interfaces, then by asking the tailscale CLI.
func tailscaleIP() (netip.Addr, error) {
	var v6 netip.Addr
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(ipnet.IP)
			if !ok {
				continue
			}
			ip = ip.Unmap()
			if tailnetV4.Contains(ip) {
				return ip, nil
			}
			if tailnetV6.Contains(ip) && !v6.IsValid() {
				v6 = ip
			}
		}
	}
	if v6.IsValid() {
		return v6, nil
	}
	if out, err := exec.Command("tailscale", "ip", "-4").Output(); err == nil {
		if ip, err := netip.ParseAddr(strings.TrimSpace(strings.Split(string(out), "\n")[0])); err == nil {
			return ip, nil
		}
	}
	return netip.Addr{}, fmt.Errorf("no Tailscale address found (is tailscale up?)")
}

// resolveListenAddr turns "tailscale:PORT" into the tailnet IP and port;
// any other host:port is used as given.
func resolveListenAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	if host != "tailscale" {
		return addr, nil
	}
	ip, err := tailscaleIP()
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(ip.String(), port), nil
}
