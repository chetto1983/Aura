// agent subcommand dispatcher for `aura agent {dry-run}`. Lives in package main
// alongside cmd/aura/main.go's switch case "agent", mirroring db.go/neo4j.go.
//
// `aura agent dry-run` is the operator-facing cornerstone proof (SC#4): it drives
// a mock LoopAgent over agenttest.InfiniteToolCallAgent through the real Budget
// tree and prints one Event per JSON line. Every line carries the same UUIDv7
// request_id (OTel-compatible run correlation). Flag precedence is CLI > env >
// builtin default (D-06): a numeric flag left at the -1 sentinel falls through to
// NewBudgetFromEnv, a non--1 flag overrides it.
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

	sub := &agenttest.InfiniteToolCallAgent{AgentName: "infinite", ToolName: dryRunToolName, ToolArgs: "{}"}
	root := workflow.NewLoop("dry-run", 0, sub) // maxIter=0 → only the budget stops it

	ic := agent.InvocationContext{
		Ctx:       context.Background(),
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

// buildBudget applies the CLI > env > default precedence (D-06): each non--1 flag
// is injected into the environment before NewBudgetFromEnv reads it, so the CLI
// value wins over an exported env value which wins over the builtin default. The
// dry-run tool is always exempt from dedup so the run terminates on max_steps
// (SC#2), preserving any operator-set exemptions.
func buildBudget(cfg dryRunConfig) (*agent.Budget, error) {
	restore := overrideEnv(map[string]int{
		"AURA_LOOP_MAX_STEPS":         cfg.maxSteps,
		"AURA_LOOP_MAX_WALLCLOCK_SEC": cfg.maxWallclockSec,
		"AURA_LOOP_DEDUP_WINDOW":      cfg.dedupWindow,
	})
	defer restore()

	prev, had := os.LookupEnv("AURA_LOOP_DEDUP_EXEMPT_TOOLS")
	exempt := dryRunToolName
	if had && prev != "" {
		exempt = prev + "," + dryRunToolName
	}
	_ = os.Setenv("AURA_LOOP_DEDUP_EXEMPT_TOOLS", exempt)
	defer func() {
		if had {
			_ = os.Setenv("AURA_LOOP_DEDUP_EXEMPT_TOOLS", prev)
		} else {
			_ = os.Unsetenv("AURA_LOOP_DEDUP_EXEMPT_TOOLS")
		}
	}()

	return agent.NewBudgetFromEnv()
}

// overrideEnv sets each AURA_LOOP_* key whose flag value is non--1 and returns a
// restore func that reinstates the prior environment. -1 flags are left untouched
// so env/default precedence (D-06) is preserved.
func overrideEnv(vals map[string]int) func() {
	type prevState struct {
		val string
		had bool
	}
	saved := make(map[string]prevState, len(vals))
	for k, v := range vals {
		if v == -1 {
			continue
		}
		prev, had := os.LookupEnv(k)
		saved[k] = prevState{val: prev, had: had}
		_ = os.Setenv(k, fmt.Sprintf("%d", v))
	}
	return func() {
		for k, st := range saved {
			if st.had {
				_ = os.Setenv(k, st.val)
			} else {
				_ = os.Unsetenv(k)
			}
		}
	}
}
