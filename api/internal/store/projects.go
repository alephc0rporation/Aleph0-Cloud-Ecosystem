package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alephc0rporation/Cardinal/api/internal/store/db"
)

// A project provisioning this long was abandoned by an API that stopped mid-request.
const staleProvisioning = 5 * time.Minute

// Projects keeps the projects of every tenant.
type Projects struct{ conn }

func NewProjects(pool *pgxpool.Pool) *Projects { return &Projects{newConn(pool)} }

func (p *Projects) List(ctx context.Context, tenantID uuid.UUID) ([]Project, error) {
	rows, err := p.q.ListProjects(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	out := make([]Project, len(rows))
	for i, r := range rows {
		out[i] = project(r)
	}
	return out, nil
}

// Create inserts a provisioning project, unless the tenant has limit projects or the name is taken.
func (p *Projects) Create(ctx context.Context, tenantID uuid.UUID, name string, limit int) (Project, error) {
	var out Project
	err := p.tx(ctx, func(q *db.Queries) error {
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
		out = project(row)
		return nil
	})
	return out, err
}

func (p *Projects) Get(ctx context.Context, id uuid.UUID) (Project, error) {
	row, err := p.q.GetProject(ctx, id)
	if err != nil {
		return Project{}, notFound(err)
	}
	return project(row), nil
}

func (p *Projects) SetOpenStackID(ctx context.Context, id uuid.UUID, openstackID string) error {
	return p.q.SetOpenStackProjectID(ctx, db.SetOpenStackProjectIDParams{ID: id, OpenstackProjectID: &openstackID})
}

func (p *Projects) SetStatus(ctx context.Context, id uuid.UUID, status Status) error {
	return p.q.SetProjectStatus(ctx, db.SetProjectStatusParams{ID: id, Status: db.ProjectStatus(status)})
}

// StartDeletion marks a project deleting and returns the status to put back if the deletion
// is refused. A project still provisioning gets ErrBusy, unless it is stale: then the API
// stopped while creating it, and it counts as failed.
func (p *Projects) StartDeletion(ctx context.Context, id uuid.UUID, now time.Time) (Status, error) {
	var prev Status
	err := p.tx(ctx, func(q *db.Queries) error {
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

func (p *Projects) Delete(ctx context.Context, id uuid.UUID) error {
	return p.q.DeleteProject(ctx, id)
}

func project(r db.Project) Project {
	out := Project{ID: r.ID, TenantID: r.TenantID, Name: r.Name, Status: Status(r.Status), CreatedAt: r.CreatedAt}
	if r.OpenstackProjectID != nil {
		out.OpenStackProjectID = *r.OpenstackProjectID
	}
	return out
}
