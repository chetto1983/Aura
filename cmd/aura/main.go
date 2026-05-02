package main

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/health"
	"github.com/aura/aura/internal/logging"
	"github.com/aura/aura/internal/settings"
	"github.com/aura/aura/internal/setup"
	"github.com/aura/aura/internal/telegram"
	"github.com/aura/aura/internal/tracing"
	"github.com/aura/aura/internal/tray"
)

var (
	auraVersion = "3.0"
	commit      = "dev"
	date        = "unknown"
)

func main() {
	// Initialize structured logger with zap backend and secret sanitization
	logger := logging.Setup("info")

	if err := loadDotEnv(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Warn("could not load .env", "error", err)
	}

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Slice 14a: overlay user-tunable settings from the SQLite settings
	// table on top of the env-loaded config. Bootstrap fields
	// (TelegramToken / HTTPPort / DBPath / LogLevel and the path roots)
	// stay env-only — see internal/settings/applier.go. Empty store is a
	// no-op, so this is safe before the dashboard ever writes a setting.
	settingsStore, err := settings.OpenStore(cfg.DBPath)
	if err != nil {
		logger.Warn("settings store unavailable, using env only", "error", err)
	} else {
		settings.ApplyToConfig(context.Background(), settingsStore, cfg)
		defer settingsStore.Close()
	}

	// Slice 14b: first-run wizard. If TELEGRAM_TOKEN is still blank after
	// env + settings overlay, the install is fresh. Open a loopback-only
	// HTTP server with a setup form, block until the user submits, then
	// re-load .env + settings so the saved values flow back into cfg.
	if !cfg.IsBootstrapped() {
		if settingsStore == nil {
			logger.Error("first-run setup needs a writable DB; check DB_PATH and disk permissions", "db_path", cfg.DBPath)
			os.Exit(1)
		}
		token, err := setup.Run(setup.Config{
			Listen:        cfg.HTTPPort,
			DotEnvPath:    ".env",
			SettingsStore: settingsStore,
			Logger:        logger,
		})
		if err != nil {
			logger.Error("setup wizard failed", "error", err)
			os.Exit(1)
		}
		// Re-load: .env now has TELEGRAM_TOKEN, settings DB now has
		// LLM_*, etc. Replace cfg in place with the fresh values.
		os.Setenv("TELEGRAM_TOKEN", token)
		if err := loadDotEnv(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warn("re-load .env after setup", "error", err)
		}
		newCfg, err := config.Load()
		if err != nil {
			logger.Error("post-setup config load", "error", err)
			os.Exit(1)
		}
		settings.ApplyToConfig(context.Background(), settingsStore, newCfg)
		cfg = newCfg
	}

	// Set log level from config
	logger = logging.Setup(cfg.LogLevel)

	// Initialize OpenTelemetry tracing (disabled unless OTEL_ENABLED is set)
	shutdown, err := tracing.SetupIfEnabled("aura", auraVersion, cfg.OTelEnabled, logger)
	if err != nil {
		logger.Warn("tracing setup failed, continuing without traces", "error", err)
	} else {
		defer func() {
			if err := shutdown(context.Background()); err != nil {
				logger.Warn("tracing shutdown failed", "error", err)
			}
		}()
	}

	// Start health/observability HTTP server
	healthServer := health.NewServer(health.ServerConfig{
		Addr:    cfg.HTTPPort,
		Version: auraVersion,
	}, logger)

	// Register component health providers
	healthServer.RegisterProvider("config", &configHealthProvider{cfg: cfg})
	if cfg.OllamaAPIKey != "" {
		healthServer.RegisterProvider("web_search", &webSearchHealthProvider{})
	}

	bot, err := telegram.New(cfg, logger)
	if err != nil {
		logger.Error("failed to create telegram bot", "error", err)
		os.Exit(1)
	}

	healthServer.SetBotUsername(bot.Username())

	// Slice 10a: mount the read-only JSON API on the health server. Strip
	// the /api prefix so api.NewRouter sees /health, /wiki/..., /sources/...
	healthServer.Mount("/api/", http.StripPrefix("/api", bot.APIHandler()))

	// Slice 10b: serve the embedded SPA at /. The static handler also handles
	// SPA fallback for deep links like /wiki/:slug. Register *after* /api/ so
	// Go's ServeMux routes the longer prefix first.
	if static, err := api.StaticHandler(); err == nil {
		healthServer.Mount("/", static)
	} else if errors.Is(err, api.ErrNoStaticAssets) {
		logger.Warn("dashboard SPA unavailable — run `make web-build`",
			"detail", "internal/api/dist is empty; /api still works, only / is missing")
	} else {
		logger.Error("failed to mount dashboard SPA", "error", err)
	}

	healthServer.Start()

	logger.Info("aura starting", "version", auraVersion, "commit", commit, "date", date)

	go bot.Start()

	// Bridge SIGINT/SIGTERM to tray.Stop so the same shutdown path runs whether
	// the user closes from the tray menu or sends a signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		tray.Stop()
	}()

	// Run tray on the main goroutine. Blocks until the user clicks Quit or a
	// signal triggers tray.Stop above.
	if err := tray.Run(tray.Options{
		Title:        "Aura",
		Tooltip:      "Aura — running on " + cfg.HTTPPort,
		Version:      auraVersion,
		DashboardURL: "http://" + dashboardHost(cfg.HTTPPort),
	}); err != nil {
		logger.Warn("tray exited with error", "error", err)
	}

	logger.Info("shutting down")
	bot.Stop()
	if err := healthServer.Shutdown(context.Background()); err != nil {
		logger.Warn("health server shutdown failed", "error", err)
	}
}

// configHealthProvider reports the health of the config subsystem.
type configHealthProvider struct {
	cfg *config.Config
}

func (p *configHealthProvider) HealthStatus() health.ComponentHealth {
	return health.ComponentHealth{
		Status: "ok",
		Detail: "configuration loaded",
	}
}

type webSearchHealthProvider struct{}

func (p *webSearchHealthProvider) HealthStatus() health.ComponentHealth {
	return health.ComponentHealth{
		Status: "ok",
		Detail: "Ollama web tools configured",
	}
}

// dashboardHost translates the HTTP_PORT bind string into a browseable URL
// host. ":8080" -> "localhost:8080"; "127.0.0.1:8080" -> "127.0.0.1:8080";
// "0.0.0.0:8080" -> "localhost:8080" (the user opens locally even when bound LAN-wide).
func dashboardHost(port string) string {
	if strings.HasPrefix(port, ":") {
		return "localhost" + port
	}
	if strings.HasPrefix(port, "0.0.0.0") {
		return "localhost" + strings.TrimPrefix(port, "0.0.0.0")
	}
	return port
}

// loadDotEnv reads KEY=VALUE pairs from the given file and sets them in the
// process environment. Mirrors the helper used by cmd/debug_tools and
// cmd/debug_ingest so all entrypoints honor the same .env. Lines starting
// with `#` and blank lines are ignored. Surrounding single/double quotes are
// stripped. Existing env values are overwritten so .env is the source of
// truth during local runs.
func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}
