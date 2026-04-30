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

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/health"
	"github.com/aura/aura/internal/logging"
	"github.com/aura/aura/internal/telegram"
	"github.com/aura/aura/internal/tracing"
	"github.com/aura/aura/internal/tray"
)

const auraVersion = "3.0"

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

	healthServer.Start()

	logger.Info("aura starting", "version", auraVersion)

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
		Title:   "Aura",
		Tooltip: "Aura — running on " + cfg.HTTPPort,
		Version: auraVersion,
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
