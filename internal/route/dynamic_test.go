package route

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSource is a Source backed by a plain map.
type fakeSource struct {
	models map[string]string // model → marker echoed by its handler
}

func (f *fakeSource) Resolve(_ context.Context, model string) (http.Handler, bool) {
	marker, ok := f.models[model]
	if !ok {
		return nil, false
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, marker)
	}), true
}

func (f *fakeSource) Models(_ context.Context) []string {
	var ids []string
	for id := range f.models {
		ids = append(ids, id)
	}
	return ids
}

func TestMultiSourceResolvesInOrder(t *testing.T) {
	static := &fakeSource{models: map[string]string{"m-static": "static"}}
	dynamic := &fakeSource{models: map[string]string{"m-dyn": "dynamic", "m-static": "shadowed"}}

	r := NewMulti(static, dynamic)

	rec := post(r, `{"model":"m-dyn"}`)
	if rec.Body.String() != "dynamic" {
		t.Errorf("dynamic model: got %q", rec.Body)
	}

	// earlier sources win on conflicts
	rec = post(r, `{"model":"m-static"}`)
	if rec.Body.String() != "static" {
		t.Errorf("conflicting model: got %q, want static source to win", rec.Body)
	}

	rec = post(r, `{"model":"nope"}`)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown model: got %d, want 404", rec.Code)
	}
}

func TestMultiSourceModelsUnion(t *testing.T) {
	r := NewMulti(
		&fakeSource{models: map[string]string{"a": "x", "b": "x"}},
		&fakeSource{models: map[string]string{"b": "y", "c": "y"}},
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	r.Models().ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, id := range []string{`"a"`, `"b"`, `"c"`} {
		if !strings.Contains(body, id) {
			t.Errorf("models list missing %s: %s", id, body)
		}
	}
	if strings.Count(body, `"b"`) != 1 {
		t.Errorf("duplicate model id in list: %s", body)
	}
}
