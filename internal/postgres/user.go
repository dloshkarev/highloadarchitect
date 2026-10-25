package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{pool: pool}
}

func (r *UserRepository) Create(ctx context.Context, user domain.User, passwordHash string) error {
	const query = `
		INSERT INTO users (
			id, first_name, second_name, birthdate, biography, city, password_hash
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err := r.pool.Exec(
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
	if err != nil {
		return dbErr("insert user", err)
	}

	return nil
}

func (r *UserRepository) GetByID(ctx context.Context, userID string) (domain.User, error) {
	const query = `
		SELECT id, first_name, second_name, birthdate, biography, city
		FROM users
		WHERE id = $1
	`
	var user domain.User
	err := r.pool.QueryRow(ctx, query, userID).Scan(
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
	err := r.pool.QueryRow(ctx, query, userID).Scan(&credentials.UserID, &credentials.PasswordHash)
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
	rows, err := r.pool.Query(ctx, query, likePrefix(firstNamePart), likePrefix(secondNamePart))
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
