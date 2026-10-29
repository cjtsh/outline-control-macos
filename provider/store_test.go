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

func TestReorderPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	st, err := NewStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var ids []string
	for _, key := range []string{
		"ss://YWVzLTI1Ni1nY206c2VjcmV0@198.51.100.11:8388#One",
		"ss://YWVzLTI1Ni1nY206c2VjcmV0@198.51.100.12:8388#Two",
		"ss://YWVzLTI1Ni1nY206c2VjcmV0@198.51.100.13:8388#Three",
	} {
		parsed, err := ParseAccessKey(key)
		if err != nil {
			t.Fatalf("parse %s: %v", key, err)
		}
		srv, err := st.Add(parsed, "")
		if err != nil {
			t.Fatalf("add: %v", err)
		}
		ids = append(ids, srv.ID)
	}

	if err := st.Reorder([]string{ids[2], ids[0], ids[1]}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	reopened, err := NewStore(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got := []string{}
	for _, s := range reopened.List() {
		got = append(got, s.ID)
	}
	want := []string{ids[2], ids[0], ids[1]}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order after reopen = %v, want %v", got, want)
		}
	}

	if err := st.Reorder([]string{ids[0]}); err == nil {
		t.Fatal("a partial list must be refused")
	}
	if err := st.Reorder([]string{ids[0], ids[1], "nope1234"}); err == nil {
		t.Fatal("an unknown id must be refused")
	}
	if err := st.Reorder([]string{ids[0], ids[0], ids[1]}); err == nil {
		t.Fatal("a duplicate id must be refused")
	}
}

func TestLegacyFileGetsPositions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "servers.json")
	legacy := `{"version":1,"servers":[
  {"id":"aaaa0001","name":"Later","method":"aes-256-gcm","password":"p","host":"198.51.100.20","port":"8388","added":"2026-01-02T00:00:00Z"},
  {"id":"bbbb0002","name":"Earlier","method":"aes-256-gcm","password":"p","host":"198.51.100.21","port":"8388","added":"2026-01-01T00:00:00Z"}
]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	st, err := NewStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	list := st.List()
	if list[0].Name != "Earlier" || list[1].Name != "Later" {
		t.Fatalf("legacy order not preserved: %s, %s", list[0].Name, list[1].Name)
	}
	if list[0].Position != 1 || list[1].Position != 2 {
		t.Fatalf("positions not assigned: %+v", list)
	}
}
