// Package gateway exposes repo-level embedded assets.
package gateway

import "embed"

// Migrations holds the SQL schema migrations applied at startup.
//
//go:embed migrations/*.sql
var Migrations embed.FS
