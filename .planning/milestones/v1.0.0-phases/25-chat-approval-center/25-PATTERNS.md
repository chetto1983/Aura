# Phase 25: Chat + Approval Center - Pattern Map

**Mapped:** 2026-06-17
**Files analyzed:** 22 (10 Go backend create/modify, 12 frontend create)
**Analogs found:** 20 / 22 (2 net-new patterns flagged)

> Phase 25 is a **wiring + thin-adapter** phase: almost every new file copies an
> already-shipped analog in THIS codebase. The two genuinely net-new items are the
> D-09 branch-tree backend (migration 0017 + path-aware loader) and the
> assistant-ui `useExternalStoreRuntime` SSE reducer — both flagged below as
> new-pattern. Every analog citation is a file:line the planner can put directly in
> a plan's `read_first`.

---

## File Classification

### Backend (Go)

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/agui/conversations_api.go` *(new)* | route/adapter | request-response (CRUD read) | `internal/agui/server.go:220` `handleMessages` | exact (thin read over a store) |
| `internal/agui/approvals_api.go` *(new)* | route/adapter | request-response (read + resume mutate) | `internal/agui/server.go:220` `handleMessages` + `server.go:357` `resumeAnswers` | role-match (read) + exact (resume map) |
| `internal/agui/server.go` *(modify)* | controller | streaming (SSE) | itself, line 214 (`Translate(…, false)` call site) | exact (one-line D-01 flip) |
| `cmd/aura/serve_webui.go` *(modify)* | config/route-mount | request-response | itself, lines 40-52 + 99-133 (route prefix registration) | exact (add the 2 new subtrees) |
| `internal/askuser/store.go` *(modify)* | store | request-response (aggregate read) | itself, `ListPending` `store.go:161` | exact (sibling method, drop the WHERE conv_id) |
| `internal/db/queries/paused_states.sql` *(modify)* | query | CRUD (read) | `ListPendingPausedStates` `paused_states.sql:14` | exact (same SELECT, no conv filter) |
| `internal/runner` resolve wiring *(modify, decline bridge)* | service | event-driven (resume) | `runner/runner_resume.go:101` `SubmitAnswers` + `:69` `SubmitAnswer` | exact (three-action model already exists) |
| `internal/conversations/store_branch.go` *(new, D-09)* | store | CRUD (branch writes + path walk) | `internal/conversations/store.go:255` `LoadHistory` + `store_append.go:40` `AppendTurn` | role-match (NEW topology) |
| `internal/conversations/context.go` *(modify, D-09)* | service | transform (path-aware ladder) | itself, `LoadManagedHistory` `context.go:137` + `dropOldestPairs` `context.go:305` | exact (extend with branch path) |
| `internal/db/migrations/0017_conversation_turn_branches.{up,down}.sql` *(new, D-09)* | migration | DDL | `0005_conversations.up.sql:23` (turns table) + `0006_*_fts.up.sql:7` (CONCURRENTLY split) | role-match (additive column + index) |
| `internal/db/queries/conversation_turns.sql` *(modify, D-09)* | query | CRUD | `conversation_turns.sql:18` `ListTurnsBySeq` + `:13` `NextConversationTurnSeq` | exact (sqlc query style) |

### Frontend (React/TS)

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `web/src/chat/ExternalStoreChat.tsx` *(new)* | provider/component | streaming | `web/src/AppShell.tsx` (mount shape) + `LoginPage.tsx` (fetch) | role-match (runtime is net-new) |
| `web/src/chat/sseAdapter.ts` *(new)* | utility (reducer) | streaming (SSE→parts) | **NO close analog** (POST-SSE fetch reducer does not exist) | **new-pattern** |
| `web/src/chat/Composer.tsx` *(new)* | component | request-response | `LoginPage.tsx:74` form + send button `:157` | role-match |
| `web/src/chat/ReasoningDrawer.tsx` *(new)* | component | streaming (collapsible) | `LoginPage.tsx` show/hide toggle `:102-144` (`aria-pressed`) | role-match |
| `web/src/chat/ToolActivityCard.tsx` *(new)* | component | streaming (raw blob) | `RuntimeHealthPanel.tsx:32` `StatusRow` + `:23` `StatusDot` | role-match (status dot + mono text) |
| `web/src/chat/BranchPicker.tsx` *(new, D-09)* | component | event-driven | **NO close analog** (assistant-ui `BranchPickerPrimitive`) | **new-pattern** |
| `web/src/chat/RuntimeFooter.tsx` *(new)* | component | streaming (read SSE state) | `RuntimeHealthPanel.tsx:128` (status cluster + mono metrics) | exact (status panel pattern) |
| `web/src/conversations/ConversationSidebar.tsx` *(new)* | component | CRUD | `RuntimeHealthPanel.tsx` rows + `AppShell.tsx:33` `aside` | role-match |
| `web/src/conversations/SearchPanel.tsx` *(new)* | component | request-response | `RuntimeHealthPanel.tsx` list-render | role-match |
| `web/src/conversations/useConversations.ts` *(new)* | hook | request-response (REST) | `web/src/health/useRuntimeHealth.ts:57` (React Query) | exact |
| `web/src/approvals/{ApprovalBadge,ApprovalList,InlineApprovalCard}.tsx` *(new)* | component | event-driven (poll + resolve) | `RuntimeHealthPanel.tsx` + `LoginPage.tsx` form | role-match |
| `web/src/approvals/useApprovals.ts` *(new)* | hook | request-response (poll) | `useRuntimeHealth.ts:57` (`refetchInterval` poll) | exact |
| `web/src/i18n/resources.ts` *(modify)* | config (copy) | — | itself, lines 1-43 (en+it bundle shape) | exact |

---

## Pattern Assignments

### `internal/agui/conversations_api.go` (route/adapter, request-response)

**Analog:** `internal/agui/server.go:220` (`handleMessages`) — the canonical thin read.

**Thin-read pattern** (`server.go:220-247`) — parse path → `uuid.Parse` guard → store call → JSON, NO business logic:
```go
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	if _, err := uuid.Parse(id); err != nil {        // malformed id → clean 404, never 500 (T-12-11)
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}
	if _, err := s.conv.Get(ctx, id); err != nil {
		if errors.Is(err, conversations.ErrConversationNotFound) {
			http.Error(w, "thread not found", http.StatusNotFound)
			return
		}
		http.Error(w, "thread lookup failed", http.StatusInternalServerError)
		return
	}
	hist, err := s.conv.LoadHistory(ctx, id)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError) // server.go:475
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(events.NewMessagesSnapshotEvent(projectMessages(hist)))
}
```

**Consumer interface to widen** — the server holds a `ConversationStore` interface
(narrower than `*conversations.Store`). The new routes call `List`/`SearchConversationTurns`/`UpdateStatus`/`Rename`/`Delete`/`SetTitleIfNull`, all already on the concrete store (`store.go:173/310/187/204/364/221`). **What to replicate:** widen the consumer-side `ConversationStore` interface in `internal/agui` (accept interfaces, return structs — D-A2-02); `*conversations.Store` satisfies it implicitly. Each route is ONE `uuid.Parse` guard + ONE store call + JSON. Body-cap mutating bodies like `maxRunBodyBytes` (`server.go:31`); redact errors with `sanitizeErr` (`server.go:475`).

---

### `internal/agui/approvals_api.go` (route/adapter, request-response + resume mutate)

**Analog (read):** `handleMessages` (above). **Analog (resume):** `server.go:357` `resumeAnswers`.

**The decline gap (Pitfall 4) — the load-bearing reason this is a NEW adapter.** The
AG-UI `Resume[]` path maps only two states (`server.go:357-370`):
```go
func resumeAnswers(entries []types.ResumeEntry) map[string]runner.ResponseInput {
	out := make(map[string]runner.ResponseInput, len(entries))
	for _, e := range entries {
		action := askuser.ActionAccept
		if e.Status == types.ResumeStatusCancelled {     // ONLY resolved/cancelled exist
			action = askuser.ActionCancel
		}
		out[e.InterruptID] = runner.ResponseInput{Action: action, Content: payloadString(e.Payload)}
	}
	return out
}
```
There is **no path to `askuser.ActionDecline`** over `POST /agent/run`. But the
Runner's `SubmitAnswers` (`runner_resume.go:101`) consumes the FULL three-action
`map[string]runner.ResponseInput` — accept/decline/cancel are all expressible there
(`runner_resume.go:151-165` `injectAnswer`, decline → `declinedContent` `runner_resume.go:50`).

**What to replicate (RESEARCH Open Question 1, recommended Option A):** a new
`POST /api/approvals/{token}/resolve` adapter that builds a
`map[string]runner.ResponseInput{token: {Action: "accept"|"decline"|"cancel", Content}}`
and calls `Runner.SubmitAnswers` directly — bypassing the two-state AG-UI limit. The
server already declares the narrow `Runner` interface with `SubmitAnswers`
(`server.go:57-60`), so no new dependency. Cancel re-uses the existing
`AutoResolveForConversation` semantics inside `SubmitAnswers`
(`runner_resume.go:116-118`). Validate the token with `uuid.Parse` first; map
`ErrPauseNotFound` (`askuser/store.go:49`) → 404/409.

**The cross-thread pending read** (`GET /api/approvals`) calls the new
`askuser.Store.ListPendingAll` (see below) and projects `askuser.Pending` to JSON.
Apply `SanitizeString` (`server.go:486`) to surfaced `question`/`resumed_answer` per
the Security Domain V7 note.

---

### `internal/agui/server.go` (controller, streaming) — D-01 reasoning-on flip

**Analog:** itself. **Exact current call site (`server.go:211-214`):**
```go
turn := s.run.Turn(ctx, in.ThreadID, userMsg)
// The HTTP/SSE gateway keeps reasoning redacted (conservative web default); live
// CoT surfacing is currently a Telegram-only opt-in (agui_subscriber.go).
s.streamSSE(ctx, w, Translate(in.ThreadID, runID, s.idgen, turn, false))
```
**What to replicate:** flip the final `false` → `true` for the cockpit SSE path
(D-01). The `showReasoning bool` is the last param of `Translate` (`translator.go:53`
threads it into `reasoningRunState{… showReasoning: showReasoning}`). The
`REASONING_*` lifecycle frames are emitted **regardless**; only the delta text is
gated. Update the comment to record the whole-origin-private justification (Phase 24
D-03). NOTE the flip is **cockpit-scoped** — if Telegram still shares this handler,
the planner threads the flag from config rather than hard-coding `true` globally
(verify the call graph; the same `handleRun` serves both transports today).

---

### `cmd/aura/serve_webui.go` (route-mount, request-response) — Pitfall 6 footgun

**Analog:** itself, lines 40-52 (`aguiRoutePrefixes`) + 99-133 (`newServeHandler`).

**The mux-collision footgun is documented IN this file (`serve_webui.go:21-25`):**
```
// The "/api/" carve-out is an EXCLUSION prefix ONLY — it is NOT registered on the mux.
// "/api/integrations/" is already mounted; a second mux.Handle("/api/", ...) would
// collide with / shadow that subtree (T-24-07).
```
**Route registration to replicate (`serve_webui.go:104-123`):**
```go
mux := http.NewServeMux()
mux.Handle("POST /agent/run", agui.RequireCapability(aguiHandler, auth, agentRunCapability))
for _, prefix := range aguiRoutePrefixes {
	mux.Handle(prefix, aguiHandler)
}
mux.HandleFunc("POST /login", auth.LoginHandler())
mux.HandleFunc("POST /logout", auth.LogoutHandler())
mux.Handle(integrationsRoutePrefix, newIntegrationsProxy())   // "/api/integrations/" — already mounted
mux.Handle("/", static)
// ...
return agui.RequireAuth(mux, auth), nil                       // whole-origin gate inherits to every route
```
**What to replicate:** register the SPECIFIC new subtrees —
`mux.Handle("/api/conversations/", aguiHandler)` and
`mux.Handle("/api/approvals", aguiHandler)` — **NEVER a bare `/api/`** (it would
shadow `integrationsRoutePrefix` = `/api/integrations/`, `integrations_proxy.go:33`).
The `/api/` SPA-fallback exclusion already returns these as backend 404s
(`fallbackExcludedPrefixes()` returns `/api/`, `serve_webui.go:71`), so **no fallback
change is needed**. The new routes inherit `RequireAuth` for free (the whole mux is
wrapped, `serve_webui.go:132`). The mutating resolve route may additionally wrap in
`RequireCapability` like `POST /agent/run` does (`serve_webui.go:109`) — V4 note.

---

### `internal/askuser/store.go` (store, aggregate read) — D-04 / APRV-01

**Analog:** itself, `ListPending` `store.go:161` (per-conversation).

**The sibling method to copy (`store.go:161-175`):**
```go
func (s *Store) ListPending(ctx context.Context, conversationID string) ([]Pending, error) {
	convID, err := parseUUID("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	rows, err := s.q.ListPendingPausedStates(ctx, convID)
	if err != nil {
		return nil, fmt.Errorf("list pending for %s: %w", conversationID, err)
	}
	out := make([]Pending, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromRow(r))   // store.go:327 projector
	}
	return out, nil
}
```
**What to replicate:** a `ListPendingAll(ctx, limit int) ([]Pending, error)` that drops
the `conversation_id` arg, binds `$1` limit (default 100 when `<= 0`, mirroring
`ListRecent`'s `limit <= 0 → 50` guard at `store.go:194`), and reuses the SAME
`fromRow` projector (`store.go:327`). Note `Pending` (`store.go:68`) already carries
`ConversationID`/`Kind`/`Question`/`Options`/`Priority`/`ToolCallID` — everything
APRV-01 needs except the proxied-from-child id (add it to `Pending` + `fromRow` if the
list must show the source thread; the row already SELECTs it).

---

### `internal/db/queries/paused_states.sql` (query, CRUD read)

**Analog:** `ListPendingPausedStates` `paused_states.sql:14` — copy it, remove the conv filter.
```sql
-- name: ListPendingPausedStates :many   (EXISTING — paused_states.sql:14)
SELECT token, conversation_id, kind, question, options, priority,
       resume_context, tool_call_id, proxied_from_child_id, proxied_tool_call_id,
       created_at, resumed_at, resumed_answer
