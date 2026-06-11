package quota

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore aggregates usage_events (written by internal/usage).
type PGStore struct {
	pool *pgxpool.Pool
}

func NewPG(pool *pgxpool.Pool) *PGStore { return &PGStore{pool: pool} }

func (s *PGStore) Totals(ctx context.Context, subject string, since time.Time) (int, int64, error) {
	var requests int
	var tokens int64
	err := s.pool.QueryRow(ctx, `
		SELECT count(*),
		       COALESCE(SUM(COALESCE(prompt_tokens, 0) + COALESCE(completion_tokens, 0)), 0)
		FROM usage_events
		WHERE subject = $1 AND occurred_at >= $2`, subject, since,
	).Scan(&requests, &tokens)
	return requests, tokens, err
}
