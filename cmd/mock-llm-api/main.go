// mock-llm-api simulates an OpenAI-compatible API with per-key rate limiting.
// It mimics the behavior of providers like Gemini that enforce strict RPM/TPM
// limits per API key, returning 429 when exceeded — allowing end-to-end testing
// of LiteLLM key rotation and fallback strategies without real API keys.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
)

// rateLimiter tracks per-key request counts within a sliding window.
type rateLimiter struct {
	mu       sync.Mutex
	windows  map[string]*window
	rpmLimit int
	tpmLimit int
}

type window struct {
	requests []time.Time
	tokens   []int
}

func newRateLimiter(rpm, tpm int) *rateLimiter {
	return &rateLimiter{
		windows:  make(map[string]*window),
		rpmLimit: rpm,
		tpmLimit: tpm,
	}
}

// check returns (allowed, retryAfterMs). If not allowed, the caller should return 429.
func (rl *rateLimiter) check(apiKey string, tokensUsed int) (bool, int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	w, ok := rl.windows[apiKey]
	if !ok {
		w = &window{}
		rl.windows[apiKey] = w
	}

	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)

	// Prune old entries.
	w.requests = pruneTime(w.requests, cutoff)
	w.tokens = pruneTokens(w.requests, w.tokens, cutoff)

	// Check RPM.
	if len(w.requests) >= rl.rpmLimit {
		oldest := w.requests[0]
		retryAfter := time.Until(oldest.Add(time.Minute))
		return false, int(retryAfter.Milliseconds())
	}

	// Check TPM.
	totalTokens := 0
	for _, t := range w.tokens {
		totalTokens += t
	}
	if rl.tpmLimit > 0 && totalTokens+tokensUsed > rl.tpmLimit {
		return false, 30_000 // suggest 30s retry
	}

	// Record this request.
	w.requests = append(w.requests, now)
	w.tokens = append(w.tokens, tokensUsed)

	return true, 0
}

func pruneTime(times []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[i] = t
			i++
		}
	}
	return times[:i]
}

func pruneTokens(times []time.Time, tokens []int, cutoff time.Time) []int {
	// After pruneTime, times and tokens might be out of sync if pruneTime
	// was called first. Re-align by keeping same length.
	if len(tokens) > len(times) {
		diff := len(tokens) - len(times)
		tokens = tokens[diff:]
	}
	return tokens
}

// stats returns current usage for observability.
func (rl *rateLimiter) stats(apiKey string) (rpm int, tpm int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	w, ok := rl.windows[apiKey]
	if !ok {
		return 0, 0
	}

	cutoff := time.Now().Add(-1 * time.Minute)
	w.requests = pruneTime(w.requests, cutoff)
	w.tokens = pruneTokens(w.requests, w.tokens, cutoff)

	total := 0
	for _, t := range w.tokens {
		total += t
	}
	return len(w.requests), total
}

