package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/config"
	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
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
	logger, cleanupLog := logging.Setup("info", "./logs")

	initialEnvPath := config.EnvPathFromEnvironment()
	if err := loadDotEnv(initialEnvPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		logger.Warn("could not load .env", "error", err)
	}

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		cleanupLog()
		os.Exit(1)
	}

	if cfg.Headless {
		runHeadless(logger, cleanupLog, cfg)
		return
	}
	runWithTray(logger, cleanupLog, cfg)
}

func runWithTray(logger *slog.Logger, cleanupLog func(), cfg *config.Config) {
	var (
		mu       sync.Mutex
		stopping bool
		stopApp  func()
	)

	setStopApp := func(stop func()) {
		mu.Lock()
		if stopping {
			mu.Unlock()
			stop()
			return
		}
		stopApp = stop
		mu.Unlock()
	}
	requestStop := func() {
		mu.Lock()
		stopping = true
		stop := stopApp
		stopApp = nil
		mu.Unlock()
		if stop != nil {
			stop()
		}
	}

	go func() {
		stop, err := startAura(logger, cleanupLog, cfg)
		if err != nil {
			logger.Error("aura startup failed", "error", err)
			tray.Stop()
			return
		}
		setStopApp(stop)
	}()

	// Bridge SIGINT/SIGTERM to tray.Stop so the same shutdown path runs whether
	// the user closes from the tray menu or sends a signal.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		tray.Stop()
	}()

	// Run tray on the main goroutine. Blocks until the user clicks Quit, a
	// signal triggers tray.Stop above, or startup fails.
	if err := tray.Run(tray.Options{
		Title:        "Aura",
		Tooltip:      "Aura - starting on " + cfg.HTTPPort,
		Version:      auraVersion,
		DashboardURL: "http://" + dashboardHost(cfg.HTTPPort),
	}); err != nil {
		logger.Warn("tray exited with error", "error", err)
	}
	requestStop()
}

func runHeadless(logger *slog.Logger, cleanupLog func(), cfg *config.Config) {
	stop, err := startAura(logger, cleanupLog, cfg)
	if err != nil {
		logger.Error("aura startup failed", "error", err)
		os.Exit(1)
	}

	logger.Info("aura running headless", "dashboard_url", "http://"+dashboardHost(cfg.HTTPPort))
	waitForShutdownSignal()
	stop()
}

func waitForShutdownSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	signal.Stop(sigCh)
}

func startAura(logger *slog.Logger, cleanupLog func(), cfg *config.Config) (_ func(), err error) {
	cleanup := cleanupLog
	defer func() {
		if err != nil && cleanup != nil {
			cleanup()
		}
	}()

	pool, err := auradb.Open(cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", cfg.DBPath, err)
	}
	closePool := true
	defer func() {
		if err != nil && closePool {
			pool.Close()
		}
	}()

	if err := migrations.Run(context.Background(), pool); err != nil {
		return nil, fmt.Errorf("migrate database %s: %w", cfg.DBPath, err)
	}

	// Slice 14a: overlay user-tunable settings from the SQLite settings
	// table on top of the env-loaded config. Bootstrap fields
	// (TelegramToken / HTTPPort / DBPath / LogLevel and the path roots)
	// stay env-only; see internal/settings/applier.go. Empty store is a
	// no-op, so this is safe before the dashboard ever writes a setting.
	settingsStore, err := settings.NewStoreWithDB(pool)
	if err != nil {
		return nil, fmt.Errorf("open settings store: %w", err)
	}
	settings.ApplyToConfig(context.Background(), settingsStore, cfg)

	// Slice 14b: first-run wizard. If TELEGRAM_TOKEN is still blank after
	// env + settings overlay, the install is fresh. Open a loopback-only
	// HTTP server with a setup form, block until the user submits, then
	// re-load .env + settings so the saved values flow back into cfg.
	if !cfg.IsBootstrapped() {
		token, err := setup.Run(setup.Config{
			Listen:        cfg.HTTPPort,
			DotEnvPath:    cfg.EnvPath,
			SettingsStore: settingsStore,
			Logger:        logger,
		})
		if err != nil {
			return nil, fmt.Errorf("setup wizard: %w", err)
		}
		// Re-load: .env now has TELEGRAM_TOKEN, settings DB now has
		// LLM_*, etc. Replace cfg in place with the fresh values.
		os.Setenv("TELEGRAM_TOKEN", token)
		if err := loadDotEnv(cfg.EnvPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warn("re-load .env after setup", "error", err)
		}
		newCfg, err := config.Load()
		if err != nil {
			return nil, fmt.Errorf("post-setup config load: %w", err)
		}
		settings.ApplyToConfig(context.Background(), settingsStore, newCfg)
		cfg = newCfg
	}

	// Set log level from config (replaces the early logger)
	if cleanup != nil {
		cleanup()
	}
	logger, cleanup = logging.Setup(cfg.LogLevel, cfg.LogDir)

	// Initialize OpenTelemetry tracing (disabled unless OTEL_ENABLED is set)
	shutdown, err := tracing.SetupIfEnabled("aura", auraVersion, cfg.OTelEnabled, logger)
	if err != nil {
		logger.Warn("tracing setup failed, continuing without traces", "error", err)
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

	bot, err := telegram.New(cfg, settingsStore, pool, logger)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
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
		logger.Warn("dashboard SPA unavailable - run `make web-build`",
			"detail", "internal/api/dist is empty; /api still works, only / is missing")
	} else {
		logger.Error("failed to mount dashboard SPA", "error", err)
	}

	healthServer.Start()

	logger.Info("aura starting", "version", auraVersion, "commit", commit, "date", date)

	go bot.Start()

	closePool = false
	return func() {
		logger.Info("shutting down")
		bot.Stop()
		if err := healthServer.Shutdown(context.Background()); err != nil {
			logger.Warn("health server shutdown failed", "error", err)
		}
		if shutdown != nil {
			if err := shutdown(context.Background()); err != nil {
				logger.Warn("tracing shutdown failed", "error", err)
			}
		}
		pool.Close()
		if cleanup != nil {
			cleanup()
		}
	}, nil
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
