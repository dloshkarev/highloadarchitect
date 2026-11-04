package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

type UserRepository struct {
	writePool *pgxpool.Pool
	readPool  *ReadPool
}

// NewUserRepository принимает writePool (мастер) для записей и чтений,
// которым нужны свежие данные, и readPool (реплики) для тяжёлых read-запросов.
func NewUserRepository(writePool *pgxpool.Pool, readPool *ReadPool) *UserRepository {
	return &UserRepository{writePool: writePool, readPool: readPool}
}

func (r *UserRepository) Create(ctx context.Context, user domain.User, passwordHash string) error {
	const query = `
		INSERT INTO users (
			id, first_name, second_name, birthdate, biography, city, password_hash
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	// Учебное упрощение: ретраим только временные ошибки, чтобы приложение
	// пережидало переключение лидера Patroni и не влияло на замер потерь
	// при репликации. duplicate key считаем успехом (идемпотентность:
	// первый insert закоммитился, но ответ клиенту потерялся).
	err := retry.Do(
		func() error {
			_, err := r.writePool.Exec(
				ctx,
				query,
				user.ID,
				user.FirstName,
				user.SecondName,
				user.Birthdate,
				user.Biography,
				user.City,
				passwordHash,
			)
			if isDuplicateKey(err) {
				return nil
			}

			return err
		},
		retry.Attempts(0),
		retry.Delay(100*time.Millisecond),
		retry.MaxDelay(2*time.Second),
		retry.DelayType(retry.BackOffDelay),
		retry.LastErrorOnly(true),
		retry.RetryIf(isTransientPGError),
		retry.Context(ctx),
	)
	if err != nil {
		return dbErr("insert user", err)
	}

	return nil
}

// isDuplicateKey — SQLSTATE 23505 (unique_violation).
func isDuplicateKey(err error) bool {
	var pgErr *pgconn.PgError

	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// isTransientPGError решает, стоит ли повторять операцию:
//   - сетевые ошибки и ошибки пула (не PgError) — ретраим;
//   - SQLSTATE 08xxx (обрыв соединения) — ретраим;
//   - 57P03 (cannot_connect_now: сервер в shutdown/recovery) — ретраим;
//   - 25006 (read_only_sql_transaction: запрос попал на реплику в окне
//     failover) — ретраим;
//   - остальные SQLSTATE — постоянные ошибки, ретраить бессмысленно.
func isTransientPGError(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return true
	}
	if strings.HasPrefix(pgErr.Code, "08") {
		return true
	}

	return pgErr.Code == "57P03" || pgErr.Code == "25006"
}

func (r *UserRepository) GetByID(ctx context.Context, userID string) (domain.User, error) {
	const query = `
		SELECT id, first_name, second_name, birthdate, biography, city
		FROM users
		WHERE id = $1
	`
	var user domain.User
	err := r.writePool.QueryRow(ctx, query, userID).Scan(
		&user.ID,
		&user.FirstName,
		&user.SecondName,
		&user.Birthdate,
		&user.Biography,
		&user.City,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, dbErr("select user", err)
	}

	return user, nil
}

func (r *UserRepository) GetCredentials(ctx context.Context, userID string) (domain.Credentials, error) {
	const query = `
		SELECT id, password_hash
		FROM users
		WHERE id = $1
	`
	var credentials domain.Credentials
	err := r.writePool.QueryRow(ctx, query, userID).Scan(&credentials.UserID, &credentials.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Credentials{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Credentials{}, dbErr("select credentials", err)
	}

	return credentials, nil
}

func (r *UserRepository) Search(
	ctx context.Context,
	firstNamePart string,
	secondNamePart string,
) ([]domain.User, error) {
	const query = `
		SELECT id, first_name, second_name, birthdate, biography, city
		FROM users
		WHERE first_name LIKE $1 AND second_name LIKE $2
		ORDER BY id
	`
	rows, err := r.readPool.Next().Query(ctx, query, likePrefix(firstNamePart), likePrefix(secondNamePart))
	if err != nil {
		return nil, dbErr("cannot do users search", err)
	}
	defer rows.Close()

	result := make([]domain.User, 0)
	for rows.Next() {
		var user domain.User
		if err := rows.Scan(
			&user.ID,
			&user.FirstName,
			&user.SecondName,
			&user.Birthdate,
			&user.Biography,
			&user.City,
		); err != nil {
			return nil, dbErr("cannot scan users search results", err)
		}
		result = append(result, user)
	}
	if err := rows.Err(); err != nil {
		return nil, dbErr("cannot read users search results", err)
	}

	return result, nil
}

func likePrefix(part string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(part)

	return escaped + "%"
}

func dbErr(operation string, err error) error {
	return fmt.Errorf("%s: %w", operation, &domain.DBError{Err: err})
}
