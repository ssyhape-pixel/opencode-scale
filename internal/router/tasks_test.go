package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	v1 "github.com/opencode-scale/opencode-scale/api/v1"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	workflowpb "go.temporal.io/api/workflow/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ---------- mock Temporal client ----------

type mockTemporalClient struct {
	client.Client

	executeWorkflowFn func(ctx context.Context, opts client.StartWorkflowOptions, wf interface{}, args ...interface{}) (client.WorkflowRun, error)
	describeFn        func(ctx context.Context, workflowID, runID string) (*workflowservice.DescribeWorkflowExecutionResponse, error)
	getWorkflowFn     func(ctx context.Context, workflowID, runID string) client.WorkflowRun
}

func (m *mockTemporalClient) ExecuteWorkflow(ctx context.Context, opts client.StartWorkflowOptions, wf interface{}, args ...interface{}) (client.WorkflowRun, error) {
	if m.executeWorkflowFn != nil {
		return m.executeWorkflowFn(ctx, opts, wf, args...)
	}
	return &mockWorkflowRun{id: "wf-123", runID: "run-456"}, nil
}

func (m *mockTemporalClient) DescribeWorkflowExecution(ctx context.Context, workflowID, runID string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	if m.describeFn != nil {
		return m.describeFn(ctx, workflowID, runID)
	}
	return nil, serviceerror.NewNotFound("not found")
}

func (m *mockTemporalClient) GetWorkflow(ctx context.Context, workflowID, runID string) client.WorkflowRun {
	if m.getWorkflowFn != nil {
		return m.getWorkflowFn(ctx, workflowID, runID)
	}
	return &mockWorkflowRun{id: workflowID, runID: runID}
}

type mockWorkflowRun struct {
	id    string
	runID string
	getFn func(ctx context.Context, valuePtr interface{}) error
}

func (r *mockWorkflowRun) GetID() string    { return r.id }
func (r *mockWorkflowRun) GetRunID() string { return r.runID }
func (r *mockWorkflowRun) GetWithOptions(_ context.Context, _ interface{}, _ client.WorkflowRunGetOptions) error {
	return nil
}
func (r *mockWorkflowRun) Get(ctx context.Context, valuePtr interface{}) error {
	if r.getFn != nil {
		return r.getFn(ctx, valuePtr)
	}
	return nil
}

// ---------- helpers ----------

func makeDescResponse(status enumspb.WorkflowExecutionStatus) *workflowservice.DescribeWorkflowExecutionResponse {
	now := timestamppb.Now()
	return &workflowservice.DescribeWorkflowExecutionResponse{
		WorkflowExecutionInfo: &workflowpb.WorkflowExecutionInfo{
			Status:    status,
			StartTime: now,
			CloseTime: now,
		},
	}
}

// ---------- createTask tests ----------

