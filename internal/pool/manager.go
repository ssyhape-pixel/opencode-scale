package pool

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var poolTracer = otel.Tracer("opencode-scale/pool")

// ErrPoolExhausted is returned when all sandbox slots are in use.
var ErrPoolExhausted = errors.New("pool: all sandbox slots are in use")

// AllocationStatus represents the lifecycle state of a sandbox allocation.
type AllocationStatus int

const (
	StatusIdle      AllocationStatus = iota
	StatusActive
	StatusReleasing
)

// Allocation tracks a single sandbox assigned to a user session.
type Allocation struct {
	SandboxName  string
	ServiceFQDN  string
	SessionID    string
	UserID       string
	AllocatedAt  time.Time
	LastActivity time.Time
	Status       AllocationStatus
}

// SandboxProvider abstracts the underlying sandbox lifecycle operations.
type SandboxProvider interface {
	CreateSandbox(ctx context.Context) (name string, fqdn string, err error)
	DeleteSandbox(ctx context.Context, name string) error
}

// PoolStats is a point-in-time snapshot of pool utilisation.
type PoolStats struct {
	Allocated int
	MaxSize   int
}

// PoolManager orchestrates sandbox allocation, release, and garbage collection.
type PoolManager struct {
	provider  SandboxProvider
	cache     *AllocationCache
	metrics   *PoolMetrics
	mu        sync.Mutex
	maxSize   int
	namespace string
}

// NewPoolManager returns a PoolManager ready to manage up to maxSize sandboxes.
// The provided cache is shared with the router for session lookups.
func NewPoolManager(provider SandboxProvider, maxSize int, namespace string, cache *AllocationCache, metrics *PoolMetrics) *PoolManager {
	return &PoolManager{
		provider:  provider,
		cache:     cache,
		metrics:   metrics,
		maxSize:   maxSize,
		namespace: namespace,
	}
}

