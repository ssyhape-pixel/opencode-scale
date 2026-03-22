package v1

import "time"

// TaskStatus represents the status of a coding task
type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusTimeout   TaskStatus = "timeout"
)

// TaskRequest is the request body for creating a new coding task
type TaskRequest struct {
	Prompt       string            `json:"prompt"`
	OutputSchema string            `json:"outputSchema,omitempty"`
	Timeout      int               `json:"timeout,omitempty"` // seconds
	UserID       string            `json:"userId,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// TaskResponse is the response for a coding task
type TaskResponse struct {
	TaskID      string     `json:"taskId"`
	SessionID   string     `json:"sessionId,omitempty"`
	Status      TaskStatus `json:"status"`
	Result      string     `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

// QueuePosition represents a position in the wait queue
type QueuePosition struct {
	Position      int           `json:"position"`
	EstimatedWait time.Duration `json:"estimatedWait"`
}

// HealthResponse is the response for health check endpoint
type HealthResponse struct {
	Status          string  `json:"status"`
	Version         string  `json:"version"`
	PoolUtilization float64 `json:"poolUtilization"`
	PoolAllocated   int     `json:"poolAllocated"`
	PoolMaxSize     int     `json:"poolMaxSize"`
	QueueDepth      int     `json:"queueDepth"`
}

// TaskStreamEvent is a single SSE event emitted by the task stream endpoint.
type TaskStreamEvent struct {
	Status      TaskStatus `json:"status"`
	SessionID   string     `json:"sessionId,omitempty"`
	Result      string     `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`
	Duration    float64    `json:"duration,omitempty"`
	TokensUsed  int64      `json:"tokensUsed,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

// ErrorResponse is a standard error response
type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
