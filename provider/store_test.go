package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := NewStore(filepath.Join(dir, "servers.json"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return st
}

func mustKey(t *testing.T, raw string) AccessKey {
	t.Helper()
	key, err := ParseAccessKey(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return key
}

func TestStoreAddListRenameDelete(t *testing.T) {
	st := testStore(t)
	srv, err := st.Add(mustKey(t, outlineKey), "")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if srv.Name != "Apple" {
		t.Errorf("default name should come from the key fragment, got %q", srv.Name)
	}
	if len(st.List()) != 1 {
		t.Fatalf("list = %d", len(st.List()))
	}

	if _, err := st.Rename(srv.ID, "  Home  "); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, _ := st.Get(srv.ID)
	if got.Name != "Home" {
		t.Errorf("name = %q", got.Name)
	}
	if _, err := st.Rename(srv.ID, "   "); err == nil {
		t.Error("blank rename should fail")
	}

	if err := st.Delete(srv.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(st.List()) != 0 {
		t.Errorf("list after delete = %d", len(st.List()))
	}
	if err := st.Delete(srv.ID); err != ErrNoSuchServer {
		t.Errorf("second delete = %v", err)
	}
}

func TestStoreRejectsDuplicateAddress(t *testing.T) {
	st := testStore(t)
	if _, err := st.Add(mustKey(t, outlineKey), "first"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := st.Add(mustKey(t, outlineKey), "second"); err != ErrDuplicate {
		t.Fatalf("duplicate = %v", err)
	}
}

func TestStorePersistsWith0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "servers.json")
	st, err := NewStore(path)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if _, err := st.Add(mustKey(t, outlineKey), "Apple"); err != nil {
		t.Fatalf("add: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("servers.json mode = %o, want 600", perm)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var f storeFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("json: %v", err)
	}

	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	list := reloaded.List()
	if len(list) != 1 || list[0].Host != "198.51.100.7" {
		t.Fatalf("reloaded = %+v", list)
	}
}

func TestServerViewHidesPassword(t *testing.T) {
	srv := Server{ID: "abcd", Name: "Apple", Method: "aes-256-gcm", Password: "hunter2", Host: "h", Port: "1"}
	raw, err := json.Marshal(srv.View())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if contains(string(raw), "hunter2") {
		t.Fatalf("view leaked the password: %s", raw)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
