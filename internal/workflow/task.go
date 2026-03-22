package workflow

import (
	"fmt"
	"time"

	"github.com/opencode-scale/opencode-scale/internal/schema"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const maxValidationRetries = 3

// CodingTaskWorkflow orchestrates a coding task: allocate sandbox, create session,
// execute prompt, optionally validate output, and release sandbox.
func CodingTaskWorkflow(ctx workflow.Context, input TaskInput) (TaskOutput, error) {
	startTime := workflow.Now(ctx)
	var output TaskOutput
	var sandboxName string

	// Ensure sandbox is always released, even on error.
	defer func() {
		if sandboxName != "" {
			// Use workflow.Go to run cleanup asynchronously since defer
			// cannot use activity calls directly.
			cleanupCtx, _ := workflow.NewDisconnectedContext(ctx)
			releaseCtx := workflow.WithActivityOptions(cleanupCtx, workflow.ActivityOptions{
				StartToCloseTimeout: 5 * time.Minute,
				RetryPolicy: &temporal.RetryPolicy{
					MaximumAttempts:    3,
					InitialInterval:    5 * time.Second,
					BackoffCoefficient: 2.0,
				},
			})
			_ = workflow.ExecuteActivity(releaseCtx, ReleaseSandboxActivity, sandboxName).Get(releaseCtx, nil)
		}
	}()

	// Step 1: Allocate Sandbox.
	allocCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:        3,
			InitialInterval:        5 * time.Second,
			BackoffCoefficient:     2.0,
			NonRetryableErrorTypes: []string{"pool: all sandbox slots are in use"},
		},
	})

	var alloc Allocation
	if err := workflow.ExecuteActivity(allocCtx, AllocateSandboxActivity, input.UserID).Get(allocCtx, &alloc); err != nil {
		return output, fmt.Errorf("allocating sandbox: %w", err)
	}
	sandboxName = alloc.SandboxName

	// Step 2: Create OpenCode Session.
	sessionCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts:    3,
			InitialInterval:    2 * time.Second,
			BackoffCoefficient: 2.0,
		},
	})

	var session SessionInfo
	if err := workflow.ExecuteActivity(sessionCtx, CreateSessionActivity, &alloc).Get(sessionCtx, &session); err != nil {
		return output, fmt.Errorf("creating session: %w", err)
	}
	output.SessionID = session.ID

	// Step 3: Execute Prompt.
	execTimeout := 30 * time.Minute
	if input.Timeout > 0 {
		execTimeout = input.Timeout
	}
	execCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: execTimeout,
		HeartbeatTimeout:    1 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 1,
		},
	})

	var execResult ExecutionResult
	if err := workflow.ExecuteActivity(execCtx, ExecutePromptActivity, &session, input.Prompt).Get(execCtx, &execResult); err != nil {
		return output, fmt.Errorf("executing prompt: %w", err)
	}
	output.TokensUsed = execResult.TokensUsed

	// Step 4: Validate output if schema provided, with retry loop.
	if input.OutputSchema != "" {
		validateCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
			StartToCloseTimeout: 1 * time.Minute,
			RetryPolicy: &temporal.RetryPolicy{
				MaximumAttempts: 1,
			},
		})

		var validated ExecutionResult
		var lastErr error

		for attempt := 1; attempt <= maxValidationRetries; attempt++ {
			lastErr = workflow.ExecuteActivity(validateCtx, ValidateOutputActivity, &execResult, input.OutputSchema).Get(validateCtx, &validated)
			if lastErr == nil {
				execResult = validated
				break
			}

			// If this is not the last attempt, re-prompt the LLM to fix its output.
			if attempt < maxValidationRetries {
				retryPrompt := schema.BuildRetryPrompt(
					input.Prompt,
					execResult.Output,
					lastErr.Error(),
					input.OutputSchema,
				)

				var retryResult ExecutionResult
				retryErr := workflow.ExecuteActivity(execCtx, ExecutePromptActivity, &session, retryPrompt).Get(execCtx, &retryResult)
				if retryErr != nil {
					return output, fmt.Errorf("retry prompt (attempt %d): %w", attempt, retryErr)
				}
				execResult = retryResult
				output.TokensUsed += retryResult.TokensUsed
			}
		}

		if lastErr != nil {
			return output, fmt.Errorf("validation failed after %d attempts: %w", maxValidationRetries, lastErr)
		}
	}

	output.Result = execResult.Output
	output.Duration = workflow.Now(ctx).Sub(startTime)

	return output, nil
}
