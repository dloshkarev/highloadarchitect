package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

var (
	errConfigPathNotSet = errors.New("CONFIG_PATH is not set")
	errConfigNotExists  = errors.New("config file does not exist")

	errInvalidShutdownTimeout = errors.New("shutdown timeout must be positive")
	errEmptyPort              = errors.New("port is empty")
	errInvalidTimeout         = errors.New("read write timeout must be positive")
	errInvalidIdleTimeout     = errors.New("idle timeout must be positive")
	errEmptyHost              = errors.New("host is empty")
	errEmptyUser              = errors.New("user is empty")
	errEmptyPassword          = errors.New("password is empty")
	errEmptyDatabase          = errors.New("database is empty")
	errInvalidMaxConns        = errors.New("max conns must be positive")
	errInvalidMaxConnLifetime = errors.New("max conn lifetime must be positive")
	errInvalidMaxConnIdleTime = errors.New("max conn idle time must be positive")
	errInvalidMaxBodyBytes    = errors.New("max body bytes must be positive")
	errInvalidReadHost        = errors.New("read host must be in host:port form")

	errInvalidFeedCacheLimit = errors.New("feed cache limit must be positive")
	errInvalidFeedWarmLimit  = errors.New("feed warm limit must be positive")
	errInvalidFeedTTL        = errors.New("feed ttl must be positive")
	errInvalidFeedPostTTL    = errors.New("feed post ttl must be positive")
	errInvalidFeedMarkerTTL  = errors.New("feed empty marker ttl must be positive")

	errEmptyRedisAddr       = errors.New("redis addr is empty")
	errInvalidRedisTimeout  = errors.New("redis timeout must be positive")
	errInvalidRedisPoolSize = errors.New("redis pool size must be positive")

	errEmptyKafkaBrokers     = errors.New("kafka brokers is empty")
	errEmptyKafkaBroker      = errors.New("kafka broker is empty")
	errEmptyKafkaTopic       = errors.New("kafka topic is empty")
	errEmptyKafkaGroupID     = errors.New("kafka group id is empty")
	errInvalidKafkaBatchSize = errors.New("kafka batch size must be positive")
	errInvalidKafkaBatchWait = errors.New("kafka batch timeout must be positive")

	errInvalidOutboxPollInterval = errors.New("outbox poll interval must be positive")
	errInvalidOutboxBatchSize    = errors.New("outbox batch size must be positive")
)

type Config struct {
	HTTPServer      HTTPServerConfig `yaml:"http-server"`
	Postgres        PostgresConfig   `yaml:"postgres"`
	Feed            FeedConfig       `yaml:"feed"`
	Redis           RedisConfig      `yaml:"redis"`
	Kafka           KafkaConfig      `yaml:"kafka"`
	Outbox          OutboxConfig     `yaml:"outbox"`
	ShutdownTimeout time.Duration    `yaml:"shutdown_timeout" env:"SHUTDOWN_TIMEOUT" env-required:"true"`
}

type HTTPServerConfig struct {
	Port         string        `yaml:"port" env:"HTTP_SERVER_PORT" env-required:"true"`
	Timeout      time.Duration `yaml:"read_write_timeout" env:"HTTP_READ_WRITE_TIMEOUT" env-required:"true"`
	IdleTimeout  time.Duration `yaml:"idle_timeout" env:"HTTP_IDLE_TIMEOUT" env-required:"true"`
	MaxBodyBytes int64         `yaml:"max_body_bytes" env:"HTTP_MAX_BODY_BYTES" env-required:"true"`
}

type PostgresConfig struct {
	Host     string `yaml:"host" env:"POSTGRES_HOST" env-required:"true"`
	Port     string `yaml:"port" env:"POSTGRES_PORT" env-required:"true"`
	User     string `yaml:"user" env:"POSTGRES_USER" env-required:"true"`
	Password string `yaml:"password" env:"POSTGRES_PASSWORD" env-required:"true"`
	Database string `yaml:"database" env:"POSTGRES_DB" env-required:"true"`
	// MigrateSeed управляет затравочными миграциями (*_data.sql), наполняющими
	// таблицы тестовыми данными. Включена для полного запуска приложения,
	// интеграционные тесты стартуют со схемой без данных.
	MigrateSeed     bool          `yaml:"migrate_seed" env:"POSTGRES_MIGRATE_SEED"`
	ReadHosts       string        `yaml:"read_hosts" env:"POSTGRES_READ_HOSTS"`
	MaxConns        int32         `yaml:"max_conns" env:"POSTGRES_MAX_CONNS" env-required:"true"`
	MaxConnLifetime time.Duration `yaml:"max_conn_lifetime" env:"POSTGRES_MAX_CONN_LIFETIME" env-required:"true"`
	MaxConnIdleTime time.Duration `yaml:"max_conn_idle_time" env:"POSTGRES_MAX_CONN_IDLE_TIME" env-required:"true"`
}

