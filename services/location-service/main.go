package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		checkHealth(port)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"location-service"}`))
	})

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func checkHealth(port string) {
	client := http.Client{Timeout: 2 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + port + "/health")
	if err != nil {
		os.Exit(1)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		os.Exit(1)
	}
}
