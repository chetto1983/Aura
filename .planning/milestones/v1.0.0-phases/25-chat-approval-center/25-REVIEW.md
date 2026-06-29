---
phase: 25-chat-approval-center
reviewed: 2026-06-17T00:00:00Z
depth: standard
files_reviewed: 32
files_reviewed_list:
  - cmd/aura/cachefakes.go
  - cmd/aura/cmdfakes_test.go
  - cmd/aura/serve_webui.go
  - cmd/aura/serve_webui_test.go
  - internal/agent/tools/shell_exec_test.go
  - internal/agui/conversations_api.go
  - internal/agui/conversations_api_unit_test.go
  - internal/agui/conversations_branch_api.go
  - internal/agui/conversations_branch_api_test.go
  - internal/agui/server.go
  - internal/agui/server_branch_fakes_test.go
  - internal/agui/server_p1_test.go
  - internal/agui/server_test.go
  - internal/agui/types.go
  - internal/conversations/store_branch.go
  - internal/conversations/store_branch_fork_test.go
  - internal/conversations/store_branch_unit_test.go
  - internal/db/queries/conversation_turns.sql
  - internal/runner/fakes_test.go
  - internal/runner/interfaces.go
  - internal/runner/runner.go
  - internal/runner/runner_branch_test.go
  - internal/runner/runner_conversation.go
  - web/e2e/chat.spec.ts
  - web/playwright.config.ts
  - web/src/AppShell.tsx
  - web/src/chat/BranchPicker.tsx
  - web/src/chat/ExternalStoreChat.tsx
  - web/src/chat/__tests__/BranchPicker.test.tsx
  - web/src/chat/sseAdapter.ts
  - web/src/i18n/resources.ts
  - web/tsconfig.json
findings:
  critical: 1
  warning: 5
  info: 4
  total: 10
status: issues_found
---

# Phase 25: Code Review Report

**Reviewed:** 2026-06-17
**Depth:** standard
**Files Reviewed:** 32
**Status:** issues_found

## Summary

Plan 25-07 wires the conversation branch tree end-to-end: a `store_branch.go` fork/list/path-walk seam, a `TurnBranch` re-run-from-a-point in the runner, a thin REST sub-adapter (`conversations_branch_api.go`), the capability-gated parent-mux mounts in `serve_webui.go`, a presentational `BranchPicker`, the `ExternalStoreChat` edit/regenerate/resume folds, and a phase-proving Playwright E2E.

The backend branch seam is clean and well-tested. The project-specific invariants hold: branch routes ride the existing `/api/conversations/` subtree (no bare `mux.Handle("/api/", …)`), the mutating edit/select re-runs are `RequireCapability`-gated like `POST /agent/run`, the re-run delegates to the path-aware loader `LoadManagedHistoryForBranch` rather than re-implementing the walk (preserving the byte-identical `messages[0]` head / CAP-04 cache invariant), `uuid.Parse`/`parseBranchSeq` guards yield clean 404s, errors are redacted via `sanitizeErr`, and every touched Go file is ≤600 LOC. The `shell_exec_test.go` margin bump (6s→8s on Windows) widens only the wall-clock tolerance; the load-bearing assertions (timeout marker present, grandchild pid reaped) are untouched — no meaningful assertion was weakened.

The one BLOCKER is on the frontend continue-after-resume path: the resume re-drive POSTs `/agent/run` with `messages: []`, which the backend rejects with a 400 before any resume logic runs — and the E2E mock masks it. The remaining findings are robustness/quality issues.

## Critical Issues

### CR-01: Continue-after-resume re-drive POSTs `messages: []` → backend 400s it; resumed turn never renders against a real server

**File:** `web/src/chat/ExternalStoreChat.tsx:165-181` (effect), gated by `internal/agui/server.go:190` + `internal/agui/types.go:64-72`
**Issue:** The D-05 continue-after-resume effect re-drives the run with:

```ts
await streamPost({
  url: '/agent/run',
  body: { threadId, messages: [] },
  ...
});
```

