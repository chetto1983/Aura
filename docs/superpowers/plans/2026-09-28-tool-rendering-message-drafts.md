# Editable Outbound Message Drafts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Before every calendar email or WhatsApp text send, let the owner review/edit the actual destination and message, then dispatch those exact arguments at most once and show the real outcome.

**Architecture:** Intercept the two actions before `Gateway.Decide` can use a standing grant or a non-strict no-op. Persist an owner-scoped draft and park the originating tool call without opening MCP. A dedicated authenticated API validates edits, atomically claims one dispatch, executes the saved tool through the gateway's reservation/audit path using only the approved effective arguments, and resumes the original tool call with the actual result. An ambiguous post-dispatch outcome is recorded and never auto-retried.

**Tech Stack:** Go gateway/agent/runner, PostgreSQL `aura` schema and sqlc, AG-UI pending projection, React/TypeScript, Tool UI Message Draft source adapted for editing and WhatsApp, Vitest, DB integration tests, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-28-tool-rendering-tool-ui-elements-design.md` (Editable message draft section). This plan uses the trusted-recipe marker from MCP plan Task 1 and the existing gateway, pause and approval surfaces; it does not depend on media cards.

## Global Constraints

- Every `calendar send_email` and `whatsapp send_message` gets its own review, even with a session/always grant or non-strict profile. Other destructive actions stay on their current gateway path. A headless/production request with no interactive owner is denied before any send.
- Server/action/sender identity is fixed for one draft. Only schema-supported destination and message fields are editable. The backend builds, validates and fingerprints the effective args; the browser cannot submit authority via hidden fields, tool name or digest.
- One draft can enter dispatch at most once. A crash or timeout after dispatch starts is **uncertain**, never silently retried. Do not show an undo countdown or assert exactly-once external delivery.
- Keep the full draft owner-scoped; no message body, recipients or attachments in logs/traces/public shares. The pending card stays above the composer, is never collapsed, and explicit Decline is required to reject. Escape closes only local edit UI.
- New migration number is the next integer in `internal/db/migrations/` at execution time, per `CLAUDE.md`. New files stay under 600 lines; tests use realistic fake MCP responses and the existing DB integration harness. Commit tested tasks separately; leave unrelated dirty files alone.

## Review Focus

1. An always grant or local-trusted profile cannot skip message review; Task 2 tests both.
2. A stale tab, duplicate click or model re-emission cannot send a second message; Tasks 1 and 3 test these.
3. An edited recipient and body must be the exact MCP arguments, including after reload; Tasks 1, 2 and 4 test this.
4. Foreign identity, public share, expired draft and a forged hidden field cannot read/approve a draft; Tasks 1 and 3 test these.
5. A crash between dispatch and receipt must surface uncertain delivery with no automatic retry; Tasks 2 and 4 test this.

---

### Task 1: Durable owner-scoped draft and exact-argument validation

**Files:**
- Create: next numbered `internal/db/migrations/*_message_drafts.sql`, matching queries in `internal/db/queries/message_drafts.sql`, regenerated `internal/db/sqlc` files
- Create: `internal/messagedrafts/store.go`, `store_test.go`, `validation.go`, `validation_test.go`
- Read source schemas: calendar fork's `send_email` action and WhatsApp fork's `send_message` tool at the revisions recorded in the spec mapping.

**Interfaces:**
- Consumes: authenticated `identityID`, `conversationID`, `toolCallID`, exact recipe/tool/action, original `json.RawMessage`, live tool schema.
- Produces: `Store.Create(ctx, DraftInput) (Draft, error)`, `Store.ListPending(ctx, identityID, conversationID) ([]Draft, error)`, `Store.ClaimSend(ctx, identityID, draftID, overrides) (ClaimedDraft, error)`, `Store.Decline(...)`, `Store.MarkOutcome(...)`. `DraftInput` contains `IdentityID`, `ConversationID`, `ToolCallID`, `Recipe`, `Tool`, `Action`, `OriginalArgs json.RawMessage`, and `ExpiresAt time.Time`. `Draft` adds `ID`, `Status`, `OriginalFingerprint` and timestamps; `ClaimedDraft` adds `EffectiveArgs json.RawMessage` and `EffectiveFingerprint string`. These are server-side records; the owner-only API maps them to bounded review DTOs.

- [ ] **Step 1: Write failing validation and DB integration tests.** Include email To/Cc/Bcc/subject/body, WhatsApp destination/body, Unicode, empty/over-cap fields, unknown override key, duplicate Create, two concurrent ClaimSend calls, expired/foreign draft, and a forged sender/tool/action. Use a DB transaction fixture with two identities; only the owner sees the row. Never print body or recipients on test failure.

```go
first, err := store.ClaimSend(ctxOwner, ownerID, draftID, json.RawMessage(`{"to":["edited@example.test"],"body":"revised"}`))
if err != nil || first.EffectiveFingerprint == "" { t.Fatal("claim must bind validated edits") }
if _, err := store.ClaimSend(ctxOwner, ownerID, draftID, nil); !errors.Is(err, ErrAlreadyClaimed) { t.Fatal("duplicate claim dispatched") }
```

- [ ] **Step 2: Run `go test ./internal/messagedrafts -count=1` and DB integration tests; confirm failures.**
- [ ] **Step 3: Implement schema, validation and compare-and-swap.** Store bounded original/effective JSONB with `identity_id`, conversation, call ID, recipe, action, original fingerprint, status, expiry and timestamps; unique `(conversation_id, tool_call_id)`, RLS owner policy. `ClaimSend` validates overrides against the two explicit editable field sets, merges them onto server-stored args, checks the live schema, then performs `UPDATE ... SET status='dispatching', effective_args=$..., dispatch_started_at=now() WHERE id=$... AND identity_id=$... AND status='pending' AND expires_at>now() RETURNING ...`. Keep returned data inside the server; public DTOs omit full args except the owner-only review projection.

```sql
UPDATE aura.message_drafts SET status = 'dispatching', effective_args = $2,
    dispatch_started_at = now()
WHERE id = $1 AND identity_id = $3 AND status = 'pending' AND expires_at > now()
RETURNING id, conversation_id, tool_call_id, effective_args;
```

- [ ] **Step 4: Run unit + DB integration, race tests for concurrent claim, and migration/RLS checks.**
- [ ] **Step 5: Commit only migration/store/generated query files.** Use `feat(gateway): persist one-shot outbound message drafts`.

### Task 2: Withhold, dispatch once, and resume the original tool call

**Files:**
- Create: `internal/agent/llm_agent_message_draft.go`, its test
- Modify: `internal/agent/llm_agent_retry.go`, `llm_agent_pause.go`, `event.go` only at the intercept and pause seams
- Create: `internal/gateway/message_draft.go`, its test
- Modify: `internal/gateway/decide.go`, `reserve.go` only to admit an exact draft continuation without re-challenge
- Create: `internal/runner/message_draft_resume.go`, its unit/DB integration tests
- Modify: `cmd/aura/chat_boot.go` to wire the store and continuation; use focused test files rather than touching unrelated server files.

**Interfaces:**
- Consumes: Task 1 `Draft`/`ClaimedDraft`, existing `ReservationKey`, tool registry, `ResumeCommitter`, gateway operation registry, and bridge-owned `tools.Spec.TrustedRecipeSource`.
- Produces: a pending `message_draft` pause tied to the original tool-call ID; `Gateway.DecideApprovedDraft(ctx, spec, effectiveArgs, key, claim)` that runs existing `beginOperation`/`reserve` without `routeApprove`; `Runner.ResolveMessageDraft(ctx, draftID, action, overrides) (ResolveDirective, error)` that appends the real result/decline under the parked call ID.

- [ ] **Step 1: Write failing gateway/runner tests.** A first `send_email`/`send_message` call emits one pending draft and zero MCP calls under strict and local-trusted profiles, with/without a standing grant. A read or other destructive action follows its old path. An approved edit yields exactly one MCP call with its effective JSON. An expired/declined/noninteractive request yields zero. A duplicate API call and a model retry yield no second send. A fake sidecar timeout after dispatch yields `uncertain` and no retry.

```go
if got := fakeMCP.CallCount(); got != 0 { t.Fatalf("send happened before review: %d", got) }
directive, err := runner.ResolveMessageDraft(ctxOwner, draft.ID, "send", json.RawMessage(`{"body":"edited"}`))
if err != nil || fakeMCP.CallCount() != 1 { t.Fatal("approved draft must dispatch once") }
_ = directive
```

- [ ] **Step 2: Run `go test ./internal/agent ./internal/gateway ./internal/runner -run 'Test.*MessageDraft' -count=1`; confirm failures.**
- [ ] **Step 3: Implement the dedicated path.** At the dispatch seam, identify only trusted calendar `send_email` and WhatsApp `send_message` by the bridge-owned recipe marker plus exact action/tool, before the gateway's profile/grant shortcuts. Create the draft and park the tool call through a dedicated pause event; persist the assistant tool call and pending state together as existing pause machinery does, without opening MCP or consuming MCP timeout. `ResolveMessageDraft` claims the draft, obtains the registered tool by the stored exact identity, calls `DecideApprovedDraft` with the effective fingerprint and same reservation key, then executes once. Append the actual tool result as the original RoleTool answer and resume the run; on decline append the standard declined result. If result append fails after dispatch, mark `uncertain` and do not send again. After dispatch begins, a process loss records or recovers `uncertain` without re-invocation.

```go
if isOutboundTextMessage(spec, args) {
    return a.withholdMessageDraft(ctx, spec, args, key) // no tool.Execute here
}
// The continuation compares claim.EffectiveFingerprint to a fresh hash of
// effectiveArgs before it enters beginOperation/reserve; a mismatch denies.
```

- [ ] **Step 4: Run gateway/runner tests, DB integration and race tests.** Inspect invocation facts: one reservation start and one real end; no body/recipient in log or trace attributes.
- [ ] **Step 5: Commit only dispatch/resume wiring.** Use `feat(gateway): execute reviewed message arguments once`.

### Task 3: Owner-only draft API and editable card

**Files:**
- Create: `internal/agui/message_drafts_api.go`, `message_drafts_api_test.go`
- Modify: `internal/agui/server.go` to register the route; `cmd/aura/serve_webui.go` only for the existing auth/capability wrapper
- Create: `web/src/approvals/useThreadMessageDrafts.ts`, `MessageDraftCard.tsx`, focused tests
- Modify: `web/src/approvals/ThreadApprovalCards.tsx` and its test, cockpit approval state hook and `web/src/i18n/resources.chatactivity.ts`
- Add only copied Tool UI `message-draft` source, adapted to controlled editable email and WhatsApp fields.

**Interfaces:**
- Consumes: Task 1 owner-scoped store read, Task 2 `ResolveMessageDraft`; current thread ID and authenticated identity.
- Produces: `GET /api/message-drafts?conversation_id=...` owner-only pending DTOs; `POST /api/message-drafts/{id}/resolve` with `{action:'send'|'decline', overrides:{...}}`, returning status/receipt. A bounded `aura.message_draft` event refreshes the pending list; reload also fetches it.

- [ ] **Step 1: Write failing API and card tests.** A foreign identity and public share cannot list/resolve; malformed UUID and unknown keys fail; stale/expired duplicate submit fails without dispatch. The card shows full destination/content, supports keyboard edits, says Send to the edited destination, disables while submitting, and announces sent/declined/uncertain. Escape does not resolve the draft. Pending card appears above composer and never folds into a tool row.

```tsx
await user.clear(screen.getByLabelText(/message body/i));
await user.type(screen.getByLabelText(/message body/i), 'Edited text');
await user.click(screen.getByRole('button', { name: /send/i }));
expect(resolveDraft).toHaveBeenCalledWith(expect.objectContaining({ overrides: { body: 'Edited text' } }));
```

- [ ] **Step 2: Run `go test ./internal/agui -run TestMessageDraftAPI -count=1` and focused Vitest; confirm failures.**
- [ ] **Step 3: Implement the API and card.** Read `identity_id` from authenticated context, ignore any client-supplied owner/server identity, and call only `ResolveMessageDraft` for sends. Return bounded public receipt fields without raw arguments. Fetch pending drafts on thread switch/reconnect, merge SSE by draft ID, and remove resolved cards. Adapt the copied message-draft source to controlled To/Cc/Bcc/subject/body and WhatsApp destination/body inputs, localized labels and error text; keep sender/account fixed and visible. Do not add client undo timers.

```ts
type DraftResolution = { action: 'send' | 'decline'; overrides: Record<string, string | string[]> };
await fetch(`/api/message-drafts/${encodeURIComponent(id)}/resolve`, {
  method: 'POST', headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ action: 'send', overrides: editedFields }),
});
```

- [ ] **Step 4: Run Go/Vitest/typecheck plus auth/reload/keyboard tests.** Verify no draft content appears in public-share responses or logs.
- [ ] **Step 5: Commit only API/card/i18n files.** Use `feat(chat): review and edit outbound message drafts`.

### Task 4: Delivery recovery and real cockpit gate

**Files:**
- Create: `internal/messagedrafts/recovery.go`, `recovery_test.go`
- Add integration tests under `internal/runner/` and a fake calendar/WhatsApp MCP sidecar test under `cmd/aura/`.
- Add one cockpit Playwright scenario under `web/e2e/` using existing login/thread fixtures.
- Modify: `prd.md` only after running and recording the required live-stack measurement.

**Interfaces:**
- Consumes: Task 1 draft states, Task 2 real dispatch outcomes, Task 3 receipt DTO.
- Produces: restart reconciliation that marks abandoned `dispatching` drafts `uncertain` without issuing a second MCP call; a user-visible receipt that survives replay.

- [ ] **Step 1: Write failing recovery/E2E tests.** Kill the process after `dispatching` is durable but before a receipt, restart, and require uncertain/no MCP retry. Verify two distinct messages each get their own review under an always grant; edit both fields; send one and decline the other; reload and see exact receipts. Trigger double click, stale tab, expiry and foreign principal attempts.

```go
recovered, err := store.MarkAbandonedDispatchesUncertain(ctx, time.Now().Add(-time.Minute))
if err != nil || recovered != 1 || fakeMCP.CallCount() != 0 { t.Fatal("recovery retried an ambiguous send") }
```

- [ ] **Step 2: Run focused recovery/runner/Playwright tests and confirm failures.**
- [ ] **Step 3: Implement reconciliation and receipts.** Sweep only old `dispatching` rows to `uncertain` with an atomic update, never to `pending`; preserve the original approved effective-argument hash and safe audit state. Project sent/failed/declined/uncertain receipts into live and snapshot, and remove the pending review card. Leave retry as a new call that creates a new draft and review.

```sql
UPDATE aura.message_drafts SET status='uncertain', resolved_at=now()
WHERE status='dispatching' AND dispatch_started_at < $1
RETURNING id;
```

- [ ] **Step 4: Run Go unit + DB integration + race, web typecheck/Vitest, and authenticated Playwright with fake sidecars; then do a real-stack smoke with a controlled test inbox/chat.** Record actual call count, effective args and receipts without logging message content. Amend `prd.md` only to reflect measured behavior.
- [ ] **Step 5: Commit recovery/tests and measured PRD amendment separately if needed; perform whole-branch review.** Use `feat(gateway): reconcile uncertain outbound sends` for the code commit.
