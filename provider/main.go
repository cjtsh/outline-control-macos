package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	var (
		listen = flag.String("listen", "127.0.0.1:9911", "address for the local control API")
		socks  = flag.String("socks", "127.0.0.1:11080", "loopback SOCKS5 address exposed while connected")
		data   = flag.String("data", defaultDataDir(), "directory for servers.json and token")
	)
	flag.Parse()

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
	tunnel := NewTunnel(*socks)
	health := NewHealth(25 * time.Second)
	health.StartRefresh(30*time.Second, store.List)

	api := &API{store: store, tunnel: tunnel, token: token, health: health}
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
		backend = "(missing — install shadowsocks-rust)"
	}
	log.Printf("outline-control %s listening on http://%s (socks %s, client %s)",
		version, ln.Addr(), *socks, backend)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		log.Print("shutting down")
		tunnel.Disconnect()
		_ = srv.Close()
		os.Exit(0)
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
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

func trimSpace(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

const version = "0.1.0"