FROM aura.paused_states
WHERE conversation_id = $1
  AND resumed_at IS NULL
ORDER BY priority DESC, created_at ASC, token ASC;
```
**What to replicate** — the new `ListAllPendingPausedStates :many`: same SELECT,
**drop** `WHERE conversation_id = $1`, keep `WHERE resumed_at IS NULL`, keep the
**mandatory total-order tiebreaker** `ORDER BY priority DESC, created_at ASC, token ASC`
(`token ASC` is non-negotiable — tx-batched rows share `created_at = now()`, see the
comment at `askuser/store.go:158-160`), bind `LIMIT $1`. The existing partial index
`paused_states_pending_idx ON (conversation_id, resumed_at) WHERE resumed_at IS NULL`
(`0003_paused_states.up.sql:30`) is conversation-first so it does not optimally serve
the cross-thread scan — for a single operator the pending set is tiny; a new
`(resumed_at, priority DESC) WHERE resumed_at IS NULL` index is likely premature
(RESEARCH A4). Run `make sqlc` after editing the query.

---

### `internal/runner` decline-bridge resolve (service, event-driven resume)

**Analog:** `runner/runner_resume.go:101` `SubmitAnswers` (batch) + `:69` `SubmitAnswer` (single).

**The three-action model already exists end-to-end** — the bridge just *reaches* it
over HTTP. Accept/decline injection (`runner_resume.go:151-165`):
```go
func (r *Runner) injectAnswer(ctx context.Context, pending askuser.Pending, resp ResponseInput) error {
	content := resp.Content
	if resp.Action == askuser.ActionDecline {
		content = declinedContent          // "user declined to answer" (runner_resume.go:50)
	}
	if err := r.Conv.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: pending.ConversationID,
		Role:           llm.RoleTool,
		ToolCallID:     pending.ToolCallID,
		Content:        content,
	}); err != nil {
		return fmt.Errorf("inject resume answer: %w", err)
	}
	return nil
}
```
Cancel short-circuits to `cancelConversation` → `AutoResolveForConversation`
(`runner_resume.go:116-118` / `:173-181`). **What to replicate:** the new
`/api/approvals/{token}/resolve` handler maps the verb to `askuser.Action*` and calls
`Runner.SubmitAnswers` with a single-entry map. Extend `runner_resume*_test.go`
(RESEARCH test map APRV-02) — the existing tests already cover accept/decline/cancel;
the new coverage is the HTTP→`ResponseInput.Action` mapping. After resolve, re-drive
the turn with a no-`Resume` `POST /agent/run` (continue-after-resume,
`Turn(…, userMsg=nil)`) so the run continues over the rehydrated history.

---

### `internal/conversations/store_branch.go` + `context.go` (D-09 — NEW topology)

**Analogs:** `store.go:255` `LoadHistory` (byte-identity contract) + `store_append.go:40`
`AppendTurn` (atomic seq allocation) + `context.go:137` `LoadManagedHistory` +
`context.go:305` `dropOldestPairs` (the protected-head logic).

**The byte-identity contract the branch walk MUST preserve (`store.go:250-269`):**
```go
// LoadHistory reconstructs the loop history from conversation_turns ORDER BY seq.
// Two consecutive calls return byte-identical slices (Req#8): the reconstruction
// is a pure function of the persisted rows ...
func (s *Store) LoadHistory(ctx context.Context, conversationID string) ([]llm.Message, error) {
	turns, err := s.loadTurns(ctx, conversationID)   // store.go:273 ListTurnsBySeq
	// ... turnToMessage per turn ... repairToolMessagePairs(out)
}
```
**The protected-head invariant (Pitfall 3 — CAP-04 cache gate) lives in `context.go:305-318`:**
```go
// Split off the PROTECTED head: a leading system turn (seq=1) AND the messages[1]
// always-block (seq=2, D-07 / Pitfall 3) if present. Neither is ever dropped ...
start := 0
if len(turns) > 0 && turns[0].Seq == 1 && turns[0].Role == llm.RoleSystem {
	start = 1
}
if len(turns) > start && isAlwaysBlock(turns[start]) {
	start++
}
```
And the `messages[0]`/`messages[1]` rebuild-per-turn comment at `context.go:37-47`
(messages[0] is branch-independent BY CONSTRUCTION). **What to replicate:** the
path-aware loader walks a *selected leaf → root* path producing a **deterministic**
ordered turn list whose head (system + always-block) is byte-identical to the linear
case; only the body turns differ per branch. Reuse `loadTurns`/`turnToMessage`/
`repairToolMessagePairs` unchanged; the new code is the parent/branch pointer walk.
Run `bash scripts/cache_invariant_audit.sh` (22 byte-stable hashes, Postgres-free,
CI-blocking) after wiring. **This is net-new topology — flag for its own plan/wave +
the ROADMAP/REQUIREMENTS CHAT-05 amendment first (PRD-first).**

---

### `internal/db/migrations/0017_conversation_turn_branches.{up,down}.sql` (migration, D-09)

**Analogs:** `0005_conversations.up.sql:23` (the `conversation_turns` table) +
`0006_conversation_turns_fts.up.sql:7` (the CONCURRENTLY split precedent).

**The target table (`0005_conversations.up.sql:23-36`):**
```sql
CREATE TABLE aura.conversation_turns (
    conversation_id      uuid        NOT NULL REFERENCES aura.conversations (id) ON DELETE CASCADE,
    seq                  integer     NOT NULL,
    role                 text        NOT NULL CHECK (role IN ('system','user','assistant','tool')),
    content              text,
    -- ...
    PRIMARY KEY (conversation_id, seq)
);
```
**The CONCURRENTLY-split rule (Pitfall 5) — `0006_*_fts.up.sql:1-8`:**
```sql
-- This file MUST contain exactly ONE statement: CREATE INDEX CONCURRENTLY cannot
-- run inside a transaction block ... a multi-statement migration string is sent to
-- Postgres as an implicit tx block.
CREATE INDEX CONCURRENTLY IF NOT EXISTS conversation_turns_content_trgm
    ON aura.conversation_turns USING GIN (content gin_trgm_ops);
