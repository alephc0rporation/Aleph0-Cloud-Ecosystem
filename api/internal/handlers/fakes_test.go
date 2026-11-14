package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/alephc0rporation/Cardinal/api/internal/auth"
	"github.com/alephc0rporation/Cardinal/api/internal/config"
	"github.com/alephc0rporation/Cardinal/api/internal/handlers"
	"github.com/alephc0rporation/Cardinal/api/internal/problem"
	"github.com/alephc0rporation/Cardinal/api/internal/store"
)

func newHandlers() (*handlers.Handlers, *fakeDB, *fakeCloud) {
	d := &fakeDB{
		tenants: map[uuid.UUID]store.Tenant{}, createdBy: map[uuid.UUID]string{},
		members: map[uuid.UUID]map[string]string{}, projects: map[uuid.UUID]store.Project{},
	}
	c := &fakeCloud{projects: map[string]string{}, networks: map[string]bool{}}
	h := &handlers.Handlers{
		Tenants:  fakeTenants{d},
		Projects: fakeProjects{d},
		Cloud:    c,
		Regions:  []config.Region{{ID: "ro-1", Name: "Romania 1", OpenStackRegion: "RegionOne", ExternalNetwork: "public"}},
		Limits:   handlers.Limits{TenantsPerUser: 2, ProjectsPerTenant: 2},
	}
	return h, d, c
}

func as(sub string) context.Context {
	return auth.WithClaims(context.Background(), auth.Claims{Subject: sub})
}

func signedInAs(sub string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), auth.Claims{Subject: sub})))
		})
	}
}

// statusOf returns the HTTP status a handler error turns into.
func statusOf(t *testing.T, err error) int {
	t.Helper()
	var pe *problem.Error
	if !errors.As(err, &pe) {
		t.Fatalf("error %v is not a problem response", err)
	}
	return pe.Status
}

// fakeDB holds the data both fake repositories share, in memory, with the store's rules
// that the handlers rely on.
type fakeDB struct {
	tenants   map[uuid.UUID]store.Tenant
	createdBy map[uuid.UUID]string
	members   map[uuid.UUID]map[string]string // tenant → sub → role
	projects  map[uuid.UUID]store.Project
}

func (d *fakeDB) addTenant(owner, name string) uuid.UUID {
	id := uuid.New()
	d.tenants[id] = store.Tenant{ID: id, Name: name, CreatedAt: time.Now()}
	d.createdBy[id] = owner
	d.members[id] = map[string]string{owner: "owner"}
	return id
}

func (d *fakeDB) addMember(tenant uuid.UUID, sub, role string) { d.members[tenant][sub] = role }

func (d *fakeDB) addProject(tenant uuid.UUID, name string, status store.Status, osID string) uuid.UUID {
	id := uuid.New()
	d.projects[id] = store.Project{ID: id, TenantID: tenant, Name: name, Status: status, OpenStackProjectID: osID, CreatedAt: time.Now()}
	return id
}

func (d *fakeDB) project(id uuid.UUID) store.Project { return d.projects[id] }

type fakeTenants struct{ *fakeDB }

func (f fakeTenants) List(_ context.Context, sub string) ([]store.Tenant, error) {
	var out []store.Tenant
	for id, m := range f.members {
		if role, ok := m[sub]; ok {
			t := f.tenants[id]
			t.Role = role
			out = append(out, t)
		}
	}
	return out, nil
}

func (f fakeTenants) Create(_ context.Context, name, sub string, limit int) (store.Tenant, error) {
	n := 0
	for _, by := range f.createdBy {
		if by == sub {
			n++
		}
	}
	if n >= limit {
		return store.Tenant{}, store.ErrLimit
	}
	id := f.addTenant(sub, name)
	t := f.tenants[id]
	t.Role = "owner"
	return t, nil
}

func (f fakeTenants) Get(_ context.Context, id uuid.UUID) (store.Tenant, error) {
	t, ok := f.tenants[id]
	if !ok {
		return store.Tenant{}, store.ErrNotFound
	}
	return t, nil
}

func (f fakeTenants) Role(_ context.Context, tenantID uuid.UUID, sub string) (string, error) {
	return f.members[tenantID][sub], nil
}

