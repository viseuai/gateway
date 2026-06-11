package server

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/viseuai/gateway/internal/kcadmin"
	"github.com/viseuai/gateway/internal/registry"
)

// MemberDirectory is the identity store the admin surface manages.
type MemberDirectory interface {
	Members(ctx context.Context) ([]kcadmin.Member, error)
	Grant(ctx context.Context, userID, role string) error
}

// MeshKeyMinter creates mesh pre-auth keys for node onboarding.
type MeshKeyMinter interface {
	Mint(ctx context.Context, expiry time.Duration) (string, error)
}

// AdminConfig enables the direção's self-service surface.
type AdminConfig struct {
	Directory MemberDirectory
	MeshKeys  MeshKeyMinter
}

// grantableRoles via the API. Privileged roles (direcao, auditor, ...)
// stay a deliberate CLI act with an audit trail.
var grantableRoles = []string{"member", "volunteer-operator"}

func registerAdmin(mux *http.ServeMux, protected func(http.Handler) http.Handler,
	admin *AdminConfig, reg NodeRegistry, ttl time.Duration) {

	gate := func(h http.HandlerFunc) http.Handler {
		return protected(roleOnly("direcao", h))
	}

	mux.Handle("GET /v1/admin/members", gate(func(w http.ResponseWriter, r *http.Request) {
		members, err := admin.Directory.Members(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not list members.", "api_error")
			return
		}
		writeJSON(w, map[string]any{"data": members})
	}))

	mux.Handle("POST /v1/admin/members/{id}/roles", gate(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Role == "" {
			writeError(w, http.StatusBadRequest, "Provide a JSON body with a role.", "invalid_request_error")
			return
		}
		if !slices.Contains(grantableRoles, req.Role) {
			writeError(w, http.StatusBadRequest,
				"Only member and volunteer-operator can be granted here.", "invalid_request_error")
			return
		}
		if err := admin.Directory.Grant(r.Context(), r.PathValue("id"), req.Role); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not grant the role.", "api_error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.Handle("POST /v1/admin/mesh-keys", gate(func(w http.ResponseWriter, r *http.Request) {
		expiry := 72 * time.Hour
		var req struct {
			ExpirationHours int `json:"expiration_hours"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		if req.ExpirationHours > 0 {
			expiry = time.Duration(req.ExpirationHours) * time.Hour
		}
		key, err := admin.MeshKeys.Mint(r.Context(), expiry)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Could not mint a mesh key.", "api_error")
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]any{"key": key, "expires_in_hours": int(expiry.Hours()), "tags": []string{"tag:node"}})
	}))

	if reg != nil {
		mux.Handle("GET /v1/admin/nodes", gate(func(w http.ResponseWriter, r *http.Request) {
			nodes, err := reg.AllNodes(r.Context(), ttl)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "Could not list nodes.", "api_error")
				return
			}
			if nodes == nil {
				nodes = []registry.NodeStatus{}
			}
			writeJSON(w, map[string]any{"data": nodes})
		}))
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
