package pool

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestNewAllocationCache(t *testing.T) {
	c := NewAllocationCache()
	if c == nil {
		t.Fatal("expected non-nil cache")
	}
	if c.Len() != 0 {
		t.Fatalf("expected empty cache, got len %d", c.Len())
	}
}

func TestCacheSetAndGet(t *testing.T) {
	c := NewAllocationCache()

	alloc := &Allocation{
		SandboxName:  "sb-1",
		ServiceFQDN:  "sb-1.default.svc.cluster.local",
		SessionID:    "sess-aaa",
		UserID:       "user-1",
		AllocatedAt:  time.Now(),
		LastActivity: time.Now(),
		Status:       StatusActive,
	}

	c.Set(alloc)

	// Retrieve by name.
	got, ok := c.Get("sb-1")
	if !ok {
		t.Fatal("expected to find allocation by sandbox name")
	}
	if got.SessionID != "sess-aaa" {
		t.Fatalf("expected session sess-aaa, got %s", got.SessionID)
	}

	// Miss by name.
	_, ok = c.Get("nonexistent")
	if ok {
		t.Fatal("expected miss for unknown sandbox name")
	}
}

func TestCacheGetBySession(t *testing.T) {
	c := NewAllocationCache()

	alloc := &Allocation{
		SandboxName:  "sb-2",
		ServiceFQDN:  "sb-2.default.svc.cluster.local",
		SessionID:    "sess-bbb",
		UserID:       "user-2",
		AllocatedAt:  time.Now(),
		LastActivity: time.Now(),
		Status:       StatusActive,
	}

	c.Set(alloc)

	// Retrieve by session.
	got, ok := c.GetBySession("sess-bbb")
	if !ok {
		t.Fatal("expected to find allocation by session ID")
	}
	if got.SandboxName != "sb-2" {
		t.Fatalf("expected sandbox sb-2, got %s", got.SandboxName)
	}

	// Miss by session.
	_, ok = c.GetBySession("nonexistent")
	if ok {
		t.Fatal("expected miss for unknown session ID")
	}
}

func TestCacheDelete(t *testing.T) {
	c := NewAllocationCache()

	alloc := &Allocation{
		SandboxName:  "sb-3",
		ServiceFQDN:  "sb-3.default.svc.cluster.local",
		SessionID:    "sess-ccc",
		UserID:       "user-3",
		AllocatedAt:  time.Now(),
		LastActivity: time.Now(),
		Status:       StatusActive,
	}

	c.Set(alloc)

	// Delete by sandbox name.
	c.Delete("sb-3")

	// Both indexes should be empty.
	if _, ok := c.Get("sb-3"); ok {
		t.Fatal("expected sandbox name index to be cleaned after delete")
	}
	if _, ok := c.GetBySession("sess-ccc"); ok {
		t.Fatal("expected session index to be cleaned after delete")
	}
	if c.Len() != 0 {
		t.Fatalf("expected cache len 0, got %d", c.Len())
	}

	// Delete of a non-existent key should not panic.
	c.Delete("does-not-exist")
}

func TestCacheList(t *testing.T) {
	c := NewAllocationCache()

	names := []string{"sb-a", "sb-b", "sb-c"}
	for i, name := range names {
		c.Set(&Allocation{
			SandboxName:  name,
			SessionID:    fmt.Sprintf("sess-%d", i),
			AllocatedAt:  time.Now(),
			LastActivity: time.Now(),
			Status:       StatusActive,
		})
	}

	list := c.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 allocations, got %d", len(list))
	}

	// Build a set for order-independent comparison.
	seen := make(map[string]bool, len(list))
	for _, a := range list {
		seen[a.SandboxName] = true
	}
	for _, name := range names {
		if !seen[name] {
			t.Fatalf("expected allocation %s in list", name)
		}
	}
}

func TestCacheLen(t *testing.T) {
	c := NewAllocationCache()

	if c.Len() != 0 {
		t.Fatal("expected len 0 on new cache")
	}

	c.Set(&Allocation{SandboxName: "sb-x", SessionID: "sess-x"})
	c.Set(&Allocation{SandboxName: "sb-y", SessionID: "sess-y"})

	if c.Len() != 2 {
		t.Fatalf("expected len 2, got %d", c.Len())
	}

	c.Delete("sb-x")
	if c.Len() != 1 {
		t.Fatalf("expected len 1 after delete, got %d", c.Len())
	}
}

func TestListByStatus(t *testing.T) {
	c := NewAllocationCache()

	c.Set(&Allocation{SandboxName: "sb-idle-1", SessionID: "", Status: StatusIdle})
	c.Set(&Allocation{SandboxName: "sb-active-1", SessionID: "sess-1", Status: StatusActive})
	c.Set(&Allocation{SandboxName: "sb-idle-2", SessionID: "", Status: StatusIdle})
	c.Set(&Allocation{SandboxName: "sb-active-2", SessionID: "sess-2", Status: StatusActive})

	idle := c.ListByStatus(StatusIdle)
	if len(idle) != 2 {
		t.Fatalf("expected 2 idle, got %d", len(idle))
	}

	active := c.ListByStatus(StatusActive)
	if len(active) != 2 {
		t.Fatalf("expected 2 active, got %d", len(active))
	}

	releasing := c.ListByStatus(StatusReleasing)
	if len(releasing) != 0 {
		t.Fatalf("expected 0 releasing, got %d", len(releasing))
	}
}

func TestCacheConcurrency(t *testing.T) {
	c := NewAllocationCache()
	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := range goroutines {
		go func(id int) {
			defer wg.Done()
			name := fmt.Sprintf("sb-%d", id)
			session := fmt.Sprintf("sess-%d", id)

			for range opsPerGoroutine {
				alloc := &Allocation{
					SandboxName:  name,
					SessionID:    session,
					AllocatedAt:  time.Now(),
					LastActivity: time.Now(),
					Status:       StatusActive,
				}
				c.Set(alloc)
				c.Get(name)
				c.GetBySession(session)
				c.List()
				c.Len()
				c.Delete(name)
			}
		}(g)
	}

	wg.Wait()

	// After all goroutines are done all entries should be deleted.
	if c.Len() != 0 {
		t.Fatalf("expected empty cache after concurrent ops, got len %d", c.Len())
	}
}
