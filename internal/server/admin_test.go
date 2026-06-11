package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/viseuai/gateway/internal/auth"
	"github.com/viseuai/gateway/internal/kcadmin"
)

// direcaoVerifier: "Bearer boss" is direção; "Bearer pleb" is a plain member.
type direcaoVerifier struct{}

func (direcaoVerifier) Verify(_ context.Context, raw string) (*auth.Identity, error) {
	switch raw {
	case "boss":
		return &auth.Identity{Subject: "boss-1", Roles: []string{"member", "direcao"}, Method: auth.MethodOIDC}, nil
	case "pleb":
		return &auth.Identity{Subject: "pleb-1", Roles: []string{"member"}, Method: auth.MethodOIDC}, nil
	}
	return nil, errors.New("invalid")
}

type fakeDirectory struct {
	grants []string
}

func (f *fakeDirectory) Members(_ context.Context) ([]kcadmin.Member, error) {
	return []kcadmin.Member{
		{ID: "u1", Username: "tarouca", Email: "t@x.pt", Roles: []string{"member", "direcao"}},
		{ID: "u2", Username: "fw4lk3r", Email: "f@x.pt", Roles: nil},
	}, nil
}

func (f *fakeDirectory) Grant(_ context.Context, userID, role string) error {
	f.grants = append(f.grants, userID+":"+role)
	return nil
}

type fakeMinter struct{ minted []time.Duration }

func (f *fakeMinter) Mint(_ context.Context, expiry time.Duration) (string, error) {
	f.minted = append(f.minted, expiry)
	return "mesh-key-plaintext", nil
}

func adminServer(dir *fakeDirectory, minter *fakeMinter) http.Handler {
	return New(Config{
		Verifier: direcaoVerifier{},
		Upstream: http.NotFoundHandler(),
		Registry: fakeNodeLister{},
		Admin:    &AdminConfig{Directory: dir, MeshKeys: minter},
	})
}

func adminReq(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rd *strings.Reader
	if body == "" {
		rd = strings.NewReader("")
	} else {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAdminRoutesRequireDirecao(t *testing.T) {
	h := adminServer(&fakeDirectory{}, &fakeMinter{})
	for _, c := range [][2]string{
		{http.MethodGet, "/v1/admin/members"},
		{http.MethodPost, "/v1/admin/mesh-keys"},
		{http.MethodGet, "/v1/admin/nodes"},
	} {
		rec := adminReq(t, h, c[0], c[1], "pleb", `{}`)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as member: got %d, want 403", c[0], c[1], rec.Code)
		}
	}
}

func TestAdminListsMembers(t *testing.T) {
	h := adminServer(&fakeDirectory{}, &fakeMinter{})
	rec := adminReq(t, h, http.MethodGet, "/v1/admin/members", "boss", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d (body %s)", rec.Code, rec.Body)
	}
	var body struct {
		Data []struct {
			Username string   `json:"username"`
			Roles    []string `json:"roles"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 2 || body.Data[1].Username != "fw4lk3r" {
		t.Errorf("members: %+v", body.Data)
	}
}

func TestAdminGrantsAllowedRole(t *testing.T) {
	dir := &fakeDirectory{}
	h := adminServer(dir, &fakeMinter{})
	rec := adminReq(t, h, http.MethodPost, "/v1/admin/members/u2/roles", "boss", `{"role":"member"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: %d (body %s)", rec.Code, rec.Body)
	}
	if len(dir.grants) != 1 || dir.grants[0] != "u2:member" {
		t.Errorf("grants: %v", dir.grants)
	}
}

func TestAdminRefusesPrivilegedRoles(t *testing.T) {
	dir := &fakeDirectory{}
	h := adminServer(dir, &fakeMinter{})
	for _, role := range []string{"direcao", "auditor", "technical-committee"} {
		rec := adminReq(t, h, http.MethodPost, "/v1/admin/members/u2/roles", "boss", `{"role":"`+role+`"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("granting %s: got %d, want 400", role, rec.Code)
		}
	}
	if len(dir.grants) != 0 {
		t.Errorf("privileged grant slipped through: %v", dir.grants)
	}
}

func TestAdminMintsMeshKey(t *testing.T) {
	minter := &fakeMinter{}
	h := adminServer(&fakeDirectory{}, minter)
	rec := adminReq(t, h, http.MethodPost, "/v1/admin/mesh-keys", "boss", `{}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d (body %s)", rec.Code, rec.Body)
	}
	var body struct {
		Key string `json:"key"`
	}
	json.NewDecoder(rec.Body).Decode(&body)
	if body.Key != "mesh-key-plaintext" {
		t.Errorf("key: %q", body.Key)
	}
	if len(minter.minted) != 1 || minter.minted[0] != 72*time.Hour {
		t.Errorf("default expiry: %v, want 72h", minter.minted)
	}
}
