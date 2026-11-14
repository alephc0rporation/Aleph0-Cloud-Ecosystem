package config_test

import (
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/alephc0rporation/Cardinal/api/internal/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

const region = `{"id":"ro-1","name":"Romania 1","openstack_region":"RegionOne","external_network":"public"}`

var full = map[string]string{
	"DATABASE_URL":            "postgres://api:api@localhost:5432/api",
	"OIDC_ISSUER":             "http://localhost:8081/realms/aleph0",
	"OIDC_AUDIENCE":           "api",
	"OS_AUTH_URL":             "http://10.10.0.10:5000/v3",
	"OS_API_USERNAME":         "aleph0-api",
	"OS_API_PASSWORD":         "secret1",
	"OS_PROVISIONER_USERNAME": "aleph0-provisioner",
	"OS_PROVISIONER_PASSWORD": "secret2",
	"REGIONS":                 "[" + region + "]",
}

// with returns full with some variables replaced; full itself stays unchanged.
func with(overrides map[string]string) map[string]string {
	m := maps.Clone(full)
	maps.Copy(m, overrides)
	return m
}

func TestLoad(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		c, err := config.Load(env(full))
		if err != nil {
			t.Fatal(err)
		}
		want := config.Config{
			HTTPAddr:               ":8080",
			DatabaseURL:            full["DATABASE_URL"],
			OIDCIssuer:             full["OIDC_ISSUER"],
			OIDCAudience:           "api",
			OSAuthURL:              full["OS_AUTH_URL"],
			OSAPIUsername:          "aleph0-api",
			OSAPIPassword:          "secret1",
			OSProvisionerUsername:  "aleph0-provisioner",
			OSProvisionerPassword:  "secret2",
			Regions:                []config.Region{{ID: "ro-1", Name: "Romania 1", OpenStackRegion: "RegionOne", ExternalNetwork: "public"}},
			LimitTenantsPerUser:    3,
			LimitProjectsPerTenant: 10,
		}
		if !reflect.DeepEqual(c, want) {
			t.Fatalf("got %+v, want %+v", c, want)
		}
	})

	t.Run("overrides", func(t *testing.T) {
		c, err := config.Load(env(with(map[string]string{
			"HTTP_ADDR": ":9999", "LIMIT_TENANTS_PER_USER": "5", "LIMIT_PROJECTS_PER_TENANT": "1",
		})))
		if err != nil {
			t.Fatal(err)
		}
		if c.HTTPAddr != ":9999" || c.LimitTenantsPerUser != 5 || c.LimitProjectsPerTenant != 1 {
			t.Fatalf("got %+v", c)
		}
	})

	t.Run("lists every missing variable", func(t *testing.T) {
		_, err := config.Load(env(map[string]string{"OIDC_AUDIENCE": "api"}))
		if err == nil {
			t.Fatal("want error")
		}
		for _, name := range []string{"DATABASE_URL", "OIDC_ISSUER", "OS_AUTH_URL", "OS_API_USERNAME", "OS_API_PASSWORD",
			"OS_PROVISIONER_USERNAME", "OS_PROVISIONER_PASSWORD", "REGIONS"} {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error %q does not mention %s", err, name)
			}
		}
		if strings.Contains(err.Error(), "OIDC_AUDIENCE") {
			t.Errorf("error %q mentions OIDC_AUDIENCE, which is set", err)
		}
	})

	for name, tc := range map[string]struct{ key, value, want string }{
		"regions not JSON":       {"REGIONS", "ro-1", "REGIONS is not a JSON list"},
		"no region":              {"REGIONS", "[]", "REGIONS lists no region"},
		"region missing a field": {"REGIONS", `[{"id":"ro-1","name":"Romania 1"}]`, "REGIONS[0] needs"},
		"duplicate region":       {"REGIONS", "[" + region + "," + region + "]", `REGIONS lists "ro-1" twice`},
		"limit not a number":     {"LIMIT_TENANTS_PER_USER", "many", "LIMIT_TENANTS_PER_USER must be a positive integer"},
		"limit zero":             {"LIMIT_PROJECTS_PER_TENANT", "0", "LIMIT_PROJECTS_PER_TENANT must be a positive integer"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := config.Load(env(with(map[string]string{tc.key: tc.value})))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