// Allocate creates a new sandbox for the given user and returns its Allocation.
// If an idle warm sandbox is available, it is claimed instead of creating a new one.
// Returns ErrPoolExhausted when the pool has reached maxSize.
func (p *PoolManager) Allocate(ctx context.Context, userID string) (*Allocation, error) {
	ctx, span := poolTracer.Start(ctx, "PoolManager.Allocate")
	defer span.End()
	span.SetAttributes(attribute.String("user.id", userID))

	// Try to claim an idle warm sandbox first.
	if alloc := p.claimIdle(userID); alloc != nil {
		return alloc, nil
	}

	p.mu.Lock()
	if p.cache.Len() >= p.maxSize {
		p.mu.Unlock()
		return nil, ErrPoolExhausted
	}
	p.mu.Unlock()

	start := time.Now()

	name, fqdn, err := p.provider.CreateSandbox(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	alloc := &Allocation{
		SandboxName:  name,
		ServiceFQDN:  fqdn,
		SessionID:    uuid.NewString(),
		UserID:       userID,
		AllocatedAt:  now,
		LastActivity: now,
		Status:       StatusActive,
	}

	p.cache.Set(alloc)

	if p.metrics != nil {
		p.metrics.RecordAllocation(ctx, time.Since(start))
		p.metrics.SetAllocatedCount(ctx, int64(p.cache.Len()))
	}

	return alloc, nil
}

// claimIdle attempts to claim an idle warm sandbox for the given user.
// Returns nil if no idle sandbox is available.
func (p *PoolManager) claimIdle(userID string) *Allocation {
	p.mu.Lock()
	defer p.mu.Unlock()

	idle := p.cache.ListByStatus(StatusIdle)
	if len(idle) == 0 {
		return nil
	}

	alloc := idle[0]
	alloc.Status = StatusActive
	alloc.UserID = userID
	alloc.SessionID = uuid.NewString()
	alloc.LastActivity = time.Now()
	// Re-index with the new session ID.
	p.cache.Set(alloc)

	return alloc
}

// Release marks the allocation as releasing, deletes the sandbox, and removes it from the cache.
func (p *PoolManager) Release(ctx context.Context, sandboxName string) error {
	ctx, span := poolTracer.Start(ctx, "PoolManager.Release")
	defer span.End()
	span.SetAttributes(attribute.String("sandbox.name", sandboxName))

	alloc, ok := p.cache.Get(sandboxName)
	if !ok {
		return nil
	}

	alloc.Status = StatusReleasing

	if err := p.provider.DeleteSandbox(ctx, sandboxName); err != nil {
		return err
	}

	p.cache.Delete(sandboxName)

	if p.metrics != nil {
		p.metrics.SetAllocatedCount(ctx, int64(p.cache.Len()))
	}

	return nil
}

// Heartbeat updates the LastActivity timestamp for the given session.
func (p *PoolManager) Heartbeat(ctx context.Context, sessionID string) error {
	alloc, ok := p.cache.GetBySession(sessionID)
	if !ok {
		return errors.New("pool: session not found")
	}
	p.mu.Lock()
	alloc.LastActivity = time.Now()
	p.mu.Unlock()
	return nil
}

// RunGC scans all allocations and releases any that have been idle longer than idleTimeout.
// Idle warm-pool sandboxes are skipped.
func (p *PoolManager) RunGC(ctx context.Context, idleTimeout time.Duration) {
	cutoff := time.Now().Add(-idleTimeout)
	for _, alloc := range p.cache.List() {
		if alloc.Status == StatusIdle || alloc.Status == StatusReleasing {
			continue
		}
		if alloc.LastActivity.Before(cutoff) {
			_ = p.Release(ctx, alloc.SandboxName)
		}
	}
}

// AllocateWarm creates a warm (idle) sandbox that is not assigned to any user.
func (p *PoolManager) AllocateWarm(ctx context.Context) (*Allocation, error) {
	ctx, span := poolTracer.Start(ctx, "PoolManager.AllocateWarm")
	defer span.End()

	p.mu.Lock()
	if p.cache.Len() >= p.maxSize {
		p.mu.Unlock()
		return nil, ErrPoolExhausted
	}
	p.mu.Unlock()

	name, fqdn, err := p.provider.CreateSandbox(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	alloc := &Allocation{
		SandboxName:  name,
		ServiceFQDN:  fqdn,
		AllocatedAt:  now,
		LastActivity: now,
		Status:       StatusIdle,
	}

	p.cache.Set(alloc)

	if p.metrics != nil {
		p.metrics.SetAllocatedCount(ctx, int64(p.cache.Len()))
	}

	return alloc, nil
}

// StartWarmPool maintains at least minReady idle sandboxes, replenishing on a ticker.
func (p *PoolManager) StartWarmPool(ctx context.Context, minReady int, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	slog.Info("warm pool started", "minReady", minReady, "interval", interval)

	// Initial replenish.
	p.replenishWarm(ctx, minReady)

	for {
		select {
		case <-ctx.Done():
			slog.Info("warm pool stopped")
			return
		case <-ticker.C:
			p.replenishWarm(ctx, minReady)
		}
	}
}

// replenishWarm ensures at least minReady idle sandboxes exist.
func (p *PoolManager) replenishWarm(ctx context.Context, minReady int) {
	idle := p.cache.ListByStatus(StatusIdle)
	deficit := minReady - len(idle)
	for i := 0; i < deficit; i++ {
		if _, err := p.AllocateWarm(ctx); err != nil {
			slog.Warn("warm pool: failed to allocate", "error", err)
			return
		}
	}
}

// StartGCLoop runs RunGC on a ticker until ctx is cancelled.
func (p *PoolManager) StartGCLoop(ctx context.Context, interval, idleTimeout time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	slog.Info("gc loop started", "interval", interval, "idleTimeout", idleTimeout)
	for {
		select {
		case <-ctx.Done():
			slog.Info("gc loop stopped")
			return
		case <-ticker.C:
			p.RunGC(ctx, idleTimeout)
		}
	}
}

// Stats returns a point-in-time snapshot of pool utilisation.
func (p *PoolManager) Stats() PoolStats {
	return PoolStats{
		Allocated: p.cache.Len(),
		MaxSize:   p.maxSize,
	}
}
