package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/alephc0rporation/Cardinal/api/internal/oapi"
	"github.com/alephc0rporation/Cardinal/api/internal/problem"
	"github.com/alephc0rporation/Cardinal/api/internal/store"
)

func (h *Handlers) ListRegions(_ context.Context, _ oapi.ListRegionsRequestObject) (oapi.ListRegionsResponseObject, error) {
	out := oapi.ListRegions200JSONResponse{}
	for _, r := range h.Regions {
		out = append(out, oapi.Region{Id: r.ID, Name: r.Name})
	}
	return out, nil
}

func (h *Handlers) ListTenants(ctx context.Context, _ oapi.ListTenantsRequestObject) (oapi.ListTenantsResponseObject, error) {
	sub, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	ts, err := h.Tenants.List(ctx, sub)
	if err != nil {
		return nil, err
	}
	out := oapi.ListTenants200JSONResponse{}
	for _, t := range ts {
		out = append(out, tenantJSON(t))
	}
	return out, nil
}

func (h *Handlers) CreateTenant(ctx context.Context, req oapi.CreateTenantRequestObject) (oapi.CreateTenantResponseObject, error) {
	sub, err := caller(ctx)
	if err != nil {
		return nil, err
	}
	t, err := h.Tenants.Create(ctx, req.Body.Name, sub, h.Limits.TenantsPerUser)
	if errors.Is(err, store.ErrLimit) {
		return nil, problem.New(http.StatusForbidden, fmt.Sprintf("you can create at most %d tenants", h.Limits.TenantsPerUser))
	}
	if err != nil {
		return nil, err
	}
	return oapi.CreateTenant201JSONResponse(tenantJSON(t)), nil
}

func (h *Handlers) GetTenant(ctx context.Context, req oapi.GetTenantRequestObject) (oapi.GetTenantResponseObject, error) {
	role, err := h.memberOf(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	t, err := h.Tenants.Get(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	t.Role = role
	return oapi.GetTenant200JSONResponse(tenantJSON(t)), nil
}

func (h *Handlers) DeleteTenant(ctx context.Context, req oapi.DeleteTenantRequestObject) (oapi.DeleteTenantResponseObject, error) {
	role, err := h.memberOf(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	if role != "owner" {
		return nil, problem.New(http.StatusForbidden, "only an owner can delete the tenant")
	}
	switch err := h.Tenants.Delete(ctx, req.TenantId); {
	case errors.Is(err, store.ErrNotEmpty):
		return nil, problem.New(http.StatusConflict, "delete the tenant's projects first")
	case errors.Is(err, store.ErrNotFound):
		return nil, errNotFound
	case err != nil:
		return nil, err
	}
	return oapi.DeleteTenant204Response{}, nil
}

func tenantJSON(t store.Tenant) oapi.Tenant {
	return oapi.Tenant{Id: t.ID, Name: t.Name, Role: oapi.TenantRole(t.Role), CreatedAt: t.CreatedAt}
}

// Placeholders until Task 6 implements the project endpoints.

func (h *Handlers) ListProjects(context.Context, oapi.ListProjectsRequestObject) (oapi.ListProjectsResponseObject, error) {
	return nil, errors.New("not implemented")
}

func (h *Handlers) CreateProject(context.Context, oapi.CreateProjectRequestObject) (oapi.CreateProjectResponseObject, error) {
	return nil, errors.New("not implemented")
}

func (h *Handlers) GetProject(context.Context, oapi.GetProjectRequestObject) (oapi.GetProjectResponseObject, error) {
	return nil, errors.New("not implemented")
}

func (h *Handlers) DeleteProject(context.Context, oapi.DeleteProjectRequestObject) (oapi.DeleteProjectResponseObject, error) {
	return nil, errors.New("not implemented")
}
