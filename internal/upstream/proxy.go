// Package upstream proxies OpenAI-compatible requests to an inference
// backend (vLLM, llama.cpp, MLX, ... — anything serving the OpenAI API).
package upstream

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
)

// Handler returns a reverse proxy to the backend at target. Paths are
// preserved (/v1/... maps to /v1/... upstream). FlushInterval -1 streams
// SSE token-by-token instead of buffering.
func Handler(target *url.URL) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()
		},
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("upstream %s %s: %v", r.Method, r.URL.Path, err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{
					"message": "The inference backend is unavailable. Try again shortly.",
					"type":    "api_error",
				},
			})
		},
	}
	return proxy
}
