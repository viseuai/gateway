// Package quota enforces per-subject caps: requests per UTC day and
// tokens per calendar month, computed from recorded usage. The platform
// is "gated and capped per user" by design (VISION.md).
package quota

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/viseuai/gateway/internal/auth"
)

// Limits are the per-subject caps. Zero values mean "no limit".
type Limits struct {
	DailyRequests int
	MonthlyTokens int64
}

// Store aggregates recorded usage for a subject since a point in time.
type Store interface {
	Totals(ctx context.Context, subject string, since time.Time) (requests int, tokens int64, err error)
}

// Checker evaluates limits against the store.
type Checker struct {
	store  Store
	limits Limits
	now    func() time.Time
}

func New(store Store, limits Limits) *Checker {
	return &Checker{store: store, limits: limits, now: time.Now}
}

// Middleware denies requests over quota with 429. Store failures fail
// OPEN: quota protects capacity, and a database hiccup must not take the
// service down with it.
func (c *Checker) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := auth.IdentityFrom(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		now := c.now().UTC()

		if c.limits.DailyRequests > 0 {
			dayStart := now.Truncate(24 * time.Hour)
			reqs, _, err := c.store.Totals(r.Context(), id.Subject, dayStart)
			if err != nil {
				log.Printf("quota: totals (daily): %v", err)
			} else if reqs >= c.limits.DailyRequests {
				deny(w, fmt.Sprintf(
					"Daily request quota exceeded (%d requests/day). Quotas reset at 00:00 UTC.",
					c.limits.DailyRequests))
				return
			}
		}

		if c.limits.MonthlyTokens > 0 {
			monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			_, tokens, err := c.store.Totals(r.Context(), id.Subject, monthStart)
			if err != nil {
				log.Printf("quota: totals (monthly): %v", err)
			} else if tokens >= c.limits.MonthlyTokens {
				deny(w, fmt.Sprintf(
					"Monthly token quota exceeded (%d tokens/month). Quotas reset on the 1st.",
					c.limits.MonthlyTokens))
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func deny(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{
			"message": msg,
			"type":    "insufficient_quota",
		},
	})
}
