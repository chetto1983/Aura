//go:build db_integration

// The verify-on-stop wiring against a real ledger.
//
// The unit tier can only prove the DISABLED path, because a non-nil EvidenceStore
// needs a pool. This is the other half, and it is the one that matters: with a store
// injected through Deps, a turn gets a ledger that reaches Postgres and a hook that
// writes to the SAME ledger, exactly once.
//
// Requires a running Postgres with the migrations applied (see integration_helpers_test.go).
//
//	go test -tags db_integration ./internal/runner -run Verification -count=1
package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// runnerWithLedger builds a Runner the way the composition root does: the store is
// injected through Deps so New's copy is what the test exercises, not a field poked
// in afterwards.
func runnerWithLedger(t *testing.T, pool *pgxpool.Pool) *Runner {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	return New(Deps{
		Identity:          newFakeIdentityStore(),
		CacheMetrics:      newFakeCacheMetricStore(),
		ToolInvocations:   newFakeToolInvocationStore(),
		Client:            agenttest.NewFakeClient(),
		Registry:          reg,
		LLM:               llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		VerificationStore: agent.NewEvidenceStore(pool),
	})
}

// verifiableProject creates a directory the FilesystemProjectDetector recognises AND
// can name a canonical command for. The Makefile is load-bearing: detectVerifyCommands
// reads run_tests.sh, package.json scripts, pytest and Makefile targets, so a bare
// go.mod project detects no command at all and only a disposable ad-hoc script could
// ever classify as evidence there.
func verifiableProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n"), 0o600); err != nil {
		t.Fatalf("write project marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("test:\n\tgo test ./...\n"), 0o600); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}
	return root
}

func shellExecCall(command string) llm.ToolCall {
	call := llm.ToolCall{ID: "c1", Type: "function"}
	call.Function.Name = "shell_exec"
	call.Function.Arguments = `{"command":` + strconv.Quote(command) + `}`
	return call
}

func TestVerificationLedgerIsLiveForAScopedTurn(t *testing.T) {
	pool := migratedRunnerPool(t)
	seedLocalIdentity(t, pool)
	r := runnerWithLedger(t, pool)

	ledger := r.verificationLedger(ownerCtx())
	if ledger == nil {
		t.Fatal("a runner with a store and a scoped identity must hand the turn a ledger")
	}
	root := verifiableProject(t)
	if facts := ledger.ProjectFactsFor(root); !facts.Found || facts.Root != root {
		t.Fatalf("project facts = %+v, want the detector to find %q", facts, root)
	}
	// A real read against Postgres, not a stub: an untouched workspace is unverified.
	status := ledger.VerificationStatusFor("session-"+uuid.Must(uuid.NewV7()).String(), root)
	if status.Status != agent.StatusUnverified {
		t.Fatalf("status = %q, want %q", status.Status, agent.StatusUnverified)
	}

	// The unscoped context is the other branch, and it needs a live store to be a
	// distinct one: a store is present, an identity is not, so there is still no gate.
	if unscoped := r.verificationLedger(context.Background()); unscoped != nil {
		t.Fatalf("ledger = %#v for an unscoped context, want nil", unscoped)
	}
}

func TestVerificationHookWritesTheLedgerTheGateReads(t *testing.T) {
	// End to end across the two halves: the hook the runner registers records a
	// finished command, and the ledger the runner hands the gate reads it back as
	// proof. If the two were wired to different stores, or the hook were registered
	// twice, or not at all, this is where it shows.
	pool := migratedRunnerPool(t)
	seedLocalIdentity(t, pool)
	r := runnerWithLedger(t, pool)

	root := verifiableProject(t)
	sessionID := "session-" + uuid.Must(uuid.NewV7()).String()
	// Scoped, like every other write in this suite: migration 0094's RLS is fail-closed,
	// so a bare pool statement neither sees nor deletes these rows.
	t.Cleanup(func() {
		_ = db.WithIdentityTxRaw(context.Background(), pool, localIdentityID, func(tx pgx.Tx) error {
			for _, table := range []string{"aura.verification_events", "aura.verification_state"} {
				if _, err := tx.Exec(context.Background(),
					`DELETE FROM `+table+` WHERE session_id = $1`, sessionID); err != nil {
					return err
				}
			}
			return nil
		})
	})

	hooks := r.verificationHooks(ownerCtx(), sessionID)
	if hooks == r.hookManager {
		t.Fatal("a scoped turn with a store must get its own hook manager, not the shared one")
	}

	meta := tools.ToolResultMeta{"cwd": root, "exit_code": 0}
	if _, err := hooks.AfterTool(ownerCtx(),
		shellExecCall("make test"),
		tools.ToolResult{Preview: "ok\n", Meta: &meta}); err != nil {
		t.Fatalf("AfterTool must never abort the turn: %v", err)
	}

	ledger := r.verificationLedger(ownerCtx())
	if status := ledger.VerificationStatusFor(sessionID, root); status.Status != agent.StatusPassed {
		t.Fatalf("status = %q, want %q: the hook's write must be what the gate reads",
			status.Status, agent.StatusPassed)
	}

	var events int
	if err := db.WithIdentityTxRaw(context.Background(), pool, localIdentityID, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT count(*) FROM aura.verification_events WHERE session_id = $1 AND root = $2`,
			sessionID, root).Scan(&events)
	}); err != nil {
		t.Fatalf("count verification events: %v", err)
	}
	if events != 1 {
		t.Fatalf("recorded %d events for one command, want 1 — the hook is registered %d times",
			events, events)
	}
}

// fakeWriteTool is named write_file so the loop's edited-path accumulator recognises
// it (agent's writeToolPathArgs), without routing to a sandbox container.
type fakeWriteTool struct{}

func (fakeWriteTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "write_file",
		Summary:     "Fake write tool.",
		Description: "Fake write tool.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		Mutating:    true,
	}
}

func (fakeWriteTool) Execute(ctx context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	return tools.NewResult(ctx, "wrote:"+string(raw))
}

// fakeShellTool is named shell_exec and publishes the cwd + exit_code Meta the real
// one publishes, which is the only part of it the ledger reads. It runs nothing: the
// production tool executes inside the per-identity box, and this suite has no box.
type fakeShellTool struct{ cwd string }

func (fakeShellTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "shell_exec",
		Summary:     "Fake shell tool.",
		Description: "Fake shell tool.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`),
		Mutating:    true,
	}
}

