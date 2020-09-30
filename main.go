package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", pingHandler)
	mux.HandleFunc("/healthz", healthHandler)

	remoteEndpoint := os.Getenv("REMOTE_ENDPOINT")

	go pingRemote(remoteEndpoint)

	serverPort := os.Getenv("PORT")

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%s", serverPort), mux))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func pingRemote(endpoint string) {
	for {
		select {
		case <-time.After(time.Second):
			start := time.Now()
			err := ping(endpoint)
			end := time.Now()
			duration := end.Sub(start)
			if err != nil {
				fmt.Printf("Received err: %v, after: %v\n", err, duration)
				continue
			}
			fmt.Printf("Received ping response after: %v\n", duration)
		}
	}
}

func ping(endpoint string) error {
	timeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(timeout, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	client := &http.Client{
		Transport: &http.Transport{},
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("expected status OK, got %v", res.Status)
	}
	return nil
}

func pingHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("received ping request from: %v\n", r.RemoteAddr)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
