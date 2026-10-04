//go:build integration

package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/dloshkarev/highloadarchitect/internal/app"
	"github.com/dloshkarev/highloadarchitect/internal/config"
)

const (
	dbUser     = "social"
	dbPassword = "social"
	dbName     = "social"
)

var (
	baseURL     string
	postgresDSN string
	httpClient  = &http.Client{Timeout: 10 * time.Second}
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	container, err := startPostgres(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres: %v\n", err)

		return 1
	}
	defer terminatePostgres(container)

	cfg, err := appConfig(ctx, container)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)

		return 1
	}

	appErr := make(chan error, 1)
	go func() {
		appErr <- app.Run(ctx, cfg)
	}()
	defer func() {
		cancel()
		if waitErr := waitErr(appErr, 15*time.Second); waitErr != nil {
			fmt.Fprintf(os.Stderr, "app: %v\n", waitErr)
		}
	}()

	if err := waitHTTP(baseURL); err != nil {
		fmt.Fprintf(os.Stderr, "http: %v\n", err)

		return 1
	}

	return m.Run()
}

// Проверяет регистрацию пользователя и чтение его анкеты.
// Ожидает 200, идентификатор пользователя и те же поля анкеты без пароля.
func TestRegisterAndGetUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	profile := sampleProfile()
	status, body := doJSON(t, http.MethodPost, "/user/register", profile)
	require.Equal(t, http.StatusOK, status)

	var created struct {
		UserID string `json:"user_id"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.NoError(t, uuid.Validate(created.UserID))

	status, body = doJSON(t, http.MethodGet, "/user/get/"+created.UserID, nil)
	require.Equal(t, http.StatusOK, status)

	var got struct {
		ID         string `json:"id"`
		FirstName  string `json:"first_name"`
		SecondName string `json:"second_name"`
		Birthdate  string `json:"birthdate"`
		Biography  string `json:"biography"`
		City       string `json:"city"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	require.Equal(t, created.UserID, got.ID)
	require.Equal(t, profile.FirstName, got.FirstName)
	require.Equal(t, profile.SecondName, got.SecondName)
	require.Equal(t, profile.Birthdate, got.Birthdate)
	require.Equal(t, profile.Biography, got.Biography)
	require.Equal(t, profile.City, got.City)
	require.NotContains(t, string(body), "password")
}

// Проверяет регистрацию с коротким паролем и с невалидным JSON.
// Ожидает 400 и пустое тело ответа в обоих случаях.
func TestRegisterRejectsInvalidInput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	profile := sampleProfile()
	profile.Password = "short"
	status, body := doJSON(t, http.MethodPost, "/user/register", profile)
	require.Equal(t, http.StatusBadRequest, status)
	require.Empty(t, body)

	status, body = doJSON(t, http.MethodPost, "/user/register", []byte("{"))
	require.Equal(t, http.StatusBadRequest, status)
	require.Empty(t, body)
}

// Проверяет чтение анкеты по неизвестному и по невалидному идентификатору.
// Ожидает 404 для отсутствующего пользователя и 400 для идентификатора не в формате UUID, оба ответа без тела.
func TestGetUserNotFoundAndInvalidID(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	status, body := doJSON(t, http.MethodGet, "/user/get/"+uuid.NewString(), nil)
	require.Equal(t, http.StatusNotFound, status)
	require.Empty(t, body)

	status, body = doJSON(t, http.MethodGet, "/user/get/not-a-uuid", nil)
	require.Equal(t, http.StatusBadRequest, status)
	require.Empty(t, body)
}

// Проверяет вход с верным паролем, с неверным паролем, с неизвестным пользователем и с невалидным идентификатором.
// Ожидает 200 и токен, 400, 404 и 400 соответственно. Ошибочные ответы без тела.
func TestLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	userID := registerUser(t, sampleProfile())

	status, body := doJSON(t, http.MethodPost, "/login", map[string]string{
		"id":       userID,
		"password": sampleProfile().Password,
	})
	require.Equal(t, http.StatusOK, status)

	var loggedIn struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(body, &loggedIn))
	require.NoError(t, uuid.Validate(loggedIn.Token))

	status, body = doJSON(t, http.MethodPost, "/login", map[string]string{
		"id":       userID,
		"password": "wrong-password",
	})
	require.Equal(t, http.StatusBadRequest, status)
	require.Empty(t, body)

	status, body = doJSON(t, http.MethodPost, "/login", map[string]string{
		"id":       uuid.NewString(),
		"password": sampleProfile().Password,
	})
	require.Equal(t, http.StatusNotFound, status)
	require.Empty(t, body)

	status, body = doJSON(t, http.MethodPost, "/login", map[string]string{
		"id":       "not-a-uuid",
		"password": sampleProfile().Password,
	})
	require.Equal(t, http.StatusBadRequest, status)
	require.Empty(t, body)
}

