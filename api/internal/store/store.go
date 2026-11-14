// Package store keeps tenants, memberships and projects in PostgreSQL.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alephc0rporation/Cardinal/api/internal/store/db"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrLimit     = errors.New("limit reached")
	ErrDuplicate = errors.New("name already used")
	ErrNotEmpty  = errors.New("tenant still has projects")
	ErrBusy      = errors.New("project is still being created")
)

// A project provisioning this long was abandoned by an API that stopped mid-request.
const staleProvisioning = 5 * time.Minute

type Status string

const (
	StatusProvisioning Status = "provisioning"
	StatusReady        Status = "ready"
	StatusFailed       Status = "failed"
	StatusDeleting     Status = "deleting"
)

type Tenant struct {
	ID        uuid.UUID
	Name      string
	Role      string // the caller's role; empty when the tenant was not loaded for a caller
	CreatedAt time.Time
}

type Project struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	Name               string
	OpenStackProjectID string // empty until the OpenStack project exists
	Status             Status
	CreatedAt          time.Time
}

type Store struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool, q: db.New(pool)} }

func (s *Store) tx(ctx context.Context, fn func(*db.Queries) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(s.q.WithTx(tx)) })
}

func (s *Store) ListTenants(ctx context.Context, sub string) ([]Tenant, error) {
	rows, err := s.q.ListTenantsForUser(ctx, sub)
	if err != nil {
		return nil, err
	}
	out := make([]Tenant, len(rows))
	for i, r := range rows {
		out[i] = Tenant{ID: r.ID, Name: r.Name, Role: r.Role, CreatedAt: r.CreatedAt}
	}
	return out, nil
}

// CreateTenant creates a tenant owned by sub, unless sub has already created limit tenants.
func (s *Store) CreateTenant(ctx context.Context, name, sub string, limit int) (Tenant, error) {
	var t Tenant
	err := s.tx(ctx, func(q *db.Queries) error {
		// Parallel requests from one user would all pass the count without this lock.
		if err := q.LockUser(ctx, sub); err != nil {
			return err
		}
		n, err := q.CountTenantsCreatedBy(ctx, sub)
		if err != nil {
			return err
		}
		if n >= int64(limit) {
			return ErrLimit
		}
		row, err := q.InsertTenant(ctx, db.InsertTenantParams{Name: name, CreatedBy: sub})
		if err != nil {
			return err
		}
		t = Tenant{ID: row.ID, Name: row.Name, Role: "owner", CreatedAt: row.CreatedAt}
		return q.InsertMembership(ctx, db.InsertMembershipParams{TenantID: row.ID, UserSub: sub, Role: "owner"})
	})
	return t, err
}

func (s *Store) GetTenant(ctx context.Context, id uuid.UUID) (Tenant, error) {
	row, err := s.q.GetTenant(ctx, id)
	if err != nil {
		return Tenant{}, notFound(err)
	}
	return Tenant{ID: row.ID, Name: row.Name, CreatedAt: row.CreatedAt}, nil
}

// Role returns sub's role in the tenant, or "" when sub is not a member.
func (s *Store) Role(ctx context.Context, tenantID uuid.UUID, sub string) (string, error) {
	role, err := s.q.GetRole(ctx, db.GetRoleParams{TenantID: tenantID, UserSub: sub})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// DeleteTenant removes a tenant and its memberships if it has no projects.
func (s *Store) DeleteTenant(ctx context.Context, id uuid.UUID) error {
	return s.tx(ctx, func(q *db.Queries) error {
		// Waits for a project being inserted in this tenant, so the count sees it.
		if _, err := q.LockTenant(ctx, id); err != nil {
			return notFound(err)
		}
		n, err := q.CountProjects(ctx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrNotEmpty
		}
		if err := q.DeleteMemberships(ctx, id); err != nil {
			return err
		}
		return q.DeleteTenant(ctx, id)
	})
}

func (s *Store) ListProjects(ctx context.Context, tenantID uuid.UUID) ([]Project, error) {
	rows, err := s.q.ListProjects(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]Project, len(rows))
	for i, r := range rows {
		out[i] = project(r)
	}
	return out, nil
}

// CreateProject inserts a provisioning project, unless the tenant has limit projects or the name is taken.
func (s *Store) CreateProject(ctx context.Context, tenantID uuid.UUID, name string, limit int) (Project, error) {
	var p Project
	err := s.tx(ctx, func(q *db.Queries) error {
		if _, err := q.LockTenant(ctx, tenantID); err != nil {
			return notFound(err)
		}
		n, err := q.CountProjects(ctx, tenantID)
		if err != nil {
			return err
		}
		if n >= int64(limit) {
			return ErrLimit
		}
		row, err := q.InsertProject(ctx, db.InsertProjectParams{TenantID: tenantID, Name: name})
		if err != nil {
			return duplicate(err)
		}
		p = project(row)
		return nil
	})
	return p, err
}

func (s *Store) GetProject(ctx context.Context, id uuid.UUID) (Project, error) {
	row, err := s.q.GetProject(ctx, id)
	if err != nil {
		return Project{}, notFound(err)
	}
	return project(row), nil
}

func (s *Store) SetOpenStackProjectID(ctx context.Context, id uuid.UUID, openstackID string) error {
	return s.q.SetOpenStackProjectID(ctx, db.SetOpenStackProjectIDParams{ID: id, OpenstackProjectID: &openstackID})
}

func (s *Store) SetProjectStatus(ctx context.Context, id uuid.UUID, status Status) error {
	return s.q.SetProjectStatus(ctx, db.SetProjectStatusParams{ID: id, Status: db.ProjectStatus(status)})
}

// StartProjectDeletion marks a project deleting and returns the status to put back if the
// deletion is refused. A project still provisioning gets ErrBusy, unless it is stale: then
// the API stopped while creating it, and it counts as failed.
func (s *Store) StartProjectDeletion(ctx context.Context, id uuid.UUID, now time.Time) (Status, error) {
	var prev Status
	err := s.tx(ctx, func(q *db.Queries) error {
		row, err := q.GetProjectForUpdate(ctx, id)
		if err != nil {
			return notFound(err)
		}
		prev = Status(row.Status)
		if prev == StatusProvisioning {
			if now.Sub(row.CreatedAt) < staleProvisioning {
				return ErrBusy
			}
			prev = StatusFailed
		}
		return q.SetProjectStatus(ctx, db.SetProjectStatusParams{ID: id, Status: db.ProjectStatusDeleting})
	})
	return prev, err
}

func (s *Store) DeleteProject(ctx context.Context, id uuid.UUID) error {
	return s.q.DeleteProject(ctx, id)
}

func project(r db.Project) Project {
	p := Project{ID: r.ID, TenantID: r.TenantID, Name: r.Name, Status: Status(r.Status), CreatedAt: r.CreatedAt}
	if r.OpenstackProjectID != nil {
		p.OpenStackProjectID = *r.OpenstackProjectID
	}
	return p
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func duplicate(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" { // unique_violation
		return ErrDuplicate
	}
	return err
}
