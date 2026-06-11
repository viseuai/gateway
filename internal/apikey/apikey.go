// Package apikey implements developer API keys: vsk_-prefixed bearer
// tokens whose SHA-256 hashes live in Postgres. Plaintext is shown once
// at creation and never stored.
package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// Prefix distinguishes API keys from OIDC JWTs in the Authorization header.
const Prefix = "vsk_"

// ErrNotFound: no key with that hash.
var ErrNotFound = errors.New("api key not found")

// Record is a stored key as the verifier sees it.
type Record struct {
	Subject string
	Roles   []string
	Revoked bool
}

// Key is the metadata exposed on management endpoints. Never the secret.
type Key struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// Generate returns a new plaintext key and its storage hash.
func Generate() (plaintext, hash string, err error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generating key material: %w", err)
	}
	plaintext = Prefix + hex.EncodeToString(raw)
	return plaintext, Hash(plaintext), nil
}

// Hash maps a plaintext key to its storage form.
func Hash(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
