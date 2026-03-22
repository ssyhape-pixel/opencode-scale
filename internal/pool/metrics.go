package pool

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"
)

// PoolMetrics holds OpenTelemetry metric instruments for the sandbox pool.
type PoolMetrics struct {
	poolSize          metric.Int64Gauge
	allocatedCount    metric.Int64Gauge
	waitQueueLength   metric.Int64Gauge
	allocationLatency metric.Float64Histogram
}

// NewPoolMetrics creates all pool-related OTel metric instruments from the given meter.
func NewPoolMetrics(meter metric.Meter) (*PoolMetrics, error) {
	poolSize, err := meter.Int64Gauge("opencode_scale_pool_size")
	if err != nil {
		return nil, err
	}
	allocatedCount, err := meter.Int64Gauge("opencode_scale_allocated_count")
	if err != nil {
		return nil, err
	}
	waitQueueLength, err := meter.Int64Gauge("opencode_scale_wait_queue_length")
	if err != nil {
		return nil, err
	}
	allocationLatency, err := meter.Float64Histogram(
		"opencode_scale_allocation_latency",
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}
	return &PoolMetrics{
		poolSize:          poolSize,
		allocatedCount:    allocatedCount,
		waitQueueLength:   waitQueueLength,
		allocationLatency: allocationLatency,
	}, nil
}

// RecordAllocation records the latency of a single sandbox allocation.
func (m *PoolMetrics) RecordAllocation(ctx context.Context, duration time.Duration) {
	m.allocationLatency.Record(ctx, duration.Seconds())
}

// SetPoolSize reports the current total pool size.
func (m *PoolMetrics) SetPoolSize(ctx context.Context, size int64) {
	m.poolSize.Record(ctx, size)
}

// SetAllocatedCount reports the number of currently allocated sandboxes.
func (m *PoolMetrics) SetAllocatedCount(ctx context.Context, count int64) {
	m.allocatedCount.Record(ctx, count)
}

// SetQueueLength reports the current wait-queue length.
func (m *PoolMetrics) SetQueueLength(ctx context.Context, length int64) {
	m.waitQueueLength.Record(ctx, length)
}