But `handleRun` calls `ValidateRunInput(in)` *before* any resume handling, and `ValidateRunInput` returns `ErrNoMessages` whenever `len(in.Messages) == 0`:

```go
// types.go
func ValidateRunInput(in types.RunAgentInput) error {
    if in.ThreadID == "" { return ErrEmptyThreadID }
    if len(in.Messages) == 0 { return ErrNoMessages } // <- fires for the resume re-drive
    return nil
}
// server.go handleRun
if err := ValidateRunInput(in); err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest) // 400 before resume logic
    return
}
```

Against the real `aura serve`, the resume POST therefore returns **400**, `streamPost` sets `state.error = "HTTP 400"`, and the "resumed turn renders in-thread" promise (AppShell `redriveRun` → `resumeNonce` bump) silently fails — the operator resolves an inline approval and sees an error part instead of the continued run. This directly defeats APRV-02 / D-05, the headline feature of the phase.

The contradiction is internal to the backend too: `lastUserMessage` (server.go:430-451) documents "a resume-only run continues over the rehydrated history without a fresh user turn" and returns `nil, nil` for an empty list — but `ValidateRunInput` makes that branch unreachable from `handleRun`.

This is masked by the E2E because `installGoldenRoutes` mocks `**/agent/run` to return a 200 SSE body **regardless of the request body** (`chat.spec.ts:200-206`), so the deterministic CI path never exercises the real validator. The BranchPicker vitest suite never drives the resume path at all.

**Fix:** Either relax the validator to admit a resume-only run, or carry the resume entries in the body. Minimal backend fix:

```go
// ValidateRunInput: a run is valid with EITHER >=1 message OR >=1 resume entry.
func ValidateRunInput(in types.RunAgentInput) error {
    if in.ThreadID == "" {
        return ErrEmptyThreadID
    }
    if len(in.Messages) == 0 && len(in.Resume) == 0 {
        return ErrNoMessages
    }
    return nil
}
```

and confirm `handleRun` tolerates `userMsg == nil` (it already does — `lastUserMessage` returns `nil` and `Turn(ctx, id, nil)` is the continue-after-resume contract). Then add an E2E/golden assertion that the resume POST body is `messages: []` and the server still streams (i.e. drive the real validator, not only the route mock), so this regression is caught next time.

## Warnings

### WR-01: `handleSelectBranch` re-runs ANY numeric leaf seq without verifying the leaf belongs to the conversation or exists as a branch

**File:** `internal/agui/conversations_branch_api.go:124-140` + `:148-179`
**Issue:** `parseBranchSeq` only checks the seq is a positive integer; `rerunBranch` then drives `TurnBranch(ctx, convID, leaf)` directly. A caller can POST `/api/conversations/{id}/branches/999999/select` for a seq that is not a leaf (or not even a turn) of that conversation. The code comment concedes this: "a numeric-but-absent seq walks to empty, which the agent handles." Walking to empty means the agent re-runs over an **empty history** (no system prompt, no turns) and persists a brand-new turn — a confusing, unintended state mutation triggered by an arbitrary client integer, on a capability-gated mutating route. Cross-conversation leakage is prevented (the recursive CTE filters `conversation_id = $1`), so this is correctness/robustness, not a security boundary break — but a privileged mutating endpoint silently accepting a meaningless leaf is a footgun.
**Fix:** Validate the leaf is an actual branch leaf of the conversation before re-running — reuse the already-present `ListBranches` seam:

```go
func (s *Server) handleSelectBranch(w http.ResponseWriter, r *http.Request) {
    id, ok := parseConvID(w, r)
    if !ok { return }
    leaf, ok := parseBranchSeq(w, r)
    if !ok { return }
    branches, err := s.conv.ListBranches(r.Context(), id)
    if err != nil { writeStoreErr(w, err); return }
    if !slices.ContainsFunc(branches, func(b conversations.Branch) bool { return b.LeafSeq == leaf }) {
        http.Error(w, "branch not found", http.StatusNotFound)
        return
    }
    s.rerunBranch(w, r, id, leaf)
}
```

