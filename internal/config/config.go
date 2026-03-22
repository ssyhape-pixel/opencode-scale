package config

// Unified configuration for opencode-scale.
// Supports loading from YAML file + environment variable overrides.

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Pool      PoolConfig      `yaml:"pool"`
	Router    RouterConfig    `yaml:"router"`
	Temporal  TemporalConfig  `yaml:"temporal"`
	Telemetry TelemetryConfig `yaml:"telemetry"`
	LiteLLM   LiteLLMConfig   `yaml:"litellm"`
}

type PoolConfig struct {
	MinReady     int           `yaml:"minReady"`
	MaxSize      int           `yaml:"maxSize"`
	IdleTimeout  time.Duration `yaml:"idleTimeout"`
	GCInterval   time.Duration `yaml:"gcInterval"`
	WarmPoolName string        `yaml:"warmPoolName"`
	Namespace    string        `yaml:"namespace"`
	Mode         string        `yaml:"mode"`       // "local" or "k8s", defaults to "local"
	MockTarget   string        `yaml:"mockTarget"` // Target address for mock provider (local mode)
}

type RouterConfig struct {
	ListenAddr    string   `yaml:"listenAddr"`
	SessionHeader string   `yaml:"sessionHeader"`
	SessionCookie string   `yaml:"sessionCookie"`
	SessionQuery  string   `yaml:"sessionQuery"`
	APIKeys       []string `yaml:"apiKeys"`       // Empty = auth disabled
	MaxBodyBytes  int64    `yaml:"maxBodyBytes"`   // 0 = no limit
}

type TemporalConfig struct {
	HostPort  string `yaml:"hostPort"`
	Namespace string `yaml:"namespace"`
	TaskQueue string `yaml:"taskQueue"`
}

type TelemetryConfig struct {
	ServiceName    string `yaml:"serviceName"`
	OTelEndpoint   string `yaml:"otelEndpoint"`
	PrometheusPort int    `yaml:"prometheusPort"`
	LogLevel       string `yaml:"logLevel"`
}

type LiteLLMConfig struct {
	Endpoint string `yaml:"endpoint"`
	APIKey   string `yaml:"apiKey"`
}

// DefaultConfig returns a Config populated with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Pool: PoolConfig{
			MinReady:     3,
			MaxSize:      50,
			IdleTimeout:  10 * time.Minute,
			GCInterval:   30 * time.Second,
			WarmPoolName: "default",
			Namespace:    "opencode-scale",
			Mode:         "local",
			MockTarget:   "localhost:4096",
		},
		Router: RouterConfig{
			ListenAddr:    ":8080",
			SessionHeader: "X-Session-ID",
			SessionCookie: "session_id",
			SessionQuery:  "session_id",
			MaxBodyBytes:  1 << 20, // 1 MB
		},
		Temporal: TemporalConfig{
			HostPort:  "localhost:7233",
			Namespace: "opencode-scale",
			TaskQueue: "coding-tasks",
		},
		Telemetry: TelemetryConfig{
			ServiceName:    "opencode-scale",
			OTelEndpoint:   "localhost:4317",
			PrometheusPort: 9090,
			LogLevel:       "info",
		},
		LiteLLM: LiteLLMConfig{
			Endpoint: "http://localhost:4000",
		},
	}
}

// Load reads a YAML config file, merges it on top of defaults, and applies
// environment variable overrides.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	applyEnvOverrides(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

// applyEnvOverrides overwrites config fields with values from environment
// variables when they are set.
func applyEnvOverrides(cfg *Config) {
	// Pool
	if v, ok := getEnvInt("POOL_MIN_READY"); ok {
		cfg.Pool.MinReady = v
	}
	if v, ok := getEnvInt("POOL_MAX_SIZE"); ok {
		cfg.Pool.MaxSize = v
	}
	if v, ok := getEnvDuration("POOL_IDLE_TIMEOUT"); ok {
		cfg.Pool.IdleTimeout = v
	}
	if v := getEnv("POOL_NAMESPACE"); v != "" {
		cfg.Pool.Namespace = v
	}
	if v := getEnv("POOL_MODE"); v != "" {
		cfg.Pool.Mode = v
	}
	if v := getEnv("MOCK_OPENCODE_TARGET"); v != "" {
		cfg.Pool.MockTarget = v
	}

	// Router
	if v := getEnv("ROUTER_LISTEN_ADDR"); v != "" {
		cfg.Router.ListenAddr = v
	}
	if v := getEnv("API_KEYS"); v != "" {
		cfg.Router.APIKeys = strings.Split(v, ",")
	}
	if v, ok := getEnvInt("MAX_BODY_BYTES"); ok {
		cfg.Router.MaxBodyBytes = int64(v)
	}

	// Temporal
	if v := getEnv("TEMPORAL_HOST_PORT"); v != "" {
		cfg.Temporal.HostPort = v
	}
	if v := getEnv("TEMPORAL_NAMESPACE"); v != "" {
		cfg.Temporal.Namespace = v
	}
	if v := getEnv("TEMPORAL_TASK_QUEUE"); v != "" {
		cfg.Temporal.TaskQueue = v
	}

	// Telemetry
	if v := getEnv("OTEL_ENDPOINT"); v != "" {
		cfg.Telemetry.OTelEndpoint = v
	}
	if v, ok := getEnvInt("PROMETHEUS_PORT"); ok {
		cfg.Telemetry.PrometheusPort = v
	}

	// LiteLLM
	if v := getEnv("LITELLM_ENDPOINT"); v != "" {
		cfg.LiteLLM.Endpoint = v
	}
	if v := getEnv("LITELLM_API_KEY"); v != "" {
		cfg.LiteLLM.APIKey = v
	}
}

// Validate checks that required configuration fields are present and
// constraints are satisfied.
func (c *Config) Validate() error {
	if c.Pool.MinReady < 0 {
		return fmt.Errorf("pool.minReady must be >= 0, got %d", c.Pool.MinReady)
	}
	if c.Pool.MaxSize < 1 {
		return fmt.Errorf("pool.maxSize must be >= 1, got %d", c.Pool.MaxSize)
	}
	if c.Pool.MinReady > c.Pool.MaxSize {
		return fmt.Errorf("pool.minReady (%d) must be <= pool.maxSize (%d)", c.Pool.MinReady, c.Pool.MaxSize)
	}
	if c.Router.ListenAddr == "" {
		return fmt.Errorf("router.listenAddr is required")
	}
	if c.Temporal.HostPort == "" {
		return fmt.Errorf("temporal.hostPort is required")
	}
	if c.Temporal.Namespace == "" {
		return fmt.Errorf("temporal.namespace is required")
	}
	if c.Temporal.TaskQueue == "" {
		return fmt.Errorf("temporal.taskQueue is required")
	}
	return nil
}

// --- helper functions for environment variable parsing ---

func getEnv(key string) string {
	return os.Getenv(key)
}

func getEnvInt(key string) (int, bool) {
	s := os.Getenv(key)
	if s == "" {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

func getEnvDuration(key string) (time.Duration, bool) {
	s := os.Getenv(key)
	if s == "" {
		return 0, false
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, false
	}
	return v, true
}
