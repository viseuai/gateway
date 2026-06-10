package usage

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGRecorder persists events to Postgres. Schema is owned by the
// migrations in /migrations (applied by internal/db at startup).
type PGRecorder struct {
	pool *pgxpool.Pool
}

func NewPG(pool *pgxpool.Pool) *PGRecorder { return &PGRecorder{pool: pool} }

func (p *PGRecorder) Record(ctx context.Context, e Event) error {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO usage_events
			(occurred_at, subject, model, route, status, duration_ms,
			 prompt_tokens, completion_tokens, streamed)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		e.OccurredAt, e.Subject, e.Model, e.Route, e.Status, e.DurationMS,
		e.PromptTokens, e.CompletionTokens, e.Streamed,
	)
	return err
}
