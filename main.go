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
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var (
	callSummary = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "payments_request_duration_ms",
			Help:    "Payments latency distributions.",
			Buckets: []float64{0.1, 1, 5, 10, 25, 50, 100, 200, 500, 1000, 5000},
		},
		[]string{"availability_zone", "endpoint"},
	)
	pingRequests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "payments_ping_request_count",
		},
		[]string{"remote_ip"},
	)
)

var availabilityZone string

func init() {
	prometheus.MustRegister(callSummary)
	prometheus.MustRegister(pingRequests)
	availabilityZone = os.Getenv("AVAILABILITY_ZONE")
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", pingHandler)
	mux.HandleFunc("/healthz", healthHandler)
	mux.Handle("/metrics", promhttp.Handler())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	remoteAddrs := os.Getenv("REMOTE_ADDR")
	if remoteAddrs != "" {
		addrs := strings.Split(remoteAddrs, ",")
		for _, addr := range addrs {
			startPinging(ctx, addr)
		}
	}

	addr := fmt.Sprintf(":%s", os.Getenv("PORT"))
	list, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("could not listen to %s: %v\n", addr, err)
	}
	proxyListener := &proxyproto.Listener{Listener: list}
	defer proxyListener.Close()

	go createPrometheusEndpoint(ctx)

	srv := &http.Server{Handler: mux}

	go func() {
		<-ctx.Done()
		timeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(timeout)
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Printf("Stopping")
		cancel()
	}()

	log.Fatal(srv.Serve(proxyListener))
}

func startPinging(ctx context.Context, remoteAddr string) {
	fmt.Printf("Resolving %v\n", remoteAddr)
	ips, err := net.LookupIP(remoteAddr)
	if err != nil {
		log.Fatalf("could not look up ip addresses: %v\n", err)
	}

	for _, ip := range ips {
		if ip.To4() == nil {
			continue
		}
		remoteEndpoint := fmt.Sprintf("http://%s:8000/ping", ip.To4())
		log.Printf("Starting client for endpoint: %v\n", remoteEndpoint)
		go newPingClient(remoteEndpoint).Start(ctx)
	}
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
			DisableKeepAlives: false,
			IdleConnTimeout:   time.Minute,
		},
	}
	return &pingClient{
		client:   client,
		endpoint: remoteEndpoint,
	}
}

func (p *pingClient) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
			start := time.Now()
			err := p.ping()
			duration := time.Since(start)
			callSummary.WithLabelValues(availabilityZone, p.endpoint).Observe(float64(duration.Milliseconds()))
			if err != nil {
				fmt.Printf("Received err: %v, after: %v\n", err, duration)
				continue
			}
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
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
	remoteAddr := strings.Split(r.RemoteAddr, ":")
	pingRequests.WithLabelValues(remoteAddr[0]).Inc()
}

func createPrometheusEndpoint(ctx context.Context) {
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
	go func() {
		select {
		case <-ctx.Done():
			timeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			srv.Shutdown(timeout)
		}
	}()
	srv.ListenAndServe()
}
