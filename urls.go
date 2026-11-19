package main

import (
	"net"
	"strconv"
	"strings"
)

// accessURLs lists the addresses the page can be opened at, for the addresses Ostler listens on.
// A wildcard bind (0.0.0.0 or ::) stands for this machine and every network address it has, so those
// are listed one by one: the user needs something they can type on another device.
func accessURLs(binds []string, port int) []string {
	var out []string
	seen := map[string]bool{}
	add := func(host string) {
		u := "http://" + net.JoinHostPort(host, strconv.Itoa(port))
		if !seen[u] {
			seen[u] = true
			out = append(out, u)
		}
	}
	for _, b := range binds {
		b = strings.Trim(strings.TrimSpace(b), "[]")
		ip := net.ParseIP(b)
		switch {
		case b == "":
		case ip != nil && ip.IsUnspecified():
			add("localhost")
			for _, a := range lanAddrs() {
				add(a)
			}
		case b == "localhost" || (ip != nil && ip.IsLoopback()):
			add("localhost")
		default:
			add(b)
		}
	}
	return out
}

// lanAddrs returns this machine's private-network IPv4 addresses, the ones a phone on the same Wi-Fi can reach.
func lanAddrs() []string {
	var out []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ip := n.IP.To4(); ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			out = append(out, ip.String())
		}
	}
	return out
}
