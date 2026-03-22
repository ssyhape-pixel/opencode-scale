package pool

import (
	"context"
	"fmt"
	"sync/atomic"
)

// MockSandboxProvider implements SandboxProvider for local development and
// testing. It returns a fixed target FQDN and incrementing sandbox names.
type MockSandboxProvider struct {
	targetFQDN string
	counter    atomic.Int64
}

// NewMockSandboxProvider creates a mock provider that returns the given
// targetFQDN for every sandbox creation.
func NewMockSandboxProvider(targetFQDN string) *MockSandboxProvider {
	return &MockSandboxProvider{
		targetFQDN: targetFQDN,
	}
}

// CreateSandbox returns a mock sandbox with an incrementing name and fixed FQDN.
func (m *MockSandboxProvider) CreateSandbox(_ context.Context) (string, string, error) {
	n := m.counter.Add(1)
	name := fmt.Sprintf("mock-sandbox-%d", n)
	return name, m.targetFQDN, nil
}

// DeleteSandbox is a no-op for the mock provider.
func (m *MockSandboxProvider) DeleteSandbox(_ context.Context, _ string) error {
	return nil
}