func TestCreateTask_Success(t *testing.T) {
	tc := &mockTemporalClient{}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	body := `{"prompt":"write hello world"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}

	var resp v1.TaskResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.TaskID != "wf-123" {
		t.Fatalf("expected taskId wf-123, got %s", resp.TaskID)
	}
	if resp.Status != v1.TaskStatusPending {
		t.Fatalf("expected status pending, got %s", resp.Status)
	}
}

func TestCreateTask_EmptyPrompt(t *testing.T) {
	tc := &mockTemporalClient{}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	body := `{"prompt":""}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateTask_InvalidJSON(t *testing.T) {
	tc := &mockTemporalClient{}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader("{invalid"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCreateTask_TimeoutCap(t *testing.T) {
	var capturedOpts client.StartWorkflowOptions
	tc := &mockTemporalClient{
		executeWorkflowFn: func(ctx context.Context, opts client.StartWorkflowOptions, wf interface{}, args ...interface{}) (client.WorkflowRun, error) {
			capturedOpts = opts
			return &mockWorkflowRun{id: "wf-cap", runID: "run-cap"}, nil
		},
	}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	// Request timeout of 10 hours (36000s), should be capped to 2h.
	body := `{"prompt":"test","timeout":36000}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}

	expectedMax := maxTaskTimeout + 10*time.Minute
	if capturedOpts.WorkflowExecutionTimeout != expectedMax {
		t.Fatalf("expected capped timeout %v, got %v", expectedMax, capturedOpts.WorkflowExecutionTimeout)
	}
}

// ---------- getTask tests ----------

func TestGetTask_NotFound(t *testing.T) {
	tc := &mockTemporalClient{
		describeFn: func(ctx context.Context, wid, rid string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
			return nil, serviceerror.NewNotFound("not found")
		},
	}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/unknown-id", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetTask_InternalError(t *testing.T) {
	tc := &mockTemporalClient{
		describeFn: func(ctx context.Context, wid, rid string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
			return nil, serviceerror.NewUnavailable("temporal down")
		},
	}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/some-id", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}

func TestGetTask_Running(t *testing.T) {
	tc := &mockTemporalClient{
		describeFn: func(ctx context.Context, wid, rid string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
			return makeDescResponse(enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING), nil
		},
	}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/running-wf", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp v1.TaskResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status != v1.TaskStatusRunning {
		t.Fatalf("expected status running, got %s", resp.Status)
	}
}

func TestGetTask_Completed(t *testing.T) {
	tc := &mockTemporalClient{
		describeFn: func(ctx context.Context, wid, rid string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
			return makeDescResponse(enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED), nil
		},
		getWorkflowFn: func(ctx context.Context, wid, rid string) client.WorkflowRun {
			return &mockWorkflowRun{id: wid, runID: rid}
		},
	}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/done-wf", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp v1.TaskResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status != v1.TaskStatusCompleted {
		t.Fatalf("expected status completed, got %s", resp.Status)
	}
	if resp.CompletedAt == nil {
		t.Fatal("expected completedAt to be set")
	}
}

func TestGetTask_Failed(t *testing.T) {
	tc := &mockTemporalClient{
		describeFn: func(ctx context.Context, wid, rid string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
			return makeDescResponse(enumspb.WORKFLOW_EXECUTION_STATUS_FAILED), nil
		},
	}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/fail-wf", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	var resp v1.TaskResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status != v1.TaskStatusFailed {
		t.Fatalf("expected status failed, got %s", resp.Status)
	}
}

func TestGetTask_TimedOut(t *testing.T) {
	tc := &mockTemporalClient{
		describeFn: func(ctx context.Context, wid, rid string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
			return makeDescResponse(enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT), nil
		},
	}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/timeout-wf", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	var resp v1.TaskResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status != v1.TaskStatusTimeout {
		t.Fatalf("expected status timeout, got %s", resp.Status)
	}
}

func TestGetTask_Terminated(t *testing.T) {
	tc := &mockTemporalClient{
		describeFn: func(ctx context.Context, wid, rid string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
			return makeDescResponse(enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED), nil
		},
	}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/terminated-wf", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	var resp v1.TaskResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status != v1.TaskStatusFailed {
		t.Fatalf("expected status failed for terminated, got %s", resp.Status)
	}
}

// ---------- streamTask tests ----------

func TestStreamTask_NotFound(t *testing.T) {
	tc := &mockTemporalClient{
		describeFn: func(ctx context.Context, wid, rid string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
			return nil, serviceerror.NewNotFound("not found")
		},
	}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/unknown-id/stream", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestStreamTask_CompletedImmediately(t *testing.T) {
	tc := &mockTemporalClient{
		describeFn: func(ctx context.Context, wid, rid string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
			return makeDescResponse(enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED), nil
		},
		getWorkflowFn: func(ctx context.Context, wid, rid string) client.WorkflowRun {
			return &mockWorkflowRun{id: wid, runID: rid}
		},
	}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/done-wf/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: result") {
		t.Fatalf("expected 'event: result' in body, got: %s", body)
	}
	if !strings.Contains(body, `"status":"completed"`) {
		t.Fatalf("expected completed status in body, got: %s", body)
	}
}

func TestStreamTask_RunningThenCompleted(t *testing.T) {
	callCount := 0
	tc := &mockTemporalClient{
		describeFn: func(ctx context.Context, wid, rid string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
			callCount++
			// First call (existence check) + second call (first poll) return running.
			// Third call returns completed.
			if callCount <= 2 {
				return makeDescResponse(enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING), nil
			}
			return makeDescResponse(enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED), nil
		},
		getWorkflowFn: func(ctx context.Context, wid, rid string) client.WorkflowRun {
			return &mockWorkflowRun{id: wid, runID: rid}
		},
	}
	h := NewTaskHandler(tc, "test-queue", testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/running-wf/stream", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: status") {
		t.Fatalf("expected 'event: status' in body, got: %s", body)
	}
	if !strings.Contains(body, "event: result") {
		t.Fatalf("expected 'event: result' in body, got: %s", body)
	}
}
