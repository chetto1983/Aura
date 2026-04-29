package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// StatusProvider supplies health data for a named component.
type StatusProvider interface {
	HealthStatus() ComponentHealth
}

// HealthStatus represents the system health for the /status endpoint.
type HealthStatus struct {
	Status     string                       `json:"status"`
	Uptime     string                       `json:"uptime"`
	Version    string                       `json:"version,omitempty"`
	Components map[string]ComponentHealth    `json:"components"`
}

// ComponentHealth represents the health of a single component.
type ComponentHealth struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Server provides an HTTP health endpoint.
type Server struct {
	server    *http.Server
	logger    *slog.Logger
	providers map[string]StatusProvider
	startTime time.Time
	version   string
}

// ServerConfig holds configuration for the health server.
type ServerConfig struct {
	Addr   string
	Version string
}

// NewServer creates a new health HTTP server.
func NewServer(cfg ServerConfig, logger *slog.Logger) *Server {
	mux := http.NewServeMux()
	s := &Server{
		server: &http.Server{
			Addr:         cfg.Addr,
			Handler:      mux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		logger:    logger,
		providers: make(map[string]StatusProvider),
		startTime: time.Now(),
		version:   cfg.Version,
	}

	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/health", s.handleHealth)

	return s
}

// RegisterProvider adds a named component for health reporting.
func (s *Server) RegisterProvider(name string, provider StatusProvider) {
	s.providers[name] = provider
}

// Start starts the HTTP server in a goroutine.
func (s *Server) Start() {
	ln, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		s.logger.Error("failed to start health server", "addr", s.server.Addr, "error", err)
		return
	}
	s.logger.Info("health server listening", "addr", s.server.Addr)
	go func() {
		if err := s.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.logger.Error("health server error", "error", err)
		}
	}()
}

// Shutdown gracefully stops the HTTP server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := HealthStatus{
		Status:     "ok",
		Uptime:     time.Since(s.startTime).Round(time.Second).String(),
		Version:    s.version,
		Components: make(map[string]ComponentHealth),
	}

	allHealthy := true
	for name, provider := range s.providers {
		ch := provider.HealthStatus()
		status.Components[name] = ch
		if ch.Status != "ok" {
			allHealthy = false
		}
	}

	if !allHealthy {
		status.Status = "degraded"
	}

	w.Header().Set("Content-Type", "application/json")
	if !allHealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "alive"})
}