// Package handlers implements the operations defined in api/openapi.yaml.
package handlers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/auth"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/oapi"
)

type Handlers struct {
	// Ping checks the database connection.
	Ping func(context.Context) error
}

var _ oapi.StrictServerInterface = (*Handlers)(nil)

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
		return nil, errors.New("no claims in context: /v1 route mounted without auth middleware")
	}
	return oapi.GetMe200JSONResponse{Id: c.Subject, Email: c.Email, Name: c.Name}, nil
}
