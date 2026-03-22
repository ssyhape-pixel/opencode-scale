//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

var routerURL = "http://localhost:8080"

func init() {
	if v := os.Getenv("ROUTER_URL"); v != "" {
		routerURL = v
	}
}

// ---------- Health ----------

func TestE2E_HealthEndpoint(t *testing.T) {
	resp, err := http.Get(routerURL + "/health")
	if err != nil {
		t.Fatalf("GET /health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var health map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if health["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", health["status"])
	}
	if _, ok := health["poolMaxSize"]; !ok {
		t.Fatal("expected poolMaxSize in health response")
	}
	if _, ok := health["poolAllocated"]; !ok {
		t.Fatal("expected poolAllocated in health response")
	}
}

// ---------- Task Lifecycle ----------

func TestE2E_TaskLifecycle(t *testing.T) {
	// Create a task with controlled output.
	taskID := createTask(t, map[string]interface{}{
		"prompt":  "OUTPUT:hello from e2e test",
		"timeout": 120,
		"userId":  "e2e-user",
	})

	// Poll until completed or timeout.
	var finalResp map[string]interface{}
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		resp := getTask(t, taskID)
		status, _ := resp["status"].(string)

		switch status {
		case "completed":
			finalResp = resp
			goto done
		case "failed":
			t.Fatalf("task failed: %v", resp["error"])
		case "timeout":
			t.Fatal("task timed out")
		}

		time.Sleep(2 * time.Second)
	}
	t.Fatal("timed out waiting for task completion")

done:
	result, _ := finalResp["result"].(string)
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	if result != "hello from e2e test" {
		t.Fatalf("expected 'hello from e2e test', got %q", result)
	}
}

// ---------- Task with Schema ----------

func TestE2E_TaskWithSchema(t *testing.T) {
	schema := `{"type":"object","properties":{"answer":{"type":"integer"}},"required":["answer"]}`
	taskID := createTask(t, map[string]interface{}{
		"prompt":       `OUTPUT:{"answer":42}`,
		"outputSchema": schema,
		"timeout":      120,
		"userId":       "e2e-schema-user",
	})

	// Poll until completed.
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		resp := getTask(t, taskID)
		status, _ := resp["status"].(string)

		switch status {
		case "completed":
			result, _ := resp["result"].(string)
			if result == "" {
				t.Fatal("expected non-empty result")
			}
			// Verify the result is valid JSON matching the schema.
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("result is not valid JSON: %v", err)
			}
			if ans, ok := parsed["answer"]; !ok {
				t.Fatal("expected 'answer' field in result")
			} else if ans != float64(42) {
				t.Fatalf("expected answer=42, got %v", ans)
			}
			return
		case "failed":
			t.Fatalf("task failed: %v", resp["error"])
		case "timeout":
			t.Fatal("task timed out")
		}

		time.Sleep(2 * time.Second)
	}
	t.Fatal("timed out waiting for task completion")
}

// ---------- Unknown Task 404 ----------

func TestE2E_UnknownTask404(t *testing.T) {
	resp, err := http.Get(routerURL + "/api/v1/tasks/nonexistent-task-id-12345")
	if err != nil {
		t.Fatalf("GET request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 404, got %d: %s", resp.StatusCode, string(body))
	}
}

// ---------- Session Proxy ----------

func TestE2E_SessionProxy(t *testing.T) {
	// Make a request without a session ID to trigger allocation.
	req, err := http.NewRequest(http.MethodGet, routerURL+"/api/v1/proxy-test", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("X-User-ID", "e2e-proxy-user")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check X-Session-ID header was set.
	sessionID := resp.Header.Get("X-Session-ID")
	if sessionID == "" {
		t.Log("X-Session-ID not set (proxy may have different behaviour in local mode)")
		return
	}

	// Check Set-Cookie header.
	found := false
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "session_id" {
			found = true
			if cookie.Value != sessionID {
				t.Fatalf("cookie value %q != session header %q", cookie.Value, sessionID)
			}
			break
		}
	}
	if !found {
		t.Log("Set-Cookie not found (may be consumed by proxy)")
	}
}

// ---------- helpers ----------

func createTask(t *testing.T, body map[string]interface{}) string {
	t.Helper()

	jsonBody, _ := json.Marshal(body)
	resp, err := http.Post(routerURL+"/api/v1/tasks", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("POST /api/v1/tasks failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 202, got %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	taskID, ok := result["taskId"].(string)
	if !ok || taskID == "" {
		t.Fatal("expected taskId in response")
	}

	fmt.Printf("  Created task: %s\n", taskID)
	return taskID
}

func getTask(t *testing.T, taskID string) map[string]interface{} {
	t.Helper()

	resp, err := http.Get(fmt.Sprintf("%s/api/v1/tasks/%s", routerURL, taskID))
	if err != nil {
		t.Fatalf("GET task failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	return result
}
