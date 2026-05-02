package setup

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/aura/aura/internal/settings"
)

//go:embed page.html
var pageHTML string

// Config controls how the wizard runs.
type Config struct {
	// Listen is the bind address. Forced to 127.0.0.1 host even if the
	// caller passes a LAN address — the wizard has no auth.
	Listen string

	// DotEnvPath is where the wizard writes TELEGRAM_TOKEN. Defaults to
	// "./.env" when blank.
	DotEnvPath string

	// SettingsStore receives every non-bootstrap key (LLM_*, embeddings,
	// OCR, etc.). Required.
	SettingsStore *settings.Store

	// Logger receives wizard activity. Required.
	Logger *slog.Logger
}

// Run starts the wizard's HTTP server, blocks until the user submits a
// valid form, then returns the saved Telegram token. Callers should
// re-load .env after Run returns. Returns context.Canceled-style error
// only on shutdown failure.
func Run(cfg Config) (telegramToken string, err error) {
	if cfg.SettingsStore == nil {
		return "", errors.New("setup: SettingsStore required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.DotEnvPath == "" {
		cfg.DotEnvPath = ".env"
	}

	listen := loopbackOnly(cfg.Listen)
	tpl, err := template.New("setup").Parse(pageHTML)
	if err != nil {
		return "", fmt.Errorf("setup: parse template: %w", err)
	}

	doneCh := make(chan string, 1) // sends the saved telegram token
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/setup" {
			http.NotFound(w, r)
			return
		}
		ollamaUp := detectOllama(r.Context(), "http://localhost:11434")
		presetsJSON, _ := json.Marshal(LLMPresets)
		data := map[string]any{
			"Presets":        LLMPresets,
			"PresetsJSON":    template.JS(presetsJSON), //nolint:gosec // local-only loopback page; presets are static
			"OllamaDetected": ollamaUp,
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := tpl.Execute(w, data); err != nil {
			cfg.Logger.Warn("setup: template render", "error", err)
		}
	})

	mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
			return
		}
		var req struct {
			Preset    string `json:"preset"`
			BaseURL   string `json:"base_url"`
			APIKey    string `json:"api_key"`
			ProbePath string `json:"probe_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON"})
			return
		}
		probePath := req.ProbePath
		if probePath == "" {
			if p, ok := PresetByID(req.Preset); ok && p.ProbePath != "" {
				probePath = p.ProbePath
			} else {
				probePath = "/models"
			}
		}
		result := probeProvider(r.Context(), req.BaseURL, req.APIKey, probePath)
		writeJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
			return
		}
		var req struct {
			TelegramToken    string `json:"telegram_token"`
			LLMPreset        string `json:"llm_preset"`
			LLMBaseURL       string `json:"llm_base_url"`
			LLMModel         string `json:"llm_model"`
			LLMAPIKey        string `json:"llm_api_key"`
			EmbeddingAPIKey  string `json:"embedding_api_key"`
			MistralAPIKey    string `json:"mistral_api_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON"})
			return
		}
		token := strings.TrimSpace(req.TelegramToken)
		if token == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Telegram token is required"})
			return
		}

		// 1. .env: persist the bootstrap secret so a restart finds it.
		if err := writeDotEnvKey(cfg.DotEnvPath, "TELEGRAM_TOKEN", token); err != nil {
			cfg.Logger.Error("setup: write .env", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "could not write .env: " + err.Error()})
			return
		}

		// 2. settings DB: everything else. Each key is overridable per
		// internal/settings/applier.go.
		ctx := r.Context()
		writes := []struct{ key, val string }{
			{settings.KeyLLMBaseURL, strings.TrimSpace(req.LLMBaseURL)},
			{settings.KeyLLMModel, strings.TrimSpace(req.LLMModel)},
			{settings.KeyLLMAPIKey, strings.TrimSpace(req.LLMAPIKey)},
			{settings.KeyEmbeddingAPIKey, strings.TrimSpace(req.EmbeddingAPIKey)},
			{settings.KeyMistralAPIKey, strings.TrimSpace(req.MistralAPIKey)},
		}
		// Ollama preset implies OLLAMA_BASE_URL is the de-facto local
		// endpoint; surface it as the failover provider so a paid
		// provider outage falls back automatically.
		if req.LLMPreset == "ollama" && req.LLMBaseURL != "" {
			// Strip /v1 suffix for OLLAMA_BASE_URL — the failover client
			// expects the bare host.
			ollamaHost := strings.TrimSuffix(strings.TrimSpace(req.LLMBaseURL), "/v1")
			ollamaHost = strings.TrimSuffix(ollamaHost, "/")
			writes = append(writes,
				struct{ key, val string }{settings.KeyOllamaBaseURL, ollamaHost},
				struct{ key, val string }{settings.KeyOllamaModel, strings.TrimSpace(req.LLMModel)},
			)
		}
		for _, w := range writes {
			if w.val == "" {
				_ = cfg.SettingsStore.Delete(ctx, w.key)
				continue
			}
			if err := cfg.SettingsStore.Set(ctx, w.key, w.val); err != nil {
				cfg.Logger.Error("setup: settings write", "key", w.key, "error", err)
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		// Defer the doneCh send so the response flushes before the
		// server starts shutting down.
		go func() {
			time.Sleep(150 * time.Millisecond)
			doneCh <- token
		}()
	})

	srv := &http.Server{
		Addr:              listen,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	cfg.Logger.Info("setup wizard listening", "url", "http://"+listen)
	cfg.Logger.Info("open the URL above in your browser to finish setup")

	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return "", fmt.Errorf("setup: bind %s: %w", listen, err)
	}

	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			cfg.Logger.Error("setup: serve", "error", err)
		}
	}()

	token := <-doneCh
	if err := srv.Close(); err != nil {
		cfg.Logger.Warn("setup: shutdown", "error", err)
	}
	return token, nil
}

// loopbackOnly forces the listen host to 127.0.0.1. Wizard has no auth so
// it must never bind to a LAN-visible interface (besides the operator's
// own LAN IP if they explicitly chose one).
func loopbackOnly(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "127.0.0.1:8080"
	}
	// Bare port (":8081") — prepend loopback host.
	if strings.HasPrefix(addr, ":") && len(addr) > 1 && !strings.Contains(addr[1:], ":") {
		return "127.0.0.1" + addr
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "127.0.0.1:8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
