package apikey

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/viseuai/gateway/internal/auth"
)

func TestGenerateShapeAndHash(t *testing.T) {
	plain, hash, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plain, Prefix) {
		t.Errorf("key %q missing %q prefix", plain, Prefix)
	}
	if len(plain) < len(Prefix)+32 {
		t.Errorf("key too short: %d chars", len(plain))
	}
	if Hash(plain) != hash {
		t.Error("Hash(plaintext) must equal the returned hash")
	}

	plain2, hash2, _ := Generate()
	if plain == plain2 || hash == hash2 {
		t.Error("two generated keys must differ")
	}
}

type fakeStore struct {
	records map[string]*Record // by hash
	touched []string
	fail    bool
}

func (f *fakeStore) Lookup(_ context.Context, hash string) (*Record, error) {
	if f.fail {
		return nil, errors.New("db down")
	}
	r, ok := f.records[hash]
	if !ok {
		return nil, ErrNotFound
	}
	return r, nil
}

func (f *fakeStore) Touch(_ context.Context, hash string) error {
	f.touched = append(f.touched, hash)
	return nil
}

func TestVerifierAcceptsValidKey(t *testing.T) {
	plain, hash, _ := Generate()
	st := &fakeStore{records: map[string]*Record{
		hash: {Subject: "user-9", Roles: []string{"member"}},
	}}

	id, err := NewVerifier(st).Verify(context.Background(), plain)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if id.Subject != "user-9" {
		t.Errorf("subject: got %q", id.Subject)
	}
	if id.Method != auth.MethodAPIKey {
		t.Errorf("method: got %q, want %q", id.Method, auth.MethodAPIKey)
	}
	if len(st.touched) != 1 {
		t.Errorf("last_used_at not touched (got %d touches)", len(st.touched))
	}
}

func TestVerifierRejectsRevokedKey(t *testing.T) {
	plain, hash, _ := Generate()
	st := &fakeStore{records: map[string]*Record{
		hash: {Subject: "user-9", Roles: []string{"member"}, Revoked: true},
	}}
	if _, err := NewVerifier(st).Verify(context.Background(), plain); err == nil {
		t.Fatal("revoked key must not verify")
	}
}

func TestVerifierRejectsUnknownAndMalformed(t *testing.T) {
	st := &fakeStore{records: map[string]*Record{}}
	v := NewVerifier(st)
	if _, err := v.Verify(context.Background(), Prefix+"deadbeef"); err == nil {
		t.Error("unknown key must not verify")
	}
	if _, err := v.Verify(context.Background(), "not-a-key"); err == nil {
		t.Error("token without prefix must not verify")
	}
}
