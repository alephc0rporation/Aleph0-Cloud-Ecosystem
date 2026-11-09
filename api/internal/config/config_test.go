package config_test

import (
	"strings"
	"testing"

	"github.com/alephc0rporation/Cardinal/api/internal/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad(t *testing.T) {
	full := map[string]string{
		"DATABASE_URL":  "postgres://api:api@localhost:5432/api",
		"OIDC_ISSUER":   "http://localhost:8081/realms/aleph0",
		"OIDC_AUDIENCE": "api",
	}

	t.Run("defaults HTTP_ADDR", func(t *testing.T) {
		c, err := config.Load(env(full))
		if err != nil {
			t.Fatal(err)
		}
		want := config.Config{
			HTTPAddr:     ":8080",
			DatabaseURL:  full["DATABASE_URL"],
			OIDCIssuer:   full["OIDC_ISSUER"],
			OIDCAudience: "api",
		}
		if c != want {
			t.Fatalf("got %+v, want %+v", c, want)
		}
	})

	t.Run("HTTP_ADDR override", func(t *testing.T) {
		m := map[string]string{"HTTP_ADDR": ":9999"}
		for k, v := range full {
			m[k] = v
		}
		c, err := config.Load(env(m))
		if err != nil {
			t.Fatal(err)
		}
		if c.HTTPAddr != ":9999" {
			t.Fatalf("HTTPAddr = %q, want :9999", c.HTTPAddr)
		}
	})

	t.Run("lists every missing variable", func(t *testing.T) {
		_, err := config.Load(env(map[string]string{"OIDC_AUDIENCE": "api"}))
		if err == nil {
			t.Fatal("want error")
		}
		for _, name := range []string{"DATABASE_URL", "OIDC_ISSUER"} {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error %q does not mention %s", err, name)
			}
		}
		if strings.Contains(err.Error(), "OIDC_AUDIENCE") {
			t.Errorf("error %q mentions OIDC_AUDIENCE, which is set", err)
		}
	})
}