type FeedConfig struct {
	Enabled        bool          `yaml:"enabled" env:"FEED_ENABLED"`
	CacheLimit     int           `yaml:"cache_limit" env:"FEED_CACHE_LIMIT" env-required:"true"`
	WarmLimit      int           `yaml:"warm_limit" env:"FEED_WARM_LIMIT" env-required:"true"`
	TTL            time.Duration `yaml:"ttl" env:"FEED_TTL" env-required:"true"`
	PostTTL        time.Duration `yaml:"post_ttl" env:"FEED_POST_TTL" env-required:"true"`
	EmptyMarkerTTL time.Duration `yaml:"empty_marker_ttl" env:"FEED_EMPTY_MARKER_TTL" env-required:"true"`
}

type RedisConfig struct {
	Addr         string        `yaml:"addr" env:"REDIS_ADDR" env-required:"true"`
	Password     string        `yaml:"password" env:"REDIS_PASSWORD"`
	DB           int           `yaml:"db" env:"REDIS_DB"`
	DialTimeout  time.Duration `yaml:"dial_timeout" env:"REDIS_DIAL_TIMEOUT" env-required:"true"`
	ReadTimeout  time.Duration `yaml:"read_timeout" env:"REDIS_READ_TIMEOUT" env-required:"true"`
	WriteTimeout time.Duration `yaml:"write_timeout" env:"REDIS_WRITE_TIMEOUT" env-required:"true"`
	PoolSize     int           `yaml:"pool_size" env:"REDIS_POOL_SIZE" env-required:"true"`
}

type KafkaConfig struct {
	Brokers      string        `yaml:"brokers" env:"KAFKA_BROKERS" env-required:"true"`
	Topic        string        `yaml:"topic" env:"KAFKA_TOPIC" env-required:"true"`
	GroupID      string        `yaml:"group_id" env:"KAFKA_GROUP_ID" env-required:"true"`
	BatchSize    int           `yaml:"batch_size" env:"KAFKA_BATCH_SIZE" env-required:"true"`
	BatchTimeout time.Duration `yaml:"batch_timeout" env:"KAFKA_BATCH_TIMEOUT" env-required:"true"`
}

type OutboxConfig struct {
	PollInterval time.Duration `yaml:"poll_interval" env:"OUTBOX_POLL_INTERVAL" env-required:"true"`
	BatchSize    int           `yaml:"batch_size" env:"OUTBOX_BATCH_SIZE" env-required:"true"`
}

func Load() (Config, error) {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		return Config{}, errConfigPathNotSet
	}

	cfg, err := LoadFromFile(configPath)
	if err != nil {
		return Config{}, err
	}

	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return Config{}, fmt.Errorf("read env: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

func LoadFromFile(file string) (Config, error) {
	if _, err := os.Stat(file); os.IsNotExist(err) { //nolint:gosec // path comes from CONFIG_PATH
		return Config{}, fmt.Errorf("%w: %s", errConfigNotExists, file)
	}

	var cfg Config
	if err := cleanenv.ReadConfig(file, &cfg); err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

func (c Config) Validate() error {
	if err := c.HTTPServer.Validate(); err != nil {
		return fmt.Errorf("http server: %w", err)
	}
	if err := c.Postgres.Validate(); err != nil {
		return fmt.Errorf("postgres: %w", err)
	}
	if err := c.Feed.Validate(); err != nil {
		return fmt.Errorf("feed: %w", err)
	}
	if err := c.Redis.Validate(); err != nil {
		return fmt.Errorf("redis: %w", err)
	}
	if err := c.Kafka.Validate(); err != nil {
		return fmt.Errorf("kafka: %w", err)
	}
	if err := c.Outbox.Validate(); err != nil {
		return fmt.Errorf("outbox: %w", err)
	}
	if c.ShutdownTimeout <= 0 {
		return errInvalidShutdownTimeout
	}

	return nil
}

func (c HTTPServerConfig) Validate() error {
	if c.Port == "" {
		return errEmptyPort
	}
	if c.Timeout <= 0 {
		return errInvalidTimeout
	}
	if c.IdleTimeout <= 0 {
		return errInvalidIdleTimeout
	}
	if c.MaxBodyBytes <= 0 {
		return errInvalidMaxBodyBytes
	}

	return nil
}

func (c PostgresConfig) Validate() error {
	if err := c.validateConnection(); err != nil {
		return err
	}
	if _, err := c.ReadAddresses(); err != nil {
		return err
	}

	return c.validatePool()
}

// ReadAddresses разбирает ReadHosts (адреса реплик через запятую, host:port).
// Пустое значение означает, что чтение идёт на мастер.
func (c PostgresConfig) ReadAddresses() ([]string, error) {
	if strings.TrimSpace(c.ReadHosts) == "" {
		return nil, nil
	}

	parts := strings.Split(c.ReadHosts, ",")
	addresses := make([]string, 0, len(parts))
	for _, part := range parts {
		address := strings.TrimSpace(part)
		host, port, err := net.SplitHostPort(address)
		if err != nil || host == "" || port == "" {
			return nil, fmt.Errorf("%w: %q", errInvalidReadHost, address)
		}
		addresses = append(addresses, address)
	}

	return addresses, nil
}

// WithAddress возвращает копию конфига, указывающую на другой host:port той же базы.
func (c PostgresConfig) WithAddress(address string) PostgresConfig {
	host, port, _ := net.SplitHostPort(address)
	c.Host = host
	c.Port = port

	return c
}

func (c PostgresConfig) DSN() string {
	postgresURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Password),
		Host:   net.JoinHostPort(c.Host, c.Port),
		Path:   "/" + c.Database,
	}
	query := postgresURL.Query()
	query.Set("sslmode", "disable")
	query.Set("application_name", "highloadarchitect")
	postgresURL.RawQuery = query.Encode()

	return postgresURL.String()
}

