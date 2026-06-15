# Phase 04: HITL + Identity + Conversations - Pattern Map

**Mapped:** 2026-05-30
**Files analyzed:** 21 new/modified target files
**Analogs found:** 21 / 21 (every target has a concrete in-repo analog or a verified "no analog" rationale)

All paths absolute-anchored to `D:\Aura\`. Excerpts are copied verbatim from the cited
analog with real line numbers. Conventions to replicate are stated per-file. The planner
must apply CONTEXT amendments AM-01/AM-02/AM-03 and OPEN QUESTION resolutions (cobra→switch,
add `ContextWindow`/`MaxOutputTokens`, FIFO tiebreaker, `0006` CONCURRENTLY isolation) on top
of these patterns.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/identity/store.go` (NEW) | service/store | CRUD | (none — first domain Store) + `internal/db/db.go` sqlc wiring | role-match |
| `internal/askuser/store.go` (NEW) | service/store | CRUD + event-driven | `internal/identity/store.go` (built first, D-A4-01) | role-match |
| `internal/conversations/store.go` (NEW) | service/store | CRUD + transform | `internal/identity/store.go` + `internal/db/migrate.go` tx style | role-match |
| `internal/conversations/context.go` (NEW) | utility | transform | `internal/agent/tools/result.go` (truncate/spill helpers) | partial |
| `internal/conversations/orphan_scan.go` (NEW) | utility | file-I/O | `internal/agent/tools/result.go` (`sidecarPath`/`validateID`) | role-match |
| `internal/conversations/title.go` (NEW) | utility | event-driven | `cmd/aura/chat.go` `runOneTurn` (LLM call) + `signalTurnCtx` (ctx mgmt) | partial |
| `internal/runner/runner.go` (NEW) | orchestrator | request-response + event-driven | `cmd/aura/chat.go` `chatLoop`/`runOneTurn` (drives `LlmAgent.Run`) | partial |
| `internal/db/tx.go` (NEW) | utility | transform | `internal/db/sqlc/db.go` (`New(DBTX)`, `WithTx(pgx.Tx)`) | role-match |
| `internal/db/queries/paused_states.sql` (NEW) | query | CRUD | `internal/db/queries/knowledge_migrations.sql` | exact |
| `internal/db/queries/identity.sql` (NEW) | query | CRUD | `internal/db/queries/knowledge_migrations.sql` | exact |
| `internal/db/queries/capability_grants.sql` (NEW) | query | CRUD | `internal/db/queries/knowledge_migrations.sql` | exact |
| `internal/db/queries/conversations.sql` (NEW) | query | CRUD | `internal/db/queries/knowledge_migrations.sql` | exact |
| `internal/db/queries/conversation_turns.sql` (NEW) | query | CRUD + FTS | `internal/db/queries/knowledge_migrations.sql` | exact |
| `internal/db/queries/context_rot_events.sql` (NEW) | query | CRUD | `internal/db/queries/knowledge_migrations.sql` | exact |
| `internal/db/migrations/0003_paused_states.{up,down}.sql` (NEW) | migration | — | `internal/db/migrations/0002_knowledge_migrations.{up,down}.sql` | exact |
| `internal/db/migrations/0004_identity.{up,down}.sql` (NEW) | migration | — | `0002` + `0001_init.up.sql` (default privileges) | exact |
| `internal/db/migrations/0005_conversations.{up,down}.sql` (NEW) | migration | — | `0002` (table+grant+FK alter) | exact |
| `internal/db/migrations/0006_conversation_turns_fts.{up,down}.sql` (NEW) | migration | — | `0002` (CONCURRENTLY isolation — see Pitfall 6) | role-match |
| `internal/agent/tools/ask_user.go` (NEW) | tool | event-driven | `internal/agent/tools/text_response.go` + `read_tool_output.go` | exact |
| `internal/agent/llm_agent_pause.go` (NEW) | agent (split) | event-driven | `internal/agent/llm_agent.go` `runTool`/`dispatch` (L191-247) | exact |
| `internal/agent/event.go` (MODIFY) | model | — | `internal/agent/event.go` `Actions` (L62-66) — add `AwaitingInput` sibling | exact |
| `internal/llm/{config,prices}.go` (MODIFY) | config | — | `internal/llm/config.go` env-override pattern (L196-254) | exact |
| `internal/config/config.go` (MODIFY) | config | — | `internal/config/config.go` `envIntDefault` (L135-145) | exact |
| `cmd/aura/identity.go` (NEW) | CLI | request-response | `cmd/aura/db.go` `runDB` switch tree (L19-44) | exact |
| `cmd/aura/paused_states.go` (NEW) | CLI | request-response | `cmd/aura/db.go` `runDB` switch tree (L19-44) | exact |
| `cmd/aura/chat.go` (MODIFY) | CLI | request-response | `cmd/aura/chat.go` `runChat`/`chatLoop` (drive Runner instead of bare agent) | exact |

