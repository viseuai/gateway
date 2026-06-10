package usage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/viseuai/gateway/internal/db"
)

// Integration test; runs when TEST_DATABASE_URL points at a Postgres.
// CI provides one via service container; locally:
//
//	docker run --rm -e POSTGRES_PASSWORD=t -p 5499:5432 postgres:17-alpine
//	TEST_DATABASE_URL=postgres://postgres:t@localhost:5499/postgres go test ./internal/usage/
func TestPGRecorderRoundTrip(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn) // applies migrations
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	pt, ct := 5, 7
	rec := NewPG(pool)
	err = rec.Record(ctx, Event{
		OccurredAt: time.Now(), Subject: "it-user", Model: "m",
		Route: "/v1/chat/completions", Status: 200, DurationMS: 12,
		PromptTokens: &pt, CompletionTokens: &ct,
	})
	if err != nil {
		t.Fatal(err)
	}

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM usage_events WHERE subject = 'it-user'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("event not persisted")
	}
}
