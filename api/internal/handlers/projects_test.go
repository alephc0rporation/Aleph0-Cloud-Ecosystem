package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/alephc0rporation/Cardinal/api/internal/oapi"
	"github.com/alephc0rporation/Cardinal/api/internal/store"
)

func newProject(name string) oapi.CreateProjectRequestObject {
	return oapi.CreateProjectRequestObject{Body: &oapi.CreateProjectJSONRequestBody{Name: name}}
}

func TestCreateProjectProvisionsItInOpenStack(t *testing.T) {
	h, s, c := newHandlers()
	tenant := s.addTenant("alice", "acme")
	req := newProject("web")
	req.TenantId = tenant

	res, err := h.CreateProject(as("alice"), req)
	if err != nil {
		t.Fatal(err)
	}
	p := res.(oapi.CreateProject201JSONResponse)
	if string(p.Status) != "ready" || p.TenantId != tenant {
		t.Fatalf("created %+v", p)
	}
	osID := "os-" + p.Id.String()
	if got := s.project(p.Id); got.OpenStackProjectID != osID || got.Status != store.StatusReady {
		t.Fatalf("stored %+v", got)
	}
	if !slices.Contains(c.calls, "CreateProject "+p.Id.String()+" tenant="+tenant.String()) || !c.networks["ro-1/"+osID] {
		t.Fatalf("cloud calls %v, networks %v", c.calls, c.networks)
	}
}

func TestCreateProjectFailuresLeaveItFailed(t *testing.T) {
	timeout := &url.Error{Op: "Post", URL: "http://10.10.0.10:5000/v3/auth/tokens", Err: errors.New("i/o timeout")}
	for name, tc := range map[string]struct {
		setup      func(*fakeCloud)
		wantStatus int
		wantOSID   bool
	}{
		"keystone error":     {func(c *fakeCloud) { c.failCreate = errors.New("boom") }, http.StatusBadGateway, false},
		"keystone not there": {func(c *fakeCloud) { c.failCreate = timeout }, http.StatusServiceUnavailable, false},
		"network error":      {func(c *fakeCloud) { c.failNetwork = errors.New("boom") }, http.StatusBadGateway, true},
	} {
		t.Run(name, func(t *testing.T) {
			h, s, c := newHandlers()
			tc.setup(c)
			tenant := s.addTenant("alice", "acme")
			req := newProject("web")
			req.TenantId = tenant

			_, err := h.CreateProject(as("alice"), req)
			if statusOf(t, err) != tc.wantStatus {
				t.Fatalf("status %d, want %d", statusOf(t, err), tc.wantStatus)
			}
			ps, _ := fakeProjects{s}.List(context.Background(), tenant)
			if len(ps) != 1 || ps[0].Status != store.StatusFailed || (ps[0].OpenStackProjectID != "") != tc.wantOSID {
				t.Fatalf("stored %+v", ps)
			}
		})
	}
}

func TestProjectNamesAndLimit(t *testing.T) {
	h, s, _ := newHandlers() // 2 projects per tenant
	tenant := s.addTenant("alice", "acme")
	create := func(name string) error {
		req := newProject(name)
		req.TenantId = tenant
		_, err := h.CreateProject(as("alice"), req)
		return err
	}
	if err := create("web"); err != nil {
		t.Fatal(err)
	}
	if statusOf(t, create("web")) != http.StatusConflict {
		t.Fatal("duplicate name: want 409")
	}
	if err := create("db"); err != nil {
		t.Fatal(err)
	}
	if statusOf(t, create("cache")) != http.StatusForbidden {
		t.Fatal("third project: want 403")
	}
}

