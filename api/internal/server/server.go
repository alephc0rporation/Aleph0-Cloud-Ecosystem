// Package server wires the HTTP stack: routing, middleware and the http.Server lifecycle.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/alephc0rporation/Cardinal/api/internal/oapi"
	"github.com/alephc0rporation/Cardinal/api/internal/problem"
)

// Handler builds the root handler. Everything under /v1/ goes through requireAuth,
// so a new /v1 endpoint is protected unless it is deliberately mounted elsewhere.
// It panics if the embedded OpenAPI spec cannot be loaded, which only a broken build causes.
func Handler(api oapi.StrictServerInterface, requireAuth func(http.Handler) http.Handler) http.Handler {
	spec, err := oapi.GetSwagger()
	if err != nil {
		panic(fmt.Sprintf("load embedded OpenAPI spec: %v", err))
	}
	badRequest := func(w http.ResponseWriter, r *http.Request, err error) {
		if writeTooLarge(w, err) {
			return
		}
		problem.Write(w, http.StatusBadRequest, err.Error())
	}
	// The generated routes are added to base, so unknown paths and methods
	// get a problem+json 404 instead of net/http's plain-text answers.
	base := http.NewServeMux()
	base.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		problem.Write(w, http.StatusNotFound, "")
	})
	generated := oapi.HandlerWithOptions(
		oapi.NewStrictHandlerWithOptions(api, nil, oapi.StrictHTTPServerOptions{
			RequestErrorHandlerFunc: badRequest,
			ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				var pe *problem.Error
				if errors.As(err, &pe) {
					problem.Write(w, pe.Status, pe.Detail)
					return
				}
				slog.ErrorContext(r.Context(), "handler failed", "error", err)
				problem.Write(w, http.StatusInternalServerError, "")
			},
		}),
		// Validation runs only on routes in the spec; anything else falls through to base's 404.
		oapi.StdHTTPServerOptions{
			BaseRouter:       base,
			Middlewares:      []oapi.MiddlewareFunc{validateRequests(spec)},
			ErrorHandlerFunc: badRequest,
		},
	)

	mux := http.NewServeMux()
	mux.Handle("/v1/", requireAuth(generated))
	mux.Handle("/", generated)
	return requestID(accessLog(recoverPanic(limitBody(maxBodyBytes)(mux))))
}

// Run serves h on addr until ctx is cancelled, then gives in-flight requests 10 s to finish.
func Run(ctx context.Context, addr string, h http.Handler) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