```
**What to replicate:** an `ALTER TABLE … ADD COLUMN … DEFAULT …` (tx-safe) for the
branch/parent pointers (RESEARCH Pitfall 5 / Runtime State note) that **defaults
existing rows into one canonical linear branch** so non-branched conversations keep
byte-identical `LoadHistory` (RESEARCH A3). Mirror the `GRANT SELECT,INSERT,UPDATE,
DELETE … TO aura_app` / `GRANT ALL … TO aura_migrate` style (`0005:60-65`,
`0003:26-27`). If a non-trivial index on the new column is needed on a large table,
split it into its OWN single-statement `CONCURRENTLY` migration (the 0006 precedent).
Latest migration is **0016** — D-09 lands at **0017** (verified via the migrations dir).

---

### Frontend — `web/src/conversations/useConversations.ts` + `approvals/useApprovals.ts` (hooks, REST)

**Analog:** `web/src/health/useRuntimeHealth.ts:57` — the React Query + same-origin fetch pattern.
```ts
async function fetchHealthz(): Promise<{ status: number; body: HealthzBody }> {
  const res = await fetch('/healthz', {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',        // the SPA is served by the very binary exposing the route
  });
  const body = (await res.json()) as HealthzBody;
  return { status: res.status, body };
}
export function useRuntimeHealth(): UseRuntimeHealthResult {
  const healthz = useQuery({
    queryKey: ['healthz'],
    queryFn: fetchHealthz,
    refetchInterval: HEALTH_REFETCH_INTERVAL_MS,   // 5000ms poll
    retry: false,
  });
  // ...
}
```
**What to replicate:** `useConversations` → `useQuery({ queryKey: ['conversations', includeArchived], queryFn: …, retry: false })` over `GET /api/conversations`; mutations (archive/delete/rename) via `useMutation` + `queryClient.invalidateQueries`. `useApprovals` → the SAME `refetchInterval` poll (`useRuntimeHealth.ts:61`) over `GET /api/approvals` to drive the badge count. ALWAYS `credentials: 'same-origin'`. POST bodies follow `LoginPage.tsx:32-37` (`Content-Type` + `credentials`).

---

### Frontend — `web/src/chat/RuntimeFooter.tsx` + `ToolActivityCard.tsx` (status cluster)

**Analog:** `web/src/health/RuntimeHealthPanel.tsx:128` — the status-cluster + mono-metric panel.

**The status-dot + text + mono pattern (`RuntimeHealthPanel.tsx:23-42`)** — color is
decorative (`aria-hidden`), the text label carries meaning (WCAG 1.4.1):
```tsx
function StatusDot({ tone }: { tone: Tone }) {
  return <span aria-hidden="true" className={`inline-block h-2 w-2 shrink-0 rounded-sm ${TONE_CLASS[tone]}`} />;
}
function StatusRow({ label, status, tone, mono }: RowState) {
  return (
    <div className="flex min-h-[var(--row-h)] items-center justify-between gap-4 py-1">
      <span className="text-sm text-text-muted">{label}</span>
      <span className="flex items-center gap-2">
        <StatusDot tone={tone} />
        <span className={`text-sm text-text ${mono ? 'font-mono' : ''}`}>{status}</span>
      </span>
    </div>
  );
}
```
**What to replicate:** the footer's Tokens/Cache/Cost/Context metrics use `font-mono`
exactly like the `bind`/`build` rows (`RuntimeHealthPanel.tsx:169-176`); the
tool-card status dot reuses `StatusDot` tones (success=done). The footer's data comes
off the SSE `STATE_DELTA` (see the sseAdapter below) — `cacheHit% = cacheHitTokens /
promptTokens`, guard `/0` at the presentation boundary.

---

### Frontend — `web/src/chat/sseAdapter.ts` (NEW-PATTERN: POST-SSE → ThreadMessage reducer)

**NO close analog** — the existing frontend reads REST via React Query
(`useRuntimeHealth.ts`); there is no streaming-`fetch` + `ReadableStream` decoder in
`web/src/` today. `EventSource` cannot POST a body, so this is a `fetch` + reader loop.

**The contract is the backend `STATE_DELTA` op shape** the translator emits
(`translator.go:131` `events.NewStateDeltaEvent(stateDeltaOps(...))`, ops built over
SORTED keys at `translator.go:341`, fed by `usageStateDelta` at
`llm_agent_events.go:229`):
```ts
// the final Event's StateDelta renders as JSONPatch ops: replace /prompt_tokens etc.
interface TurnUsage { promptTokens: number; completionTokens: number; cacheHitTokens: number; costUsd?: number; }
function usageFromStateDelta(ops: Array<{op: string; path: string; value: unknown}>): Partial<TurnUsage> {
  const byPath = Object.fromEntries(ops.map(o => [o.path, o.value]));
  return {
    promptTokens: Number(byPath['/prompt_tokens'] ?? 0),
    completionTokens: Number(byPath['/completion_tokens'] ?? 0),
    cacheHitTokens: Number(byPath['/cache_hit_tokens'] ?? 0),
    costUsd: byPath['/cost_usd'] !== undefined ? Number(byPath['/cost_usd']) : undefined,
  };
}
```
**Frame→part mapping** (RESEARCH Pattern 1, verified against `translator.go`):
`TEXT_MESSAGE_*` → text part; `REASONING_*` → reasoning drawer (D-01); `TOOL_CALL_START/ARGS/END/RESULT` → raw tool card; a `STATE_DELTA` carrying `tool_call_id` is a **tool-result marker, NEVER assistant prose** (Pitfall 2 — trust the event TYPE not the content, `translator.go:325-336` `toolResultCallID`); `STATE_DELTA` with usage keys → footer (not a message part). **Test fixture:** the reducer test uses captured real translator output (`internal/agui/testdata/golden-events.json`), not synthetic shapes (RESEARCH no-skip discipline).

---

### Frontend — `web/src/chat/ExternalStoreChat.tsx` + `BranchPicker.tsx` (NEW-PATTERN: assistant-ui runtime)

**No close in-repo analog** — assistant-ui (`@assistant-ui/react`) is net-new to
`web/package.json` (it is NOT yet a dependency). Use `useExternalStoreRuntime` (UI-SPEC
§Runtime adapter, RESOLVED) mapping the SSE reducer above onto `ThreadMessage[]`;
`BranchPickerPrimitive` binds to the D-09 tree backend; stop = `ComposerPrimitive.Cancel`
→ `api.thread().cancelRun()` → `onCancel` aborts the `fetch` (`AbortController`); the
server's `streamSSE` unwinds cleanly on `ctx.Done()` (`server.go:274-285`, goleak-clean).
**Mount shape:** wire into the existing `AppShell.tsx:48` center `<section aria-label={t('shell.chatRegion')}>` (currently empty). The provider/mount idiom mirrors how `AppShell.tsx` composes `RuntimeHealthPanel`. **Consult `assistant-ui.com/llms.txt` for exact 0.14.22 API names at plan time (RESEARCH A1, SKILL.md mandate).**

---

## Shared Patterns

### Authentication / origin gate
**Source:** `cmd/aura/serve_webui.go:99-132` (`newServeHandler` wraps the whole mux in `agui.RequireAuth`).
**Apply to:** ALL new `/api/conversations…` + `/api/approvals` routes — they inherit
`RequireAuth` for free by mounting on the parent mux. NO new auth this phase. The
mutating resolve route may additionally interpose `agui.RequireCapability(…, agentRunCapability)` like `POST /agent/run` (`serve_webui.go:109`).
```go
mux.Handle("POST /agent/run", agui.RequireCapability(aguiHandler, auth, agentRunCapability))
// ...
return agui.RequireAuth(mux, auth), nil
```

### Error redaction on the wire
**Source:** `internal/agui/server.go:471-516` (`sanitizeErr` / `SanitizeString` / `redactEvent`).
**Apply to:** every new handler's error response (`http.Error(w, sanitizeErr(err), …)`)
AND the surfaced `question`/`resumed_answer` strings in the cross-thread list
(`SanitizeString(...)`, V7 note). The DSN/userinfo/token redaction is already written
— do not re-spell it.

### uuid.Parse-before-store-call guard
**Source:** `server.go:167` (run) + `server.go:226` (messages).
**Apply to:** every new route taking a conversation/token path param — `uuid.Parse`
first so a malformed id is a clean 404, never a 500 (T-12-11).
```go
if _, err := uuid.Parse(id); err != nil {
	http.Error(w, "thread not found", http.StatusNotFound)
	return
}
```

### Store method = thin pgtype-boundary wrapper
**Source:** `conversations/store.go:173` (`List`) + `askuser/store.go:161` (`ListPending`).
**Apply to:** the new `ListPendingAll` — `parseUUID`/limit-default at the boundary,
ONE sqlc call, project rows via the existing `fromRow`. SQLSTATE error classification
via `errors.As` + sentinel errors (never message matching). `db.WithTx` for any
multi-statement write (`askuser/store.go:253` `MarkResumedBatch` is the precedent).

### React Query same-origin REST
**Source:** `web/src/health/useRuntimeHealth.ts:26-69`.
**Apply to:** `useConversations` + `useApprovals` — `useQuery`/`useMutation`,
`credentials: 'same-origin'`, `retry: false`, `refetchInterval` for the polled badge,
`queryClient.invalidateQueries` after a mutation.

### i18n en+it bundle discipline
**Source:** `web/src/i18n/resources.ts:1-43`.
**Apply to:** ALL new copy — add keys to BOTH `en` and `it` under `chat.*`/`approval.*`/
`conversations.*`/`footer.*`; reference via `t('feature.key')`; rebuild `web/dist` after
copy changes (memory `reference_cockpit_i18n_react_i18next`). The UI-SPEC Copywriting
Contract is the en source of truth.

### a11y component idioms (status by icon+text, aria-pressed toggle, omit-when-valid)
**Source:** `RuntimeHealthPanel.tsx:23` (decorative dot + text label) +
`LoginPage.tsx:102-144` (`aria-pressed` show/hide toggle, 24×24 SVG `aria-hidden`) +
`LoginPage.tsx:98` (`aria-invalid={error !== null || undefined}` omit-when-valid).
**Apply to:** the reasoning drawer toggle, tool-card expander, approval verbs, badge —
status NEVER encoded by color alone; icon-only buttons carry `aria-label`; focus ring
`focus-visible:outline-2 outline-offset-2 outline-accent` (shipped everywhere).

---

## No Analog Found (net-new — planner uses RESEARCH.md patterns)

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `web/src/chat/sseAdapter.ts` | utility (reducer) | streaming | No POST-SSE `fetch`+`ReadableStream` decoder exists in `web/src/` (only React Query REST). Contract = the `translator.go` STATE_DELTA op shape + RESEARCH Pattern 1 frame→part map. Test against `internal/agui/testdata/golden-events.json`. |
| `web/src/chat/ExternalStoreChat.tsx` | provider | streaming | `@assistant-ui/react` `useExternalStoreRuntime` is net-new to `package.json`. No in-repo precedent. Mount shape mirrors `AppShell.tsx`; API names from `assistant-ui.com/llms.txt` (RESEARCH A1). |
| `web/src/chat/BranchPicker.tsx` | component | event-driven | `BranchPickerPrimitive` (assistant-ui) + the D-09 tree backend — both net-new. |
| `internal/conversations/store_branch.go` | store | CRUD (branch topology) | The parent/branch-pointer walk is NEW topology. The byte-identity + protected-head invariants are reused from `store.go:255` / `context.go:305`, but the path walk itself has no analog. Net-new sub-slice; ROADMAP/REQUIREMENTS CHAT-05 amendment first. |

---

## Metadata

**Analog search scope:** `internal/agui`, `internal/conversations`, `internal/askuser`,
`internal/runner`, `internal/agent`, `internal/db/{queries,migrations}`,
`internal/webui`, `cmd/aura`, `web/src/{health,routes,i18n}`, `web/src/AppShell.tsx`.
**Files scanned (read at file:line):** server.go, serve_webui.go, conversations/store.go,
askuser/store.go, conversations/context.go, conversations/store_append.go,
paused_states.sql, conversation_turns.sql, migrations 0003/0005/0006, runner_resume.go,
llm_agent_events.go, translator.go, webui/embed.go, web useRuntimeHealth.ts,
RuntimeHealthPanel.tsx, AppShell.tsx, LoginPage.tsx, i18n/resources.ts, integrations_proxy.go.
**Pattern extraction date:** 2026-06-17
