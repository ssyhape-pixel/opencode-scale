package controller

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ControllerMetrics holds OpenTelemetry metric instruments for the sandbox controller.
type ControllerMetrics struct {
	sandboxClaims metric.Int64Counter
	gcDeletions   metric.Int64Counter
}

// NewControllerMetrics creates all controller-related OTel metric instruments.
func NewControllerMetrics(meter metric.Meter) (*ControllerMetrics, error) {
	claims, err := meter.Int64Counter(
		"opencode_scale_sandbox_claims_total",
		metric.WithDescription("Total sandbox claim events by phase"),
	)
	if err != nil {
		return nil, err
	}

	gc, err := meter.Int64Counter(
		"opencode_scale_gc_deletions_total",
		metric.WithDescription("Total GC deletions of orphaned sandbox claims"),
	)
	if err != nil {
		return nil, err
	}

	return &ControllerMetrics{
		sandboxClaims: claims,
		gcDeletions:   gc,
	}, nil
}

// RecordClaimEvent increments the sandbox claim counter for the given phase.
func (m *ControllerMetrics) RecordClaimEvent(ctx context.Context, phase string) {
	m.sandboxClaims.Add(ctx, 1,
		metric.WithAttributes(attribute.String("phase", phase)),
	)
}

// RecordGCDeletion increments the GC deletion counter.
func (m *ControllerMetrics) RecordGCDeletion(ctx context.Context) {
	m.gcDeletions.Add(ctx, 1)
}