func (c FeedConfig) Validate() error {
	switch {
	case c.CacheLimit <= 0:
		return errInvalidFeedCacheLimit
	case c.WarmLimit <= 0:
		return errInvalidFeedWarmLimit
	case c.TTL <= 0:
		return errInvalidFeedTTL
	case c.PostTTL <= 0:
		return errInvalidFeedPostTTL
	case c.EmptyMarkerTTL <= 0:
		return errInvalidFeedMarkerTTL
	default:
		return nil
	}
}

func (c RedisConfig) Validate() error {
	switch {
	case c.Addr == "":
		return errEmptyRedisAddr
	case c.DialTimeout <= 0, c.ReadTimeout <= 0, c.WriteTimeout <= 0:
		return errInvalidRedisTimeout
	case c.PoolSize <= 0:
		return errInvalidRedisPoolSize
	default:
		return nil
	}
}

func (c KafkaConfig) Validate() error {
	if _, err := c.BrokersList(); err != nil {
		return err
	}

	switch {
	case c.Topic == "":
		return errEmptyKafkaTopic
	case c.GroupID == "":
		return errEmptyKafkaGroupID
	case c.BatchSize <= 0:
		return errInvalidKafkaBatchSize
	case c.BatchTimeout <= 0:
		return errInvalidKafkaBatchWait
	default:
		return nil
	}
}

// BrokersList разбирает Brokers (адреса через запятую).
func (c KafkaConfig) BrokersList() ([]string, error) {
	if strings.TrimSpace(c.Brokers) == "" {
		return nil, errEmptyKafkaBrokers
	}

	parts := strings.Split(c.Brokers, ",")
	brokers := make([]string, 0, len(parts))
	for _, part := range parts {
		broker := strings.TrimSpace(part)
		if broker == "" {
			return nil, fmt.Errorf("%w: %q", errEmptyKafkaBroker, c.Brokers)
		}
		brokers = append(brokers, broker)
	}

	return brokers, nil
}

func (c OutboxConfig) Validate() error {
	switch {
	case c.PollInterval <= 0:
		return errInvalidOutboxPollInterval
	case c.BatchSize <= 0:
		return errInvalidOutboxBatchSize
	default:
		return nil
	}
}

func (c PostgresConfig) validateConnection() error {
	switch {
	case c.Host == "":
		return errEmptyHost
	case c.Port == "":
		return errEmptyPort
	case c.User == "":
		return errEmptyUser
	case c.Password == "":
		return errEmptyPassword
	case c.Database == "":
		return errEmptyDatabase
	default:
		return nil
	}
}

func (c PostgresConfig) validatePool() error {
	switch {
	case c.MaxConns <= 0:
		return errInvalidMaxConns
	case c.MaxConnLifetime <= 0:
		return errInvalidMaxConnLifetime
	case c.MaxConnIdleTime <= 0:
		return errInvalidMaxConnIdleTime
	default:
		return nil
	}
}
