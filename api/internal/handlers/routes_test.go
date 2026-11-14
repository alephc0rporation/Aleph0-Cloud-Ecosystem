package handlers_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/alephc0rporation/Cardinal/api/internal/oapi"
	"github.com/alephc0rporation/Cardinal/api/internal/server"
	"github.com/alephc0rporation/Cardinal/api/internal/store"
)

var pathParam = regexp.MustCompile(`\{[^}]+\}`)

// Every tenant and project route in the OpenAPI spec answers 404 to a signed-in user who is not
// a member. The routes come from the spec, so a new route is checked without editing this test;
// only a route with a request body needs an entry in bodies.
func TestEveryTenantAndProjectRouteHidesFromStrangers(t *testing.T) {
	h, s, c := newHandlers()
	tenant := s.addTenant("alice", "acme")
	project := s.addProject(tenant, "web", store.StatusReady, "os-1")

	bodies := map[string]string{
		"POST /v1/tenants/{tenantId}/projects": `{"name":"x"}`,
	}
	values := map[string]string{"tenantId": tenant.String(), "projectId": project.String(), "region": "ro-1"}

	spec, err := oapi.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	srv := server.Handler(h, signedInAs("mallory"))
	checked := 0
	for path, item := range spec.Paths.Map() {
		if !strings.HasPrefix(path, "/v1/tenants/{") && !strings.HasPrefix(path, "/v1/projects/{") {
			continue
		}
		for method, op := range item.Operations() {
			route := method + " " + path
			target := pathParam.ReplaceAllStringFunc(path, func(p string) string {
				if v, ok := values[strings.Trim(p, "{}")]; ok {
					return v
				}
				return uuid.NewString() // another resource's ID, e.g. a server's
			})
			var body io.Reader
			if op.RequestBody != nil {
				b, ok := bodies[route]
				if !ok {
					t.Errorf("%s takes a request body: add one to bodies", route)
					continue
				}
				body = strings.NewReader(b)
			}
			req := httptest.NewRequest(method, target, body)
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s as a stranger: status %d, want 404; body %s", route, rec.Code, rec.Body)
			}
			checked++
		}
	}
	if checked < 6 {
		t.Fatalf("checked %d routes, want at least the 6 tenant and project routes", checked)
	}
	if len(c.calls) != 0 {
		t.Fatalf("a stranger reached the cloud: %v", c.calls)
	}
}
