//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const apiBaseURL = "http://127.0.0.1:8080"

// Проверяет запуск приложения и PostgreSQL в Docker и успешный сценарий API: регистрация, вход и чтение анкеты.
// Ожидает 200, идентификатор пользователя, токен и те же поля анкеты без пароля.
// Контейнеры останавливаются перед тестом и остаются запущенными после него.
func TestE2E_RegisterLoginGetUser(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()

	t.Log("stopping docker containers")
	if err := stopDockerContainers(); err != nil {
		t.Logf("stop docker: %v", err)
	}

	t.Log("starting docker containers")
	require.NoError(t, startDockerContainers(ctx))
	require.NoError(t, waitAPI(ctx))

	user := profile{
		FirstName:  "Иван",
		SecondName: "Иванов",
		Birthdate:  "1990-01-02",
		Biography:  "Обо мне",
		City:       "Москва",
		Password:   "password1",
	}

	status, body := doJSON(ctx, t, http.MethodPost, "/user/register", user)
	require.Equal(t, http.StatusOK, status)

	var created struct {
		UserID string `json:"user_id"`
	}
	require.NoError(t, json.Unmarshal(body, &created))
	require.NoError(t, uuid.Validate(created.UserID))

	status, body = doJSON(ctx, t, http.MethodPost, "/login", map[string]string{
		"id":       created.UserID,
		"password": user.Password,
	})
	require.Equal(t, http.StatusOK, status)

	var loggedIn struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(body, &loggedIn))
	require.NoError(t, uuid.Validate(loggedIn.Token))

	status, body = doJSON(ctx, t, http.MethodGet, "/user/get/"+created.UserID, nil)
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
	require.Equal(t, user.FirstName, got.FirstName)
	require.Equal(t, user.SecondName, got.SecondName)
	require.Equal(t, user.Birthdate, got.Birthdate)
	require.Equal(t, user.Biography, got.Biography)
	require.Equal(t, user.City, got.City)
	require.NotContains(t, string(body), "password")
}

func startDockerContainers(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "make", "docker-run")
	cmd.Dir = ".."
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("start docker: %w", err)
	}

	return nil
}

func stopDockerContainers() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "make", "docker-stop")
	cmd.Dir = ".."
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("stop docker: %w", err)
	}

	return nil
}

func waitAPI(ctx context.Context) error {
	client := &http.Client{Timeout: time.Second}
	url := apiBaseURL + "/user/get/" + uuid.NewString()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("create readiness request: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusNotFound {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("api was not ready: %w", ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
}

type profile struct {
	FirstName  string `json:"first_name"`
	SecondName string `json:"second_name"`
	Birthdate  string `json:"birthdate"`
	Biography  string `json:"biography"`
	City       string `json:"city"`
	Password   string `json:"password"`
}

func doJSON(ctx context.Context, t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, apiBaseURL+path, reader)
	require.NoError(t, err)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, data
}
