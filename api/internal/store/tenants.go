package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alephc0rporation/Cardinal/api/internal/store/db"
)

// Tenants keeps tenants and their memberships, which always change together.
type Tenants struct{ conn }

func NewTenants(pool *pgxpool.Pool) *Tenants { return &Tenants{newConn(pool)} }

// List returns the tenants sub is a member of, with sub's role in each.
func (t *Tenants) List(ctx context.Context, sub string) ([]Tenant, error) {
	rows, err := t.q.ListTenantsForUser(ctx, sub)
	if err != nil {
		return nil, err
	}
	out := make([]Tenant, len(rows))
	for i, r := range rows {
		out[i] = Tenant{ID: r.ID, Name: r.Name, Role: r.Role, CreatedAt: r.CreatedAt}
	}
	return out, nil
}

// Create creates a tenant owned by sub, unless sub has already created limit tenants.
func (t *Tenants) Create(ctx context.Context, name, sub string, limit int) (Tenant, error) {
	var out Tenant
	err := t.tx(ctx, func(q *db.Queries) error {
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
		out = Tenant{ID: row.ID, Name: row.Name, Role: "owner", CreatedAt: row.CreatedAt}
		return q.InsertMembership(ctx, db.InsertMembershipParams{TenantID: row.ID, UserSub: sub, Role: "owner"})
	})
	return out, err
}

func (t *Tenants) Get(ctx context.Context, id uuid.UUID) (Tenant, error) {
	row, err := t.q.GetTenant(ctx, id)
	if err != nil {
		return Tenant{}, notFound(err)
	}
	return Tenant{ID: row.ID, Name: row.Name, CreatedAt: row.CreatedAt}, nil
}

// Role returns sub's role in the tenant, or "" when sub is not a member.
func (t *Tenants) Role(ctx context.Context, tenantID uuid.UUID, sub string) (string, error) {
	role, err := t.q.GetRole(ctx, db.GetRoleParams{TenantID: tenantID, UserSub: sub})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return role, err
}

// Delete removes a tenant and its memberships if it has no projects.
func (t *Tenants) Delete(ctx context.Context, id uuid.UUID) error {
	return t.tx(ctx, func(q *db.Queries) error {
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
