package telemetry

import (
	"log/slog"
	"os"
)

// InitLogger creates a structured JSON logger with a default service.name
// attribute.
func InitLogger(serviceName string) *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{})
	return slog.New(handler).With("service.name", serviceName)
}
