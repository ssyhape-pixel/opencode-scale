package workflow

import (
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/testsuite"
)

func TestCodingTaskWorkflow_HappyPath(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// Register mocked activities.
	env.RegisterActivity(AllocateSandboxActivity)
	env.RegisterActivity(CreateSessionActivity)
	env.RegisterActivity(ExecutePromptActivity)
	env.RegisterActivity(ValidateOutputActivity)
	env.RegisterActivity(ReleaseSandboxActivity)

	userID := "test-user"
	prompt := "write a hello world"

	env.OnActivity(AllocateSandboxActivity, mock.Anything, userID).Return(
		&Allocation{
			SandboxName: "sandbox-test-user-1",
			ServiceFQDN: "sandbox-test-user.svc.local",
			UserID:      userID,
		}, nil,
	)

	env.OnActivity(CreateSessionActivity, mock.Anything, mock.Anything).Return(
		&SessionInfo{
			ID:          "sess-001",
			SandboxName: "sandbox-test-user-1",
			ServiceFQDN: "sandbox-test-user.svc.local",
		}, nil,
	)

	env.OnActivity(ExecutePromptActivity, mock.Anything, mock.Anything, prompt).Return(
		&ExecutionResult{
			Output:     "hello world program output",
			TokensUsed: 500,
		}, nil,
	)

	env.OnActivity(ReleaseSandboxActivity, mock.Anything, "sandbox-test-user-1").Return(nil)

	input := TaskInput{
		UserID:  userID,
		Prompt:  prompt,
		Timeout: 10 * time.Minute,
	}

	env.ExecuteWorkflow(CodingTaskWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var output TaskOutput
	require.NoError(t, env.GetWorkflowResult(&output))

	require.Equal(t, "sess-001", output.SessionID)
	require.Equal(t, "hello world program output", output.Result)
	require.Equal(t, int64(500), output.TokensUsed)
	require.True(t, output.Duration >= 0)
}

func TestCodingTaskWorkflow_WithSchema(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivity(AllocateSandboxActivity)
	env.RegisterActivity(CreateSessionActivity)
	env.RegisterActivity(ExecutePromptActivity)
	env.RegisterActivity(ValidateOutputActivity)
	env.RegisterActivity(ReleaseSandboxActivity)

	userID := "schema-user"
	prompt := "generate JSON output"
	outputSchema := `{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}`

	env.OnActivity(AllocateSandboxActivity, mock.Anything, userID).Return(
		&Allocation{
			SandboxName: "sandbox-schema-1",
			ServiceFQDN: "sandbox-schema.svc.local",
			UserID:      userID,
		}, nil,
	)

	env.OnActivity(CreateSessionActivity, mock.Anything, mock.Anything).Return(
		&SessionInfo{
			ID:          "sess-002",
			SandboxName: "sandbox-schema-1",
			ServiceFQDN: "sandbox-schema.svc.local",
		}, nil,
	)

	rawOutput := `{"answer":"42"}`
	env.OnActivity(ExecutePromptActivity, mock.Anything, mock.Anything, prompt).Return(
		&ExecutionResult{
			Output:     rawOutput,
			TokensUsed: 200,
		}, nil,
	)

	validatedOutput := `{"answer":"42"}`
	env.OnActivity(ValidateOutputActivity, mock.Anything, mock.Anything, outputSchema).Return(
		&ExecutionResult{
			Output:     validatedOutput,
			TokensUsed: 200,
		}, nil,
	)

	env.OnActivity(ReleaseSandboxActivity, mock.Anything, "sandbox-schema-1").Return(nil)

	input := TaskInput{
		UserID:       userID,
		Prompt:       prompt,
		OutputSchema: outputSchema,
		Timeout:      5 * time.Minute,
	}

	env.ExecuteWorkflow(CodingTaskWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var output TaskOutput
	require.NoError(t, env.GetWorkflowResult(&output))

	require.Equal(t, "sess-002", output.SessionID)
	require.Equal(t, validatedOutput, output.Result)
	require.Equal(t, int64(200), output.TokensUsed)
}
