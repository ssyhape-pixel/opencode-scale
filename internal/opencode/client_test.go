package opencode

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCreateSession_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/sessions" {
			t.Fatalf("expected /sessions, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":"sess-abc-123"}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	session, err := c.CreateSession(context.Background())
	if err != nil {
		t.Fatalf("CreateSession error: %v", err)
	}
	if session.ID != "sess-abc-123" {
		t.Fatalf("expected session id sess-abc-123, got %s", session.ID)
	}
}

func TestCreateSession_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.CreateSession(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error to contain 500, got: %v", err)
	}
}

func TestSendMessage_SSEStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/sess-1/messages" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected Flusher")
		}

		events := []string{
			`data: {"content":"Hello"}`,
			"",
			`data: {"content":" World"}`,
			"",
			"data: [DONE]",
			"",
		}
		for _, e := range events {
			fmt.Fprintln(w, e)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	result, err := c.SendMessage(context.Background(), "sess-1", "say hello", nil)
	if err != nil {
		t.Fatalf("SendMessage error: %v", err)
	}
	if result.Content != "Hello World" {
		t.Fatalf("expected 'Hello World', got %q", result.Content)
	}
}

func TestSendMessage_JSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"content":"json response"}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	result, err := c.SendMessage(context.Background(), "sess-1", "hello", nil)
	if err != nil {
		t.Fatalf("SendMessage error: %v", err)
	}
	if result.Content != "json response" {
		t.Fatalf("expected 'json response', got %q", result.Content)
	}
}

func TestSendMessage_MultiLineData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Multi-line data: fields for a single event.
		lines := "data: {\"content\":\"part1\"}\n\ndata: {\"content\":\"part2\"}\n\ndata: [DONE]\n\n"
		fmt.Fprint(w, lines)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	result, err := c.SendMessage(context.Background(), "sess-1", "multi", nil)
	if err != nil {
		t.Fatalf("SendMessage error: %v", err)
	}
	if result.Content != "part1part2" {
		t.Fatalf("expected 'part1part2', got %q", result.Content)
	}
}

func TestSendMessage_Heartbeat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Send 25 events to trigger heartbeats at 10 and 20.
		for i := 0; i < 25; i++ {
			fmt.Fprintf(w, "data: {\"content\":\"x\"}\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	heartbeatCount := 0
	c := NewClient(srv.URL)
	_, err := c.SendMessage(context.Background(), "sess-1", "test", func(msg string) {
		heartbeatCount++
	})
	if err != nil {
		t.Fatalf("SendMessage error: %v", err)
	}
	if heartbeatCount != 2 {
		t.Fatalf("expected 2 heartbeats, got %d", heartbeatCount)
	}
}

func TestSendMessage_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		flusher.Flush()

		// Block forever.
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	c := NewClient(srv.URL)
	_, err := c.SendMessage(ctx, "sess-1", "test", nil)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestSendMessage_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "bad request")
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.SendMessage(context.Background(), "sess-1", "test", nil)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected error to contain 400, got: %v", err)
	}
}

func TestSendMessage_TokenExtraction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected Flusher")
		}

		// Content event.
		fmt.Fprint(w, "data: {\"content\":\"hello\"}\n\n")
		flusher.Flush()
		// Usage event with token counts.
		fmt.Fprint(w, "data: {\"content\":\"\",\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20,\"total_tokens\":30}}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	result, err := c.SendMessage(context.Background(), "sess-1", "test", nil)
	if err != nil {
		t.Fatalf("SendMessage error: %v", err)
	}
	if result.Content != "hello" {
		t.Fatalf("expected 'hello', got %q", result.Content)
	}
	if result.TokensUsed != 30 {
		t.Fatalf("expected 30 tokens, got %d", result.TokensUsed)
	}
}

func TestSendMessage_TokenFromHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Usage-Total-Tokens", "42")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected Flusher")
		}

		fmt.Fprint(w, "data: {\"content\":\"hi\"}\n\n")
		flusher.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	result, err := c.SendMessage(context.Background(), "sess-1", "test", nil)
	if err != nil {
		t.Fatalf("SendMessage error: %v", err)
	}
	if result.TokensUsed != 42 {
		t.Fatalf("expected 42 tokens from header, got %d", result.TokensUsed)
	}
}

func TestNewClient_TransportConfig(t *testing.T) {
	c := NewClient("http://localhost:4096")
	if c.httpClient.Timeout != 0 {
		t.Fatalf("expected zero timeout, got %v", c.httpClient.Timeout)
	}
	if c.httpClient.Transport == nil {
		t.Fatal("expected transport to be configured")
	}
}
