package auth_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"

	"github.com/alephc0rporation/Cardinal/api/internal/auth"
)

const issuer = "http://localhost:8081/realms/aleph0"

func sign(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := jws.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func validClaims() map[string]any {
	return map[string]any{
		"iss":   issuer,
		"aud":   []string{"api", "account"},
		"sub":   "user-1",
		"email": "dev@aleph0.local",
		"name":  "Dev User",
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
}

func with(c map[string]any, k string, v any) map[string]any {
	c[k] = v
	return c
}

func TestMiddleware(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	v := auth.NewVerifier(issuer, "api", &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}})

	var seen auth.Claims
	h := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = auth.FromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"valid token", "Bearer " + sign(t, key, validClaims()), http.StatusNoContent},
		{"lowercase scheme", "bearer " + sign(t, key, validClaims()), http.StatusNoContent},
		{"no header", "", http.StatusUnauthorized},
		{"basic scheme", "Basic ZGV2OmRldg==", http.StatusUnauthorized},
		{"empty token", "Bearer ", http.StatusUnauthorized},
		{"garbage token", "Bearer not-a-jwt", http.StatusUnauthorized},
		{"wrong audience", "Bearer " + sign(t, key, with(validClaims(), "aud", "web")), http.StatusUnauthorized},
		{"wrong issuer", "Bearer " + sign(t, key, with(validClaims(), "iss", "http://evil.example/realms/aleph0")), http.StatusUnauthorized},
		{"expired", "Bearer " + sign(t, key, with(validClaims(), "exp", time.Now().Add(-time.Minute).Unix())), http.StatusUnauthorized},
		{"wrong signing key", "Bearer " + sign(t, otherKey, validClaims()), http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen = auth.Claims{}
			req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
			if tc.want == http.StatusUnauthorized {
				if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
					t.Errorf("WWW-Authenticate = %q, want Bearer", got)
				}
				if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
					t.Errorf("Content-Type = %q", got)
				}
				return
			}
			want := auth.Claims{Subject: "user-1", Email: "dev@aleph0.local", Name: "Dev User"}
			if seen != want {
				t.Errorf("claims = %+v, want %+v", seen, want)
			}
		})
	}
}

func TestFromContextEmpty(t *testing.T) {
	if _, ok := auth.FromContext(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Fatal("want ok = false without claims")
	}
}
