package router

import (
	"log/slog"
	"net/http"
	"time"
)

// AuditLog returns middleware that logs every request with method, path,
// status code, duration, and client IP for compliance and debugging.
func AuditLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			logger.Info("audit",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
				"user_id", extractUserID(r),
			)
		})
	}
}

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap returns the underlying ResponseWriter, allowing http.Flusher
// and other interface assertions to work through the wrapper.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
