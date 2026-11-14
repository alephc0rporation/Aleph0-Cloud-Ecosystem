package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/alephc0rporation/Cardinal/api/internal/oapi"
	"github.com/alephc0rporation/Cardinal/api/internal/server"
	"github.com/alephc0rporation/Cardinal/api/internal/store"
)

func TestListRegions(t *testing.T) {
	h, _, _ := newHandlers()
	res, err := h.ListRegions(as("alice"), oapi.ListRegionsRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	got := res.(oapi.ListRegions200JSONResponse)
	if len(got) != 1 || got[0].Id != "ro-1" || got[0].Name != "Romania 1" {
		t.Fatalf("got %+v", got)
	}
}

func TestTenantIsInvisibleToStrangers(t *testing.T) {
	h, _, _ := newHandlers()
	res, err := h.CreateTenant(as("alice"), oapi.CreateTenantRequestObject{Body: &oapi.CreateTenantJSONRequestBody{Name: "acme"}})
	if err != nil {
		t.Fatal(err)
	}
	tenant := res.(oapi.CreateTenant201JSONResponse)
	if tenant.Name != "acme" || string(tenant.Role) != "owner" {
		t.Fatalf("created %+v", tenant)
	}

	if res, err := h.GetTenant(as("alice"), oapi.GetTenantRequestObject{TenantId: tenant.Id}); err != nil ||
		string(res.(oapi.GetTenant200JSONResponse).Role) != "owner" {
		t.Fatalf("GetTenant as owner = %+v, %v", res, err)
	}
	if _, err := h.GetTenant(as("bob"), oapi.GetTenantRequestObject{TenantId: tenant.Id}); statusOf(t, err) != http.StatusNotFound {
		t.Fatal("GetTenant as a stranger: want 404")
	}
	if _, err := h.DeleteTenant(as("bob"), oapi.DeleteTenantRequestObject{TenantId: tenant.Id}); statusOf(t, err) != http.StatusNotFound {
		t.Fatal("DeleteTenant as a stranger: want 404")
	}
	list, err := h.ListTenants(as("bob"), oapi.ListTenantsRequestObject{})
	if err != nil || len(list.(oapi.ListTenants200JSONResponse)) != 0 {
		t.Fatalf("ListTenants as a stranger = %+v, %v; want none", list, err)
	}
}

func TestTenantLimit(t *testing.T) {
	h, _, _ := newHandlers() // 2 tenants per user
	for range 2 {
		if _, err := h.CreateTenant(as("alice"), oapi.CreateTenantRequestObject{Body: &oapi.CreateTenantJSONRequestBody{Name: "t"}}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := h.CreateTenant(as("alice"), oapi.CreateTenantRequestObject{Body: &oapi.CreateTenantJSONRequestBody{Name: "t"}})
	if statusOf(t, err) != http.StatusForbidden {
		t.Fatal("third tenant: want 403")
	}
}

func TestOnlyOwnersDeleteEmptyTenants(t *testing.T) {
	h, s, _ := newHandlers()
	tenant := s.addTenant("alice", "acme")
	s.addMember(tenant, "bob", "member")
	project := s.addProject(tenant, "web", store.StatusReady, "os-1")

	if _, err := h.DeleteTenant(as("bob"), oapi.DeleteTenantRequestObject{TenantId: tenant}); statusOf(t, err) != http.StatusForbidden {
		t.Fatal("member deleting the tenant: want 403")
	}
	if _, err := h.DeleteTenant(as("alice"), oapi.DeleteTenantRequestObject{TenantId: tenant}); statusOf(t, err) != http.StatusConflict {
		t.Fatal("deleting a tenant with projects: want 409")
	}
	delete(s.projects, project)
	if res, err := h.DeleteTenant(as("alice"), oapi.DeleteTenantRequestObject{TenantId: tenant}); err != nil {
		t.Fatalf("DeleteTenant = %+v, %v", res, err)
	}
	if _, err := h.GetTenant(as("alice"), oapi.GetTenantRequestObject{TenantId: tenant}); statusOf(t, err) != http.StatusNotFound {
		t.Fatal("deleted tenant: want 404")
	}
}

// Through the HTTP stack: a handler's problem error reaches the client with its status.
func TestProblemErrorsReachTheClient(t *testing.T) {
	h, _, _ := newHandlers()
	rec := httptest.NewRecorder()
	server.Handler(h, signedInAs("bob")).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tenants/"+uuid.NewString(), nil))
	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status %d, content type %q; want 404 problem+json", rec.Code, rec.Header().Get("Content-Type"))
	}
}
