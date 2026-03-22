package telemetry

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// InitMeter creates a MeterProvider backed by a Prometheus exporter. The
// returned http.Handler serves Prometheus metrics on a /metrics endpoint.
func InitMeter(serviceName string) (*sdkmetric.MeterProvider, http.Handler, error) {
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion("0.1.0"),
	)

	exporter, err := prometheus.New()
	if err != nil {
		return nil, nil, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(res),
	)
	return mp, promhttp.Handler(), nil
}
