// Package route dispatches OpenAI-compatible requests to the backend
// serving the requested model. Backends come from ordered Sources: the
// static configuration map and the live node registry.
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
	sources []Source
}

// New builds a Router over a fixed model→handler map.
func New(backends map[string]http.Handler) *Router {
	return NewMulti(StaticSource(backends))
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

	for _, src := range rt.sources {
		if backend, ok := src.Resolve(r.Context(), req.Model); ok {
			backend.ServeHTTP(w, r)
			return
		}
	}
	writeError(w, http.StatusNotFound,
		fmt.Sprintf("The model %q does not exist on this platform. List available models at /v1/models.", req.Model),
		"invalid_request_error")
}

// Models serves the OpenAI-compatible model list: the deduplicated union
// of all sources, computed per request so registry changes show live.
func (rt *Router) Models() http.Handler {
	type model struct {
		ID     string `json:"id"`
		Object string `json:"object"`
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen := map[string]bool{}
		var ids []string
		for _, src := range rt.sources {
			for _, id := range src.Models(r.Context()) {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
		sort.Strings(ids)

		data := make([]model, len(ids))
		for i, id := range ids {
			data[i] = model{ID: id, Object: "model"}
		}
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