### WR-02: `onEdit` / `onReload` compute a bogus `diverge_seq` when `parentId` is not found (parentIndex == -1)

**File:** `web/src/chat/ExternalStoreChat.tsx:201-216` (onEdit), `:221-233` (onReload), `:196` (divergeSeqAt)
**Issue:** Both handlers do `const parentIndex = messages.findIndex((m) => m.id === ...)`. When the id is absent, `findIndex` returns `-1`. `divergeSeqAt(-1)` returns `0` (guarded), so:
- `onEdit` then forks at `diverge_seq: 0` → the backend returns 400 ("diverge_seq must be a positive turn seq", `conversations_branch_api.go:104-107`), and the locally-built `base` becomes `[...messages.slice(0,-1), userMessage(text)]` (slice(0,-1) drops the LAST message — wrong base).
- `onReload` does `divergeSeqAt(-1) + 1 = 1`, forking at **seq 1 (the system turn)**, and `base = messages.slice(0, 0) = []` — the whole visible thread is replaced by a single regenerated message.

These are reachable if the assistant-ui runtime ever passes a `parentId`/message id that is not in the current `messages` array (e.g. a stale id after a branch switch). `onReload` is the more dangerous of the two (silent destructive re-run at the system turn rather than a clean 400).
**Fix:** Bail out when the parent is not found, before mutating state or POSTing:

```ts
const parentIndex = messages.findIndex((m) => m.id === parentId);
if (parentIndex < 0) return; // unknown parent — do not fork/re-run
```

### WR-03: `streamPost` / `streamRun` swallow the HTTP error body — operator only ever sees "HTTP 400/409/500"

**File:** `web/src/chat/sseAdapter.ts:445-450` (streamPost), `:485-490` (streamRun)
**Issue:** On a non-OK response the reducer sets `state.error = `HTTP ${String(res.status)}``and discards`res.body`. The backend sends meaningful, already-sanitized text bodies (e.g.`runner.ErrThreadBusy`on 409,`"diverge_seq must be a positive turn seq"`on 400,`sanitizeErr`-redacted 500s). Collapsing every failure to`HTTP 400`hides the actual cause from the only viewer (the operator) and makes the 409 "thread already has an in-flight run" indistinguishable from a malformed-request 400. It also compounds CR-01 (the resume 400 surfaces as a bare`HTTP 400`with no hint why).
**Fix:** Read the (already-sanitized) body and surface it:

```ts
if (!res.ok || res.body === null) {
  const detail = res.body ? (await res.text().catch(() => '')).trim() : '';
  state.error = detail.length > 0 ? detail : `HTTP ${String(res.status)}`;
  state.status = { type: 'incomplete', reason: 'error' };
  opts.onUpdate(toThreadMessage(state), state.usage);
  return state.usage;
}
```

### WR-04: `handleSelectBranch` is capability-gated in the mux but the read path it shares (`rerunBranch`) is also reachable un-gated via `handleEditBranch` — gating is correct, but the in-flight-lock check Get is duplicated and a `Get` failure other than NotFound is a blind 500

**File:** `internal/agui/conversations_branch_api.go:148-157`
**Issue:** `rerunBranch` re-does `s.conv.Get(ctx, convID)` even though `handleEditBranch` already round-tripped the store via `ForkBranch` (which fails with `ErrTurnNotFound`/`ErrConversationNotFound` if the conv is gone) and `handleSelectBranch` has no prior Get. The extra Get is defensible for select, redundant for edit. More importantly, a non-NotFound `Get` error is mapped to a bare `http.Error(w, "thread lookup failed", 500)` WITHOUT `sanitizeErr` — consistent with `handleRun`/`handleMessages`, so no leak (the literal string is constant), but it discards the diagnostic. Low severity, but the duplicate Get on the edit path is dead weight on the hot re-run.
**Fix:** Skip the redundant Get on the edit path (ForkBranch already proved the conversation exists), or accept the duplication and leave a note. Keep the constant-string 500 (no leak) but consider logging the underlying err at WARN for diagnosability.

