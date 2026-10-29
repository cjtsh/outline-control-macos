package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("generated config is not valid JSON: %v", err)
	}
	return cfg
}

func TestTunConfigRendersTheExit(t *testing.T) {
	srv := Server{ID: "abc", Name: "Apple", Host: "192.0.2.10", Port: "16014",
		Method: "chacha20-ietf-poly1305", Password: "s3cret"}

	cfg := decode(t, mustConfig(t, srv, TunOptions{}))

	outbounds := cfg["outbounds"].([]any)
	proxy := outbounds[0].(map[string]any)
	if proxy["type"] != "shadowsocks" || proxy["tag"] != "proxy" {
		t.Fatalf("first outbound should be the shadowsocks proxy, got %v", proxy)
	}
	if proxy["server"] != "192.0.2.10" || proxy["method"] != "chacha20-ietf-poly1305" || proxy["password"] != "s3cret" {
		t.Fatalf("proxy does not carry the exit credentials: %v", proxy)
	}
	if port, _ := proxy["server_port"].(float64); int(port) != 16014 {
		t.Fatalf("server_port = %v, want 16014", proxy["server_port"])
	}

	inbound := cfg["inbounds"].([]any)[0].(map[string]any)
	if inbound["type"] != "tun" || inbound["auto_route"] != true {
		t.Fatalf("inbound should be an auto-routing tun device, got %v", inbound)
	}
	if route := cfg["route"].(map[string]any); route["final"] != "proxy" {
		t.Fatalf("unmatched traffic must go to the proxy, got %v", route["final"])
	}
	if dns := cfg["dns"].(map[string]any); dns["final"] != "remote" {
		t.Fatalf("unmatched DNS must go through the exit, got %v", dns["final"])
	}
}

func TestTunConfigExcludesTheRelay(t *testing.T) {
	srv := Server{ID: "abc", Name: "Apple", Host: "192.0.2.10", Port: "16014",
		Method: "chacha20-ietf-poly1305", Password: "s3cret"}
	cfg := decode(t, mustConfig(t, srv, TunOptions{
		ExcludeCIDRs:   []string{"203.0.113.10/32"},
		ExcludeDomains: []string{"relay.example.com"},
	}))

	direct := map[string]bool{}
	for _, raw := range cfg["route"].(map[string]any)["rules"].([]any) {
		rule := raw.(map[string]any)
		if rule["outbound"] != "direct" {
			continue
		}
		for _, c := range anyList(rule["ip_cidr"]) {
			direct[c] = true
		}
		for _, d := range anyList(rule["domain_suffix"]) {
			direct[d] = true
		}
	}
	if !direct["203.0.113.10/32"] {
		t.Fatal("the relay address must be routed directly, or a dead VPN kills the control channel")
	}
	if !direct["relay.example.com"] {
		t.Fatal("the relay hostname must be routed directly")
	}

	// The relay's own DNS has to be answered locally as well.
	var localForRelay bool
	for _, raw := range cfg["dns"].(map[string]any)["rules"].([]any) {
		rule := raw.(map[string]any)
		for _, d := range anyList(rule["domain_suffix"]) {
			if d == "relay.example.com" && rule["server"] != "remote" {
				localForRelay = true
			}
		}
	}
	if !localForRelay {
		t.Fatal("relay DNS must not be resolved through the exit")
	}
}

func TestTunConfigRejectsAnEmptyExit(t *testing.T) {
	if _, err := tunConfig(Server{}, TunOptions{}); err == nil {
		t.Fatal("a server without a host must not produce a config")
	}
}

func mustConfig(t *testing.T, srv Server, opt TunOptions) []byte {
	t.Helper()
	raw, err := tunConfig(srv, opt)
	if err != nil {
		t.Fatalf("tunConfig: %v", err)
	}
	return raw
}

func anyList(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// A whole address excluded from the tunnel takes every other service on that
// host with it — which is how a Vaultwarden behind the same VPS as the relay
// became unreachable. Exclusions that only need one port must say so.
func TestTunConfigNarrowsExclusionsToPorts(t *testing.T) {
	srv := Server{Name: "Apple", Host: "198.51.100.7", Port: "16014", Method: "chacha20-ietf-poly1305", Password: "secret"}
	cfg, err := tunConfig(srv, TunOptions{
		ExcludeHosts:   []string{"relay.example.com"},
		ExcludeDomains: []string{"relay.example.com"},
		ExcludeCIDRs:   []string{"203.0.113.10/32"},
		ExcludePorts:   []int{58371},
		MTU:            1500,
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	var parsed struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal(cfg, &parsed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var relayRule, resolverRule map[string]any
	for _, rule := range parsed.Route.Rules {
		cidrs, _ := rule["ip_cidr"].([]any)
		for _, raw := range cidrs {
			if raw == "203.0.113.10/32" {
				relayRule = rule
			}
			if strings.HasSuffix(raw.(string), "/32") && rule["port"] == nil && raw != "203.0.113.10/32" {
				resolverRule = rule
			}
		}
	}
	if relayRule == nil {
		t.Fatal("no rule for the excluded relay address")
	}
	ports, _ := relayRule["port"].([]any)
	if len(ports) != 1 || ports[0].(float64) != 58371 {
		t.Fatalf("relay exclusion should be limited to port 58371, got %v", relayRule["port"])
	}
	if resolverRule == nil {
		t.Fatal("DNS resolvers must be routed direct without a port restriction")
	}
	// The domain rule stays broad: the relay hostname is only used for the
	// control channel.
	found := false
	for _, rule := range parsed.Route.Rules {
		if suffix, ok := rule["domain_suffix"].([]any); ok {
			for _, raw := range suffix {
				if raw == "relay.example.com" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("the relay hostname should still be excluded by domain")
	}
}

func TestSSLocalConfigListensLocally(t *testing.T) {
	srv := Server{ID: "abc", Name: "Apple", Host: "192.0.2.10", Port: "16014",
		Method: "chacha20-ietf-poly1305", Password: "s3cret"}

	raw, err := sslocalConfig(srv, "127.0.0.1:11080")
	if err != nil {
		t.Fatalf("sslocalConfig: %v", err)
	}
	cfg := decode(t, raw)

	if cfg["server"] != "192.0.2.10" || cfg["method"] != "chacha20-ietf-poly1305" || cfg["password"] != "s3cret" {
		t.Fatalf("config does not carry the exit credentials: %v", cfg)
	}
	if port, _ := cfg["server_port"].(float64); int(port) != 16014 {
		t.Fatalf("server_port = %v, want 16014", cfg["server_port"])
	}
	if cfg["local_address"] != "127.0.0.1" {
		t.Fatalf("local_address = %v, want 127.0.0.1", cfg["local_address"])
	}
	if port, _ := cfg["local_port"].(float64); int(port) != 11080 {
		t.Fatalf("local_port = %v, want 11080", cfg["local_port"])
	}
}

func TestSSLocalConfigRefusesBadAddresses(t *testing.T) {
	srv := Server{ID: "abc", Name: "Apple", Host: "192.0.2.10", Port: "http",
		Method: "chacha20-ietf-poly1305", Password: "s3cret"}

	if _, err := sslocalConfig(srv, "127.0.0.1:11080"); err == nil {
		t.Fatal("a non-numeric exit port must be refused, not sent to sslocal")
	}
	srv.Port = "16014"
	if _, err := sslocalConfig(srv, "no-address"); err == nil {
		t.Fatal("a local proxy address without a port must be refused")
	}
}
