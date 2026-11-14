//go:build integration

package store_test

import (
	"context"
	"crypto/rand"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alephc0rporation/Cardinal/api/internal/store"
)

// newStore migrates a schema created for this test and dropped after it.
func newStore(t *testing.T) (*store.Tenants, *store.Projects) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	schema := "test_" + strings.ToLower(rand.Text())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})

	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	if err := store.Migrate(ctx, u.String()); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return store.NewTenants(pool), store.NewProjects(pool)
}

func TestTenantLimitHoldsUnderParallelRequests(t *testing.T) {
	tenants, _ := newStore(t)
	var wg sync.WaitGroup
	var created atomic.Int32
	for range 10 {
		wg.Go(func() {
			_, err := tenants.Create(context.Background(), "acme", "user-1", 3)
			switch {
			case err == nil:
				created.Add(1)
			case !errors.Is(err, store.ErrLimit):
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := created.Load(); n != 3 {
		t.Fatalf("created %d tenants, want 3", n)
	}
}

func TestTenantsAndRoles(t *testing.T) {
	tenants, _ := newStore(t)
	ctx := context.Background()
	tn, err := tenants.Create(ctx, "acme", "alice", 3)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := tenants.List(ctx, "alice")
	if err != nil || len(ts) != 1 || ts[0].ID != tn.ID || ts[0].Role != "owner" {
		t.Fatalf("ListTenants(alice) = %+v, %v", ts, err)
	}
	if role, err := tenants.Role(ctx, tn.ID, "bob"); err != nil || role != "" {
		t.Fatalf("Role(bob) = %q, %v; want no role", role, err)
	}
	if ts, err := tenants.List(ctx, "bob"); err != nil || len(ts) != 0 {
		t.Fatalf("ListTenants(bob) = %+v, %v; want none", ts, err)
	}
}

func TestProjectsRespectNamesLimitsAndTenantDeletion(t *testing.T) {
	tenants, projects := newStore(t)
	ctx := context.Background()
	tn, _ := tenants.Create(ctx, "acme", "alice", 3)

	p, err := projects.Create(ctx, tn.ID, "web", 2)
	if err != nil || p.Status != store.StatusProvisioning {
		t.Fatalf("CreateProject = %+v, %v", p, err)
	}
	if _, err := projects.Create(ctx, tn.ID, "web", 2); !errors.Is(err, store.ErrDuplicate) {
		t.Fatalf("duplicate name: err = %v, want ErrDuplicate", err)
	}
	if _, err := projects.Create(ctx, tn.ID, "db", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := projects.Create(ctx, tn.ID, "cache", 2); !errors.Is(err, store.ErrLimit) {
		t.Fatalf("third project: err = %v, want ErrLimit", err)
	}
	if err := tenants.Delete(ctx, tn.ID); !errors.Is(err, store.ErrNotEmpty) {
		t.Fatalf("DeleteTenant with projects: err = %v, want ErrNotEmpty", err)
	}

	if err := projects.SetOpenStackID(ctx, p.ID, "os-1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := projects.Get(ctx, p.ID); got.OpenStackProjectID != "os-1" {
		t.Fatalf("OpenStackProjectID = %q, want os-1", got.OpenStackProjectID)
	}

	ps, _ := projects.List(ctx, tn.ID)
	for _, p := range ps {
		if err := projects.Delete(ctx, p.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := tenants.Delete(ctx, tn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tenants.Get(ctx, tn.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetTenant after delete: err = %v, want ErrNotFound", err)
	}
}

func TestStartProjectDeletion(t *testing.T) {
	tenants, projects := newStore(t)
	ctx := context.Background()
	tn, _ := tenants.Create(ctx, "acme", "alice", 3)
	p, _ := projects.Create(ctx, tn.ID, "web", 10)

	if _, err := projects.StartDeletion(ctx, p.ID, time.Now()); !errors.Is(err, store.ErrBusy) {
		t.Fatalf("fresh provisioning project: err = %v, want ErrBusy", err)
	}
	prev, err := projects.StartDeletion(ctx, p.ID, time.Now().Add(6*time.Minute))
	if err != nil || prev != store.StatusFailed {
		t.Fatalf("stale provisioning project: prev = %q, err = %v; want failed", prev, err)
	}
	if got, _ := projects.Get(ctx, p.ID); got.Status != store.StatusDeleting {
		t.Fatalf("status = %q, want deleting", got.Status)
	}

	q, _ := projects.Create(ctx, tn.ID, "db", 10)
	_ = projects.SetStatus(ctx, q.ID, store.StatusReady)
	if prev, err := projects.StartDeletion(ctx, q.ID, time.Now()); err != nil || prev != store.StatusReady {
		t.Fatalf("ready project: prev = %q, err = %v; want ready", prev, err)
	}
	if _, err := projects.StartDeletion(ctx, [16]byte{1}, time.Now()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown project: err = %v, want ErrNotFound", err)
	}
}
