// Package config reads the API configuration from environment variables.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Region is one cluster: the ID and name the API shows, and what OpenStack calls it.
type Region struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	OpenStackRegion string `json:"openstack_region"`
	ExternalNetwork string `json:"external_network"`
}

type Config struct {
	HTTPAddr     string
	DatabaseURL  string
	OIDCIssuer   string
	OIDCAudience string

	OSAuthURL             string
	OSAPIUsername         string
	OSAPIPassword         string
	OSProvisionerUsername string
	OSProvisionerPassword string
	Regions               []Region

	LimitTenantsPerUser    int
	LimitProjectsPerTenant int
}

// Load reads the configuration through getenv (os.Getenv outside tests)
// and reports every missing required variable at once.
func Load(getenv func(string) string) (Config, error) {
	c := Config{
		HTTPAddr:              getenv("HTTP_ADDR"),
		DatabaseURL:           getenv("DATABASE_URL"),
		OIDCIssuer:            getenv("OIDC_ISSUER"),
		OIDCAudience:          getenv("OIDC_AUDIENCE"),
		OSAuthURL:             getenv("OS_AUTH_URL"),
		OSAPIUsername:         getenv("OS_API_USERNAME"),
		OSAPIPassword:         getenv("OS_API_PASSWORD"),
		OSProvisionerUsername: getenv("OS_PROVISIONER_USERNAME"),
		OSProvisionerPassword: getenv("OS_PROVISIONER_PASSWORD"),
	}
	if c.HTTPAddr == "" {
		c.HTTPAddr = ":8080"
	}

	required := []struct{ name, value string }{
		{"DATABASE_URL", c.DatabaseURL},
		{"OIDC_ISSUER", c.OIDCIssuer},
		{"OIDC_AUDIENCE", c.OIDCAudience},
		{"OS_AUTH_URL", c.OSAuthURL},
		{"OS_API_USERNAME", c.OSAPIUsername},
		{"OS_API_PASSWORD", c.OSAPIPassword},
		{"OS_PROVISIONER_USERNAME", c.OSProvisionerUsername},
		{"OS_PROVISIONER_PASSWORD", c.OSProvisionerPassword},
		{"REGIONS", getenv("REGIONS")},
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

	var err error
	if c.Regions, err = parseRegions(getenv("REGIONS")); err != nil {
		return Config{}, err
	}
	if c.LimitTenantsPerUser, err = positiveInt(getenv, "LIMIT_TENANTS_PER_USER", 3); err != nil {
		return Config{}, err
	}
	if c.LimitProjectsPerTenant, err = positiveInt(getenv, "LIMIT_PROJECTS_PER_TENANT", 10); err != nil {
		return Config{}, err
	}
	return c, nil
}

func parseRegions(s string) ([]Region, error) {
	var rs []Region
	if err := json.Unmarshal([]byte(s), &rs); err != nil {
		return nil, fmt.Errorf("REGIONS is not a JSON list of regions: %w", err)
	}
	if len(rs) == 0 {
		return nil, errors.New("REGIONS lists no region")
	}
	seen := map[string]bool{}
	for i, r := range rs {
		if r.ID == "" || r.Name == "" || r.OpenStackRegion == "" || r.ExternalNetwork == "" {
			return nil, fmt.Errorf("REGIONS[%d] needs id, name, openstack_region and external_network", i)
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("REGIONS lists %q twice", r.ID)
		}
		seen[r.ID] = true
	}
	return rs, nil
}

func positiveInt(getenv func(string) string, name string, def int) (int, error) {
	v := getenv(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s must be a positive integer, got %q", name, v)
	}
	return n, nil
}
