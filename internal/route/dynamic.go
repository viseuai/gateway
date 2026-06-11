package route

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/viseuai/gateway/internal/upstream"
)

// Source provides backends for models. Sources are consulted in order;
// the first to resolve a model wins.
type Source interface {
	Resolve(ctx context.Context, model string) (http.Handler, bool)
	Models(ctx context.Context) []string
}

// NewMulti builds a Router over ordered sources.
func NewMulti(sources ...Source) *Router {
	return &Router{sources: sources}
}

// StaticSource adapts a fixed model→handler map.
type StaticSource map[string]http.Handler

func (s StaticSource) Resolve(_ context.Context, model string) (http.Handler, bool) {
	h, ok := s[model]
	return h, ok
}

func (s StaticSource) Models(_ context.Context) []string {
	ids := make([]string, 0, len(s))
	for id := range s {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// RegistryStore is what the dynamic source needs from the node registry.
type RegistryStore interface {
	Lookup(ctx context.Context, model string, ttl time.Duration) (string, bool, error)
	Models(ctx context.Context, ttl time.Duration) ([]string, error)
}

// RegistrySource resolves models from live node heartbeats.
type RegistrySource struct {
	Store RegistryStore
	TTL   time.Duration
}

func (r *RegistrySource) Resolve(ctx context.Context, model string) (http.Handler, bool) {
	raw, ok, err := r.Store.Lookup(ctx, model, r.TTL)
	if err != nil {
		log.Printf("route: registry lookup %s: %v", model, err)
		return nil, false
	}
	if !ok {
		return nil, false
	}
	u, err := url.Parse(raw)
	if err != nil {
		log.Printf("route: bad registered url for %s: %v", model, err)
		return nil, false
	}
	return upstream.Handler(u), true
}

func (r *RegistrySource) Models(ctx context.Context) []string {
	ids, err := r.Store.Models(ctx, r.TTL)
	if err != nil {
		log.Printf("route: registry models: %v", err)
		return nil
	}
	return ids
}
