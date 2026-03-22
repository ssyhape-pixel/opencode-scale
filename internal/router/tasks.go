package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	v1 "github.com/opencode-scale/opencode-scale/api/v1"
	"github.com/opencode-scale/opencode-scale/internal/workflow"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
)

var taskTracer = otel.Tracer("opencode-scale/tasks")

const maxTaskTimeout = 2 * time.Hour

// TaskHandler handles /api/v1/tasks endpoints for submitting and querying
// Temporal workflow-based coding tasks.
type TaskHandler struct {
	temporal  client.Client
	taskQueue string
	logger    *slog.Logger
}

// NewTaskHandler creates a handler for task API endpoints.
func NewTaskHandler(tc client.Client, taskQueue string, logger *slog.Logger) *TaskHandler {
	return &TaskHandler{
		temporal:  tc,
		taskQueue: taskQueue,
		logger:    logger,
	}
}

// ServeHTTP routes task requests: POST for create, GET for status.
func (h *TaskHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// POST /api/v1/tasks → create task
	if r.Method == http.MethodPost && (r.URL.Path == "/api/v1/tasks" || r.URL.Path == "/api/v1/tasks/") {
		h.createTask(w, r)
		return
	}

	// GET /api/v1/tasks/{taskId}/stream → SSE stream
	// GET /api/v1/tasks/{taskId} → get task status
	if r.Method == http.MethodGet {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "tasks" && parts[4] == "stream" {
			h.streamTask(w, r, parts[3])
			return
		}
		if len(parts) == 4 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "tasks" {
			h.getTask(w, r, parts[3])
			return
		}
	}

	http.NotFound(w, r)
}

func (h *TaskHandler) createTask(w http.ResponseWriter, r *http.Request) {
	ctx, span := taskTracer.Start(r.Context(), "TaskHandler.createTask")
	defer span.End()
	r = r.WithContext(ctx)

	var req v1.TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, v1.ErrorResponse{
			Code:    400,
			Message: "invalid request body: " + err.Error(),
		})
		return
	}

	if req.Prompt == "" {
		writeJSON(w, http.StatusBadRequest, v1.ErrorResponse{
			Code:    400,
			Message: "prompt is required",
		})
		return
	}

	userID := req.UserID
	if userID == "" {
		userID = r.Header.Get("X-User-ID")
	}
	if userID == "" {
		userID = "anonymous"
	}

	timeout := 30 * time.Minute
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	if timeout > maxTaskTimeout {
		timeout = maxTaskTimeout
	}

	input := workflow.TaskInput{
		UserID:       userID,
		Prompt:       req.Prompt,
		OutputSchema: req.OutputSchema,
		Timeout:      timeout,
	}

	opts := client.StartWorkflowOptions{
		TaskQueue:                h.taskQueue,
		WorkflowExecutionTimeout: timeout + 10*time.Minute, // Extra buffer.
	}

	run, err := h.temporal.ExecuteWorkflow(r.Context(), opts, workflow.CodingTaskWorkflow, input)
	if err != nil {
		h.logger.Error("failed to start workflow", "error", err)
		writeJSON(w, http.StatusInternalServerError, v1.ErrorResponse{
			Code:    500,
			Message: "failed to start task",
		})
		return
	}

	now := time.Now()
	resp := v1.TaskResponse{
		TaskID:    run.GetID(),
		Status:    v1.TaskStatusPending,
		CreatedAt: now,
	}

	h.logger.Info("task created", "taskId", run.GetID(), "runId", run.GetRunID(), "userID", userID)
	writeJSON(w, http.StatusAccepted, resp)
}

