package quota

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/viseuai/gateway/internal/auth"
)

var testNow = time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)

var (
	dayStart   = time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)
	monthStart = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
)

type fakeStore struct {
	totals map[time.Time]struct {
		reqs   int
		tokens int64
	}
	err error
}

func (f *fakeStore) Totals(_ context.Context, _ string, since time.Time) (int, int64, error) {
	if f.err != nil {
		return 0, 0, f.err
	}
	t := f.totals[since]
	return t.reqs, t.tokens, nil
}

func makeStore(dayReqs int, monthTokens int64) *fakeStore {
	return &fakeStore{totals: map[time.Time]struct {
		reqs   int
		tokens int64
	}{
		dayStart:   {reqs: dayReqs},
		monthStart: {tokens: monthTokens},
	}}
}

func run(t *testing.T, c *Checker) (*httptest.ResponseRecorder, *bool) {
	t.Helper()
	reached := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.Write([]byte(`{}`))
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	id := &auth.Identity{Subject: "user-123", Roles: []string{"member"}}
	req = req.WithContext(auth.WithIdentity(req.Context(), id))
	rec := httptest.NewRecorder()
	c.Middleware(next).ServeHTTP(rec, req)
	return rec, &reached
}

func assertQuotaDenied(t *testing.T, rec *httptest.ResponseRecorder, reached bool) {
	t.Helper()
	if reached {
		t.Fatal("upstream must not be called when quota is exceeded")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status: got %d, want 429", rec.Code)
	}
	var body struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("429 body not JSON: %v", err)
	}
	if body.Error.Type != "insufficient_quota" {
		t.Errorf("error.type: got %q, want insufficient_quota", body.Error.Type)
	}
}

func newChecker(s Store, l Limits) *Checker {
	c := New(s, l)
	c.now = func() time.Time { return testNow }
	return c
}

func TestUnderLimitsPasses(t *testing.T) {
	c := newChecker(makeStore(10, 1000), Limits{DailyRequests: 100, MonthlyTokens: 100000})
	rec, reached := run(t, c)
	if !*reached || rec.Code != http.StatusOK {
		t.Fatalf("under-limit request blocked: reached=%v status=%d", *reached, rec.Code)
	}
}

func TestDailyRequestLimitBlocks(t *testing.T) {
	c := newChecker(makeStore(100, 0), Limits{DailyRequests: 100, MonthlyTokens: 100000})
	rec, reached := run(t, c)
	assertQuotaDenied(t, rec, *reached)
}

func TestMonthlyTokenLimitBlocks(t *testing.T) {
	c := newChecker(makeStore(1, 100000), Limits{DailyRequests: 100, MonthlyTokens: 100000})
	rec, reached := run(t, c)
	assertQuotaDenied(t, rec, *reached)
}

func TestStoreErrorFailsOpen(t *testing.T) {
	c := newChecker(&fakeStore{err: errors.New("db down")}, Limits{DailyRequests: 1, MonthlyTokens: 1})
	rec, reached := run(t, c)
	if !*reached || rec.Code != http.StatusOK {
		t.Fatalf("quota must fail open on store errors: reached=%v status=%d", *reached, rec.Code)
	}
}
