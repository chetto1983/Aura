//go:build agent_eval

// The behaviour gate, run against the live deployment.
//
// It carries a build tag and is NOT part of any CI job, for one reason: every case
// is a real turn against a real model and costs real money. It is operator-invoked
// — `make agent-eval` — and it is the only thing in this repository that can tell
// whether Aura still answers the question. Everything else gates the code.
//
// Only THIS file is tagged. case.go, cases.go and probe.go compile and vet on every
// build, and their logic is unit-tested without a container, so the gate cannot rot
// unnoticed the way arcadedb_integration did: ten files behind a tag nothing runs,
// not even the compiler.
package agenteval

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestBehaviourGate runs every case against the running stack and reports each
// one's violations. Cases run in sequence, not in parallel: they share one
// deployment, one sandbox and one document library, and a flaky gate is a gate
// people learn to ignore.
func TestBehaviourGate(t *testing.T) {
	identity := strings.TrimSpace(os.Getenv("AURA_EVAL_IDENTITY"))
	if identity == "" {
		t.Fatal("AURA_EVAL_IDENTITY is required: it owns the conversations the gate reads back, " +
			"and without it the fail-closed RLS read returns zero rows — which would pass every case " +
			"having measured nothing")
	}
	driver := CLIDriver{Container: os.Getenv("AURA_EVAL_CONTAINER")}
	reader := PostgresEvidence{
		Container:  os.Getenv("AURA_EVAL_PG_CONTAINER"),
		IdentityID: identity,
	}

	for _, c := range Cases() {
		t.Run(c.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			started := time.Now()
			evidence, failures, err := Run(ctx, driver, reader, c)
			if err != nil {
				t.Fatalf("%v", err)
			}
			// Logged on pass as well as failure: the numbers are the point. A case
			// that still passes at nine calls where it used to take five is the
			// regression this gate exists to make visible before it becomes a
			// complaint.
			t.Logf("%s: %d LLM calls in %s — tools: %s",
				c.Name, evidence.LLMCalls, time.Since(started).Round(time.Millisecond),
				strings.Join(evidence.Tools, " → "))
			for _, f := range failures {
				t.Error(f)
			}
		})
	}
}
