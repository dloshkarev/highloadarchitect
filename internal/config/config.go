package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
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
)

type Config struct {
	HTTPServer      HTTPServerConfig `yaml:"http-server"`
	Postgres        PostgresConfig   `yaml:"postgres"`
	ShutdownTimeout time.Duration    `yaml:"shutdown_timeout" env:"SHUTDOWN_TIMEOUT" env-required:"true"`
}

type HTTPServerConfig struct {
	Port         string        `yaml:"port" env:"HTTP_SERVER_PORT" env-required:"true"`
	Timeout      time.Duration `yaml:"read_write_timeout" env:"HTTP_READ_WRITE_TIMEOUT" env-required:"true"`
	IdleTimeout  time.Duration `yaml:"idle_timeout" env:"HTTP_IDLE_TIMEOUT" env-required:"true"`
	MaxBodyBytes int64         `yaml:"max_body_bytes" env:"HTTP_MAX_BODY_BYTES" env-required:"true"`
}

type PostgresConfig struct {
	Host            string        `yaml:"host" env:"POSTGRES_HOST" env-required:"true"`
	Port            string        `yaml:"port" env:"POSTGRES_PORT" env-required:"true"`
	User            string        `yaml:"user" env:"POSTGRES_USER" env-required:"true"`
	Password        string        `yaml:"password" env:"POSTGRES_PASSWORD" env-required:"true"`
	Database        string        `yaml:"database" env:"POSTGRES_DB" env-required:"true"`
	MaxConns        int32         `yaml:"max_conns" env:"POSTGRES_MAX_CONNS" env-required:"true"`
	MaxConnLifetime time.Duration `yaml:"max_conn_lifetime" env:"POSTGRES_MAX_CONN_LIFETIME" env-required:"true"`
	MaxConnIdleTime time.Duration `yaml:"max_conn_idle_time" env:"POSTGRES_MAX_CONN_IDLE_TIME" env-required:"true"`
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

	return c.validatePool()
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
