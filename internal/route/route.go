// Package route dispatches OpenAI-compatible requests to the backend
// serving the requested model. v1 is a static map from configuration;
// dynamic registration via the node agent replaces the map later without
// changing the routing contract.
package route

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
)

// Router picks a backend by the request's model field.
type Router struct {
	backends map[string]http.Handler
}

func New(backends map[string]http.Handler) *Router {
	return &Router{backends: backends}
}

// ServeHTTP handles completion-style requests (model in the JSON body).
func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Could not read the request body.", "invalid_request_error")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var req struct {
		Model string `json:"model"`
	}
	json.Unmarshal(body, &req)
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "The model field is required.", "invalid_request_error")
		return
	}

	backend, ok := rt.backends[req.Model]
	if !ok {
		writeError(w, http.StatusNotFound,
			fmt.Sprintf("The model %q does not exist on this platform. List available models at /v1/models.", req.Model),
			"invalid_request_error")
		return
	}
	backend.ServeHTTP(w, r)
}

// Models serves the OpenAI-compatible model list for the configured map.
func (rt *Router) Models() http.Handler {
	type model struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	}
	ids := make([]string, 0, len(rt.backends))
	for id := range rt.backends {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	data := make([]model, len(ids))
	for i, id := range ids {
		data[i] = model{ID: id, Object: "model"}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
	})
}

func writeError(w http.ResponseWriter, status int, msg, typ string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": msg, "type": typ},
	})
}
