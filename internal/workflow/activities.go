package workflow

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.temporal.io/sdk/activity"

	"github.com/opencode-scale/opencode-scale/internal/opencode"
	"github.com/opencode-scale/opencode-scale/internal/pool"
	"github.com/opencode-scale/opencode-scale/internal/schema"
)

var activityTracer = otel.Tracer("opencode-scale/activities")

// Activities holds dependencies for Temporal activity implementations.
type Activities struct {
	Pool      *pool.PoolManager
	Validator *schema.Validator
	Metrics   *WorkflowMetrics
}

// AllocateSandboxActivity reserves a sandbox for the given user via the pool manager.
func (a *Activities) AllocateSandboxActivity(ctx context.Context, userID string) (*Allocation, error) {
	ctx, span := activityTracer.Start(ctx, "AllocateSandboxActivity")
	defer span.End()
	span.SetAttributes(attribute.String("user.id", userID))

	if a.Pool == nil {
		// Fallback mock for testing / Phase 1.
		return &Allocation{
			SandboxName: fmt.Sprintf("sandbox-%s-%d", userID, time.Now().UnixMilli()),
			ServiceFQDN: fmt.Sprintf("sandbox-%s.opencode-scale.svc.cluster.local:4096", userID),
			UserID:      userID,
		}, nil
	}

	alloc, err := a.Pool.Allocate(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("allocating sandbox: %w", err)
	}

	return &Allocation{
		SandboxName: alloc.SandboxName,
		ServiceFQDN: alloc.ServiceFQDN,
		UserID:      userID,
	}, nil
}

// CreateSessionActivity creates an OpenCode session on the allocated sandbox.
func (a *Activities) CreateSessionActivity(ctx context.Context, alloc *Allocation) (*SessionInfo, error) {
	ctx, span := activityTracer.Start(ctx, "CreateSessionActivity")
	defer span.End()
	span.SetAttributes(attribute.String("sandbox.name", alloc.SandboxName))

	client := opencode.NewClient("http://" + alloc.ServiceFQDN)

	session, err := client.CreateSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating opencode session: %w", err)
	}

	return &SessionInfo{
		ID:          session.ID,
		SandboxName: alloc.SandboxName,
		ServiceFQDN: alloc.ServiceFQDN,
	}, nil
}

// ExecutePromptActivity sends a prompt to OpenCode and streams the response.
// Reports heartbeat periodically so Temporal knows the activity is alive.
func (a *Activities) ExecutePromptActivity(ctx context.Context, session *SessionInfo, prompt string) (*ExecutionResult, error) {
	ctx, span := activityTracer.Start(ctx, "ExecutePromptActivity")
	defer span.End()
	span.SetAttributes(attribute.String("session.id", session.ID))

	activity.RecordHeartbeat(ctx, "starting prompt execution")

	client := opencode.NewClient("http://" + session.ServiceFQDN)

	sendResult, err := client.SendMessage(ctx, session.ID, prompt, func(progress string) {
		activity.RecordHeartbeat(ctx, progress)
	})
	if err != nil {
		return nil, fmt.Errorf("executing prompt: %w", err)
	}

	activity.RecordHeartbeat(ctx, "prompt execution complete")

	result := &ExecutionResult{
		Output:     sendResult.Content,
		TokensUsed: sendResult.TokensUsed,
	}

	if a.Metrics != nil {
		a.Metrics.RecordTokens(ctx, int64(result.TokensUsed))
	}

	return result, nil
}

// ValidateOutputActivity validates the execution result against a JSON schema.
// Uses ValidateWithFeedback for structured error reporting. The workflow can
// use the feedback to re-prompt the LLM if validation fails.
func (a *Activities) ValidateOutputActivity(ctx context.Context, result *ExecutionResult, jsonSchema string) (*ExecutionResult, error) {
	_, span := activityTracer.Start(ctx, "ValidateOutputActivity")
	defer span.End()

	v := a.Validator
	if v == nil {
		v = schema.NewValidator()
	}

	vr := v.ValidateWithFeedback([]byte(result.Output), jsonSchema)
	if !vr.Valid {
		// Return a structured error so the workflow can decide to retry.
		return nil, &ValidationError{
			Feedback:     vr.Feedback,
			FailedOutput: vr.Output,
		}
	}

	return &ExecutionResult{
		Output:     vr.Output,
		TokensUsed: result.TokensUsed,
	}, nil
}

// ValidationError is returned when schema validation fails. It carries
// feedback suitable for constructing a re-prompt.
type ValidationError struct {
	Feedback     string
	FailedOutput string
}

func (e *ValidationError) Error() string {
	return "output validation failed: " + e.Feedback
}

// ReleaseSandboxActivity releases a sandbox back to the pool.
func (a *Activities) ReleaseSandboxActivity(ctx context.Context, sandboxName string) error {
	ctx, span := activityTracer.Start(ctx, "ReleaseSandboxActivity")
	defer span.End()
	span.SetAttributes(attribute.String("sandbox.name", sandboxName))

	if a.Pool == nil {
		return nil
	}
	return a.Pool.Release(ctx, sandboxName)
}

// --- Standalone functions for backward compatibility / testing ---

// AllocateSandboxActivity is a standalone function for use without dependency injection.
func AllocateSandboxActivity(ctx context.Context, userID string) (*Allocation, error) {
	return (&Activities{}).AllocateSandboxActivity(ctx, userID)
}

// CreateSessionActivity is a standalone function (mock) for testing.
func CreateSessionActivity(ctx context.Context, alloc *Allocation) (*SessionInfo, error) {
	return &SessionInfo{
		ID:          fmt.Sprintf("sess-%d", time.Now().UnixMilli()),
		SandboxName: alloc.SandboxName,
		ServiceFQDN: alloc.ServiceFQDN,
	}, nil
}

// ExecutePromptActivity is a standalone function (mock) for testing.
func ExecutePromptActivity(ctx context.Context, session *SessionInfo, prompt string) (*ExecutionResult, error) {
	activity.RecordHeartbeat(ctx, "starting prompt execution")
	activity.RecordHeartbeat(ctx, "prompt execution complete")
	return &ExecutionResult{
		Output:     fmt.Sprintf("placeholder result for prompt: %s", prompt),
		TokensUsed: 0,
	}, nil
}

// ValidateOutputActivity is a standalone function for testing.
func ValidateOutputActivity(ctx context.Context, result *ExecutionResult, jsonSchema string) (*ExecutionResult, error) {
	return (&Activities{}).ValidateOutputActivity(ctx, result, jsonSchema)
}

// ReleaseSandboxActivity is a standalone function (no-op) for testing.
func ReleaseSandboxActivity(ctx context.Context, sandboxName string) error {
	return nil
}
