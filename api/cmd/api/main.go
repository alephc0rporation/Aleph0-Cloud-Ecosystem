// Command api is the Aleph0 Cloud backend.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/auth"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/config"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/handlers"
	"github.com/alephc0rporation/Aleph0-Cloud-Ecosystem/api/internal/server"
)

const usage = "usage: api serve"

func main() {
	if len(os.Args) != 2 || os.Args[1] != "serve" {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	slog.SetDefault(server.NewLogger(os.Stdout))
	if err := serve(); err != nil {
		slog.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func serve() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// The pool connects lazily: the API starts while the DB is down and /healthz reports it.
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database config: %w", err)
	}
	defer pool.Close()

	verifier := auth.NewKeycloakVerifier(cfg.OIDCIssuer, cfg.OIDCAudience)
	h := server.Handler(&handlers.Handlers{Ping: pool.Ping}, verifier.Middleware)

	slog.Info("listening", "addr", cfg.HTTPAddr)
	return server.Run(ctx, cfg.HTTPAddr, h)
}
