package problem_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/problem"
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
