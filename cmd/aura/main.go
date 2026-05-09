package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/config"
	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/health"
	"github.com/aura/aura/internal/logging"
	"github.com/aura/aura/internal/memoryindex"
	"github.com/aura/aura/internal/runtimebootstrap"
	"github.com/aura/aura/internal/settings"
	"github.com/aura/aura/internal/setup"
	"github.com/aura/aura/internal/telegram"
	"github.com/aura/aura/internal/tray"
)

var (
	auraVersion = "3.0"
	commit      = "dev"
	date        = "unknown"
)

const restartDelayEnv = "AURA_RESTART_DELAY_MS"

func main() {
	if handleCLIArgs(os.Args[1:], os.Stdout) {
		return
	}
	initialEnvPath := config.EnvPathFromEnvironment()
	envErr := loadDotEnv(initialEnvPath)

	cfg, err := config.Load()
	logLevel, logDir := "info", os.Getenv("LOG_DIR")
	if logDir == "" {
		logDir = "./logs"
	}
	if cfg != nil {
		logLevel = cfg.LogLevel
		logDir = cfg.LogDir
	}

	// Initialize structured logger with zap backend and secret sanitization.
	// Load dotenv/config first so container defaults such as LOG_DIR=/data/logs
	// are honored before the first log file is opened.
	logger, cleanupLog := logging.Setup(logLevel, logDir)
	maybeDelayRestartChild(logger)
	if envErr != nil && !errors.Is(envErr, os.ErrNotExist) {
		logger.Warn("could not load .env", "error", envErr)
	}
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

func handleCLIArgs(args []string, out io.Writer) bool {
	if len(args) == 0 {
		return false
	}
	switch strings.TrimSpace(args[0]) {
	case "-h", "--help", "help":
		fmt.Fprintf(out, "Aura %s\n\nUsage:\n  aura\n\nEnvironment:\n  AURA_HEADLESS=true       run without desktop tray\n  AURA_ENV_PATH=/data/.env load container/runtime configuration\n  HTTP_PORT=0.0.0.0:8080  dashboard listen address\n", auraVersion)
		return true
	case "-v", "--version", "version":
		fmt.Fprintf(out, "Aura %s (%s, %s)\n", auraVersion, commit, date)
		return true
	default:
		return false
	}
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
		stop, activeLogger, err := startAura(logger, cleanupLog, cfg)
		if err != nil {
			activeLogger.Error("aura startup failed", "error", err)
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
	stop, activeLogger, err := startAura(logger, cleanupLog, cfg)
	if err != nil {
		activeLogger.Error("aura startup failed", "error", err)
		os.Exit(1)
	}

	activeLogger.Info("aura running headless", "dashboard_url", "http://"+dashboardHost(cfg.HTTPPort))
	waitForShutdownSignal()
	stop()
}

func waitForShutdownSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	signal.Stop(sigCh)
}

func maybeDelayRestartChild(logger *slog.Logger) {
	raw := strings.TrimSpace(os.Getenv(restartDelayEnv))
	if raw == "" {
		return
	}
	os.Unsetenv(restartDelayEnv)
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return
	}
	if logger != nil {
		logger.Info("delaying restarted child", "ms", ms)
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func spawnRestartProcess() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	if wd, err := os.Getwd(); err == nil {
		cmd.Dir = wd
	}
	cmd.Env = append(os.Environ(), restartDelayEnv+"=1500")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start replacement process: %w", err)
	}
	return cmd.Process.Release()
}

