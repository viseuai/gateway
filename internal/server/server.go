// Package server wires the gateway's HTTP surface.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/viseuai/gateway/internal/apikey"
	"github.com/viseuai/gateway/internal/auth"
	"github.com/viseuai/gateway/internal/registry"
	"github.com/viseuai/gateway/internal/usage"
)

// KeyService is the API-key lifecycle the management routes need.
type KeyService interface {
	Create(ctx context.Context, subject, name string, roles []string) (string, apikey.Key, error)
	List(ctx context.Context, subject string) ([]apikey.Key, error)
	Revoke(ctx context.Context, subject string, id int64) error
}

// NodeRegistry receives node heartbeats and lists nodes.
type NodeRegistry interface {
	Upsert(ctx context.Context, hb registry.Heartbeat) error
	NodesBySubject(ctx context.Context, subject string, ttl time.Duration) ([]registry.NodeStatus, error)
	AllNodes(ctx context.Context, ttl time.Duration) ([]registry.NodeStatus, error)
}

// keyRoles maps the public key "type" to stored roles.
var keyRoles = map[string][]string{
	"":     {"member"},
	"api":  {"member"},
	"node": {"node"},
}

// Config carries the server's dependencies.
type Config struct {
	Verifier auth.Verifier
	Upstream http.Handler                    // completions backend (single proxy or model router)
	Models   http.Handler                    // /v1/models; nil falls back to Upstream
	Usage    usage.Recorder                  // metadata-only accounting; nil disables
	Keys     KeyService                      // api key management; nil disables the routes
	Quota       func(http.Handler) http.Handler // per-subject caps; nil disables
	Registry    NodeRegistry                    // node heartbeats; nil disables
	CORSOrigins []string                        // browser origins allowed to call the API
	RegistryTTL time.Duration                   // node liveness window (default 60s)
	Admin       *AdminConfig                    // direção self-service; nil disables
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
	if cfg.Quota != nil {
		// quota gates BEFORE usage recording: denied requests are not billed
		completions = cfg.Quota(completions)
	}

	models := cfg.Models
	if models == nil {
		models = cfg.Upstream
	}

	protected := auth.Middleware(cfg.Verifier)
	mux.Handle("GET /v1/me", protected(http.HandlerFunc(handleMe)))
	mux.Handle("POST /v1/chat/completions", protected(completions))
	mux.Handle("GET /v1/models", protected(models))

	if cfg.Keys != nil {
		h := keyHandlers{keys: cfg.Keys}
		mux.Handle("POST /v1/keys", protected(oidcOnly(h.create)))
		mux.Handle("GET /v1/keys", protected(oidcOnly(h.list)))
		mux.Handle("DELETE /v1/keys/{id}", protected(oidcOnly(h.revoke)))
	}

	if cfg.Registry != nil {
		ttl := cfg.RegistryTTL
		if ttl == 0 {
			ttl = 60 * time.Second
		}
		mux.Handle("POST /v1/nodes/heartbeat",
			protected(roleOnly("node", handleHeartbeat(cfg.Registry))))
		mux.Handle("GET /v1/nodes", protected(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, _ := auth.IdentityFrom(r.Context())
			nodes, err := cfg.Registry.NodesBySubject(r.Context(), id.Subject, ttl)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "Could not list nodes.", "api_error")
				return
			}
			if nodes == nil {
				nodes = []registry.NodeStatus{}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"data": nodes})
		})))
	}

	if cfg.Admin != nil {
		ttl := cfg.RegistryTTL
		if ttl == 0 {
			ttl = 60 * time.Second
		}
		registerAdmin(mux, protected, cfg.Admin, cfg.Registry, ttl)
	}

	return cors(cfg.CORSOrigins, mux)
}

// cors wraps the mux with an origin allowlist. Non-listed origins get no
// CORS headers (the browser blocks them); requests without Origin pass
// through untouched.
func cors(origins []string, next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range origins {
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowed[origin] {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				h.Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// roleOnly forbids callers without the given realm/key role.
func roleOnly(role string, next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := auth.IdentityFrom(r.Context())
		for _, have := range id.Roles {
			if have == role {
				next(w, r)
				return
			}
		}
		writeError(w, http.StatusForbidden,
			fmt.Sprintf("This operation requires the %q role.", role), "permission_error")
	})
}

func handleHeartbeat(reg NodeRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := auth.IdentityFrom(r.Context())
		var req struct {
			Node   string             `json:"node"`
			Models []registry.ModelAd `json:"models"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Node == "" {
			writeError(w, http.StatusBadRequest,
				"Provide a JSON body with a node name and a models list.", "invalid_request_error")
			return
		}
		hb := registry.Heartbeat{Subject: id.Subject, Node: req.Node, Models: req.Models}
		if err := reg.Upsert(r.Context(), hb); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not record the heartbeat.", "api_error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
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
		Type string `json:"type"` // "" | "api" | "node"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeError(w, http.StatusBadRequest, "Provide a JSON body with a non-empty name.", "invalid_request_error")
		return
	}
	roles, ok := keyRoles[req.Type]
	if !ok {
		writeError(w, http.StatusBadRequest, `Key type must be "api" or "node".`, "invalid_request_error")
		return
	}
	plaintext, key, err := h.keys.Create(r.Context(), id.Subject, req.Name, roles)
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
