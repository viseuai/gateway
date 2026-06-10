// Package auth authenticates requests to the gateway.
//
// v1 verifies OIDC bearer tokens issued by the association's Keycloak.
// API keys (hashed in Postgres) arrive in a later iteration as a second
// Verifier implementation behind the same middleware.
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// Identity is the authenticated caller as the rest of the gateway sees it.
type Identity struct {
	Subject string
	Email   string
	Roles   []string
}

// Verifier turns a raw bearer token into an Identity.
type Verifier interface {
	Verify(ctx context.Context, rawToken string) (*Identity, error)
}

type contextKey struct{}

// WithIdentity returns a context carrying the identity.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// IdentityFrom extracts the authenticated identity, if any.
func IdentityFrom(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(contextKey{}).(*Identity)
	return id, ok
}

// Middleware enforces bearer-token authentication, rejecting with the
// OpenAI-compatible error shape the rest of the API uses.
func Middleware(v Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := bearer(r)
			if !ok {
				unauthorized(w, "Missing bearer token. Pass it in the Authorization header.")
				return
			}
			id, err := v.Verify(r.Context(), raw)
			if err != nil {
				unauthorized(w, "Invalid or expired token.")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	scheme, token, found := strings.Cut(h, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

func unauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"message": msg,
			"type":    "authentication_error",
		},
	})
}