func startAura(logger *slog.Logger, cleanupLog func(), cfg *config.Config) (_ func(), activeLogger *slog.Logger, err error) {
	activeLogger = logger
	cleanup := cleanupLog
	defer func() {
		if err != nil && cleanup != nil {
			cleanup()
		}
	}()

	if err := runtimebootstrap.EnsureLayout(runtimebootstrap.LayoutConfig{
		RuntimeWorkspacePath: cfg.RuntimeWorkspacePath,
		EnvPath:              cfg.EnvPath,
		DBPath:               cfg.DBPath,
		LogDir:               cfg.LogDir,
		WikiPath:             cfg.WikiPath,
		SkillsPath:           cfg.SkillsPath,
		MCPServersPath:       cfg.MCPServersPath,
		PromptOverlayPath:    cfg.PromptOverlayPath,
	}); err != nil {
		return nil, activeLogger, fmt.Errorf("bootstrap runtime workspace: %w", err)
	}

	openedDBPath := cfg.DBPath
	pool, err := auradb.Open(openedDBPath)
	if err != nil {
		return nil, activeLogger, fmt.Errorf("open database %s: %w", openedDBPath, err)
	}
	closePool := true
	defer func() {
		if err != nil && closePool {
			pool.Close()
		}
	}()

	if err := migrations.Run(context.Background(), pool); err != nil {
		return nil, activeLogger, fmt.Errorf("migrate database %s: %w", openedDBPath, err)
	}
	if err := ensureDatabaseIntegrity(context.Background(), pool, activeLogger); err != nil {
		return nil, activeLogger, fmt.Errorf("database integrity check failed for %s: %w", openedDBPath, err)
	}
	if journalMode, err := auradb.JournalMode(context.Background(), pool); err != nil {
		return nil, activeLogger, fmt.Errorf("check database journal mode %s: %w", openedDBPath, err)
	} else {
		activeLogger.Info("sqlite database ready", "path", openedDBPath, "integrity", "ok", "journal_mode", journalMode)
	}

	// Slice 14a: overlay user-tunable settings from the SQLite settings
	// table on top of the env-loaded config. Bootstrap fields
	// (TelegramToken / HTTPPort / DBPath / LogLevel and the path roots)
	// stay env-only; see internal/settings/applier.go. Empty store is a
	// no-op, so this is safe before the dashboard ever writes a setting.
	settingsStore, err := settings.NewStoreWithDB(pool)
	if err != nil {
		return nil, activeLogger, fmt.Errorf("open settings store: %w", err)
	}
	reconcileBestDefaults(context.Background(), settingsStore, cfg, logger)
	settings.ApplyToConfig(context.Background(), settingsStore, cfg)
	cfg.DBPath = openedDBPath

	// Slice 14b: first-run wizard. If TELEGRAM_TOKEN is still blank after
	// env + settings overlay, the install is fresh. Open a loopback-only
	// HTTP server with a setup form, block until the user submits, then
	// re-load .env + settings so the saved values flow back into cfg.
	if !cfg.IsBootstrapped() {
		token, err := setup.Run(setup.Config{
			Listen:          cfg.HTTPPort,
			AllowRemoteBind: cfg.Headless,
			DotEnvPath:      cfg.EnvPath,
			SettingsStore:   settingsStore,
			Logger:          logger,
		})
		if err != nil {
			return nil, activeLogger, fmt.Errorf("setup wizard: %w", err)
		}
		// Re-load: .env now has TELEGRAM_TOKEN, settings DB now has
		// LLM_*, etc. Replace cfg in place with the fresh values.
		os.Setenv("TELEGRAM_TOKEN", token)
		if err := loadDotEnv(cfg.EnvPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warn("re-load .env after setup", "error", err)
		}
		newCfg, err := config.Load()
		if err != nil {
			return nil, activeLogger, fmt.Errorf("post-setup config load: %w", err)
		}
		reconcileBestDefaults(context.Background(), settingsStore, newCfg, logger)
		settings.ApplyToConfig(context.Background(), settingsStore, newCfg)
		newCfg.DBPath = openedDBPath
		cfg = newCfg
	}

	// Set log level from config (replaces the early logger)
	if cleanup != nil {
		cleanup()
	}
	logger, cleanup = logging.Setup(cfg.LogLevel, cfg.LogDir)
	activeLogger = logger

	// Start health/observability HTTP server
	healthServer := health.NewServer(health.ServerConfig{
		Addr:    cfg.HTTPPort,
		Version: auraVersion,
	}, logger)

	// Register component health providers
	healthServer.RegisterProvider("config", &configHealthProvider{cfg: cfg})
	healthServer.RegisterProvider("database", &databaseHealthProvider{db: pool})
	if cfg.WebSearchProvider != "" && cfg.WebSearchProvider != "disabled" {
		healthServer.RegisterProvider("web_search", &webSearchHealthProvider{})
	}

	var stopCurrent func()
	restart := func(context.Context) error {
		if err := spawnRestartProcess(); err != nil {
			return err
		}
		go func() {
			time.Sleep(250 * time.Millisecond)
			if stopCurrent != nil {
				stopCurrent()
			}
			os.Exit(0)
		}()
		return nil
	}

	bot, err := telegram.New(cfg, settingsStore, pool, logger, telegram.WithRestart(restart))
	if err != nil {
		return nil, activeLogger, fmt.Errorf("create telegram bot: %w", err)
	}

	healthServer.SetBotUsername(bot.Username())
	healthServer.RegisterProvider("compact_memory", &compactMemoryHealthProvider{reader: bot})

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

	var stopOnce sync.Once
	stopCurrent = func() {
		stopOnce.Do(func() {
			logger.Info("shutting down")
			bot.Stop()
			if err := healthServer.Shutdown(context.Background()); err != nil {
				logger.Warn("health server shutdown failed", "error", err)
			}
			pool.Close()
			if cleanup != nil {
				cleanup()
			}
		})
	}

	closePool = false
	return stopCurrent, activeLogger, nil
}

