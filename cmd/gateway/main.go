package main

import (
	"log"
	"net/http"
	"os"

	"github.com/viseuai/gateway/internal/server"
)

func main() {
	addr := ":" + envOr("PORT", "8080")
	log.Printf("gateway listening on %s", addr)
	if err := http.ListenAndServe(addr, server.New()); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
