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
	// cfg is the TENANT template: the resolver overrides User/Password/Database per
	// identity. No client is opened against it here, because there is no shared
	// database left to open.
	// The dense leg is OPTIONAL by construction: NewSidecarEmbedder returns nil
	// for an empty AURA_EMBED_BASE_URL, and a nil embedder leaves retrieval
	// lexical. So an operator with no embedding sidecar gets the behaviour that
	// shipped, and one with a sidecar that is merely DOWN gets it too — the
	// search falls back per call rather than at boot.
	embedder := arcadedb.NewSidecarEmbedder(
		os.Getenv("AURA_EMBED_BASE_URL"),
		os.Getenv("AURA_EMBED_MODEL"),
		os.Getenv("AURA_EMBED_API_KEY"),
		0,
	)
	if embedder != nil {
		// NOT attached to `client`: that one only ever runs DDL as the admin, and
		// the per-tenant clients the resolver builds get the embedder themselves.
		logger.Info("dense retrieval enabled", "embed_url", os.Getenv("AURA_EMBED_BASE_URL"))
	} else {
		logger.Info("dense retrieval disabled: no AURA_EMBED_BASE_URL; retrieval is lexical only")
	}
	// The schema is NOT created at boot any more: there is no single database to
	// create it in. Each tenant's database is provisioned on its first call, by
	// tenants.For, which is also the only place that needs the admin credential.

	// One database per identity. The admin client exists only to CREATE them; every
	// read and write goes through a client bound to a single tenant's database, so
	// isolation is the server's job and not a WHERE clause we must never forget.
	// TWO credentials, the same split Postgres already has between AURA_DB_URL and
	// AURA_DB_MIGRATE_URL: the tenant credential reads and writes one database, and
	// a separate admin credential does DDL. ArcadeDB enforces it — "Only root user
	// is authorized to execute server commands" is what the tenant user gets if it
	// tries to create a database, which is the refusal you want.
	//
	// No admin credential is a supported configuration: databases must then be
	// pre-provisioned, and a call for an unknown identity fails rather than
	// silently creating one.
	var admin *arcadedb.Client
	if adminCfg, ok := adminConfigFromEnv(cfg); ok {
		if adminClient, aerr := arcadedb.New(adminCfg); aerr == nil {
			admin = adminClient
			logger.Info("database provisioning enabled", "admin_user", adminCfg.User)
		} else {
			logger.Warn("admin credential unusable; databases must be pre-provisioned", "error", aerr)
		}
	} else {
		logger.Info("no admin credential; databases must be pre-provisioned")
		admin = nil
	}
	credentials, cerr := newTenantCredentials()
	if cerr != nil {
		// Fail CLOSED at boot. Falling back to one shared credential would leave
		// every identity reading the same memory, which is the failure this whole
		// design exists to prevent, and it would do it silently.
		return fmt.Errorf("memory isolation: %w", cerr)
	}
	server := newServer(newTenants(cfg, admin, embedder, credentials), time.Now)
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
func newServer(tenants *tenants, now clock) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Title:   "Aura ArcadeDB",
		Version: serverVersion,
	}, nil)
	addGraphSchemaTool(server, tenants)
	addMemoryUpsertFactTool(server, tenants, now)
	addMemoryFactsAboutTool(server, tenants)
	addMemorySearchTool(server, tenants)
	addMemoryForgetTool(server, tenants)
	addMemoryEntitiesTool(server, tenants)
	addMemoryDigestTool(server, tenants)
	addMemoryMergeTool(server, tenants)
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

// adminConfigFromEnv reads the DDL credential. It is deliberately separate from
// the tenant one: the whole point of the split is that the credential doing the
// reading cannot create or drop a database.
func adminConfigFromEnv(base arcadedb.Config) (arcadedb.Config, bool) {
	user := strings.TrimSpace(os.Getenv("ARCADEDB_ADMIN_USER"))
	password := os.Getenv("ARCADEDB_ADMIN_PASSWORD")
	if user == "" || strings.TrimSpace(password) == "" {
		return arcadedb.Config{}, false
	}
	admin := base
	admin.User = user
	admin.Password = password
	return admin, true
}
