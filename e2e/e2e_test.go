//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// Проверяет поиск под нагрузкой после того же запуска контейнеров, что и основной сценарий.
// Для 1, 10, 100 и 1000 одновременных запросов отдельно ищет существующих и несуществующих пользователей.
// Ожидает 200 и отчёт: latency и throughput запроса (1/latency), минимум, максимум, среднее и медиана.
func TestE2E_SearchLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()

	t.Log("stopping docker containers and removing postgres volume")
	if err := resetDockerContainers(); err != nil {
		t.Logf("reset docker: %v", err)
	}

	t.Log("starting docker containers")
	require.NoError(t, startDockerContainers(ctx))
	require.NoError(t, waitAPI(ctx))

	client := &http.Client{
		Timeout: 3 * time.Minute,
		Transport: &http.Transport{
			MaxIdleConns:        2048,
			MaxIdleConnsPerHost: 2048,
			IdleConnTimeout:     time.Minute,
		},
	}
	defer client.CloseIdleConnections()

	levels := []int{1, 10, 100, 1000}
	targets := []searchTarget{
		{label: "существующие", firstName: "Роберт", lastName: "Абрамов", expectFound: true},
		{label: "несуществующие", firstName: "Nomatch", lastName: "Missing", expectFound: false},
	}
	reports := make([]loadReport, 0, len(levels)*len(targets))
	for _, level := range levels {
		for _, target := range targets {
			samples, err := runSearchLoad(ctx, client, level, target)
			require.NoError(t, err)
			reports = append(reports, summarizeLoad(fmt.Sprintf("%d %s", level, target.label), samples))
		}
	}
	t.Logf("\n%s", formatLoadReport(reports))
}

type searchTarget struct {
	label       string
	firstName   string
	lastName    string
	expectFound bool
}

type requestSample struct {
	latency    time.Duration
	throughput float64
}

type loadReport struct {
	name             string
	latencyMin       time.Duration
	latencyMax       time.Duration
	latencyAvg       time.Duration
	latencyMedian    time.Duration
	throughputMin    float64
	throughputMax    float64
	throughputAvg    float64
	throughputMedian float64
}

func runSearchLoad(ctx context.Context, client *http.Client, level int, target searchTarget) ([]requestSample, error) {
	samples := make([]requestSample, level)
	errs := make([]error, level)
	var ready sync.WaitGroup
	var done sync.WaitGroup
	start := make(chan struct{})
	ready.Add(level)
	done.Add(level)
	for i := range level {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-start

			latency, status, body, err := doSearch(ctx, client, target.firstName, target.lastName)
			if err != nil {
				errs[i] = err
				return
			}
			if status != http.StatusOK {
				errs[i] = fmt.Errorf("статус %d: %s", status, truncateBody(body))
				return
			}
			if err := validateSearchResponse(body, target); err != nil {
				errs[i] = err
				return
			}
			samples[i] = requestSample{
				latency:    latency,
				throughput: 1 / latency.Seconds(),
			}
		}(i)
	}
	ready.Wait()
	close(start)
	done.Wait()

	var failed []error
	for _, err := range errs {
		if err != nil {
			failed = append(failed, err)
		}
	}
	if len(failed) > 0 {
		shown := failed
		if len(shown) > 3 {
			shown = shown[:3]
		}

		return nil, fmt.Errorf("%d %s: ошибок %d, первые: %w", level, target.label, len(failed), errors.Join(shown...))
	}

	return samples, nil
}

func doSearch(ctx context.Context, client *http.Client, firstName, lastName string) (time.Duration, int, []byte, error) {
	query := url.Values{}
	query.Set("first_name", firstName)
	query.Set("last_name", lastName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBaseURL+"/user/search?"+query.Encode(), nil)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("создать запрос: %w", err)
	}

	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("выполнить запрос: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	latency := time.Since(started)
	if err != nil {
		return 0, resp.StatusCode, nil, fmt.Errorf("прочитать ответ: %w", err)
	}
	if latency <= 0 {
		latency = time.Nanosecond
	}

	return latency, resp.StatusCode, body, nil
}

func validateSearchResponse(body []byte, target searchTarget) error {
	var users []struct {
		FirstName  string `json:"first_name"`
		SecondName string `json:"second_name"`
	}
	if err := json.Unmarshal(body, &users); err != nil {
		return fmt.Errorf("разобрать ответ: %w", err)
	}
	if !target.expectFound {
		if len(users) != 0 {
			return fmt.Errorf("ожидался пустой список, получено %d", len(users))
		}

		return nil
	}
	if len(users) == 0 {
		return errors.New("ожидались анкеты")
	}
	for _, user := range users {
		if strings.HasPrefix(user.FirstName, target.firstName) && strings.HasPrefix(user.SecondName, target.lastName) {
			continue
		}

		return fmt.Errorf("анкета %s %s не подходит под префикс", user.FirstName, user.SecondName)
	}

	return nil
}

func summarizeLoad(name string, samples []requestSample) loadReport {
	latencies := make([]time.Duration, len(samples))
	throughputs := make([]float64, len(samples))
	var latencySum time.Duration
	var throughputSum float64
	for i, sample := range samples {
		latencies[i] = sample.latency
		throughputs[i] = sample.throughput
		latencySum += sample.latency
		throughputSum += sample.throughput
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	sort.Float64s(throughputs)

	return loadReport{
		name:             name,
		latencyMin:       latencies[0],
		latencyMax:       latencies[len(latencies)-1],
		latencyAvg:       latencySum / time.Duration(len(latencies)),
		latencyMedian:    medianDuration(latencies),
		throughputMin:    throughputs[0],
		throughputMax:    throughputs[len(throughputs)-1],
		throughputAvg:    throughputSum / float64(len(throughputs)),
		throughputMedian: medianFloat(throughputs),
	}
}

func medianDuration(sorted []time.Duration) time.Duration {
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}

	return (sorted[n/2-1] + sorted[n/2]) / 2
}

func medianFloat(sorted []float64) float64 {
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}

	return (sorted[n/2-1] + sorted[n/2]) / 2
}

func formatLoadReport(reports []loadReport) string {
	var builder strings.Builder
	writer := csv.NewWriter(&builder)
	_ = writer.Write([]string{
		"тест",
		"latency мин (мс)",
		"latency макс (мс)",
		"latency средняя (мс)",
		"latency медиана (мс)",
		"throughput мин (req/s)",
		"throughput макс (req/s)",
		"throughput средняя (req/s)",
		"throughput медиана (req/s)",
	})
	for _, report := range reports {
		_ = writer.Write([]string{
			report.name,
			formatLatency(report.latencyMin),
			formatLatency(report.latencyMax),
			formatLatency(report.latencyAvg),
			formatLatency(report.latencyMedian),
			formatThroughput(report.throughputMin),
			formatThroughput(report.throughputMax),
			formatThroughput(report.throughputAvg),
			formatThroughput(report.throughputMedian),
		})
	}
	writer.Flush()

	return builder.String()
}

func formatLatency(value time.Duration) string {
	return strconv.FormatFloat(float64(value)/float64(time.Millisecond), 'f', 3, 64)
}

func formatThroughput(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func truncateBody(body []byte) string {
	const limit = 200
	if len(body) <= limit {
		return string(body)
	}

	return string(body[:limit])
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
	return runMake("docker-stop", time.Minute, "stop docker")
}

func resetDockerContainers() error {
	return runMake("docker-reset", 2*time.Minute, "reset docker")
}

func runMake(target string, timeout time.Duration, action string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "make", target)
	cmd.Dir = ".."
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", action, err)
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