func (h *TaskHandler) getTask(w http.ResponseWriter, r *http.Request, taskID string) {
	ctx, span := taskTracer.Start(r.Context(), "TaskHandler.getTask")
	defer span.End()
	span.SetAttributes(attribute.String("task.id", taskID))
	r = r.WithContext(ctx)

	desc, err := h.temporal.DescribeWorkflowExecution(r.Context(), taskID, "")
	if err != nil {
		var notFound *serviceerror.NotFound
		if errors.As(err, &notFound) {
			writeJSON(w, http.StatusNotFound, v1.ErrorResponse{
				Code:    404,
				Message: "task not found",
			})
			return
		}
		h.logger.Error("failed to describe workflow", "taskId", taskID, "error", err)
		writeJSON(w, http.StatusInternalServerError, v1.ErrorResponse{
			Code:    500,
			Message: "failed to query task status",
		})
		return
	}

	info := desc.WorkflowExecutionInfo
	resp := v1.TaskResponse{
		TaskID:    taskID,
		CreatedAt: info.StartTime.AsTime(),
	}

	switch info.Status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		resp.Status = v1.TaskStatusRunning
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		resp.Status = v1.TaskStatusCompleted
		closeTime := info.CloseTime.AsTime()
		resp.CompletedAt = &closeTime

		// Try to get the result.
		run := h.temporal.GetWorkflow(r.Context(), taskID, "")
		var output workflow.TaskOutput
		if err := run.Get(r.Context(), &output); err == nil {
			resp.SessionID = output.SessionID
			resp.Result = output.Result
		}
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		resp.Status = v1.TaskStatusFailed
		resp.Error = "workflow execution failed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		resp.Status = v1.TaskStatusTimeout
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED,
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		resp.Status = v1.TaskStatusFailed
		resp.Error = "workflow was cancelled"
	default:
		resp.Status = v1.TaskStatusPending
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *TaskHandler) streamTask(w http.ResponseWriter, r *http.Request, taskID string) {
	ctx, span := taskTracer.Start(r.Context(), "TaskHandler.streamTask")
	defer span.End()
	span.SetAttributes(attribute.String("task.id", taskID))
	r = r.WithContext(ctx)

	// Verify the task exists.
	_, err := h.temporal.DescribeWorkflowExecution(r.Context(), taskID, "")
	if err != nil {
		var notFound *serviceerror.NotFound
		if errors.As(err, &notFound) {
			writeJSON(w, http.StatusNotFound, v1.ErrorResponse{
				Code:    404,
				Message: "task not found",
			})
			return
		}
		h.logger.Error("failed to describe workflow for stream", "taskId", taskID, "error", err)
		writeJSON(w, http.StatusInternalServerError, v1.ErrorResponse{
			Code:    500,
			Message: "failed to query task status",
		})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, v1.ErrorResponse{
			Code:    500,
			Message: "streaming not supported",
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			event, terminal := h.buildStreamEvent(r.Context(), taskID)
			data, _ := json.Marshal(event)

			if terminal {
				fmt.Fprintf(w, "event: result\ndata: %s\n\n", data)
				flusher.Flush()
				return
			}

			fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// buildStreamEvent queries Temporal for the current task state and returns
// a stream event. The second return value is true when the task has reached
// a terminal state.
func (h *TaskHandler) buildStreamEvent(ctx context.Context, taskID string) (v1.TaskStreamEvent, bool) {
	desc, err := h.temporal.DescribeWorkflowExecution(ctx, taskID, "")
	if err != nil {
		return v1.TaskStreamEvent{Status: v1.TaskStatusRunning}, false
	}

	info := desc.WorkflowExecutionInfo
	event := v1.TaskStreamEvent{}

	switch info.Status {
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		event.Status = v1.TaskStatusRunning
		return event, false
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		event.Status = v1.TaskStatusCompleted
		closeTime := info.CloseTime.AsTime()
		event.CompletedAt = &closeTime

		run := h.temporal.GetWorkflow(ctx, taskID, "")
		var output workflow.TaskOutput
		if err := run.Get(ctx, &output); err == nil {
			event.SessionID = output.SessionID
			event.Result = output.Result
			event.Duration = output.Duration.Seconds()
			event.TokensUsed = output.TokensUsed
		}
		return event, true
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		event.Status = v1.TaskStatusFailed
		event.Error = "workflow execution failed"
		return event, true
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		event.Status = v1.TaskStatusTimeout
		return event, true
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED,
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		event.Status = v1.TaskStatusFailed
		event.Error = "workflow was cancelled"
		return event, true
	default:
		event.Status = v1.TaskStatusPending
		return event, false
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
