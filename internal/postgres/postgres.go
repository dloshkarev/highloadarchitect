package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/dloshkarev/highloadarchitect/internal/config"
	"github.com/dloshkarev/highloadarchitect/migrations"
)

const pingTimeout = 5 * time.Second

func NewPool(ctx context.Context, postgresConfig config.PostgresConfig) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(postgresConfig.DSN())
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	cfg.MaxConns = postgresConfig.MaxConns
	cfg.MaxConnLifetime = postgresConfig.MaxConnLifetime
	cfg.MaxConnIdleTime = postgresConfig.MaxConnIdleTime

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}

// Migrate применяет миграции из embed. При seed=false пропускаются затравочные
// миграции (*_data.sql): таблицы создаются пустыми. Полный запуск приложения
// мигрирует всё, интеграционные тесты стартуют со схемой без данных.
func Migrate(ctx context.Context, pool *pgxpool.Pool, seed bool) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	source := fs.FS(migrations.FS)
	if !seed {
		schemaOnly, err := migrations.SchemaOnly()
		if err != nil {
			return fmt.Errorf("schema migrations: %w", err)
		}
		source = schemaOnly
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, source)
	if err != nil {
		return fmt.Errorf("goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	return nil
}
