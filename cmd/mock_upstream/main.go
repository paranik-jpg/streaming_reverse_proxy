package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Chunk struct {
	Model   string  `json:"model"`
	Message Message `json:"message"`
	Done    bool    `json:"done"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func main() {
	http.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		rc := http.NewResponseController(w)

		filler := strings.Repeat("X", 8192)

		for i := 1; ; i++ {
			select {
			// Because r.Context() was passed into http.NewRequestWithContext,
			// closing the client connection stops both the proxy and upstream LLM immediately.
			case <-r.Context().Done():
				fmt.Println("[Upstream] Downstream cancelled! Reclaiming compute.")
				return
			case <-time.After(20 * time.Millisecond):
				chunk := Chunk{
					Model: "mock-engine",
					Message: Message{
						Role:    "assistant",
						Content: fmt.Sprintf("[%04d-CHUNK:%s] ", i, filler),
					},
					Done: false,
				}
				data, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "%s\n", data)
				rc.Flush()
			}
		}
	})

	fmt.Println("Mock inference engine running on :11434")
	http.ListenAndServe(":11434", nil)
}
