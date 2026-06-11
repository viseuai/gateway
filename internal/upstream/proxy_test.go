package upstream

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// flushRecorder counts flushes so streaming behavior is observable.
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func (f *flushRecorder) Flush() { f.flushes++ }

func target(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestForwardsJSONResponse(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("upstream path: got %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"hello"`) {
			t.Errorf("upstream did not receive request body: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"cmpl-1","choices":[{"message":{"content":"olá"}}]}`)
	}))
	defer up.Close()

	h := Handler(target(t, up.URL))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"model":"test","messages":[{"role":"user","content":"hello"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "olá") {
		t.Errorf("body not forwarded: %s", rec.Body)
	}
}

func TestStreamsSSEWithFlushes(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, "data: {\"chunk\":%d}\n\n", i)
			fl.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		fl.Flush()
	}))
	defer up.Close()

	h := Handler(target(t, up.URL))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	rec := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type: got %q, want text/event-stream", ct)
	}
	if !strings.Contains(rec.Body.String(), "data: [DONE]") {
		t.Errorf("stream incomplete: %s", rec.Body)
	}
	if rec.flushes < 2 {
		t.Errorf("expected incremental flushes, got %d", rec.flushes)
	}
}

func TestStripsUpstreamCORSHeaders(t *testing.T) {
	// engines (llama.cpp, mlx_lm.server) emit Access-Control-Allow-Origin: *;
	// passing it through duplicates the gateway's own CORS header and
	// browsers reject the response. The gateway is the only CORS authority.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "X-Whatever")
		fmt.Fprint(w, `{}`)
	}))
	defer up.Close()

	h := Handler(target(t, up.URL))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	for _, header := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Headers"} {
		if got := rec.Header().Get(header); got != "" {
			t.Errorf("upstream %s leaked through the proxy: %q", header, got)
		}
	}
}

func TestStripsSensitiveAndBrowserHeadersOutbound(t *testing.T) {
	// Authorization/Cookie must never reach third-party nodes (credential
	// leak); Origin/Referer trip engine origin checks (Ollama 403s).
	var got http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		fmt.Fprint(w, `{}`)
	}))
	defer up.Close()

	h := Handler(target(t, up.URL))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer member-secret")
	req.Header.Set("Cookie", "session=abc")
	req.Header.Set("Origin", "https://chat.viseuai.org")
	req.Header.Set("Referer", "https://chat.viseuai.org/")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	for _, header := range []string{"Authorization", "Cookie", "Origin", "Referer"} {
		if v := got.Get(header); v != "" {
			t.Errorf("%s leaked to upstream: %q", header, v)
		}
	}
}

func TestUpstreamDownIs502(t *testing.T) {
	dead := httptest.NewServer(nil)
	dead.Close() // guaranteed-refused port

	h := Handler(target(t, dead.URL))
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status: got %d, want 502", rec.Code)
	}
	var body struct {
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("502 body is not JSON: %v (%s)", err, rec.Body)
	}
	if body.Error.Type != "api_error" {
		t.Errorf("error.type: got %q, want api_error", body.Error.Type)
	}
}
