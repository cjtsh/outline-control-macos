package main

import (
	"encoding/json"
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