### WR-05: E2E `**/api/conversations*` route glob can shadow the more-specific list/get fulfilments and is order-fragile

**File:** `web/e2e/chat.spec.ts:147-168`
**Issue:** The first `page.route('**/api/conversations*', …)` matches `/api/conversations`, `/api/conversations/{id}`, AND `/api/conversations/{id}/rot-events` / `.../branches` (Playwright `*` spans path separators). The rot-events route is registered afterward and wins by last-registered precedence, but the branch routes (`/branches`, `/edit`, `/branches/{seq}/select`) have NO dedicated mock — they all fall into the broad handler, which returns either the single-conversation JSON (if the URL contains `/api/conversations/{CONV_ID}`) or `[]`. A future test that exercises the BranchPicker against this fixture would silently get a conversation object or `[]` for a `/branches` GET, not a branch list — a latent false-green. The test passes today only because the branch routes are never hit in this scenario.
**Fix:** Tighten the broad glob (e.g. anchor the list route to `**/api/conversations` and the get route to `**/api/conversations/${CONV_ID}`) and add explicit mocks for the branch routes if/when the E2E drives them, so an unmocked branch call fails loudly rather than borrowing the conversation handler's body.

## Info

### IN-01: Dead i18n key `chat.branch.label`

**File:** `web/src/i18n/resources.ts:71-75` (en) + `:283-287` (it)
**Issue:** `chat.branch.label` ("Branch {{current}} of {{count}}" / "Ramo {{current}} di {{count}}") is defined in both bundles but never referenced — `BranchPicker.tsx` renders the readout via `<BranchPickerPrimitive.Number /> / <BranchPickerPrimitive.Count />` and only uses `chat.branch.previous` / `.next`. Grep confirms zero call sites for `branch.label`.
**Fix:** Either wire the label into the `aria-live` readout (it would give screen-reader users a fuller announcement than the bare "n / count") or drop the key from both bundles.

### IN-02: `redactEvent` mutates the shared event in place

**File:** `internal/agui/server.go:549-556`
**Issue:** `redactEvent` does `re.Message = SanitizeString(re.Message)` on the `*events.RunErrorEvent` pointer it received from the translator stream, mutating the producer's event. In the current single-consumer SSE pump this is harmless (the event is not reused), but it is a surprising side-effect for a function named like a pure projection, and would corrupt a future fanout/multi-subscriber path. Not a bug today.
**Fix:** Return a copy: `clone := *re; clone.Message = SanitizeString(re.Message); return &clone` — or document the in-place mutation explicitly at the call site.

### IN-03: `divergeSeqAt` index→seq mapping is an implicit, untested coupling to the persisted seq layout

**File:** `web/src/chat/ExternalStoreChat.tsx:192-196`
**Issue:** `divergeSeqAt(index) = index + 2` hard-codes the assumption "visible list index i ⇒ backend seq i+2" (system=1, user=2, …). This is correct only while the persisted history is exactly `system, user, assistant, user, assistant, …` with no gaps and no pre-existing branches (a branch switch can change which seqs are on the visible path). There is no unit test pinning this mapping against a real branched history; the BranchPicker vitest test asserts the route is hit with the right `role`/`content` but never asserts the computed `diverge_seq` value.
**Fix:** Add a unit assertion on the `diverge_seq` sent for an edit and a reload over a known message list, and add a comment that this mapping breaks if the visible list ever diverges from the linear seq order (it will once a branch is selected and re-rendered).

### IN-04: `firstTurnFrames` comment/identifier drift in the E2E

**File:** `web/e2e/chat.spec.ts:84-90`
**Issue:** The doc comment above `goldenDelta` reads "firstTurnFrames assembles the initial streamed answer…" but the function it annotates is `goldenDelta`, not `firstTurnFrames` (defined two functions later at `:92`). Harmless, but a misleading docstring on a no-skip-as-green guard file that future maintainers must trust.
**Fix:** Move the comment to sit above `firstTurnFrames` (`:92`) and give `goldenDelta` its own one-liner.

---

_Reviewed: 2026-06-17_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
