// Command api is the Aleph0 Cardinal backend.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alephc0rporation/Cardinal/api/internal/auth"
	"github.com/alephc0rporation/Cardinal/api/internal/config"
	"github.com/alephc0rporation/Cardinal/api/internal/handlers"
	"github.com/alephc0rporation/Cardinal/api/internal/server"
	"github.com/alephc0rporation/Cardinal/api/internal/store"
)

const usage = "usage: api serve|migrate"

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	slog.SetDefault(server.NewLogger(os.Stdout))
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "migrate":
		err = migrate()
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

// migrate applies pending migrations. It needs only DATABASE_URL.
func migrate() error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("missing required environment variables: DATABASE_URL")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return store.Migrate(ctx, url)
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
