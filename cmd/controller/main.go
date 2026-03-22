package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/opencode-scale/opencode-scale/internal/config"
	"github.com/opencode-scale/opencode-scale/internal/controller"
	"github.com/opencode-scale/opencode-scale/internal/telemetry"
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
		ServiceName:    cfg.Telemetry.ServiceName + "-controller",
		OTelEndpoint:   cfg.Telemetry.OTelEndpoint,
		PrometheusPort: cfg.Telemetry.PrometheusPort,
		LogLevel:       cfg.Telemetry.LogLevel,
	}
	shutdownTel, err := telemetry.Setup(ctx, telCfg)
	if err != nil {
		slog.Error("failed to setup telemetry", "error", err)
		os.Exit(1)
	}

	// Build K8s client.
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

	// Create and start reconciler.
	logger := slog.Default()
	reconciler := controller.NewSandboxReconciler(dynClient, cfg.Pool.Namespace, logger,
		controller.WithGCTimeout(cfg.Pool.IdleTimeout),
	)

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	slog.Info("starting controller", "namespace", cfg.Pool.Namespace)
	if err := reconciler.Start(ctx); err != nil {
		slog.Error("controller error", "error", err)
	}

	if err := shutdownTel(context.Background()); err != nil {
		slog.Error("telemetry shutdown error", "error", err)
	}

	slog.Info("controller stopped")
}
