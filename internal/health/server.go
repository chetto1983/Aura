package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// StatusProvider supplies health data for a named component.
type StatusProvider interface {
	HealthStatus() ComponentHealth
}

// HealthStatus represents the system health for the /status endpoint.
type HealthStatus struct {
	Status     string                     `json:"status"`
	Uptime     string                     `json:"uptime"`
	Version    string                     `json:"version,omitempty"`
	Components map[string]ComponentHealth `json:"components"`
}

// TelegramInfo is intentionally public: it contains only the bot handle and
// deep links needed by the unauthenticated dashboard login page.
type TelegramInfo struct {
	Username string `json:"username"`
	URL      string `json:"url"`
	StartURL string `json:"start_url"`
}

// ComponentHealth represents the health of a single component.
type ComponentHealth struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Server provides an HTTP health endpoint.
type Server struct {
	server      *http.Server
	mux         *http.ServeMux
	logger      *slog.Logger
	providers   map[string]StatusProvider
	startTime   time.Time
	version     string
	botUsername string
}

// ServerConfig holds configuration for the health server.
type ServerConfig struct {
	Addr    string
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
		mux:       mux,
		logger:    logger,
		providers: make(map[string]StatusProvider),
		startTime: time.Now(),
		version:   cfg.Version,
	}

	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/telegram", s.handleTelegram)
	// Slice 10b: the / route is owned by the SPA static handler mounted by
	// cmd/aura/main.go via Server.Mount. Leaving / unbound here means a
	// fresh server (no static assets) returns 404 on /, which is fine.

	return s
}

// Mount registers a sub-handler at the given prefix. Used by the API
// (internal/api) to attach JSON routes under /api/ alongside the existing
// /, /status, /health endpoints.
func (s *Server) Mount(prefix string, handler http.Handler) {
	s.mux.Handle(prefix, handler)
}

// RegisterProvider adds a named component for health reporting.
func (s *Server) RegisterProvider(name string, provider StatusProvider) {
	s.providers[name] = provider
}

// SetBotUsername lets the unauthenticated login page point the user at the
// exact Telegram bot for first-run bootstrap and future /login tokens.
func (s *Server) SetBotUsername(username string) {
	s.botUsername = strings.TrimPrefix(strings.TrimSpace(username), "@")
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

func (s *Server) handleTelegram(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.botUsername == "" {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "telegram bot username unavailable"})
		return
	}
	url := "https://t.me/" + s.botUsername
	json.NewEncoder(w).Encode(TelegramInfo{
		Username: s.botUsername,
		URL:      url,
		StartURL: url + "?start=login",
	})
}
