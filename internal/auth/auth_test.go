package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fakeIssuer serves a minimal OIDC discovery document and JWKS for a
// generated RSA key, so verification tests need no live Keycloak.
type fakeIssuer struct {
	srv *httptest.Server
	key *rsa.PrivateKey
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIssuer{key: key}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                f.srv.URL,
			"jwks_uri":                              f.srv.URL + "/jwks",
			"authorization_endpoint":                f.srv.URL + "/authorize",
			"token_endpoint":                        f.srv.URL + "/token",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		pub := f.key.Public().(*rsa.PublicKey)
		json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "test-key",
				"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIssuer) token(t *testing.T, mutate func(jwt.MapClaims)) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss":   f.srv.URL,
		"sub":   "user-123",
		"email": "membro@viseuai.org",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
		"realm_access": map[string]any{
			"roles": []string{"member"},
		},
	}
	if mutate != nil {
		mutate(claims)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "test-key"
	signed, err := tok.SignedString(f.key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func newVerifier(t *testing.T, f *fakeIssuer) Verifier {
	t.Helper()
	v, err := NewOIDC(context.Background(), f.srv.URL)
	if err != nil {
		t.Fatalf("NewOIDC: %v", err)
	}
	return v
}

func protectedEcho(t *testing.T, v Verifier) http.Handler {
	t.Helper()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := IdentityFrom(r.Context())
		if !ok {
			t.Fatal("identity missing from context inside protected handler")
		}
		fmt.Fprintf(w, "%s|%v", id.Subject, id.Roles)
	})
	return Middleware(v)(inner)
}

func do(h http.Handler, authz string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func assertAuthError(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want 401", rec.Code)
	}
	var body struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("401 body is not JSON: %v", err)
	}
	if body.Error.Type != "authentication_error" {
		t.Errorf("error.type: got %q, want authentication_error", body.Error.Type)
	}
}

func TestMissingTokenRejected(t *testing.T) {
	h := protectedEcho(t, newVerifier(t, newFakeIssuer(t)))
	assertAuthError(t, do(h, ""))
}

func TestMalformedHeaderRejected(t *testing.T) {
	h := protectedEcho(t, newVerifier(t, newFakeIssuer(t)))
	assertAuthError(t, do(h, "Basic dXNlcjpwYXNz"))
}

func TestGarbageTokenRejected(t *testing.T) {
	h := protectedEcho(t, newVerifier(t, newFakeIssuer(t)))
	assertAuthError(t, do(h, "Bearer not-a-jwt"))
}

func TestExpiredTokenRejected(t *testing.T) {
	f := newFakeIssuer(t)
	h := protectedEcho(t, newVerifier(t, f))
	expired := f.token(t, func(c jwt.MapClaims) {
		c["exp"] = time.Now().Add(-time.Hour).Unix()
	})
	assertAuthError(t, do(h, "Bearer "+expired))
}

func TestValidTokenPassesWithIdentity(t *testing.T) {
	f := newFakeIssuer(t)
	h := protectedEcho(t, newVerifier(t, f))

	rec := do(h, "Bearer "+f.token(t, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body: %s)", rec.Code, rec.Body)
	}
	if got, want := rec.Body.String(), "user-123|[member]"; got != want {
		t.Errorf("identity echo: got %q, want %q", got, want)
	}
}
