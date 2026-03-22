package router

import (
	"testing"
	"time"

	"github.com/opencode-scale/opencode-scale/internal/pool"
)

func TestWaitQueueEnqueue(t *testing.T) {
	q := NewWaitQueue(testLogger())

	entry := q.Enqueue("user-1")

	if entry.UserID != "user-1" {
		t.Fatalf("expected UserID %q, got %q", "user-1", entry.UserID)
	}
	if entry.Position != 1 {
		t.Fatalf("expected Position 1, got %d", entry.Position)
	}
	if entry.Ready == nil {
		t.Fatal("expected Ready channel to be initialised")
	}
	if entry.EnqueuedAt.IsZero() {
		t.Fatal("expected EnqueuedAt to be set")
	}

	// Enqueue a second entry and verify positions.
	entry2 := q.Enqueue("user-2")
	if entry2.Position != 2 {
		t.Fatalf("expected Position 2, got %d", entry2.Position)
	}
	if q.Len() != 2 {
		t.Fatalf("expected queue length 2, got %d", q.Len())
	}
}

func TestWaitQueueDequeue(t *testing.T) {
	q := NewWaitQueue(testLogger())

	q.Enqueue("user-a")
	q.Enqueue("user-b")
	q.Enqueue("user-c")

	entry, ok := q.Dequeue()
	if !ok {
		t.Fatal("expected Dequeue to return true")
	}
	if entry.UserID != "user-a" {
		t.Fatalf("expected first dequeued user %q, got %q", "user-a", entry.UserID)
	}

	// After dequeue, remaining entries should have recalculated positions.
	if q.Len() != 2 {
		t.Fatalf("expected queue length 2, got %d", q.Len())
	}

	// Dequeue second entry and check position recalculation for the remaining one.
	entry2, ok := q.Dequeue()
	if !ok {
		t.Fatal("expected Dequeue to return true")
	}
	if entry2.UserID != "user-b" {
		t.Fatalf("expected second dequeued user %q, got %q", "user-b", entry2.UserID)
	}

	// The last remaining entry ("user-c") should now be at position 1.
	// We dequeue it to verify.
	entry3, ok := q.Dequeue()
	if !ok {
		t.Fatal("expected Dequeue to return true for last entry")
	}
	if entry3.UserID != "user-c" {
		t.Fatalf("expected third dequeued user %q, got %q", "user-c", entry3.UserID)
	}
}

func TestWaitQueueDequeueEmpty(t *testing.T) {
	q := NewWaitQueue(testLogger())

	entry, ok := q.Dequeue()
	if ok {
		t.Fatal("expected Dequeue to return false on empty queue")
	}
	if entry != nil {
		t.Fatalf("expected nil entry, got %+v", entry)
	}
}

func TestWaitQueueLen(t *testing.T) {
	q := NewWaitQueue(testLogger())

	if q.Len() != 0 {
		t.Fatalf("expected length 0, got %d", q.Len())
	}

	q.Enqueue("u1")
	q.Enqueue("u2")
	q.Enqueue("u3")

	if q.Len() != 3 {
		t.Fatalf("expected length 3, got %d", q.Len())
	}

	q.Dequeue()
	if q.Len() != 2 {
		t.Fatalf("expected length 2 after dequeue, got %d", q.Len())
	}
}

func TestWaitQueueNotifyNext(t *testing.T) {
	q := NewWaitQueue(testLogger())

	entry := q.Enqueue("user-notify")

	alloc := &pool.Allocation{
		SandboxName: "sb-notify",
		ServiceFQDN: "sb-notify.default.svc.cluster.local",
		SessionID:   "sess-notify",
		UserID:      "user-notify",
	}

	// NotifyNext should deliver the allocation to the first waiter.
	q.NotifyNext(alloc)

	select {
	case received := <-entry.Ready:
		if received.SessionID != alloc.SessionID {
			t.Fatalf("expected SessionID %q, got %q", alloc.SessionID, received.SessionID)
		}
		if received.SandboxName != alloc.SandboxName {
			t.Fatalf("expected SandboxName %q, got %q", alloc.SandboxName, received.SandboxName)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for allocation on Ready channel")
	}

	if q.Len() != 0 {
		t.Fatalf("expected queue to be empty after NotifyNext, got length %d", q.Len())
	}
}

func TestWaitQueueNotifyNextEmpty(t *testing.T) {
	q := NewWaitQueue(testLogger())

	alloc := &pool.Allocation{
		SandboxName: "sb-noop",
		SessionID:   "sess-noop",
	}

	// Should not panic on an empty queue.
	q.NotifyNext(alloc)

	if q.Len() != 0 {
		t.Fatalf("expected queue length 0, got %d", q.Len())
	}
}
