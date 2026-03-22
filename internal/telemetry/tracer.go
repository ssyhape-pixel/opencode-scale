package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// InitTracer creates a TracerProvider. If otlpEndpoint is empty, a provider
// with no exporter is returned (traces are discarded). Otherwise an OTLP/gRPC
// exporter is configured that sends spans to the given endpoint.
func InitTracer(ctx context.Context, serviceName, otlpEndpoint string) (*sdktrace.TracerProvider, error) {
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion("0.1.0"),
	)

	if otlpEndpoint == "" {
		return sdktrace.NewTracerProvider(sdktrace.WithResource(res)), nil
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(otlpEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	return tp, nil
}
