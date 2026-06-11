// Package upstream proxies OpenAI-compatible requests to an inference
// backend (vLLM, llama.cpp, MLX, ... — anything serving the OpenAI API).
package upstream

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
)

// Handler returns a reverse proxy to the backend at target. Paths are
// preserved (/v1/... maps to /v1/... upstream). FlushInterval -1 streams
// SSE token-by-token instead of buffering.
func Handler(target *url.URL) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.SetXForwarded()
			// Never leak caller credentials to (possibly volunteer-run)
			// backends, and drop browser headers that trip engine origin
			// checks (Ollama rejects foreign Origins with 403).
			for _, h := range []string{"Authorization", "Cookie", "Origin", "Referer"} {
				pr.Out.Header.Del(h)
			}
		},
		// The gateway is the only CORS authority: engines (llama.cpp, MLX)
		// emit their own permissive CORS headers, and forwarding them
		// duplicates ours, which browsers reject.
		ModifyResponse: func(res *http.Response) error {
			for header := range res.Header {
				if strings.HasPrefix(header, "Access-Control-") {
					res.Header.Del(header)
				}
			}
			return nil
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
