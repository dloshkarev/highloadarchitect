package postgres

import (
	"context"

	"github.com/avast/retry-go/v4"
)

// executeWrite повторяет запись при временных ошибках (например, при
// переключении лидера Patroni) и оборачивает итоговую ошибку в domain.DBError.
func executeWrite(ctx context.Context, operation string, fn func() error) error {
	err := retry.Do(
		fn,
		retry.Attempts(0),
		retry.Delay(retryDelay),
		retry.MaxDelay(retryMaxDelay),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.RetryIf(isTransientPGError),
		retry.Context(ctx),
	)
	if err != nil {
		return dbErr(operation, err)
	}

	return nil
}
