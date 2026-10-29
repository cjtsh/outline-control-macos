package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func main() {
	var (
		listen = flag.String("listen", "127.0.0.1:9911", "address for the local control API")
		socks  = flag.String("socks", "127.0.0.1:11080", "loopback SOCKS5 address exposed while connected")
		data   = flag.String("data", defaultDataDir(), "directory for servers.json and token")
		mode   = flag.String("mode", string(ModeSocks), "how traffic reaches the exit: socks (local proxy) or tunnel (full-device VPN)")
		mtu    = flag.Int("mtu", 1500, "utun MTU for tunnel mode")

		owner   = flag.String("token-owner", "", "give the token file to this user (for tunnel mode, where the daemon runs as root)")
		tunOpts = TunOptions{}
	)
	flag.Func("exclude-host", "host dialled directly, never through the exit (repeatable)", func(v string) error {
		tunOpts.ExcludeHosts = append(tunOpts.ExcludeHosts, v)
		tunOpts.ExcludeDomains = append(tunOpts.ExcludeDomains, v)
		return nil
	})
	flag.Func("exclude-cidr", "CIDR dialled directly (repeatable)", func(v string) error {
		tunOpts.ExcludeCIDRs = append(tunOpts.ExcludeCIDRs, v)
		return nil
	})
	flag.Func("exclude-domain", "domain suffix resolved and dialled directly (repeatable)", func(v string) error {
		tunOpts.ExcludeDomains = append(tunOpts.ExcludeDomains, v)
		return nil
	})
	flag.Parse()
	tunOpts.MTU = *mtu

	if err := os.MkdirAll(*data, 0o700); err != nil {
		log.Fatalf("cannot create %s: %v", *data, err)
	}
	token, err := ensureToken(filepath.Join(*data, "token"))
	if err != nil {
		log.Fatalf("cannot read token: %v", err)
	}
	store, err := NewStore(filepath.Join(*data, "servers.json"))
	if err != nil {
		log.Fatalf("%v", err)
	}
	if err := chownToken(*owner, filepath.Join(*data, "token")); err != nil {
		log.Printf("could not hand the token to %q: %v", *owner, err)
	}
	settings, err := NewSettingsStore(filepath.Join(*data, "settings.json"))
	if err != nil {
		log.Fatalf("cannot read settings: %v", err)
	}
	tunnel := NewTunnel(*socks, Mode(*mode), tunOpts, *data)
	health := NewHealth(25 * time.Second)
	health.StartRefresh(30*time.Second, store.List)

	api := &API{
		store:    store,
		tunnel:   tunnel,
		token:    token,
		health:   health,
		settings: settings,
		started:  time.Now().UTC(),
	}
	srv := &http.Server{
		Addr:              *listen,
		Handler:           api.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatalf("cannot listen on %s: %v", *listen, err)
	}
	backend := tunnel.backend
	if backend == "" {
		if tunnel.mode == ModeTunnel {
			backend = "(missing — install sing-box)"
		} else {
			backend = "(missing — install shadowsocks-rust)"
		}
	}
	log.Printf("outline-control %s listening on http://%s (mode %s, engine %s)",
		version, ln.Addr(), tunnel.mode, backend)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		log.Print("shutting down")
		tunnel.Disconnect()
		_ = srv.Close()
		os.Exit(0)
	}()

	// Boot behaviour: bring the default exit up when the Mac starts, waiting for
	// the network rather than failing once. A manual disconnect is not undone.
	go autoconnect(tunnel, store, settings)

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

type userIDs struct {
	uid int
	gid int
}

// userLookup resolves a user name or numeric id without cgo (os/user works fine
// here, but finding the owner by name keeps the flag readable).
func userLookup(name string) (userIDs, error) {
	if n, err := strconv.Atoi(name); err == nil {
		return userIDs{uid: n, gid: n}, nil
	}
	out, err := exec.Command("/usr/bin/id", "-u", name).Output()
	if err != nil {
		return userIDs{}, err
	}
	uid, err := strconv.Atoi(trimSpace(string(out)))
	if err != nil {
		return userIDs{}, err
	}
	gidOut, err := exec.Command("/usr/bin/id", "-g", name).Output()
	if err != nil {
		return userIDs{}, err
	}
	gid, err := strconv.Atoi(trimSpace(string(gidOut)))
	if err != nil {
		return userIDs{}, err
	}
	return userIDs{uid: uid, gid: gid}, nil
}

// autoconnect honours "start at boot": connect to the default exit, retrying
// while the network comes up, and give up after a couple of minutes so a dead
// exit cannot keep the daemon busy.
func autoconnect(t *Tunnel, store *Store, settings *SettingsStore) {
	s := settings.Get()
	if s.Suspended || !s.Autoconnect || s.DefaultServer == "" {
		return
	}
	srv, ok := store.Get(s.DefaultServer)
	if !ok {
		log.Printf("autoconnect: default exit %q is no longer saved", s.DefaultServer)
		return
	}
	for attempt := 1; attempt <= 24; attempt++ {
		if err := t.Connect(srv); err != nil {
			log.Printf("autoconnect attempt %d: %v", attempt, err)
			time.Sleep(5 * time.Second)
			continue
		}
		time.Sleep(1500 * time.Millisecond)
		if t.Status().State == "connected" {
			log.Printf("autoconnect: connected to %s", srv.Name)
			return
		}
		log.Printf("autoconnect attempt %d: tunnel did not come up, retrying", attempt)
		time.Sleep(5 * time.Second)
	}
	log.Printf("autoconnect: giving up after 24 attempts")
}

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Library", "Application Support", "Outline Control")
}

// ensureToken returns the API token, creating it on first run.
func ensureToken(path string) (string, error) {
	if raw, err := os.ReadFile(path); err == nil {
		if t := string(raw); len(t) > 0 {
			return trimSpace(t), nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", err
	}
	return token, nil
}

// chownToken hands the API token to the desktop user when the daemon runs as
// root, so the GUI can still read it without running as root itself.
func chownToken(user, path string) error {
	if user == "" {
		return nil
	}
	u, err := userLookup(user)
	if err != nil {
		return err
	}
	return os.Chown(path, u.uid, u.gid)
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

const version = "0.1.0"
