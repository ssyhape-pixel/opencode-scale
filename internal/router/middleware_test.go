package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIKeyAuth_NoKeys_PassThrough(t *testing.T) {
	handler := APIKeyAuth(nil, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/123", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with no keys configured, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_HealthBypass(t *testing.T) {
	handler := APIKeyAuth([]string{"secret"}, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /health without key, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_MissingKey(t *testing.T) {
	handler := APIKeyAuth([]string{"secret"}, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/123", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without key, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_InvalidKey(t *testing.T) {
	handler := APIKeyAuth([]string{"secret"}, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/123", nil)
	req.Header.Set("X-API-Key", "wrong")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with wrong key, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_ValidBearer(t *testing.T) {
	handler := APIKeyAuth([]string{"my-secret"}, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/123", nil)
	req.Header.Set("Authorization", "Bearer my-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid bearer, got %d", rec.Code)
	}
}

func TestAPIKeyAuth_ValidXAPIKey(t *testing.T) {
	handler := APIKeyAuth([]string{"key-1", "key-2"}, testLogger())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/123", nil)
	req.Header.Set("X-API-Key", "key-2")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid X-API-Key, got %d", rec.Code)
	}
}

func TestMaxBodySize_Enforced(t *testing.T) {
	handler := MaxBodySize(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	// Body within limit.
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("short"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for small body, got %d", rec.Code)
	}

	// Body exceeding limit.
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("this body is way too long"))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for large body, got %d", rec.Code)
	}
}

func TestMaxBodySize_Zero_NoLimit(t *testing.T) {
	handler := MaxBodySize(0)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	bigBody := strings.Repeat("x", 10_000_000) // 10MB
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(bigBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with no limit, got %d", rec.Code)
	}
}
