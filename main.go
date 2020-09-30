package main

import (
	"context"
	"fmt"
	"github.com/pires/go-proxyproto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

var (
	callSummary = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "payments_request_duration_ms",
			Help:    "Payments latency distributions.",
			Buckets: []float64{0.001, 0.01, 0.1, 1, 5, 10, 50, 500, 1000, 5000},
		},
		[]string{"response_code"},
	)
)

func init() {
	prometheus.MustRegister(callSummary)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", pingHandler)
	mux.HandleFunc("/healthz", healthHandler)

	remoteEndpoint := os.Getenv("REMOTE_ENDPOINT")

	go newPingClient(remoteEndpoint).Start()

	addr := fmt.Sprintf(":%s", os.Getenv("PORT"))
	list, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("could not listen to %s: %v\n", addr, err)
	}
	proxyListener := &proxyproto.Listener{Listener: list}
	defer proxyListener.Close()

	go createPrometheusEndpoint()

	log.Fatal(http.Serve(proxyListener, mux))
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

type pingClient struct {
	client   *http.Client
	endpoint string
}

func newPingClient(remoteEndpoint string) *pingClient {
	client := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives: true,
		},
	}
	return &pingClient{
		client:   client,
		endpoint: remoteEndpoint,
	}
}

func (p *pingClient) Start() {
	for {
		select {
		case <-time.After(time.Second):
			start := time.Now()
			err := p.ping()
			duration := time.Since(start)
			callSummary.WithLabelValues("-").Observe(float64(duration.Milliseconds()))
			if err != nil {
				fmt.Printf("Received err: %v, after: %v\n", err, duration)
				continue
			}
			fmt.Printf("Received ping response after: %v\n", duration)
		}
	}
}

func (p *pingClient) ping() error {
	timeout, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(timeout, http.MethodGet, p.endpoint, nil)
	if err != nil {
		return err
	}
	res, err := p.client.Do(req)
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

func createPrometheusEndpoint() {
	mux := http.NewServeMux()

	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "OK")
	})

	srv := &http.Server{
		Handler:      mux,
		Addr:         ":8001",
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
	}
	srv.ListenAndServe()
}
