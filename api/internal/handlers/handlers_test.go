package handlers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/auth"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/handlers"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/oapi"
)

func TestGetHealthz(t *testing.T) {
	ok := &handlers.Handlers{Ping: func(context.Context) error { return nil }}
	res, err := ok.GetHealthz(context.Background(), oapi.GetHealthzRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if got, isOK := res.(oapi.GetHealthz200JSONResponse); !isOK || got.Status != "ok" {
		t.Fatalf("got %#v, want 200 ok", res)
	}

	down := &handlers.Handlers{Ping: func(context.Context) error { return errors.New("connection refused") }}
	res, err = down.GetHealthz(context.Background(), oapi.GetHealthzRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	if got, isDown := res.(oapi.GetHealthz503JSONResponse); !isDown || got.Status != "unavailable" {
		t.Fatalf("got %#v, want 503 unavailable", res)
	}
}

func TestGetHealthzBoundsThePing(t *testing.T) {
	h := &handlers.Handlers{Ping: func(ctx context.Context) error {
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			t.Error("ping context has no deadline")
		}
		return nil
	}}
	if _, err := h.GetHealthz(context.Background(), oapi.GetHealthzRequestObject{}); err != nil {
		t.Fatal(err)
	}
}

func TestGetMe(t *testing.T) {
	h := &handlers.Handlers{}
	ctx := auth.WithClaims(context.Background(), auth.Claims{Subject: "user-1", Email: "dev@aleph0.local", Name: "Dev User"})

	res, err := h.GetMe(ctx, oapi.GetMeRequestObject{})
	if err != nil {
		t.Fatal(err)
	}
	want := oapi.GetMe200JSONResponse{Id: "user-1", Email: "dev@aleph0.local", Name: "Dev User"}
	if res != want {
		t.Fatalf("got %#v, want %#v", res, want)
	}

	if _, err := h.GetMe(context.Background(), oapi.GetMeRequestObject{}); err == nil {
		t.Fatal("want error without claims in context")
	}
}
