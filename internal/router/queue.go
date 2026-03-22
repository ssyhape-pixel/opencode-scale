package router

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/opencode-scale/opencode-scale/internal/pool"
)

// WaitEntry represents a single request waiting for a sandbox allocation.
type WaitEntry struct {
	UserID     string
	Ready      chan *pool.Allocation
	Position   int
	EnqueuedAt time.Time
}

// WaitQueue is a thread-safe FIFO queue for requests that are waiting for a
// sandbox to become available.
type WaitQueue struct {
	mu      sync.Mutex
	entries []*WaitEntry
	logger  *slog.Logger
}

// NewWaitQueue returns an initialised WaitQueue.
func NewWaitQueue(logger *slog.Logger) *WaitQueue {
	return &WaitQueue{
		entries: make([]*WaitEntry, 0),
		logger:  logger,
	}
}

// Enqueue adds a new wait entry to the back of the queue and returns it. The
// caller should select on entry.Ready to receive the allocation when one
// becomes available.
func (q *WaitQueue) Enqueue(userID string) *WaitEntry {
	q.mu.Lock()
	defer q.mu.Unlock()

	entry := &WaitEntry{
		UserID:     userID,
		Ready:      make(chan *pool.Allocation, 1),
		Position:   len(q.entries) + 1,
		EnqueuedAt: time.Now(),
	}
	q.entries = append(q.entries, entry)

	q.logger.Info("enqueued request", "userID", userID, "position", entry.Position)
	return entry
}

// Dequeue removes and returns the first entry in the queue. Returns false when
// the queue is empty.
func (q *WaitQueue) Dequeue() (*WaitEntry, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.entries) == 0 {
		return nil, false
	}

	entry := q.entries[0]
	q.entries = q.entries[1:]

	// Recalculate positions for remaining entries.
	for i, e := range q.entries {
		e.Position = i + 1
	}

	return entry, true
}

// Len returns the current number of waiting entries.
func (q *WaitQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.entries)
}

// NotifyNext sends an allocation to the next waiter in the queue. If the queue
// is empty the call is a no-op.
func (q *WaitQueue) NotifyNext(alloc *pool.Allocation) {
	entry, ok := q.Dequeue()
	if !ok {
		return
	}
	q.logger.Info("notifying waiter", "userID", entry.UserID)
	entry.Ready <- alloc
}

// WriteSSEQueuePosition writes Server-Sent Events to w that inform the client
// of their queue position. It blocks until the entry receives an allocation on
// its Ready channel or the request context is cancelled.
func WriteSSEQueuePosition(w http.ResponseWriter, req *http.Request, entry *WaitEntry) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send initial position.
	fmt.Fprintf(w, "event: queue\ndata: {\"position\":%d}\n\n", entry.Position)
	flusher.Flush()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case alloc := <-entry.Ready:
			fmt.Fprintf(w, "event: allocated\ndata: {\"sessionID\":%q,\"host\":%q}\n\n", alloc.SessionID, alloc.ServiceFQDN)
			flusher.Flush()
			return
		case <-req.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, "event: queue\ndata: {\"position\":%d}\n\n", entry.Position)
			flusher.Flush()
		}
	}
}
