package server

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/problem"
)

// validateRequests checks parameters and bodies against spec, so constraints like
// minLength, pattern or maximum are enforced before a handler runs.
// It panics if spec cannot be turned into a router.
func validateRequests(spec *openapi3.T) func(http.Handler) http.Handler {
	return nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			// requireAuth has already verified the token; only the request shape is checked here.
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
		DoNotValidateServers: true,
		ErrorHandlerWithOpts: func(ctx context.Context, err error, w http.ResponseWriter, r *http.Request, opts nethttpmiddleware.ErrorHandlerOpts) {
			if writeTooLarge(w, err) {
				return
			}
			if opts.StatusCode >= http.StatusInternalServerError {
				slog.ErrorContext(ctx, "request validation failed", "error", err)
				problem.Write(w, opts.StatusCode, "")
				return
			}
			// The first line names the failing field; the rest dumps the schema.
			detail, _, _ := strings.Cut(err.Error(), "\n")
			problem.Write(w, opts.StatusCode, detail)
		},
	})
}
