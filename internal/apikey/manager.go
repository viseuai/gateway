package apikey

import (
	"context"
	"fmt"
)

// ManagerStore is the persistence the management API needs.
type ManagerStore interface {
	Insert(ctx context.Context, subject, name, hash string, roles []string) (Key, error)
	BySubject(ctx context.Context, subject string) ([]Key, error)
	RevokeOwned(ctx context.Context, subject string, id int64) error
}

// Manager implements key lifecycle for the management endpoints.
type Manager struct {
	store ManagerStore
}

func NewManager(s ManagerStore) *Manager { return &Manager{store: s} }

// Create mints a key for subject. The plaintext is returned exactly once.
func (m *Manager) Create(ctx context.Context, subject, name string) (string, Key, error) {
	plaintext, hash, err := Generate()
	if err != nil {
		return "", Key{}, err
	}
	// API keys carry the member role; finer scoping is a later iteration.
	key, err := m.store.Insert(ctx, subject, name, hash, []string{"member"})
	if err != nil {
		return "", Key{}, fmt.Errorf("storing key: %w", err)
	}
	return plaintext, key, nil
}

func (m *Manager) List(ctx context.Context, subject string) ([]Key, error) {
	return m.store.BySubject(ctx, subject)
}

func (m *Manager) Revoke(ctx context.Context, subject string, id int64) error {
	return m.store.RevokeOwned(ctx, subject, id)
}
