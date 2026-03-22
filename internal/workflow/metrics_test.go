package workflow

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/metric/noop"
)

func TestNewWorkflowMetrics(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	wm, err := NewWorkflowMetrics(meter)
	if err != nil {
		t.Fatalf("unexpected error creating noop workflow metrics: %v", err)
	}
	if wm == nil {
		t.Fatal("expected non-nil WorkflowMetrics")
	}
}

func TestRecordTaskDuration(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	wm, err := NewWorkflowMetrics(meter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	// Ensure recording does not panic with noop instruments.
	wm.RecordTaskDuration(ctx, 2*time.Second, "success")
	wm.RecordTaskDuration(ctx, 500*time.Millisecond, "failure")
	wm.RecordTaskDuration(ctx, 30*time.Second, "timeout")
}

func TestRecordTaskStatus(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	wm, err := NewWorkflowMetrics(meter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	// Ensure recording does not panic with noop instruments.
	wm.RecordTaskStatus(ctx, "success")
	wm.RecordTaskStatus(ctx, "failure")
	wm.RecordTaskStatus(ctx, "timeout")
}

func TestRecordTokens(t *testing.T) {
	meter := noop.NewMeterProvider().Meter("test")
	wm, err := NewWorkflowMetrics(meter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	// Ensure recording does not panic with noop instruments.
	wm.RecordTokens(ctx, 1500)
	wm.RecordTokens(ctx, 0)
}
