package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/viseuai/gateway/internal/apikey"
	"github.com/viseuai/gateway/internal/registry"
)

// ---- CORS ----

func corsServer() http.Handler {
	return New(Config{
		Verifier:    stubVerifier{},
		Upstream:    http.NotFoundHandler(),
		CORSOrigins: []string{"https://chat.viseuai.org", "https://platform.viseuai.org"},
	})
}

func TestPreflightAllowedOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/v1/chat/completions", nil)
	req.Header.Set("Origin", "https://chat.viseuai.org")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	corsServer().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status: got %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://chat.viseuai.org" {
		t.Errorf("allow-origin: %q", got)
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Errorf("allow-headers: %q", rec.Header().Get("Access-Control-Allow-Headers"))
	}
}

func TestPreflightUnknownOriginGetsNoCORS(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/v1/chat/completions", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	corsServer().ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("unknown origin must get no allow-origin, got %q", got)
	}
}

func TestActualRequestCarriesCORSHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://platform.viseuai.org")
	rec := httptest.NewRecorder()
	corsServer().ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://platform.viseuai.org" {
		t.Errorf("allow-origin on actual request: %q", got)
	}
	if !strings.Contains(rec.Header().Get("Vary"), "Origin") {
		t.Errorf("Vary must include Origin: %q", rec.Header().Get("Vary"))
	}
}

// ---- node-type keys ----

type typedFakeKeys struct {
	created []string
	roles   [][]string
}

func (f *typedFakeKeys) Create(_ context.Context, subject, name string, roles []string) (string, apikey.Key, error) {
	f.created = append(f.created, subject+"/"+name)
	f.roles = append(f.roles, roles)
	return "vsk_plaintext-once", apikey.Key{ID: 8, Name: name}, nil
}

func (f *typedFakeKeys) List(_ context.Context, _ string) ([]apikey.Key, error) { return nil, nil }
func (f *typedFakeKeys) Revoke(_ context.Context, _ string, _ int64) error     { return nil }

func postKeys(t *testing.T, keys KeyService, body string) *httptest.ResponseRecorder {
	t.Helper()
	srv := New(Config{Verifier: stubVerifier{}, Upstream: http.NotFoundHandler(), Keys: keys})
	req := httptest.NewRequest(http.MethodPost, "/v1/keys", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer good")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestCreateNodeKey(t *testing.T) {
	keys := &typedFakeKeys{}
	rec := postKeys(t, keys, `{"name":"laptop-ops","type":"node"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d (body: %s)", rec.Code, rec.Body)
	}
	if len(keys.roles) != 1 || len(keys.roles[0]) != 1 || keys.roles[0][0] != "node" {
		t.Errorf("roles passed to store: %v", keys.roles)
	}
}

func TestCreateKeyDefaultsToMemberRole(t *testing.T) {
	keys := &typedFakeKeys{}
	rec := postKeys(t, keys, `{"name":"sdk"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d", rec.Code)
	}
	if keys.roles[0][0] != "member" {
		t.Errorf("default roles: %v", keys.roles)
	}
}

func TestCreateKeyRejectsUnknownType(t *testing.T) {
	rec := postKeys(t, &typedFakeKeys{}, `{"name":"x","type":"admin"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown key type: got %d, want 400", rec.Code)
	}
}

// ---- GET /v1/nodes ----

type fakeNodeLister struct{}

func (fakeNodeLister) Upsert(_ context.Context, _ registry.Heartbeat) error { return nil }

func (fakeNodeLister) AllNodes(_ context.Context, _ time.Duration) ([]registry.NodeStatus, error) {
	return nil, nil
}

func (fakeNodeLister) NodesBySubject(_ context.Context, subject string, _ time.Duration) ([]registry.NodeStatus, error) {
	if subject != "user-123" {
		return nil, nil
	}
	return []registry.NodeStatus{{
		Node: "newton", Models: []string{"qwen-3b"},
		LastSeen: time.Now(), Online: true,
	}}, nil
}

func TestListOwnNodes(t *testing.T) {
	srv := New(Config{Verifier: stubVerifier{}, Upstream: http.NotFoundHandler(), Registry: fakeNodeLister{}})

	rec := get(t, srv, "/v1/nodes", "Bearer good")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d (body: %s)", rec.Code, rec.Body)
	}
	var body struct {
		Data []struct {
			Node   string   `json:"node"`
			Models []string `json:"models"`
			Online bool     `json:"online"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 1 || body.Data[0].Node != "newton" || !body.Data[0].Online {
		t.Errorf("nodes: %+v", body.Data)
	}
}
