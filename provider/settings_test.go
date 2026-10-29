package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// settingsSnapshot is what the GUI reads back after a change.
type settingsSnapshot struct {
	Settings Settings `json:"settings"`
	Service  struct {
		PID        int    `json:"pid"`
		Mode       string `json:"mode"`
		Supervised bool   `json:"supervised"`
	} `json:"service"`
}

func TestSettingsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	store, err := NewSettingsStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := store.Set(Settings{Autoconnect: true, DefaultServer: "abc12345"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	reopened, err := NewSettingsStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got := reopened.Get()
	if !got.Autoconnect || got.DefaultServer != "abc12345" || got.Suspended {
		t.Fatalf("settings not persisted: %+v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat settings: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("settings.json mode = %o, want 600", perm)
	}
}

func TestSuspendReleasesTunnelAndResumeBringsItBack(t *testing.T) {
	ts, tunnel := newTestAPI(t)
	body := map[string]string{"key": "ss://YWVzLTI1Ni1nY206c2VjcmV0@198.51.100.7:8388#Test"}
	code, raw := call(t, ts, "POST", "/servers", testToken, body)
	if code != 201 {
		t.Fatalf("add exit: %d %s", code, raw)
	}
	var added ServerView
	if err := json.Unmarshal(raw, &added); err != nil {
		t.Fatalf("decode: %v", err)
	}

	code, raw = call(t, ts, "PATCH", "/settings", testToken, map[string]any{
		"autoconnect":   true,
		"defaultServer": added.ID,
	})
	if code != 200 {
		t.Fatalf("patch settings: %d %s", code, raw)
	}
	if st := tunnel.Status(); st.State != "connected" {
		t.Fatalf("enabling start-at-boot should connect the default exit, state = %s", st.State)
	}

	code, raw = call(t, ts, "POST", "/service/suspend", testToken, nil)
	if code != 200 {
		t.Fatalf("suspend: %d %s", code, raw)
	}
	if st := tunnel.Status(); st.State != "disconnected" {
		t.Fatalf("suspend should release the tunnel, state = %s", st.State)
	}

	code, raw = call(t, ts, "POST", "/service/resume", testToken, nil)
	if code != 200 {
		t.Fatalf("resume: %d %s", code, raw)
	}
	if st := tunnel.Status(); st.State != "connected" {
		t.Fatalf("resume should reconnect the default exit, state = %s", st.State)
	}
}

func TestDeleteClearsDefaultExit(t *testing.T) {
	ts, _ := newTestAPI(t)
	_, raw := call(t, ts, "POST", "/servers", testToken, map[string]string{
		"key": "ss://YWVzLTI1Ni1nY206c2VjcmV0@198.51.100.8:8388#Gone",
	})
	var added ServerView
	if err := json.Unmarshal(raw, &added); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if code, raw := call(t, ts, "PATCH", "/settings", testToken, map[string]any{"defaultServer": added.ID}); code != 200 {
		t.Fatalf("set default: %d %s", code, raw)
	}
	if code, _ := call(t, ts, "DELETE", "/servers/"+added.ID, testToken, nil); code != 204 {
		t.Fatalf("delete exit: %d", code)
	}

	_, raw = call(t, ts, "GET", "/settings", testToken, nil)
	var snap settingsSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if snap.Settings.DefaultServer != "" {
		t.Fatalf("deleting the default exit should clear it, got %q", snap.Settings.DefaultServer)
	}
}

func TestDefaultExitMustExist(t *testing.T) {
	ts, _ := newTestAPI(t)
	code, _ := call(t, ts, "PATCH", "/settings", testToken, map[string]any{"defaultServer": "nope1234"})
	if code != 404 {
		t.Fatalf("unknown default exit: got %d, want 404", code)
	}
}

func TestRestartIsReportedBeforeExit(t *testing.T) {
	ts, _ := newTestAPI(t)
	code, raw := call(t, ts, "POST", "/service/restart", testToken, nil)
	if code != 200 {
		t.Fatalf("restart: %d %s", code, raw)
	}
	var resp struct {
		Restarting bool `json:"restarting"`
		Supervised bool `json:"supervised"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Restarting {
		t.Fatalf("restart should say it is restarting: %s", raw)
	}
	// The exit path is stubbed by newTestAPI, so this test can end cleanly.
}
