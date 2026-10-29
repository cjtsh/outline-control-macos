package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testToken = "0123456789abcdef"

func newTestAPI(t *testing.T) (*httptest.Server, *Tunnel) {
	t.Helper()
	dir := t.TempDir()

	// A stand-in for sslocal: the tunnel lifecycle is what we test here, and
	// the real cipher is somebody else's code.
	stub := filepath.Join(dir, "fakessl")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatalf("stub: %v", err)
	}
	t.Setenv("OUTLINE_CONTROL_SSLOCAL", stub)

	store, err := NewStore(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	tunnel := NewTunnel("127.0.0.1:11080")
	api := &API{store: store, tunnel: tunnel, token: testToken, health: NewHealth(time.Minute)}
	ts := httptest.NewServer(api.Routes())
	t.Cleanup(func() {
		tunnel.Disconnect()
		ts.Close()
	})
	return ts, tunnel
}

func call(t *testing.T, ts *httptest.Server, method, path, token string, body any) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, ts.URL+path, rdr)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if token != "" {
		req.Header.Set("X-Outline-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func TestAPIRefusesWithoutToken(t *testing.T) {
	ts, _ := newTestAPI(t)
	for _, path := range []string{"/status", "/servers", "/health"} {
		if code, _ := call(t, ts, http.MethodGet, path, "", nil); code != http.StatusUnauthorized {
			t.Errorf("%s without token = %d", path, code)
		}
	}
	if code, _ := call(t, ts, http.MethodGet, "/status", "wrong", nil); code != http.StatusUnauthorized {
		t.Errorf("wrong token = %d", code)
	}
}

func TestAPIAddListRenameDelete(t *testing.T) {
	ts, _ := newTestAPI(t)

	code, raw := call(t, ts, http.MethodPost, "/servers", testToken, map[string]string{"key": outlineKey})
	if code != http.StatusCreated {
		t.Fatalf("add = %d %s", code, raw)
	}
	var added ServerView
	if err := json.Unmarshal(raw, &added); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if added.Name != "Apple" || added.Port != "16014" {
		t.Errorf("added = %+v", added)
	}

	code, raw = call(t, ts, http.MethodGet, "/servers", testToken, nil)
	if code != http.StatusOK || strings.Contains(string(raw), "test-password") {
		t.Fatalf("list leaked the password or failed: %d %s", code, raw)
	}

	if code, raw := call(t, ts, http.MethodPost, "/servers", testToken, map[string]string{"key": outlineKey}); code != http.StatusConflict {
		t.Errorf("duplicate add = %d %s", code, raw)
	}
	if code, _ := call(t, ts, http.MethodPost, "/servers", testToken, map[string]string{"key": "nonsense"}); code != http.StatusBadRequest {
		t.Errorf("bad key = %d", code)
	}

	if code, raw := call(t, ts, http.MethodPatch, "/servers/"+added.ID, testToken, map[string]string{"name": "Home"}); code != http.StatusOK {
		t.Errorf("rename = %d %s", code, raw)
	}
	if code, _ := call(t, ts, http.MethodPatch, "/servers/nope", testToken, map[string]string{"name": "x"}); code != http.StatusNotFound {
		t.Errorf("rename missing = %d", code)
	}

	if code, _ := call(t, ts, http.MethodDelete, "/servers/"+added.ID, testToken, nil); code != http.StatusNoContent {
		t.Errorf("delete = %d", code)
	}
	if code, _ := call(t, ts, http.MethodDelete, "/servers/"+added.ID, testToken, nil); code != http.StatusNotFound {
		t.Errorf("delete again = %d", code)
	}
}

func TestAPIConnectAndDisconnect(t *testing.T) {
	ts, tunnel := newTestAPI(t)
	_, raw := call(t, ts, http.MethodPost, "/servers", testToken, map[string]string{"key": outlineKey})
	var added ServerView
	if err := json.Unmarshal(raw, &added); err != nil {
		t.Fatalf("decode: %v", err)
	}

	code, raw := call(t, ts, http.MethodPost, "/connect", testToken, map[string]string{"id": added.ID})
	if code != http.StatusOK {
		t.Fatalf("connect = %d %s", code, raw)
	}
	if tunnel.Status().State != "connected" {
		t.Fatalf("state after connect = %q", tunnel.Status().State)
	}

	code, raw = call(t, ts, http.MethodGet, "/status", testToken, nil)
	if code != http.StatusOK || !strings.Contains(string(raw), `"connected"`) {
		t.Fatalf("status = %d %s", code, raw)
	}

	if code, _ := call(t, ts, http.MethodPost, "/connect", testToken, map[string]string{"id": "missing"}); code != http.StatusNotFound {
		t.Errorf("connect missing = %d", code)
	}

	if code, raw := call(t, ts, http.MethodPost, "/disconnect", testToken, nil); code != http.StatusOK {
		t.Fatalf("disconnect = %d %s", code, raw)
	}
	if state := tunnel.Status().State; state != "disconnected" {
		t.Errorf("state after disconnect = %q", state)
	}
}

func TestAPIDeleteStopsItsTunnel(t *testing.T) {
	ts, tunnel := newTestAPI(t)
	_, raw := call(t, ts, http.MethodPost, "/servers", testToken, map[string]string{"key": outlineKey})
	var added ServerView
	if err := json.Unmarshal(raw, &added); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if code, raw := call(t, ts, http.MethodPost, "/connect", testToken, map[string]string{"id": added.ID}); code != http.StatusOK {
		t.Fatalf("connect = %d %s", code, raw)
	}
	if code, _ := call(t, ts, http.MethodDelete, "/servers/"+added.ID, testToken, nil); code != http.StatusNoContent {
		t.Fatalf("delete = %d", code)
	}
	if state := tunnel.Status().State; state != "disconnected" {
		t.Errorf("deleting the live exit left the tunnel %q", state)
	}
}

func TestAPIHealthReportsUnreachable(t *testing.T) {
	ts, _ := newTestAPI(t)
	// 127.0.0.1:1 is not listening; the probe must say so rather than hang.
	_, raw := call(t, ts, http.MethodPost, "/servers", testToken,
		map[string]string{"key": "ss://YWVzLTI1Ni1nY206c2VjcmV0@127.0.0.1:1#Dead"})
	var added ServerView
	if err := json.Unmarshal(raw, &added); err != nil {
		t.Fatalf("decode: %v", err)
	}
	code, raw := call(t, ts, http.MethodGet, "/health", testToken, nil)
	if code != http.StatusOK {
		t.Fatalf("health = %d %s", code, raw)
	}
	if !strings.Contains(string(raw), `"reachable":false`) {
		t.Errorf("health = %s", raw)
	}
}
