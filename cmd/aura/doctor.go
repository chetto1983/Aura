package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

type doctorProbe func(context.Context, *config.Config) (string, error)

type doctorPostgresPool interface {
	Ping(context.Context) error
	Close()
}

type doctorNeo4jClient interface {
	Read(context.Context, string, map[string]any) ([]map[string]any, error)
	Close() error
}

type doctorCheck struct {
	name        string
	probe       doctorProbe
	failureCode int
}

var (
	doctorProbePostgres   doctorProbe = defaultDoctorProbePostgres
	doctorProbeNeo4j      doctorProbe = defaultDoctorProbeNeo4j
	doctorProbeEmbed      doctorProbe = defaultDoctorProbeEmbed
	doctorProbeMCPBinary  doctorProbe = defaultDoctorProbeMCPBinary
	doctorProbeMCPServers doctorProbe = defaultDoctorProbeMCPServers
	doctorLookupLLMKey                = func() string { return os.Getenv("OPENROUTER_API_KEY") } //nolint:gosec // boolean presence check only; value is never printed.
	doctorLookPath                    = exec.LookPath
	doctorHTTPClient                  = &http.Client{Timeout: 10 * time.Second}
	doctorOpenPostgres                = func(ctx context.Context, cfg *config.Config) (doctorPostgresPool, error) {
		return db.Open(ctx, &cfg.DB)
	}
	doctorOpenNeo4j = func(ctx context.Context, cfg *config.Config) (doctorNeo4jClient, error) {
		return knowledge.Open(ctx, &cfg.Neo4j)
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
		{name: "neo4j", probe: doctorProbeNeo4j, failureCode: exitUnreachable},
		{name: "embed", probe: doctorProbeEmbed, failureCode: exitUnreachable},
		{name: "mcp-neo4j-cypher", probe: doctorProbeMCPBinary, failureCode: exitInfra},
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

func defaultDoctorProbeNeo4j(ctx context.Context, cfg *config.Config) (string, error) {
	mcp, err := doctorOpenNeo4j(ctx, cfg)
	if err != nil {
		return "", err
	}
	defer func() { _ = mcp.Close() }()
	rows, err := mcp.Read(ctx, "RETURN 1 AS ok", nil)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("RETURN 1 returned no rows")
	}
	return "RETURN 1 round-trip OK", nil
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

func defaultDoctorProbeMCPBinary(_ context.Context, cfg *config.Config) (string, error) {
	bin := strings.TrimSpace(cfg.Neo4j.MCPBinary)
	if bin == "" {
		return "", fmt.Errorf("AURA_MCP_NEO4J_CYPHER_BIN is empty")
	}
	path, err := doctorLookPath(bin)
	if err != nil {
		return "", fmt.Errorf("%s not found on PATH: %w", bin, err)
	}
	return "found " + path, nil
}

// defaultDoctorProbeMCPServers is the 6th doctor check (D-16/D-17/D-18, MCPH-09):
// it live-probes ONLY the enabled + runnable + streamable-HTTP managed MCP servers
// via mcp.ProbeServer, bounded per-server by AURA_MCP_PROBE_TIMEOUT. It deliberately
// does NOT probe doctorProbeMCPBinary's target (the unrelated Neo4j-Cypher sidecar
// binary, RESEARCH Pitfall #11) and does NOT dial disabled, trust-blocked, or stdio
// servers: RunnableManagedServers already filters out disabled/blocked (a server the
// operator turned off or hasn't approved is never dialed here either), and the
// streamable_http filter below drops stdio (D-18 scopes this check to the
// dead-HTTP-endpoint problem the CLI/status surfaces share). A single unreachable
// server fails only its own name in the aggregated detail, never the others
// (isolation) — the managed config itself is loaded fresh, independent of cfg.
func defaultDoctorProbeMCPServers(ctx context.Context, _ *config.Config) (string, error) {
	doc, _, err := loadManagedMCPConfig()
	if err != nil {
		return "", err
	}
	runnable, err := mcpmanager.RunnableManagedServers(doc)
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

func doctorProbeLLMKey(context.Context, *config.Config) (string, error) {
	if strings.TrimSpace(doctorLookupLLMKey()) == "" {
		return "not configured (keyless boot allowed; agent calls return llm_not_configured)", nil
	}
	return "configured", nil
}