---

## Pattern Assignments

### `internal/db/tx.go` (utility, transform) — BUILD FIRST (DRY base for every Store)

**Analog:** `internal/db/sqlc/db.go` (the generated DBTX surface).

**The exact contract to wrap** (`internal/db/sqlc/db.go:14-32`):
```go
type DBTX interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}
func New(db DBTX) *Queries { return &Queries{db: db} }
func (q *Queries) WithTx(tx pgx.Tx) *Queries { return &Queries{db: tx} }
```

**Convention to copy:** `pgx.Tx` satisfies `DBTX`, so `sqlc.New(tx)` is legal. Implement the
RESEARCH Pattern-2 helper (`04-RESEARCH.md` L242-255) verbatim — `Begin` → `defer`
rollback-on-error / rollback-and-repanic-on-panic / `Commit` — and return the named `err`.
This is the single seam `conversations.Store.AppendTurn` (SC-2 atomicity) and any future
multi-statement write reuses. D-A2-03 leaves the path (root `db.go` vs `internal/db/tx.go`)
to discretion; DRY intent is the only constraint.

---

### `internal/identity/store.go` (service/store, CRUD) — BUILD SECOND (derisks the Store pattern, D-A4-01)

**Analog:** `internal/db/db.go` (pool open + redact discipline) + the generated `sqlc.New`.

**Store-construction pattern to copy** (RESEARCH Pattern-1, `04-RESEARCH.md` L228-236, grounded in `sqlc.New`):
```go
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool, q: sqlc.New(pool)} }
```

**Error-classification pattern (idempotent grant / wildcard reject)** — copy the SQLSTATE
discipline from `internal/db/db_test.go:206-209` (the live property test), never string-match:
```go
var pgErr *pgconn.PgError
if !errors.As(err, &pgErr) || pgErr.Code != insufficientPrivilege { ... }
// for grant idempotency: ignore pgErr.Code == "23505" (unique_violation);
// for FK cascade: pgErr.Code == "23503". RESEARCH Pitfall 2 (L323-327).
```

