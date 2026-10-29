package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	Mode       Mode       `json:"mode"`
	Socks      string     `json:"socks,omitempty"`
	PID        int        `json:"pid,omitempty"`
	Backend    string     `json:"backend,omitempty"`
}

// Tunnel supervises the external shadowsocks client. This process never
// implements the cipher itself: a proven binary owns the wire format, and all
// we own is its lifecycle.
type Tunnel struct {
	mu       sync.RWMutex
	mode     Mode
	tun      TunOptions
	dataDir  string
	singbox  string
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
func NewTunnel(socks string, mode Mode, opt TunOptions, dataDir string) *Tunnel {
	t := &Tunnel{socks: socks, mode: mode, tun: opt, dataDir: dataDir, state: "disconnected"}
	if mode == ModeTunnel {
		t.singbox = findSingbox()
		t.backend = t.singbox
	} else {
		t.backend = findBackend()
	}
	return t
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

// sslocalConfig renders the shadowsocks-rust client configuration for one exit
// and the local proxy address to listen on.
func sslocalConfig(srv Server, socks string) ([]byte, error) {
	host, port, err := net.SplitHostPort(socks)
	if err != nil {
		return nil, fmt.Errorf("local proxy address %q: %w", socks, err)
	}
	localPort, err := strconv.Atoi(port)
	if err != nil {
		return nil, fmt.Errorf("local proxy port %q is not a number", port)
	}
	serverPort := atoiOr(srv.Port, 0)
	if serverPort == 0 {
		return nil, fmt.Errorf("exit port %q is not a number", srv.Port)
	}
	return json.MarshalIndent(map[string]any{
		"server":        srv.Host,
		"server_port":   serverPort,
		"local_address": host,
		"local_port":    localPort,
		"method":        srv.Method,
		"password":      srv.Password,
	}, "", "  ")
}

// writeSocksConfig stores the sslocal configuration beside the daemon's other
// state, at 0600, the same way connectTun stores singbox.json.
func (t *Tunnel) writeSocksConfig(srv Server, socks string) (string, error) {
	raw, err := sslocalConfig(srv, socks)
	if err != nil {
		return "", err
	}
	path := filepath.Join(t.dataDir, "sslocal.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	return path, nil
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
		Mode:       t.mode,
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
		if t.mode == ModeTunnel {
			err := fmt.Errorf("no tunnel engine found (install one: brew install sing-box)")
			t.setError(err.Error())
			return err
		}
		t.setError(ErrNoBackend.Error())
		return ErrNoBackend
	}
	if t.mode == ModeTunnel && os.Geteuid() != 0 {
		t.setError(ErrNotRoot.Error())
		return ErrNotRoot
	}
	t.Disconnect()

	if t.mode == ModeTunnel {
		return t.connectTun(srv)
	}

	// Pick a port that is actually free: a stale client from an earlier run
	// must not make the whole feature fail.
	socks := t.freeSocks()
	configPath, err := t.writeSocksConfig(srv, socks)
	if err != nil {
		t.setError(fmt.Sprintf("cannot write the proxy config: %v", err))
		return err
	}

	// The password travels in the 0600 config file, never on the command line:
	// argv is readable by every local process through ps.
	cmd := exec.Command(t.backend, "-c", configPath)
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

	return nil
}

// connectTun starts sing-box with a rendered configuration. The utun device and
// the default route belong to it for as long as it runs.
func (t *Tunnel) connectTun(srv Server) error {
	raw, err := tunConfig(srv, t.tun)
	if err != nil {
		t.setError(err.Error())
		return err
	}
	path := filepath.Join(t.dataDir, "singbox.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.setError(fmt.Sprintf("cannot write the tunnel config: %v", err))
		return err
	}

	cmd := exec.Command(t.singbox, "run", "-c", path)
	var output strings.Builder
	cmd.Stderr = &output
	cmd.Stdout = &output
	cmd.Env = append(os.Environ(), "SING_BOX_LOG_LEVEL=warn")

	t.mu.Lock()
	t.gen++
	gen := t.gen
	t.state = "connecting"
	t.serverID = srv.ID
	t.serverN = srv.Name
	t.since = nil
	t.lastErr = ""
	t.cmd = cmd
	t.mu.Unlock()

	if err := cmd.Start(); err != nil {
		t.setError(fmt.Sprintf("cannot start %s: %v", t.singbox, err))
		return err
	}
	go t.watch(cmd, gen, &output)

	// sing-box needs a moment longer than sslocal: it creates the utun device
	// and takes the default route before it is really up.
	time.Sleep(1200 * time.Millisecond)
	t.mu.RLock()
	state, lastErr := t.state, t.lastErr
	t.mu.RUnlock()
	if state == "error" {
		return fmt.Errorf("the tunnel did not start: %s", lastErr)
	}

	t.mu.Lock()
	if t.gen == gen {
		t.state = "connected"
		now := time.Now().UTC()
		t.since = &now
	}
	t.mu.Unlock()
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
		case strings.Contains(l, "operation not permitted"), strings.Contains(l, "permission denied"):
			return "the tunnel engine needs root (run it from the privileged daemon)"
		case strings.Contains(l, "empty direct outbound"):
			return "the tunnel configuration was rejected"
		case strings.Contains(l, "Address already in use"):
			return "a previous tunnel still holds the device"
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
