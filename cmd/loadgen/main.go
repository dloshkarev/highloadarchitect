package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	defaultRequests    = 100
	defaultConcurrency = 10
	defaultTimeout     = 30 * time.Second
	maxIdleConns       = 512
	shownFailures      = 3
	medianPercentile   = 0.50
	p95Percentile      = 0.95
	p99Percentile      = 0.99
)

var errUnknownMode = errors.New("неизвестный режим, поддерживаются read и write")

type config struct {
	baseURL     string
	mode        string
	requests    int
	concurrency int
	timeout     time.Duration
	firstName   string
	lastName    string
}

type requestSample struct {
	latency time.Duration
	status  int
	err     error
}

type loadReport struct {
	total      int
	succeeded  int
	latencyMin time.Duration
	latencyMax time.Duration
	latencyAvg time.Duration
	latencyMed time.Duration
	latencyP95 time.Duration
	latencyP99 time.Duration
	throughput float64
}

type requestFactory func(ctx context.Context) (*http.Request, error)

// Консольный инструмент нагрузочного тестирования уже запущенного приложения.
// Дёргает выбранную ручку заданное число раз с заданным параллелизмом
// и печатает отчёт: latency (минимум, максимум, среднее, медиана, p95, p99) и throughput.
//
// Режимы: read — сложное чтение (поиск анкет), write — запись (регистрация пользователей).
//
// Примеры:
//
//	go run ./cmd/loadgen -mode read -requests 1000 -concurrency 100
//	go run ./cmd/loadgen -mode read -first-name Роберт -last-name Абрамов
//	go run ./cmd/loadgen -mode write -requests 1000 -concurrency 10
func main() {
	if err := run(); err != nil {
		slog.Error("loadgen stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := parseFlags()

	newRequest, err := makeRequestFactory(cfg)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := &http.Client{
		Timeout: cfg.timeout,
		Transport: &http.Transport{
			MaxIdleConns:        max(cfg.concurrency, maxIdleConns),
			MaxIdleConnsPerHost: max(cfg.concurrency, maxIdleConns),
			IdleConnTimeout:     time.Minute,
		},
	}
	defer client.CloseIdleConnections()

	printOutf(
		"нагрузка: %s %s, запросов %d, параллелизм %d\n",
		cfg.mode,
		cfg.baseURL,
		cfg.requests,
		cfg.concurrency,
	)

	samples, elapsed := runLoad(ctx, client, cfg, newRequest)
	if ctx.Err() != nil {
		printOutf("прервано пользователем, отчёт по выполненным запросам\n")
	}

	printReport(samples, elapsed)

	return nil
}

func parseFlags() config {
	cfg := config{}
	flag.StringVar(&cfg.baseURL, "base-url", "http://localhost:8080", "базовый URL приложения")
	flag.StringVar(&cfg.mode, "mode", "read", "режим нагрузки: read (поиск анкет) | write (регистрация)")
	flag.IntVar(&cfg.requests, "requests", defaultRequests, "общее число запросов")
	flag.IntVar(&cfg.concurrency, "concurrency", defaultConcurrency, "число одновременных запросов")
	flag.DurationVar(&cfg.timeout, "timeout", defaultTimeout, "таймаут одного запроса")
	flag.StringVar(&cfg.firstName, "first-name", "Роберт", "префикс имени для режима read")
	flag.StringVar(&cfg.lastName, "last-name", "Абрамов", "префикс фамилии для режима read")
	flag.Parse()

	cfg.baseURL = strings.TrimRight(cfg.baseURL, "/")

	return cfg
}

// makeRequestFactory возвращает функцию, строящую HTTP-запрос под выбранный режим.
func makeRequestFactory(cfg config) (requestFactory, error) {
	switch cfg.mode {
	case "read":
		return func(ctx context.Context) (*http.Request, error) {
			query := url.Values{}
			query.Set("first_name", cfg.firstName)
			query.Set("last_name", cfg.lastName)

			return http.NewRequestWithContext(ctx, http.MethodGet, cfg.baseURL+"/user/search?"+query.Encode(), nil)
		}, nil
	case "write":
		// Каждый запрос регистрирует уникального пользователя; база счётчика от времени,
		// чтобы повторные прогоны не пересекались с предыдущими.
		var sequence atomic.Uint64
		sequence.Add(uint64(time.Now().UnixMilli()))

		return func(ctx context.Context) (*http.Request, error) {
			n := sequence.Add(1)
			payload, err := json.Marshal(map[string]string{
				"first_name":  "Нагруз",
				"second_name": fmt.Sprintf("Тестовый%d", n),
				"birthdate":   "2000-01-01",
				"biography":   "создан loadgen",
				"city":        "Москва",
				"password":    "password1",
			})
			if err != nil {
				return nil, fmt.Errorf("сериализовать анкету: %w", err)
			}

			req, err := http.NewRequestWithContext(
				ctx,
				http.MethodPost,
				cfg.baseURL+"/user/register",
				bytes.NewReader(payload),
			)
			if err != nil {
				return nil, fmt.Errorf("создать запрос регистрации: %w", err)
			}
			req.Header.Set("Content-Type", "application/json")

			return req, nil
		}, nil
	default:
		return nil, fmt.Errorf("%w: %q", errUnknownMode, cfg.mode)
	}
}

// runLoad выполняет cfg.requests запросов пулом из cfg.concurrency воркеров.
// Каждые progressStep запросов печатает прогресс в лог.
// Возвращает семплы выполненных запросов и время от старта до завершения последнего.
func runLoad(
	ctx context.Context,
	client *http.Client,
	cfg config,
	newRequest requestFactory,
) ([]requestSample, time.Duration) {
	const progressStep = 10000

	jobs := make(chan struct{})
	samples := make([]requestSample, 0, cfg.requests)
	var mu sync.Mutex
	var completed atomic.Int64
	started := time.Now()

	var workers sync.WaitGroup
	workers.Add(cfg.concurrency)
	for range cfg.concurrency {
		go func() {
			defer workers.Done()
			for range jobs {
				sample := doRequest(ctx, client, newRequest)
				mu.Lock()
				samples = append(samples, sample)
				mu.Unlock()

				if done := completed.Add(1); done%progressStep == 0 || done == int64(cfg.requests) {
					elapsed := time.Since(started)
					printOutf(
						"прогресс: %d/%d запросов за %s (%.0f req/s)\n",
						done,
						cfg.requests,
						elapsed.Round(time.Millisecond),
						float64(done)/elapsed.Seconds(),
					)
				}
			}
		}()
	}

scheduled:
	for range cfg.requests {
		select {
		case <-ctx.Done():
			break scheduled
		case jobs <- struct{}{}:
		}
	}
	close(jobs)
	workers.Wait()

	elapsed := time.Since(started)
	if elapsed <= 0 {
		elapsed = time.Nanosecond
	}

	return samples, elapsed
}

func doRequest(
	ctx context.Context,
	client *http.Client,
	newRequest requestFactory,
) requestSample {
	req, err := newRequest(ctx)
	if err != nil {
		return requestSample{err: fmt.Errorf("создать запрос: %w", err)}
	}

	started := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return requestSample{err: fmt.Errorf("выполнить запрос: %w", err)}
	}
	defer resp.Body.Close()

	_, err = io.Copy(io.Discard, resp.Body)
	latency := time.Since(started)
	if err != nil {
		return requestSample{status: resp.StatusCode, err: fmt.Errorf("прочитать ответ: %w", err)}
	}
	if latency <= 0 {
		latency = time.Nanosecond
	}

	return requestSample{latency: latency, status: resp.StatusCode}
}

func printReport(samples []requestSample, elapsed time.Duration) {
	report := summarizeLoad(samples, elapsed)

	statuses := make(map[int]int)
	var failures []error
	for _, sample := range samples {
		if sample.err != nil {
			failures = append(failures, sample.err)

			continue
		}
		statuses[sample.status]++
	}

	printOutf(
		"\nзапросов выполнено: %d, успешно (2xx): %d, ошибок: %d\n",
		report.total,
		report.succeeded,
		len(failures),
	)
	if len(statuses) > 0 {
		parts := make([]string, 0, len(statuses))
		for status, count := range statuses {
			parts = append(parts, fmt.Sprintf("%d: %d", status, count))
		}
		slices.Sort(parts)
		printOutf("статусы: %s\n", strings.Join(parts, ", "))
	}

	var builder strings.Builder
	writer := csv.NewWriter(&builder)
	_ = writer.Write([]string{
		"latency мин (мс)",
		"latency макс (мс)",
		"latency средняя (мс)",
		"latency медиана (мс)",
		"latency p95 (мс)",
		"latency p99 (мс)",
		"throughput (req/s)",
	})
	_ = writer.Write([]string{
		formatLatency(report.latencyMin),
		formatLatency(report.latencyMax),
		formatLatency(report.latencyAvg),
		formatLatency(report.latencyMed),
		formatLatency(report.latencyP95),
		formatLatency(report.latencyP99),
		strconv.FormatFloat(report.throughput, 'f', 2, 64),
	})
	writer.Flush()
	printOutf("%s\n", builder.String())

	if len(failures) > 0 {
		shown := failures
		if len(shown) > shownFailures {
			shown = shown[:shownFailures]
		}
		printOutf("первые ошибки: %v\n", shown)
	}
}

func summarizeLoad(samples []requestSample, elapsed time.Duration) loadReport {
	report := loadReport{total: len(samples)}

	latencies := make([]time.Duration, 0, len(samples))
	var latencySum time.Duration
	for _, sample := range samples {
		if sample.err != nil || sample.status < http.StatusOK || sample.status >= http.StatusMultipleChoices {
			continue
		}
		latencies = append(latencies, sample.latency)
		latencySum += sample.latency
	}
	if len(latencies) == 0 {
		return report
	}

	slices.Sort(latencies)

	report.succeeded = len(latencies)
	report.latencyMin = latencies[0]
	report.latencyMax = latencies[len(latencies)-1]
	report.latencyAvg = latencySum / time.Duration(len(latencies))
	report.latencyMed = percentile(latencies, medianPercentile)
	report.latencyP95 = percentile(latencies, p95Percentile)
	report.latencyP99 = percentile(latencies, p99Percentile)
	report.throughput = float64(len(latencies)) / elapsed.Seconds()

	return report
}

// percentile возвращает перцентиль по отсортированному срезу (p от 0 до 1).
func percentile(sorted []time.Duration, p float64) time.Duration {
	index := int(float64(len(sorted)-1) * p)

	return sorted[index]
}

func formatLatency(value time.Duration) string {
	return strconv.FormatFloat(float64(value)/float64(time.Millisecond), 'f', 3, 64)
}

func printOutf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stdout, format, args...)
}
