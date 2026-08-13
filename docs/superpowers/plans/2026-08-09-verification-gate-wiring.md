# Plan — wire the verification ledger into the turn

The ledger landed as a component in commit `ea88ff0` (four Go modules ported from
NousResearch/hermes-agent, migration 0094, all green, nothing wired). This plan connects
it: the writes that fill it, and the gate that reads it.

## Why this exists

Measured on the live stack on 2026-08-09, driving the real agent over 40 turns: the one
abstention error went the WRONG way — a confident refusal on a question the corpus
answered — and in another case the agent answered from the web while the gold document
said it verbatim. Both are read-only turns.

Aura already has a completion gate (`llm_agent_completion.go`, amendment #54 / D-43), and
it **structurally cannot catch these**: it fires only when `a.sideEffected`, and it spends
an LLM critic call to decide. The ported gate is deterministic, costs no model call, and
fires on evidence the turn already produced.

## Global constraints

These bind every task. They are the project's, not this plan's invention.

1. **No file over 600 LOC.** `internal/agent/llm_agent.go` is at 555. A change that would
   push it over must land in a new `llm_agent_<concern>.go`, which is the convention that
   file's own header already follows.
2. **Fail open, always.** A ledger read or write that errors must never abort or wedge a
   turn. The existing completion gate documents this rule for itself ("a broken/empty/
   unparseable critic fails OPEN so a verifier outage can never wedge a turn"); this gate
   inherits it.
3. **Bounded.** The nudge must be issued at most `MaxAttempts` times per run, counted on
   the agent, exactly as `completionAttempts` bounds the existing gate to one.
4. **No new dependency, no new tool, no change to any tool's behaviour.** Everything
   needed is already on `tools.ToolResult.Meta` and `llm.ToolCall.Function.Arguments`.
5. **Post-edit validation is mandatory** and must be run in WSL, not Windows:
   `go vet ./internal/agent/`, `go build ./internal/...`,
   `go test ./internal/agent/ -count=1`, `golangci-lint run internal/agent/...`.
   Windows-only failures in this repo are environment artifacts; WSL is the truth.
6. **Never modify a test to make it pass** unless the test itself encodes the wrong
   contract, and then say so explicitly in the commit message.

## Interfaces that already exist — do not redesign them

```go
// internal/agent/verification_stop.go
func BuildVerifyOnStopNudge(request VerifyOnStopRequest) (string, bool)
type VerifyOnStopRequest struct {
    Ledger VerificationLedger; SessionID string; ChangedPaths []string
    Attempts, MaxAttempts int; Guidance, TempDir string
}
type VerificationLedger interface {
    ProjectFactsFor(cwd string) ProjectFacts
    VerificationStatusFor(sessionID, cwd string) VerificationStatus
}
func verifyOnStopEnabled(configured string) bool   // env AURA_AGENT_VERIFY_ON_STOP_ENABLED wins

// internal/agent/verification_evidence_store.go
func NewEvidenceStore(pool *pgxpool.Pool) *EvidenceStore   // nil pool -> nil store, every method a no-op
type LedgerAdapter struct { Detector FilesystemProjectDetector; Store *EvidenceStore; IdentityID string }
// LedgerAdapter implements VerificationLedger.

// internal/agent/verification_hook.go
type VerificationHook struct { Store *EvidenceStore; Detector FilesystemProjectDetector; IdentityID, SessionID string }
// implements agent.Hook: AfterTool records shell results and marks edits.
```

## Task 1 — the gate at the content-stop seam

**File:** a NEW `internal/agent/llm_agent_verification.go`, plus the minimum edit to
`internal/agent/llm_agent.go` and `internal/agent/llm_agent_construct.go`.

**Where.** `internal/agent/llm_agent.go:490` is the voluntary-termination seam:

```go
if veto, feedback := a.gateCompletion(ic, answer); veto {
    a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: feedback})
    continue
}
```

**What.** Add a second gate immediately BEFORE that one, with the same shape:

```go
if nudge, ok := a.gateVerification(ic); ok {
    a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: nudge})
    continue
}
```

Order matters and is not arbitrary: the verification gate is free (no model call), so it
must run before the gate that costs one. Put the reason in a comment.

**`gateVerification` must:**

- return `("", false)` immediately when `a.ledger == nil`, when
  `verifyOnStopEnabled(a.cfg.VerifyOnStop)` is false, or when
  `a.verificationAttempts >= verificationMaxAttempts`.
- call `BuildVerifyOnStopNudge` with `Ledger: a.ledger`, `SessionID: a.sessionID`,
  `ChangedPaths: a.turnEditedPaths()`, `Attempts: a.verificationAttempts`,
  `MaxAttempts: verificationMaxAttempts`.
- on a nudge, increment `a.verificationAttempts` and return it.
- `verificationMaxAttempts` is **2** — the value the source's `build_verify_on_stop_nudge`
  defaults to.

**The changed paths.** `LlmAgent` must accumulate, per run, the paths the write tools
touched, so the gate knows what to check. The tool names and their path argument are
already listed in `writeToolPathArgs` (verification_hook.go) — reuse that map, do not
duplicate it. Record them where the loop already sees every dispatched call; find that
place rather than adding a second interception.

**New `LlmAgent` fields:** `ledger VerificationLedger`, `verificationAttempts int`, and
the edited-path accumulator. Add `VerifyOnStop string` to the agent config struct that
already carries `CompletionGate`, defaulting to `""` (which `verifyOnStopEnabled` reads
as the surface-aware default: on).

**Tests** in `internal/agent/llm_agent_verification_test.go`, using a fake
`VerificationLedger` (there is one to copy in `verification_stop_test.go`):

1. a nil ledger never nudges
2. a turn with no edited paths never nudges
3. an edited path in an unverified project nudges once, and the nudge lands in history as
   a `RoleUser` message and the loop continues
4. the second attempt is refused — `verificationAttempts` is respected
5. `AURA_AGENT_VERIFY_ON_STOP_ENABLED=0` disables it entirely
6. a ledger whose status is `passed` does not nudge

## Task 2 — construct the ledger and register the hook

**Files:** wherever the agent is constructed and hooks are registered. Start from
`internal/agent/llm_agent_construct.go` and `cmd/aura/chat_boot.go`; `grep -rn
"NewHookManager\|HookManager{" --include=*.go` finds the registration sites. There may be
more than one (CLI boot and server boot); wire **every** site that builds a real agent, or
say in your report which you did not and why.

**What.**

- Build `NewEvidenceStore(pool)` once per process, not per turn: it holds a pool.
- Build a `LedgerAdapter{Detector: FilesystemProjectDetector{}, Store: store, IdentityID: <the turn's identity>}`
  and give it to the agent as its `ledger`.
- Register `&VerificationHook{Store: store, IdentityID: …, SessionID: …}` on the
  HookManager with **FailOpen** — a ledger write must never abort a turn. Read
  `hooks.go`'s FailPolicy doc before choosing; `Register` defaults to FailClosed and that
  default is wrong here.
- When the pool is nil (tests, standalone), `NewEvidenceStore` returns nil and everything
  downstream must degrade to "no gate", not to a panic. Assert this.

**Tests:** a constructor-level test that a real agent built with a nil pool has a nil or
inert ledger and never nudges, and one that the hook is registered exactly once with
FailOpen.

## Out of scope

Do not touch: the retrieval/abstention work, `internal/documents`, the ingest sidecar, any
tool implementation, or the existing completion critic. Do not add configuration beyond
the single `VerifyOnStop` field named above.
