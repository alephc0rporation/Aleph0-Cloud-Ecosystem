package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

const testSpec = `
openapi: 3.0.3
info: {title: test, version: "0"}
security:
  - bearerAuth: []
paths:
  /v1/things:
    post:
      parameters:
        - {name: limit, in: query, schema: {type: integer, maximum: 10}}
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name]
              properties:
                name: {type: string, minLength: 1}
      responses:
        "204": {description: created}
components:
  securitySchemes:
    bearerAuth: {type: http, scheme: bearer}
`

func TestValidateRequests(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromData([]byte(testSpec))
	if err != nil {
		t.Fatal(err)
	}
	var gotBody string
	h := validateRequests(spec)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))

	tests := []struct {
		name, target, body string
		want               int
		detail             string
	}{
		{"valid", "/v1/things?limit=5", `{"name":"x"}`, http.StatusNoContent, ""},
		{"body violates schema", "/v1/things", `{"name":""}`, http.StatusBadRequest, "minimum string length is 1"},
		{"missing required field", "/v1/things", `{}`, http.StatusBadRequest, `property "name" is missing`},
		{"query over maximum", "/v1/things?limit=11", `{"name":"x"}`, http.StatusBadRequest, "limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No Authorization header: authentication is requireAuth's job, not the validator's.
			req := httptest.NewRequest(http.MethodPost, tt.target, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.want, rec.Body)
			}
			if tt.want == http.StatusNoContent {
				if gotBody != tt.body {
					t.Fatalf("handler saw body %q, want %q", gotBody, tt.body)
				}
				return
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Fatalf("Content-Type = %q", ct)
			}
			var p struct{ Detail string }
			if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(p.Detail, tt.detail) || strings.Contains(p.Detail, "\n") {
				t.Fatalf("detail = %q, want one line containing %q", p.Detail, tt.detail)
			}
		})
	}
}

func TestLimitBody(t *testing.T) {
	spec, err := openapi3.NewLoader().LoadFromData([]byte(testSpec))
	if err != nil {
		t.Fatal(err)
	}
	var called bool
	h := limitBody(16)(validateRequests(spec)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})))
	big := `{"name":"` + strings.Repeat("x", 100) + `"}`

	tests := []struct {
		name, body    string
		contentLength int64
		want          int
	}{
		{"under the limit", `{"name":"x"}`, -1, http.StatusNoContent},
		{"Content-Length over the limit", big, int64(len(big)), http.StatusRequestEntityTooLarge},
		// Unknown length (chunked): the limit trips while the validator reads the body.
		{"unknown length over the limit", big, -1, http.StatusRequestEntityTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(http.MethodPost, "/v1/things", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req.ContentLength = tt.contentLength
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tt.want, rec.Body)
			}
			if tt.want == http.StatusNoContent {
				return
			}
			if called {
				t.Fatal("handler ran for an oversized body")
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
				t.Fatalf("Content-Type = %q", ct)
			}
			if !strings.Contains(rec.Body.String(), "larger than 16 bytes") {
				t.Fatalf("body = %s", rec.Body)
			}
		})
	}
}
