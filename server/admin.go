package server

import (
	"encoding/json"
	"net/http"
)

// adminHandler builds the admin API mux (docs/serving.md §3): GET /servers
// (the resolved topology), GET /health (readiness), and POST /reset (clear
// the engine's call counters). It listens on loopback by default (§3, §6).
func (s *Server) adminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/servers", s.handleServers)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/reset", s.handleReset)
	return mux
}

func (s *Server) handleServers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, s.topology)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	// Serve returns only after every listener is bound, so a reachable admin
	// API means the mocks are serving.
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return
	}
	s.engine.Reset()
	writeJSON(w, http.StatusOK, map[string]any{"status": "reset"})
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
