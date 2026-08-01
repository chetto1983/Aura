// Command arcadedb-mcp exposes Aura's graph substrate as an MCP server backed
// directly by ArcadeDB.
//
// It replaces two sidecars that both sat on Neo4j: aura-agent-memory-mcp (a
// vendored Python fork) and mcp-neo4j-cypher. The tool surface here is designed
// for Aura rather than filtered down from someone else's -- internal/agent/
// mcptools/bridge_memory.go currently suppresses three inherited tools and
// injects a parameter the upstream server documents but never receives.
//
// Tools are added one file at a time (tool_*.go); every one is exercised
// against a live database before the next is written.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

const (
	serverName    = "aura-arcadedb"
	serverVersion = "0.1.0"

	defaultPort            = 8096
	defaultReadTimeout     = 15 * time.Second
	defaultWriteTimeout    = 120 * time.Second
	defaultShutdownTimeout = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(logger); err != nil {
		logger.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, addr, err := configFromEnv()
	if err != nil {
		return err
	}
	client, err := arcadedb.New(cfg)
	if err != nil {
		return err
	}
	// The schema is created at boot, not on first write: a tool call that has
	// to run DDL first would pay for it in its own latency and race a sibling.
	schemaCtx, cancelSchema := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	err = client.EnsureMemorySchema(schemaCtx)
	cancelSchema()
	if err != nil {
		return err
	}

	server := newServer(client, time.Now)
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.Handle("/mcp/", handler)
	// Liveness must not touch the database: a readiness probe that fails when
	// ArcadeDB blips would restart this process for someone else's outage.
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", addr, "database", cfg.Database, "server", serverName)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
		defer cancel()
		logger.Info("shutting down")
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return <-errCh
	}
}

// newServer builds the MCP server and registers every tool. One line per tool,
// so the surface is readable in one place.
func newServer(client *arcadedb.Client, now clock) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Title:   "Aura ArcadeDB",
		Version: serverVersion,
	}, nil)
	addGraphSchemaTool(server, client)
	addMemoryUpsertFactTool(server, client, now)
	addMemoryFactsAboutTool(server, client)
	addMemorySearchTool(server, client)
	addMemoryForgetTool(server, client)
	addMemoryEntitiesTool(server, client)
	addMemoryDigestTool(server, client)
	addMemoryMergeTool(server, client)
	// Documents are NOT here. They live as bytes in Garage with a catalog row in
	// Postgres, found by document_search and handed over whole by document_open —
	// measured, chunk retrieval answered every aggregate at 0% for any k. Mounting
	// a second document_search over ArcadeDB would give the model two tools with
	// one name and a store that holds nothing.
	return server
}

func configFromEnv() (arcadedb.Config, string, error) {
	cfg := arcadedb.Config{
		BaseURL:  os.Getenv("ARCADEDB_URL"),
		Database: os.Getenv("ARCADEDB_DATABASE"),
		User:     os.Getenv("ARCADEDB_USER"),
		Password: os.Getenv("ARCADEDB_PASSWORD"),
	}
	if timeout := strings.TrimSpace(os.Getenv("ARCADEDB_TIMEOUT_SECONDS")); timeout != "" {
		seconds, err := strconv.Atoi(timeout)
		if err != nil || seconds <= 0 {
			return cfg, "", fmt.Errorf("ARCADEDB_TIMEOUT_SECONDS must be a positive integer, got %q", timeout)
		}
		cfg.Timeout = time.Duration(seconds) * time.Second
	}
	port := defaultPort
	if raw := strings.TrimSpace(os.Getenv("AURA_ARCADEDB_MCP_PORT")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			return cfg, "", fmt.Errorf("AURA_ARCADEDB_MCP_PORT must be a port number, got %q", raw)
		}
		port = parsed
	}
	host := strings.TrimSpace(os.Getenv("AURA_ARCADEDB_MCP_HOST"))
	if host == "" {
		host = "0.0.0.0"
	}
	return cfg, host + ":" + strconv.Itoa(port), nil
}
