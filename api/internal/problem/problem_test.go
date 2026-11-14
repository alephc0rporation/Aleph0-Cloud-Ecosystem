package problem_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alephc0rporation/Cardinal/api/internal/problem"
)

func TestWrite(t *testing.T) {
	rec := httptest.NewRecorder()
	problem.Write(rec, http.StatusUnauthorized, "missing bearer token")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"type":   "about:blank",
		"title":  "Unauthorized",
		"status": float64(401),
		"detail": "missing bearer token",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestWriteOmitsEmptyDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	problem.Write(rec, http.StatusInternalServerError, "")

	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["detail"]; ok {
		t.Fatalf("detail present: %v", got)
	}
}

func TestErrorCarriesStatusAndDetail(t *testing.T) {
	var err error = problem.New(http.StatusConflict, "delete the tenant's projects first")
	var pe *problem.Error
	if !errors.As(err, &pe) || pe.Status != http.StatusConflict || pe.Detail != "delete the tenant's projects first" {
		t.Fatalf("got %#v", err)
	}
}