func (f fakeTenants) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := f.tenants[id]; !ok {
		return store.ErrNotFound
	}
	for _, p := range f.projects {
		if p.TenantID == id {
			return store.ErrNotEmpty
		}
	}
	delete(f.tenants, id)
	delete(f.members, id)
	delete(f.createdBy, id)
	return nil
}

type fakeProjects struct{ *fakeDB }

func (f fakeProjects) List(_ context.Context, tenantID uuid.UUID) ([]store.Project, error) {
	var out []store.Project
	for _, p := range f.projects {
		if p.TenantID == tenantID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f fakeProjects) Create(_ context.Context, tenantID uuid.UUID, name string, limit int) (store.Project, error) {
	if _, ok := f.tenants[tenantID]; !ok {
		return store.Project{}, store.ErrNotFound
	}
	n := 0
	for _, p := range f.projects {
		if p.TenantID == tenantID {
			n++
			if p.Name == name {
				return store.Project{}, store.ErrDuplicate
			}
		}
	}
	if n >= limit {
		return store.Project{}, store.ErrLimit
	}
	return f.projects[f.addProject(tenantID, name, store.StatusProvisioning, "")], nil
}

func (f fakeProjects) Get(_ context.Context, id uuid.UUID) (store.Project, error) {
	p, ok := f.projects[id]
	if !ok {
		return store.Project{}, store.ErrNotFound
	}
	return p, nil
}

func (f fakeProjects) SetOpenStackID(_ context.Context, id uuid.UUID, osID string) error {
	p := f.projects[id]
	p.OpenStackProjectID = osID
	f.projects[id] = p
	return nil
}

func (f fakeProjects) SetStatus(_ context.Context, id uuid.UUID, s store.Status) error {
	p := f.projects[id]
	p.Status = s
	f.projects[id] = p
	return nil
}

// The real store also lets a stale provisioning project through; its integration test covers that.
func (f fakeProjects) StartDeletion(_ context.Context, id uuid.UUID, _ time.Time) (store.Status, error) {
	p, ok := f.projects[id]
	if !ok {
		return "", store.ErrNotFound
	}
	if p.Status == store.StatusProvisioning {
		return "", store.ErrBusy
	}
	prev := p.Status
	p.Status = store.StatusDeleting
	f.projects[id] = p
	return prev, nil
}

func (f fakeProjects) Delete(_ context.Context, id uuid.UUID) error {
	delete(f.projects, id)
	return nil
}

// fakeCloud records calls and fails where a test asks it to.
type fakeCloud struct {
	failCreate, failNetwork, failDeleteNetwork, failCount error
	servers                                               int
	projects                                              map[string]string // our project ID → OpenStack ID
	networks                                              map[string]bool   // region/OpenStack ID
	calls                                                 []string
}

func (f *fakeCloud) CreateProject(_ context.Context, projectID, tenantID, _ string) (string, error) {
	f.calls = append(f.calls, "CreateProject "+projectID+" tenant="+tenantID)
	if f.failCreate != nil {
		return "", f.failCreate
	}
	f.projects[projectID] = "os-" + projectID
	return f.projects[projectID], nil
}

func (f *fakeCloud) DeleteProject(_ context.Context, projectID string) error {
	f.calls = append(f.calls, "DeleteProject "+projectID)
	delete(f.projects, projectID)
	return nil
}

func (f *fakeCloud) EnsureDefaultNetwork(_ context.Context, region, osID string) error {
	f.calls = append(f.calls, "EnsureDefaultNetwork "+region+"/"+osID)
	if f.failNetwork != nil {
		return f.failNetwork
	}
	f.networks[region+"/"+osID] = true
	return nil
}

func (f *fakeCloud) DeleteDefaultNetwork(_ context.Context, region, osID string) error {
	f.calls = append(f.calls, "DeleteDefaultNetwork "+region+"/"+osID)
	if f.failDeleteNetwork != nil {
		return f.failDeleteNetwork
	}
	delete(f.networks, region+"/"+osID)
	return nil
}

func (f *fakeCloud) CountServers(_ context.Context, region, osID string) (int, error) {
	f.calls = append(f.calls, "CountServers "+region+"/"+osID)
	return f.servers, f.failCount
}
