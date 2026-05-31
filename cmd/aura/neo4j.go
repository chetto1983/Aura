// neo4j subcommand dispatcher for `aura neo4j {migrate|ping|status|reset|cypher}`.
// Lives in package main alongside main.go's switch case "neo4j". Each branch
// loads config, opens what it needs (Postgres pool for the audit trail; an MCP
// subprocess client for Cypher), prints a human-readable line, exits non-zero on
// error. The `cypher` branch is the raw escape hatch the smoke harness drives.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/knowledge"
)

func runNeo4j(args []string) {
	if len(args) < 1 {
		neo4jUsage()
		os.Exit(1)
	}
	// Neo4j-admin commands are graph operations — use the LLM-free config load so
	// `aura neo4j migrate` does not require OPENROUTER_API_KEY (e.g. CI's migrate step).
	cfg := config.LoadDB()
	ctx := context.Background()

	switch args[0] {
	case "migrate":
		neo4jMigrate(ctx, cfg)
	case "ping":
		neo4jPing(ctx, cfg)
	case "status":
		neo4jStatus(ctx, cfg)
	case "reset":
		neo4jReset(ctx, cfg, args[1:])
	case "cypher":
		neo4jCypher(ctx, cfg, args[1:])
	default:
		neo4jUsage()
		os.Exit(1)
	}
}

func neo4jUsage() {
	fmt.Fprintln(os.Stderr, "usage: aura neo4j {migrate|ping|status|reset|cypher {read|write} \"QUERY\" [--param k=v ...]}")
}

// openMCP spawns the mcp-neo4j-cypher subprocess from config; caller closes it.
func openMCP(ctx context.Context, cfg *config.Config) *knowledge.Client {
	mcp, err := knowledge.Open(ctx, &cfg.Neo4j)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return mcp
}

// openSchema dials Neo4j via the driver for schema DDL; caller closes it.
func openSchema(ctx context.Context, cfg *config.Config) *knowledge.SchemaExecutor {
	schema, err := knowledge.OpenSchema(ctx, &cfg.Neo4j)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return schema
}

func neo4jMigrate(ctx context.Context, cfg *config.Config) {
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()
	schema := openSchema(ctx, cfg)
	defer func() { _ = schema.Close(ctx) }()

	n, err := knowledge.Migrate(ctx, schema, pool)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if n == 0 {
		fmt.Println("ok: no pending migrations")
		return
	}
	fmt.Printf("ok: %d migration(s) applied\n", n)
}

func neo4jPing(ctx context.Context, cfg *config.Config) {
	mcp := openMCP(ctx, cfg)
	defer func() { _ = mcp.Close() }()
	res, err := knowledge.Ping(ctx, mcp, &cfg.Neo4j)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("ok: Neo4j %s + embed sidecar dim %d\n", res.ServerVersion, res.EmbedDim)
}

func neo4jStatus(ctx context.Context, cfg *config.Config) {
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()
	rows, err := knowledge.Status(ctx, pool)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(rows) == 0 {
		fmt.Println("ok: no knowledge migrations applied yet")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "VERSION\tNAME\tAPPLIED_AT")
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, "%d\t%s\t%s\n", r.Version, r.Name, r.AppliedAt.Format("2006-01-02 15:04:05"))
	}
	_ = w.Flush()
}

func neo4jReset(ctx context.Context, cfg *config.Config, extra []string) {
	if !slices.Contains(extra, "--yes") || os.Getenv("AURA_RESET_YES") != "1" {
		fmt.Fprintln(os.Stderr, "refusing — pass --yes AND set AURA_RESET_YES=1 to confirm destructive reset")
		os.Exit(1)
	}
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()
	schema := openSchema(ctx, cfg)
	defer func() { _ = schema.Close(ctx) }()
	if err := knowledge.Reset(ctx, schema, pool); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("ok: neo4j reset + re-migrated")
}

// neo4jCypher runs a raw read/write Cypher call: the smoke harness uses it to
// upsert chunks and run vector queries. Params arrive as repeated --param k=v;
// values that look like JSON (leading [ or {) are decoded so embeddings pass as
// numeric arrays, everything else stays a string.
func neo4jCypher(ctx context.Context, cfg *config.Config, args []string) {
	if len(args) < 2 {
		neo4jUsage()
		os.Exit(1)
	}
	write := args[0] == "write"
	if args[0] != "read" && args[0] != "write" {
		neo4jUsage()
		os.Exit(1)
	}
	query := args[1]
	params, err := parseParams(args[2:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mcp := openMCP(ctx, cfg)
	defer func() { _ = mcp.Close() }()
	raw, err := mcp.Cypher(ctx, query, params, write)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(raw))
}

// parseParams turns repeated `--param k=v` tokens into a params map.
func parseParams(args []string) (map[string]any, error) {
	params := map[string]any{}
	for i := 0; i < len(args); i++ {
		if args[i] != "--param" {
			return nil, fmt.Errorf("unexpected token %q (want --param k=v)", args[i])
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("--param requires a k=v argument")
		}
		k, v, found := strings.Cut(args[i+1], "=")
		if !found {
			return nil, fmt.Errorf("--param %q is not in k=v form", args[i+1])
		}
		params[k] = coerceParam(v)
		i++
	}
	return params, nil
}

// coerceParam decodes JSON-looking values (arrays/objects) and leaves plain
// scalars as strings so embeddings arrive as numeric lists.
func coerceParam(v string) any {
	trimmed := strings.TrimSpace(v)
	if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{") {
		var decoded any
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			return decoded
		}
	}
	return v
}
