package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var ocTracer = otel.Tracer("opencode-scale/opencode")

// Client talks to an OpenCode Server instance over HTTP.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates an OpenCode HTTP client for the given base URL.
func NewClient(baseURL string) *Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   5,
	}

	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout:   0, // No overall timeout — activity heartbeat manages liveness.
			Transport: transport,
		},
	}
}

// Session represents an OpenCode server session.
type Session struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
}

// Message represents a message sent to an OpenCode session.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamEvent represents a single SSE event from OpenCode.
type StreamEvent struct {
	Event string `json:"event,omitempty"`
	Data  string `json:"data,omitempty"`
}

// SendResult contains the response content and token usage from a SendMessage call.
type SendResult struct {
	Content    string
	TokensUsed int64
}

// CreateSession creates a new session on the OpenCode server.
func (c *Client) CreateSession(ctx context.Context) (*Session, error) {
	ctx, span := ocTracer.Start(ctx, "OpenCode.CreateSession")
	defer span.End()

	url := c.baseURL + "/sessions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("opencode: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencode: creating session: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opencode: create session status %d: %s", resp.StatusCode, string(body))
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("opencode: decoding session: %w", err)
	}

	return &session, nil
}

// SendMessage sends a prompt to an OpenCode session and collects the full response.
// It reads the SSE stream and calls heartbeatFn periodically so the caller can
// report liveness (e.g. Temporal activity heartbeat).
func (c *Client) SendMessage(ctx context.Context, sessionID, prompt string, heartbeatFn func(string)) (SendResult, error) {
	ctx, span := ocTracer.Start(ctx, "OpenCode.SendMessage")
	defer span.End()
	span.SetAttributes(attribute.String("session.id", sessionID))

	url := fmt.Sprintf("%s/sessions/%s/messages", c.baseURL, sessionID)

	body, err := json.Marshal(map[string]string{
		"role":    "user",
		"content": prompt,
	})
	if err != nil {
		return SendResult{}, fmt.Errorf("opencode: marshaling message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return SendResult{}, fmt.Errorf("opencode: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return SendResult{}, fmt.Errorf("opencode: sending message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return SendResult{}, fmt.Errorf("opencode: send message status %d: %s", resp.StatusCode, string(respBody))
	}

	// Check if response is SSE stream.
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		content, tokens, err := c.readSSEStream(resp.Body, resp.Header, heartbeatFn)
		if err != nil {
			return SendResult{}, err
		}
		return SendResult{Content: content, TokensUsed: tokens}, nil
	}

	// Plain JSON response.
	var result struct {
		Content string                 `json:"content"`
		Usage   map[string]interface{} `json:"usage,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return SendResult{}, fmt.Errorf("opencode: decoding response: %w", err)
	}
	var tokens int64
	if result.Usage != nil {
		if v, ok := result.Usage["total_tokens"].(float64); ok {
			tokens = int64(v)
		}
	}
	return SendResult{Content: result.Content, TokensUsed: tokens}, nil
}

// readSSEStream reads SSE events and accumulates the assistant's response.
// Supports multi-line data: fields and large payloads up to 1MB per line.
// Returns the accumulated content, total tokens used, and any error.
func (c *Client) readSSEStream(reader io.Reader, headers http.Header, heartbeatFn func(string)) (string, int64, error) {
	scanner := bufio.NewScanner(reader)
	// Increase buffer to 1MB for large SSE payloads.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var result strings.Builder
	var dataBuf strings.Builder
	var tokensUsed int64
	eventCount := 0

	for scanner.Scan() {
		line := scanner.Text()

		// Empty line = end of SSE event, process accumulated data.
		if line == "" {
			if dataBuf.Len() > 0 {
				data := dataBuf.String()
				dataBuf.Reset()

				if data == "[DONE]" {
					break
				}

				tokensUsed += c.processSSEData(data, &result)

				eventCount++
				if heartbeatFn != nil && eventCount%10 == 0 {
					heartbeatFn(fmt.Sprintf("received %d events", eventCount))
				}
			}
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// For single-line SSE events (common case), process immediately
			// if followed by the standard empty line. But also support
			// multi-line data: fields by accumulating in dataBuf.
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(data)
			continue
		}

		// Ignore other SSE fields (event:, id:, retry:, comments).
	}

	// Handle any remaining data that wasn't terminated by an empty line.
	if dataBuf.Len() > 0 {
		data := dataBuf.String()
		if data != "[DONE]" {
			tokensUsed += c.processSSEData(data, &result)
		}
	}

	if err := scanner.Err(); err != nil {
		return result.String(), tokensUsed, fmt.Errorf("opencode: reading stream: %w", err)
	}

	// Fallback: check response header for token count if not found in stream.
	if tokensUsed == 0 {
		if v := headers.Get("X-Usage-Total-Tokens"); v != "" {
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				tokensUsed = n
			}
		}
	}

	return result.String(), tokensUsed, nil
}

// processSSEData parses a single SSE data payload, appends content to result,
// and returns the token count from a usage field if present.
func (c *Client) processSSEData(data string, result *strings.Builder) int64 {
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(data), &event); err == nil {
		if content, ok := event["content"].(string); ok {
			result.WriteString(content)
		}
		if text, ok := event["text"].(string); ok {
			result.WriteString(text)
		}
		if usage, ok := event["usage"].(map[string]interface{}); ok {
			if v, ok := usage["total_tokens"].(float64); ok {
				return int64(v)
			}
		}
	}
	return 0
}
