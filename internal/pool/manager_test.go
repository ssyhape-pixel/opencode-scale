package pool

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric/noop"
)

// ---------------------------------------------------------------------------
// Mock sandbox provider
// ---------------------------------------------------------------------------

type mockSandboxProvider struct {
	mu          sync.Mutex
	createCount int
	deleteCount int
	createErr   error
	deleteErr   error
}

func (m *mockSandboxProvider) CreateSandbox(_ context.Context) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return "", "", m.createErr
	}
	m.createCount++
	name := fmt.Sprintf("sb-%d", m.createCount)
	fqdn := fmt.Sprintf("%s.default.svc.cluster.local", name)
	return name, fqdn, nil
}

func (m *mockSandboxProvider) DeleteSandbox(_ context.Context, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteErr != nil {
		return m.deleteErr
	}
	m.deleteCount++
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestMetrics(t *testing.T) *PoolMetrics {
	t.Helper()
	meter := noop.NewMeterProvider().Meter("test")
	pm, err := NewPoolMetrics(meter)
	if err != nil {
		t.Fatalf("failed to create noop metrics: %v", err)
	}
	return pm
}

func newTestManager(t *testing.T, maxSize int, provider *mockSandboxProvider) (*PoolManager, *AllocationCache) {
	t.Helper()
	cache := NewAllocationCache()
	metrics := newTestMetrics(t)
	pm := NewPoolManager(provider, maxSize, "default", cache, metrics)
	return pm, cache
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestAllocate(t *testing.T) {
	provider := &mockSandboxProvider{}
	pm, cache := newTestManager(t, 10, provider)

	alloc, err := pm.Allocate(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify allocation fields.
	if alloc.SandboxName == "" {
		t.Fatal("expected non-empty SandboxName")
	}
	if alloc.ServiceFQDN == "" {
		t.Fatal("expected non-empty ServiceFQDN")
	}
	if alloc.SessionID == "" {
		t.Fatal("expected non-empty SessionID")
	}
	if alloc.UserID != "user-1" {
		t.Fatalf("expected UserID user-1, got %s", alloc.UserID)
	}
	if alloc.Status != StatusActive {
		t.Fatalf("expected StatusActive, got %d", alloc.Status)
	}
	if alloc.AllocatedAt.IsZero() {
		t.Fatal("expected AllocatedAt to be set")
	}
	if alloc.LastActivity.IsZero() {
		t.Fatal("expected LastActivity to be set")
	}

	// Verify it is in the cache.
	if cache.Len() != 1 {
		t.Fatalf("expected cache len 1, got %d", cache.Len())
	}

	// Verify provider was invoked.
	if provider.createCount != 1 {
		t.Fatalf("expected 1 create call, got %d", provider.createCount)
	}
}

func TestAllocatePoolExhausted(t *testing.T) {
	provider := &mockSandboxProvider{}
	pm, _ := newTestManager(t, 1, provider)

	// First allocation should succeed.
	_, err := pm.Allocate(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error on first allocate: %v", err)
	}

	// Second allocation should fail with ErrPoolExhausted.
	_, err = pm.Allocate(context.Background(), "user-2")
	if err == nil {
		t.Fatal("expected error on second allocate, got nil")
	}
	if err != ErrPoolExhausted {
		t.Fatalf("expected ErrPoolExhausted, got %v", err)
	}
}

func TestRelease(t *testing.T) {
	provider := &mockSandboxProvider{}
	pm, cache := newTestManager(t, 10, provider)

	alloc, err := pm.Allocate(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = pm.Release(context.Background(), alloc.SandboxName)
	if err != nil {
		t.Fatalf("unexpected error on release: %v", err)
	}

	// Cache should now be empty.
	if cache.Len() != 0 {
		t.Fatalf("expected empty cache after release, got len %d", cache.Len())
	}

	// Provider delete should have been called.
	if provider.deleteCount != 1 {
		t.Fatalf("expected 1 delete call, got %d", provider.deleteCount)
	}

	// Releasing an already-released sandbox is a no-op.
	err = pm.Release(context.Background(), alloc.SandboxName)
	if err != nil {
		t.Fatalf("expected no error on double release, got %v", err)
	}
}

func TestHeartbeat(t *testing.T) {
	provider := &mockSandboxProvider{}
	pm, _ := newTestManager(t, 10, provider)

	alloc, err := pm.Allocate(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	before := alloc.LastActivity

	// Ensure the clock moves forward so that the heartbeat timestamp differs.
	time.Sleep(5 * time.Millisecond)

	err = pm.Heartbeat(context.Background(), alloc.SessionID)
	if err != nil {
		t.Fatalf("unexpected error on heartbeat: %v", err)
	}

	if !alloc.LastActivity.After(before) {
		t.Fatal("expected LastActivity to be updated after heartbeat")
	}
}

func TestHeartbeatNotFound(t *testing.T) {
	provider := &mockSandboxProvider{}
	pm, _ := newTestManager(t, 10, provider)

	err := pm.Heartbeat(context.Background(), "unknown-session-id")
	if err == nil {
		t.Fatal("expected error for unknown session, got nil")
	}
}

func TestRunGC(t *testing.T) {
	provider := &mockSandboxProvider{}
	pm, cache := newTestManager(t, 10, provider)

	alloc, err := pm.Allocate(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Push LastActivity into the past so GC considers it idle.
	alloc.LastActivity = time.Now().Add(-10 * time.Minute)

	pm.RunGC(context.Background(), 5*time.Minute)

	// The allocation should have been released.
	if cache.Len() != 0 {
		t.Fatalf("expected cache empty after GC, got len %d", cache.Len())
	}
	if provider.deleteCount != 1 {
		t.Fatalf("expected 1 delete from GC, got %d", provider.deleteCount)
	}
}

func TestRunGCSkipsActive(t *testing.T) {
	provider := &mockSandboxProvider{}
	pm, cache := newTestManager(t, 10, provider)

	_, err := pm.Allocate(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// LastActivity is now (just allocated), so GC with 5m timeout should NOT collect.
	pm.RunGC(context.Background(), 5*time.Minute)

	if cache.Len() != 1 {
		t.Fatalf("expected cache len 1 (GC should skip active), got %d", cache.Len())
	}
}

func TestStats(t *testing.T) {
	provider := &mockSandboxProvider{}
	pm, _ := newTestManager(t, 5, provider)

	stats := pm.Stats()
	if stats.Allocated != 0 {
		t.Fatalf("expected 0 allocated, got %d", stats.Allocated)
	}
	if stats.MaxSize != 5 {
		t.Fatalf("expected maxSize 5, got %d", stats.MaxSize)
	}

	_, err := pm.Allocate(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stats = pm.Stats()
	if stats.Allocated != 1 {
		t.Fatalf("expected 1 allocated, got %d", stats.Allocated)
	}
}

func TestStartGCLoop(t *testing.T) {
	provider := &mockSandboxProvider{}
	pm, cache := newTestManager(t, 10, provider)

	alloc, err := pm.Allocate(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Push LastActivity into the past.
	alloc.LastActivity = time.Now().Add(-10 * time.Minute)

	ctx, cancel := context.WithCancel(context.Background())

	go pm.StartGCLoop(ctx, 50*time.Millisecond, 5*time.Minute)

	// Wait for at least one GC tick.
	time.Sleep(150 * time.Millisecond)
	cancel()

	// The idle allocation should have been released.
	if cache.Len() != 0 {
		t.Fatalf("expected cache empty after GC loop, got len %d", cache.Len())
	}
}

func TestAllocateClaimsIdle(t *testing.T) {
	provider := &mockSandboxProvider{}
	pm, cache := newTestManager(t, 10, provider)

	// Create a warm (idle) sandbox.
	warm, err := pm.AllocateWarm(context.Background())
	if err != nil {
		t.Fatalf("unexpected error allocating warm: %v", err)
	}
	if warm.Status != StatusIdle {
		t.Fatalf("expected StatusIdle, got %d", warm.Status)
	}
	if warm.SessionID != "" {
		t.Fatalf("expected empty SessionID for warm, got %s", warm.SessionID)
	}

	createsBefore := provider.createCount

	// Allocate should claim the idle sandbox instead of creating a new one.
	alloc, err := pm.Allocate(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error allocating: %v", err)
	}

	// Should have reused the warm sandbox (no new provider.CreateSandbox call).
	if provider.createCount != createsBefore {
		t.Fatalf("expected no new sandbox creation, got %d creates", provider.createCount-createsBefore)
	}

	if alloc.SandboxName != warm.SandboxName {
		t.Fatalf("expected same sandbox name %s, got %s", warm.SandboxName, alloc.SandboxName)
	}
	if alloc.Status != StatusActive {
		t.Fatalf("expected StatusActive after claim, got %d", alloc.Status)
	}
	if alloc.UserID != "user-1" {
		t.Fatalf("expected UserID user-1, got %s", alloc.UserID)
	}
	if alloc.SessionID == "" {
		t.Fatal("expected non-empty SessionID after claim")
	}

	// Cache should still have exactly 1 entry.
	if cache.Len() != 1 {
		t.Fatalf("expected cache len 1, got %d", cache.Len())
	}
}

func TestReplenishWarm(t *testing.T) {
	provider := &mockSandboxProvider{}
	pm, cache := newTestManager(t, 10, provider)

	ctx, cancel := context.WithCancel(context.Background())

	go pm.StartWarmPool(ctx, 3, 50*time.Millisecond)

	// Wait for initial replenish + at least one tick.
	time.Sleep(150 * time.Millisecond)
	cancel()

	idle := cache.ListByStatus(StatusIdle)
	if len(idle) < 3 {
		t.Fatalf("expected at least 3 idle sandboxes, got %d", len(idle))
	}
}

func TestGCSkipsIdleSandboxes(t *testing.T) {
	provider := &mockSandboxProvider{}
	pm, cache := newTestManager(t, 10, provider)

	// Create an idle warm sandbox.
	warm, err := pm.AllocateWarm(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Push LastActivity into the past (older than idle timeout).
	warm.LastActivity = time.Now().Add(-10 * time.Minute)

	pm.RunGC(context.Background(), 5*time.Minute)

	// Warm idle sandbox should NOT have been released.
	if cache.Len() != 1 {
		t.Fatalf("expected cache len 1 (GC should skip idle), got %d", cache.Len())
	}

	got, ok := cache.Get(warm.SandboxName)
	if !ok {
		t.Fatal("expected warm sandbox to still be in cache")
	}
	if got.Status != StatusIdle {
		t.Fatalf("expected StatusIdle, got %d", got.Status)
	}
}

func TestMetricsWithNoopMeter(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	pm, err := NewPoolMetrics(meter)
	if err != nil {
		t.Fatalf("unexpected error creating noop metrics: %v", err)
	}
	if pm == nil {
		t.Fatal("expected non-nil PoolMetrics")
	}

	ctx := context.Background()
	// Ensure recording methods don't panic with noop instruments.
	pm.RecordAllocation(ctx, 100*time.Millisecond)
	pm.SetPoolSize(ctx, 5)
	pm.SetAllocatedCount(ctx, 2)
	pm.SetQueueLength(ctx, 0)
}
