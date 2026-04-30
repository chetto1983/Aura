package health

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/skip2/go-qrcode"
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
		mux:       mux,
		logger:    logger,
		providers: make(map[string]StatusProvider),
		startTime: time.Now(),
		version:   cfg.Version,
	}

	mux.HandleFunc("/", s.handleLanding)
	mux.HandleFunc("/status", s.handleStatus)
	mux.HandleFunc("/health", s.handleHealth)

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

// SetBotUsername sets the Telegram bot username for the invite page.
func (s *Server) SetBotUsername(username string) {
	s.botUsername = username
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

func (s *Server) handleLanding(w http.ResponseWriter, r *http.Request) {
	if s.botUsername == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"error": "bot username not available"})
		return
	}

	link := fmt.Sprintf("https://t.me/%s?start=invite", s.botUsername)

	png, err := qrcode.Encode(link, qrcode.Medium, 256)
	if err != nil {
		s.logger.Error("failed to generate QR code", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "qr generation failed"})
		return
	}

	qrBase64 := base64.StdEncoding.EncodeToString(png)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, landingPage, qrBase64, link, s.version)
}

const landingPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Aura — Personal AI Agent</title>
<style>
:root { --primary: #6c5ce7; --primary-hover: #5a4bd1; --bg: #0a0a0f; --card: #12121e; --card-border: #1e1e30; --text: #e0e0e0; --muted: #666; --success: #00b894; --warning: #fdcb6e; --danger: #e17055; }
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: var(--bg); color: var(--text); line-height: 1.6; }
.container { max-width: 960px; margin: 0 auto; padding: 24px 16px; }
header { text-align: center; padding: 48px 0 32px; }
header h1 { font-size: 48px; font-weight: 700; background: linear-gradient(135deg, var(--primary), #a29bfe); -webkit-background-clip: text; -webkit-text-fill-color: transparent; }
header p { color: var(--muted); font-size: 18px; margin-top: 8px; }
.grid { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; margin-top: 32px; }
@media (max-width: 640px) { .grid { grid-template-columns: 1fr; } }
.card { background: var(--card); border: 1px solid var(--card-border); border-radius: 16px; padding: 32px; }
.card h2 { font-size: 20px; margin-bottom: 16px; display: flex; align-items: center; gap: 10px; }
.card h2 .icon { font-size: 24px; }
.qr-wrap { text-align: center; margin: 16px 0; }
.qr-wrap img { background: #fff; border-radius: 12px; padding: 12px; width: 200px; height: 200px; }
.btn { display: inline-block; background: var(--primary); color: #fff; text-decoration: none; padding: 12px 28px; border-radius: 8px; font-size: 15px; font-weight: 600; transition: background 0.2s; cursor: pointer; border: none; }
.btn:hover { background: var(--primary-hover); }
.btn-outline { background: transparent; border: 1px solid var(--primary); color: var(--primary); }
.btn-outline:hover { background: var(--primary); color: #fff; }
.skill-list { list-style: none; margin-top: 12px; }
.skill-list li { padding: 8px 0; border-bottom: 1px solid var(--card-border); display: flex; justify-content: space-between; align-items: center; }
.skill-list li:last-child { border-bottom: none; }
.badge { font-size: 11px; padding: 2px 8px; border-radius: 4px; font-weight: 600; text-transform: uppercase; }
.badge-local { background: #00b89422; color: var(--success); }
.badge-verified { background: #6c5ce722; color: var(--primary); }
.badge-untrusted { background: #e1705522; color: var(--danger); }
.mcp-endpoint { background: #0d0d15; border-radius: 8px; padding: 12px; margin-top: 12px; font-family: monospace; font-size: 13px; color: #a29bfe; word-break: break-all; }
.section { margin-top: 24px; }
.section h2 { font-size: 20px; margin-bottom: 12px; }
.status-dot { display: inline-block; width: 8px; height: 8px; border-radius: 50%%; margin-right: 6px; }
.status-ok { background: var(--success); }
.status-degraded { background: var(--warning); }
.status-error { background: var(--danger); }
footer { text-align: center; padding: 48px 0 24px; color: var(--muted); font-size: 12px; }
</style>
</head>
<body>
<div class="container">
<header>
<h1>Aura</h1>
<p>Your Personal AI Agent with Compounding Memory</p>
</header>

<div class="grid">
<div class="card">
<h2><span class="icon">&#9993;</span> Connect via Telegram</h2>
<p>Scan the QR code or click the button to start chatting with Aura on Telegram. You'll be automatically granted access.</p>
<div class="qr-wrap"><img src="data:image/png;base64,%s" alt="Telegram QR Code"></div>
<div style="text-align:center">
<a class="btn" href="%s" target="_blank">Open in Telegram</a>
</div>
</div>

<div class="card">
<h2><span class="icon">&#9889;</span> Skills</h2>
<p style="color:var(--muted);font-size:14px;">Aura can execute skills with different trust levels:</p>
<ul class="skill-list">
<li><span>Shell commands</span><span class="badge badge-local">local</span></li>
<li><span>Web search</span><span class="badge badge-verified">verified</span></li>
<li><span>Custom scripts</span><span class="badge badge-untrusted">untrusted</span></li>
</ul>
<div style="margin-top:16px">
<a class="btn btn-outline" href="/status" target="_blank">View System Status</a>
</div>
</div>
</div>

<div class="section card" style="margin-top:24px">
<h2><span class="icon">&#128279;</span> MCP Endpoint</h2>
<p style="color:var(--muted);font-size:14px;">Connect external tools via the Model Context Protocol. Use this endpoint to integrate Aura with your AI workflows:</p>
<div class="mcp-endpoint">/v1/mcp/sse</div>
<p style="color:var(--muted);font-size:12px;margin-top:8px;">SSE transport &middot; JSON-RPC 2.0 &middot; Compatible with Claude Desktop, Cursor, and other MCP clients</p>
</div>

<footer>
Aura v%s &middot; <span class="status-dot status-ok"></span>All systems operational
</footer>
</div>
</body>
</html>`