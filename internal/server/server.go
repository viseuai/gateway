// Package server wires the gateway's HTTP surface.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/viseuai/gateway/internal/auth"
	"github.com/viseuai/gateway/internal/usage"
)

// Config carries the server's dependencies.
type Config struct {
	Verifier auth.Verifier
	Upstream http.Handler   // OpenAI-compatible inference backend proxy
	Usage    usage.Recorder // metadata-only accounting; nil disables
}

// New returns the gateway's root HTTP handler. /healthz is public;
// everything under /v1 requires authentication.
func New(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)

	completions := cfg.Upstream
	if cfg.Usage != nil {
		completions = usage.Middleware(cfg.Usage)(completions)
	}

	protected := auth.Middleware(cfg.Verifier)
	mux.Handle("GET /v1/me", protected(http.HandlerFunc(handleMe)))
	mux.Handle("POST /v1/chat/completions", protected(completions))
	mux.Handle("GET /v1/models", protected(cfg.Upstream))

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
