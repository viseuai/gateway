package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"

	"github.com/viseuai/gateway/internal/apikey"
	"github.com/viseuai/gateway/internal/auth"
	"github.com/viseuai/gateway/internal/db"
	"github.com/viseuai/gateway/internal/quota"
	"github.com/viseuai/gateway/internal/server"
	"github.com/viseuai/gateway/internal/upstream"
	"github.com/viseuai/gateway/internal/usage"
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

	cfg := server.Config{Upstream: upstream.Handler(upstreamURL)}
	cfg.Verifier = verifier

	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		pool, err := db.Connect(context.Background(), dsn)
		if err != nil {
			log.Fatalf("database: %v", err)
		}
		defer pool.Close()
		store := apikey.NewPG(pool)
		cfg.Usage = usage.NewPG(pool)
		cfg.Keys = apikey.NewManager(store)
		cfg.Verifier = auth.PrefixVerifier(apikey.Prefix, apikey.NewVerifier(store), verifier)

		limits := quota.Limits{
			DailyRequests: envInt("QUOTA_DAILY_REQUESTS", 500),
			MonthlyTokens: int64(envInt("QUOTA_MONTHLY_TOKENS", 2_000_000)),
		}
		cfg.Quota = quota.New(quota.NewPG(pool), limits).Middleware
		log.Printf("postgres: usage accounting + api keys + quotas enabled (%d req/day, %d tokens/month)",
			limits.DailyRequests, limits.MonthlyTokens)
	} else {
		log.Print("postgres: DISABLED (no DATABASE_URL) — oidc only, no accounting")
	}

	addr := ":" + envOr("PORT", "8080")
	log.Printf("gateway listening on %s (issuer: %s, upstream: %s)", addr, issuer, upstreamURL)
	srv := server.New(cfg)
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

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Fatalf("%s must be an integer, got %q", key, v)
	}
	return n
}
