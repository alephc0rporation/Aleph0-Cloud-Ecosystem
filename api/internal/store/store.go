// Package store keeps tenants, memberships and projects in PostgreSQL, one repository per
// aggregate: Tenants (a tenant and its memberships) and Projects.
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

// conn is what both repositories share: the pool, the generated queries and transactions.
type conn struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

func newConn(pool *pgxpool.Pool) conn { return conn{pool: pool, q: db.New(pool)} }

func (c conn) tx(ctx context.Context, fn func(*db.Queries) error) error {
	return pgx.BeginFunc(ctx, c.pool, func(tx pgx.Tx) error { return fn(c.q.WithTx(tx)) })
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
