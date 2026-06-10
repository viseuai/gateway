package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/viseuai/gateway/internal/auth"
	"github.com/viseuai/gateway/internal/server"
)

func main() {
	issuer := envOr("OIDC_ISSUER", "https://auth.viseuai.org/realms/viseu")
	verifier, err := auth.NewOIDC(context.Background(), issuer)
	if err != nil {
		log.Fatalf("oidc setup: %v", err)
	}

	addr := ":" + envOr("PORT", "8080")
	log.Printf("gateway listening on %s (issuer: %s)", addr, issuer)
	if err := http.ListenAndServe(addr, server.New(server.Config{Verifier: verifier})); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
