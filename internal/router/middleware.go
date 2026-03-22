package router

import (
	"log/slog"
	"net/http"
	"strings"
)

// APIKeyAuth returns middleware that validates API key from the Authorization
// header (Bearer token) or X-API-Key header. The /health endpoint is always
// allowed without authentication. If apiKeys is empty, authentication is
// disabled (pass-through).
func APIKeyAuth(apiKeys []string, logger *slog.Logger) func(http.Handler) http.Handler {
	if len(apiKeys) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	allowed := make(map[string]struct{}, len(apiKeys))
	for _, k := range apiKeys {
		allowed[k] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Health endpoint is unauthenticated.
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			key := extractAPIKey(r)
			if key == "" {
				logger.Warn("missing API key", "path", r.URL.Path, "remote", r.RemoteAddr)
				http.Error(w, `{"code":401,"message":"missing API key"}`, http.StatusUnauthorized)
				return
			}

			if _, ok := allowed[key]; !ok {
				logger.Warn("invalid API key", "path", r.URL.Path, "remote", r.RemoteAddr)
				http.Error(w, `{"code":403,"message":"invalid API key"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractAPIKey reads the API key from Authorization (Bearer) or X-API-Key.
func extractAPIKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
	}
	return r.Header.Get("X-API-Key")
}

// MaxBodySize returns middleware that limits request body size to the given
// number of bytes. If maxBytes is 0, no limit is applied.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	if maxBytes <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}
