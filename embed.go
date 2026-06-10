// Package gateway exposes repo-level embedded assets.
package gateway

import "embed"

// Migrations holds the SQL schema migrations applied at startup.
// The digit-anchored pattern keeps editor/OS junk files (.#foo, ._foo)
// out of the embedded FS — goose requires numeric version prefixes.
//
//go:embed migrations/[0-9]*.sql
var Migrations embed.FS
