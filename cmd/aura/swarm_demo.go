// swarm-demo subcommand for `aura swarm-demo`. Lives in package main alongside
// main.go's switch case "swarm-demo", mirroring agent.go's `aura agent dry-run`.
//
// `aura swarm-demo` is the operator-facing proof that the swarm engine fans goals
// out and collects an ordered report array end-to-end — WITHOUT a live LLM or any
// network call (D-16 convenience deliverable). It drives the real internal/swarm.Run
// engine over an agenttest.FakeClient mock-LLM fixture: every worker pops one
// content-stop turn returning a deterministic answer, so each goal yields a
// {status:ok} report in goal order. The result is printed as a JSON array, one
// run, deterministic, no OPENROUTER key required.
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
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/swarm"
)

// demoGoals is the fixed goals array the demo fans out when the operator passes no
// --goals override.
var demoGoals = []string{
	"Summarize the Q1 revenue figures",
	"List the top competitor launches this quarter",
	"Draft a one-line status update",
}

// demoWorkerAnswer is the deterministic content-stop answer every mock worker
// returns. It is goal-agnostic on purpose: the demo proves the fan-out + ordered
// collection, not per-goal reasoning (that is the live cot_eval tier, not this).
const demoWorkerAnswer = "done (mock worker)"

func runSwarmDemo(args []string) {
	fs := flag.NewFlagSet("swarm-demo", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	maxSteps := fs.Int("max-steps", 50, "budget step ceiling for the demo swarm")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	goals := demoGoals
	if rest := fs.Args(); len(rest) > 0 {
		goals = rest
	}
	if err := swarmDemo(os.Stdout, goals, *maxSteps); err != nil {
		fmt.Fprintln(os.Stderr, "swarm-demo:", err)
		os.Exit(1)
	}
}

// swarmDemo runs the engine over a FakeClient and writes the ordered []ChildReport
// as indented JSON to w. It returns an error only on a real failure (budget config
// or a marshal fault); the deterministic fixture never makes a network call.
func swarmDemo(w io.Writer, goals []string, maxSteps int) error {
	// One content-stop turn per worker, all identical, so worker completion order is
	// irrelevant: each pop returns the same answer and the engine orders reports by
	// goal index. Script a few extra turns so a worker that loops once still resolves.
	turns := make([]agenttest.FakeTurn, 0, len(goals)*2)
	for i := 0; i < len(goals)*2; i++ {
		turns = append(turns, agenttest.TextChunks("stop", demoWorkerAnswer))
	}
	client := agenttest.NewFakeClient(turns...)

	budget, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		return fmt.Errorf("budget config: %w", err)
	}

	// Per-invocation run dir, not a fixed os.TempDir()/swarm-demo path: two concurrent
	// demos (or a hostile co-tenant pre-creating the predictable path) would otherwise
	// collide, and the transcripts persist with no TTL sweep in the demo (WR-03).
	runDir, err := os.MkdirTemp("", "aura-swarm-demo-")
	if err != nil {
		return fmt.Errorf("swarm-demo run dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(runDir) }()

	rc := swarm.RunConfig{
		ParentBudget:   budget,
		ParentRegistry: buildBaseRegistry(&config.Config{MaxSwarmGoals: len(goals) + 1}, nil),
		Client:         client,
		LLM:            llm.Config{Model: "mock", Provider: "fake", TotalTimeoutSec: 30},
		Cfg: config.Config{
			RunDir:             runDir,
			ToolPreviewCap:     2048,
			MaxSwarmGoals:      len(goals) + 1,
			SwarmChildIdleSec:  30,
			MaxSwarmConcurrent: 4,
		},
		ConvID: "swarm-demo",
		Depth:  1,
	}

	out, err := swarm.Run(context.Background(), rc, goals)
	if err != nil {
		return fmt.Errorf("run swarm: %w", err)
	}

	var reports []swarm.ChildReport
	if err := json.Unmarshal([]byte(out), &reports); err != nil {
		return fmt.Errorf("decode reports: %w", err)
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(reports); err != nil {
		return fmt.Errorf("encode reports: %w", err)
	}
	return nil
}
