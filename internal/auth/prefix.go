package auth

import (
	"context"
	"strings"
)

// PrefixVerifier routes tokens starting with prefix to primary and
// everything else to fallback. Used to multiplex API keys and OIDC JWTs
// through one Authorization header.
func PrefixVerifier(prefix string, primary, fallback Verifier) Verifier {
	return prefixVerifier{prefix: prefix, primary: primary, fallback: fallback}
}

type prefixVerifier struct {
	prefix            string
	primary, fallback Verifier
}

func (p prefixVerifier) Verify(ctx context.Context, rawToken string) (*Identity, error) {
	if strings.HasPrefix(rawToken, p.prefix) {
		return p.primary.Verify(ctx, rawToken)
	}
	return p.fallback.Verify(ctx, rawToken)
}
