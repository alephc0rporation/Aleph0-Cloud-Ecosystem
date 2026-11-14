package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/alephc0rporation/Cardinal/api/internal/oapi"
	"github.com/alephc0rporation/Cardinal/api/internal/openstack"
	"github.com/alephc0rporation/Cardinal/api/internal/problem"
	"github.com/alephc0rporation/Cardinal/api/internal/store"
)

// A whole project create or delete fits under the server's 30 s write timeout.
const provisioningBudget = 25 * time.Second

func (h *Handlers) ListProjects(ctx context.Context, req oapi.ListProjectsRequestObject) (oapi.ListProjectsResponseObject, error) {
	if _, err := h.memberOf(ctx, req.TenantId); err != nil {
		return nil, err
	}
	ps, err := h.Projects.List(ctx, req.TenantId)
	if err != nil {
		return nil, err
	}
	out := oapi.ListProjects200JSONResponse{}
	for _, p := range ps {
		out = append(out, projectJSON(p))
	}
	return out, nil
}

func (h *Handlers) CreateProject(ctx context.Context, req oapi.CreateProjectRequestObject) (oapi.CreateProjectResponseObject, error) {
	if _, err := h.memberOf(ctx, req.TenantId); err != nil {
		return nil, err
	}
	p, err := h.Projects.Create(ctx, req.TenantId, req.Body.Name, h.Limits.ProjectsPerTenant)
	switch {
	case errors.Is(err, store.ErrLimit):
		return nil, problem.New(http.StatusForbidden, fmt.Sprintf("a tenant can have at most %d projects", h.Limits.ProjectsPerTenant))
	case errors.Is(err, store.ErrDuplicate):
		return nil, problem.New(http.StatusConflict, "a project with this name already exists")
	case errors.Is(err, store.ErrNotFound):
		return nil, errNotFound
	case err != nil:
		return nil, err
	}
	if err := h.provision(ctx, &p); err != nil {
		return nil, err
	}
	return oapi.CreateProject201JSONResponse(projectJSON(p)), nil
}

// provision creates the project's OpenStack project and its default network in every region.
// On failure the project is left failed; the user deletes it and tries again.
func (h *Handlers) provision(ctx context.Context, p *store.Project) error {
	ctx, cancel := context.WithTimeout(ctx, provisioningBudget)
	defer cancel()
	osID, err := h.Cloud.CreateProject(ctx, p.ID.String(), p.TenantID.String(), p.Name)
	if err != nil {
		return h.failed(ctx, p.ID, err)
	}
	if err := h.Projects.SetOpenStackID(ctx, p.ID, osID); err != nil {
		return err
	}
	p.OpenStackProjectID = osID
	for _, r := range h.Regions {
		if err := h.Cloud.EnsureDefaultNetwork(ctx, r.ID, osID); err != nil {
			return h.failed(ctx, p.ID, err)
		}
	}
	if err := h.Projects.SetStatus(ctx, p.ID, store.StatusReady); err != nil {
		return err
	}
	p.Status = store.StatusReady
	return nil
}

// failed marks the project failed, even when the budget is spent, and answers 502 or 503.
func (h *Handlers) failed(ctx context.Context, id uuid.UUID, err error) error {
	if serr := h.Projects.SetStatus(context.WithoutCancel(ctx), id, store.StatusFailed); serr != nil {
		return serr
	}
	return cloudError(ctx, "creating the project failed", id, err)
}

// cloudError logs an OpenStack error with the request ID and turns it into a 503 when OpenStack
// did not answer, or a 502 when it answered with an error. Its details never reach the client.
func cloudError(ctx context.Context, msg string, project uuid.UUID, err error) error {
	slog.ErrorContext(ctx, msg, "project", project, "error", err)
	if openstack.IsUnavailable(err) {
		return problem.New(http.StatusServiceUnavailable, "the region did not answer; try again later")
	}
	return problem.New(http.StatusBadGateway, "the cloud could not complete the request")
}

func (h *Handlers) GetProject(ctx context.Context, req oapi.GetProjectRequestObject) (oapi.GetProjectResponseObject, error) {
	p, err := h.project(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	return oapi.GetProject200JSONResponse(projectJSON(p)), nil
}

// project loads a project for a caller who is a member of its tenant; anyone else gets 404.
// The OpenStack project used afterwards comes from this row, never from the request.
func (h *Handlers) project(ctx context.Context, id uuid.UUID) (store.Project, error) {
	p, err := h.Projects.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return store.Project{}, errNotFound
	}
	if err != nil {
		return store.Project{}, err
	}
	if _, err := h.memberOf(ctx, p.TenantID); err != nil {
		return store.Project{}, err
	}
	return p, nil
}

func (h *Handlers) DeleteProject(ctx context.Context, req oapi.DeleteProjectRequestObject) (oapi.DeleteProjectResponseObject, error) {
	p, err := h.project(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	prev, err := h.Projects.StartDeletion(ctx, p.ID, time.Now())
	switch {
	case errors.Is(err, store.ErrBusy):
		return nil, problem.New(http.StatusConflict, "the project is still being created; try again in a minute")
	case errors.Is(err, store.ErrNotFound):
		return nil, errNotFound
	case err != nil:
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, provisioningBudget)
	defer cancel()
	if p.OpenStackProjectID != "" {
		for _, r := range h.Regions {
			n, err := h.Cloud.CountServers(ctx, r.ID, p.OpenStackProjectID)
			if err != nil || n > 0 {
				// Nothing is removed yet: the project goes back to how it was.
				if serr := h.Projects.SetStatus(context.WithoutCancel(ctx), p.ID, prev); serr != nil {
					return nil, serr
				}
				if err != nil {
					return nil, cloudError(ctx, "counting the project's servers failed", p.ID, err)
				}
				return nil, problem.New(http.StatusConflict, "delete the project's VMs first")
			}
		}
		for _, r := range h.Regions {
			if err := h.Cloud.DeleteDefaultNetwork(ctx, r.ID, p.OpenStackProjectID); err != nil {
				return nil, cloudError(ctx, "removing the project's network failed", p.ID, err)
			}
		}
	}
	// By name, so an OpenStack project whose ID was never saved is removed too.
	if err := h.Cloud.DeleteProject(ctx, p.ID.String()); err != nil {
		return nil, cloudError(ctx, "removing the OpenStack project failed", p.ID, err)
	}
	if err := h.Projects.Delete(ctx, p.ID); err != nil {
		return nil, err
	}
	return oapi.DeleteProject204Response{}, nil
}

func projectJSON(p store.Project) oapi.Project {
	return oapi.Project{Id: p.ID, TenantId: p.TenantID, Name: p.Name, Status: oapi.ProjectStatus(p.Status), CreatedAt: p.CreatedAt}
}
