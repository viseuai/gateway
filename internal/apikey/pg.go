package apikey

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore implements Store and ManagerStore on Postgres.
type PGStore struct {
	pool *pgxpool.Pool
}

func NewPG(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

func (s *PGStore) Lookup(ctx context.Context, hash string) (*Record, error) {
	var rec Record
	err := s.pool.QueryRow(ctx, `
		SELECT subject, roles, revoked_at IS NOT NULL
		FROM api_keys WHERE key_hash = $1`, hash,
	).Scan(&rec.Subject, &rec.Roles, &rec.Revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *PGStore) Touch(ctx context.Context, hash string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE api_keys SET last_used_at = now() WHERE key_hash = $1`, hash)
	return err
}

func (s *PGStore) Insert(ctx context.Context, subject, name, hash string, roles []string) (Key, error) {
	var k Key
	err := s.pool.QueryRow(ctx, `
		INSERT INTO api_keys (key_hash, subject, name, roles)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, created_at`, hash, subject, name, roles,
	).Scan(&k.ID, &k.Name, &k.CreatedAt)
	return k, err
}

func (s *PGStore) BySubject(ctx context.Context, subject string) ([]Key, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, created_at, last_used_at, revoked_at
		FROM api_keys WHERE subject = $1 ORDER BY id`, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []Key
	for rows.Next() {
		var k Key
		if err := rows.Scan(&k.ID, &k.Name, &k.CreatedAt, &k.LastUsed, &k.RevokedAt); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s *PGStore) RevokeOwned(ctx context.Context, subject string, id int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE api_keys SET revoked_at = now()
		WHERE id = $1 AND subject = $2 AND revoked_at IS NULL`, id, subject)
	return err
}
