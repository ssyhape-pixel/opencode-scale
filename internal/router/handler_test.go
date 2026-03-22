package router

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/opencode-scale/opencode-scale/internal/pool"
)

// ---------- mock SandboxProvider ----------

// mockSandboxProvider implements pool.SandboxProvider for testing.
type mockSandboxProvider struct {
	createFunc func(ctx context.Context) (string, string, error)
	deleteFunc func(ctx context.Context, name string) error
}

func (m *mockSandboxProvider) CreateSandbox(ctx context.Context) (string, string, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx)
	}
	return "sandbox-1", "sandbox-1.default.svc.cluster.local", nil
}

func (m *mockSandboxProvider) DeleteSandbox(ctx context.Context, name string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, name)
	}
	return nil
}

// ---------- helpers ----------

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func defaultExtractor() SessionExtractor {
	return SessionExtractor{
		Header:     "X-Session-ID",
		Cookie:     "session_id",
		QueryParam: "session_id",
	}
}

// ---------- extractSessionID tests ----------

func TestExtractSessionID_Header(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Session-ID", "header-session-123")

	got := extractSessionID(req, defaultExtractor())
	if got != "header-session-123" {
		t.Fatalf("expected %q, got %q", "header-session-123", got)
	}
}

func TestExtractSessionID_Cookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "cookie-session-456"})

	got := extractSessionID(req, defaultExtractor())
	if got != "cookie-session-456" {
		t.Fatalf("expected %q, got %q", "cookie-session-456", got)
	}
}

func TestExtractSessionID_QueryParam(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?session_id=query-session-789", nil)

	got := extractSessionID(req, defaultExtractor())
	if got != "query-session-789" {
		t.Fatalf("expected %q, got %q", "query-session-789", got)
	}
}

func TestExtractSessionID_Priority(t *testing.T) {
	// Header should win over cookie and query param.
	req := httptest.NewRequest(http.MethodGet, "/?session_id=from-query", nil)
	req.Header.Set("X-Session-ID", "from-header")
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "from-cookie"})

	got := extractSessionID(req, defaultExtractor())
	if got != "from-header" {
		t.Fatalf("expected header value %q, got %q", "from-header", got)
	}
}

func TestExtractSessionID_Empty(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	got := extractSessionID(req, defaultExtractor())
	if got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

// ---------- extractUserID tests ----------

func TestExtractUserID_WithHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-User-ID", "user-42")

	got := extractUserID(req)
	if got != "user-42" {
		t.Fatalf("expected %q, got %q", "user-42", got)
	}
}

func TestExtractUserID_Anonymous(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	got := extractUserID(req)
	if got != "anonymous" {
		t.Fatalf("expected %q, got %q", "anonymous", got)
	}
}

// ---------- Router.ServeHTTP tests ----------

func TestRouterServeHTTP_ExistingSession(t *testing.T) {
	// Start a mock backend that replies with 200 and a known body.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "backend-ok")
	}))
	defer backend.Close()

	cache := pool.NewAllocationCache()
	alloc := &pool.Allocation{
		SandboxName: "sb-1",
		ServiceFQDN: strings.TrimPrefix(backend.URL, "http://"),
		SessionID:   "existing-session",
		UserID:      "user-1",
		AllocatedAt: time.Now(),
		Status:      pool.StatusActive,
	}
	cache.Set(alloc)

	provider := &mockSandboxProvider{}
	pm := pool.NewPoolManager(provider, 10, "default", cache, nil)
	logger := testLogger()

	router := NewRouter(pm, cache, defaultExtractor(), logger)

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	req.Header.Set("X-Session-ID", "existing-session")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "backend-ok" {
		t.Fatalf("expected body %q, got %q", "backend-ok", body)
	}
}

func TestRouterServeHTTP_NewAllocation(t *testing.T) {
	// Start a mock backend that the proxy will forward to.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "new-sandbox-ok")
	}))
	defer backend.Close()

	backendAddr := strings.TrimPrefix(backend.URL, "http://")

	provider := &mockSandboxProvider{
		createFunc: func(ctx context.Context) (string, string, error) {
			return "sandbox-new", backendAddr, nil
		},
	}

	cache := pool.NewAllocationCache()
	pm := pool.NewPoolManager(provider, 10, "default", cache, nil)
	logger := testLogger()

	router := NewRouter(pm, cache, defaultExtractor(), logger)

	req := httptest.NewRequest(http.MethodGet, "/work", nil)
	req.Header.Set("X-User-ID", "user-99")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if body := rec.Body.String(); body != "new-sandbox-ok" {
		t.Fatalf("expected body %q, got %q", "new-sandbox-ok", body)
	}

	// The response should carry the X-Session-ID header set by handleAllocateAndProxy.
	if sid := rec.Header().Get("X-Session-ID"); sid == "" {
		t.Fatal("expected X-Session-ID header to be set on new allocation")
	}

	// Verify Set-Cookie header is present with session_id.
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "session_id" {
			found = true
			if c.HttpOnly != true {
				t.Fatal("expected session cookie to be HttpOnly")
			}
			if c.SameSite != http.SameSiteLaxMode {
				t.Fatalf("expected SameSite=Lax, got %v", c.SameSite)
			}
			break
		}
	}
	if !found {
		t.Fatal("expected Set-Cookie header with session_id")
	}
}

func TestRouterServeHTTP_PoolExhausted(t *testing.T) {
	// Create a pool with maxSize=0 so it is immediately exhausted.
	provider := &mockSandboxProvider{}
	cache := pool.NewAllocationCache()
	pm := pool.NewPoolManager(provider, 0, "default", cache, nil)
	logger := testLogger()

	router := NewRouter(pm, cache, defaultExtractor(), logger)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-User-ID", "user-blocked")

	rec := httptest.NewRecorder()

	// ServeHTTP will call WriteSSEQueuePosition which blocks until the entry
	// receives an allocation or the context is cancelled. We use a short-lived
	// context to prevent the test from hanging.
	ctx, cancel := context.WithTimeout(req.Context(), 200*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	router.ServeHTTP(rec, req)

	// The response should be an SSE stream (Content-Type: text/event-stream).
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected Content-Type text/event-stream, got %q", ct)
	}

	// The body should contain at least the initial position event.
	body := rec.Body.String()
	if !strings.Contains(body, "event: queue") {
		t.Fatalf("expected SSE queue event in body, got %q", body)
	}
	if !strings.Contains(body, "\"position\":1") {
		t.Fatalf("expected position 1 in body, got %q", body)
	}
}
