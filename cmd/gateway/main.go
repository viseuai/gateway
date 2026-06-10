package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/viseuai/gateway/internal/auth"
	"github.com/viseuai/gateway/internal/server"
	"github.com/viseuai/gateway/internal/upstream"
)

func main() {
	issuer := envOr("OIDC_ISSUER", "https://auth.viseuai.org/realms/viseu")
	verifier, err := auth.NewOIDC(context.Background(), issuer)
	if err != nil {
		log.Fatalf("oidc setup: %v", err)
	}

	upstreamURL, err := url.Parse(envOr("UPSTREAM_URL", "http://llamacpp:8081"))
	if err != nil {
		log.Fatalf("parsing UPSTREAM_URL: %v", err)
	}

	addr := ":" + envOr("PORT", "8080")
	log.Printf("gateway listening on %s (issuer: %s, upstream: %s)", addr, issuer, upstreamURL)
	srv := server.New(server.Config{
		Verifier: verifier,
		Upstream: upstream.Handler(upstreamURL),
	})
	if err := http.ListenAndServe(addr, srv); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
