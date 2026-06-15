---
phase: 12-ag-ui-gateway
fixed_date: 2026-06-07
review_path: .planning/phases/12-ag-ui-gateway/12-REVIEW.md
fix_scope: critical_warning
findings_in_scope: 6
fixed: 6
skipped: 0
iteration: 1
status: all_fixed
---

# Phase 12: Code Review Fix Report

**Fixed at:** 2026-06-07
**Source review:** .planning/phases/12-ag-ui-gateway/12-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope (critical + warning): 6
- Fixed: 6
- Skipped: 0
- Out of scope (Info IN-01..IN-05): not touched

All six warnings (WR-01..WR-06) were fixed, each as one atomic `fix(12): …` commit
scoped to `internal/agui/` source + tests. No Critical findings existed. The five Info
findings were out of the `critical_warning` scope and were not touched.

## Validation evidence (full gate, run in isolated worktree at HEAD)

- `go vet ./...` — clean (no output)
- `go build ./...` — clean
- `go test ./internal/agui/ ./internal/agent/ ./cmd/aura/ -count=1` — all `ok`
  - `internal/agui` ok (0.93s), `internal/agent` ok (2.49s), `cmd/aura` ok (1.28s)
- `BASH_ENV=~/.aura-toolchain.sh go test -race ./internal/agui/ -count=1` — `ok` (2.17s)
- Pre-commit hooks (gofmt + vet + file-size ≤600 LOC) passed on every commit.
  - server.go 433 LOC, fanout.go 120 LOC after edits — both well under the 600 cap.

## Fixed Issues

### WR-01: SSE drop-on-full could drop the terminal RUN_FINISHED/RUN_ERROR frame

