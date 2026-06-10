package usage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/viseuai/gateway/internal/auth"
)

type captureRecorder struct {
	mu     sync.Mutex
	events []Event
	fail   bool
}

func (c *captureRecorder) Record(_ context.Context, e Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return errors.New("db down")
	}
	c.events = append(c.events, e)
	return nil
}

func (c *captureRecorder) last(t *testing.T) Event {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) == 0 {
		t.Fatal("no usage event recorded")
	}
	return c.events[len(c.events)-1]
}

func member(r *http.Request) *http.Request {
	id := &auth.Identity{Subject: "user-123", Roles: []string{"member"}}
	return r.WithContext(auth.WithIdentity(r.Context(), id))
}

func jsonUpstream(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, body)
	})
}

func sseUpstream() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[]}\n\ndata: [DONE]\n\n")
	})
}

func post(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, member(req))
	return rec
}

func TestRecordsModelSubjectStatusAndTokens(t *testing.T) {
	rec := &captureRecorder{}
	h := Middleware(rec)(jsonUpstream(`{"id":"x","usage":{"prompt_tokens":5,"completion_tokens":7}}`))

	res := post(h, `{"model":"qwen2.5-0.5b-instruct","messages":[]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("status: %d", res.Code)
	}

	e := rec.last(t)
	if e.Subject != "user-123" {
		t.Errorf("subject: got %q", e.Subject)
	}
	if e.Model != "qwen2.5-0.5b-instruct" {
		t.Errorf("model: got %q", e.Model)
	}
	if e.Status != http.StatusOK {
		t.Errorf("status: got %d", e.Status)
	}
	if e.PromptTokens == nil || *e.PromptTokens != 5 {
		t.Errorf("prompt tokens: got %v", e.PromptTokens)
	}
	if e.CompletionTokens == nil || *e.CompletionTokens != 7 {
		t.Errorf("completion tokens: got %v", e.CompletionTokens)
	}
	if e.Streamed {
		t.Error("streamed should be false for JSON response")
	}
	if e.DurationMS < 0 {
		t.Errorf("duration: got %d", e.DurationMS)
	}
}

func TestUpstreamBodyStillReachesBackend(t *testing.T) {
	var received string
	backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 1024)
		n, _ := r.Body.Read(b)
		received = string(b[:n])
		w.Write([]byte(`{}`))
	})
	h := Middleware(&captureRecorder{})(backend)

	post(h, `{"model":"m","messages":[{"role":"user","content":"olá"}]}`)
	if !strings.Contains(received, "olá") {
		t.Errorf("backend did not receive the request body: %q", received)
	}
}

func TestStreamedResponseRecordedWithoutTokens(t *testing.T) {
	rec := &captureRecorder{}
	h := Middleware(rec)(sseUpstream())

	post(h, `{"model":"m","stream":true}`)
	e := rec.last(t)
	if !e.Streamed {
		t.Error("streamed: got false, want true")
	}
	if e.PromptTokens != nil || e.CompletionTokens != nil {
		t.Errorf("tokens should be nil for v1 streamed responses, got %v/%v", e.PromptTokens, e.CompletionTokens)
	}
}

func TestRecorderFailureDoesNotFailRequest(t *testing.T) {
	h := Middleware(&captureRecorder{fail: true})(jsonUpstream(`{"id":"x"}`))
	res := post(h, `{"model":"m"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("request must succeed even when recording fails: got %d", res.Code)
	}
	if res.Body.String() != `{"id":"x"}` {
		t.Errorf("body altered: %s", res.Body)
	}
}
