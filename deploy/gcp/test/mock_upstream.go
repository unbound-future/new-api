package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

var requestCount atomic.Uint64

type chatRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write JSON: %v", err)
	}
}

func chatHandler(streamDuration time.Duration, chunks int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":{"message":"invalid JSON"}}`, http.StatusBadRequest)
			return
		}
		if req.Model == "" {
			req.Model = "gcp-validation-model"
		}
		if !req.Stream {
			writeJSON(w, map[string]any{
				"id":      fmt.Sprintf("chatcmpl-mock-%d", time.Now().UnixNano()),
				"object":  "chat.completion",
				"created": time.Now().Unix(),
				"model":   req.Model,
				"choices": []any{map[string]any{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "mock-complete-response",
					},
					"finish_reason": "stop",
				}},
				"usage": map[string]any{
					"prompt_tokens":     7,
					"completion_tokens": 5,
					"total_tokens":      12,
					"completion_tokens_details": map[string]any{
						"reasoning_tokens": 2,
					},
				},
			})
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		id := fmt.Sprintf("chatcmpl-mock-%d", time.Now().UnixNano())
		created := time.Now().Unix()
		firstFollowupDelay := 50 * time.Millisecond
		remainingDelay := time.Duration(0)
		if chunks > 2 {
			remaining := streamDuration - firstFollowupDelay
			if remaining < 0 {
				remaining = 0
			}
			remainingDelay = remaining / time.Duration(chunks-2)
		}
		for i := 0; i < chunks; i++ {
			if i == 1 {
				time.Sleep(firstFollowupDelay)
			} else if i > 1 && remainingDelay > 0 {
				time.Sleep(remainingDelay)
			}
			content := "mock-stream-part-" + strconv.Itoa(i)
			chunk := map[string]any{
				"id": id, "object": "chat.completion.chunk", "created": created, "model": req.Model,
				"choices": []any{map[string]any{
					"index":         0,
					"delta":         map[string]any{"content": content},
					"finish_reason": nil,
				}},
			}
			encoded, _ := json.Marshal(chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
			flusher.Flush()
		}
		finalChunk := map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created, "model": req.Model,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{}, "finish_reason": "stop",
			}},
			"usage": map[string]any{
				"prompt_tokens": 7, "completion_tokens": 5, "total_tokens": 12,
				"completion_tokens_details": map[string]any{"reasoning_tokens": 2},
			},
		}
		encoded, _ := json.Marshal(finalChunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", encoded)
		flusher.Flush()
	}
}

func responsesHandler(w http.ResponseWriter, r *http.Request) {
	requestCount.Add(1)
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":{"message":"invalid JSON"}}`, http.StatusBadRequest)
		return
	}
	model, _ := body["model"].(string)
	if model == "" {
		model = "gcp-validation-model"
	}
	writeJSON(w, map[string]any{
		"id":      fmt.Sprintf("resp_mock_%d", time.Now().UnixNano()),
		"object":  "response",
		"created": time.Now().Unix(),
		"status":  "completed",
		"model":   model,
		"output": []any{
			map[string]any{
				"id": "rs_mock", "type": "reasoning", "summary": []any{},
				"encrypted_content": "mock-encrypted-reasoning-content",
			},
			map[string]any{
				"id": "msg_mock", "type": "message", "role": "assistant", "status": "completed",
				"content": []any{map[string]any{"type": "output_text", "text": "mock-response-output", "annotations": []any{}}},
			},
		},
		"usage": map[string]any{
			"input_tokens": 7, "output_tokens": 5, "total_tokens": 12,
			"output_tokens_details": map[string]any{"reasoning_tokens": 2},
		},
	})
}

func main() {
	listen := flag.String("listen", ":4001", "listen address")
	streamDuration := flag.Duration("stream-duration", 18*time.Second, "total duration of a streaming response")
	chunks := flag.Int("chunks", 6, "number of streaming chunks")
	flag.Parse()
	if *chunks < 1 {
		*chunks = 1
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", chatHandler(*streamDuration, *chunks))
	mux.HandleFunc("/chat/completions", chatHandler(*streamDuration, *chunks))
	mux.HandleFunc("/v1/responses", responsesHandler)
	mux.HandleFunc("/responses", responsesHandler)
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"object": "list", "data": []any{map[string]any{"id": "gcp-validation-model", "object": "model"}}})
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "requests": requestCount.Load()})
	})

	server := &http.Server{
		Addr:              *listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	log.Printf("mock upstream listening on %s; stream duration %s", *listen, *streamDuration)
	log.Fatal(server.ListenAndServe())
}
