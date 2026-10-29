package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// AccessKey is a parsed Outline access key: everything needed to reach one
// server, plus the label that came with the key.
type AccessKey struct {
	Method   string
	Password string
	Host     string
	Port     string
	Name     string
}

var ErrNotAccessKey = errors.New("not an ss:// access key")

// supportedCiphers is the set of AEAD methods we are willing to run. Anything
// else is refused rather than passed to the tunnel binary, so a pasted key can
// never select an arbitrary cipher.
var supportedCiphers = map[string]bool{
	"aes-128-gcm":             true,
	"aes-192-gcm":             true,
	"aes-256-gcm":             true,
	"chacha20-ietf-poly1305":  true,
	"xchacha20-ietf-poly1305": true,
}

// ParseAccessKey accepts the two shapes Outline has shipped:
//
//	ss://BASE64(method:password)@host:port/?outline=1#Name   (SIP002 / Outline)
//	ss://BASE64(method:password@host:port)#Name              (legacy)
//
// The userinfo may be standard or URL-safe base64, padded or not, because
// every generator picks differently.
func ParseAccessKey(raw string) (AccessKey, error) {
	s := strings.TrimSpace(raw)
	if len(s) < 5 || !strings.EqualFold(s[:5], "ss://") {
		return AccessKey{}, ErrNotAccessKey
	}
	s = s[5:]

	key := AccessKey{}
	if i := strings.LastIndex(s, "#"); i >= 0 {
		key.Name = unescape(s[i+1:])
		s = s[:i]
	}
	if i := strings.Index(s, "?"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(s, "/")
	if s == "" {
		return AccessKey{}, fmt.Errorf("access key is empty")
	}

	var userinfo, hostport string
	if i := strings.LastIndex(s, "@"); i >= 0 {
		// SIP002: the part before "@" is base64 userinfo.
		userinfo, hostport = s[:i], s[i+1:]
	} else {
		// Legacy: the whole thing is one base64 blob.
		blob, err := decodeB64(s)
		if err != nil {
			return AccessKey{}, fmt.Errorf("access key is not valid base64: %w", err)
		}
		i := strings.LastIndex(string(blob), "@")
		if i < 0 {
			return AccessKey{}, fmt.Errorf("access key has no host")
		}
		userinfo, hostport = string(blob[:i]), string(blob[i+1:])
	}

	method, password, ok := strings.Cut(userinfo, ":")
	if !ok {
		blob, err := decodeB64(userinfo)
		if err != nil {
			return AccessKey{}, fmt.Errorf("userinfo is neither method:password nor base64: %w", err)
		}
		method, password, ok = strings.Cut(string(blob), ":")
		if !ok {
			return AccessKey{}, fmt.Errorf("userinfo has no password")
		}
	}
	method = strings.ToLower(strings.TrimSpace(method))
	if !supportedCiphers[method] {
		return AccessKey{}, fmt.Errorf("unsupported cipher %q", method)
	}
	if password == "" {
		return AccessKey{}, fmt.Errorf("access key has an empty password")
	}

	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		return AccessKey{}, fmt.Errorf("access key has no usable host:port: %w", err)
	}
	if host == "" || port == "" {
		return AccessKey{}, fmt.Errorf("access key has an empty host or port")
	}

	key.Method = method
	key.Password = password
	key.Host = strings.Trim(host, "[]")
	key.Port = port
	if key.Name == "" {
		key.Name = key.Host
	}
	return key, nil
}

func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{
		base64.RawURLEncoding, base64.URLEncoding,
		base64.RawStdEncoding, base64.StdEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("no base64 variant matched")
}

func unescape(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}
