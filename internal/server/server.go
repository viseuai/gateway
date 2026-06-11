// Package server wires the gateway's HTTP surface.
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/viseuai/gateway/internal/apikey"
	"github.com/viseuai/gateway/internal/auth"
	"github.com/viseuai/gateway/internal/usage"
)

// KeyService is the API-key lifecycle the management routes need.
type KeyService interface {
	Create(ctx context.Context, subject, name string) (string, apikey.Key, error)
	List(ctx context.Context, subject string) ([]apikey.Key, error)
	Revoke(ctx context.Context, subject string, id int64) error
}

// Config carries the server's dependencies.
type Config struct {
	Verifier auth.Verifier
	Upstream http.Handler   // OpenAI-compatible inference backend proxy
	Usage    usage.Recorder // metadata-only accounting; nil disables
	Keys     KeyService     // api key management; nil disables the routes
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

	if cfg.Keys != nil {
		h := keyHandlers{keys: cfg.Keys}
		mux.Handle("POST /v1/keys", protected(oidcOnly(h.create)))
		mux.Handle("GET /v1/keys", protected(oidcOnly(h.list)))
		mux.Handle("DELETE /v1/keys/{id}", protected(oidcOnly(h.revoke)))
	}

	return mux
}

// oidcOnly forbids api-key-authenticated callers: a key must never be able
// to mint, list, or revoke keys.
func oidcOnly(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := auth.IdentityFrom(r.Context())
		if id.Method == auth.MethodAPIKey {
			writeError(w, http.StatusForbidden,
				"Key management requires an interactive session, not an API key.",
				"permission_error")
			return
		}
		next(w, r)
	})
}

type keyHandlers struct{ keys KeyService }

func (h keyHandlers) create(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "Provide a JSON body with a non-empty name.", "invalid_request_error")
		return
	}
	plaintext, key, err := h.keys.Create(r.Context(), id.Subject, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create the key.", "api_error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"id":   key.ID,
		"name": key.Name,
		"key":  plaintext, // shown exactly once
	})
}

func (h keyHandlers) list(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	keys, err := h.keys.List(r.Context(), id.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not list keys.", "api_error")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"data": keys})
}

func (h keyHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.IdentityFrom(r.Context())
	keyID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Key id must be numeric.", "invalid_request_error")
		return
	}
	if err := h.keys.Revoke(r.Context(), id.Subject, keyID); err != nil {
		writeError(w, http.StatusInternalServerError, "Could not revoke the key.", "api_error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeError(w http.ResponseWriter, status int, msg, typ string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": msg, "type": typ},
	})
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