**Files modified:** `internal/agui/server.go`, `internal/agui/fanout.go`, `internal/agui/fanout_test.go`
**Commit:** 9bf17923
**Applied fix:** `pumpSend` (server SSE) and `send` (in-process fanout) applied the
drop-on-full policy uniformly to every event. Now run-lifecycle frames
(RUN_STARTED/RUN_FINISHED/RUN_ERROR), detected by a shared `isLifecycleFrame`, use a
bounded blocking send still abortable on ctx-cancel, so a terminal frame is never lost
under backpressure; intermediate deltas keep the drop-on-full, never-stall-the-Loop
behavior (T-12-09). The accepted tradeoff: a genuinely dead subscriber can stall the
producer until ctx-cancel.
**Tests:** `TestFanoutSlowSubscriberDropped` was reframed (the old never-read peer now
deadlocks the producer's blocking lifecycle send by design) to a single overflowing
subscriber that still receives RUN_FINISHED last and fewer-than-all deltas — pinning both
the drop-on-overflow of deltas AND the WR-01 terminal-frame survival. Cross-subscriber
non-back-pressure remains covered by `TestFanoutDistributesToAllSubscribers`.
Justification for editing the existing test: it asserted the old behavior (slow peer never
back-pressures even on the terminal frame) that the fix deliberately changes.
**Note (human verification):** WR-01 changes observable concurrency behavior — pinned by
the reframed/added tests and a clean `-race` run, but a human should confirm the blocking
tradeoff is acceptable for the eventual Telegram consumer.

### WR-02: Translator error path could yield after a prior yield returned false

**Files modified:** `internal/agui/translator.go`, `internal/agui/translator_test.go`
**Commit:** b6b33a83
**Applied fix:** The error branch discarded `closeRuns()`'s result (`_ = closeRuns()`)
then yielded RUN_ERROR unconditionally — a yield-after-false iter.Seq2 contract violation
that Go's range-over-func runtime panics on. Guarded it like every other branch:
`if !closeRuns() { return }`.
**Tests:** Added `TestTranslatorErrorPathHonorsConsumerStop` — a strict consumer that stops
on the close frame must not see RUN_ERROR after. Adversarially verified: reverting the
guard to `_ = closeRuns()` makes the test panic at `translator.go:37`; with the guard it
passes.

### WR-03: Secret redaction missed non-DSN credential URLs (bearer/basic-auth/token)

**Files modified:** `internal/agui/server.go`, `internal/agui/server_test.go`
**Commit:** 5b52d0ba
**Applied fix:** `secretPattern` only collapsed the five DB DSN schemes. `sanitizeString`
now runs three passes: the existing whole-DSN collapse (unchanged), a generic
`scheme://user:pass@` userinfo collapser across all URL schemes (keeps the rest of the URL
diagnosable), and a `bearer/api[_-]?key=/token=` token collapser. The DSN pass runs first
so the generic userinfo pass never double-matches the DB schemes; existing tests stay
green.
**Tests:** Extended `TestSanitizeErr` with https/http userinfo, bearer, api_key, and token
cases; existing `TestServer_RunErrorRedaction`, `TestServer_ResumeSubmitError`, and
`TestServer_MessagesLoadHistoryError` still pass.

### WR-04: MESSAGES_SNAPSHOT dropped assistant tool_calls from projected history

**Files modified:** `internal/agui/server.go`, `internal/agui/server_test.go`
**Commit:** a1134f70
**Applied fix:** `projectMessages` copied only Content/ToolCallID. It now maps
`llm.Message.ToolCalls` onto `events.Message.ToolCalls` via a new `projectToolCalls`
helper (`llm.ToolCall` → `types.ToolCall` with nested `types.FunctionCall`), so a combined
ask_user pause turn (empty Content, payload entirely in ToolCalls) is faithfully
rehydrated. Field shapes verified via `go doc` before mapping (events.Message is an alias
for coretypes.Message; ToolCalls is `[]ToolCall` with `omitempty toolCalls`).
**Tests:** Added `TestProjectMessagesToolCalls` asserting the structured projection and the
wire JSON shape (`toolCalls`, id, name, args present; absent on non-tool turns).

### WR-05: No OPTIONS/preflight handler — permissive CORS was unusable from a browser

**Files modified:** `internal/agui/server.go`, `internal/agui/server_test.go`
**Commit:** 6e5ced16
**Applied fix:** Added `withCORS`, wrapping the mux in `Mux()` when `CORSPermissive` is on:
it sets `Access-Control-Allow-Origin: *` on every response (set before the handler runs,
so it also lands on 400/404/500 error bodies) and short-circuits a preflight OPTIONS with
204 + Allow-Origin/Methods/Headers. The knob-off path returns the bare mux unchanged
(restrictive default, T-12-13). The now-redundant inline ACAO in `handleRun` was removed.
`Mux()` now returns `http.Handler` (all callers — serve.go and the tests — already consume
it as one; no caller broke).
**Tests:** Added `TestServer_CORSPermissive` (preflight 204 + headers; ACAO on a 404 error;
the off path emits no CORS headers and OPTIONS is not 204). Added a `newTestServerCfg`
helper to inject `ServerConfig`.

### WR-06: Fanout had no guard against Subscribe-after-Run / double-Run

**Files modified:** `internal/agui/fanout.go`, `internal/agui/fanout_test.go`
**Commit:** 0edeec58
**Applied fix:** Added a `started atomic.Bool`. `Run` `CompareAndSwap`s it (a second Run
panics before launching a second producer that would double-close subscriber channels);
`Subscribe` panics if it is already set (a late subscriber would otherwise be silently
never-fed or race the producer's slice snapshot). Both panic messages name the misuse —
consistent with the codebase's sharp-edges posture for the in-process seam Phase 13
(Telegram) will consume.
**Tests:** Added `TestFanoutSubscribeAfterRunPanics` and `TestFanoutDoubleRunPanics`, both
goleak/race-clean (the legitimate producer is drained on teardown). Race-verified with
`-race -count=2`.

## Skipped Issues

None — all six in-scope findings were fixed.

---

_Fixed: 2026-06-07_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
