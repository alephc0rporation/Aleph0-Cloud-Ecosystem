package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/auth"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/handlers"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/server"
)

var claims = auth.Claims{Subject: "user-1", Email: "dev@aleph0.local", Name: "Dev User"}

func allow(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), claims)))
	})
}

func deny(http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
}

func pingOK(context.Context) error { return nil }

func do(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestHealthzSkipsAuth(t *testing.T) {
	rec := do(t, server.Handler(&handlers.Handlers{Ping: pingOK}, deny), "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("body = %s", rec.Body)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id")
	}
}

func TestV1RequiresAuth(t *testing.T) {
	h := server.Handler(&handlers.Handlers{Ping: pingOK}, deny)
	for _, path := range []string{"/v1/me", "/v1/does-not-exist"} {
		if rec := do(t, h, path); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", path, rec.Code)
		}
	}
}

func TestMe(t *testing.T) {
	rec := do(t, server.Handler(&handlers.Handlers{Ping: pingOK}, allow), "/v1/me")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["id"] != "user-1" || got["email"] != "dev@aleph0.local" || got["name"] != "Dev User" {
		t.Fatalf("body = %v", got)
	}
}

func TestUnknownPathIsProblem404(t *testing.T) {
	h := server.Handler(&handlers.Handlers{Ping: pingOK}, allow)
	for _, path := range []string{"/nope", "/v1/does-not-exist"} {
		rec := do(t, h, path)
		if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%s: status = %d, Content-Type = %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}

func TestPanicIsProblem500AndLoggedWithRequestID(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(server.NewLogger(&logs))
	t.Cleanup(func() { slog.SetDefault(prev) })

	boom := &handlers.Handlers{Ping: func(context.Context) error { panic("boom") }}
	rec := do(t, server.Handler(boom, allow), "/healthz")

	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("status = %d, Content-Type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	id := rec.Header().Get("X-Request-Id")
	var sawPanic, sawAccess bool
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %s", line)
		}
		if entry["request_id"] != id {
			t.Errorf("log line without request_id %s: %s", id, line)
		}
		switch entry["msg"] {
		case "panic":
			sawPanic = true
		case "request":
			sawAccess = entry["status"] == float64(500)
		}
	}
	if !sawPanic || !sawAccess {
		t.Fatalf("panic logged = %v, access log with 500 = %v; logs:\n%s", sawPanic, sawAccess, logs.String())
	}
}
