package workflow

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// WorkflowMetrics holds OpenTelemetry metric instruments for workflow tasks.
type WorkflowMetrics struct {
	taskDuration  metric.Float64Histogram
	taskStatus    metric.Int64Counter
	llmTokensUsed metric.Int64Counter
}

// NewWorkflowMetrics creates all workflow-related OTel metric instruments from the given meter.
func NewWorkflowMetrics(meter metric.Meter) (*WorkflowMetrics, error) {
	taskDuration, err := meter.Float64Histogram(
		"opencode_scale_task_duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of workflow task execution"),
	)
	if err != nil {
		return nil, err
	}

	taskStatus, err := meter.Int64Counter(
		"opencode_scale_task_status",
		metric.WithDescription("Count of workflow task completions by status"),
	)
	if err != nil {
		return nil, err
	}

	llmTokensUsed, err := meter.Int64Counter(
		"opencode_scale_llm_tokens_total",
		metric.WithDescription("Total LLM tokens used across workflow tasks"),
	)
	if err != nil {
		return nil, err
	}

	return &WorkflowMetrics{
		taskDuration:  taskDuration,
		taskStatus:    taskStatus,
		llmTokensUsed: llmTokensUsed,
	}, nil
}

// RecordTaskDuration records the duration of a workflow task along with its completion status.
func (m *WorkflowMetrics) RecordTaskDuration(ctx context.Context, duration time.Duration, status string) {
	m.taskDuration.Record(ctx, duration.Seconds(),
		metric.WithAttributes(attribute.String("status", status)),
	)
}

// RecordTaskStatus increments the task status counter for the given status (success, failure, or timeout).
func (m *WorkflowMetrics) RecordTaskStatus(ctx context.Context, status string) {
	m.taskStatus.Add(ctx, 1,
		metric.WithAttributes(attribute.String("status", status)),
	)
}

// RecordTokens adds the given token count to the cumulative LLM token counter.
func (m *WorkflowMetrics) RecordTokens(ctx context.Context, count int64) {
	m.llmTokensUsed.Add(ctx, count)
}
