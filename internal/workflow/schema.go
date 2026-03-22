package workflow

import "time"

type TaskInput struct {
	UserID       string        `json:"userId"`
	Prompt       string        `json:"prompt"`
	OutputSchema string        `json:"outputSchema,omitempty"` // JSON Schema string
	Timeout      time.Duration `json:"timeout,omitempty"`
}

type TaskOutput struct {
	SessionID  string        `json:"sessionId"`
	Result     string        `json:"result"`
	Duration   time.Duration `json:"duration"`
	TokensUsed int64         `json:"tokensUsed,omitempty"`
}

type ExecutionResult struct {
	Output     string `json:"output"`
	TokensUsed int64  `json:"tokensUsed"`
}

type SessionInfo struct {
	ID          string `json:"id"`
	SandboxName string `json:"sandboxName"`
	ServiceFQDN string `json:"serviceFqdn"`
}

type Allocation struct {
	SandboxName string `json:"sandboxName"`
	ServiceFQDN string `json:"serviceFqdn"`
	UserID      string `json:"userId"`
}
