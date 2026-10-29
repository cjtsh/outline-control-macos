package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
)

// Mode is how captured traffic reaches the exit.
//
//	socks  — a local SOCKS5 proxy; apps must opt in.
//	tunnel — a full-device VPN: a utun device takes the default route and
//	         sing-box forwards everything to the exit.
type Mode string

const (
	ModeSocks  Mode = "socks"
	ModeTunnel Mode = "tunnel"
)

// TunOptions are the knobs the tunnel mode needs, all of them decided by the
// daemon's flags so the GUI cannot invent routing policy.
type TunOptions struct {
	// Exclude hosts are resolved and dialled directly, never through the exit.
	// The relay lives here: the control channel must survive a dead VPN.
	ExcludeHosts []string
	// ExcludeCIDRs are dialled directly as well.
	ExcludeCIDRs []string
	// ExcludePorts narrows ExcludeCIDRs to those destination ports. Without it
	// a whole address is outside the tunnel, which breaks every other service
	// hosted there — including the ones the VPN exists to reach.
	ExcludePorts []int
	// ExcludeDomains covers DNS for those hosts plus their subdomains.
	ExcludeDomains []string
	MTU            int
}

var ErrNotRoot = errors.New("tunnel mode needs the privileged daemon (see install/install-tunnel.sh)")

// findSingbox looks for the tunnel binary in the usual places.
func findSingbox() string {
	if p := strings.TrimSpace(os.Getenv("OUTLINE_CONTROL_SINGBOX")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, p := range []string{
		"/opt/homebrew/bin/sing-box",
		"/usr/local/bin/sing-box",
		"/opt/homebrew/opt/sing-box/bin/sing-box",
		"/usr/local/opt/sing-box/bin/sing-box",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("sing-box"); err == nil {
		return p
	}
	return ""
}

// systemResolvers reads the resolvers macOS is using right now, so the tunnel
// can keep resolving names for excluded hosts without asking the exit to do it.
func systemResolvers() []string {
	out := []string{}
	data, err := os.ReadFile("/etc/resolv.conf")
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "nameserver" {
				if ip := net.ParseIP(fields[1]); ip != nil {
					out = append(out, ip.String())
				}
			}
		}
	}
	if len(out) == 0 {
		out = append(out, "1.1.1.1")
	}
	return out
}

// tunConfig renders the sing-box configuration for one exit. It is a pure
// function of the exit and the options, which keeps the routing policy testable
// and keeps the daemon from ever writing a config a human did not ask for.
func tunConfig(srv Server, opt TunOptions) ([]byte, error) {
	if srv.Host == "" || srv.Port == "" {
		return nil, fmt.Errorf("exit is missing an address")
	}
	mtu := opt.MTU
	if mtu == 0 {
		mtu = 1500
	}

	resolvers := systemResolvers()
	localServers := make([]any, 0, len(resolvers))
	// Resolver traffic stays out of the tunnel, always: routing DNS through the
	// proxy to bootstrap the proxy is the classic chicken-and-egg.
	resolverCIDRs := make([]string, 0, len(resolvers))
	for _, ns := range resolvers {
		localServers = append(localServers, map[string]any{
			"tag":    "local-" + strings.ReplaceAll(ns, ":", "-"),
			"type":   "udp",
			"server": ns,
		})
		resolverCIDRs = append(resolverCIDRs, ns+"/32")
	}

	remote := map[string]any{
		"type":   "https",
		"tag":    "remote",
		"server": "1.1.1.1",
		"detour": "proxy",
	}
	servers := append([]any{remote}, localServers...)

	dnsRules := []any{}
	for _, d := range opt.ExcludeDomains {
		dnsRules = append(dnsRules, map[string]any{
			"domain_suffix": []string{d},
			"action":        "route",
			"server":        "local-" + strings.ReplaceAll(resolvers[0], ":", "-"),
		})
	}

	routeRules := []any{}
	if len(resolverCIDRs) > 0 {
		routeRules = append(routeRules, map[string]any{
			"ip_cidr":  resolverCIDRs,
			"outbound": "direct",
		})
	}
	if len(opt.ExcludeCIDRs) > 0 {
		rule := map[string]any{"ip_cidr": opt.ExcludeCIDRs, "outbound": "direct"}
		if len(opt.ExcludePorts) > 0 {
			rule["port"] = opt.ExcludePorts
		}
		routeRules = append(routeRules, rule)
	}
	for _, d := range opt.ExcludeDomains {
		routeRules = append(routeRules, map[string]any{
			"domain_suffix": []string{d},
			"outbound":      "direct",
		})
	}
	routeRules = append(routeRules, map[string]any{
		"ip_is_private": true,
		"outbound":      "direct",
	})

	cfg := map[string]any{
		"log": map[string]any{"level": "warn", "timestamp": true},
		"dns": map[string]any{
			"servers": servers,
			"rules":   dnsRules,
			"final":   "remote",
		},
		"inbounds": []any{
			map[string]any{
				"type":         "tun",
				"tag":          "tun-in",
				"address":      []string{"172.19.0.1/30"},
				"mtu":          mtu,
				"auto_route":   true,
				"strict_route": true,
			},
		},
		"outbounds": []any{
			map[string]any{
				"type":        "shadowsocks",
				"tag":         "proxy",
				"server":      srv.Host,
				"server_port": atoiOr(srv.Port, 0),
				"method":      srv.Method,
				"password":    srv.Password,
			},
			map[string]any{"type": "direct", "tag": "direct"},
		},
		"route": map[string]any{
			"rules":                   routeRules,
			"final":                   "proxy",
			"auto_detect_interface":   true,
			"default_domain_resolver": "local-" + strings.ReplaceAll(resolvers[0], ":", "-"),
		},
	}
	return json.MarshalIndent(cfg, "", "  ")
}

func atoiOr(s string, fallback int) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return fallback
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return fallback
	}
	return n
}
