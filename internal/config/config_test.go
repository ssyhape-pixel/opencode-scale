package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Pool.MinReady != 3 {
		t.Errorf("Pool.MinReady = %d, want 3", cfg.Pool.MinReady)
	}
	if cfg.Pool.MaxSize != 50 {
		t.Errorf("Pool.MaxSize = %d, want 50", cfg.Pool.MaxSize)
	}
	if cfg.Pool.IdleTimeout != 10*time.Minute {
		t.Errorf("Pool.IdleTimeout = %v, want 10m", cfg.Pool.IdleTimeout)
	}
	if cfg.Pool.GCInterval != 30*time.Second {
		t.Errorf("Pool.GCInterval = %v, want 30s", cfg.Pool.GCInterval)
	}
	if cfg.Router.ListenAddr != ":8080" {
		t.Errorf("Router.ListenAddr = %q, want %q", cfg.Router.ListenAddr, ":8080")
	}
	if cfg.Router.SessionHeader != "X-Session-ID" {
		t.Errorf("Router.SessionHeader = %q, want %q", cfg.Router.SessionHeader, "X-Session-ID")
	}
	if cfg.Temporal.HostPort != "localhost:7233" {
		t.Errorf("Temporal.HostPort = %q, want %q", cfg.Temporal.HostPort, "localhost:7233")
	}
	if cfg.Temporal.Namespace != "opencode-scale" {
		t.Errorf("Temporal.Namespace = %q, want %q", cfg.Temporal.Namespace, "opencode-scale")
	}
	if cfg.Temporal.TaskQueue != "coding-tasks" {
		t.Errorf("Temporal.TaskQueue = %q, want %q", cfg.Temporal.TaskQueue, "coding-tasks")
	}
	if cfg.Telemetry.PrometheusPort != 9090 {
		t.Errorf("Telemetry.PrometheusPort = %d, want 9090", cfg.Telemetry.PrometheusPort)
	}
	if cfg.LiteLLM.Endpoint != "http://localhost:4000" {
		t.Errorf("LiteLLM.Endpoint = %q, want %q", cfg.LiteLLM.Endpoint, "http://localhost:4000")
	}
}

