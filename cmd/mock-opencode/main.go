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
	"syscall"
	"time"

	"github.com/google/uuid"
)

func main() {
	addr := flag.String("addr", ":4096", "listen address")
	delay := flag.Duration("delay", 0, "base response delay to simulate LLM thinking (e.g. 5s)")
	jitter := flag.Duration("jitter", 0, "random jitter added to delay (e.g. 3s)")
	chunkDelay := flag.Duration("chunk-delay", 0, "delay between SSE chunks to simulate token streaming (e.g. 50ms)")
	flag.Parse()

	slog.Info("mock-opencode config", "delay", *delay, "jitter", *jitter, "chunkDelay", *chunkDelay)

	mux := http.NewServeMux()

	// POST /sessions - create a new session.
	mux.HandleFunc("POST /sessions", func(w http.ResponseWriter, r *http.Request) {
		sess := map[string]interface{}{
			"id":        uuid.NewString(),
			"createdAt": time.Now().UTC().Format(time.RFC3339),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(sess)
		slog.Info("session created", "id", sess["id"])
	})

	// POST /sessions/{id}/messages - send a message, respond via SSE.
	mux.HandleFunc("POST /sessions/{id}/messages", func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.PathValue("id")

		var msg struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		slog.Info("message received", "sessionID", sessionID, "contentLen", len(msg.Content))

		// If content starts with "OUTPUT:", stream the remainder as SSE.
		// Otherwise, return a placeholder result.
		var output string
		if strings.HasPrefix(msg.Content, "OUTPUT:") {
			output = strings.TrimPrefix(msg.Content, "OUTPUT:")
		} else {
			output = fmt.Sprintf("placeholder result for prompt: %s", msg.Content)
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			// Fallback to plain JSON if streaming not supported.
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"content": output})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		// Simulate LLM thinking time.
		if *delay > 0 {
			wait := *delay
			if *jitter > 0 {
				wait += time.Duration(rand.Int64N(int64(*jitter)))
			}
			slog.Info("simulating LLM delay", "sessionID", sessionID, "wait", wait)
			select {
			case <-r.Context().Done():
				return
			case <-time.After(wait):
			}
		}

		// Stream the output in chunks.
		chunkSize := 100
		for i := 0; i < len(output); i += chunkSize {
			end := i + chunkSize
			if end > len(output) {
				end = len(output)
			}
			chunk := output[i:end]
			data, _ := json.Marshal(map[string]string{"content": chunk})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if *chunkDelay > 0 {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(*chunkDelay):
				}
			}
		}

		// If output is empty, send at least one event.
		if len(output) == 0 {
			data, _ := json.Marshal(map[string]string{"content": ""})
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}

		// Emit usage chunk before [DONE].
		usageData, _ := json.Marshal(map[string]interface{}{
			"content": "",
			"usage": map[string]interface{}{
				"prompt_tokens":     len(output) / 12,
				"completion_tokens": len(output) / 6,
				"total_tokens":      len(output) / 4,
			},
		})
		fmt.Fprintf(w, "data: %s\n\n", usageData)
		flusher.Flush()

		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	// Health check.
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	srv := &http.Server{
		Addr:    *addr,
		Handler: mux,
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("mock-opencode listening", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-sigCh
	slog.Info("shutting down mock-opencode")
	srv.Close()
}
