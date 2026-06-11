package apikey

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/viseuai/gateway/internal/auth"
)

// Store is the persistence the verifier needs.
type Store interface {
	Lookup(ctx context.Context, hash string) (*Record, error)
	Touch(ctx context.Context, hash string) error // best-effort last_used_at
}

// Verifier authenticates vsk_ tokens against the store.
type Verifier struct {
	store Store
}

func NewVerifier(s Store) *Verifier { return &Verifier{store: s} }

func (v *Verifier) Verify(ctx context.Context, rawToken string) (*auth.Identity, error) {
	if !strings.HasPrefix(rawToken, Prefix) {
		return nil, errors.New("not an api key")
	}
	hash := Hash(rawToken)
	rec, err := v.store.Lookup(ctx, hash)
	if err != nil {
		return nil, err
	}
	if rec.Revoked {
		return nil, errors.New("api key revoked")
	}
	if err := v.store.Touch(ctx, hash); err != nil {
		log.Printf("apikey: touching last_used_at: %v", err)
	}
	return &auth.Identity{
		Subject: rec.Subject,
		Roles:   rec.Roles,
		Method:  auth.MethodAPIKey,
	}, nil
}