func TestLoad(t *testing.T) {
	yaml := `
pool:
  minReady: 5
  maxSize: 100
  idleTimeout: 20m
router:
  listenAddr: ":9090"
temporal:
  hostPort: "temporal:7233"
  namespace: "test-ns"
  taskQueue: "test-queue"
litellm:
  endpoint: "http://litellm:4000"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Pool.MinReady != 5 {
		t.Errorf("Pool.MinReady = %d, want 5", cfg.Pool.MinReady)
	}
	if cfg.Pool.MaxSize != 100 {
		t.Errorf("Pool.MaxSize = %d, want 100", cfg.Pool.MaxSize)
	}
	if cfg.Pool.IdleTimeout != 20*time.Minute {
		t.Errorf("Pool.IdleTimeout = %v, want 20m", cfg.Pool.IdleTimeout)
	}
	if cfg.Router.ListenAddr != ":9090" {
		t.Errorf("Router.ListenAddr = %q, want %q", cfg.Router.ListenAddr, ":9090")
	}
	if cfg.Temporal.HostPort != "temporal:7233" {
		t.Errorf("Temporal.HostPort = %q, want %q", cfg.Temporal.HostPort, "temporal:7233")
	}
	// SessionHeader should keep default since not overridden
	if cfg.Router.SessionHeader != "X-Session-ID" {
		t.Errorf("Router.SessionHeader = %q, want default %q", cfg.Router.SessionHeader, "X-Session-ID")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("Load() with missing file should return error")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	// Use invalid YAML that will fail validation (maxSize=0)
	if err := os.WriteFile(path, []byte("pool:\n  maxSize: 0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Error("Load() with invalid config should return error")
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	cfg := DefaultConfig()

	t.Setenv("POOL_MIN_READY", "10")
	t.Setenv("POOL_MAX_SIZE", "200")
	t.Setenv("POOL_IDLE_TIMEOUT", "5m")
	t.Setenv("POOL_NAMESPACE", "custom-ns")
	t.Setenv("ROUTER_LISTEN_ADDR", ":3000")
	t.Setenv("TEMPORAL_HOST_PORT", "temporal.prod:7233")
	t.Setenv("TEMPORAL_NAMESPACE", "prod-ns")
	t.Setenv("TEMPORAL_TASK_QUEUE", "prod-queue")
	t.Setenv("OTEL_ENDPOINT", "otel:4317")
	t.Setenv("PROMETHEUS_PORT", "8888")
	t.Setenv("LITELLM_ENDPOINT", "http://litellm.prod:4000")
	t.Setenv("LITELLM_API_KEY", "sk-test-key")

	applyEnvOverrides(cfg)

	if cfg.Pool.MinReady != 10 {
		t.Errorf("Pool.MinReady = %d, want 10", cfg.Pool.MinReady)
	}
	if cfg.Pool.MaxSize != 200 {
		t.Errorf("Pool.MaxSize = %d, want 200", cfg.Pool.MaxSize)
	}
	if cfg.Pool.IdleTimeout != 5*time.Minute {
		t.Errorf("Pool.IdleTimeout = %v, want 5m", cfg.Pool.IdleTimeout)
	}
	if cfg.Pool.Namespace != "custom-ns" {
		t.Errorf("Pool.Namespace = %q, want %q", cfg.Pool.Namespace, "custom-ns")
	}
	if cfg.Router.ListenAddr != ":3000" {
		t.Errorf("Router.ListenAddr = %q, want %q", cfg.Router.ListenAddr, ":3000")
	}
	if cfg.Temporal.HostPort != "temporal.prod:7233" {
		t.Errorf("Temporal.HostPort = %q, want %q", cfg.Temporal.HostPort, "temporal.prod:7233")
	}
	if cfg.Temporal.Namespace != "prod-ns" {
		t.Errorf("Temporal.Namespace = %q, want %q", cfg.Temporal.Namespace, "prod-ns")
	}
	if cfg.Temporal.TaskQueue != "prod-queue" {
		t.Errorf("Temporal.TaskQueue = %q, want %q", cfg.Temporal.TaskQueue, "prod-queue")
	}
	if cfg.Telemetry.OTelEndpoint != "otel:4317" {
		t.Errorf("Telemetry.OTelEndpoint = %q, want %q", cfg.Telemetry.OTelEndpoint, "otel:4317")
	}
	if cfg.Telemetry.PrometheusPort != 8888 {
		t.Errorf("Telemetry.PrometheusPort = %d, want 8888", cfg.Telemetry.PrometheusPort)
	}
	if cfg.LiteLLM.Endpoint != "http://litellm.prod:4000" {
		t.Errorf("LiteLLM.Endpoint = %q, want %q", cfg.LiteLLM.Endpoint, "http://litellm.prod:4000")
	}
	if cfg.LiteLLM.APIKey != "sk-test-key" {
		t.Errorf("LiteLLM.APIKey = %q, want %q", cfg.LiteLLM.APIKey, "sk-test-key")
	}
}

func TestApplyEnvOverrides_InvalidValues(t *testing.T) {
	cfg := DefaultConfig()

	t.Setenv("POOL_MIN_READY", "not-a-number")
	t.Setenv("POOL_IDLE_TIMEOUT", "not-a-duration")

	applyEnvOverrides(cfg)

	// Should keep defaults when env values are invalid
	if cfg.Pool.MinReady != 3 {
		t.Errorf("Pool.MinReady = %d, want default 3 (invalid env)", cfg.Pool.MinReady)
	}
	if cfg.Pool.IdleTimeout != 10*time.Minute {
		t.Errorf("Pool.IdleTimeout = %v, want default 10m (invalid env)", cfg.Pool.IdleTimeout)
	}
}

func TestValidate_Valid(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() on defaults = %v, want nil", err)
	}
}

func TestValidate_InvalidMinReady(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Pool.MinReady = -1
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() with MinReady=-1 should return error")
	}
}

func TestValidate_InvalidMaxSize(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Pool.MaxSize = 0
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() with MaxSize=0 should return error")
	}
}

func TestValidate_MinReadyExceedsMaxSize(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Pool.MinReady = 10
	cfg.Pool.MaxSize = 5
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() with MinReady > MaxSize should return error")
	}
}

func TestValidate_EmptyListenAddr(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Router.ListenAddr = ""
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() with empty ListenAddr should return error")
	}
}

func TestValidate_EmptyTemporalHostPort(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Temporal.HostPort = ""
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() with empty Temporal.HostPort should return error")
	}
}

func TestValidate_EmptyTemporalNamespace(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Temporal.Namespace = ""
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() with empty Temporal.Namespace should return error")
	}
}

func TestValidate_EmptyTaskQueue(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Temporal.TaskQueue = ""
	if err := cfg.Validate(); err == nil {
		t.Error("Validate() with empty Temporal.TaskQueue should return error")
	}
}
