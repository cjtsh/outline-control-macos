package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Server is one saved Outline exit. Password never leaves this process: the
// JSON API only ever serialises ServerView.
type Server struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Method   string    `json:"method"`
	Password string    `json:"password"`
	Host     string    `json:"host"`
	Port     string    `json:"port"`
	Added    time.Time `json:"added"`
}

// ServerView is the public shape of a server.
type ServerView struct {
	ID     string    `json:"id"`
	Name   string    `json:"name"`
	Host   string    `json:"host"`
	Port   string    `json:"port"`
	Method string    `json:"method"`
	Added  time.Time `json:"added"`
}

func (s Server) View() ServerView {
	return ServerView{ID: s.ID, Name: s.Name, Host: s.Host, Port: s.Port, Method: s.Method, Added: s.Added}
}

func (s Server) Address() string { return s.Host + ":" + s.Port }

var (
	ErrNoSuchServer = errors.New("no such server")
	ErrDuplicate    = errors.New("that exit is already saved")
)

// Store is the on-disk list of exits. The file is 0600 because it holds the
// access-key passwords.
type Store struct {
	mu      sync.Mutex
	path    string
	servers []Server
}

type storeFile struct {
	Version int      `json:"version"`
	Servers []Server `json:"servers"`
}

// NewStore loads the list, creating an empty one when the file is absent.
func NewStore(path string) (*Store, error) {
	st := &Store{path: path}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	var f storeFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	st.servers = f.Servers
	sort.SliceStable(st.servers, func(i, j int) bool { return st.servers[i].Added.Before(st.servers[j].Added) })
	return st, nil
}

func (st *Store) List() []Server {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]Server, len(st.servers))
	copy(out, st.servers)
	return out
}

func (st *Store) Get(id string) (Server, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, s := range st.servers {
		if s.ID == id {
			return s, true
		}
	}
	return Server{}, false
}

// Add saves a parsed access key. Names are de-duplicated by address so the
// same exit cannot be added twice.
func (st *Store) Add(key AccessKey, name string) (Server, error) {
	st.mu.Lock()
	defer st.mu.Unlock()

	addr := key.Host + ":" + key.Port
	for _, s := range st.servers {
		if strings.EqualFold(s.Address(), addr) {
			return Server{}, ErrDuplicate
		}
	}
	if name = strings.TrimSpace(name); name == "" {
		name = key.Name
	}
	srv := Server{
		ID:       newID(),
		Name:     name,
		Method:   key.Method,
		Password: key.Password,
		Host:     key.Host,
		Port:     key.Port,
		Added:    time.Now().UTC(),
	}
	st.servers = append(st.servers, srv)
	if err := st.save(); err != nil {
		st.servers = st.servers[:len(st.servers)-1]
		return Server{}, err
	}
	return srv, nil
}

func (st *Store) Rename(id, name string) (Server, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return Server{}, errors.New("name cannot be empty")
	}
	for i := range st.servers {
		if st.servers[i].ID != id {
			continue
		}
		old := st.servers[i].Name
		st.servers[i].Name = name
		if err := st.save(); err != nil {
			st.servers[i].Name = old
			return Server{}, err
		}
		return st.servers[i], nil
	}
	return Server{}, ErrNoSuchServer
}

func (st *Store) Delete(id string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, s := range st.servers {
		if s.ID != id {
			continue
		}
		backup := append([]Server(nil), st.servers...)
		st.servers = append(st.servers[:i:i], st.servers[i+1:]...)
		if err := st.save(); err != nil {
			st.servers = backup
			return err
		}
		return nil
	}
	return ErrNoSuchServer
}

func (st *Store) save() error {
	f := storeFile{Version: 1, Servers: st.servers}
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(st.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".servers-*")
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
	return os.Rename(tmp.Name(), st.path)
}

func newID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xffffffff)
	}
	return hex.EncodeToString(b)
}
