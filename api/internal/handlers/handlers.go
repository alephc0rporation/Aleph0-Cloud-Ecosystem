// Package handlers implements the operations defined in api/openapi.yaml.
package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/alephc0rporation/Cardinal/api/internal/auth"
	"github.com/alephc0rporation/Cardinal/api/internal/config"
	"github.com/alephc0rporation/Cardinal/api/internal/oapi"
	"github.com/alephc0rporation/Cardinal/api/internal/openstack"
	"github.com/alephc0rporation/Cardinal/api/internal/problem"
	"github.com/alephc0rporation/Cardinal/api/internal/store"
)

type Handlers struct {
	// Ping checks the database connection.
	Ping     func(context.Context) error
	Tenants  Tenants
	Projects Projects
	Cloud    openstack.Cloud
	Regions  []config.Region
	Limits   Limits
}

type Limits struct{ TenantsPerUser, ProjectsPerTenant int }

// Tenants is what the handlers need from store.Tenants; tests pass a fake.
type Tenants interface {
	List(ctx context.Context, sub string) ([]store.Tenant, error)
	Create(ctx context.Context, name, sub string, limit int) (store.Tenant, error)
	Get(ctx context.Context, id uuid.UUID) (store.Tenant, error)
	Role(ctx context.Context, tenantID uuid.UUID, sub string) (string, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

// Projects is what the handlers need from store.Projects; tests pass a fake.
type Projects interface {
	List(ctx context.Context, tenantID uuid.UUID) ([]store.Project, error)
	Create(ctx context.Context, tenantID uuid.UUID, name string, limit int) (store.Project, error)
	Get(ctx context.Context, id uuid.UUID) (store.Project, error)
	SetOpenStackID(ctx context.Context, id uuid.UUID, openstackID string) error
	SetStatus(ctx context.Context, id uuid.UUID, s store.Status) error
	StartDeletion(ctx context.Context, id uuid.UUID, now time.Time) (store.Status, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

var _ oapi.StrictServerInterface = (*Handlers)(nil)

var errNotFound = problem.New(http.StatusNotFound, "")

func (h *Handlers) GetHealthz(ctx context.Context, _ oapi.GetHealthzRequestObject) (oapi.GetHealthzResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := h.Ping(ctx); err != nil {
		slog.WarnContext(ctx, "database ping failed", "error", err)
		return oapi.GetHealthz503JSONResponse{Status: "unavailable"}, nil
	}
	return oapi.GetHealthz200JSONResponse{Status: "ok"}, nil
}

func (h *Handlers) GetMe(ctx context.Context, _ oapi.GetMeRequestObject) (oapi.GetMeResponseObject, error) {
	c, ok := auth.FromContext(ctx)
	if !ok {
		return nil, errNoClaims
	}
	return oapi.GetMe200JSONResponse{Id: c.Subject, Email: c.Email, Name: c.Name}, nil
}

var errNoClaims = errors.New("no claims in context: /v1 route mounted without auth middleware")

func caller(ctx context.Context) (string, error) {
	c, ok := auth.FromContext(ctx)
	if !ok {
		return "", errNoClaims
	}
	return c.Subject, nil
}

// memberOf returns the caller's role in the tenant. A caller who is not a member gets 404, so
// the API never reveals that a tenant exists. Every tenant and project route goes through it.
func (h *Handlers) memberOf(ctx context.Context, tenantID uuid.UUID) (string, error) {
	sub, err := caller(ctx)
	if err != nil {
		return "", err
	}
	role, err := h.Tenants.Role(ctx, tenantID, sub)
	if err != nil {
		return "", err
	}
	if role == "" {
		return "", errNotFound
	}
	return role, nil
}
