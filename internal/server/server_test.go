package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/viseuai/gateway/internal/auth"
	"github.com/viseuai/gateway/internal/usage"
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
	return newTestServerWithUpstream(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"cmpl-upstream"}`))
	}))
}

func newTestServerWithUpstream(upstream http.Handler) http.Handler {
	return New(Config{Verifier: stubVerifier{}, Upstream: upstream})
}

func TestChatCompletionsRequiresAuth(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	newTestServer().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /v1/chat/completions without token: got %d, want 401", rec.Code)
	}
}

func TestChatCompletionsForwardsToUpstream(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer good")
	rec := httptest.NewRecorder()
	newTestServer().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d (body: %s)", rec.Code, rec.Body)
	}
	if got := rec.Body.String(); got != `{"id":"cmpl-upstream"}` {
		t.Errorf("upstream body not forwarded: %s", got)
	}
}

type captureUsage struct{ events []usage.Event }

func (c *captureUsage) Record(_ context.Context, e usage.Event) error {
	c.events = append(c.events, e)
	return nil
}

func TestCompletionsRecordUsageWithIdentity(t *testing.T) {
	rec := &captureUsage{}
	srv := New(Config{
		Verifier: stubVerifier{},
		Upstream: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte(`{}`))
		}),
		Usage: rec,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"qwen2.5-0.5b-instruct"}`))
	req.Header.Set("Authorization", "Bearer good")
	srv.ServeHTTP(httptest.NewRecorder(), req)

	if len(rec.events) != 1 {
		t.Fatalf("usage events: got %d, want 1", len(rec.events))
	}
	if rec.events[0].Subject != "user-123" {
		t.Errorf("subject: got %q (auth identity must reach usage)", rec.events[0].Subject)
	}
	if rec.events[0].Model != "qwen2.5-0.5b-instruct" {
		t.Errorf("model: got %q", rec.events[0].Model)
	}
}

func TestModelsListRequiresAuthAndForwards(t *testing.T) {
	srv := newTestServer()

	rec := get(t, srv, "/v1/models", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /v1/models without token: got %d, want 401", rec.Code)
	}

	rec = get(t, srv, "/v1/models", "Bearer good")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/models: got %d", rec.Code)
	}
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
