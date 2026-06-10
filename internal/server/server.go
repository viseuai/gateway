// Package server wires the gateway's HTTP surface.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/viseuai/gateway/internal/auth"
)

// Config carries the server's dependencies.
type Config struct {
	Verifier auth.Verifier
}

// New returns the gateway's root HTTP handler. /healthz is public;
// everything under /v1 requires authentication.
func New(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)

	protected := auth.Middleware(cfg.Verifier)
	mux.Handle("GET /v1/me", protected(http.HandlerFunc(handleMe)))

	return mux
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"subject": id.Subject,
		"email":   id.Email,
		"roles":   id.Roles,
	})
}
