// Package auth verifies Keycloak access tokens and carries the caller's identity in the request context.
package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/alephc0rporation/Cardinal/api/internal/problem"
)

// Claims is the part of the access token the API uses.
type Claims struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
}

type Verifier struct {
	v *oidc.IDTokenVerifier
}

// NewVerifier checks signature (RS256, keys from keySet), iss, exp, and that aud contains audience.
// ID tokens are rejected too: Keycloak issues them with aud = the client ID, never "api".
func NewVerifier(issuer, audience string, keySet oidc.KeySet) *Verifier {
	return &Verifier{oidc.NewVerifier(issuer, keySet, &oidc.Config{
		ClientID:             audience,
		SupportedSigningAlgs: []string{oidc.RS256},
	})}
}

// NewKeycloakVerifier fetches Keycloak's signing keys on first use and again when a token
// carries an unknown key ID, so the API can start while Keycloak is down.
func NewKeycloakVerifier(issuer, audience string) *Verifier {
	ctx := oidc.ClientContext(context.Background(), &http.Client{Timeout: 10 * time.Second})
	return NewVerifier(issuer, audience, oidc.NewRemoteKeySet(ctx, issuer+"/protocol/openid-connect/certs"))
}

func (v *Verifier) Verify(ctx context.Context, rawToken string) (Claims, error) {
	tok, err := v.v.Verify(ctx, rawToken)
	if err != nil {
		return Claims{}, err
	}
	var c Claims
	if err := tok.Claims(&c); err != nil {
		return Claims{}, err
	}
	return c, nil
}

// Middleware answers 401 unless the request carries a valid bearer token, and stores its claims in the context.
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			unauthorized(w, "missing bearer token")
			return
		}
		c, err := v.Verify(r.Context(), raw)
		if err != nil {
			slog.InfoContext(r.Context(), "access token rejected", "error", err)
			unauthorized(w, "invalid access token")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), c)))
	})
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

func unauthorized(w http.ResponseWriter, detail string) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	problem.Write(w, http.StatusUnauthorized, detail)
}

type ctxKey struct{}

func WithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

func FromContext(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(Claims)
	return c, ok
}
