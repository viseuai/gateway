package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/viseuai/gateway/internal/apikey"
	"github.com/viseuai/gateway/internal/auth"
	"github.com/viseuai/gateway/internal/db"
	"github.com/viseuai/gateway/internal/kcadmin"
	"github.com/viseuai/gateway/internal/meshkey"
	"github.com/viseuai/gateway/internal/quota"
	"github.com/viseuai/gateway/internal/registry"
	"github.com/viseuai/gateway/internal/route"
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

	cfg := server.Config{}
	cfg.Verifier = verifier

	// Static backends: BACKENDS="model-id=http://host:port,...".
	static := route.StaticSource{}
	for _, pair := range strings.Split(os.Getenv("BACKENDS"), ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		model, rawURL, ok := strings.Cut(pair, "=")
		if !ok {
			log.Fatalf("BACKENDS entry %q: want model=url", pair)
		}
		u, err := url.Parse(rawURL)
		if err != nil {
			log.Fatalf("BACKENDS url for %s: %v", model, err)
		}
		static[model] = upstream.Handler(u)
		log.Printf("static backend: %s → %s", model, rawURL)
	}
	sources := []route.Source{static}

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

		reg := registry.NewPG(pool)
		cfg.Registry = reg
		ttl := time.Duration(envInt("REGISTRY_TTL_SECONDS", 60)) * time.Second
		cfg.RegistryTTL = ttl
		sources = append(sources, &route.RegistrySource{Store: reg, TTL: ttl})

		log.Printf("postgres: accounting + api keys + quotas (%d req/day, %d tok/mo) + node registry (ttl %s)",
			limits.DailyRequests, limits.MonthlyTokens, ttl)
	} else {
		log.Print("postgres: DISABLED (no DATABASE_URL) — oidc only, no accounting")
	}

	router := route.NewMulti(sources...)
	cfg.Upstream = router
	cfg.Models = router.Models()

	// Admin surface (direção self-service) needs both upstream credentials.
	if kcSecret := os.Getenv("KC_ADMIN_CLIENT_SECRET"); kcSecret != "" && os.Getenv("HEADSCALE_API_KEY") != "" {
		cfg.Admin = &server.AdminConfig{
			Directory: kcadmin.New(
				envOr("KC_ADMIN_BASE", "http://keycloak:8080"),
				envOr("KC_REALM", "viseu"),
				envOr("KC_ADMIN_CLIENT_ID", "gateway-admin"),
				kcSecret,
			),
			MeshKeys: meshkey.New(
				envOr("HEADSCALE_BASE", "http://headscale:8080"),
				os.Getenv("HEADSCALE_API_KEY"),
				envOr("HEADSCALE_USER", "1"),
			),
		}
		log.Print("admin surface: enabled")
	} else {
		log.Print("admin surface: DISABLED (missing KC_ADMIN_CLIENT_SECRET / HEADSCALE_API_KEY)")
	}

	if origins := os.Getenv("CORS_ORIGINS"); origins != "" {
		for _, o := range strings.Split(origins, ",") {
			cfg.CORSOrigins = append(cfg.CORSOrigins, strings.TrimSpace(o))
		}
		log.Printf("cors origins: %v", cfg.CORSOrigins)
	}

	addr := ":" + envOr("PORT", "8080")
	log.Printf("gateway listening on %s (issuer: %s)", addr, issuer)
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
