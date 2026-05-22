package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// HealthStatusProvider supplies health data for a named component.
type HealthStatusProvider interface {
	HealthStatus() HealthComponent
}

// HealthStatus represents the system health for the /status endpoint.
type HealthStatus struct {
	Status     string                     `json:"status"`
	Uptime     string                     `json:"uptime"`
	Version    string                     `json:"version,omitempty"`
	Components map[string]HealthComponent `json:"components"`
}

// HealthTelegramInfo is intentionally public: it contains only the bot handle and
// deep links needed by the unauthenticated dashboard login page.
type HealthTelegramInfo struct {
	Username string `json:"username"`
	URL      string `json:"url"`
	StartURL string `json:"start_url"`
	QRURL    string `json:"qr_url"`
}

// ComponentHealth represents the health of a single component.
type HealthComponent struct {
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Server provides an HTTP health endpoint.
type HealthServer struct {
	server      *http.Server
	mux         *http.ServeMux
	logger      *slog.Logger
	providers   map[string]HealthStatusProvider
	startTime   time.Time
	version     string
	botUsername string
}

// HealthServerConfig holds configuration for the health server.
type HealthServerConfig struct {
	Addr    string
	Version string
}

var telegramUsernameRE = regexp.MustCompile(`^[A-Za-z0-9_]{5,32}$`)

// NewHealthServer creates a new health HTTP server.
func NewHealthServer(cfg HealthServerConfig, logger *slog.Logger) *HealthServer {
	mux := http.NewServeMux()
	s := &HealthServer{
		server: &http.Server{
			Addr:        cfg.Addr,
			Handler:     mux,
			ReadTimeout: 5 * time.Second,
			// WriteTimeout was 10s historically — fine for /status and
			// /health, but the same server hosts /api/chat which runs the
			// full agent loop (LLM + tools) and routinely takes 30-120s
			// for fetch+summarize work. A 10s WriteTimeout silently
			// cuts the response and the client sees "Empty reply from
			// server" with no log line. Bumped to 5min to match the
			// agent default Timeout (5min). Reads still cap at 5s so a
			// slow client can't hold a connection.
			WriteTimeout: 5 * time.Minute,
		},
		mux:       mux,
		logger:    logger,
		providers: make(map[string]HealthStatusProvider),
		startTime: time.Now(),
		version:   cfg.Version,
	}

	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/telegram", s.handleTelegram)
	mux.HandleFunc("/telegram/qr.png", s.handleTelegramQR)
	// Slice 10b: the / route is owned by the SPA static handler mounted by
	// cmd/aura/main.go via Server.Mount. Leaving / unbound here means a
	// fresh server (no static assets) returns 404 on /, which is fine.

	return s
}

// Mount registers a sub-handler at the given prefix. Used by the API
// (internal/api) to attach JSON routes under /api/ alongside the existing
// /, /status, /health endpoints.
func (s *HealthServer) Mount(prefix string, handler http.Handler) {
	s.mux.Handle(prefix, handler)
}

// RegisterProvider adds a named component for health reporting.
func (s *HealthServer) RegisterProvider(name string, provider HealthStatusProvider) {
	s.providers[name] = provider
}

// SetBotUsername lets the unauthenticated login page point the user at the
// exact Telegram bot for first-run bootstrap and future /login tokens.
func (s *HealthServer) SetBotUsername(username string) {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	if !telegramUsernameRE.MatchString(username) {
		s.botUsername = ""
		return
	}
	s.botUsername = username
}

// Start starts the HTTP server in a goroutine.
func (s *HealthServer) Start() {
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
func (s *HealthServer) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// writeJSONResponse encodes v as JSON to w, logging a warning if the write fails.
func (s *HealthServer) writeJSONResponse(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil && s.logger != nil {
		s.logger.Warn("health: response write failed", "error", err)
	}
}

func (s *HealthServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	status := HealthStatus{
		Status:     "ok",
		Uptime:     time.Since(s.startTime).Round(time.Second).String(),
		Version:    s.version,
		Components: make(map[string]HealthComponent),
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
	s.writeJSONResponse(w, status)
}

func (s *HealthServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	s.writeJSONResponse(w, map[string]string{"status": "alive"})
}

func (s *HealthServer) handleTelegram(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if s.botUsername == "" {
		w.WriteHeader(http.StatusServiceUnavailable)
		s.writeJSONResponse(w, map[string]string{"error": "telegram bot username unavailable"})
		return
	}
	url := "https://t.me/" + s.botUsername
	s.writeJSONResponse(w, HealthTelegramInfo{
		Username: s.botUsername,
		URL:      url,
		StartURL: url + "?start=login",
		QRURL:    "/telegram/qr.png",
	})
}

func (s *HealthServer) handleTelegramQR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.botUsername == "" {
		http.Error(w, "telegram bot username unavailable", http.StatusServiceUnavailable)
		return
	}
	url := "https://t.me/" + s.botUsername + "?start=login"
	png, err := qrcode.Encode(url, qrcode.Medium, 256)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("telegram qr generation failed", "error", err)
		}
		http.Error(w, "could not generate telegram qr", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(png)
	}
}