func reconcileBestDefaults(ctx context.Context, store settings.Repository, cfg *config.Config, logger *slog.Logger) {
	changes, err := settings.ApplyBestDefaults(ctx, store, cfg)
	if err != nil {
		logger.Warn("settings best-default reconciliation failed", "error", err)
		return
	}
	if len(changes) == 0 {
		return
	}
	keys := make([]string, 0, len(changes))
	for _, change := range changes {
		keys = append(keys, change.Key)
	}
	sort.Strings(keys)
	logger.Info("settings best-defaults reconciled", "count", len(keys), "keys", strings.Join(keys, ","))
}

func ensureDatabaseIntegrity(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	status, err := auradb.CheckIntegrity(ctx, db)
	if err == nil && status == "ok" {
		return nil
	}
	detail := status
	if err != nil {
		detail = err.Error()
	}
	if !strings.Contains(detail, "compact_memory_fts") {
		if err != nil {
			return err
		}
		return fmt.Errorf("%s", status)
	}
	store, storeErr := memoryindex.NewStore(db)
	if storeErr != nil {
		if err != nil {
			return errors.Join(err, storeErr)
		}
		return errors.Join(fmt.Errorf("%s", status), storeErr)
	}
	if repairErr := store.RebuildFTS(ctx); repairErr != nil {
		if err != nil {
			return errors.Join(err, repairErr)
		}
		return errors.Join(fmt.Errorf("%s", status), repairErr)
	}
	if logger != nil {
		logger.Warn("repaired derived compact memory FTS after sqlite integrity error", "detail", detail)
	}
	status, err = auradb.CheckIntegrity(ctx, db)
	if err != nil {
		return err
	}
	if status != "ok" {
		return fmt.Errorf("%s", status)
	}
	return nil
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

type databaseHealthProvider struct {
	db *sql.DB
}

func (p *databaseHealthProvider) HealthStatus() health.ComponentHealth {
	status, err := auradb.CheckIntegrity(context.Background(), p.db)
	if err != nil {
		return health.ComponentHealth{
			Status: "error",
			Detail: err.Error(),
		}
	}
	if status != "ok" {
		return health.ComponentHealth{
			Status: "error",
			Detail: status,
		}
	}
	journalMode, err := auradb.JournalMode(context.Background(), p.db)
	if err != nil {
		return health.ComponentHealth{
			Status: "error",
			Detail: err.Error(),
		}
	}
	return health.ComponentHealth{
		Status: "ok",
		Detail: "integrity ok; journal=" + journalMode,
	}
}

type webSearchHealthProvider struct{}

func (p *webSearchHealthProvider) HealthStatus() health.ComponentHealth {
	return health.ComponentHealth{
		Status: "ok",
		Detail: "web search provider configured",
	}
}

type compactMemoryHealthReader interface {
	CompactMemoryHealth() memoryindex.VectorHealth
}

type compactMemoryHealthProvider struct {
	reader compactMemoryHealthReader
}

func (p *compactMemoryHealthProvider) HealthStatus() health.ComponentHealth {
	if p == nil || p.reader == nil {
		return health.ComponentHealth{Status: "ok", Detail: "compact memory mirror unavailable"}
	}
	state := p.reader.CompactMemoryHealth()
	if !state.Enabled {
		return health.ComponentHealth{Status: "ok", Detail: "compact memory mirror disabled"}
	}
	status := "ok"
	if state.Running {
		status = "ok"
	}
	detail := fmt.Sprintf("collection=%s docs=%d vector=%d", state.Collection, state.LastDocsIndexed, state.VectorSize)
	if state.Running {
		detail += " running=true"
	}
	if state.LastError != "" {
		status = "degraded"
		detail += " last_error=" + state.LastError
	}
	return health.ComponentHealth{Status: status, Detail: detail}
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
// process environment when the key is not already set. Explicit process
// environment wins so Docker Compose can provide container paths/ports while
// mounted .env fills in user-managed secrets like TELEGRAM_TOKEN.
// Lines starting with `#` and blank lines are ignored. Surrounding
// single/double quotes are stripped.
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
		if key != "" && os.Getenv(key) == "" {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}
