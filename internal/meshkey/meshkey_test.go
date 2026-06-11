package meshkey

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMintCreatesTaggedPreAuthKey(t *testing.T) {
	var got map[string]any
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/preauthkey" || r.Method != http.MethodPost {
			t.Errorf("unexpected call: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer hs-api-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{
			"preAuthKey": map[string]any{"key": "minted-mesh-key"},
		})
	}))
	t.Cleanup(hs.Close)

	c := New(hs.URL, "hs-api-key", "1")
	key, err := c.Mint(context.Background(), 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if key != "minted-mesh-key" {
		t.Errorf("key: %q", key)
	}
	if got["user"] != "1" {
		t.Errorf("user: %v", got["user"])
	}
	tags, _ := got["aclTags"].([]any)
	if len(tags) != 1 || tags[0] != "tag:node" {
		t.Errorf("aclTags: %v", got["aclTags"])
	}
	if got["reusable"] != false {
		t.Errorf("reusable: %v", got["reusable"])
	}
	if _, err := time.Parse(time.RFC3339, got["expiration"].(string)); err != nil {
		t.Errorf("expiration not RFC3339: %v", got["expiration"])
	}
}
