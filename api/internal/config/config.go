// Package config reads the API configuration from environment variables.
package config

import (
	"fmt"
	"strings"
)

type Config struct {
	HTTPAddr     string
	DatabaseURL  string
	OIDCIssuer   string
	OIDCAudience string
}

// Load reads the configuration through getenv (os.Getenv outside tests)
// and reports every missing required variable at once.
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		HTTPAddr:     getenv("HTTP_ADDR"),
		DatabaseURL:  getenv("DATABASE_URL"),
		OIDCIssuer:   getenv("OIDC_ISSUER"),
		OIDCAudience: getenv("OIDC_AUDIENCE"),
	}
	if c.HTTPAddr == "" {
		c.HTTPAddr = ":8080"
	}

	required := []struct{ name, value string }{
		{"DATABASE_URL", c.DatabaseURL},
		{"OIDC_ISSUER", c.OIDCIssuer},
		{"OIDC_AUDIENCE", c.OIDCAudience},
	}
	var missing []string
	for _, r := range required {
		if r.value == "" {
			missing = append(missing, r.name)
		}
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	return c, nil
}
