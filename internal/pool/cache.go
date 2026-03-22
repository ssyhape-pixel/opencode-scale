package pool

import "sync"

// AllocationCache provides thread-safe in-memory storage for sandbox allocations,
// indexed by both sandbox name and session ID.
type AllocationCache struct {
	mu        sync.RWMutex
	byName    map[string]*Allocation
	bySession map[string]*Allocation
}

// NewAllocationCache returns an initialised AllocationCache.
func NewAllocationCache() *AllocationCache {
	return &AllocationCache{
		byName:    make(map[string]*Allocation),
		bySession: make(map[string]*Allocation),
	}
}

// Get retrieves an allocation by sandbox name.
func (c *AllocationCache) Get(sandboxName string) (*Allocation, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	a, ok := c.byName[sandboxName]
	return a, ok
}

// GetBySession retrieves an allocation by session ID.
func (c *AllocationCache) GetBySession(sessionID string) (*Allocation, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	a, ok := c.bySession[sessionID]
	return a, ok
}

// Set stores an allocation, indexing it by sandbox name and (if non-empty) session ID.
func (c *AllocationCache) Set(alloc *Allocation) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byName[alloc.SandboxName] = alloc
	if alloc.SessionID != "" {
		c.bySession[alloc.SessionID] = alloc
	}
}

// Delete removes an allocation by sandbox name from both indexes.
func (c *AllocationCache) Delete(sandboxName string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if a, ok := c.byName[sandboxName]; ok {
		if a.SessionID != "" {
			delete(c.bySession, a.SessionID)
		}
		delete(c.byName, sandboxName)
	}
}

// List returns a snapshot of all current allocations.
func (c *AllocationCache) List() []*Allocation {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*Allocation, 0, len(c.byName))
	for _, a := range c.byName {
		out = append(out, a)
	}
	return out
}

// ListByStatus returns all allocations matching the given status.
func (c *AllocationCache) ListByStatus(status AllocationStatus) []*Allocation {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []*Allocation
	for _, a := range c.byName {
		if a.Status == status {
			out = append(out, a)
		}
	}
	return out
}

// Len returns the number of stored allocations.
func (c *AllocationCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.byName)
}
