package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"go.opentelemetry.io/otel"
)

// TelemetryConfig holds configuration for telemetry initialization.
type TelemetryConfig struct {
	ServiceName    string
	OTelEndpoint   string
	PrometheusPort int
	LogLevel       string
}

// Setup initialises all telemetry sub-systems (tracer, meter, logger) and
// returns a shutdown function that flushes and stops providers gracefully.
func Setup(ctx context.Context, cfg TelemetryConfig) (shutdown func(context.Context) error, err error) {
	// 1. Tracer
	tp, err := InitTracer(ctx, cfg.ServiceName, cfg.OTelEndpoint)
	if err != nil {
		return nil, fmt.Errorf("init tracer: %w", err)
	}
	otel.SetTracerProvider(tp)

	// 2. Meter
	mp, metricsHandler, err := InitMeter(cfg.ServiceName)
	if err != nil {
		return nil, fmt.Errorf("init meter: %w", err)
	}
	otel.SetMeterProvider(mp)

	// 3. Logger
	logger := InitLogger(cfg.ServiceName)
	slog.SetDefault(logger)

	// 4. Prometheus HTTP server
	if cfg.PrometheusPort > 0 {
		mux := http.NewServeMux()
		mux.Handle("/metrics", metricsHandler)
		srv := &http.Server{
			Addr:    fmt.Sprintf(":%d", cfg.PrometheusPort),
			Handler: mux,
		}
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("prometheus server error", "error", err)
			}
		}()
	}

	// 5. Shutdown function
	shutdown = func(ctx context.Context) error {
		tpErr := tp.Shutdown(ctx)
		mpErr := mp.Shutdown(ctx)
		if tpErr != nil {
			return tpErr
		}
		return mpErr
	}

	return shutdown, nil
}
