// agent subcommand dispatcher for `aura agent {dry-run}`. Lives in package main
// alongside cmd/aura/main.go's switch case "agent", mirroring db.go/neo4j.go.
//
// `aura agent dry-run` is the operator-facing cornerstone proof (SC#4): it drives
// a mock LoopAgent over agenttest.InfiniteToolCallAgent through the real Budget
// tree and prints one Event per JSON line. Every line carries the same UUIDv7
// request_id (OTel-compatible run correlation). Flag precedence is CLI > env >
// builtin default (D-06): a numeric flag left at the -1 sentinel falls through to
// env/default inside agent.NewBudget, a non--1 flag overrides it. The flag values
// are passed through agent.BudgetOptions, never injected into the process env.
//
// W7: Events are serialized via json.NewEncoder(w).SetEscapeHTML(false), honoring
// Event.MarshalJSON — the SINGLE user-facing serialization path. canonicaljson is
// for hashing/dedup only and never appears on this print path.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/workflow"
	"github.com/google/uuid"
)

// dryRunToolName is the constant tool the InfiniteToolCallAgent repeats forever.
// It is added to AURA_LOOP_DEDUP_EXEMPT_TOOLS so the run terminates on the hard
// max_steps cap (SC#2 26-line contract), NOT on the dedup veto — the fixture
// emits a constant result, which would otherwise trip period-1 dedup at window=3.
const dryRunToolName = "noop"

// dryRunConfig holds the parsed `dry-run` flags. The numeric fields use a -1
// sentinel meaning "unset → fall through to NewBudgetFromEnv" (D-06).
type dryRunConfig struct {
	requestID       string // "auto" → uuid.NewV7(); else a literal UUID parsed verbatim
	maxSteps        int    // -1 = unset
	maxWallclockSec int    // -1 = unset
	dedupWindow     int    // -1 = unset
}

func runAgent(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aura agent {dry-run}")
		os.Exit(1)
	}
	switch args[0] {
	case "dry-run":
		cfg, err := parseDryRunArgs(args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if err := dryRun(cfg, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: aura agent {dry-run}")
		os.Exit(1)
	}
}

// parseDryRunArgs parses the dry-run flag set. Numeric flags default to -1
// (D-06 sentinel: fall through to env/default); --request-id defaults to "auto".
func parseDryRunArgs(args []string) (dryRunConfig, error) {
	fs := flag.NewFlagSet("dry-run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	cfg := dryRunConfig{}
	fs.StringVar(&cfg.requestID, "request-id", "auto", "run correlation id: 'auto' (UUIDv7) or a literal UUID")
	fs.IntVar(&cfg.maxSteps, "max-steps", -1, "override AURA_LOOP_MAX_STEPS (-1 = use env/default)")
	fs.IntVar(&cfg.maxWallclockSec, "max-wallclock-sec", -1, "override AURA_LOOP_MAX_WALLCLOCK_SEC (-1 = use env/default)")
	fs.IntVar(&cfg.dedupWindow, "dedup-window", -1, "override AURA_LOOP_DEDUP_WINDOW (-1 = use env/default)")
	if err := fs.Parse(args); err != nil {
		return dryRunConfig{}, err
	}
	return cfg, nil
}

// dryRun builds the Budget (CLI > env > default, D-06), mints/parses the request
// id, drives a mock LoopAgent over InfiniteToolCallAgent, and prints each Event as
// one JSON line through Event.MarshalJSON (W7). It returns an error only on real
// failures (malformed flags/env or a yielded iterator error, D-04); natural budget
// termination is an Event, not an error.
func dryRun(cfg dryRunConfig, w io.Writer) error {
	requestID, err := resolveRequestID(cfg.requestID)
	if err != nil {
		return err
	}

	budget, err := buildBudget(cfg)
	if err != nil {
		return err
	}
	runCtx, cancel := budget.WithDeadline(context.Background())
	defer cancel()

	sub := &agenttest.InfiniteToolCallAgent{AgentName: "infinite", ToolName: dryRunToolName, ToolArgs: "{}"}
	root := workflow.NewLoop("dry-run", 0, sub) // maxIter=0 → only the budget stops it

	// SpanID/ParentSpanID are intentionally left at their zero value here: per-node
	// span minting is DEFERRED to the future OTel-integration slice (WR-04). The
	// dry-run therefore emits the constant "span_id":"0000000000000000" on every line
	// by design — see internal/agent/event.go for the deferral rationale.
	ic := agent.InvocationContext{
		Ctx:       runCtx,
		Agent:     root,
		RequestID: requestID,
		Branch:    "root",
		Budget:    budget,
	}

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // W7: single user-facing path, honors Event.MarshalJSON

	for ev, runErr := range root.Run(ic) {
		if runErr != nil {
			return fmt.Errorf("dry-run: %w", runErr)
		}
		if ev == nil {
			continue
		}
		// KEEP (QUAL-02 triage T1, load-bearing): the fake InfiniteToolCallAgent builds
		// its step events with NO RequestID (agenttest/mocks.go) — a real LlmAgent.newEvent
		// copies ic.RequestID, the fake does not. scopeToToolCall returns those single-tool
		// step events unchanged, so this re-stamp is the UNIFORM run-id source on the dry-run
		// path (only the LoopAgent terminal event already carries ic.RequestID). Remove it and
		// every step line emits the zero UUID, breaking SC#4 run correlation — pinned by
		// TestDryRun_EveryEventCarriesRequestID_LoadBearing.
		ev.RequestID = requestID // stamp the shared run id on every emitted Event (SC#4)
		if err := enc.Encode(ev); err != nil {
			return fmt.Errorf("encode event: %w", err)
		}
	}
	return nil
}

// resolveRequestID returns a UUIDv7 for "auto", else parses a literal UUID
// verbatim (smoke reproducibility, SC#4).
func resolveRequestID(s string) (uuid.UUID, error) {
	if s == "auto" {
		id, err := uuid.NewV7()
		if err != nil {
			return uuid.Nil, fmt.Errorf("mint request id: %w", err)
		}
		return id, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("--request-id %q: not a valid UUID", s)
	}
	return id, nil
}

// buildBudget applies the CLI > env > default precedence (D-06) by passing the
// resolved flag values DIRECTLY into agent.NewBudget — no process-global env
// mutation (WR-04). Each non--1 flag becomes an explicit BudgetOptions override
// that wins over env then default; a -1 flag stays nil so NewBudget falls through
// to env/default. The dry-run tool is always added to the dedup exempt set so the
// run terminates on max_steps (SC#2), preserving any operator-set exemptions.
func buildBudget(cfg dryRunConfig) (*agent.Budget, error) {
	return agent.NewBudget(agent.BudgetOptions{
		MaxSteps:        overrideInt(cfg.maxSteps),
		MaxWallclockSec: overrideInt(cfg.maxWallclockSec),
		DedupWindow:     overrideInt(cfg.dedupWindow),
		ExemptTools:     agent.ExemptToolsFromEnv(dryRunToolName),
	})
}

// overrideInt maps the -1 "unset" sentinel to a nil override (fall through to
// env/default, D-06) and any other value to an explicit override pointer.
func overrideInt(v int) *int {
	if v == -1 {
		return nil
	}
	return &v
}