func TestDeleteProjectRefusesWhileVMsRun(t *testing.T) {
	h, s, c := newHandlers()
	tenant := s.addTenant("alice", "acme")
	project := s.addProject(tenant, "web", store.StatusReady, "os-1")
	c.servers = 1

	_, err := h.DeleteProject(as("alice"), oapi.DeleteProjectRequestObject{ProjectId: project})
	if statusOf(t, err) != http.StatusConflict {
		t.Fatal("project with VMs: want 409")
	}
	if got := s.project(project).Status; got != store.StatusReady {
		t.Fatalf("status %q, want ready again", got)
	}
	if slices.ContainsFunc(c.calls, func(call string) bool { return call != "CountServers ro-1/os-1" }) {
		t.Fatalf("cloud calls %v, want only the count", c.calls)
	}
}

func TestDeleteProjectResumesAfterAFailure(t *testing.T) {
	h, s, c := newHandlers()
	tenant := s.addTenant("alice", "acme")
	project := s.addProject(tenant, "web", store.StatusReady, "os-1")
	c.projects[project.String()] = "os-1"
	c.networks["ro-1/os-1"] = true
	c.failDeleteNetwork = errors.New("boom")

	_, err := h.DeleteProject(as("alice"), oapi.DeleteProjectRequestObject{ProjectId: project})
	if statusOf(t, err) != http.StatusBadGateway {
		t.Fatal("network removal failing: want 502")
	}
	if got := s.project(project).Status; got != store.StatusDeleting {
		t.Fatalf("status %q, want deleting", got)
	}

	c.failDeleteNetwork = nil
	if _, err := h.DeleteProject(as("alice"), oapi.DeleteProjectRequestObject{ProjectId: project}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.GetProject(as("alice"), oapi.GetProjectRequestObject{ProjectId: project}); statusOf(t, err) != http.StatusNotFound {
		t.Fatal("deleted project: want 404")
	}
	if len(c.projects) != 0 || len(c.networks) != 0 {
		t.Fatalf("left in the cloud: projects %v, networks %v", c.projects, c.networks)
	}
}

func TestDeleteProjectWhoseOpenStackIDWasNeverSaved(t *testing.T) {
	h, s, c := newHandlers()
	tenant := s.addTenant("alice", "acme")
	project := s.addProject(tenant, "web", store.StatusFailed, "")

	if _, err := h.DeleteProject(as("alice"), oapi.DeleteProjectRequestObject{ProjectId: project}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"DeleteProject " + project.String()}; !slices.Equal(c.calls, want) {
		t.Fatalf("cloud calls %v, want %v", c.calls, want)
	}
}

func TestDeleteProjectStillProvisioning(t *testing.T) {
	h, s, _ := newHandlers()
	tenant := s.addTenant("alice", "acme")
	project := s.addProject(tenant, "web", store.StatusProvisioning, "")
	_, err := h.DeleteProject(as("alice"), oapi.DeleteProjectRequestObject{ProjectId: project})
	if statusOf(t, err) != http.StatusConflict {
		t.Fatal("project being created: want 409")
	}
}

func TestProjectsAreInvisibleToStrangers(t *testing.T) {
	h, s, c := newHandlers()
	tenant := s.addTenant("alice", "acme")
	project := s.addProject(tenant, "web", store.StatusReady, "os-1")

	if _, err := h.GetProject(as("bob"), oapi.GetProjectRequestObject{ProjectId: project}); statusOf(t, err) != http.StatusNotFound {
		t.Fatal("GetProject as a stranger: want 404")
	}
	if _, err := h.DeleteProject(as("bob"), oapi.DeleteProjectRequestObject{ProjectId: project}); statusOf(t, err) != http.StatusNotFound {
		t.Fatal("DeleteProject as a stranger: want 404")
	}
	if _, err := h.GetProject(as("alice"), oapi.GetProjectRequestObject{ProjectId: uuid.New()}); statusOf(t, err) != http.StatusNotFound {
		t.Fatal("unknown project: want 404")
	}
	if len(c.calls) != 0 {
		t.Fatalf("a stranger reached the cloud: %v", c.calls)
	}
}
