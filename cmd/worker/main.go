package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/opencode-scale/opencode-scale/internal/config"
	"github.com/opencode-scale/opencode-scale/internal/pool"
	"github.com/opencode-scale/opencode-scale/internal/schema"
	"github.com/opencode-scale/opencode-scale/internal/telemetry"
	"github.com/opencode-scale/opencode-scale/internal/workflow"
	"go.opentelemetry.io/otel"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
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
		ServiceName:    cfg.Telemetry.ServiceName + "-worker",
		OTelEndpoint:   cfg.Telemetry.OTelEndpoint,
		PrometheusPort: cfg.Telemetry.PrometheusPort,
		LogLevel:       cfg.Telemetry.LogLevel,
	}
	shutdownTel, err := telemetry.Setup(ctx, telCfg)
	if err != nil {
		slog.Error("failed to setup telemetry", "error", err)
		os.Exit(1)
	}

	// Create pool manager based on mode.
	meter := otel.Meter("opencode-scale-worker")
	poolMetrics, err := pool.NewPoolMetrics(meter)
	if err != nil {
		slog.Error("failed to create pool metrics", "error", err)
		os.Exit(1)
	}

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
		slog.Info("worker using k8s sandbox provider", "namespace", cfg.Pool.Namespace)
	default:
		provider = pool.NewMockSandboxProvider(cfg.Pool.MockTarget)
		slog.Info("worker using mock sandbox provider", "target", cfg.Pool.MockTarget)
	}

	cache := pool.NewAllocationCache()
	pm := pool.NewPoolManager(provider, cfg.Pool.MaxSize, cfg.Pool.Namespace, cache, poolMetrics)

	// Create workflow metrics.
	wfMetrics, err := workflow.NewWorkflowMetrics(meter)
	if err != nil {
		slog.Error("failed to create workflow metrics", "error", err)
		os.Exit(1)
	}

	// Create Temporal client.
	tc, err := client.Dial(client.Options{
		HostPort:  cfg.Temporal.HostPort,
		Namespace: cfg.Temporal.Namespace,
	})
	if err != nil {
		slog.Error("failed to create temporal client", "error", err)
		os.Exit(1)
	}
	defer tc.Close()

	// Create and configure Temporal worker.
	w := worker.New(tc, cfg.Temporal.TaskQueue, worker.Options{})

	// Register workflows and activities with real dependencies.
	activities := &workflow.Activities{
		Pool:      pm,
		Validator: schema.NewValidator(),
		Metrics:   wfMetrics,
	}

	w.RegisterWorkflow(workflow.CodingTaskWorkflow)
	w.RegisterActivity(activities)

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("starting temporal worker", "taskQueue", cfg.Temporal.TaskQueue, "mode", cfg.Pool.Mode)
		if err := w.Run(worker.InterruptCh()); err != nil {
			slog.Error("temporal worker error", "error", err)
			os.Exit(1)
		}
	}()

	sig := <-sigCh
	slog.Info("received signal, shutting down", "signal", sig)
	cancel()

	w.Stop()

	if err := shutdownTel(context.Background()); err != nil {
		slog.Error("telemetry shutdown error", "error", err)
	}

	slog.Info("worker stopped")
}
