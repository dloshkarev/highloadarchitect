package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

// OutboxRepository читает и удаляет события transactional outbox.
type OutboxRepository struct {
	pool *pgxpool.Pool
}

func NewOutboxRepository(pool *pgxpool.Pool) *OutboxRepository {
	return &OutboxRepository{pool: pool}
}

// GetBatch возвращает до limit событий в порядке возрастания id.
func (r *OutboxRepository) GetBatch(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	const query = `
		SELECT id, event_type, payload, created_at
		FROM outbox
		ORDER BY id
		LIMIT $1
	`
	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, dbErr("select outbox batch", err)
	}
	defer rows.Close()

	events := make([]domain.OutboxEvent, 0, limit)
	for rows.Next() {
		var event domain.OutboxEvent
		if err := rows.Scan(&event.ID, &event.Type, &event.Payload, &event.CreatedAt); err != nil {
			return nil, dbErr("scan outbox event", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, dbErr("iterate outbox batch", err)
	}

	return events, nil
}

// Delete удаляет обработанное событие. Повторное удаление безопасно.
func (r *OutboxRepository) Delete(ctx context.Context, id int64) error {
	const query = `DELETE FROM outbox WHERE id = $1`

	return executeWrite(ctx, "delete outbox event", func() error {
		if _, err := r.pool.Exec(ctx, query, id); err != nil {
			return fmt.Errorf("exec query: %w", err)
		}

		return nil
	})
}

// Count возвращает число необработанных событий: показывает отставание
// воркера, когда события копятся быстрее обработки.
func (r *OutboxRepository) Count(ctx context.Context) (int64, error) {
	const query = `SELECT count(*) FROM outbox`

	var count int64
	if err := r.pool.QueryRow(ctx, query).Scan(&count); err != nil {
		return 0, dbErr("count outbox events", err)
	}

	return count, nil
}
