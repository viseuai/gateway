package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/viseuai/gateway/internal/auth"
)

// stubVerifier accepts exactly the token "good".
type stubVerifier struct{}

func (stubVerifier) Verify(_ context.Context, raw string) (*auth.Identity, error) {
	if raw != "good" {
		return nil, errors.New("invalid token")
	}
	return &auth.Identity{Subject: "user-123", Email: "membro@viseuai.org", Roles: []string{"member"}}, nil
}

func newTestServer() http.Handler {
	return New(Config{Verifier: stubVerifier{}})
}

func get(t *testing.T, h http.Handler, path, authz string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthzIsPublic(t *testing.T) {
	rec := get(t, newTestServer(), "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /healthz: got status %d, want %d", rec.Code, http.StatusOK)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status field: got %q, want %q", body.Status, "ok")
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	rec := get(t, newTestServer(), "/nope", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /nope: got status %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestMeRequiresAuth(t *testing.T) {
	rec := get(t, newTestServer(), "/v1/me", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /v1/me without token: got status %d, want 401", rec.Code)
	}
}

func TestMeReturnsIdentity(t *testing.T) {
	rec := get(t, newTestServer(), "/v1/me", "Bearer good")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/me: got status %d, want 200 (body: %s)", rec.Code, rec.Body)
	}
	var body struct {
		Subject string   `json:"subject"`
		Email   string   `json:"email"`
		Roles   []string `json:"roles"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if body.Subject != "user-123" || len(body.Roles) != 1 || body.Roles[0] != "member" {
		t.Errorf("identity: got %+v", body)
	}
}
