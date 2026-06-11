package kcadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fake Keycloak: token endpoint + admin users/roles endpoints.
func fakeKC(t *testing.T, granted *[]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /realms/viseu/protocol/openid-connect/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.PostForm.Get("client_id") != "gateway-admin" || r.PostForm.Get("client_secret") != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "admin-token", "expires_in": 60})
	})
	requireAuth := func(r *http.Request) bool { return r.Header.Get("Authorization") == "Bearer admin-token" }

	mux.HandleFunc("GET /admin/realms/viseu/users", func(w http.ResponseWriter, r *http.Request) {
		if !requireAuth(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `[{"id":"u1","username":"tarouca","email":"t@x.pt"},{"id":"u2","username":"fw4lk3r","email":"f@x.pt"}]`)
	})
	mux.HandleFunc("GET /admin/realms/viseu/roles/member/users", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"id":"u1"}]`)
	})
	mux.HandleFunc("GET /admin/realms/viseu/roles/member", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"role-member-id","name":"member"}`)
	})
	mux.HandleFunc("POST /admin/realms/viseu/users/{id}/role-mappings/realm", func(w http.ResponseWriter, r *http.Request) {
		var roles []struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&roles)
		*granted = append(*granted, r.PathValue("id")+":"+roles[0].Name)
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestMembersMergesRoles(t *testing.T) {
	var granted []string
	kc := fakeKC(t, &granted)
	c := New(kc.URL, "viseu", "gateway-admin", "s3cret")

	members, err := c.Members(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("members: %+v", members)
	}
	if members[0].Username != "tarouca" || len(members[0].Roles) == 0 || members[0].Roles[0] != "member" {
		t.Errorf("tarouca should carry member role: %+v", members[0])
	}
	if len(members[1].Roles) != 0 {
		t.Errorf("fw4lk3r should have no roles yet: %+v", members[1])
	}
}

func TestGrantPostsRoleMapping(t *testing.T) {
	var granted []string
	kc := fakeKC(t, &granted)
	c := New(kc.URL, "viseu", "gateway-admin", "s3cret")

	if err := c.Grant(context.Background(), "u2", "member"); err != nil {
		t.Fatal(err)
	}
	if len(granted) != 1 || granted[0] != "u2:member" {
		t.Errorf("granted: %v", granted)
	}
}
