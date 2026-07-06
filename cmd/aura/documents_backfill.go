package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/jackc/pgx/v5/pgxpool"
)

const documentsUsage = "usage: aura documents backfill [--operator <identity-uuid>]"

// backfillDeps carries the live Postgres pool + Neo4j client the backfill needs, plus a
// closer that releases both. It is produced by a factory so the CLI dispatch can be unit
// tested with fakes (no live stack) while the default factory opens the real stores.
type backfillDeps struct {
	pool     *pgxpool.Pool
	graph    documents.KnowledgeClient
	closeFn  func()
	operator string
	// owners overrides the Postgres owner-map load (unit-test seam). When nil the default
	// documents.LoadDocumentOwners over the live pool is used.
	owners func(context.Context) ([]documents.DocumentOwner, error)
}

type backfillFactory func(context.Context) (*backfillDeps, error)

// runDocuments dispatches `aura documents <sub>`. Today only `backfill` exists (D-12): the
// idempotent Neo4j owner-edge backfill the rollout runbook runs BEFORE flipping
// AURA_MUSR_ISOLATION on. Kept separate from `aura docs` (retrieval/ingest) because it is an
// operator rollout step, not a per-document operation.
func runDocuments(args []string) {
	if err := runDocumentsCommand(context.Background(), args, os.Stdout, newBackfillDeps); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runDocumentsCommand(ctx context.Context, args []string, out io.Writer, factory backfillFactory) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", documentsUsage)
	}
	switch args[0] {
	case "backfill":
		return documentsBackfill(ctx, args[1:], out, factory)
	default:
		return fmt.Errorf("unknown documents command %q\n%s", args[0], documentsUsage)
	}
}

func documentsBackfill(ctx context.Context, args []string, out io.Writer, factory backfillFactory) error {
	fs := flag.NewFlagSet("documents backfill", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	operator := fs.String("operator", "", "identity uuid to attach orphan documents to (default: seeded local identity)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	deps, err := factory(ctx)
	if err != nil {
		return err
	}
	defer deps.closeFn()

	load := deps.owners
	if load == nil {
		load = func(ctx context.Context) ([]documents.DocumentOwner, error) {
			return documents.LoadDocumentOwners(ctx, deps.pool)
		}
	}
	owners, err := load(ctx)
	if err != nil {
		return err
	}
	op := *operator
	if op == "" {
		op = deps.operator
	}
	backfiller := &documents.Backfiller{Graph: deps.graph, Operator: op}
	report, err := backfiller.Run(ctx, owners)
	if err != nil {
		return err
	}
	return writeJSON(out, map[string]any{
		"owners_sourced":               report.OwnersSourced,
		"edges_from_map":               report.EdgesFromMap,
		"orphans_attached_to_operator": report.OrphansAttached,
	})
}

// newBackfillDeps opens the real Postgres pool + Neo4j client (mirrors newDocsService).
func newBackfillDeps(ctx context.Context) (*backfillDeps, error) {
	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		return nil, err
	}
	mcp, err := knowledge.Open(ctx, &cfg.Neo4j)
	if err != nil {
		pool.Close()
		return nil, err
	}
	return &backfillDeps{
		pool:  pool,
		graph: mcp,
		closeFn: func() {
			_ = mcp.Close()
			pool.Close()
		},
	}, nil
}
