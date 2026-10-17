package proxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

type UpstreamChunk struct {
	Model   string `json:"model"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// Proxy is an SSE reverse proxy that streams LLM inference chunks with backpressure handling.
type Proxy struct {
	UpstreamURL         string
	Client              *http.Client
	BackpressureTimeout time.Duration
	BufferSize          int
}

// New creates a new Proxy instance with default guardrails.
func New(upstreamURL string, client *http.Client) *Proxy {
	if client == nil {
		client = &http.Client{
			Timeout: 0, // CRITICAL for streaming!
		}
	}
	return &Proxy{
		UpstreamURL:         upstreamURL,
		Client:              client,
		BackpressureTimeout: 1500 * time.Millisecond,
		BufferSize:          2,
	}
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Prevents Nginx/proxies from buffering chunks

	rc := http.NewResponseController(w)

	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, p.UpstreamURL, nil)
	if err != nil {
		slog.Error("Failed to build upstream request", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	upstreamResp, err := p.Client.Do(upstreamReq)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(r.Context().Err(), context.Canceled) {
			slog.Warn("Downstream client disconnected before upstream responded")
		}
		slog.Error("Failed to reach upstream inference engine", "error", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer upstreamResp.Body.Close()

	if upstreamResp.StatusCode != http.StatusOK {
		slog.Error("Upstream returned non-200 code", "status", upstreamResp.StatusCode)
		http.Error(w, "Upstream Engine Error", http.StatusBadGateway)
		return
	}

	chunkChan := make(chan string, p.BufferSize) // Max 2 chunks allowed in flight
	errChan := make(chan error, 1)

	go func() {
		defer close(chunkChan)
		scanner := bufio.NewScanner(upstreamResp.Body)

		for scanner.Scan() {
			line := scanner.Text()
			if len(line) == 0 {
				continue
			}

			select {
			case <-r.Context().Done():
				slog.Info("Client disconnected; halting upstream ingestion goroutine")
				return
			case chunkChan <- line:
			// If a client takes more than 1.5 seconds to acknowledge the previous 2 chunks,
			// drop the connection to safeguard server memory.
			case <-time.After(p.BackpressureTimeout):
				slog.Warn("Backpressure threshold exceeded: downstream client too slow, dropping connection")
				errChan <- errors.New("slow_consumer_backpressure")
				return
			}
		}

		if err := scanner.Err(); err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) {
			errChan <- err
		}
	}()

	tokensDelivered := 0
	for {
		select {
		case <-r.Context().Done():
			slog.Warn("Downstream client aborted connection mid-stream",
				"tokens_delivered", tokensDelivered,
				"elapsed_ms", time.Since(start).Milliseconds(),
			)
			return

		case err := <-errChan:
			slog.Error("Streaming terminated prematurely", "error", err)
			return

		case raw, ok := <-chunkChan:
			if !ok {
				// EOF reached! Upstream finished generating text.
				fmt.Fprintf(w, "data: [DONE]\n\n")
				_ = rc.Flush()
				slog.Info("Stream completed successfully",
					"tokens_delivered", tokensDelivered,
					"duration_ms", time.Since(start).Milliseconds(),
				)
				return
			}

			var chunk UpstreamChunk
			var payload string
			if err := json.Unmarshal([]byte(raw), &chunk); err == nil {
				payload = chunk.Message.Content
			} else {
				payload = raw
			}

			if _, err := fmt.Fprintf(w, "data: %s\n\n", payload); err != nil {
				slog.Warn("Socket write error", "error", err)
				return
			}

			_ = rc.SetWriteDeadline(time.Now().Add(1 * time.Second))
			if err := rc.Flush(); err != nil {
				slog.Warn("Flush failed, downstream socket likely dropped", "error", err)
				return
			}
			tokensDelivered++
		}
	}
}
