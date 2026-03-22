package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyForward(t *testing.T) {
	// Start a backend that echoes back the request method, path, and a custom header.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "reached")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "method=%s path=%s custom=%s", r.Method, r.URL.Path, r.Header.Get("X-Custom"))
	}))
	defer backend.Close()

	backendAddr := strings.TrimPrefix(backend.URL, "http://")
	logger := testLogger()
	proxy := NewProxy(logger)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/exec?foo=bar", nil)
	req.Header.Set("X-Custom", "test-value")

	rec := httptest.NewRecorder()
	proxy.Forward(rec, req, backendAddr)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "method=POST") {
		t.Fatalf("expected POST method in body, got %q", body)
	}
	if !strings.Contains(body, "path=/api/v1/exec") {
		t.Fatalf("expected path /api/v1/exec in body, got %q", body)
	}
	if !strings.Contains(body, "custom=test-value") {
		t.Fatalf("expected custom header in body, got %q", body)
	}

	if rec.Header().Get("X-Backend") != "reached" {
		t.Fatal("expected X-Backend response header from backend")
	}
}
