package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/viseuai/gateway/internal/auth"
	"github.com/viseuai/gateway/internal/registry"
)

// nodeVerifier authenticates "vsk_node" as a node-role key and
// "vsk_member" as a plain member key.
type nodeVerifier struct{}

func (nodeVerifier) Verify(_ context.Context, raw string) (*auth.Identity, error) {
	switch raw {
	case "vsk_node":
		return &auth.Identity{Subject: "op-1", Roles: []string{"node"}, Method: auth.MethodAPIKey}, nil
	case "vsk_member":
		return &auth.Identity{Subject: "op-2", Roles: []string{"member"}, Method: auth.MethodAPIKey}, nil
	}
	return nil, errors.New("invalid")
}

type captureRegistry struct {
	upserts []registry.Heartbeat
	fail    bool
}

func (c *captureRegistry) Upsert(_ context.Context, hb registry.Heartbeat) error {
	if c.fail {
		return errors.New("db down")
	}
	c.upserts = append(c.upserts, hb)
	return nil
}

func heartbeat(t *testing.T, reg NodeRegistry, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	srv := New(Config{Verifier: nodeVerifier{}, Upstream: http.NotFoundHandler(), Registry: reg})
	req := httptest.NewRequest(http.MethodPost, "/v1/nodes/heartbeat", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestHeartbeatUpsertsModels(t *testing.T) {
	reg := &captureRegistry{}
	rec := heartbeat(t, reg, "vsk_node",
		`{"node":"newton","models":[{"id":"qwen-3b","url":"http://100.64.0.3:8090"}]}`)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d, want 204 (body: %s)", rec.Code, rec.Body)
	}
	if len(reg.upserts) != 1 {
		t.Fatalf("upserts: %d", len(reg.upserts))
	}
	hb := reg.upserts[0]
	if hb.Subject != "op-1" || hb.Node != "newton" {
		t.Errorf("heartbeat identity: %+v", hb)
	}
	if len(hb.Models) != 1 || hb.Models[0].ID != "qwen-3b" || hb.Models[0].URL != "http://100.64.0.3:8090" {
		t.Errorf("models: %+v", hb.Models)
	}
}

func TestHeartbeatRequiresNodeRole(t *testing.T) {
	rec := heartbeat(t, &captureRegistry{}, "vsk_member", `{"node":"x","models":[]}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member key heartbeating: got %d, want 403", rec.Code)
	}
}

func TestHeartbeatRejectsBadPayload(t *testing.T) {
	rec := heartbeat(t, &captureRegistry{}, "vsk_node", `{"models":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing node name: got %d, want 400", rec.Code)
	}
}
