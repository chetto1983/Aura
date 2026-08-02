package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

type doctorProbe func(context.Context, *config.Config) (string, error)

type doctorPostgresPool interface {
	Ping(context.Context) error
	Close()
}

type doctorCheck struct {
	name        string
	probe       doctorProbe
	failureCode int
}

var (
	doctorProbePostgres   doctorProbe = defaultDoctorProbePostgres
	doctorProbeEmbed      doctorProbe = defaultDoctorProbeEmbed
	doctorProbeMCPServers doctorProbe = defaultDoctorProbeMCPServers
	doctorLookupLLMKey                = func() string { return os.Getenv("OPENROUTER_API_KEY") } //nolint:gosec // boolean presence check only; value is never printed.
	doctorHTTPClient                  = &http.Client{Timeout: 10 * time.Second}
	doctorOpenPostgres                = func(ctx context.Context, cfg *config.Config) (doctorPostgresPool, error) {
		return db.Open(ctx, &cfg.DB)
	}
)

func runDoctor(args []string) {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: aura doctor")
		os.Exit(exitUsage)
	}
	os.Exit(runDoctorWithConfig(context.Background(), os.Stdout, config.LoadDB()))
}

func runDoctorWithConfig(ctx context.Context, out io.Writer, cfg *config.Config) int {
	exitCode := 0
	for _, check := range doctorChecks() {
		detail, err := check.probe(ctx, cfg)
		if err != nil {
			if _, writeErr := fmt.Fprintf(out, "FAIL %s: %v\n", check.name, err); writeErr != nil {
				return exitInfra
			}
			if exitCode == 0 {
				exitCode = check.failureCode
			}
			continue
		}
		if _, writeErr := fmt.Fprintf(out, "PASS %s: %s\n", check.name, detail); writeErr != nil {
			return exitInfra
		}
	}
	if exitCode == 0 {
		if _, err := fmt.Fprintln(out, "status: OK"); err != nil {
			return exitInfra
		}
		return 0
	}
	if _, err := fmt.Fprintln(out, "status: FAIL"); err != nil {
		return exitInfra
	}
	return exitCode
}

func doctorChecks() []doctorCheck {
	return []doctorCheck{
		{name: "postgres", probe: doctorProbePostgres, failureCode: exitUnreachable},
		{name: "embed", probe: doctorProbeEmbed, failureCode: exitUnreachable},
		{name: "llm_key", probe: doctorProbeLLMKey, failureCode: 0},
		{name: "mcp", probe: doctorProbeMCPServers, failureCode: exitUnreachable},
	}
}

func defaultDoctorProbePostgres(ctx context.Context, cfg *config.Config) (string, error) {
	start := time.Now()
	pool, err := doctorOpenPostgres(ctx, cfg)
	if err != nil {
		return "", err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return "", err
	}
	latency := time.Since(start)
	return fmt.Sprintf("reachable (%s)", latency.Round(time.Millisecond)), nil
}

func defaultDoctorProbeEmbed(ctx context.Context, cfg *config.Config) (string, error) {
	client := doctorHTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	defer client.CloseIdleConnections()
	embedder := embeddingClient(cfg, client)
	vectors, err := embedder.Embed(ctx, []string{"aura doctor probe"})
	if err != nil {
		return "", err
	}
	if len(vectors) == 0 {
		return "", fmt.Errorf("embedding sidecar returned no vectors")
	}
	return fmt.Sprintf("dimension %d", len(vectors[0])), nil
}

// defaultDoctorProbeMCPServers live-probes ONLY the enabled + runnable +
// streamable-HTTP managed MCP servers (D-16/D-17/D-18, MCPH-09) via mcp.ProbeServer,
// bounded per-server by AURA_MCP_PROBE_TIMEOUT. It does NOT dial disabled,
// trust-blocked, or stdio servers: the resolved runtime policy already excludes
// disabled/blocked entries, and the streamable_http filter below drops stdio. A
// single unreachable server fails only its own name in the aggregated detail,
// never the others.
func defaultDoctorProbeMCPServers(ctx context.Context, cfg *config.Config) (string, error) {
	runnable, err := doctorRuntimeMCPServers(cfg)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(runnable))
	for name, server := range runnable {
		serverType, _, classifyErr := mcp.Classify(server)
		if classifyErr != nil || serverType != mcp.ServerTypeStreamableHTTP {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return "no HTTP MCP servers configured", nil
	}

	var unreachable []string
	for _, name := range names {
		probeCtx, cancel := context.WithTimeout(ctx, resolveMCPProbeTimeout())
		res := probeManagedMCPServer(probeCtx, name, runnable[name])
		cancel()
		if !res.OK {
			unreachable = append(unreachable, name)
		}
	}
	if len(unreachable) > 0 {
		return "", fmt.Errorf("%d/%d HTTP MCP servers unreachable: %s", len(unreachable), len(names), strings.Join(unreachable, ", "))
	}
	return fmt.Sprintf("%d/%d HTTP MCP servers reachable", len(names), len(names)), nil
}

func doctorRuntimeMCPServers(cfg *config.Config) (map[string]mcp.ManagedServer, error) {
	if cfg != nil && cfg.MCPServersErr != nil {
		return nil, cfg.MCPServersErr
	}
	if cfg != nil && (cfg.MCPPolicies != nil || cfg.MCPServers != nil) {
		return cfg.MCPPolicies, nil
	}
	doc, _, err := loadManagedMCPConfig()
	if err != nil {
		return nil, err
	}
	return mcpmanager.RunnableManagedServers(doc)
}

func doctorProbeLLMKey(context.Context, *config.Config) (string, error) {
	if strings.TrimSpace(doctorLookupLLMKey()) == "" {
		return "not configured (keyless boot allowed; agent calls return llm_not_configured)", nil
	}
	return "configured", nil
}
