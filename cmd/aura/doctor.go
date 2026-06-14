package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
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
	doctorProbePostgres  doctorProbe = defaultDoctorProbePostgres
	doctorProbeNeo4j     doctorProbe = defaultDoctorProbeNeo4j
	doctorProbeEmbed     doctorProbe = defaultDoctorProbeEmbed
	doctorProbeMCPBinary doctorProbe = defaultDoctorProbeMCPBinary
	doctorLookupLLMKey               = func() string { return os.Getenv("OPENROUTER_API_KEY") } //nolint:gosec // boolean presence check only; value is never printed.
	doctorLookPath                   = exec.LookPath
	doctorHTTPClient                 = &http.Client{Timeout: 10 * time.Second}
	doctorOpenPostgres               = func(ctx context.Context, cfg *config.Config) (doctorPostgresPool, error) {
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
	embedder := &documents.EmbeddingClient{
		BaseURL:    cfg.Neo4j.EmbedURL,
		Client:     client,
		Dimensions: cfg.Neo4j.EmbedDimensions,
	}
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

func doctorProbeLLMKey(context.Context, *config.Config) (string, error) {
	if strings.TrimSpace(doctorLookupLLMKey()) == "" {
		return "not configured (keyless boot allowed; agent calls return llm_not_configured)", nil
	}
	return "configured", nil
}
