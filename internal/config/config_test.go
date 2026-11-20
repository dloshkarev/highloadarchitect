package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		ShutdownTimeout: 10 * time.Second,
		HTTPServer: HTTPServerConfig{
			Port:         "8080",
			Timeout:      5 * time.Minute,
			IdleTimeout:  time.Minute,
			MaxBodyBytes: 65536,
		},
		Postgres: PostgresConfig{
			Host:            "localhost",
			Port:            "5432",
			User:            "social",
			Password:        "secret",
			Database:        "social",
			MaxConns:        10,
			MaxConnLifetime: 30 * time.Minute,
			MaxConnIdleTime: 5 * time.Minute,
		},
		Feed: FeedConfig{
			Enabled:        true,
			CacheLimit:     1000,
			WarmLimit:      1000,
			TTL:            24 * time.Hour,
			PostTTL:        72 * time.Hour,
			EmptyMarkerTTL: 24 * time.Hour,
		},
		Redis: RedisConfig{
			Addr:         "localhost:6379",
			DialTimeout:  time.Second,
			ReadTimeout:  500 * time.Millisecond,
			WriteTimeout: 500 * time.Millisecond,
			PoolSize:     10,
		},
		Kafka: KafkaConfig{
			Brokers:      "localhost:9092",
			Topic:        "post-events",
			GroupID:      "feed-fanout",
			BatchSize:    500,
			BatchTimeout: time.Second,
		},
		Outbox: OutboxConfig{
			PollInterval: time.Second,
			BatchSize:    100,
		},
	}
}

func TestConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*Config)
		want   error
	}{
		{
			name:   "valid",
			mutate: func(*Config) {},
		},
		{
			name:   "zero feed cache limit",
			mutate: func(c *Config) { c.Feed.CacheLimit = 0 },
			want:   errInvalidFeedCacheLimit,
		},
		{
			name:   "zero feed warm limit",
			mutate: func(c *Config) { c.Feed.WarmLimit = 0 },
			want:   errInvalidFeedWarmLimit,
		},
		{
			name:   "zero feed ttl",
			mutate: func(c *Config) { c.Feed.TTL = 0 },
			want:   errInvalidFeedTTL,
		},
		{
			name:   "zero feed post ttl",
			mutate: func(c *Config) { c.Feed.PostTTL = 0 },
			want:   errInvalidFeedPostTTL,
		},
		{
			name:   "zero feed empty marker ttl",
			mutate: func(c *Config) { c.Feed.EmptyMarkerTTL = 0 },
			want:   errInvalidFeedMarkerTTL,
		},
		{
			name:   "empty redis addr",
			mutate: func(c *Config) { c.Redis.Addr = "" },
			want:   errEmptyRedisAddr,
		},
		{
			name:   "zero redis dial timeout",
			mutate: func(c *Config) { c.Redis.DialTimeout = 0 },
			want:   errInvalidRedisTimeout,
		},
		{
			name:   "zero redis read timeout",
			mutate: func(c *Config) { c.Redis.ReadTimeout = 0 },
			want:   errInvalidRedisTimeout,
		},
		{
			name:   "zero redis write timeout",
			mutate: func(c *Config) { c.Redis.WriteTimeout = 0 },
			want:   errInvalidRedisTimeout,
		},
		{
			name:   "zero redis pool size",
			mutate: func(c *Config) { c.Redis.PoolSize = 0 },
			want:   errInvalidRedisPoolSize,
		},
		{
			name:   "empty kafka brokers",
			mutate: func(c *Config) { c.Kafka.Brokers = " " },
			want:   errEmptyKafkaBrokers,
		},
		{
			name:   "empty kafka broker in list",
			mutate: func(c *Config) { c.Kafka.Brokers = "localhost:9092,," },
			want:   errEmptyKafkaBroker,
		},
		{
			name:   "empty kafka topic",
			mutate: func(c *Config) { c.Kafka.Topic = "" },
			want:   errEmptyKafkaTopic,
		},
		{
			name:   "empty kafka group id",
			mutate: func(c *Config) { c.Kafka.GroupID = "" },
			want:   errEmptyKafkaGroupID,
		},
		{
			name:   "zero kafka batch size",
			mutate: func(c *Config) { c.Kafka.BatchSize = 0 },
			want:   errInvalidKafkaBatchSize,
		},
		{
			name:   "zero kafka batch timeout",
			mutate: func(c *Config) { c.Kafka.BatchTimeout = 0 },
			want:   errInvalidKafkaBatchWait,
		},
		{
			name:   "zero outbox poll interval",
			mutate: func(c *Config) { c.Outbox.PollInterval = 0 },
			want:   errInvalidOutboxPollInterval,
		},
		{
			name:   "zero outbox batch size",
			mutate: func(c *Config) { c.Outbox.BatchSize = 0 },
			want:   errInvalidOutboxBatchSize,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := validConfig()
			tt.mutate(&cfg)

			err := cfg.Validate()
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Validate() error = %v, want nil", err)
				}

				return
			}

			if !errors.Is(err, tt.want) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestKafkaBrokersList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		brokers string
		want    []string
		wantErr error
	}{
		{
			name:    "single broker",
			brokers: "localhost:9092",
			want:    []string{"localhost:9092"},
		},
		{
			name:    "multiple brokers with spaces",
			brokers: "localhost:9092, kafka1:9092,kafka2:9092",
			want:    []string{"localhost:9092", "kafka1:9092", "kafka2:9092"},
		},
		{
			name:    "empty",
			brokers: "",
			wantErr: errEmptyKafkaBrokers,
		},
		{
			name:    "blank",
			brokers: "   ",
			wantErr: errEmptyKafkaBrokers,
		},
		{
			name:    "empty element",
			brokers: "localhost:9092,,kafka1:9092",
			wantErr: errEmptyKafkaBroker,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := KafkaConfig{Brokers: tt.brokers}.BrokersList()
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("BrokersList() error = %v, want %v", err, tt.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatalf("BrokersList() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("BrokersList() = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("BrokersList() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestLoadFromFile(t *testing.T) {
	clearConfigEnv(t)

	path := filepath.Join(t.TempDir(), "config.yaml")
	content := `shutdown_timeout: "10s"

http-server:
  port: "8080"
  read_write_timeout: "5m"
  idle_timeout: "1m"
  max_body_bytes: 65536

postgres:
  host: "localhost"
  port: "5432"
  user: "social"
  password: "secret"
  database: "social"
  max_conns: 10
  max_conn_lifetime: "30m"
  max_conn_idle_time: "5m"

feed:
  enabled: true
  cache_limit: 1000
  warm_limit: 900
  ttl: "24h"
  post_ttl: "72h"
  empty_marker_ttl: "12h"

redis:
  addr: "localhost:6379"
  db: 1
  dial_timeout: "1s"
  read_timeout: "500ms"
  write_timeout: "500ms"
  pool_size: 10

kafka:
  brokers: "localhost:9092, kafka1:9092"
  topic: "post-events"
  group_id: "feed-fanout"
  batch_size: 500
  batch_timeout: "1s"

outbox:
  poll_interval: "1s"
  batch_size: 100
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile() error = %v", err)
	}

	if cfg.Feed.WarmLimit != 900 {
		t.Fatalf("Feed.WarmLimit = %d, want 900", cfg.Feed.WarmLimit)
	}
	if cfg.Feed.TTL != 24*time.Hour {
		t.Fatalf("Feed.TTL = %v, want 24h", cfg.Feed.TTL)
	}
	if cfg.Feed.PostTTL != 72*time.Hour {
		t.Fatalf("Feed.PostTTL = %v, want 72h", cfg.Feed.PostTTL)
	}
	if cfg.Feed.EmptyMarkerTTL != 12*time.Hour {
		t.Fatalf("Feed.EmptyMarkerTTL = %v, want 12h", cfg.Feed.EmptyMarkerTTL)
	}
	if cfg.Redis.DB != 1 {
		t.Fatalf("Redis.DB = %d, want 1", cfg.Redis.DB)
	}

	brokers, err := cfg.Kafka.BrokersList()
	if err != nil {
		t.Fatalf("BrokersList() error = %v", err)
	}
	if len(brokers) != 2 || brokers[0] != "localhost:9092" || brokers[1] != "kafka1:9092" {
		t.Fatalf("BrokersList() = %v", brokers)
	}
	if cfg.Outbox.BatchSize != 100 {
		t.Fatalf("Outbox.BatchSize = %d, want 100", cfg.Outbox.BatchSize)
	}
}

func clearConfigEnv(t *testing.T) {
	t.Helper()

	names := []string{
		"SHUTDOWN_TIMEOUT",
		"HTTP_SERVER_PORT", "HTTP_READ_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT", "HTTP_MAX_BODY_BYTES",
		"POSTGRES_HOST", "POSTGRES_PORT", "POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB",
		"POSTGRES_READ_HOSTS", "POSTGRES_MAX_CONNS", "POSTGRES_MAX_CONN_LIFETIME", "POSTGRES_MAX_CONN_IDLE_TIME",
		"FEED_ENABLED", "FEED_CACHE_LIMIT", "FEED_WARM_LIMIT", "FEED_TTL", "FEED_POST_TTL", "FEED_EMPTY_MARKER_TTL",
		"REDIS_ADDR", "REDIS_PASSWORD", "REDIS_DB", "REDIS_DIAL_TIMEOUT", "REDIS_READ_TIMEOUT",
		"REDIS_WRITE_TIMEOUT", "REDIS_POOL_SIZE",
		"KAFKA_BROKERS", "KAFKA_TOPIC", "KAFKA_GROUP_ID", "KAFKA_BATCH_SIZE", "KAFKA_BATCH_TIMEOUT",
		"OUTBOX_POLL_INTERVAL", "OUTBOX_BATCH_SIZE",
	}

	for _, name := range names {
		value, ok := os.LookupEnv(name)
		t.Cleanup(func() {
			if ok {
				_ = os.Setenv(name, value)
			} else {
				_ = os.Unsetenv(name)
			}
		})
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}
}