func (s fakeShellTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	res, err := tools.NewResult(ctx, "ok\n")
	if err != nil {
		return tools.ToolResult{}, err
	}
	meta := tools.ToolResultMeta{"cwd": s.cwd, "exit_code": 0}
	res.Meta = &meta
	return res, nil
}

// TestVerifyOnStopFiresOnARealTurn is the claim the whole wiring exists to support:
// an ordinary turn, through Runner.Turn, over a REAL Postgres ledger, that edits code
// and tries to finish is SENT BACK, and is accepted once it has actually verified.
// Everything else in this file tests a seam; this tests the behaviour, and it fails if
// any link in the chain — hook, store, adapter, gate — is not wired.
func TestVerifyOnStopFiresOnARealTurn(t *testing.T) {
	pool := migratedRunnerPool(t)
	seedLocalIdentity(t, pool)

	root := verifiableProject(t)
	edited := filepath.Join(root, "app.go")

	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(fakeWriteTool{})
	reg.Register(fakeShellTool{cwd: root})
	convStore := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})

	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("w1", "write_file",
			`{"path":`+strconv.Quote(edited)+`,"content":"package main"}`)),
		agenttest.ToolCallTurn(textResponseCall("t1", "Done, all good.")),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("s1", "shell_exec", `{"command":"make test"}`)),
		agenttest.ToolCallTurn(textResponseCall("t2", "Ran make test, it passed.")),
	)
	r := New(Deps{
		Conv:              convStore,
		Pause:             askuser.New(pool),
		Identity:          newFakeIdentityStore(),
		CacheMetrics:      newFakeCacheMetricStore(),
		ToolInvocations:   newFakeToolInvocationStore(),
		Client:            client,
		Registry:          reg,
		LLM:               llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		TitleTimeout:      2 * time.Second,
		StopTimeout:       2 * time.Second,
		VerificationStore: agent.NewEvidenceStore(pool),
	})
	// The auto-title worker fires past seq>=3; join it so goleak does not see it.
	t.Cleanup(func() { r.waitWorkers(5 * time.Second) })

	convID := newIntegrationConversation(t, pool, convStore)
	t.Cleanup(func() {
		_ = db.WithIdentityTxRaw(context.Background(), pool, localIdentityID, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(),
				`DELETE FROM aura.verification_state WHERE session_id = $1`, convID)
			return err
		})
	})

	evs, err := drain(r.Turn(ownerCtx(), convID, new("edit app.go")))
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("turn produced no events")
	}

	// Four model rounds, not two: the first termination was refused, the agent went
	// and ran the suite, and only then was allowed to finish.
	if client.CallCount() != 4 {
		t.Fatalf("model called %d times, want 4 — the gate did not send the turn back", client.CallCount())
	}

	// The refusal is visible to the caller, not just inferable from the round count.
	// The nudge itself rides the AGENT's history as a tool result and is never
	// persisted as a conversation turn, so the Event stream is where it surfaces.
	nudged := false
	for _, ev := range evs {
		if ev != nil && ev.LLMResponse != nil && strings.Contains(ev.LLMResponse.Content, "verification gate") {
			nudged = true
		}
	}
	if !nudged {
		t.Fatal("no verification-gate event: the extra round was spent on something else")
	}

	// And the outcome is durable, not merely narrated at the model: the workspace this
	// turn edited and then verified is recorded as passed.
	ledger := r.verificationLedger(ownerCtx())
	if status := ledger.VerificationStatusFor(convID, root); status.Status != agent.StatusPassed {
		t.Fatalf("ledger status = %q, want %q after the turn ran its suite", status.Status, agent.StatusPassed)
	}
}
