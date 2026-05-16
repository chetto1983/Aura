package api

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

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/secrets"
)

//go:embed setup_page.html
var pageHTML string

// Config controls how the wizard runs.
type SetupConfig struct {
	// Listen is the bind address. Forced to 127.0.0.1 host even if the
	// caller passes a LAN address — the wizard has no auth.
	Listen string

	// AllowRemoteBind preserves Listen when it binds all interfaces. Use only
	// for headless/container installs where Docker controls host exposure.
	AllowRemoteBind bool

	// SecretsStore receives secret keys (Telegram token, LLM API keys, etc.).
	// Required — wizard cannot proceed without it.
	SecretsStore secrets.Store

	// SettingsStore receives non-secret tunables (LLM base URL, model,
	// OCR settings, etc.). Required.
	SettingsStore config.Writer

	// Logger receives wizard activity. Required.
	Logger *slog.Logger
}

// Run starts the wizard's HTTP server, blocks until the user submits a
// valid form, then returns the saved Telegram token. Callers should
// re-load .env after Run returns. Returns context.Canceled-style error
// only on shutdown failure.
func SetupRun(cfg SetupConfig) (telegramToken string, err error) {
	if cfg.SecretsStore == nil {
		return "", errors.New("setup: SecretsStore required")
	}
	if cfg.SettingsStore == nil {
		return "", errors.New("setup: SettingsStore required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	listen := listenAddress(cfg.Listen, cfg.AllowRemoteBind)
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
		locale := detectLocale(r.Header.Get("Accept-Language"))
		t := setupText(locale)
		presets := localizedPresets(locale)
		presetsJSON, _ := json.Marshal(presets)
		textJSON, _ := json.Marshal(t)
		data := map[string]any{
			"Locale":      locale,
			"T":           t,
			"TextJSON":    template.JS(textJSON), //nolint:gosec // static translation table
			"Presets":     presets,
			"PresetsJSON": template.JS(presetsJSON), //nolint:gosec // local-only loopback page; presets are static
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := tpl.Execute(w, data); err != nil {
			cfg.Logger.Warn("setup: template render", "error", err)
		}
	})

	mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeSetupJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
			return
		}
		var req struct {
			Preset    string `json:"preset"`
			BaseURL   string `json:"base_url"`
			APIKey    string `json:"api_key"`
			ProbePath string `json:"probe_path"`
		}
		if err := decodeJSONBody(r, &req); err != nil {
			writeSetupJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON"})
			return
		}
		probePath := req.ProbePath
		if probePath == "" {
			if p, ok := SetupPresetByID(req.Preset); ok && p.ProbePath != "" {
				probePath = p.ProbePath
			} else {
				probePath = "/models"
			}
		}
		result := SetupProbeProvider(r.Context(), req.BaseURL, req.APIKey, probePath)
		writeSetupJSON(w, http.StatusOK, result)
	})

	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeSetupJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
			return
		}
		var req struct {
			TelegramToken   string `json:"telegram_token"`
			LLMPreset       string `json:"llm_preset"`
			LLMBaseURL      string `json:"llm_base_url"`
			LLMModel        string `json:"llm_model"`
			LLMAPIKey       string `json:"llm_api_key"`
			EmbeddingAPIKey string `json:"embedding_api_key"`
			MistralAPIKey   string `json:"mistral_api_key"`
		}
		if err := decodeJSONBody(r, &req); err != nil {
			writeSetupJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid JSON"})
			return
		}
		token := strings.TrimSpace(req.TelegramToken)
		if token == "" {
			writeSetupJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "Telegram token is required"})
			return
		}

		ctx := r.Context()

		// 1. Secrets store: Telegram token + API keys never touch .env.
		if err := writeSecret(ctx, cfg.SecretsStore, secrets.KeyTelegramToken, token); err != nil {
			cfg.Logger.Error("setup: write telegram token", "error", err)
			writeSetupJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "could not save Telegram token"})
			return
		}
		secretWrites := []struct{ key, val string }{
			{secrets.KeyLLMAPIKey, strings.TrimSpace(req.LLMAPIKey)},
			{secrets.KeyEmbeddingAPIKey, strings.TrimSpace(req.EmbeddingAPIKey)},
		}
		for _, sw := range secretWrites {
			if err := writeSecret(ctx, cfg.SecretsStore, sw.key, sw.val); err != nil {
				cfg.Logger.Error("setup: write secret", "key", sw.key, "error", err)
			}
		}

		// 2. Settings DB: non-secret tunables only. Each key is overridable
		// per internal/config/applier.go.
		writes := []struct{ key, val string }{
			{config.KeyLLMBaseURL, strings.TrimSpace(req.LLMBaseURL)},
			{config.KeyLLMModel, strings.TrimSpace(req.LLMModel)},
			{config.KeyMistralAPIKey, strings.TrimSpace(req.MistralAPIKey)},
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

		writeSetupJSON(w, http.StatusOK, map[string]any{"ok": true})
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

func listenAddress(addr string, allowRemoteBind bool) string {
	if allowRemoteBind {
		addr = strings.TrimSpace(addr)
		if addr != "" {
			return addr
		}
	}
	return loopbackOnly(addr)
}

func writeSetupJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
