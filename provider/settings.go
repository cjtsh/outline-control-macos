package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// Settings are the user's standing instructions to the service, as opposed to
// the per-exit bookkeeping in servers.json: whether to bring the VPN up when the
// Mac boots, which exit to use for that, and whether the service is "off" for
// now (released the tunnel, will not reconnect by itself).
type Settings struct {
	Autoconnect   bool   `json:"autoconnect"`
	DefaultServer string `json:"defaultServer"`
	Suspended     bool   `json:"suspended"`
}

// SettingsStore persists Settings beside servers.json with the same 0600
// discipline: this file decides what happens on the next boot, so a local user
// should not be able to rewrite it silently.
type SettingsStore struct {
	mu   sync.Mutex
	path string
	cur  Settings
}

func NewSettingsStore(path string) (*SettingsStore, error) {
	st := &SettingsStore{path: path}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &st.cur); err != nil {
		return nil, err
	}
	return st, nil
}

func (st *SettingsStore) Get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.cur
}

func (st *SettingsStore) Set(next Settings) (Settings, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := writeAtomic(st.path, next); err != nil {
		return st.cur, err
	}
	st.cur = next
	return next, nil
}

func (st *SettingsStore) Path() string { return st.path }

// writeAtomic writes a small JSON file through a temp file in the same
// directory, at 0600, then renames it into place.
func writeAtomic(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
