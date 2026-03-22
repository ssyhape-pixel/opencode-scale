package router

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/opencode-scale/opencode-scale/internal/pool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// SessionExtractor defines the request fields to check when extracting a
// session identifier.
type SessionExtractor struct {
	Header     string
	Cookie     string
	QueryParam string
}

// Router is the main HTTP handler that routes incoming requests to sandbox
// backends. It allocates sandboxes on demand and enqueues requests when the
// pool is exhausted.
type Router struct {
	pool      *pool.PoolManager
	cache     *pool.AllocationCache
	proxy     *Proxy
	queue     *WaitQueue
	extractor SessionExtractor
	logger    *slog.Logger
}

// NewRouter creates a Router with the given pool manager, allocation cache,
// session extractor settings, and logger.
func NewRouter(pm *pool.PoolManager, cache *pool.AllocationCache, ext SessionExtractor, logger *slog.Logger) *Router {
	return &Router{
		pool:      pm,
		cache:     cache,
		proxy:     NewProxy(logger),
		queue:     NewWaitQueue(logger),
		extractor: ext,
		logger:    logger,
	}
}

// ServeHTTP implements the http.Handler interface. Request routing logic:
//  1. Extract session ID from header / cookie / query param.
//  2. If a session already exists in the cache, proxy to the allocated sandbox.
//  3. For new requests, allocate a sandbox and proxy.
//  4. If the pool is exhausted, enqueue the request and stream SSE position
//     updates until a sandbox becomes available.
var routerTracer = otel.Tracer("opencode-scale/router")

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	ctx, span := routerTracer.Start(req.Context(), "Router.ServeHTTP")
	defer span.End()
	req = req.WithContext(ctx)

	sessionID := extractSessionID(req, r.extractor)

	// Fast path: known session.
	if sessionID != "" {
		span.SetAttributes(attribute.String("session.id", sessionID))
		if alloc, ok := r.cache.GetBySession(sessionID); ok {
			span.SetAttributes(attribute.String("sandbox.name", alloc.SandboxName))
			r.logger.Debug("routing to existing session", "sessionID", sessionID, "sandbox", alloc.SandboxName)
			r.proxy.Forward(w, req, alloc.ServiceFQDN)
			return
		}
	}

	// Slow path: allocate a new sandbox.
	userID := extractUserID(req)
	r.handleAllocateAndProxy(w, req, userID)
}

// handleAllocateAndProxy tries to allocate a sandbox for the user. When the
// pool is exhausted it falls back to the wait queue with SSE updates.
func (r *Router) handleAllocateAndProxy(w http.ResponseWriter, req *http.Request, userID string) {
	alloc, err := r.pool.Allocate(req.Context(), userID)
	if err == nil {
		r.logger.Info("allocated sandbox", "sessionID", alloc.SessionID, "sandbox", alloc.SandboxName, "userID", userID)
		w.Header().Set("X-Session-ID", alloc.SessionID)
		http.SetCookie(w, &http.Cookie{
			Name:     r.extractor.Cookie,
			Value:    alloc.SessionID,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		r.proxy.Forward(w, req, alloc.ServiceFQDN)
		return
	}

	if !errors.Is(err, pool.ErrPoolExhausted) {
		r.logger.Error("allocation failed", "error", err, "userID", userID)
		http.Error(w, "failed to allocate sandbox", http.StatusInternalServerError)
		return
	}

	// Pool exhausted: enqueue and stream position updates via SSE.
	r.logger.Warn("pool exhausted, enqueuing request", "userID", userID)
	entry := r.queue.Enqueue(userID)
	WriteSSEQueuePosition(w, req, entry)
}

// extractSessionID checks the request for a session identifier in the
// following order: header, cookie, query parameter.
func extractSessionID(req *http.Request, ext SessionExtractor) string {
	if ext.Header != "" {
		if v := req.Header.Get(ext.Header); v != "" {
			return v
		}
	}

	if ext.Cookie != "" {
		if c, err := req.Cookie(ext.Cookie); err == nil && c.Value != "" {
			return c.Value
		}
	}

	if ext.QueryParam != "" {
		if v := req.URL.Query().Get(ext.QueryParam); v != "" {
			return v
		}
	}

	return ""
}

// extractUserID returns the user identifier from the X-User-ID request header,
// falling back to "anonymous" when the header is absent.
func extractUserID(req *http.Request) string {
	if v := req.Header.Get("X-User-ID"); v != "" {
		return v
	}
	return "anonymous"
}
