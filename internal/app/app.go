package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dloshkarev/highloadarchitect/internal/api"
	"github.com/dloshkarev/highloadarchitect/internal/config"
	"github.com/dloshkarev/highloadarchitect/internal/postgres"
	"github.com/dloshkarev/highloadarchitect/internal/service"
)

func Run(ctx context.Context, cfg config.Config) error {
	writePool, err := postgres.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return fmt.Errorf("create postgres write pool: %w", err)
	}
	defer writePool.Close()

	if err := postgres.Migrate(ctx, writePool); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	readPool, closeReadPool, err := makeReadPool(ctx, cfg.Postgres, writePool)
	if err != nil {
		return fmt.Errorf("create postgres read pool: %w", err)
	}
	defer closeReadPool()

	users := postgres.NewUserRepository(writePool, readPool)
	sessions := postgres.NewSessionRepository(writePool)
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

// makeReadPool собирает read-пулы реплик. Без настроенных реплик чтение идёт на writePool.
// Возвращённая функция закрывает только пулы реплик, writePool закрывает вызывающий.
func makeReadPool(
	ctx context.Context,
	cfg config.PostgresConfig,
	writePool *pgxpool.Pool,
) (*postgres.ReadPool, func(), error) {
	addresses, err := cfg.ReadAddresses()
	if err != nil {
		return nil, nil, fmt.Errorf("parse read hosts: %w", err)
	}

	if len(addresses) == 0 {
		return postgres.NewReadPool([]*pgxpool.Pool{writePool}), func() {}, nil
	}

	pools := make([]*pgxpool.Pool, 0, len(addresses))
	closeAll := func() {
		for _, pool := range pools {
			pool.Close()
		}
	}

	for _, address := range addresses {
		pool, err := postgres.NewPool(ctx, cfg.WithAddress(address))
		if err != nil {
			closeAll()

			return nil, nil, fmt.Errorf("create read pool %s: %w", address, err)
		}
		pools = append(pools, pool)
		slog.Info("postgres read replica configured", "addr", address)
	}

	return postgres.NewReadPool(pools), closeAll, nil
}
