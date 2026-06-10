package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
)

// OIDCVerifier validates tokens against an OIDC issuer's JWKS (Keycloak).
type OIDCVerifier struct {
	verifier *oidc.IDTokenVerifier
}

// NewOIDC discovers the issuer and prepares signature verification.
//
// Audience checking is skipped for now: Keycloak access tokens carry
// aud=account by default. Hardening via an audience protocol mapper is a
// tracked follow-up before public exposure.
func NewOIDC(ctx context.Context, issuer string) (*OIDCVerifier, error) {
	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discovering OIDC issuer %s: %w", issuer, err)
	}
	return &OIDCVerifier{
		verifier: provider.Verifier(&oidc.Config{SkipClientIDCheck: true}),
	}, nil
}

func (v *OIDCVerifier) Verify(ctx context.Context, rawToken string) (*Identity, error) {
	tok, err := v.verifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, err
	}
	var claims struct {
		Email       string `json:"email"`
		RealmAccess struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
	}
	if err := tok.Claims(&claims); err != nil {
		return nil, fmt.Errorf("parsing claims: %w", err)
	}
	return &Identity{
		Subject: tok.Subject,
		Email:   claims.Email,
		Roles:   claims.RealmAccess.Roles,
	}, nil
}