func main() {
	addr := flag.String("addr", ":4099", "listen address")
	rpm := flag.Int("rpm", 5, "requests per minute per key")
	tpm := flag.Int("tpm", 10000, "tokens per minute per key (0=unlimited)")
	latencyMs := flag.Int("latency", 200, "simulated response latency in ms")
	failRate := flag.Float64("fail-rate", 0.0, "random failure rate (0.0-1.0)")
	flag.Parse()

	limiter := newRateLimiter(*rpm, *tpm)
	simulatedLatency := time.Duration(*latencyMs) * time.Millisecond

	mux := http.NewServeMux()

	// OpenAI-compatible chat completions endpoint.
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		apiKey := extractAPIKey(r)
		if apiKey == "" {
			writeError(w, http.StatusUnauthorized, "missing api key")
			return
		}

		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request")
			return
		}

		// Estimate tokens from prompt length.
		promptTokens := estimateTokens(req)

		// Rate limit check.
		allowed, retryAfterMs := limiter.check(apiKey, promptTokens+200)
		if !allowed {
			rpm, tpm := limiter.stats(apiKey)
			slog.Warn("rate limited",
				"key", maskKey(apiKey),
				"rpm", rpm,
				"tpm", tpm,
				"retryAfterMs", retryAfterMs,
			)
			w.Header().Set("Retry-After", fmt.Sprintf("%d", (retryAfterMs/1000)+1))
			w.Header().Set("X-RateLimit-Remaining-Requests", "0")
			writeError(w, http.StatusTooManyRequests,
				fmt.Sprintf("Rate limit exceeded. Retry after %dms", retryAfterMs))
			return
		}

		// Random failure simulation.
		if *failRate > 0 && rand.Float64() < *failRate {
			writeError(w, http.StatusInternalServerError, "simulated server error")
			return
		}

		// Simulate processing latency.
		time.Sleep(simulatedLatency)

		// Generate response.
		content := generateResponse(req)
		completionTokens := len(content) / 4

		resp := chatResponse{
			ID:      "chatcmpl-" + uuid.NewString()[:8],
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []choice{
				{
					Index: 0,
					Message: message{
						Role:    "assistant",
						Content: content,
					},
					FinishReason: "stop",
				},
			},
			Usage: usage{
				PromptTokens:     promptTokens,
				CompletionTokens: completionTokens,
				TotalTokens:      promptTokens + completionTokens,
			},
		}

		rpm, tpm := limiter.stats(apiKey)
		slog.Info("request served",
			"key", maskKey(apiKey),
			"model", req.Model,
			"promptTokens", promptTokens,
			"rpm", rpm,
			"tpm", tpm,
		)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// Models list endpoint (LiteLLM health check probes this).
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{"id": "gemini-2.5-pro", "object": "model"},
				{"id": "gemini-2.5-flash", "object": "model"},
				{"id": "gpt-4o", "object": "model"},
			},
		})
	})

	// Rate limit status endpoint (for debugging).
	mux.HandleFunc("GET /debug/rate-limits", func(w http.ResponseWriter, r *http.Request) {
		limiter.mu.Lock()
		defer limiter.mu.Unlock()

		stats := make(map[string]interface{})
		for key, win := range limiter.windows {
			cutoff := time.Now().Add(-1 * time.Minute)
			win.requests = pruneTime(win.requests, cutoff)
			total := 0
			for _, t := range win.tokens {
				total += t
			}
			stats[maskKey(key)] = map[string]interface{}{
				"rpm_used":  len(win.requests),
				"rpm_limit": limiter.rpmLimit,
				"tpm_used":  total,
				"tpm_limit": limiter.tpmLimit,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"limits": stats,
			"config": map[string]interface{}{
				"rpm": limiter.rpmLimit,
				"tpm": limiter.tpmLimit,
			},
		})
	})

	// Health check.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	srv := &http.Server{Addr: *addr, Handler: mux}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("mock-llm-api listening",
			"addr", *addr,
			"rpm", *rpm,
			"tpm", *tpm,
			"latencyMs", *latencyMs,
			"failRate", *failRate,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-sigCh
	slog.Info("shutting down mock-llm-api")
	srv.Close()
}

// --- types ---

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type choice struct {
	Index        int     `json:"index"`
	Message      message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// --- helpers ---

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": msg,
			"type":    "error",
			"code":    status,
		},
	})
}

func extractAPIKey(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return r.Header.Get("x-api-key")
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}

func estimateTokens(req chatRequest) int {
	total := 0
	for _, m := range req.Messages {
		total += len(m.Content) / 4 // rough estimate: 4 chars per token
	}
	if total < 10 {
		total = 10
	}
	return total
}

func generateResponse(req chatRequest) string {
	// Extract the last user message.
	var lastContent string
	for _, m := range req.Messages {
		if m.Role == "user" {
			lastContent = m.Content
		}
	}

	// If it starts with OUTPUT:, return the remainder (same as mock-opencode).
	if strings.HasPrefix(lastContent, "OUTPUT:") {
		return strings.TrimPrefix(lastContent, "OUTPUT:")
	}

	return fmt.Sprintf("This is a mock response from model %s for prompt: %s",
		req.Model, truncate(lastContent, 100))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