**Convention to copy:**
- Non-tx reads use `s.q`; the package exposes `HasCapability(ctx, identityID, cap) (bool, error)`
  (wildcard-or-exact, SPEC Req#6). No interface inside the domain package — the *consumer*
  (`runner`) declares the narrow interface (D-A2-02).
- Capability name validation regex `^[a-z][a-z0-9._-]{0,63}$` lives here.
- Reject grant/revoke of `'*'` with a clear "wildcard is system-managed" error (SPEC Req#6).
- Wrap every error with `%w` and a context prefix (mirror `db.go:35` `fmt.Errorf("...: %w", ...)`).

---

### `internal/askuser/store.go` (service/store, CRUD + event-driven)

**Analog:** `internal/identity/store.go` (built first). Same `Store{pool,q}` shape.

**Query surface (D-A2-04, SPEC Req#3):** `InsertPausedState / GetByToken / ListPending /
MarkResumed / MarkResumedBatch / CleanupResumedOlderThan` — generated into `internal/db/sqlc`
from `queries/paused_states.sql`.

**Convention to copy:**
- `askuser.Store` NEVER imports `internal/agent/tools` (D-A1-04) — the Event carries the
  pause payload; the Store takes plain fields (`token, conversationID, kind, question,
  options, priority, toolCallID`).
- FIFO ordering is `ORDER BY priority DESC, created_at ASC, token ASC` — the **`token ASC`
  tiebreaker is mandatory** because rows inserted in one tx share `created_at = now()`
  (RESEARCH Pitfall 4, L335-339; CONTEXT-side Assumption A4).
- `MarkResumedBatch` and `Loop.Stop` auto-resolve are multi-row writes → wrap `db.WithTx`.
- `resumed_answer` stores `{action: accept|decline|cancel, content}` JSON (AM-02), NOT plain text.

---

### `internal/conversations/store.go` (service/store, CRUD + transform)

**Analog:** `internal/identity/store.go` (Store shape) + `internal/db/migrate.go` (tx discipline) + `internal/llm/prices.go:32` (`CostUSD` source).

**Atomic per-turn write (SC-2 / SPEC Req#8)** — wrap `db.WithTx`:
```go
// AppendTurn: BEGIN; INSERT conversation_turns; UPDATE conversations SET
// last_active_at, total_*_tokens, total_cost_usd; COMMIT. All inside one db.WithTx fn.
```

**Token/USD aggregation source** (already exists — Store sums these; `04-RESEARCH.md` L377-383):
```go
// internal/llm/client.go:61 — Usage flows on the trailing chunk
type Usage struct { PromptTokens, CompletionTokens, CachedTokens int; Cost *float64 }
// internal/llm/prices.go:32 — never reports $0 for unknown model
func CostUSD(prices map[string]Price, model string, promptTokens, completionTokens int, providerCost *float64) (display string, ok bool)
```

**Convention to copy:**
- `LoadHistory(convID) []llm.Message` reconstructs from `conversation_turns ORDER BY seq`;
  two calls byte-identical (SPEC Req#8). Reuse `llm.Message`/`llm.RoleTool` shapes (`internal/llm/client.go:24-30`).
- `total_cost_usd numeric(10,4)` → sqlc emits `pgtype.Numeric` (RESEARCH Pitfall 5, L341-345);
  aggregate in SQL (`total_cost_usd = total_cost_usd + $delta`) inside the tx, convert at the
  read boundary. NULL `title` → `pgtype.Text` (render `(untitled <created_at>)`).
- Content > `AURA_CONVERSATION_TURN_CAP_BYTES` (65536) spills to a sidecar — reuse the
  `tools/result.go` path scheme (see `orphan_scan.go` below); store `content_sidecar_path`, `content=NULL`.

---

### `internal/conversations/context.go` (utility, transform) — L1/L2/L2.5 ladder

**Analog:** `internal/agent/tools/result.go` (`truncatePreview` rune-boundary + sidecar layout).

**Rune-safe truncation pattern to reuse** (`internal/agent/tools/result.go:75-84`):
```go
func truncatePreview(content string, capBytes int) string {
	if len(content) <= capBytes { return content }
	cut := capBytes
	for cut > 0 && !utf8.RuneStart(content[cut]) { cut-- }
	return content[:cut]
}
```

**Convention to copy:**
- L1 rewrites ONLY `role='tool'` turns older than `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS` (10) to
  a `read_tool_output(<id>)` pointer. **NEVER touch `seq=1` (system)** — `llm_agent.go:69,133`
  proves the system prompt is byte-stable; mutating the cached prefix poisons the provider KV
  cache (RESEARCH Pitfall 1, L317-321). SC-1 requires L1-alone-suffices to write NO
  `context_rot_events` row.
- L2 budget formula `hard_cap = ContextWindow - max(MaxOutputTokens,20000) - 13000`,
  `warn_cap = 0.75×hard_cap`; over hard → return an explicit `Runner.Turn` error suggesting
  `aura chat new` (never the error slot for a normal-flow case — it surfaces to the REPL).
- L2.5 drops oldest user/assistant **pair** preserving `len(history) % 2 == 0`, writes a
  `context_rot_events` row.
- Token estimation: cached `tiktoken-go` cl100k_base encoder, **init once at boot** (goleak-safe,
  `feedback_minipc_cpu_budget`). Verify offline vocab (RESEARCH Assumption A6).

---

### `internal/conversations/orphan_scan.go` (utility, file-I/O)

**Analog:** `internal/agent/tools/result.go` — the path-traversal guard is the exact prior art.

**Path-safety pattern to copy** (`internal/agent/tools/result.go:45-71`):
```go
func validateID(kind, id string) error {
	if id == "" { return fmt.Errorf("%s is empty", kind) }
	if strings.Contains(id, "..") { return fmt.Errorf("%s %q contains %q", kind, id, "..") }
	for i := 0; i < len(id); i++ {
		if id[i] == '/' || id[i] == '\\' || os.IsPathSeparator(id[i]) {
			return fmt.Errorf("%s %q contains a path separator", kind, id)
		}
	}
	return nil
}
func sidecarPath(runDir, sessionID, toolCallID string) (string, error) {
	// validateID both segments BEFORE filepath.Join — sidecar lives at
	// filepath.Join(runDir, "conversations", sessionID, toolCallID+".result")
}
```

**Convention to copy:**
- `ScanOrphans(ctx, pool, runDir)` walks `$AURA_RUN_DIR/conversations/*`; `RemoveAll` dirs with
  no `conversations` row. **`session_id == conversation_id`** (Phase-3 D-26; RESEARCH L313) — the
  orphan key is the conversation UUID.
- Add `O_NOFOLLOW`/`Lstat` symlink-escape guard on the walk (D-A5-02) — beyond `validateID`'s
  string check. tmp/* >24h sweep. `du` size WARN audit-only, NEVER auto-purge.
- rm-failure → WARN + Notifier, recovered next boot (SPEC Req#12). Errors wrapped `%w`.

---

### `internal/conversations/title.go` (utility, event-driven) — auto-title worker

**Analog:** `cmd/aura/chat.go` `runOneTurn` (LLM call shape) + `signalTurnCtx` (ctx lifecycle).

**Worker pattern to copy** (RESEARCH Pattern-5, `04-RESEARCH.md` L267-278 + Specifics D-A5-01):
```go
r.wg.Add(1)
go func() {
	defer r.wg.Done()
	ctx := context.WithoutCancel(turnCtx)          // turnCtx dies when Turn returns — load-bearing
	ctx, cancel := context.WithTimeout(ctx, r.titleTimeout)
	defer cancel()
	title, err := generateTitle(ctx, ...)          // best-effort
	if err != nil { return }
	_ = r.conv.SetTitleIfNull(ctx, convID, title)   // idempotent UPDATE ... WHERE title IS NULL
}()
```

**Convention to copy:**
- The `sync.WaitGroup` is owned by the **Runner**, not this file; `Runner.Stop` does a bounded
  `wg.Wait()` so `goleak.VerifyTestMain` sees no leak (RESEARCH Pitfall 3, L329-333). Tests call
  `Stop` as the sync point.
- Fires after `seq >= 3`; errors NEVER block chat (SPEC Req#9). NULL title renders `(untitled <created_at>)`.

---

### `internal/runner/runner.go` (orchestrator, request-response + event-driven)

**Analog:** `cmd/aura/chat.go` `chatLoop`/`runOneTurn` (L104-203) — the existing in-memory loop
driver; the Runner generalizes it with persistence. NOT an `agent.Agent`; must NOT collide with
`internal/agent/workflow.LoopAgent` (`workflow/loop.go:32`, AM-03).

**Fresh-agent-per-round pattern to copy** (`cmd/aura/chat.go:160-168`, RESEARCH Pattern-4):
```go
la := agent.NewLlmAgent(agent.LlmAgentConfig{
	Client:     d.client,
	LLM:        d.cfg.LLM,
	Registry:   reg,
	PreviewCap: d.cfg.ToolPreviewCap,
	RunDir:     d.cfg.RunDir,
	SessionID:  d.sessionID,       // = conversation_id (D-26)
	UserTurns:  history,           // seeded from conversations.Store.LoadHistory (AM-01)
})
```

**Convention to copy:**
- Verb surface (D-A1-06, AM-03): `Turn(ctx, convID string, userMsg *string) iter.Seq2[*agent.Event, error]`
  (`userMsg=nil` = continue-after-resume); `SubmitAnswer(ctx, token, response) (remaining int, err error)`
  + `SubmitAnswers(ctx, map)`; `Stop(ctx, convID) error`.
- Resume = a FRESH `agent.Run` over rehydrated history (D-A1-05) — a `range`-over-func cannot be
  suspended. Injected answers are already `RoleTool{ToolCallID:<original>}` rows in the loaded
  history (SC-4: no silent LLM re-run).
- **Consumer-side narrow interfaces** declared HERE (D-A2-02): `ConversationStore`, `PauseStore`,
  `IdentityStore` — only the methods Runner calls; concrete `*conversations.Store` etc. satisfy
  implicitly. Unit tests pass hand-written fakes (supports 85% floor without DB).
- The Runner observes the pause Event (`Actions.AwaitingInput`) and is the SOLE writer of
  `paused_states` (D-A1-08). It owns the auto-title `sync.WaitGroup`.
- `iter.Seq2` discipline: copy the yield-after-false guard from `llm_agent.go:155-157` — never
  yield again after the consumer returns false.

---

### `internal/agent/tools/ask_user.go` (tool, event-driven) — NEW non-deferred tool + sentinel

**Analog:** `internal/agent/tools/text_response.go` (non-deferred, validating) + `read_tool_output.go` (arg shape).

**Spec/Execute skeleton to copy** (`internal/agent/tools/text_response.go:18-46`):
```go
func (TextResponse) Spec() Spec {
	params := json.RawMessage(`{ "type": "object", "properties": { ... }, "required": ["text"] }`)
	return Spec{ Name: "text_response", Summary: "...", Description: "...", Parameters: params, Deferred: false }
}
func (TextResponse) Execute(_ context.Context, raw json.RawMessage) (ToolResult, error) {
	var a textResponseArgs
	if err := json.Unmarshal(raw, &a); err != nil { return ToolResult{}, fmt.Errorf("text_response args: %w", err) }
	if a.Text == "" { return ToolResult{}, fmt.Errorf("text_response: text is required") }
	return ToolResult{Preview: a.Text, Bytes: len(a.Text)}, nil
}
```

**Sentinel pattern to copy** (`internal/agent/errors.go:1-10` — but as a *struct* error carrying payload, D-A1-04):
```go
// Not a bare errors.New — ask_user must carry the pause payload so dispatch can
// errors.As(err, &ErrAwaitingUserInput{}) and emit the Event.
type ErrAwaitingUserInput struct { Question string; Options []Option; Kind string; Priority int; ToolCallID string }
func (e *ErrAwaitingUserInput) Error() string { return "awaiting user input" }
```

**Convention to copy:**
- `Deferred: false` (small, always-visible primitive — same class as `text_response`/`current_time`).
- Args `{question, options?:[2-4], kind:clarification|approval|choice, priority?:0-100}`; validation
  rejects empty `question`, exactly-1 option, non-distinct labels, priority out of 0-100 (SPEC Req#1).
- `Execute` returns the sentinel (`ErrAwaitingUserInput`), NOT a `ToolResult` (SPEC Req#1).
- No-secrets guardrail text in `Description` (D-A3-03, mirrors MCP elicitation prohibition).
- This file owns ONLY pure types — no DB import (D-A1-04).

---

### `internal/agent/llm_agent_pause.go` (agent split, event-driven) — pause DETECTION seam

**Analog:** `internal/agent/llm_agent.go` `runTool` (L236-247) + `dispatch` (L191-230) — the exact seam.

**The seam to intercept** (`internal/agent/llm_agent.go:236-247`):
```go
func (a *LlmAgent) runTool(ctx context.Context, call llm.ToolCall) string {
	tool, ok := a.registry.Get(call.Function.Name)
	if !ok { return fmt.Sprintf("error: unknown tool %q", call.Function.Name) }
	toolCtx := tools.WithToolCallContext(ctx, a.sessionID, call.ID, a.runDir, a.previewCap)
	res, err := tool.Execute(toolCtx, json.RawMessage(call.Function.Arguments))
	if err != nil {
		return fmt.Sprintf("error: %s", err.Error())  // ← pause sentinel MUST be intercepted BEFORE this line
	}
	return res.Preview
}
```

**Convention to copy:**
- Catch `errors.As(err, &tools.ErrAwaitingUserInput{})` in `dispatch` BEFORE the generic
  `err != nil` fallback. On a pause: suppress the `RoleTool` (do NOT append a fake tool result),
  rewrite the persisted assistant message to `ask_user`-only tool_calls (D-A1-07 wire-correctness),
  and emit a pause Event with the NEW `Actions.AwaitingInput` field.
- Pause is an **Event**, NEVER the `iter.Seq2` error slot (RESEARCH Pattern-3 anti-pattern, L260;
  mirrors how budget exhaustion is Event-only — `llm_agent.go:121-124`, `workflow/loop.go:8`).
- Only the pause-DETECTION half moves here (AM-01); the agent stays DB-free. `LoadHistory` is
  `conversations.Store.LoadHistory`, NOT in the agent. Split only if `llm_agent.go` would cross
  600 LOC (it is 321 now — the split is for separation, not size).

---

### `internal/agent/event.go` (model, MODIFY) — add `Actions.AwaitingInput`

**Analog:** the file itself — `Actions` struct (`internal/agent/event.go:62-66`).

**Existing struct to extend** (`internal/agent/event.go:62-66`):
```go
type Actions struct {
	Escalate      bool           `json:"escalate,omitempty"`       // true → stop this branch / cancel siblings
	StateDelta    map[string]any `json:"state_delta,omitempty"`
	ArtifactDelta map[string]any `json:"artifact_delta,omitempty"`
}
```

**Convention to copy:** Add `AwaitingInput *AwaitingInput \`json:"awaiting_input,omitempty"\`` as a
sibling to `Escalate` (D-A1-03). Carry `Question/Options/Kind/Priority/ToolCallID` +
originating-agent id (D-A1-08 swarm forward-compat). The wire projection `eventWire`
(L87-98) and `MarshalJSON`/`UnmarshalJSON` (L105-164) must round-trip the new field
byte-identically — the file already enforces decode(encode())==identity; mirror the
`omitempty`-via-pointer trick used for `MessageID` (L94, L178-183) so an unset pause
field never leaks onto the wire.

---

### `internal/llm/{config,prices}.go` (config, MODIFY) — add `ContextWindow`/`MaxOutputTokens`

**Analog:** `internal/llm/config.go` env-override block (L196-254) — exact pattern.

**Env-override pattern to copy** (`internal/llm/config.go:211-241`):
```go
if v, ok, err := envInt(envMaxTokens); err != nil { return err } else if ok { cfg.MaxTokens = v }
// envInt: ok=false when unset/empty; set-but-malformed is a fail-fast error (D-22)
func envInt(key string) (val int, ok bool, err error) {
	v := os.Getenv(key)
	if v == "" { return 0, false, nil }
	n, perr := strconv.Atoi(v)
	if perr != nil { return 0, false, fmt.Errorf("%s=%q: not a valid integer", key, v) }
	return n, true, nil
}
```

**Convention to copy:**
- Add `ContextWindow int` + `MaxOutputTokens int` to `Config` (`config.go:54-65`) — they do NOT
  exist today (RESEARCH OPEN QUESTION 2 / Assumption A3). Default reflecting the ~1M DeepSeek-V4
  window. Read by `conversations/context.go` for the L2 formula.
- Env names `AURA_MODEL_CONTEXT_WINDOW` / `AURA_MODEL_MAX_OUTPUT_TOKENS` (SPEC Constraints L116).
  Wire through the same 4-tier load order; add to `fileConfig` (L71-82) + `overlayFile` (L163-194)
  + `applyEnvOverrides` (L199-227) consistently. Fail-fast on malformed numeric.

---

### `internal/config/config.go` (config, MODIFY) — new `AURA_*` env vars

**Analog:** the file itself — `envIntDefault` (L135-145).

**Pattern to copy** (`internal/config/config.go:135-145`):
```go
func envIntDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" { return fallback }
	n, err := strconv.Atoi(v)
	if err != nil { return fallback }  // ad-hoc tweak falls back, not fatal
	return n
}
```

**Convention to copy:** Add to the root `Config` struct + `Load()` return: `AURA_CONVERSATION_TURN_CAP_BYTES`
(65536), `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS` (10), `AURA_HISTORY_HARD_CAP_TURNS` (50),
`AURA_RUN_DIR_WARN_THRESHOLD_BYTES` (1073741824) via `envIntDefault`. NOTE the file's own header
warns "No new fields land here without an owning slice plan" (L8) — this slice IS the owner.
Whether these live in the root composite or a new `conversations` sub-config is the planner's call;
the file comment favors per-subsystem configs in their owning packages.

---

### `internal/db/queries/*.sql` (query, CRUD) — 6 new files

**Analog:** `internal/db/queries/knowledge_migrations.sql` (exact — the established sqlc surface).

**Query-annotation pattern to copy** (`internal/db/queries/knowledge_migrations.sql:1-8`):
```sql
-- name: RecordKnowledgeMigration :exec
INSERT INTO aura.knowledge_migrations (version, name, checksum) VALUES ($1, $2, $3);

-- name: ListAppliedKnowledgeMigrations :many
SELECT version, name, checksum, applied_at FROM aura.knowledge_migrations ORDER BY version ASC;
```

**FTS query (locked cross-slice contract, SPEC Req#13 / D-A5-03)** — copy verbatim:
```sql
-- name: SearchConversationTurns :many
SELECT conversation_id, seq, content, similarity(content, $1) AS sim
FROM aura.conversation_turns
WHERE content % $1
ORDER BY similarity(content, $1) DESC
LIMIT $2;
```

**Convention to copy:** `:exec` for writes, `:many` for list reads, `:one` for single-row.
`emit_interface: true` (sqlc.yaml L17) regenerates `querier.go` with `var _ Querier = (*Queries)(nil)`.
Run `sqlc generate` after the queries land; inspect generated `models.go` for `pgtype.*` on
nullable/numeric columns BEFORE writing Store code (Pitfall 5). Files: `paused_states.sql`,
`identity.sql`, `capability_grants.sql` (one file or two — discretion), `conversations.sql`,
`conversation_turns.sql`, `context_rot_events.sql`.

---

### `internal/db/migrations/0003`–`0006` (migration) — up/down pairs

**Analog:** `internal/db/migrations/0002_knowledge_migrations.{up,down}.sql` + `0001_init.up.sql` (default privileges).

**Table+grant pattern to copy** (`internal/db/migrations/0002_knowledge_migrations.up.sql:4-17`):
```sql
CREATE TABLE aura.knowledge_migrations ( version integer PRIMARY KEY, name text NOT NULL, ... );
-- Belt + suspenders (DEFAULT PRIVILEGES from 0001 should already cover this)
GRANT SELECT, INSERT ON aura.knowledge_migrations TO aura_app;
GRANT ALL            ON aura.knowledge_migrations TO aura_migrate;
CREATE INDEX knowledge_migrations_applied_at_idx ON aura.knowledge_migrations (applied_at DESC);
```

**Down pattern** (`0002_knowledge_migrations.down.sql:1`): `DROP TABLE IF EXISTS aura.<table>;`

**Identity seed pattern (SPEC Req#5)** — `04-RESEARCH.md` L399-410:
```sql
INSERT INTO aura.identities (id, name, kind)
  VALUES ('00000000-0000-0000-0000-000000000001','local','system') ON CONFLICT DO NOTHING;
INSERT INTO aura.capability_grants (identity_id, capability)
  VALUES ('00000000-0000-0000-0000-000000000001','*') ON CONFLICT DO NOTHING;
```

**Convention to copy:**
- `aura_migrate` runs DDL; `ALTER DEFAULT PRIVILEGES` from `0001_init.up.sql:22-25` already grants
  `aura_app` DML on new tables — explicit GRANTs are forensic belt-and-suspenders only; NEVER grant
  TRUNCATE/DROP/CREATE to `aura_app` (RESEARCH Pitfall 7, L353-357; `0001_init.up.sql:30-33`).
- `0003 paused_states`: `conversation_id` is plain `text` (NO FK) for 1.5↔1.8 independence
  (SPEC Constraints L115); `proxied_*` columns NULL (D-A1-08). Partial index
  `(conversation_id, resumed_at) WHERE resumed_at IS NULL`.
- `0005 conversations` ALTERs `paused_states.conversation_id` → `uuid` + FK
  `conversations(id) ON DELETE CASCADE` + adds `resumed_answer` + the `context_rot_events` table.
- `0006 FTS`: **`CREATE INDEX CONCURRENTLY` MUST be the SOLE statement in its file** — golang-migrate
  v4.19.1 wraps each migration in a tx and CONCURRENTLY cannot run in a tx (RESEARCH Pitfall 6,
  L347-351; Assumption A5). Put `CREATE EXTENSION IF NOT EXISTS pg_trgm` as a separate
  statement/migration concern; verify golang-migrate's single-statement heuristic at plan time.
- Down reverses up; `aura db migrate` must be idempotent on re-run (no-op), proven by
  `TestMigrate_Idempotent` (`db_test.go:71-94`) — write the equivalent for 0003-0006.

---

### `cmd/aura/identity.go` + `cmd/aura/paused_states.go` (CLI, request-response) — NEW

**Analog:** `cmd/aura/db.go` `runDB` (L19-44) — the canonical nested-`switch` dispatcher.

**Dispatcher pattern to copy** (`cmd/aura/db.go:19-44`):
```go
func runDB(args []string) {
	if len(args) < 1 { fmt.Fprintln(os.Stderr, "usage: aura db {migrate|ping|status|reset}"); os.Exit(1) }
	cfg, err := config.Load()
	if err != nil { fmt.Fprintln(os.Stderr, "config load:", err); os.Exit(1) }
	ctx := context.Background()
	switch args[0] {
	case "migrate": dbMigrate(ctx, cfg)
	...
	default: fmt.Fprintln(os.Stderr, "usage: ..."); os.Exit(1)
	}
}
```

**Convention to copy:**
- **Hand-rolled `switch`, NOT cobra** — go.mod has no `spf13/cobra`; CLAUDE.md mandates following
  the existing pattern. CONTEXT D-A3-05 says "cobra group" but RESEARCH OPEN QUESTION 1 (L438-441,
  Assumption A2) resolves this to the `switch` tree. SPEC never says cobra. Flag the deviation in PLAN.
- `aura identity {list|get <name>|grant <name> <cap>|revoke <name> <cap>}` and
  `aura paused-states {list|purge --before <ISO> --confirm}` each get a `run<X>(args []string)`
  func mirroring `runDB`. Wire into `cmd/aura/main.go`'s top switch (L29-49).
- `config.Load()` → `db.Open` → construct the Store → call it; print human-readable line; `os.Exit(1)`
  on error (exactly the `db.go` per-branch shape, L46-128). Destructive ops gated like `dbReset`
  (`db.go:118-122`: require `--confirm`/`--yes`).
- `tabwriter` for tabular list output (`db.go:110-115`).

---

### `cmd/aura/chat.go` (CLI, MODIFY) — drive the Runner

**Analog:** the file itself — `runChat`/`chatLoop`/`runOneTurn` (L52-203).

**REPL structure to preserve** (`cmd/aura/chat.go:104-146` `chatLoop`):
- Streaming prose, dim tool-activity (D-12), per-turn cost footer (D-11), two-stage Ctrl+C
  (`signalTurnCtx`, L226-230) — all preserved, now sourced from `runner.Runner`'s Event stream
  instead of a bare `LlmAgent` (D-A3-06).

**Convention to copy:**
- `runChat` becomes a `switch args[0]` group `{list|new|resume|archive|unarchive|delete|rename|search}`
  (mirror `runDB`); bare `aura chat` = start a NEW persisted conversation REPL; `aura chat resume`
  (no id) = most-recent active.
- Composition root (D-A2-05): `config.Load` → `db.Open` → construct the 3 Stores + boot
  `ScanOrphans` + tiktoken encoder init → construct `runner.Runner` → REPL drives `Runner.Turn`.
- On a pause Event: render inline (`clarification`→free-text, `approval`→`[y/N]` default No,
  `choice`→numbered, D-A3-02) → `SubmitAnswer(token, {action,content})` → when `remaining==0`,
  `Turn(convID, nil)` to continue.
- Keep the `chatDeps` injectable-dependency shape (L37-47) so the REPL stays testable with
  scripted stdin + a fake client (no live OpenRouter).

---

## Shared Patterns

### Per-domain Store construction (canonical — every DB slice copies this)
**Source:** RESEARCH Pattern-1 (`04-RESEARCH.md` L228-236) grounded in `internal/db/sqlc/db.go:20`.
**Apply to:** `internal/identity`, `internal/askuser`, `internal/conversations`.
```go
type Store struct { pool *pgxpool.Pool; q *sqlc.Queries }
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool, q: sqlc.New(pool)} }
```

### Atomic multi-statement write (DRY)
**Source:** `internal/db/tx.go` (NEW, RESEARCH Pattern-2 L242-255).
**Apply to:** `conversations.Store.AppendTurn` (SC-2), `askuser.Store.MarkResumedBatch` + Stop auto-resolve.

### SQLSTATE error classification (never string-match)
**Source:** `internal/db/db_test.go:206-209`; RESEARCH Pitfall 2 (L323-327).
**Apply to:** all Store error handling — `errors.As(err, &pgErr)` + switch on `pgErr.Code`
(`23505` unique, `23503` FK, `42501` privilege). pgx errs lazily at `rows.Err`/`Scan`, not at `Query`.

### goleak + env-gated integration tier (no-skip-as-green)
**Source:** `internal/db/db_test.go:1-45` (`//go:build db_integration`, `goleak.VerifyTestMain`, `envOrSkip`).
**Apply to:** every new package's `*_test.go`. `envOrSkip` `t.Fatal`s under `$CI` when the DSN is
unset (skip locally, fail-loud in CI). Coverage floor 85% across the full tag matrix (CLAUDE.md).

### Error wrapping discipline
**Source:** `internal/db/db.go:35,54,58` + `internal/db/migrate.go:49,54`.
**Apply to:** all packages — `fmt.Errorf("<context>: %w", err)`; redact DSNs via `redactDSN`
(`db.go:72`) anywhere a connection string could enter a log/error.

### Event-only termination (never the iter error slot)
**Source:** `internal/agent/llm_agent.go:121-124` (budget) + `internal/agent/workflow/loop.go:8`.
**Apply to:** the pause path — `Actions.AwaitingInput` Event, not the `iter.Seq2` error slot
(D-04/D-15). The error slot is for REAL infra failure only.

### Non-deferred tool spec
**Source:** `internal/agent/tools/text_response.go:18-46` + `current_time.go:20-34`.
**Apply to:** `ask_user.go` — `Deferred: false`, JSON-schema `Parameters`, validate-then-act `Execute`.

---

## No Analog Found

No target file is fully without prior art. Two carry genuinely-new mechanism that the analogs
only partially cover; the planner should lean on RESEARCH's grounding for the new portions:

| File | Role | Data Flow | New portion (analog covers the rest) |
|------|------|-----------|--------------------------------------|
| `internal/conversations/context.go` | utility | transform | The L1/L2/L2.5 context-management *ladder* itself is new (no in-repo compaction code); `result.go` covers only the rune-safe truncation + sidecar layout. Use RESEARCH Pattern + SPEC Req#10 + Pitfall 1 for the ladder logic. |
| `internal/runner/runner.go` | orchestrator | event-driven | The pause/resume *orchestration* (observe Event → write `paused_states` → resume-as-fresh-Run) is new; `chat.go` covers only the single-turn agent-driving loop. Use RESEARCH Pattern-3/4 + D-A1-01..08. |

## Metadata

**Analog search scope:** `internal/agent`, `internal/agent/tools`, `internal/agent/workflow`,
`internal/db`, `internal/db/sqlc`, `internal/db/queries`, `internal/db/migrations`, `internal/llm`,
`internal/config`, `cmd/aura`.
**Files scanned:** 22 source files + sqlc.yaml + 1 integration test (db_test.go) read in full.
**Pattern extraction date:** 2026-05-30
