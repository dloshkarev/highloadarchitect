package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/dloshkarev/highloadarchitect/internal/api"
	"github.com/dloshkarev/highloadarchitect/internal/config"
	"github.com/dloshkarev/highloadarchitect/internal/kafka"
	"github.com/dloshkarev/highloadarchitect/internal/outbox"
	"github.com/dloshkarev/highloadarchitect/internal/postgres"
	"github.com/dloshkarev/highloadarchitect/internal/redis"
	"github.com/dloshkarev/highloadarchitect/internal/service"
	"github.com/dloshkarev/highloadarchitect/internal/service/feed"
)

func Run(ctx context.Context, cfg config.Config) error {
	writePool, err := postgres.NewPool(ctx, cfg.Postgres)
	if err != nil {
		return fmt.Errorf("create postgres write pool: %w", err)
	}
	defer writePool.Close()

	if err := postgres.Migrate(ctx, writePool, cfg.Postgres.MigrateSeed); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	readPool, closeReadPool, err := makeReadPool(ctx, cfg.Postgres, writePool)
	if err != nil {
		return fmt.Errorf("create postgres read pool: %w", err)
	}
	defer closeReadPool()

	// Redis недоступен — падаем на старте (fail fast): без кэша не работают
	// ни чтение ленты, ни фоновые записи outbox-воркера.
	feedCache, err := redis.Open(ctx, cfg.Redis, cfg.Feed)
	if err != nil {
		return fmt.Errorf("open redis: %w", err)
	}
	defer feedCache.Close()

	users := postgres.NewUserRepository(writePool, readPool)
	sessions := postgres.NewSessionRepository(writePool)
	friends := postgres.NewFriendRepository(writePool)
	posts := postgres.NewPostRepository(writePool)
	outboxRepo := postgres.NewOutboxRepository(writePool)

	producer, err := kafka.NewProducer(cfg.Kafka)
	if err != nil {
		return fmt.Errorf("create kafka producer: %w", err)
	}
	defer producer.Close()

	consumer, err := kafka.NewConsumer(ctx, cfg.Kafka, feed.NewSeeder(friends, feedCache))
	if err != nil {
		return fmt.Errorf("create kafka consumer: %w", err)
	}
	defer consumer.Close()

	worker := outbox.NewWorker(outboxRepo, producer, feedCache, cfg.Outbox)

	router := api.NewRouter(
		service.NewUserService(users),
		service.NewAuthService(users, sessions),
		service.NewFriendService(friends),
		service.NewPostService(posts, feed.New(feedCache, posts, cfg.Feed)),
	)

	server := newHTTPServer(cfg.HTTPServer, router)

	return serve(ctx, cfg.ShutdownTimeout, server, worker, consumer)
}

// serve запускает HTTP-сервер и фоновые процессы в одном errgroup: ошибка
// любого компонента отменяет общий контекст и останавливает остальные.
func serve(
	ctx context.Context,
	shutdownTimeout time.Duration,
	server *http.Server,
	worker *outbox.Worker,
	consumer *kafka.Consumer,
) error {
	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		<-gCtx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(gCtx), shutdownTimeout)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown http server: %w", err)
		}

		return nil
	})

	g.Go(func() error {
		slog.Info("http server started", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}

		return nil
	})

	g.Go(func() error {
		slog.Info("outbox worker started")
		defer slog.Info("outbox worker stopped")

		worker.Run(gCtx)

		return nil
	})

	g.Go(func() error {
		slog.Info("kafka consumer started")
		defer slog.Info("kafka consumer stopped")

		consumer.Run(gCtx)

		return nil
	})

	slog.Info("application started")
	if err := g.Wait(); err != nil {
		return fmt.Errorf("application: %w", err)
	}
	slog.Info("application stopped")

	return nil
}

func newHTTPServer(cfg config.HTTPServerConfig, router http.Handler) *http.Server {
	return &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           http.MaxBytesHandler(router, cfg.MaxBodyBytes),
		ReadHeaderTimeout: cfg.Timeout,
		ReadTimeout:       cfg.Timeout,
		WriteTimeout:      cfg.Timeout,
		IdleTimeout:       cfg.IdleTimeout,
	}
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
