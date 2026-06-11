package auth

import (
	"context"
	"errors"
	"testing"
)

type namedVerifier string

func (n namedVerifier) Verify(_ context.Context, raw string) (*Identity, error) {
	if raw == "boom" {
		return nil, errors.New("rejected")
	}
	return &Identity{Subject: string(n)}, nil
}

func TestPrefixVerifierRoutes(t *testing.T) {
	v := PrefixVerifier("vsk_", namedVerifier("keys"), namedVerifier("oidc"))

	id, err := v.Verify(context.Background(), "vsk_abc123")
	if err != nil || id.Subject != "keys" {
		t.Errorf("prefixed token: got %v/%v, want keys verifier", id, err)
	}

	id, err = v.Verify(context.Background(), "eyJhbGciOi.something.jwt")
	if err != nil || id.Subject != "oidc" {
		t.Errorf("unprefixed token: got %v/%v, want fallback verifier", id, err)
	}
}
