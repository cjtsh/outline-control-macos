package main

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"
)

// API serves the local control surface. It is bound to a loopback address and
// requires the bearer token from the data directory, so another local process
// cannot drive the VPN without being able to read a 0600 file.
type API struct {
	store  *Store
	tunnel *Tunnel
	token  string
	health *Health
}

const maxBody = 8 << 10

func (a *API) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", a.auth(a.handleStatus))
	mux.HandleFunc("GET /servers", a.auth(a.handleServers))
	mux.HandleFunc("POST /servers", a.auth(a.handleAdd))
	mux.HandleFunc("PATCH /servers/{id}", a.auth(a.handleRename))
	mux.HandleFunc("DELETE /servers/{id}", a.auth(a.handleDelete))
	mux.HandleFunc("POST /connect", a.auth(a.handleConnect))
	mux.HandleFunc("POST /disconnect", a.auth(a.handleDisconnect))
	mux.HandleFunc("GET /health", a.auth(a.handleHealth))
	return mux
}

func (a *API) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Outline-Token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(a.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "bad or missing token")
			return
		}
		next(w, r)
	}
}

func (a *API) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"tunnel":  a.tunnel.Status(),
		"servers": len(a.store.List()),
	})
}

func (a *API) handleServers(w http.ResponseWriter, r *http.Request) {
	servers := a.store.List()
	views := make([]ServerView, 0, len(servers))
	for _, s := range servers {
		views = append(views, s.View())
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": views})
}

type addRequest struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (a *API) handleAdd(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	key, err := ParseAccessKey(req.Key)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	srv, err := a.store.Add(key, req.Name)
	if errors.Is(err, ErrDuplicate) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.health.Probe(srv)
	writeJSON(w, http.StatusCreated, srv.View())
}

func (a *API) handleRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	srv, err := a.store.Rename(r.PathValue("id"), req.Name)
	if errors.Is(err, ErrNoSuchServer) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, srv.View())
}

func (a *API) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Never leave a tunnel running against an exit that no longer exists.
	if st := a.tunnel.Status(); st.ServerID == id {
		a.tunnel.Disconnect()
	}
	if err := a.store.Delete(id); errors.Is(err, ErrNoSuchServer) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	a.health.Forget(id)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	srv, ok := a.store.Get(req.ID)
	if !ok {
		writeError(w, http.StatusNotFound, ErrNoSuchServer.Error())
		return
	}
	if err := a.tunnel.Connect(srv); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a.tunnel.Status())
}

func (a *API) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	a.tunnel.Disconnect()
	writeJSON(w, http.StatusOK, a.tunnel.Status())
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	out := make([]HealthView, 0)
	servers := a.store.List()
	for _, s := range servers {
		h := a.health.Probe(s)
		out = append(out, HealthView{ID: s.ID, Reachable: h.Reachable, Checked: h.Checked, Error: h.Error})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	writeJSON(w, http.StatusOK, map[string]any{"exits": out})
}

// Health caches a TCP reachability probe per exit so the GUI can show a dead
// server without the daemon dialling on every poll.
type Health struct {
	mu   sync.Mutex
	ttl  time.Duration
	seen map[string]healthEntry
}

type healthEntry struct {
	Reachable bool
	Checked   time.Time
	Error     string
}

type HealthView struct {
	ID        string    `json:"id"`
	Reachable bool      `json:"reachable"`
	Checked   time.Time `json:"checked"`
	Error     string    `json:"error,omitempty"`
}

func NewHealth(ttl time.Duration) *Health {
	return &Health{ttl: ttl, seen: map[string]healthEntry{}}
}

func (h *Health) Probe(srv Server) healthEntry {
	h.mu.Lock()
	if e, ok := h.seen[srv.ID]; ok && time.Since(e.Checked) < h.ttl {
		h.mu.Unlock()
		return e
	}
	h.mu.Unlock()

	conn, err := net.DialTimeout("tcp", srv.Address(), 3*time.Second)
	entry := healthEntry{Reachable: err == nil, Checked: time.Now().UTC()}
	if err != nil {
		entry.Error = trimNetErr(err.Error())
	}
	if conn != nil {
		_ = conn.Close()
	}

	h.mu.Lock()
	h.seen[srv.ID] = entry
	h.mu.Unlock()
	return entry
}

func (h *Health) Forget(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.seen, id)
}

// StartRefresh probes every exit in the background so the first GUI poll is
// already warm.
func (h *Health) StartRefresh(interval time.Duration, servers func() []Server) {
	go func() {
		for {
			for _, s := range servers() {
				h.Probe(s)
			}
			time.Sleep(interval)
		}
	}()
}

func trimNetErr(s string) string {
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

func readJSON(r *http.Request, dst any) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return errors.New("empty request body")
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return errors.New("request body is not valid JSON")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
