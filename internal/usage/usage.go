// Package usage records metadata-only accounting events per inference
// request: who, which model, when, status, duration, token counts.
// Request/response CONTENT is never stored here — bounded audit retention
// is a separate subsystem with its own rules (see VISION 2026-06).
package usage

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/viseuai/gateway/internal/auth"
)

// Event is one recorded inference request.
type Event struct {
	OccurredAt       time.Time
	Subject          string
	Model            string
	Route            string
	Status           int
	DurationMS       int64
	PromptTokens     *int
	CompletionTokens *int
	Streamed         bool
}

// Recorder persists events.
type Recorder interface {
	Record(ctx context.Context, e Event) error
}

// maxCapturedBody caps how much of a JSON response is buffered to extract
// token counts. Responses larger than this just lose token metadata.
const maxCapturedBody = 256 << 10

// Middleware records one Event per request flowing through next.
// Recording failures are logged, never surfaced: accounting must not take
// the service down.
func Middleware(rec Recorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			model := peekModel(r)

			cw := &captureWriter{ResponseWriter: w}
			next.ServeHTTP(cw, r)

			e := Event{
				OccurredAt: start,
				Route:      r.URL.Path,
				Model:      model,
				Status:     cw.status(),
				DurationMS: time.Since(start).Milliseconds(),
				Streamed:   cw.isSSE(),
			}
			if id, ok := auth.IdentityFrom(r.Context()); ok {
				e.Subject = id.Subject
			}
			if !cw.isSSE() {
				e.PromptTokens, e.CompletionTokens = parseTokens(cw.body.Bytes())
			}
			if err := rec.Record(r.Context(), e); err != nil {
				log.Printf("usage: recording event: %v", err)
			}
		})
	}
}

// peekModel reads the request body to extract the model field, then
// restores it for the upstream handler.
func peekModel(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return ""
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var req struct {
		Model string `json:"model"`
	}
	json.Unmarshal(body, &req)
	return req.Model
}

func parseTokens(body []byte) (prompt, completion *int) {
	var res struct {
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &res); err != nil || res.Usage == nil {
		return nil, nil
	}
	return &res.Usage.PromptTokens, &res.Usage.CompletionTokens
}

// captureWriter records status and, for non-SSE responses, a bounded copy
// of the body. SSE bytes pass straight through with working flushes.
type captureWriter struct {
	http.ResponseWriter
	code int
	sse  bool
	body bytes.Buffer
}

func (c *captureWriter) WriteHeader(code int) {
	c.code = code
	c.sse = strings.HasPrefix(c.Header().Get("Content-Type"), "text/event-stream")
	c.ResponseWriter.WriteHeader(code)
}

func (c *captureWriter) Write(b []byte) (int, error) {
	if c.code == 0 {
		c.WriteHeader(http.StatusOK)
	}
	if !c.sse && c.body.Len() < maxCapturedBody {
		c.body.Write(b)
	}
	return c.ResponseWriter.Write(b)
}

func (c *captureWriter) Flush() {
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *captureWriter) status() int {
	if c.code == 0 {
		return http.StatusOK
	}
	return c.code
}

func (c *captureWriter) isSSE() bool { return c.sse }
