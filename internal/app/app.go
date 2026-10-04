package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/dloshkarev/highloadarchitect/internal/api"
	"github.com/dloshkarev/highloadarchitect/internal/config"
	"github.com/dloshkarev/highloadarchitect/internal/postgres"
	"github.com/dloshkarev/highloadarchitect/internal/service"
)

func Run(ctx context.Context, cfg config.Config) error {
	pool, err := postgres.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return fmt.Errorf("create postgres pool: %w", err)
	}
	defer pool.Close()

	if err := postgres.Migrate(ctx, pool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	users := postgres.NewUserRepository(pool)
	sessions := postgres.NewSessionRepository(pool)
	router := api.NewRouter(
		service.NewUserService(users),
		service.NewAuthService(users, sessions),
	)

	addr := ":" + cfg.HTTPServer.Port
	server := &http.Server{
		Addr:              addr,
		Handler:           http.MaxBytesHandler(router, cfg.HTTPServer.MaxBodyBytes),
		ReadHeaderTimeout: cfg.HTTPServer.Timeout,
		ReadTimeout:       cfg.HTTPServer.Timeout,
		WriteTimeout:      cfg.HTTPServer.Timeout,
		IdleTimeout:       cfg.HTTPServer.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("http server started", "addr", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		return shutdownHTTPServer(ctx, server, cfg.ShutdownTimeout)
	case err := <-errCh:
		return err
	}
}

func shutdownHTTPServer(ctx context.Context, server *http.Server, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}

	return nil
}
