package main

import "testing"

// The key shape Outline's own client emits, including the ?outline=1 query and
// the fragment that carries the display name.
const outlineKey = "ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTp0ZXN0LXBhc3N3b3Jk@198.51.100.7:16014/?outline=1#Apple"

func TestParseAccessKeyOutlineShape(t *testing.T) {
	key, err := ParseAccessKey(outlineKey)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if key.Method != "chacha20-ietf-poly1305" {
		t.Errorf("method = %q", key.Method)
	}
	if key.Password != "test-password" {
		t.Errorf("password = %q", key.Password)
	}
	if key.Host != "198.51.100.7" || key.Port != "16014" {
		t.Errorf("host:port = %q:%q", key.Host, key.Port)
	}
	if key.Name != "Apple" {
		t.Errorf("name = %q", key.Name)
	}
}

func TestParseAccessKeyAcceptsBase64Variants(t *testing.T) {
	// URL-safe, unpadded userinfo; no query string; IPv6 literal host.
	key, err := ParseAccessKey("ss://YWVzLTI1Ni1nY206c2VjcmV0@[2001:db8::1]:8388#Tokyo")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if key.Method != "aes-256-gcm" || key.Password != "secret" {
		t.Errorf("got %q / %q", key.Method, key.Password)
	}
	if key.Host != "2001:db8::1" || key.Port != "8388" {
		t.Errorf("host:port = %q:%q", key.Host, key.Port)
	}
}

func TestParseAccessKeyLegacyShape(t *testing.T) {
	// Whole-payload base64: method:password@host:port
	key, err := ParseAccessKey("ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwdw@example.com:443")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if key.Host != "example.com" || key.Port != "443" {
		t.Errorf("host:port = %q:%q", key.Host, key.Port)
	}
}

func TestParseAccessKeyRejects(t *testing.T) {
	cases := map[string]string{
		"empty":          "",
		"not a key":      "https://example.com",
		"truncated":      "ss://",
		"bad base64":     "ss://!!!@host:1",
		"no port":        "ss://YWVzLTI1Ni1nY206cA@host#x",
		"unsupported":    "ss://cmM0LW1kNTpwYXNz@host:1",
		"empty password": "ss://YWVzLTI1Ni1nY206@host:1",
	}
	for name, raw := range cases {
		if _, err := ParseAccessKey(raw); err == nil {
			t.Errorf("%s: expected an error for %q", name, raw)
		}
	}
}

func TestParseAccessKeyTrim(t *testing.T) {
	if _, err := ParseAccessKey("  " + outlineKey + "\n"); err != nil {
		t.Fatalf("pasted whitespace should be tolerated: %v", err)
	}
}
