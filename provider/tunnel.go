package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// TunnelStatus is the public state of the single tunnel this daemon runs.
type TunnelStatus struct {
	State      string     `json:"state"` // disconnected | connecting | connected | error
	ServerID   string     `json:"serverId,omitempty"`
	ServerName string     `json:"serverName,omitempty"`
	Since      *time.Time `json:"since,omitempty"`
	LastError  string     `json:"lastError,omitempty"`
	Socks      string     `json:"socks"`
	PID        int        `json:"pid,omitempty"`
	Backend    string     `json:"backend,omitempty"`
}

// Tunnel supervises the external shadowsocks client. This process never
// implements the cipher itself: a proven binary owns the wire format, and all
// we own is its lifecycle.
type Tunnel struct {
	mu       sync.RWMutex
	socks    string
	state    string
	serverID string
	serverN  string
	since    *time.Time
	lastErr  string
	cmd      *exec.Cmd
	backend  string
	gen      int
}

var ErrNoBackend = errors.New("no shadowsocks client found (install one: brew install shadowsocks-rust)")

// NewTunnel resolves the client binary once at startup.
func NewTunnel(socks string) *Tunnel {
	return &Tunnel{socks: socks, state: "disconnected", backend: findBackend()}
}

// findBackend looks for sslocal in the usual places. The environment variable
// wins so a custom build can be pointed at.
func findBackend() string {
	if p := strings.TrimSpace(os.Getenv("OUTLINE_CONTROL_SSLOCAL")); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, p := range []string{
		"/opt/homebrew/bin/sslocal",
		"/usr/local/bin/sslocal",
		"/usr/bin/sslocal",
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("sslocal"); err == nil {
		return p
	}
	return ""
}

func (t *Tunnel) Status() TunnelStatus {
	t.mu.RLock()
	defer t.mu.RUnlock()
	st := TunnelStatus{
		State:      t.state,
		ServerID:   t.serverID,
		ServerName: t.serverN,
		Since:      t.since,
		LastError:  t.lastErr,
		Socks:      t.socks,
		Backend:    t.backend,
	}
	if t.cmd != nil && t.cmd.Process != nil {
		st.PID = t.cmd.Process.Pid
	}
	return st
}

// Connect stops whatever is running and starts the client for srv.
func (t *Tunnel) Connect(srv Server) error {
	if t.backend == "" {
		t.setError(ErrNoBackend.Error())
		return ErrNoBackend
	}
	t.Disconnect()

	args := []string{
		"-b", t.socks,
		"-s", srv.Address(),
		"-m", srv.Method,
		"-k", srv.Password,
	}
	// Pick a port that is actually free: a stale client from an earlier run
	// must not make the whole feature fail.
	socks := t.freeSocks()
	args[1] = socks

	cmd := exec.Command(t.backend, args...)
	cmd.Env = append(os.Environ(), "RUST_LOG=warn")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr

	t.mu.Lock()
	t.gen++
	gen := t.gen
	t.socks = socks
	t.state = "connecting"
	t.serverID = srv.ID
	t.serverN = srv.Name
	t.since = nil
	t.lastErr = ""
	t.cmd = cmd
	t.mu.Unlock()

	if err := cmd.Start(); err != nil {
		t.setError(fmt.Sprintf("cannot start %s: %v", t.backend, err))
		return err
	}
	go t.watch(cmd, gen, &stderr)

	// Give the client a moment to fail loudly (bad cipher, refused port).
	time.Sleep(400 * time.Millisecond)
	t.mu.RLock()
	state, lastErr := t.state, t.lastErr
	t.mu.RUnlock()
	if state == "error" {
		return fmt.Errorf("tunnel did not start: %s", lastErr)
	}

	t.mu.Lock()
	if t.gen == gen {
		t.state = "connected"
		now := time.Now().UTC()
		t.since = &now
	}
	t.mu.Unlock()

	go t.watch(cmd, gen, &stderr)
	return nil
}

// watch notices a client that dies on its own and reports why.
func (t *Tunnel) watch(cmd *exec.Cmd, gen int, stderr *strings.Builder) {
	err := cmd.Wait()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.gen != gen {
		return // superseded by a newer connect or a deliberate disconnect
	}
	t.state = "error"
	t.lastErr = cleanErr(stderr.String())
	if t.lastErr == "" {
		if err != nil {
			t.lastErr = fmt.Sprintf("tunnel exited: %v", err)
		} else {
			t.lastErr = "tunnel exited"
		}
	}
	t.cmd = nil
	t.since = nil
}

// Disconnect stops the client, if any, and clears the state.
func (t *Tunnel) Disconnect() {
	t.mu.Lock()
	cmd := t.cmd
	t.gen++
	t.cmd = nil
	t.state = "disconnected"
	t.serverID = ""
	t.serverN = ""
	t.since = nil
	t.lastErr = ""
	t.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

func (t *Tunnel) setError(msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state = "error"
	t.lastErr = msg
	t.cmd = nil
	t.since = nil
}

// cleanErr keeps the last meaningful line of client output, ignoring the
// backtrace hint that makes panics look like our own error.
func cleanErr(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "note:") || strings.HasPrefix(strings.ToLower(l), "info ") {
			continue
		}
		switch {
		case strings.Contains(l, "AddrInUse"):
			return "the SOCKS port is already in use by another program"
		case strings.Contains(l, "Connection refused"):
			return "the exit refused the connection"
		}
		if len(l) > 300 {
			l = l[:300] + "…"
		}
		return l
	}
	return ""
}

// freeSocks returns the configured address when nothing owns it, otherwise an
// address that is free right now. The chosen address is published in /status so
// the GUI and any local client can follow it.
func (t *Tunnel) freeSocks() string {
	if canListen(t.socks) {
		return t.socks
	}
	if host, _, err := net.SplitHostPort(t.socks); err == nil {
		for port := 11081; port <= 11089; port++ {
			candidate := net.JoinHostPort(host, fmt.Sprint(port))
			if canListen(candidate) {
				return candidate
			}
		}
	}
	return "127.0.0.1:0"
}

func canListen(addr string) bool {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
