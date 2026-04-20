// Package observability – metrics HTTP server.
//
// Starts a lightweight HTTP server on a configurable port (default :9090)
// exposing two endpoints:
//
//   GET /metrics   — Prometheus text format (for Grafana scraping)
//   GET /health    — JSON health check (for Docker/K8s liveness probes)
//
// The server is intentionally separate from the main API server (port 3000)
// so that Prometheus can be granted network access without exposing the
// full trading API externally.
package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ─────────────────────────────────────────────────────────────────────────────
// Server
// ─────────────────────────────────────────────────────────────────────────────

// ServerConfig controls the observability server.
type ServerConfig struct {
	// Port is the TCP port to listen on. Default: 9090.
	Port int
	// Path is the metrics endpoint path. Default: /metrics.
	Path string
	// EnableHealthCheck adds a /health endpoint. Default: true.
	EnableHealthCheck bool
}

// DefaultServerConfig returns standard Prometheus defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Port:              9090,
		Path:              "/metrics",
		EnableHealthCheck: true,
	}
}

// Server is the observability HTTP server.
type Server struct {
	cfg    ServerConfig
	srv    *http.Server
	startTime time.Time
}

// NewServer creates a Server with cfg.
func NewServer(cfg ServerConfig) *Server {
	if cfg.Port == 0 {
		cfg.Port = 9090
	}
	if cfg.Path == "" {
		cfg.Path = "/metrics"
	}
	return &Server{cfg: cfg, startTime: time.Now()}
}

// Start begins serving metrics. It blocks until ctx is cancelled or the
// server fails. Call it in a goroutine.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Prometheus metrics endpoint
	mux.Handle(s.cfg.Path, promhttp.Handler())

	// Health check
	if s.cfg.EnableHealthCheck {
		mux.HandleFunc("/health", s.healthHandler)
	}

	// Build info endpoint (useful for Grafana annotations)
	mux.HandleFunc("/info", s.infoHandler)

	s.srv = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.cfg.Port),
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Printf("observability: metrics server listening on :%d%s", s.cfg.Port, s.cfg.Path)

	errCh := make(chan error, 1)
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(shutCtx)
	case err := <-errCh:
		return fmt.Errorf("observability server: %w", err)
	}
}

type healthResponse struct {
	Status   string `json:"status"`
	Uptime   string `json:"uptime"`
	Version  string `json:"version"`
}

func (s *Server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(healthResponse{
		Status:  "ok",
		Uptime:  time.Since(s.startTime).Round(time.Second).String(),
		Version: "1.3",
	})
}

type infoResponse struct {
	App       string `json:"app"`
	Version   string `json:"version"`
	StartTime string `json:"start_time"`
	BasedOn   string `json:"based_on"`
}

func (s *Server) infoHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(infoResponse{
		App:       "EvolvX",
		Version:   "1.3",
		StartTime: s.startTime.Format(time.RFC3339),
		BasedOn:   "NOFX by NoFxAiOS (github.com/NoFxAiOS/nofx)",
	})
}
