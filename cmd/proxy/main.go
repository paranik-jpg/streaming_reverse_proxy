package main

import (
	"log/slog"
	"net/http"
	"os"

	"streaming_reverse_proxy/pkg/proxy"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	upstreamURL := "http://localhost:11434/api/chat"
	if envURL := os.Getenv("UPSTREAM_URL"); envURL != "" {
		upstreamURL = envURL
	}

	listenAddr := ":8080"
	if envAddr := os.Getenv("LISTEN_ADDR"); envAddr != "" {
		listenAddr = envAddr
	}

	p := proxy.New(upstreamURL, nil)
	http.Handle("/v1/chat/stream", p)

	slog.Info("Aegis-AI Streaming Reverse Proxy listening", "addr", listenAddr, "upstream", upstreamURL)
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		slog.Error("Server failed", "error", err)
	}
}