// Проверяет повторный вход того же пользователя.
// Ожидает новый токен и единственную сессию в базе с этим токеном.
func TestLoginReplacesPreviousToken(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	profile := sampleProfile()
	userID := registerUser(t, profile)
	first := login(t, userID, profile.Password)
	second := login(t, userID, profile.Password)
	require.NotEqual(t, first, second)

	tokens := sessionTokens(t, userID)
	require.Equal(t, []string{second}, tokens)
}

// Проверяет запрос к незарегистрированному маршруту.
// Ожидает 404 и пустое тело ответа.
func TestUnknownRoute(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	status, body := doJSON(t, http.MethodGet, "/unknown", nil)
	require.Equal(t, http.StatusNotFound, status)
	require.Empty(t, body)
}

func startPostgres(ctx context.Context) (*postgres.PostgresContainer, error) {
	container, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase(dbName),
		postgres.WithUsername(dbUser),
		postgres.WithPassword(dbPassword),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(time.Minute),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("start postgres: %w", err)
	}

	return container, nil
}

func terminatePostgres(container *postgres.PostgresContainer) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := container.Terminate(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "terminate postgres: %v\n", err)
	}
}

func appConfig(ctx context.Context, container *postgres.PostgresContainer) (config.Config, error) {
	host, err := container.Host(ctx)
	if err != nil {
		return config.Config{}, fmt.Errorf("postgres host: %w", err)
	}
	mappedPort, err := container.MappedPort(ctx, "5432/tcp")
	if err != nil {
		return config.Config{}, fmt.Errorf("postgres port: %w", err)
	}
	httpPort, err := freePort()
	if err != nil {
		return config.Config{}, err
	}

	cfg := config.Config{
		ShutdownTimeout: 5 * time.Second,
		HTTPServer: config.HTTPServerConfig{
			Port:         httpPort,
			Timeout:      5 * time.Second,
			IdleTimeout:  time.Minute,
			MaxBodyBytes: 65536,
		},
		Postgres: config.PostgresConfig{
			Host:            host,
			Port:            mappedPort.Port(),
			User:            dbUser,
			Password:        dbPassword,
			Database:        dbName,
			MaxConns:        5,
			MaxConnLifetime: time.Minute,
			MaxConnIdleTime: time.Minute,
		},
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, err
	}

	postgresDSN = cfg.Postgres.DSN()
	baseURL = "http://127.0.0.1:" + httpPort

	return cfg, nil
}

func waitHTTP(url string) error {
	deadline := time.Now().Add(30 * time.Second)
	probe := url + "/user/get/" + uuid.NewString()
	client := &http.Client{Timeout: time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(probe)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	return errors.New("http server was not ready")
}

func waitErr(ch <-chan error, timeout time.Duration) error {
	select {
	case err := <-ch:
		return err
	case <-time.After(timeout):
		return errors.New("app shutdown timed out")
	}
}

func freePort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return "", errors.New("unexpected listener address")
	}

	return strconv.Itoa(addr.Port), nil
}

type profile struct {
	FirstName  string `json:"first_name"`
	SecondName string `json:"second_name"`
	Birthdate  string `json:"birthdate"`
	Biography  string `json:"biography"`
	City       string `json:"city"`
	Password   string `json:"password"`
}

func sampleProfile() profile {
	return profile{
		FirstName:  "Иван",
		SecondName: "Иванов",
		Birthdate:  "1990-01-02",
		Biography:  "Обо мне",
		City:       "Москва",
		Password:   "password1",
	}
}

func registerUser(t *testing.T, user profile) string {
	t.Helper()

	status, body := doJSON(t, http.MethodPost, "/user/register", user)
	require.Equal(t, http.StatusOK, status)

	var created struct {
		UserID string `json:"user_id"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.NoError(t, uuid.Validate(created.UserID))

	return created.UserID
}

func login(t *testing.T, userID, password string) string {
	t.Helper()

	status, body := doJSON(t, http.MethodPost, "/login", map[string]string{
		"id":       userID,
		"password": password,
	})
	require.Equal(t, http.StatusOK, status)

	var loggedIn struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(body, &loggedIn))
	require.NoError(t, uuid.Validate(loggedIn.Token))

	return loggedIn.Token
}

func sessionTokens(t *testing.T, userID string) []string {
	t.Helper()

	ctx := t.Context()
	conn, err := pgx.Connect(ctx, postgresDSN)
	require.NoError(t, err)
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `SELECT token::text FROM sessions WHERE user_id = $1`, userID)
	require.NoError(t, err)
	defer rows.Close()

	var tokens []string
	for rows.Next() {
		var token string
		require.NoError(t, rows.Scan(&token))
		tokens = append(tokens, token)
	}
	require.NoError(t, rows.Err())

	return tokens
}

func doJSON(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()

	var reader io.Reader
	switch payload := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(payload)
	default:
		encoded, err := json.Marshal(payload)
		require.NoError(t, err)
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, baseURL+path, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, data
}
