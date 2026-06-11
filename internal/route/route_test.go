package route

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func namedBackend(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "%s:%s", name, body)
	})
}

func testRouter() *Router {
	return New(map[string]http.Handler{
		"qwen2.5-0.5b-instruct": namedBackend("zero"),
		"qwen2.5-3b-mlx":        namedBackend("mac"),
	})
}

func post(h http.Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRoutesByModel(t *testing.T) {
	r := testRouter()

	rec := post(r, `{"model":"qwen2.5-3b-mlx","messages":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Body.String(), "mac:") {
		t.Errorf("routed to wrong backend: %s", rec.Body)
	}

	rec = post(r, `{"model":"qwen2.5-0.5b-instruct","messages":[]}`)
	if !strings.HasPrefix(rec.Body.String(), "zero:") {
		t.Errorf("routed to wrong backend: %s", rec.Body)
	}
}

func TestBackendReceivesFullBody(t *testing.T) {
	rec := post(testRouter(), `{"model":"qwen2.5-3b-mlx","messages":[{"role":"user","content":"olá"}]}`)
	if !strings.Contains(rec.Body.String(), `"olá"`) {
		t.Errorf("body not preserved for backend: %s", rec.Body)
	}
}

func TestUnknownModelIs404(t *testing.T) {
	rec := post(testRouter(), `{"model":"gpt-99"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Type != "invalid_request_error" {
		t.Errorf("error.type: got %q", body.Error.Type)
	}
	if !strings.Contains(body.Error.Message, "gpt-99") {
		t.Errorf("message should name the unknown model: %q", body.Error.Message)
	}
}

func TestMissingModelIs400(t *testing.T) {
	rec := post(testRouter(), `{"messages":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}

func TestModelsListsConfiguredModels(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	testRouter().Models().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var body struct {
		Object string `json:"object"`
		Data   []struct {
			ID     string `json:"id"`
			Object string `json:"object"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Object != "list" || len(body.Data) != 2 {
		t.Fatalf("models list: %+v", body)
	}
	ids := map[string]bool{}
	for _, m := range body.Data {
		ids[m.ID] = true
		if m.Object != "model" {
			t.Errorf("object: got %q", m.Object)
		}
	}
	if !ids["qwen2.5-0.5b-instruct"] || !ids["qwen2.5-3b-mlx"] {
		t.Errorf("ids: %v", ids)
	}
}
