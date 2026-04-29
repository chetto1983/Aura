package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/health"
	"github.com/aura/aura/internal/logging"
	"github.com/aura/aura/internal/telegram"
	"github.com/aura/aura/internal/tracing"
)

func main() {
	// Initialize structured logger with zap backend and secret sanitization
	logger := logging.Setup("info")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Set log level from config
	logger = logging.Setup(cfg.LogLevel)

	// Initialize OpenTelemetry tracing (disabled unless OTEL_ENABLED is set)
	shutdown, err := tracing.SetupIfEnabled("aura", "3.0", cfg.OTelEnabled, logger)
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
		Version: "3.0",
	}, logger)

	// Register component health providers
	healthServer.RegisterProvider("config", &configHealthProvider{cfg: cfg})

	healthServer.Start()
	defer func() {
		if err := healthServer.Shutdown(context.Background()); err != nil {
			logger.Warn("health server shutdown failed", "error", err)
		}
	}()

	bot, err := telegram.New(cfg, logger)
	if err != nil {
		logger.Error("failed to create telegram bot", "error", err)
		os.Exit(1)
	}

	logger.Info("aura starting", "version", "3.0")

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go bot.Start()

	<-sigCh
	logger.Info("shutting down")
	bot.Stop()
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