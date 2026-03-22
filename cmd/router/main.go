package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	v1 "github.com/opencode-scale/opencode-scale/api/v1"
	"github.com/opencode-scale/opencode-scale/internal/config"
	"github.com/opencode-scale/opencode-scale/internal/pool"
	"github.com/opencode-scale/opencode-scale/internal/router"
	"github.com/opencode-scale/opencode-scale/internal/telemetry"
	"go.opentelemetry.io/otel"
	temporalclient "go.temporal.io/sdk/client"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to configuration file")
	kubeconfig := flag.String("kubeconfig", "", "path to kubeconfig (uses in-cluster if empty)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialise telemetry.
	telCfg := telemetry.TelemetryConfig{
		ServiceName:    cfg.Telemetry.ServiceName,
		OTelEndpoint:   cfg.Telemetry.OTelEndpoint,
		PrometheusPort: cfg.Telemetry.PrometheusPort,
		LogLevel:       cfg.Telemetry.LogLevel,
	}
	shutdownTel, err := telemetry.Setup(ctx, telCfg)
	if err != nil {
		slog.Error("failed to setup telemetry", "error", err)
		os.Exit(1)
	}

	// Create pool metrics.
	meter := otel.Meter("opencode-scale-router")
	poolMetrics, err := pool.NewPoolMetrics(meter)
	if err != nil {
		slog.Error("failed to create pool metrics", "error", err)
		os.Exit(1)
	}

	// Create sandbox provider based on mode.
	var provider pool.SandboxProvider
	switch cfg.Pool.Mode {
	case "k8s":
		var restCfg *rest.Config
		if *kubeconfig != "" {
			restCfg, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
		} else {
			restCfg, err = rest.InClusterConfig()
		}
		if err != nil {
			slog.Error("failed to get kubernetes config", "error", err)
			os.Exit(1)
		}
		dynClient, err := dynamic.NewForConfig(restCfg)
		if err != nil {
			slog.Error("failed to create dynamic client", "error", err)
			os.Exit(1)
		}
		provider = pool.NewK8sSandboxProvider(dynClient, cfg.Pool.Namespace, cfg.Pool.WarmPoolName, 4096)
		slog.Info("using k8s sandbox provider", "namespace", cfg.Pool.Namespace)
	default:
		provider = pool.NewMockSandboxProvider(cfg.Pool.MockTarget)
		slog.Info("using mock sandbox provider", "target", cfg.Pool.MockTarget)
	}

	cache := pool.NewAllocationCache()
	pm := pool.NewPoolManager(provider, cfg.Pool.MaxSize, cfg.Pool.Namespace, cache, poolMetrics)

	// Report pool size metric.
	poolMetrics.SetPoolSize(ctx, int64(cfg.Pool.MaxSize))

	// Start GC loop to reclaim idle sandboxes.
	go pm.StartGCLoop(ctx, cfg.Pool.GCInterval, cfg.Pool.IdleTimeout)

	// Start warm pool pre-allocation (k8s mode only).
	if cfg.Pool.Mode == "k8s" && cfg.Pool.MinReady > 0 {
		go pm.StartWarmPool(ctx, cfg.Pool.MinReady, cfg.Pool.GCInterval)
	}

	// Create router.
	logger := slog.Default()
	ext := router.SessionExtractor{
		Header:     cfg.Router.SessionHeader,
		Cookie:     cfg.Router.SessionCookie,
		QueryParam: cfg.Router.SessionQuery,
	}
	r := router.NewRouter(pm, cache, ext, logger)

	// Connect Temporal client for task API.
	tc, err := temporalclient.Dial(temporalclient.Options{
		HostPort:  cfg.Temporal.HostPort,
		Namespace: cfg.Temporal.Namespace,
	})
	if err != nil {
		slog.Warn("temporal client unavailable, task API disabled", "error", err)
	}
	if tc != nil {
		defer tc.Close()
	}

	// Set up HTTP mux.
	mux := http.NewServeMux()
	if tc != nil {
		taskHandler := router.NewTaskHandler(tc, cfg.Temporal.TaskQueue, logger)
		mux.Handle("/api/v1/tasks", taskHandler)
		mux.Handle("/api/v1/tasks/", taskHandler)
	}
	mux.Handle("/api/v1/", r)
	mux.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		stats := pm.Stats()
		utilization := 0.0
		if stats.MaxSize > 0 {
			utilization = float64(stats.Allocated) / float64(stats.MaxSize)
		}
		resp := v1.HealthResponse{
			Status:          "ok",
			Version:         "0.1.0",
			PoolUtilization: utilization,
			PoolAllocated:   stats.Allocated,
			PoolMaxSize:     stats.MaxSize,
			QueueDepth:      0,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Apply middleware (innermost → outermost):
	// 1. body size limit  2. API key auth  3. audit log
	var handler http.Handler = mux
	handler = router.MaxBodySize(cfg.Router.MaxBodyBytes)(handler)
	handler = router.APIKeyAuth(cfg.Router.APIKeys, logger)(handler)
	handler = router.AuditLog(logger)(handler)

	srv := &http.Server{
		Addr:    cfg.Router.ListenAddr,
		Handler: handler,
	}

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("router listening", "addr", cfg.Router.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "error", err)
			os.Exit(1)
		}
	}()

	sig := <-sigCh
	slog.Info("received signal, shutting down", "signal", sig)
	cancel()

	if err := srv.Shutdown(context.Background()); err != nil {
		slog.Error("http server shutdown error", "error", err)
	}
	if err := shutdownTel(context.Background()); err != nil {
		slog.Error("telemetry shutdown error", "error", err)
	}

	slog.Info("router stopped")
}
