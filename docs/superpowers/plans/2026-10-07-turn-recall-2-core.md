# Turn recall, plan 2 of 3: the decision path and its memory

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every user turn is read once, before its first request, through one decision path that picks the reasoning effort (composer > compatible remembered label > seeds > teacher) and preloads the deferred tools similar past turns ran; the decision and its provenance are persisted in Postgres and projected onto `ConversationTurn`, where later turns recall them.

**Architecture:** Postgres gains seven nullable provenance columns on `aura.conversation_turns` (migration 0137) and the runner writes them on the exact user row it dispatched. The conversation projection carries them to ArcadeDB, where `Client.RecallTurns` reads the identity's past user turns through three pools (user labels, teacher labels, tool turns), each filtered before its top-k, with tools read through the reasoning graph. The agent owns the decision (`readTurn`): it asks memory through a port the runner binds per identity, falls back to the seed bank and the synchronous teacher, and adds remembered tools to `a.activated`. A frozen bilingual evaluation and a lab-VM end-to-end run gate the release.

**Tech Stack:** Go 1.26, PostgreSQL + sqlc 1.31.1 + golang-migrate, ArcadeDB 26.10.1 (HTTP SQL, `vector.neighbors`), EmbeddingGemma sidecar, OpenTelemetry metrics, build tags `db_integration`, `arcadedb_integration`, `reasoning_live`, `turn_recall_eval`.

**Spec:** `docs/superpowers/specs/2026-10-06-turn-recall-design.md` (commit `4583dc0bc`): "Shape", "The shared function: `RecallTurns`", "Compatibility and label provenance", "Effort decision", "Tool preload", "Persisting what was learned → The effort", "Errors", "Constants", "Observability", "Testing". Plan 1 (`docs/superpowers/plans/2026-10-06-turn-recall-1-traces-and-version-floor.md`) shipped the tool-only traces and the 26.10.1 floor this plan reads. Plan 3 (the `tool_search` dense leg) follows.

## Global Constraints

- Every Go file stays at or under 600 lines (`make file-size`); split on touch. `internal/runner/runner_persist.go` is at 597 and is split in Task 6 before anything is added to it.
- Per task, run only the tests the task names (`go test -race -count=1 -run '<names>' <package>`, every command prefixed with `time`), plus `go vet` of the packages whose signatures the task changes, which compiles their tests. Never `go build ./...` and never a whole-package test run per task: lefthook's pre-commit runs gofmt, vet, golangci-lint and the file-size cap on the staged files, and the full sweep runs once, in Task 8.
- Go runs in WSL. Below, `W '<cmd>'` means: `wsl -e bash -lc 'cd /mnt/d/Aura && export PATH=$HOME/.local/bin:$HOME/go/bin:/usr/local/go/bin:$PATH && <cmd>'`.
- Commit from WSL with lefthook, never `--no-verify`. Write the message with the Write tool to a file in your scratchpad directory, then `W 'git add <new files> && LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -q -F <wsl path of the message file> -- <paths>'`. Every message ends with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.
- Commit with pathspecs only. Leave unrelated dirty files alone: `.claude/settings.json`, `Aura_analisi_codebase.md`, and anything else this plan does not name.
- Mutation testing runs in CI only. Never mutate code locally, by tool or by hand.
- The migration number is the next free slot when the task runs: `ls internal/db/migrations/ | tail -1` printed `0136_assets_drop_searchable.up.sql` when this plan was written, so the slot is `0137`. Re-run the command first; if the head moved, use the next free number everywhere this plan says 0137.
- After editing `internal/db/migrations/` or `internal/db/queries/`, run `W 'sqlc generate'` and commit `internal/db/sqlc/` with the change (CI job `sqlc-golden` fails otherwise).
- ArcadeDB live tests (recipe **A**): `W 'source scripts/lib/disposable_stack.sh && export ARCADEDB_URL=http://127.0.0.1:2480 ARCADEDB_PASSWORD="$(read_secret ARCADEDB_PASSWORD)" CI=true && go test -tags arcadedb_integration -race -count=1 -run "<PATTERN>" ./internal/arcadedb/'`. It needs the local ArcadeDB 26.10.1 (`make memory-up`). Each test creates and drops its own database. If no local ArcadeDB is reachable, the `agent-memory-eval` CI job runs the tier on push, and the task stays open until that job is green on the SHA.
- Postgres integration tests (recipe **P**), never against the live `aura` database: `W 'source scripts/lib/disposable_stack.sh && PGPW="$(read_secret POSTGRES_PASSWORD)" && disposable_stack_bring_up_auto aura_cov "$PGPW" 5433 postgres:18.4-alpine3.24 aura-postgres-cov aura-postgres && trap disposable_stack_teardown EXIT && disposable_stack_export_env "$PGPW" && export CI=true && go test -tags db_integration -race -count=1 -run "<PATTERN>" <packages>'`. If Docker is not reachable from WSL, the CI `db_integration` job is the authority and the task stays open until it is green on the SHA.
- Constants, exactly as the spec's table: `teacherMargin = 0.075`, `recallRadius = 0.10` (cosine distance, document template), `recallNeighbours = 5` per pool, `preloadMax = 3`, `recallTimeout = 500 * time.Millisecond`, teacher timeout `reasoningRouterTimeout()` (at most 2 s), `tierNeighbours = 3` (unchanged).
- Effort sources, exactly: `user`, `teacher`, `memory`, `seeds`, `greeting`, `fallback`. Only `user` and `teacher` rows are reusable labels.
- Every effort the agent sends passes through `cfg.ClampReasoningEffort`; this feature has no mapping table of its own. A tier becomes an effort only through `ReasoningTier.Effort`.
- No reasoning text reaches `ConversationTurn` and no credential enters a route key, a log line or a digest.
- Paid runs (the frozen evaluation in Task 7, every lab-VM turn in Task 8) start only after the operator says so in this session.
- Comments only where the why is not obvious; all code, comments and messages in English.

## Review Focus

1. **The same words in a different conversation history.** "ok" after "rewrite this function with tests" and "ok" after "thanks for the help" must not share a label, and the first must not take the greeting fast path. Pinned by `TestTurnContextKeySeparatesDifferentHistories` (Task 5), `TestReadTurnContextIsStandaloneOnlyWithoutConversation` (Task 6) and `TestRecallTurnsLiveExcludesOtherContexts` (Task 3).
2. **The operator switches model in the cockpit between two turns.** A label decided on the old route is not reused; changing only the API key or a header does not change the route. Pinned by `TestRouteKeyNamesTheRouteWithoutItsCredentials` (Task 5) and the route predicate in `TestRecallTurnsDropsIncompatibleRows` (Task 3).
3. **Memory is slow or down (ArcadeDB unreachable, embedder outage).** The turn still decides within `recallTimeout` from seeds or the teacher and logs one warning. Pinned by `TestReadTurnReadsWithoutMemoryWhenRecallHangs` (Task 5).
4. **A remembered tool was unmounted since.** It is not preloaded, and neither is a non-deferred tool or `tool_search`. Pinned by `TestReadTurnPreloadsOnlyRegisteredDeferredTools` (Task 5).
5. **A turn that fails, or a branch re-run.** Neither writes a decision, so an unsuccessful attempt never becomes a label and an older user row is never labelled by a newer run. Pinned by `TestFailedTurnRecordsNoDecision` and `TestBranchRerunRecordsNoDecision` (Task 6).

## Decisions taken while writing this plan

Each is a reading of the spec against the code, recorded so a reviewer can reject it.

- **The decision reaches the runner through a callback, not an event.** `agent.TurnReading.OnDecision` is called once, synchronously, from the run loop before the first request. An `Actions` field would travel to every consumer (AG-UI translator, Telegram, CLI renderer, swarm parents) that has no use for it.
- **The runner computes the context key** (`readTurnContext`, Task 6), because only it knows which history messages are the always-block (governing instructions: in the key) and the per-turn memory block (volatile: out of the key, as the plan-1 carryover required), and how the current message was composed. The digest itself is `agent.TurnContextKey`, because the system prompt it covers is the agent's.
- **Attachments make a turn ineligible for recall.** The spec allows an unversioned referenced input to make a turn ineligible; current-turn native media are not versioned in the key, so a turn with attachments gets no key and no reusable label in this first release.
- **The knowledge catalog block is part of the key.** It rides the current message (`assets.WithContextBlocks`), so the key covers the composed message minus the typed text. A library change therefore changes the key.
- **Only a run with a dispatched user message reads memory and writes a decision.** A resumed run and a branch re-run have no dispatched user row, so they decide from seeds and the teacher as today and write nothing.
- **The decision is written when the round first reaches a durable stop: its answer, or its pause.** A failed round writes nothing. A paused turn writes at the pause flush, and its post-resume tools still reach the graph through the trace anchored on the eventual answer.
- **Labels reach ArcadeDB through the periodic reconciliation.** The user row is projected when it is appended, before any decision exists; the decision updates the projected row on the next `Reconcile` replay (spec: "A late successful write is recovered by periodic replay"). Task 8 measures that lag.
- **A recall miss is named per turn, not per row.** `recall_miss` is `no_memory`, `context_ineligible`, `no_text`, `recall_error` or `no_compatible_label`. Compatibility (context, route, policy, source) is filtered inside each pool before top-k (Task 3), so there is no rejected row whose reason could be logged.
- **The teacher's cancel outcome is spelled `canceled`**, the value the obs catalog's bounded outcome set already holds (the spec's prose writes "cancelled").
- **The frozen evaluation measures the effort decision only** (Task 7). Task success, tool use, unused preloads and the paired latency/cost gate are measured on the lab-VM workload (Task 8, Step 9). BM25 versus BM25+dense, MCP schema changes and concurrent index builds belong to plan 3's dense leg.
- **Route change, embedder outage and process restart run on the VM only with the operator's answer** (Task 8, Step 8.6): each disrupts the operator's live appliance. Teacher timeout and memory timeout are pinned by unit tests (`TestAskTeacherNamesEveryOutcomeAndCountsIt`, `TestReadTurnReadsWithoutMemoryWhenRecallHangs`).
- **Out of scope, carried to a later plan with their evidence:** the tools a run ran before pausing on `ask_user` and the tools of a failed run never reach the graph (measured on the lab VM, 2026-10-06); the parked set-read `LIMIT` truncation and the summary-before-cap write in `appendReasoning` (plan 1's final review).

## File structure

| File | Task | Responsibility |
|---|---|---|
| `internal/db/migrations/0137_conversation_turn_decision.{up,down}.sql` | 1 | Seven nullable provenance columns + source CHECK |
| `internal/db/queries/conversation_turns.sql` | 1 | `RecordConversationTurnDecision`; `ListTurnDump` reads the new columns |
| `internal/conversations/store_turn_decision.go` (new) | 1 | `TurnDecision`, `AppendTurnSeq`, `RecordTurnDecision` |
| `internal/conversations/store_append.go`, `store_projection.go`, `store_dump.go`, `dump_markdown.go` | 1 | Seq return, projection read, raw export |
| `internal/arcadedb/memory_conversation.go` | 2 | `ConversationTurn` routing properties, upsert incl. null clearing |
| `internal/runner/runner_memory_projection.go` | 2 | Projection copies the decision |
| `internal/arcadedb/embedding_space.go` | 3 | `embedOne`, the space-checked single embedding `denseQueryVector` and recall share |
| `internal/arcadedb/turn_recall.go` (new) | 3 | `RecallTurns` and its three pools |
| `internal/agent/prompt/reasoning_classifier.go`, `reasoning_policy.go`, `builder.go` | 4 | Margin, `IsTrivialGreeting`, `ReasoningTier.Effort`, `ApplyAdaptiveEffort`, policy fingerprint |
| `internal/agent/llm_agent_reasoning.go` | 4, 5 | `askTeacher` and its outcomes |
| `internal/obs/catalog.go`, `internal/agent/metrics.go` | 4, 5 | Teacher-attempt and decision-source counters |
| `internal/agent/turn_recall.go` (new) | 5 | Port types, effort sources, `TurnContextKey`, `routeKey`, policy version |
| `internal/agent/llm_agent_turn_reading.go` (new) | 5 | `readTurn`: the decision table, recall, preload, log line |
| `internal/runner/runner_persist_pause.go` (new) | 6 | Pause persistence moved out of `runner_persist.go` |
| `internal/runner/runner_turn_recall.go` (new) | 6 | `readTurnContext`, recall binding, `recordTurnDecision` |
| `cmd/aura/chat_boot_memory.go`, `cmd/aura/chat_boot.go`, `cmd/aura/cachefakes.go` | 6 | `tenantTurnRecall`, wiring, the cache-audit fake's `AppendTurnSeq` |
| `internal/agent/testdata/turn_recall_frozen_2026-10-07.json` (new), `docs/verification/turn-recall-frozen-eval.md` (new) | 7 | Frozen set and protocol |
| `internal/agent/turn_recall_eval_test.go` (new) | 7 | Replay harness, tag `turn_recall_eval` |
| `prd.md` | 8 | Amendment after the measurement |

---

### Task 1: Decision provenance in Postgres

**Files:**
- Create: `internal/db/migrations/0137_conversation_turn_decision.up.sql`
- Create: `internal/db/migrations/0137_conversation_turn_decision.down.sql`
- Modify: `internal/db/queries/conversation_turns.sql` (new query `RecordConversationTurnDecision`; `ListTurnDump` columns)
- Regenerate: `internal/db/sqlc/` (`W 'sqlc generate'`)
- Create: `internal/conversations/store_turn_decision.go`
- Modify: `internal/conversations/store_append.go` (`AppendTurn` delegates to `AppendTurnSeq`)
- Modify: `internal/conversations/store_projection.go` (`ProjectionTurn.Decision`, SELECT)
- Modify: `internal/conversations/store_dump.go` (`DumpTurn.Decision`), `internal/conversations/dump_markdown.go` (one line per decided turn)
- Create: `internal/db/migrate_0137_integration_test.go`
- Create: `internal/conversations/store_turn_decision_test.go` (tag `db_integration`)
- Modify: `internal/conversations/dump_markdown_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `conversations.TurnDecision{ContextKey, Effort, EffortRequested, EffortSource, RouteKey, PolicyVersion, OriginRef string}` — field order is load-bearing: `arcadedb.TurnDecision` (Task 2) declares the same fields in the same order and the projector converts one into the other.
  - `func (s *Store) AppendTurnSeq(ctx context.Context, p AppendTurnParams) (int, error)`
  - `func (s *Store) RecordTurnDecision(ctx context.Context, conversationID string, seq int, d TurnDecision) error`
  - `var ErrTurnDecisionTarget error`
  - `ProjectionTurn.Decision TurnDecision`, `DumpTurn.Decision TurnDecision`

- [ ] **Step 1: Confirm the migration slot**

Run: `ls internal/db/migrations/ | tail -1`
Expected: `0136_assets_drop_searchable.up.sql`. If anything later is there, use the next free number in every file name and test below.

- [ ] **Step 2: Write the failing migration test**

Create `internal/db/migrate_0137_integration_test.go`:

```go
//go:build db_integration

package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var turnDecisionColumns = []string{
	"recall_context_key", "reasoning_effort", "reasoning_effort_requested", "reasoning_effort_source",
	"reasoning_effort_route_key", "reasoning_effort_policy_version", "reasoning_effort_origin_ref",
}

// 0137 adds the seven columns turn recall persists a decision in (spec 2026-10-06, "Persisting
// what was learned"). All nullable and never backfilled: a row without provenance can never
// become a reusable label.
func TestMigrate0137AddsTheTurnDecisionColumns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0137_decision")
	migrateToVersion(t, ctx, migrateURL, admin, 136)
	for _, column := range turnDecisionColumns {
		if turnColumnExists(t, ctx, admin, column) {
			t.Fatalf("%s exists before 0137", column)
		}
	}

	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0137 up: %v", err)
	}
	for _, column := range turnDecisionColumns {
		if !turnColumnExists(t, ctx, admin, column) {
			t.Errorf("%s missing after 0137", column)
		}
	}
	var definition string
	if err := admin.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conname = 'conversation_turns_reasoning_effort_source_check'`).Scan(&definition); err != nil {
		t.Fatalf("read the source CHECK: %v", err)
	}
	for _, source := range []string{"user", "teacher", "memory", "seeds", "greeting", "fallback"} {
		if !strings.Contains(definition, "'"+source+"'") {
			t.Errorf("source CHECK %q does not admit %q", definition, source)
		}
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate 0137 down: %v", err)
	}
	for _, column := range turnDecisionColumns {
		if turnColumnExists(t, ctx, admin, column) {
			t.Errorf("%s survived 0137 down", column)
		}
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate 0137 up again: %v", err)
	}
}

func turnColumnExists(t *testing.T, ctx context.Context, admin *pgxpool.Pool, column string) bool {
	t.Helper()
	var exists bool
	if err := admin.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'aura' AND table_name = 'conversation_turns' AND column_name = $1)`, column).Scan(&exists); err != nil {
		t.Fatalf("read column %s: %v", column, err)
	}
	return exists
}
```

- [ ] **Step 3: Run it to verify it fails**

Run recipe **P** with `-run TestMigrate0137 ./internal/db/`.
Expected: FAIL — `MigrateSteps(ctx, migrateURL, 1)` reports no migration to apply (or the columns stay missing).

- [ ] **Step 4: Write the migration**

Create `internal/db/migrations/0137_conversation_turn_decision.up.sql`:

```sql
-- Turn recall (docs/superpowers/specs/2026-10-06-turn-recall-design.md, "Persisting what was
-- learned"): every user turn records how its reasoning effort was decided, so a later
-- paraphrase can reuse a decision instead of guessing again. Slot measured with
-- `ls internal/db/migrations/ | tail -1`: 0136 was the head.
--
-- All nullable and nothing is backfilled: a row without provenance can never become a
-- reusable label, which is the point. NULL means "no effort field"; the explicit effort
-- `none` is stored as 'none'. Only `user` and `teacher` decisions are labels; the other four
-- sources record how a turn was decided and are never copied.

ALTER TABLE aura.conversation_turns
    ADD COLUMN recall_context_key text,
    ADD COLUMN reasoning_effort text,
    ADD COLUMN reasoning_effort_requested text,
    ADD COLUMN reasoning_effort_source text,
    ADD COLUMN reasoning_effort_route_key text,
    ADD COLUMN reasoning_effort_policy_version text,
    ADD COLUMN reasoning_effort_origin_ref text,
    ADD CONSTRAINT conversation_turns_reasoning_effort_source_check CHECK (
        reasoning_effort_source IS NULL OR reasoning_effort_source IN (
            'user', 'teacher', 'memory', 'seeds', 'greeting', 'fallback'
        )
    );

COMMENT ON COLUMN aura.conversation_turns.recall_context_key IS
    'Digest of what the model read before this user message (turn recall). NULL = not eligible for recall.';
COMMENT ON COLUMN aura.conversation_turns.reasoning_effort IS
    'Effort sent on the first accepted main request, after the clamp. NULL = no effort field.';
COMMENT ON COLUMN aura.conversation_turns.reasoning_effort_requested IS
    'Effort the decision asked for, before the clamp. A reused label is clamped again, never inferred from reasoning_effort.';
COMMENT ON COLUMN aura.conversation_turns.reasoning_effort_source IS
    'user, teacher, memory, seeds, greeting or fallback. Only user and teacher rows are reusable labels.';
```

Create `internal/db/migrations/0137_conversation_turn_decision.down.sql`:

```sql
ALTER TABLE aura.conversation_turns
    DROP CONSTRAINT conversation_turns_reasoning_effort_source_check,
    DROP COLUMN reasoning_effort_origin_ref,
    DROP COLUMN reasoning_effort_policy_version,
    DROP COLUMN reasoning_effort_route_key,
    DROP COLUMN reasoning_effort_source,
    DROP COLUMN reasoning_effort_requested,
    DROP COLUMN reasoning_effort,
    DROP COLUMN recall_context_key;
```

- [ ] **Step 5: Run the migration test to verify it passes**

Run recipe **P** with `-run TestMigrate0137 ./internal/db/`.
Expected: PASS.

- [ ] **Step 6: Add the write query and widen the dump query**

In `internal/db/queries/conversation_turns.sql`, append after the `SetTurnBranchPointers` query:

```sql
-- name: RecordConversationTurnDecision :execrows
-- Turn recall (migration 0137): one user row's decision provenance, all seven fields in one
-- statement so a retried write never leaves half of one decision beside half of another.
-- The row is addressed by the seq the runner kept when it appended the user turn, never by
-- "newest user turn", and RLS scopes it to the caller's identity. role = 'user' makes a
-- wrong seq a zero-row update rather than a label on an assistant turn.
UPDATE aura.conversation_turns
SET recall_context_key              = sqlc.narg(recall_context_key),
    reasoning_effort                = sqlc.narg(reasoning_effort),
    reasoning_effort_requested      = sqlc.narg(reasoning_effort_requested),
    reasoning_effort_source         = sqlc.narg(reasoning_effort_source),
    reasoning_effort_route_key      = sqlc.narg(reasoning_effort_route_key),
    reasoning_effort_policy_version = sqlc.narg(reasoning_effort_policy_version),
    reasoning_effort_origin_ref     = sqlc.narg(reasoning_effort_origin_ref)
WHERE conversation_id = sqlc.arg(conversation_id)
  AND seq = sqlc.arg(seq)
  AND role = 'user';
```

In the same file, replace the `ListTurnDump` SELECT list so it reads:

```sql
SELECT seq, role, content, content_sidecar_path, tool_call_id, tool_calls,
       reasoning, reasoning_duration_ms, branch_id, parent_seq, attachment_ids,
       delivery_key, input_tokens, output_tokens, cached_tokens, context_tokens, created_at,
       recall_context_key, reasoning_effort, reasoning_effort_requested, reasoning_effort_source,
       reasoning_effort_route_key, reasoning_effort_policy_version, reasoning_effort_origin_ref
```

Run: `W 'sqlc generate && git diff --stat internal/db/sqlc/'`
Expected: `models.go`, `conversation_turns.sql.go` and `querier.go` change; `RecordConversationTurnDecisionParams` has `RecallContextKey`, `ReasoningEffort`, `ReasoningEffortRequested`, `ReasoningEffortSource`, `ReasoningEffortRouteKey`, `ReasoningEffortPolicyVersion`, `ReasoningEffortOriginRef` (all `pgtype.Text`), `ConversationID pgtype.UUID` and `Seq int32`; `ListTurnDumpRow` gains the same seven `pgtype.Text` fields. If a generated name differs, use the generated one below and say so in the report.

- [ ] **Step 7: Write the failing store tests**

Create `internal/conversations/store_turn_decision_test.go`:

```go
//go:build db_integration

package conversations

import (
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/jackc/pgx/v5/pgtype"
)

func appendSeq(t *testing.T, s *Store, convID, role, content string) int {
	t.Helper()
	seq, err := s.AppendTurnSeq(ownerCtx(), AppendTurnParams{ConversationID: convID, Role: role, Content: content})
	if err != nil {
		t.Fatalf("AppendTurnSeq %s: %v", role, err)
	}
	return seq
}

func storedDecision(t *testing.T, s *Store, convID string, seq int) (TurnDecision, bool) {
	t.Helper()
	var key, effort, requested, source, route, policy, origin pgtype.Text
	if err := s.pool.QueryRow(ownerCtx(), `SELECT recall_context_key, reasoning_effort,
		reasoning_effort_requested, reasoning_effort_source, reasoning_effort_route_key,
		reasoning_effort_policy_version, reasoning_effort_origin_ref
		FROM aura.conversation_turns WHERE conversation_id = $1 AND seq = $2`, convID, seq).
		Scan(&key, &effort, &requested, &source, &route, &policy, &origin); err != nil {
		t.Fatalf("read decision of seq %d: %v", seq, err)
	}
	anySet := key.Valid || effort.Valid || requested.Valid || source.Valid || route.Valid || policy.Valid || origin.Valid
	return TurnDecision{
		ContextKey: key.String, Effort: effort.String, EffortRequested: requested.String,
		EffortSource: source.String, RouteKey: route.String, PolicyVersion: policy.String, OriginRef: origin.String,
	}, anySet
}

func TestAppendTurnSeqReturnsTheSeqItStored(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := newConversation(t, s)
	first := appendSeq(t, s, convID, llm.RoleUser, "first question")
	second := appendSeq(t, s, convID, llm.RoleAssistant, "first answer")
	if second != first+1 {
		t.Fatalf("seqs = %d then %d, want consecutive", first, second)
	}
	var role string
	if err := s.pool.QueryRow(ownerCtx(), `SELECT role FROM aura.conversation_turns
		WHERE conversation_id = $1 AND seq = $2`, convID, first).Scan(&role); err != nil || role != llm.RoleUser {
		t.Fatalf("row at returned seq %d = %q, %v; want the user turn", first, role, err)
	}
}

// The runner writes the decision on the row it dispatched, never on whatever user turn is
// newest by the time the answer commits (spec, "The write").
func TestRecordTurnDecisionWritesOnlyTheAddressedUserRow(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := newConversation(t, s)
	older := appendSeq(t, s, convID, llm.RoleUser, "che tempo fa domani?")
	appendSeq(t, s, convID, llm.RoleAssistant, "Sereno.")
	newer := appendSeq(t, s, convID, llm.RoleUser, "e dopodomani?")

	decision := TurnDecision{
		ContextKey: "ctx1:aa", Effort: "none", EffortRequested: "none", EffortSource: "teacher",
		RouteKey: "route1:bb", PolicyVersion: "policy1:cc",
	}
	if err := s.RecordTurnDecision(ownerCtx(), convID, older, decision); err != nil {
		t.Fatalf("RecordTurnDecision: %v", err)
	}
	got, _ := storedDecision(t, s, convID, older)
	if got != decision {
		t.Fatalf("stored decision = %+v, want %+v (the explicit effort none must be stored as 'none')", got, decision)
	}
	if _, anySet := storedDecision(t, s, convID, newer); anySet {
		t.Fatal("the newer user row was labelled by a decision addressed to the older one")
	}
}

func TestRecordTurnDecisionKeepsTheContextWithoutAnEffort(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := newConversation(t, s)
	seq := appendSeq(t, s, convID, llm.RoleUser, "manda un messaggio a Luca")
	if err := s.RecordTurnDecision(ownerCtx(), convID, seq, TurnDecision{ContextKey: "ctx1:dd"}); err != nil {
		t.Fatalf("RecordTurnDecision: %v", err)
	}
	var effort pgtype.Text
	if err := s.pool.QueryRow(ownerCtx(), `SELECT reasoning_effort FROM aura.conversation_turns
		WHERE conversation_id = $1 AND seq = $2`, convID, seq).Scan(&effort); err != nil {
		t.Fatal(err)
	}
	if effort.Valid {
		t.Fatalf("reasoning_effort = %q, want NULL when no effort field was decided", effort.String)
	}
	if got, _ := storedDecision(t, s, convID, seq); got.ContextKey != "ctx1:dd" {
		t.Fatalf("recall_context_key = %q, want the context kept for tool examples", got.ContextKey)
	}
}

func TestRecordTurnDecisionRefusesAnAssistantRowAndAnUnknownSource(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := newConversation(t, s)
	user := appendSeq(t, s, convID, llm.RoleUser, "ciao")
	assistant := appendSeq(t, s, convID, llm.RoleAssistant, "Ciao!")
	err := s.RecordTurnDecision(ownerCtx(), convID, assistant, TurnDecision{EffortSource: "seeds"})
	if !errors.Is(err, ErrTurnDecisionTarget) {
		t.Fatalf("decision on an assistant row = %v, want ErrTurnDecisionTarget", err)
	}
	if err := s.RecordTurnDecision(ownerCtx(), convID, user, TurnDecision{EffortSource: "guess"}); err == nil {
		t.Fatal("an unknown source passed the CHECK")
	}
}

func TestProjectionAndDumpCarryTheDecision(t *testing.T) {
	s := newStore(t, migratedPool(t))
	convID := newConversation(t, s)
	user := appendSeq(t, s, convID, llm.RoleUser, "scrivi uno script che ruota i log")
	appendSeq(t, s, convID, llm.RoleAssistant, "Ecco lo script.")
	decision := TurnDecision{
		ContextKey: "ctx1:ee", Effort: "high", EffortRequested: "high", EffortSource: "seeds",
		RouteKey: "route1:ff", PolicyVersion: "policy1:gg",
	}
	if err := s.RecordTurnDecision(ownerCtx(), convID, user, decision); err != nil {
		t.Fatal(err)
	}

	turns, _, err := s.ListProjectionTurns(ownerCtx(), localID, ProjectionCursor{}, 1000)
	if err != nil {
		t.Fatalf("ListProjectionTurns: %v", err)
	}
	found := 0
	for _, turn := range turns {
		if turn.ConversationID != convID {
			continue
		}
		found++
		switch turn.Role {
		case llm.RoleUser:
			if turn.Decision != decision {
				t.Errorf("projected user decision = %+v, want %+v", turn.Decision, decision)
			}
		default:
			if turn.Decision != (TurnDecision{}) {
				t.Errorf("projected assistant decision = %+v, want none", turn.Decision)
			}
		}
	}
	if found != 2 {
		t.Fatalf("projected %d turns of the conversation, want 2", found)
	}

	dump, err := s.LoadDump(ownerCtx(), convID)
	if err != nil {
		t.Fatalf("LoadDump: %v", err)
	}
	for _, turn := range dump.Turns {
		if turn.Seq == user && turn.Decision != decision {
			t.Fatalf("dumped decision = %+v, want %+v", turn.Decision, decision)
		}
	}
}
```

- [ ] **Step 8: Run them to verify they fail**

Run recipe **P** with `-run 'TestAppendTurnSeq|TestRecordTurnDecision|TestProjectionAndDumpCarry' ./internal/conversations/`.
Expected: FAIL to compile — `AppendTurnSeq`, `TurnDecision`, `RecordTurnDecision`, `ErrTurnDecisionTarget`, `ProjectionTurn.Decision` undefined.

- [ ] **Step 9: Return the seq from the append**

In `internal/conversations/store_append.go`, rename the body of `AppendTurn` into `AppendTurnSeq` and make `AppendTurn` delegate. The method becomes:

```go
// AppendTurn writes one turn AND folds its token/cost delta into the conversation
// aggregates in a SINGLE db.WithCallerIdentityTx transaction (SC-2) — see AppendTurnSeq,
// which it is, without the seq.
func (s *Store) AppendTurn(ctx context.Context, p AppendTurnParams) error {
	_, err := s.AppendTurnSeq(ctx, p)
	return err
}

// AppendTurnSeq writes one turn AND folds its token/cost delta into the conversation
// aggregates in a SINGLE db.WithCallerIdentityTx transaction (SC-2): a failure between the turn
// INSERT and the aggregates UPDATE rolls the whole thing back, leaving no partial
// turn. When Seq <= 0, the Store row-locks the parent conversation and allocates
// MAX(seq)+1 inside the same tx so concurrent appenders cannot race. Content over
// turnCapBytes spills to a sidecar file once the final seq is known (the file write
// is not part of the DB atomicity); cleanupSidecarOnTxError removes a just-spilled
// file when the tx rolls back (M-04), and a boot scan reconciles any leftover. The
// row then stores content=NULL + content_sidecar_path.
//
// It returns the seq the turn was stored at. The runner keeps the seq of the user turn it
// dispatches, so that turn's decision (migration 0137) is written to exactly that row. A
// delivery-keyed append that was already delivered stores nothing and still returns the seq
// it would have used; nothing writes a decision on a delivery-keyed turn.
func (s *Store) AppendTurnSeq(ctx context.Context, p AppendTurnParams) (int, error) {
	if p.Seq > 0 {
		turn, agg, err := s.appendTurnWrites(p)
		if err != nil {
			return 0, err
		}
		if err := cleanupSidecarOnTxError(
			func() error {
				return db.WithCallerIdentityTx(ctx, s.pool, func(q *sqlc.Queries) error {
					return insertTurnAndAggregates(ctx, q, turn, agg)
				})
			},
			func() string { return turn.ContentSidecarPath.String },
		); err != nil {
			return 0, fmt.Errorf("append turn %s seq %d: %w", p.ConversationID, p.Seq, err)
		}
		return p.Seq, nil
	}

	// Seq is allocated inside the tx, so the sidecar (keyed by seq) is spilled there
	// too; record the spilled path so a rollback removes the orphaned file.
	var spilledPath string
	if err := cleanupSidecarOnTxError(
		func() error {
			return db.WithCallerIdentityTx(ctx, s.pool, func(q *sqlc.Queries) error {
				seq, err := s.allocateTurnSeq(ctx, q, p.ConversationID)
				if err != nil {
					return err
				}
				p.Seq = seq
				turn, agg, err := s.appendTurnWrites(p)
				if err != nil {
					return err
				}
				spilledPath = turn.ContentSidecarPath.String
				return insertTurnAndAggregates(ctx, q, turn, agg)
			})
		},
		func() string { return spilledPath },
	); err != nil {
		return 0, fmt.Errorf("append turn %s seq %d: %w", p.ConversationID, p.Seq, err)
	}
	return p.Seq, nil
}
```

Delete the old doc comment above the former `AppendTurn` (its text now lives on `AppendTurnSeq`).

- [ ] **Step 10: Write the decision store**

Create `internal/conversations/store_turn_decision.go`:

```go
package conversations

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrTurnDecisionTarget is returned when no user turn sits at the seq a decision addresses.
var ErrTurnDecisionTarget = errors.New("conversations: no user turn at the decision's seq")

// TurnDecision is how a user turn's reasoning effort was decided (migration 0137). Every
// field is optional and "" is NULL, which is how a turn with no effort field differs from
// one that recorded the explicit effort `none`. The source vocabulary is the column's CHECK.
//
// arcadedb.TurnDecision declares the same fields in the same order: the projector converts
// one into the other, so a field added here must be added there too.
type TurnDecision struct {
	ContextKey      string
	Effort          string
	EffortRequested string
	EffortSource    string
	RouteKey        string
	PolicyVersion   string
	OriginRef       string
}

// RecordTurnDecision writes one user turn's decision provenance in one statement, scoped by
// the caller's identity (RLS) and by the conversation.
func (s *Store) RecordTurnDecision(ctx context.Context, conversationID string, seq int, d TurnDecision) error {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return fmt.Errorf("record turn decision: %w", err)
	}
	if seq <= 0 {
		return fmt.Errorf("record turn decision %s: seq %d: %w", conversationID, seq, ErrTurnDecisionTarget)
	}
	return db.WithCallerIdentityTx(ctx, s.pool, func(q *sqlc.Queries) error {
		n, err := q.RecordConversationTurnDecision(ctx, sqlc.RecordConversationTurnDecisionParams{
			RecallContextKey:             optionalText(d.ContextKey),
			ReasoningEffort:              optionalText(d.Effort),
			ReasoningEffortRequested:     optionalText(d.EffortRequested),
			ReasoningEffortSource:        optionalText(d.EffortSource),
			ReasoningEffortRouteKey:      optionalText(d.RouteKey),
			ReasoningEffortPolicyVersion: optionalText(d.PolicyVersion),
			ReasoningEffortOriginRef:     optionalText(d.OriginRef),
			ConversationID:               id,
			Seq:                          int32(seq),
		})
		if err != nil {
			return fmt.Errorf("record turn decision %s seq %d: %w", conversationID, seq, err)
		}
		if n == 0 {
			return fmt.Errorf("record turn decision %s seq %d: %w", conversationID, seq, ErrTurnDecisionTarget)
		}
		return nil
	})
}

func turnDecisionFromColumns(key, effort, requested, source, route, policy, origin pgtype.Text) TurnDecision {
	return TurnDecision{
		ContextKey: key.String, Effort: effort.String, EffortRequested: requested.String,
		EffortSource: source.String, RouteKey: route.String, PolicyVersion: policy.String, OriginRef: origin.String,
	}
}
```

- [ ] **Step 11: Carry the decision through the projection read and the raw export**

In `internal/conversations/store_projection.go`, add the field to `ProjectionTurn` (last field):

```go
	// Decision is the turn's routing metadata (migration 0137): how its effort was decided
	// and under which context, route and policy. Never reasoning text.
	Decision TurnDecision
```

Replace the query's SELECT list in `ListProjectionTurns` with:

```go
SELECT c.identity_id::text, t.conversation_id::text, t.seq, t.role,
       COALESCE(t.content, ''), COALESCE(t.content_sidecar_path, ''), t.created_at,
       t.recall_context_key, t.reasoning_effort, t.reasoning_effort_requested,
       t.reasoning_effort_source, t.reasoning_effort_route_key,
       t.reasoning_effort_policy_version, t.reasoning_effort_origin_ref
```

and the scan loop with:

```go
		for rows.Next() {
			var identity, conversationID, role, content, sidecarPath string
			var seq int
			var occurredAt time.Time
			var key, effort, requested, source, route, policy, origin pgtype.Text
			if scanErr := rows.Scan(&identity, &conversationID, &seq, &role, &content, &sidecarPath, &occurredAt,
				&key, &effort, &requested, &source, &route, &policy, &origin); scanErr != nil {
				return fmt.Errorf("scan projection turn: %w", scanErr)
			}
```

and add `Decision: turnDecisionFromColumns(key, effort, requested, source, route, policy, origin),` to the `ProjectionTurn` literal appended below it. Add `"github.com/jackc/pgx/v5/pgtype"` to the imports.

In `internal/conversations/store_dump.go`, add `Decision TurnDecision` as the last field of `DumpTurn`, and in `dumpTurnFromRow` add:

```go
		Decision: turnDecisionFromColumns(r.RecallContextKey, r.ReasoningEffort, r.ReasoningEffortRequested,
			r.ReasoningEffortSource, r.ReasoningEffortRouteKey, r.ReasoningEffortPolicyVersion,
			r.ReasoningEffortOriginRef),
```

- [ ] **Step 12: Render the decision in the markdown export**

Write the failing test first. Add to `internal/conversations/dump_markdown_test.go` (its imports already cover `strings`, `testing`, `time`, `llm` and `uuid`):

```go
func TestWriteDumpTurnNamesTheDecision(t *testing.T) {
	var b strings.Builder
	writeDumpTurn(&b, DumpTurn{
		Seq: 3, Role: llm.RoleUser, Content: "che tempo fa?", BranchID: uuid.Nil.String(), ParentSeq: 2,
		CreatedAt: time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC),
		Decision: TurnDecision{
			ContextKey: "ctx1:aa", Effort: "low", EffortRequested: "low", EffortSource: "memory",
			RouteKey: "route1:bb", PolicyVersion: "policy1:cc",
			OriginRef: "postgres://aura/conversations/c/turns/1",
		},
	}, map[string]string{})
	want := "\neffort: `low` (requested `low`) · source `memory` · from `postgres://aura/conversations/c/turns/1`" +
		" · route `route1:bb` · policy `policy1:cc` · context `ctx1:aa`\n"
	if !strings.Contains(b.String(), want) {
		t.Fatalf("dump turn does not name the decision:\n%s", b.String())
	}

	b.Reset()
	writeDumpTurn(&b, DumpTurn{Seq: 4, Role: llm.RoleAssistant, Content: "Sereno.", BranchID: uuid.Nil.String(), ParentSeq: 3}, map[string]string{})
	if strings.Contains(b.String(), "effort:") {
		t.Fatalf("a turn with no decision names one:\n%s", b.String())
	}
}
```

Run: `W 'go test -race -count=1 -run TestWriteDumpTurnNamesTheDecision ./internal/conversations/'`
Expected: FAIL — no `effort:` line.

Then in `internal/conversations/dump_markdown.go`, call `writeTurnDecision(b, t.Decision)` right after the `attachments:` block in `writeDumpTurn`, and add:

```go
// writeTurnDecision names how the turn's effort was decided (migration 0137), and nothing for
// a turn that recorded no decision.
func writeTurnDecision(b *strings.Builder, d TurnDecision) {
	if d == (TurnDecision{}) {
		return
	}
	fmt.Fprintf(b, "\neffort: `%s` (requested `%s`) · source `%s`", d.Effort, d.EffortRequested, d.EffortSource)
	if d.OriginRef != "" {
		fmt.Fprintf(b, " · from `%s`", d.OriginRef)
	}
	fmt.Fprintf(b, " · route `%s` · policy `%s` · context `%s`\n", d.RouteKey, d.PolicyVersion, d.ContextKey)
}
```

- [ ] **Step 13: Run every test of the task**

Run: `W 'time go vet ./internal/conversations/ ./internal/db/ ./internal/runner/ ./cmd/aura/ && time go test -race -count=1 -run "TestWriteDump|TestDumpMarkdown|TestProjectionTurn|TestAppendTurn" ./internal/conversations/'`
Then recipe **P** with `-run 'TestMigrate0137|TestAppendTurnSeq|TestRecordTurnDecision|TestProjectionAndDumpCarry' ./internal/db/ ./internal/conversations/`.
Expected: PASS. The vet of `internal/runner` and `cmd/aura` proves nothing else calls a changed signature.

- [ ] **Step 14: Commit**

```bash
W 'git add internal/db/migrations/0137_conversation_turn_decision.up.sql internal/db/migrations/0137_conversation_turn_decision.down.sql internal/db/migrate_0137_integration_test.go internal/conversations/store_turn_decision.go internal/conversations/store_turn_decision_test.go && LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -q -F <wsl path of the message file> -- internal/db/migrations/0137_conversation_turn_decision.up.sql internal/db/migrations/0137_conversation_turn_decision.down.sql internal/db/migrate_0137_integration_test.go internal/db/queries/conversation_turns.sql internal/db/sqlc internal/conversations/store_turn_decision.go internal/conversations/store_turn_decision_test.go internal/conversations/store_append.go internal/conversations/store_projection.go internal/conversations/store_dump.go internal/conversations/dump_markdown.go internal/conversations/dump_markdown_test.go'
```

Message subject: `feat(conversations): persist how each user turn's effort was decided`.

---
### Task 2: The projection carries the decision to `ConversationTurn`

**Files:**
- Modify: `internal/arcadedb/memory_conversation.go` (type `TurnDecision`, field `ConversationTurnProjection.Decision`, schema, upsert)
- Modify: `internal/runner/runner_memory_projection.go:148-152` (copy the decision)
- Modify: `internal/arcadedb/memory_conversation_test.go`
- Create: `internal/arcadedb/memory_conversation_decision_live_test.go` (tag `arcadedb_integration`)
- Modify: `internal/runner/runner_memory_projection_test.go`

**Interfaces:**
- Consumes: `conversations.TurnDecision` and `conversations.ProjectionTurn.Decision` (Task 1).
- Produces:
  - `arcadedb.TurnDecision{ContextKey, Effort, EffortRequested, EffortSource, RouteKey, PolicyVersion, OriginRef string}` — same fields, same order as `conversations.TurnDecision`.
  - `arcadedb.ConversationTurnProjection.Decision TurnDecision`
  - `ConversationTurn` string properties `recall_context_key`, `effort`, `effort_requested`, `effort_source`, `effort_route_key`, `effort_policy_version`, `effort_origin_ref` (Task 3 reads them).

- [ ] **Step 1: Write the failing unit tests**

In `internal/arcadedb/memory_conversation_test.go`, add these seven names to the `required` list of `TestConversationSchemaStatements`: `"recall_context_key", "effort", "effort_requested", "effort_source", "effort_route_key", "effort_policy_version", "effort_origin_ref"`. Then append:

```go
func decisionProjection(decision TurnDecision) ConversationProjection {
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	return ConversationProjection{
		IdentityID: "identity-a", ConversationID: "conversation-1",
		Turns: []ConversationTurnProjection{
			{
				IdentityID: "identity-a", ConversationID: "conversation-1", Seq: 1, Role: "user",
				Content: "che tempo fa domani?", ContentHash: conversationContentHash("che tempo fa domani?"),
				OccurredAt: at, SourceRef: "postgres://aura/conversations/conversation-1/turns/1", Decision: decision,
			},
			{
				IdentityID: "identity-a", ConversationID: "conversation-1", Seq: 2, Role: "assistant",
				Content: "Sereno.", ContentHash: conversationContentHash("Sereno."),
				OccurredAt: at, SourceRef: "postgres://aura/conversations/conversation-1/turns/2",
			},
		},
	}
}

var turnDecisionProperties = []string{
	"recall_context_key", "effort", "effort_requested", "effort_source",
	"effort_route_key", "effort_policy_version", "effort_origin_ref",
}

// The routing properties are written on every replay, so a decision that reached Postgres
// after the turn was first projected lands on the next reconciliation, and one Postgres
// cleared is cleared here too (spec, "The projection").
func TestConversationProjectionWritesTheTurnDecision(t *testing.T) {
	client, rec := recordingClient(t, `{"result":[]}`)
	decision := TurnDecision{
		ContextKey: "ctx1:aa", Effort: "low", EffortRequested: "low", EffortSource: "teacher",
		RouteKey: "route1:bb", PolicyVersion: "policy1:cc",
	}
	if err := client.ApplyConversationProjection(context.Background(), decisionProjection(decision)); err != nil {
		t.Fatalf("ApplyConversationProjection: %v", err)
	}
	upserts := map[int]map[string]any{}
	for index, statement := range rec.statements {
		if !strings.HasPrefix(statement, "UPDATE "+conversationTurnType+" ") {
			continue
		}
		for _, property := range turnDecisionProperties {
			if !strings.Contains(statement, property+" = :"+property) {
				t.Fatalf("turn upsert does not set %s:\n%s", property, statement)
			}
		}
		seq, _ := rec.params[index]["turn_seq"].(float64)
		upserts[int(seq)] = rec.params[index]
	}
	user, assistant := upserts[1], upserts[2]
	if user == nil || assistant == nil {
		t.Fatalf("turn upserts by seq = %v, want seq 1 and 2", upserts)
	}
	for property, want := range map[string]any{
		"recall_context_key": "ctx1:aa", "effort": "low", "effort_requested": "low", "effort_source": "teacher",
		"effort_route_key": "route1:bb", "effort_policy_version": "policy1:cc", "effort_origin_ref": nil,
	} {
		if got, ok := user[property]; !ok || got != want {
			t.Errorf("user turn %s = %v (bound %v), want %v", property, got, ok, want)
		}
	}
	for _, property := range turnDecisionProperties {
		if got, ok := assistant[property]; !ok || got != nil {
			t.Errorf("assistant turn %s = %v (bound %v), want an explicit NULL", property, got, ok)
		}
	}
}
```

In `internal/runner/runner_memory_projection_test.go`, append:

```go
func TestConversationProjectorCarriesTheTurnDecision(t *testing.T) {
	decision := conversations.TurnDecision{
		ContextKey: "ctx1:aa", Effort: "high", EffortRequested: "high", EffortSource: "seeds",
		RouteKey: "route1:bb", PolicyVersion: "policy1:cc",
	}
	content := "scrivi uno script che ruota i log"
	sum := sha256.Sum256([]byte(content))
	source := &reconciliationProjectionSource{turns: []conversations.ProjectionTurn{{
		IdentityID: "identity-a", ConversationID: "conversation-1", Seq: 1, Role: "user",
		Content: content, ContentHash: hex.EncodeToString(sum[:]), OccurredAt: time.Now().UTC(),
		SourceRef: "postgres://aura/conversations/conversation-1/turns/1", Decision: decision,
	}}}
	sink := newReconciliationProjectionSink()
	projector := NewConversationProjector(source, sink, 16)
	t.Cleanup(func() { _ = projector.Close(context.Background()) })

	if _, err := projector.ProjectPage(context.Background(), "identity-a", conversations.ProjectionCursor{}); err != nil {
		t.Fatalf("ProjectPage: %v", err)
	}
	if got := sink.turns["identity-a/conversation-1/1"].Decision; got != arcadedb.TurnDecision(decision) {
		t.Fatalf("projected decision = %+v, want %+v", got, decision)
	}
}
```

(The file already imports `crypto/sha256`, `encoding/hex`, `time`, `arcadedb` and `conversations`; add any that are missing.)

- [ ] **Step 2: Run them to verify they fail**

Run: `W 'go test -race -count=1 -run "TestConversationSchemaStatements|TestConversationProjectionWritesTheTurnDecision" ./internal/arcadedb/ && go test -race -count=1 -run TestConversationProjectorCarriesTheTurnDecision ./internal/runner/'`
Expected: FAIL to compile — `TurnDecision`, `ConversationTurnProjection.Decision` undefined.

- [ ] **Step 3: Add the type, the properties and the upsert**

In `internal/arcadedb/memory_conversation.go`, add after the `ConversationTurnProjection` type, and add `Decision TurnDecision` as that type's last field:

```go
// TurnDecision is the routing metadata a user turn carries into the graph (spec 2026-10-06,
// "Persisting what was learned"): how its effort was decided and under which context, route
// and policy. Strings only and never reasoning text. Postgres is authoritative; a replay
// rewrites every field, so a rebuild keeps the provenance and a field cleared there is
// cleared here.
//
// conversations.TurnDecision declares the same fields in the same order: the projector
// converts one into the other, so a field added here must be added there too.
type TurnDecision struct {
	ContextKey      string
	Effort          string
	EffortRequested string
	EffortSource    string
	RouteKey        string
	PolicyVersion   string
	OriginRef       string
}

// bind sets every routing parameter, to NULL where the decision has nothing.
func (d TurnDecision) bind(params map[string]any) {
	params["recall_context_key"] = nullableString(d.ContextKey)
	params["effort"] = nullableString(d.Effort)
	params["effort_requested"] = nullableString(d.EffortRequested)
	params["effort_source"] = nullableString(d.EffortSource)
	params["effort_route_key"] = nullableString(d.RouteKey)
	params["effort_policy_version"] = nullableString(d.PolicyVersion)
	params["effort_origin_ref"] = nullableString(d.OriginRef)
}
```

In `conversationSchemaStatements`, after the `embedding` property line of `ConversationTurn`, add:

```go
		"CREATE PROPERTY " + conversationTurnType + ".recall_context_key IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationTurnType + ".effort IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationTurnType + ".effort_requested IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationTurnType + ".effort_source IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationTurnType + ".effort_route_key IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationTurnType + ".effort_policy_version IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationTurnType + ".effort_origin_ref IF NOT EXISTS STRING",
		// Every recall pool starts from the turns that share the reader's prior context.
		"CREATE INDEX IF NOT EXISTS ON " + conversationTurnType + " (recall_context_key) NOTUNIQUE",
```

Replace `upsertConversationTurnStatement` with:

```go
const upsertConversationTurnStatement = "UPDATE " + conversationTurnType +
	" SET identity_id = :identity_id, conversation_id = :conversation_id," +
	" turn_seq = :turn_seq, role = :role, content = :content," +
	" content_hash = :content_hash, occurred_at = :occurred_at," +
	" source_ref = :source_ref, deleted_at = NULL," +
	" recall_context_key = :recall_context_key, effort = :effort, effort_requested = :effort_requested," +
	" effort_source = :effort_source, effort_route_key = :effort_route_key," +
	" effort_policy_version = :effort_policy_version, effort_origin_ref = :effort_origin_ref"
```

In `ApplyConversationProjection`, right after the `turnParams` literal, add `turn.Decision.bind(turnParams)`.

In `internal/runner/runner_memory_projection.go`, add `Decision: arcadedb.TurnDecision(turn.Decision),` to the `arcadedb.ConversationTurnProjection` literal in `applyTurns`.

- [ ] **Step 4: Run the unit tests to verify they pass**

Run: `W 'time go vet ./internal/arcadedb/ ./internal/runner/ && time go test -race -count=1 -run "TestConversationSchema|TestConversationProjection" ./internal/arcadedb/ && time go test -race -count=1 -run TestConversationProjector ./internal/runner/'`
Expected: PASS.

- [ ] **Step 5: Write the live test**

Create `internal/arcadedb/memory_conversation_decision_live_test.go`:

```go
//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"testing"
	"time"
)

// A decision reaches Postgres after its turn was first projected, so the update that carries
// it changes no content: it must still land, keep the vector, and clear what Postgres clears.
func TestConversationProjectionLiveUpdatesTheDecisionOverUnchangedContent(t *testing.T) {
	ctx := context.Background()
	client := disposableMemoryClient(t).WithEmbedder(constantEmbedder{value: 1, space: "es1-decision-live"})
	content := "che tempo fa domani a Cuneo?"
	turn := ConversationTurnProjection{
		IdentityID: "identity-a", ConversationID: "conversation-1", Seq: 1, Role: "user",
		Content: content, ContentHash: conversationContentHash(content), OccurredAt: time.Now().UTC(),
		SourceRef: "postgres://aura/conversations/conversation-1/turns/1",
	}
	apply := func(decision TurnDecision) map[string]any {
		t.Helper()
		turn.Decision = decision
		if err := client.ApplyConversationProjection(ctx, ConversationProjection{
			IdentityID: "identity-a", ConversationID: "conversation-1", Turns: []ConversationTurnProjection{turn},
		}); err != nil {
			t.Fatalf("ApplyConversationProjection: %v", err)
		}
		rows, err := client.Query(ctx, "SELECT recall_context_key, effort, effort_requested, effort_source,"+
			" effort_route_key, effort_policy_version, effort_origin_ref, embed_space FROM "+conversationTurnType+
			" WHERE identity_id = 'identity-a' AND conversation_id = 'conversation-1' AND turn_seq = 1", map[string]any{})
		if err != nil || len(rows) != 1 {
			t.Fatalf("read the projected turn = %v, %v", rows, err)
		}
		return rows[0]
	}

	if got := apply(TurnDecision{}); got["effort_source"] != nil {
		t.Fatalf("an undecided turn carries effort_source %v", got["effort_source"])
	}
	decided := TurnDecision{
		ContextKey: "ctx1:aa", Effort: "low", EffortRequested: "low", EffortSource: "teacher",
		RouteKey: "route1:bb", PolicyVersion: "policy1:cc", OriginRef: "postgres://aura/conversations/c0/turns/1",
	}
	got := apply(decided)
	for property, want := range map[string]any{
		"recall_context_key": "ctx1:aa", "effort": "low", "effort_requested": "low", "effort_source": "teacher",
		"effort_route_key": "route1:bb", "effort_policy_version": "policy1:cc",
		"effort_origin_ref": "postgres://aura/conversations/c0/turns/1",
	} {
		if got[property] != want {
			t.Errorf("%s = %v after the decision update, want %v", property, got[property], want)
		}
	}
	if got["embed_space"] != "es1-decision-live" {
		t.Fatalf("embed_space = %v: the decision-only update lost the turn's vector", got["embed_space"])
	}
	got = apply(TurnDecision{})
	for _, property := range turnDecisionProperties {
		if got[property] != nil {
			t.Errorf("%s = %v after Postgres cleared it", property, got[property])
		}
	}
}
```

- [ ] **Step 6: Run the live test**

Run recipe **A** with `-run TestConversationProjectionLive`.
Expected: PASS (or, without a local ArcadeDB, green in CI's `agent-memory-eval` job before the task closes).

- [ ] **Step 7: Commit**

```bash
W 'git add internal/arcadedb/memory_conversation_decision_live_test.go && LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -q -F <wsl path of the message file> -- internal/arcadedb/memory_conversation.go internal/arcadedb/memory_conversation_test.go internal/arcadedb/memory_conversation_decision_live_test.go internal/runner/runner_memory_projection.go internal/runner/runner_memory_projection_test.go'
```

Message subject: `feat(memory): project each turn's effort decision onto ConversationTurn`.

---

### Task 3: `RecallTurns` and its three pools

**Files:**
- Modify: `internal/arcadedb/embedding_space.go` (extract `embedOne` from `denseQueryVector`)
- Create: `internal/arcadedb/turn_recall.go`
- Create: `internal/arcadedb/turn_recall_test.go`
- Create: `internal/arcadedb/turn_recall_live_test.go` (tag `arcadedb_integration`)

**Interfaces:**
- Consumes: the `ConversationTurn` routing properties (Task 2); the reasoning graph edges `NEXT_TURN`, `INITIATED_BY`, `HAS_STEP`, `INVOKED` and `ReasoningToolCall.status`/`tool_name` (plan 1).
- Produces:
  - `type TurnRecallRequest struct { IdentityID, Text, ContextKey, RouteKey, PolicyVersion, SourceRef string; DeferredTools []string; IncludeLabels bool }`
  - `type RecalledTurn struct { Distance float64; SourceRef, Effort, RequestedEffort, EffortSource, ContextKey, RouteKey, PolicyVersion, OriginRef string; Tools []string }` — field order is load-bearing: `agent.RecalledTurn` (Task 5) declares the same fields in the same order and the runner converts one into the other.
  - `type TurnRecall struct { UserLabels, TeacherLabels, ToolTurns []RecalledTurn }`
  - `func (c *Client) RecallTurns(ctx context.Context, request TurnRecallRequest) (TurnRecall, error)`

- [ ] **Step 1: Write the failing unit tests**

Create `internal/arcadedb/turn_recall_test.go`:

```go
package arcadedb

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/embeddings"
)

func recallRequest() TurnRecallRequest {
	return TurnRecallRequest{
		IdentityID: "identity-a", Text: "che tempo fa domani a Torino?", ContextKey: "ctx1:k",
		RouteKey: "route1:r", PolicyVersion: "policy1:p", SourceRef: "postgres://aura/conversations/c9/turns/3",
		DeferredTools: []string{"calendar_add", "web_search"}, IncludeLabels: true,
	}
}

func labelRow(ref, source, effort string) string {
	return `{"distance":0.03,"source_ref":"` + ref + `","effort":"` + effort + `","effort_requested":"` + effort +
		`","effort_source":"` + source + `","recall_context_key":"ctx1:k","effort_route_key":"route1:r",` +
		`"effort_policy_version":"policy1:p"}`
}

// recallResponder answers each pool with rows, and records which statements were asked.
func recallResponder(user, teacher, tools []string) func(recordedRequest) testResponse {
	return func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		params, _ := request.Payload["params"].(map[string]any)
		rows := []string{}
		switch {
		case strings.Contains(statement, "CONTAINS (status = 'succeeded'"):
			rows = tools
		case params["effort_source"] == "user":
			rows = user
		case params["effort_source"] == "teacher":
			rows = teacher
		}
		return testResponse{Body: `{"result":[` + strings.Join(rows, ",") + `]}`}
	}
}

func recallStatements(requests []recordedRequest) []recordedRequest {
	var out []recordedRequest
	for _, request := range requests {
		if statement, _ := request.Payload["command"].(string); strings.Contains(statement, "vector.neighbors") {
			out = append(out, request)
		}
	}
	return out
}

func TestRecallTurnsReadsThreePoolsWithOneEmbedding(t *testing.T) {
	embedder := &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}
	client, requests := routedClient(t, recallResponder(
		[]string{labelRow("postgres://aura/conversations/c2/turns/1", "user", "high")},
		[]string{labelRow("postgres://aura/conversations/c3/turns/1", "teacher", "low")},
		[]string{`{"distance":0.04,"source_ref":"postgres://aura/conversations/c1/turns/1","recall_context_key":"ctx1:k",` +
			`"tools":["web_search","web_search","calendar_add"]}`},
	))
	request := recallRequest()
	recall, err := client.WithEmbedder(embedder).RecallTurns(context.Background(), request)
	if err != nil {
		t.Fatalf("RecallTurns: %v", err)
	}
	if want := withTask(taskDocumentPrefix, []string{request.Text}); len(embedder.calls) != 1 || !slices.Equal(embedder.calls[0], want) {
		t.Fatalf("embedder calls = %q, want one document-template embedding %q", embedder.calls, want)
	}
	asked := recallStatements(*requests)
	if len(asked) != 3 {
		t.Fatalf("pool queries = %d, want user labels, teacher labels and tool turns", len(asked))
	}
	for _, query := range asked {
		statement, _ := query.Payload["command"].(string)
		params, _ := query.Payload["params"].(map[string]any)
		for _, fragment := range []string{
			"maxDistance: :radius", "identity_id = :identity_id", "role = 'user'", "deleted_at IS NULL",
			"embed_space = :space", "source_ref <> :source_ref", "recall_context_key = :context_key",
			"@rid.out('NEXT_TURN').in('INITIATED_BY').out('HAS_STEP').out('INVOKED')",
		} {
			if !strings.Contains(statement, fragment) {
				t.Errorf("pool query lacks %q:\n%s", fragment, statement)
			}
		}
		if params["radius"] != 0.1 || params["neighbours"] != float64(5) || params["space"] != stubSpace ||
			params["context_key"] != "ctx1:k" || params["source_ref"] != request.SourceRef || params["identity_id"] != "identity-a" {
			t.Errorf("pool query params = %v", params)
		}
	}
	if len(recall.UserLabels) != 1 || recall.UserLabels[0].SourceRef != "postgres://aura/conversations/c2/turns/1" ||
		recall.UserLabels[0].RequestedEffort != "high" {
		t.Errorf("user labels = %+v", recall.UserLabels)
	}
	if len(recall.TeacherLabels) != 1 || recall.TeacherLabels[0].Effort != "low" {
		t.Errorf("teacher labels = %+v", recall.TeacherLabels)
	}
	if len(recall.ToolTurns) != 1 || !slices.Equal(recall.ToolTurns[0].Tools, []string{"calendar_add", "web_search"}) ||
		recall.ToolTurns[0].Distance != 0.04 {
		t.Errorf("tool turns = %+v, want one turn with its tools deduplicated and sorted", recall.ToolTurns)
	}
}

// The engine filters before top-k; these rows check the second line of defence, which keeps
// a row the engine should never have returned from becoming a label or a preload.
func TestRecallTurnsDropsIncompatibleRows(t *testing.T) {
	good := labelRow("postgres://aura/conversations/good/turns/1", "user", "high")
	user := []string{
		strings.Replace(good, `"route1:r"`, `"route1:other"`, 1),
		strings.Replace(good, `"policy1:p"`, `"policy1:other"`, 1),
		strings.Replace(good, `"recall_context_key":"ctx1:k"`, `"recall_context_key":"ctx1:other"`, 1),
		strings.Replace(good, "conversations/good/turns/1", "conversations/c9/turns/3", 1),
		strings.Replace(good, `"effort":"high"`, `"effort":""`, 1),
		strings.Replace(good, `"effort_requested":"high"`, `"effort_requested":""`, 1),
		strings.Replace(good, `"distance":0.03`, `"distance":0.2`, 1),
		strings.Replace(good, `"effort_source":"user"`, `"effort_source":"memory"`, 1),
		strings.Replace(good, `"source_ref":"postgres://aura/conversations/good/turns/1"`, `"source_ref":""`, 1),
		good,
	}
	tools := []string{
		`{"distance":0.04,"source_ref":"postgres://aura/conversations/t1/turns/1","recall_context_key":"ctx1:other","tools":["web_search"]}`,
		`{"distance":0.04,"source_ref":"postgres://aura/conversations/c9/turns/3","recall_context_key":"ctx1:k","tools":["web_search"]}`,
		`{"distance":0.04,"source_ref":"postgres://aura/conversations/t3/turns/1","recall_context_key":"ctx1:k","tools":[]}`,
		`{"distance":0.04,"source_ref":"postgres://aura/conversations/t4/turns/1","recall_context_key":"ctx1:k","tools":["web_search"]}`,
	}
	client, _ := routedClient(t, recallResponder(user, nil, tools))
	recall, err := client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}).
		RecallTurns(context.Background(), recallRequest())
	if err != nil {
		t.Fatalf("RecallTurns: %v", err)
	}
	if len(recall.UserLabels) != 1 || recall.UserLabels[0].SourceRef != "postgres://aura/conversations/good/turns/1" {
		t.Fatalf("user labels = %+v, want only the compatible row", recall.UserLabels)
	}
	if len(recall.ToolTurns) != 1 || recall.ToolTurns[0].SourceRef != "postgres://aura/conversations/t4/turns/1" {
		t.Fatalf("tool turns = %+v, want only the compatible row with tools", recall.ToolTurns)
	}
}

func TestRecallTurnsAsksOnlyThePoolsItCanUse(t *testing.T) {
	for _, test := range []struct {
		name  string
		edit  func(*TurnRecallRequest)
		pools int
	}{
		{name: "no labels requested", edit: func(r *TurnRecallRequest) { r.IncludeLabels = false }, pools: 1},
		{name: "no route", edit: func(r *TurnRecallRequest) { r.RouteKey = "" }, pools: 1},
		{name: "no policy", edit: func(r *TurnRecallRequest) { r.PolicyVersion = "" }, pools: 1},
		{name: "no deferred tools", edit: func(r *TurnRecallRequest) { r.DeferredTools = nil }, pools: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, requests := routedClient(t, recallResponder(nil, nil, nil))
			request := recallRequest()
			test.edit(&request)
			if _, err := client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}).
				RecallTurns(context.Background(), request); err != nil {
				t.Fatalf("RecallTurns: %v", err)
			}
			if got := len(recallStatements(*requests)); got != test.pools {
				t.Fatalf("pool queries = %d, want %d", got, test.pools)
			}
		})
	}
}

func TestRecallTurnsReadsNothingWithoutAnEmbedderTextOrContext(t *testing.T) {
	for _, test := range []struct {
		name     string
		embedder DenseEmbedder
		edit     func(*TurnRecallRequest)
	}{
		{name: "no embedder"},
		{name: "no text", embedder: &stubEmbedder{}, edit: func(r *TurnRecallRequest) { r.Text = "  " }},
		{name: "no context key", embedder: &stubEmbedder{}, edit: func(r *TurnRecallRequest) { r.ContextKey = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, requests := routedClient(t, recallResponder(nil, nil, nil))
			if test.embedder != nil {
				client = client.WithEmbedder(test.embedder)
			}
			request := recallRequest()
			if test.edit != nil {
				test.edit(&request)
			}
			recall, err := client.RecallTurns(context.Background(), request)
			if err != nil {
				t.Fatalf("RecallTurns: %v", err)
			}
			if len(*requests) != 0 || len(recall.UserLabels)+len(recall.TeacherLabels)+len(recall.ToolTurns) != 0 {
				t.Fatalf("recall = %+v after %d requests, want nothing asked and nothing returned", recall, len(*requests))
			}
		})
	}
}

type flippingSpaceEmbedder struct {
	spaces []string
	reads  int
}

func (e *flippingSpaceEmbedder) Space(context.Context) (embeddings.Space, error) {
	id := e.spaces[min(e.reads, len(e.spaces)-1)]
	e.reads++
	return embeddings.Space{ID: id}, nil
}

func (e *flippingSpaceEmbedder) Embed(context.Context, []string) ([][]float64, error) {
	return [][]float64{vectorOf(1)}, nil
}

// A model swapped in between the two space reads would rank a new model's vector against
// rows embedded by the old one; recall refuses rather than guessing.
func TestRecallTurnsFailsClosedOnASpaceChangeOrABadVector(t *testing.T) {
	for _, test := range []struct {
		name     string
		embedder DenseEmbedder
	}{
		{name: "space changed", embedder: &flippingSpaceEmbedder{spaces: []string{"es1-a", "es1-b"}}},
		{name: "wrong width", embedder: &stubEmbedder{vectors: [][][]float64{{{1, 0}}}}},
		{name: "embed error", embedder: &stubEmbedder{err: context.DeadlineExceeded}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, requests := routedClient(t, recallResponder(nil, nil, nil))
			if _, err := client.WithEmbedder(test.embedder).RecallTurns(context.Background(), recallRequest()); err == nil {
				t.Fatal("RecallTurns succeeded; want an error the agent logs once")
			}
			if len(recallStatements(*requests)) != 0 {
				t.Fatal("a pool was queried with a vector recall refused")
			}
		})
	}
}

func TestRecallTurnsNeedsAnIdentityAndItsOwnSourceRef(t *testing.T) {
	client, _ := routedClient(t, recallResponder(nil, nil, nil))
	client = client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	for _, edit := range []func(*TurnRecallRequest){
		func(r *TurnRecallRequest) { r.IdentityID = "" },
		func(r *TurnRecallRequest) { r.SourceRef = "" },
	} {
		request := recallRequest()
		edit(&request)
		if _, err := client.RecallTurns(context.Background(), request); err == nil {
			t.Errorf("RecallTurns(%+v) succeeded, want a validation error", request)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `W 'go test -race -count=1 -run TestRecallTurns ./internal/arcadedb/'`
Expected: FAIL to compile — `TurnRecallRequest`, `RecallTurns` undefined.

- [ ] **Step 3: Extract the space-checked embedding**

In `internal/arcadedb/embedding_space.go`, replace `denseQueryVector` with:

```go
// denseQueryVector is the dense leg's entry for every memory read: the embedded query, or
// none and the reason the read must be lexical. The gate is asked before the query is
// embedded, so a closed gate costs no embedding request.
func (c *Client) denseQueryVector(ctx context.Context, query string) (denseQuery, string) {
	return c.embedOne(ctx, taskQueryPrefix, query, func(space string) string {
		open, err := c.memoryDenseOpen(ctx, space)
		if err != nil {
			return reasonSpaceCheckFailed
		}
		if !open {
			return reasonEmbeddingSpaceMismatch
		}
		return ""
	})
}

// embedOne embeds text under prefix and names the space its vector is in, or returns the
// reason it could not. admit, when set, may refuse the space before an embedding request is
// spent.
//
// The space is read again once the vector is back. For a write, reading it first is the
// safe order (embedStored); for a read it is not: a model swapped in between would rank a
// new model's vector against a corpus checked in the old space.
func (c *Client) embedOne(ctx context.Context, prefix, text string, admit func(space string) string) (denseQuery, string) {
	if c == nil || c.embedder == nil {
		return denseQuery{}, reasonEmbedderNotConfigured
	}
	space, err := c.embedder.Space(ctx)
	if err != nil {
		return denseQuery{}, reasonEmbeddingFailed
	}
	if admit != nil {
		if reason := admit(space.ID); reason != "" {
			return denseQuery{}, reason
		}
	}
	vectors, err := c.embedder.Embed(ctx, withTask(prefix, []string{text}))
	if err != nil {
		return denseQuery{}, reasonEmbeddingFailed
	}
	if len(vectors) != 1 || len(vectors[0]) != vectorDimensions {
		return denseQuery{}, reasonEmbeddingInvalid
	}
	after, err := c.embedder.Space(ctx)
	if err != nil {
		return denseQuery{}, reasonEmbeddingFailed
	}
	if after.ID != space.ID {
		return denseQuery{}, reasonEmbeddingSpaceMismatch
	}
	return denseQuery{vector: vectors[0], space: space.ID}, ""
}
```

Run: `W 'time go test -race -count=1 -run "TestMemoryGate|TestSearchFactsHybrid|TestDenseLegs|TestDenseQuery|TestMemoryRecall|TestSearchReasoningTraces|TestUpsertFact" ./internal/arcadedb/'` — the existing tests over `denseQueryVector` and the space gate (`embedding_space_test.go`, `memory_vector_test.go`) must stay green.

- [ ] **Step 4: Write `RecallTurns`**

Create `internal/arcadedb/turn_recall.go`:

```go
package arcadedb

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

// Turn recall (docs/superpowers/specs/2026-10-06-turn-recall-design.md): the identity's own
// past user turns nearest to the one being read, with the tools they ran read back through
// the reasoning graph. One query vector serves three pools, and each pool is filtered BEFORE
// its top-k: five closer unlabelled copies can never evict a label, teacher rows can never
// crowd out a user label, and turns that ran only unusable tools can never crowd out one
// that ran a usable tool.

const (
	// recallRadius is the cosine distance (1 - cosine) a recalled turn may sit at, on the
	// document template. A candidate from the lab VM on 2026-10-06: repeats of one request
	// sat at cosine 0.91-1.00 while unrelated short prompts reached 0.88-0.90, so the bound
	// is tight and is not, alone, evidence that a reuse is valid.
	recallRadius = 0.10
	// recallNeighbours bounds each pool.
	recallNeighbours = 5
)

// The two decision sources that are reusable labels (migration 0137's CHECK names all six).
const (
	labelSourceUser    = "user"
	labelSourceTeacher = "teacher"
)

// TurnRecallRequest is one read of the identity's past turns for the turn being decided.
type TurnRecallRequest struct {
	IdentityID    string
	Text          string
	ContextKey    string
	RouteKey      string
	PolicyVersion string
	// SourceRef is the turn being read: it is excluded from every pool, because it may already
	// be projected by the time it is read.
	SourceRef string
	// DeferredTools are the registered deferred tools a remembered turn may preload.
	DeferredTools []string
	// IncludeLabels is false when the effort is already fixed and only tools are wanted.
	IncludeLabels bool
}

// RecalledTurn is one past user turn within the radius. Its field order matches
// agent.RecalledTurn: the runner converts one into the other.
type RecalledTurn struct {
	Distance        float64
	SourceRef       string
	Effort          string
	RequestedEffort string
	EffortSource    string
	ContextKey      string
	RouteKey        string
	PolicyVersion   string
	OriginRef       string
	// Tools are the names of the turn's successful tool calls, sorted.
	Tools []string
}

// TurnRecall holds each pool nearest first.
type TurnRecall struct {
	UserLabels    []RecalledTurn
	TeacherLabels []RecalledTurn
	ToolTurns     []RecalledTurn
}

// recallTurnSelect is what every pool shares: the identity's live user turns embedded in the
// reader's space, other than the turn being read, with the same prior context. The tool
// traversal starts from @rid because the expanded neighbour rows are projections, and the
// same path written without it reads null.
const recallTurnSelect = "SELECT distance, source_ref, effort, effort_requested, effort_source," +
	" recall_context_key, effort_route_key, effort_policy_version, effort_origin_ref," +
	" @rid.out('" + nextTurnEdgeType + "').in('INITIATED_BY').out('HAS_STEP').out('INVOKED')" +
	"[status = 'succeeded'].tool_name AS tools" +
	" FROM (SELECT expand(`vector.neighbors`('" + conversationTurnType + "[embedding]', :vector, :neighbours," +
	" { filter: (SELECT @rid FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND role = 'user' AND deleted_at IS NULL" + denseSpaceFilter +
	" AND source_ref <> :source_ref AND recall_context_key = :context_key"

// A filter that matches nothing must mean "no candidates", not "no filter": ArcadeDB
// before 26.10.1 confused the two (#8959), which TenantClients.For refuses.
const recallTurnClose = ").@rid, maxDistance: :radius }))) ORDER BY distance"

// A label must also share the route and the policy and carry a complete decision.
const recallLabelStatement = recallTurnSelect +
	" AND effort_route_key = :route_key AND effort_policy_version = :policy_version" +
	" AND effort_source = :effort_source AND effort IS NOT NULL AND effort_requested IS NOT NULL" +
	recallTurnClose

// A tool example must have run at least one currently eligible deferred tool successfully.
// CONTAINS with a condition is ArcadeDB's collection filter (arcadedb-docs
// reference/sql/sql-select.adoc: `races CONTAINS(name in [...])`).
const recallToolStatement = recallTurnSelect +
	" AND out('" + nextTurnEdgeType + "').in('INITIATED_BY').out('HAS_STEP').out('INVOKED')" +
	" CONTAINS (status = 'succeeded' AND tool_name IN :tools)" +
	recallTurnClose

// RecallTurns returns the identity's nearest eligible past user turns within recallRadius,
// in three pools. With no embedder, no text or no context key it returns nothing and no
// error, as when memory is off; a failed or refused embedding is an error the agent logs
// once before reading the turn without memory.
func (c *Client) RecallTurns(ctx context.Context, request TurnRecallRequest) (TurnRecall, error) {
	if strings.TrimSpace(request.IdentityID) == "" {
		return TurnRecall{}, errors.New("arcadedb: turn recall identity must be non-empty")
	}
	if request.SourceRef == "" {
		return TurnRecall{}, errors.New("arcadedb: turn recall needs the source_ref of the turn being read")
	}
	if strings.TrimSpace(request.Text) == "" || request.ContextKey == "" {
		return TurnRecall{}, nil
	}
	query, reason := c.embedOne(ctx, taskDocumentPrefix, request.Text, nil)
	switch reason {
	case "":
	case reasonEmbedderNotConfigured:
		return TurnRecall{}, nil
	default:
		return TurnRecall{}, fmt.Errorf("arcadedb: turn recall query: %s", reason)
	}
	params := map[string]any{
		"identity_id": request.IdentityID, "source_ref": request.SourceRef, "context_key": request.ContextKey,
		"neighbours": recallNeighbours, "radius": recallRadius,
	}
	query.bind(params)

	var recall TurnRecall
	var err error
	if request.IncludeLabels && request.RouteKey != "" && request.PolicyVersion != "" {
		if recall.UserLabels, err = c.recallLabels(ctx, params, request, labelSourceUser); err != nil {
			return TurnRecall{}, err
		}
		if recall.TeacherLabels, err = c.recallLabels(ctx, params, request, labelSourceTeacher); err != nil {
			return TurnRecall{}, err
		}
	}
	if len(request.DeferredTools) > 0 {
		if recall.ToolTurns, err = c.recallToolTurns(ctx, params, request); err != nil {
			return TurnRecall{}, err
		}
	}
	return recall, nil
}

func (c *Client) recallLabels(ctx context.Context, base map[string]any, request TurnRecallRequest, source string) ([]RecalledTurn, error) {
	params := maps.Clone(base)
	params["route_key"], params["policy_version"], params["effort_source"] = request.RouteKey, request.PolicyVersion, source
	rows, err := c.Query(ctx, recallLabelStatement, params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: recall %s labels: %w", source, err)
	}
	labels := make([]RecalledTurn, 0, len(rows))
	for _, row := range rows {
		turn, ok := recalledTurnFromRow(row)
		if ok && turn.EffortSource == source && turn.Effort != "" && turn.RequestedEffort != "" &&
			turn.RouteKey == request.RouteKey && turn.PolicyVersion == request.PolicyVersion &&
			turn.ContextKey == request.ContextKey && turn.SourceRef != request.SourceRef {
			labels = append(labels, turn)
		}
	}
	return labels, nil
}

func (c *Client) recallToolTurns(ctx context.Context, base map[string]any, request TurnRecallRequest) ([]RecalledTurn, error) {
	params := maps.Clone(base)
	params["tools"] = request.DeferredTools
	rows, err := c.Query(ctx, recallToolStatement, params)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: recall tool turns: %w", err)
	}
	turns := make([]RecalledTurn, 0, len(rows))
	for _, row := range rows {
		turn, ok := recalledTurnFromRow(row)
		if ok && len(turn.Tools) > 0 && turn.ContextKey == request.ContextKey && turn.SourceRef != request.SourceRef {
			turns = append(turns, turn)
		}
	}
	return turns, nil
}

// recalledTurnFromRow refuses a row without a source or outside the radius: the engine
// enforces both, and a row that slipped past it must not become a label or a preload.
func recalledTurnFromRow(row map[string]any) (RecalledTurn, bool) {
	distance, ok := row["distance"].(float64)
	if !ok || math.IsNaN(distance) || distance < 0 || distance > recallRadius {
		return RecalledTurn{}, false
	}
	turn := RecalledTurn{
		Distance: distance, SourceRef: rowString(row, "source_ref"),
		Effort: rowString(row, "effort"), RequestedEffort: rowString(row, "effort_requested"),
		EffortSource: rowString(row, "effort_source"), ContextKey: rowString(row, "recall_context_key"),
		RouteKey: rowString(row, "effort_route_key"), PolicyVersion: rowString(row, "effort_policy_version"),
		OriginRef: rowString(row, "effort_origin_ref"),
	}
	if tools := rowStrings(row, "tools"); len(tools) > 0 {
		slices.Sort(tools)
		turn.Tools = slices.Compact(tools)
	}
	return turn, turn.SourceRef != ""
}
```

- [ ] **Step 5: Run the unit tests to verify they pass**

Run: `W 'time go vet ./internal/arcadedb/ && time go test -race -count=1 -run "TestRecallTurns|TestMemoryGate|TestSearchFactsHybrid|TestDenseLegs|TestDenseQuery|TestMemoryRecall" ./internal/arcadedb/'`
Expected: PASS.

- [ ] **Step 6: Write the live tests**

Create `internal/arcadedb/turn_recall_live_test.go`:

```go
//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/embeddings"
)

const (
	recallLiveSpace  = "es1-recall-live"
	recallLiveQuery  = "che tempo fa domani a Torino?"
	recallLiveKey    = "ctx1:live"
	recallLiveRoute  = "route1:live"
	recallLivePolicy = "policy1:live"
	recallLiveSelf   = "postgres://aura/conversations/current/turns/1"
)

// recallEmbedder puts each fixture text on a chosen unit vector, so a test sets cosine
// distances exactly: blend(c) sits at distance 1-c from the query, on axis 0.
type recallEmbedder struct {
	space   string
	vectors map[string][]float64
}

func (e recallEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: e.space}, nil
}

func (e recallEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for index, text := range texts {
		if vector, ok := e.vectors[strings.TrimPrefix(text, taskDocumentPrefix)]; ok {
			out[index] = vector
			continue
		}
		out[index] = recallAxis(2)
	}
	return out, nil
}

func recallAxis(index int) []float64 {
	vector := make([]float64, vectorDimensions)
	vector[index] = 1
	return vector
}

func recallBlend(cosine float64) []float64 {
	vector := make([]float64, vectorDimensions)
	vector[0], vector[1] = cosine, math.Sqrt(1-cosine*cosine)
	return vector
}

type recallFixture struct {
	t        *testing.T
	client   *Client
	embedder recallEmbedder
}

func newRecallFixture(t *testing.T) *recallFixture {
	embedder := recallEmbedder{space: recallLiveSpace, vectors: map[string][]float64{recallLiveQuery: recallAxis(0)}}
	return &recallFixture{t: t, client: disposableMemoryClient(t).WithEmbedder(embedder), embedder: embedder}
}

func recallLiveLabel(source, effort string) TurnDecision {
	return TurnDecision{
		ContextKey: recallLiveKey, Effort: effort, EffortRequested: effort, EffortSource: source,
		RouteKey: recallLiveRoute, PolicyVersion: recallLivePolicy,
	}
}

func (f *recallFixture) project(conversation string, turn ConversationTurnProjection) {
	f.t.Helper()
	turn.IdentityID, turn.ConversationID = "identity-a", conversation
	turn.ContentHash, turn.OccurredAt = conversationContentHash(turn.Content), time.Now().UTC()
	if turn.SourceRef == "" {
		turn.SourceRef = fmt.Sprintf("postgres://aura/conversations/%s/turns/%d", conversation, turn.Seq)
	}
	if err := f.client.ApplyConversationProjection(context.Background(), ConversationProjection{
		IdentityID: "identity-a", ConversationID: conversation, Turns: []ConversationTurnProjection{turn},
	}); err != nil {
		f.t.Fatalf("project %s/%d: %v", conversation, turn.Seq, err)
	}
}

// user projects one user turn at cosine c from the query and returns its source ref.
func (f *recallFixture) user(conversation, text string, cosine float64, decision TurnDecision) string {
	f.embedder.vectors[text] = recallBlend(cosine)
	ref := fmt.Sprintf("postgres://aura/conversations/%s/turns/1", conversation)
	f.project(conversation, ConversationTurnProjection{Seq: 1, Role: "user", Content: text, SourceRef: ref, Decision: decision})
	return ref
}

// answer projects the answer after a user turn and the trace on it, whose one step ran tools
// with the given statuses.
func (f *recallFixture) answer(conversation string, tools map[string]string) {
	f.t.Helper()
	ref := fmt.Sprintf("postgres://aura/conversations/%s/turns/2", conversation)
	f.project(conversation, ConversationTurnProjection{Seq: 2, Role: "assistant", Content: "Fatto: " + conversation, SourceRef: ref})
	if len(tools) == 0 {
		return
	}
	trace := freshReasoningTrace()
	trace.IdentityID, trace.TraceID, trace.ConversationID, trace.TurnSeq, trace.SourceRef =
		"identity-a", conversation+"-trace", conversation, 2, ref
	trace.Steps[0].ToolCalls = nil
	for _, name := range slices.Sorted(maps.Keys(tools)) {
		trace.Steps[0].ToolCalls = append(trace.Steps[0].ToolCalls, ReasoningToolCall{
			CallID: "call-" + name, ToolName: name, Status: tools[name], DurationMillis: 10,
			ArgumentDigest: strings.Repeat("a", reasoningDigestRunes), Observation: "observed", SourceRef: ref,
		})
	}
	if err := f.client.UpsertReasoningTrace(context.Background(), trace); err != nil {
		f.t.Fatalf("UpsertReasoningTrace %s: %v", conversation, err)
	}
}

func (f *recallFixture) recall(edit func(*TurnRecallRequest)) TurnRecall {
	f.t.Helper()
	request := TurnRecallRequest{
		IdentityID: "identity-a", Text: recallLiveQuery, ContextKey: recallLiveKey, RouteKey: recallLiveRoute,
		PolicyVersion: recallLivePolicy, SourceRef: recallLiveSelf,
		DeferredTools: []string{"calendar_add", "web_search", "weather_lookup"}, IncludeLabels: true,
	}
	if edit != nil {
		edit(&request)
	}
	recall, err := f.client.RecallTurns(context.Background(), request)
	if err != nil {
		f.t.Fatalf("RecallTurns: %v", err)
	}
	return recall
}

func refs(turns []RecalledTurn) []string {
	out := make([]string, len(turns))
	for index, turn := range turns {
		out[index] = turn.SourceRef
	}
	return out
}

func TestRecallTurnsLiveReturnsTheEffortAndToolsOfAParaphrase(t *testing.T) {
	f := newRecallFixture(t)
	paraphrase := f.user("c-a", "previsioni meteo per domani a Torino", 0.97, recallLiveLabel("teacher", "low"))
	f.answer("c-a", map[string]string{"web_search": "succeeded", "weather_lookup": "failed"})

	recall := f.recall(nil)
	if len(recall.TeacherLabels) != 1 || recall.TeacherLabels[0].SourceRef != paraphrase ||
		recall.TeacherLabels[0].RequestedEffort != "low" || math.Abs(recall.TeacherLabels[0].Distance-0.03) > 0.005 {
		t.Fatalf("teacher labels = %+v, want the paraphrase at distance 0.03 with effort low", recall.TeacherLabels)
	}
	if len(recall.ToolTurns) != 1 || !slices.Equal(recall.ToolTurns[0].Tools, []string{"web_search"}) {
		t.Fatalf("tool turns = %+v, want the paraphrase with only its successful call", recall.ToolTurns)
	}
	if len(recall.UserLabels) != 0 {
		t.Fatalf("user labels = %+v, want none", recall.UserLabels)
	}
}

// The floor this plan depends on: before 26.10.1 a filter matching nothing was no filter,
// so an identity with no row in the current space would have ranked another model's rows.
func TestRecallTurnsLiveIgnoresAnotherSpaceEvenWhenTheCurrentOneIsEmpty(t *testing.T) {
	f := newRecallFixture(t)
	other := recallEmbedder{space: "es1-another-model", vectors: map[string][]float64{"previsioni meteo per domani a Torino": recallBlend(0.99)}}
	f.client = f.client.WithEmbedder(other)
	f.user("c-other", "previsioni meteo per domani a Torino", 0.99, recallLiveLabel("teacher", "low"))
	f.answer("c-other", map[string]string{"web_search": "succeeded"})
	f.client = f.client.WithEmbedder(f.embedder)

	recall := f.recall(nil)
	if n := len(recall.UserLabels) + len(recall.TeacherLabels) + len(recall.ToolTurns); n != 0 {
		t.Fatalf("recall returned %d rows from another embedding space: %+v", n, recall)
	}
}

func TestRecallTurnsLiveExcludesOtherContexts(t *testing.T) {
	f := newRecallFixture(t)
	far := f.user("c-far", "come si fa il risotto ai funghi", 0.5, recallLiveLabel("teacher", "none"))
	f.project("current", ConversationTurnProjection{Seq: 1, Role: "user", Content: recallLiveQuery, SourceRef: recallLiveSelf, Decision: recallLiveLabel("teacher", "low")})
	otherContext := recallLiveLabel("teacher", "high")
	otherContext.ContextKey = "ctx1:another-history"
	ctxRef := f.user("c-ctx", "meteo domani a Torino?", 0.98, otherContext)
	f.answer("c-ctx", map[string]string{"web_search": "succeeded"})
	otherRoute := recallLiveLabel("teacher", "high")
	otherRoute.RouteKey = "route1:another-model"
	routeRef := f.user("c-route", "domani a Torino piove?", 0.98, otherRoute)
	otherPolicy := recallLiveLabel("user", "high")
	otherPolicy.PolicyVersion = "policy1:older"
	policyRef := f.user("c-policy", "tempo previsto domani a Torino", 0.98, otherPolicy)

	recall := f.recall(nil)
	for _, excluded := range []string{far, recallLiveSelf, ctxRef, routeRef, policyRef} {
		if slices.Contains(refs(recall.UserLabels), excluded) || slices.Contains(refs(recall.TeacherLabels), excluded) {
			t.Errorf("%s was returned as a label", excluded)
		}
	}
	if slices.Contains(refs(recall.ToolTurns), ctxRef) {
		t.Errorf("a tool turn from another context was returned")
	}
}

// Five closer rows of the wrong kind must not hide the one the pool is for (spec,
// "Production retrieval must apply these predicates BEFORE each pool's top-k").
func TestRecallTurnsLiveKeepsTheRightRowBehindCloserCopies(t *testing.T) {
	f := newRecallFixture(t)
	unlabelled := TurnDecision{ContextKey: recallLiveKey}
	for index := range 6 {
		f.user(fmt.Sprintf("c-copy-%d", index), fmt.Sprintf("che tempo farà domani a Torino %d", index), 0.995, unlabelled)
		f.user(fmt.Sprintf("c-teach-%d", index), fmt.Sprintf("meteo di domani su Torino %d", index), 0.995, recallLiveLabel("teacher", "low"))
		conversation := fmt.Sprintf("c-stale-%d", index)
		f.user(conversation, fmt.Sprintf("tempo domani Torino %d", index), 0.995, unlabelled)
		f.answer(conversation, map[string]string{"stale_tool": "succeeded"})
	}
	userRef := f.user("c-user", "previsioni per domani a Torino", 0.95, recallLiveLabel("user", "low"))
	f.answer("c-user", map[string]string{"weather_lookup": "succeeded"})

	recall := f.recall(nil)
	if len(recall.UserLabels) != 1 || recall.UserLabels[0].SourceRef != userRef {
		t.Fatalf("user labels = %v: teacher rows or unlabelled copies crowded out the user label", refs(recall.UserLabels))
	}
	if len(recall.TeacherLabels) != recallNeighbours {
		t.Fatalf("teacher labels = %d, want the pool full of teacher rows", len(recall.TeacherLabels))
	}
	if len(recall.ToolTurns) != 1 || recall.ToolTurns[0].SourceRef != userRef {
		t.Fatalf("tool turns = %v: turns that ran only an unusable tool crowded out a usable one", refs(recall.ToolTurns))
	}
}

// On a corpus small enough to rank by hand, bounded ANN must return exactly the exact top-k
// within the radius, in order. Any miss is reported with both lists.
func TestRecallTurnsLiveMatchesExactDistances(t *testing.T) {
	f := newRecallFixture(t)
	type labelled struct {
		ref    string
		cosine float64
	}
	var corpus []labelled
	for index, cosine := range []float64{0.999, 0.99, 0.98, 0.97, 0.96, 0.95, 0.94, 0.93, 0.85, 0.6} {
		ref := f.user(fmt.Sprintf("c-exact-%d", index), fmt.Sprintf("previsione del tempo %d", index), cosine, recallLiveLabel("teacher", "low"))
		corpus = append(corpus, labelled{ref: ref, cosine: cosine})
	}
	var exact []string
	for _, row := range corpus {
		if 1-row.cosine <= recallRadius && len(exact) < recallNeighbours {
			exact = append(exact, row.ref)
		}
	}
	got := refs(f.recall(nil).TeacherLabels)
	if !slices.Equal(got, exact) {
		t.Fatalf("bounded ANN = %v, exact = %v", got, exact)
	}
}
```

- [ ] **Step 7: Run the live tests**

Run recipe **A** with `-run TestRecallTurnsLive`.
Expected: PASS, and record the wall time of `TestRecallTurnsLiveKeepsTheRightRowBehindCloserCopies`'s `recall` call in the report (this is the first measurement of the revised SQL; it is not a latency claim). If ArcadeDB rejects the `CONTAINS (...)` condition or the bracket filter, read `reference/sql/sql-where.adoc` and `sql-select.adoc` in `ArcadeData/arcadedb-docs` (`gh api repos/ArcadeData/arcadedb-docs/contents/src/main/asciidoc/reference/sql/<page>.adoc --jq .content | base64 -d`) before changing the statement, cite the page in the comment, and say so in the report.

- [ ] **Step 8: Commit**

```bash
W 'git add internal/arcadedb/turn_recall.go internal/arcadedb/turn_recall_test.go internal/arcadedb/turn_recall_live_test.go && LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -q -F <wsl path of the message file> -- internal/arcadedb/embedding_space.go internal/arcadedb/turn_recall.go internal/arcadedb/turn_recall_test.go internal/arcadedb/turn_recall_live_test.go'
```

Message subject: `feat(memory): recall past turns in three pools filtered before top-k`.

---
### Task 4: Seed margin, greeting predicate, effort mapping and the teacher as a function

Behaviour-preserving refactor that Task 5 builds the decision table on. After this task the adaptive path makes exactly the decisions it made before.

**Files:**
- Modify: `internal/agent/prompt/reasoning_classifier.go` (`ReasoningVerdict`, `Classify`, `IsTrivialGreeting`, `ReasoningPolicyFingerprint`)
- Modify: `internal/agent/prompt/reasoning_policy.go` (`ReasoningTier.Effort`, `ApplyAdaptiveEffort` replaces `ApplyAdaptiveReasoning`)
- Modify: `internal/agent/prompt/builder.go` (`BuildWithAdaptiveEffort` replaces `BuildWithReasoningTier`)
- Modify: `internal/agent/llm_agent.go` (`classifier` field type, run-loop effort, `buildRequest`), `internal/agent/llm_agent_request.go`, `internal/agent/llm_agent_construct.go` (no field change; `resolveClassifier` result type)
- Modify: `internal/agent/llm_agent_reasoning.go` (`tierClassifier`, `askTeacher`, `teacherOutcome`)
- Modify: `internal/obs/catalog.go`, `internal/obs/catalog_test.go`, `internal/agent/metrics.go`
- Create: `internal/agent/llm_agent_teacher_test.go`
- Modify tests (mechanical, rules in Step 6): `internal/agent/prompt/reasoning_classifier_test.go`, `reasoning_classifier_equivalence_test.go`, `reasoning_classifier_live_test.go`, `reasoning_policy_test.go`, `reasoning_policy_agnostic_test.go`, `reasoning_policy_edges_test.go`, `reasoning_chatgpt_test.go`, `reasoning_router_test.go`, `internal/agent/llm_agent_buildreq_internal_test.go`, `internal/llm/openai_compat/adaptive_reasoning_e2e_test.go`, `internal/llm/openai_compat/adaptive_reasoning_live_e2e_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `prompt.ReasoningVerdict{Tier ReasoningTier; Margin float64}`
  - `func (c *ReasoningClassifier) Classify(ctx context.Context, userText string) (ReasoningVerdict, bool)` — no greeting pre-filter any more.
  - `func IsTrivialGreeting(text string) bool`
  - `func (t ReasoningTier) Effort() llm.ReasoningEffort` — `""` for an invalid tier.
  - `func ApplyAdaptiveEffort(req *llm.Request, provider string, cfg llm.Config, effort llm.ReasoningEffort)`
  - `func (b *PromptBuilder) BuildWithAdaptiveEffort(history []llm.Message, reg *tools.Registry, provider string, cfg llm.Config, budget Budget, effort llm.ReasoningEffort, activated map[string]struct{}) llm.Request`
  - `func ReasoningPolicyFingerprint() string`
  - agent (unexported): `type tierClassifier interface { Classify(context.Context, string) (prompt.ReasoningVerdict, bool) }`; `type teacherOutcome string` with `teacherSuccess`, `teacherTimeout`, `teacherInvalid`, `teacherError`, `teacherCanceled`; `func (a *LlmAgent) askTeacher(ctx context.Context, user string) (prompt.ReasoningTier, teacherOutcome)`; `func (a *LlmAgent) buildRequest(budget prompt.Budget, effort llm.ReasoningEffort) llm.Request`; `func (a *LlmAgent) prepareReasoningRequest(ctx context.Context, budget prompt.Budget, round modelRound, effort llm.ReasoningEffort) (PreparedReasoningRequest, error)`; `func recordTeacherAttempt(outcome string)`.
  - `obs.AgentTeacherAttemptsID` (`aura_agent_teacher_attempt_total`, attribute `outcome`), outcome value `invalid`.

- [ ] **Step 1: Write the failing prompt tests**

In `internal/agent/prompt/reasoning_classifier_test.go`, replace `TestReasoningClassifier_GreetingPrefilterSkipsEmbed` with:

```go
// The allowlist moved out of Classify: whether "ok" is a greeting depends on what came before
// it, which only the caller knows (spec 2026-10-06, "The greeting fast path").
func TestIsTrivialGreeting(t *testing.T) {
	t.Parallel()
	for _, greeting := range []string{"ciao", "Buonasera!", "  Grazie mille ", "ok perfetto", "a presto!"} {
		if !IsTrivialGreeting(greeting) {
			t.Errorf("IsTrivialGreeting(%q) = false, want true", greeting)
		}
	}
	for _, request := range []string{"", "   ", "ciao, che tempo fa domani?", "debugga lo script"} {
		if IsTrivialGreeting(request) {
			t.Errorf("IsTrivialGreeting(%q) = true, want false", request)
		}
	}
}

func TestReasoningClassifierReportsTheMargin(t *testing.T) {
	t.Parallel()
	c := NewReasoningClassifier(&fakeEmbedder{})
	verdict, ok := c.Classify(context.Background(), "debugga il mio script python")
	if !ok || verdict.Tier != ReasoningTierHigh {
		t.Fatalf("Classify = %+v,%v; want high", verdict, ok)
	}
	// fakeEmbedder puts every high exemplar on one axis and the others off it, so the high
	// tier scores 1 and the runner-up 0.
	if math.Abs(verdict.Margin-1) > 1e-9 {
		t.Fatalf("margin = %v, want 1", verdict.Margin)
	}
}

func TestClassifyNoLongerShortCircuitsGreetings(t *testing.T) {
	t.Parallel()
	f := &fakeEmbedder{}
	c := NewReasoningClassifier(f)
	if _, ok := c.Classify(context.Background(), "ciao"); !ok {
		t.Fatal("Classify(ciao) failed")
	}
	if f.calls == 0 {
		t.Fatal("Classify answered a greeting without embedding it; the allowlist belongs to the caller now")
	}
}

func TestReasoningPolicyFingerprintIsStable(t *testing.T) {
	t.Parallel()
	first, second := ReasoningPolicyFingerprint(), ReasoningPolicyFingerprint()
	if first != second || len(first) != 64 {
		t.Fatalf("fingerprints %q and %q, want one stable sha256 hex", first, second)
	}
}
```

Add `"math"` to that file's imports. In `internal/agent/prompt/reasoning_policy_test.go`, append:

```go
func TestReasoningTierEffortIsTheOneMapping(t *testing.T) {
	for tier, want := range map[ReasoningTier]llm.ReasoningEffort{
		ReasoningTierNone: llm.ReasoningEffortNone, ReasoningTierLow: llm.ReasoningEffortLow,
		ReasoningTierHigh: llm.ReasoningEffortHigh, ReasoningTier("bogus"): "",
	} {
		if got := tier.Effort(); got != want {
			t.Errorf("%q.Effort() = %q, want %q", tier, got, want)
		}
	}
}

func TestApplyAdaptiveEffortClampsAndSkipsAnEmptyEffort(t *testing.T) {
	cfg := llm.Config{
		Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", AdaptiveReasoning: true,
		SupportedReasoningEfforts: []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh},
		ReasoningMandatory:        true,
	}
	req := &llm.Request{}
	ApplyAdaptiveEffort(req, cfg.Provider, cfg, llm.ReasoningEffortNone)
	if req.Reasoning.Effort != llm.ReasoningEffortLow || req.Reasoning.Exclude == nil || !*req.Reasoning.Exclude {
		t.Fatalf("reasoning = %+v, want none clamped to low on a mandatory model, CoT excluded", req.Reasoning)
	}
	untouched := &llm.Request{}
	ApplyAdaptiveEffort(untouched, cfg.Provider, cfg, "")
	if untouched.Reasoning != (llm.ReasoningConfig{}) {
		t.Fatalf("an empty effort set reasoning %+v", untouched.Reasoning)
	}
}
```

(Confirm `reasoning_policy_test.go` imports `llm`; it calls `BuildWithReasoningTier` today with `llm.Message`, so it does.)

- [ ] **Step 2: Run them to verify they fail**

Run: `W 'go test -race -count=1 -run "TestIsTrivialGreeting|TestReasoningClassifierReportsTheMargin|TestClassifyNoLonger|TestReasoningPolicyFingerprint|TestReasoningTierEffort|TestApplyAdaptiveEffort" ./internal/agent/prompt/'`
Expected: FAIL to compile — `IsTrivialGreeting`, `Margin`, `ReasoningPolicyFingerprint`, `Effort`, `ApplyAdaptiveEffort` undefined.

- [ ] **Step 3: Change the classifier**

In `internal/agent/prompt/reasoning_classifier.go`, add after the `ReasoningClassifier` type:

```go
// ReasoningVerdict is the tier of a turn's nearest exemplars and by how much its score led
// the runner-up's. A small margin is an uncertain verdict: the turn sits between tiers, and
// the turn reading asks the teacher below teacherMargin (internal/agent).
type ReasoningVerdict struct {
	Tier   ReasoningTier
	Margin float64
}
```

Replace `Classify` with:

```go
// Classify returns the reasoning tier of userText's nearest exemplars, with its margin, and
// true when it produced a usable verdict. It returns false on any embedding failure so the
// caller can fall back conservatively; the embedding path is an optimization, never a hard
// dependency. Greetings are the caller's (IsTrivialGreeting): only it knows whether the turn
// stands alone.
func (c *ReasoningClassifier) Classify(ctx context.Context, userText string) (ReasoningVerdict, bool) {
	if c == nil {
		return ReasoningVerdict{}, false
	}
	cls, err := c.ensureAnchors(ctx)
	if err != nil {
		return ReasoningVerdict{}, false
	}
	vecs, err := c.embed.Embed(ctx, []string{userText})
	if err != nil || len(vecs) != 1 || len(vecs[0]) == 0 {
		return ReasoningVerdict{}, false
	}
	verdict := cls.RankNearest(vecs[0], tierNeighbours)
	tier := ReasoningTier(verdict.Label)
	if !verdict.Ok || !tier.Valid() {
		return ReasoningVerdict{}, false
	}
	return ReasoningVerdict{Tier: tier, Margin: verdict.Margin}, true
}

// IsTrivialGreeting reports whether text, normalized, is an exact entry of the greeting
// allowlist. It says nothing about context: "ok" after an action request acknowledges that
// request, and the caller must not treat it as a greeting.
func IsTrivialGreeting(text string) bool {
	normalized := normalizeForGreeting(text)
	if normalized == "" {
		return false
	}
	_, ok := trivialGreetings[normalized]
	return ok
}

// ReasoningPolicyFingerprint digests every input the seed bank and the teacher decide from:
// the neighbour count, each tier's definition, seeds and effort, the greeting allowlist and
// the router prompt. The turn reading folds it into its policy version, so a label decided
// under different seeds or a different prompt is never reused.
func ReasoningPolicyFingerprint() string {
	h := sha256.New()
	write := func(part string) {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	write(strconv.Itoa(tierNeighbours))
	for _, tier := range classifierTierOrder {
		write(string(tier))
		write(string(tier.Effort()))
		write(reasoningTierDefs[tier])
		for _, seed := range reasoningTierSeeds[tier] {
			write(seed)
		}
	}
	for _, greeting := range slices.Sorted(maps.Keys(trivialGreetings)) {
		write(greeting)
	}
	write(ReasoningRouterSystemPrompt)
	return hex.EncodeToString(h.Sum(nil))
}
```

Add `"crypto/sha256"`, `"encoding/hex"`, `"maps"`, `"slices"`, `"strconv"` to the imports (keep what is there). Update the type's doc comment line "this type owns only the tier policy (defs/seeds, greeting pre-filter, soft fallback)" to "(defs/seeds, the greeting allowlist, soft fallback)".

- [ ] **Step 4: Change the effort mapping, the adaptive applier and the builder**

In `internal/agent/prompt/reasoning_policy.go`:

1. Rename `ApplyAdaptiveReasoning` to `ApplyAdaptiveEffort`, keep its whole doc comment (replace "a precomputed tier" with "a decided effort" in its first sentence and "the tier" with "the effort" where it names the applied value), and replace its body with:

```go
func ApplyAdaptiveEffort(req *llm.Request, provider string, cfg llm.Config, effort llm.ReasoningEffort) {
	if !cfg.AdaptiveReasoning {
		return
	}
	if !IsReasoningTarget(provider, cfg.BaseURL) || effort == "" {
		// SAY SO. The adaptive path logged nothing at all, so a backend it silently
		// skipped was indistinguishable from a model that had chosen not to think — which
		// is exactly how it went unnoticed on Ollama that adaptive reasoning had never
		// once been applied.
		slog.Debug("adaptive reasoning: not applied",
			"target", redact.Line(llm.ReasoningTarget(provider, cfg.BaseURL).String()), "effort", string(effort))
		return
	}
	req.Reasoning = llm.ReasoningConfig{Effort: cfg.ClampReasoningEffort(effort), Exclude: new(!cfg.ShowReasoning)}
	slog.Info("adaptive reasoning: effort applied",
		"target", redact.Line(llm.ReasoningTarget(provider, cfg.BaseURL).String()),
		"effort", string(req.Reasoning.Effort),
		// Name the substitution when one happened: an effort that silently differs from
		// the decision is the kind of thing that has to be readable in a log, not inferred.
		"requested", string(effort), "clamped", effort != req.Reasoning.Effort)
}
```

2. Replace `func (t ReasoningTier) reasoning(showReasoning bool) llm.ReasoningConfig` with the function below. Keep the long doc comment above it, change its first line to "Effort maps a tier to the effort it asks for — the one tier→effort mapping (spec 2026-10-06: a tier becomes an effort only here). Verified live against DeepSeek-V4 Flash on 2026-06-11 …" and move its last paragraph (about `exclude`) above `ApplyAdaptiveEffort`'s body comment, where `Exclude` is now set:

```go
func (t ReasoningTier) Effort() llm.ReasoningEffort {
	switch t {
	case ReasoningTierHigh:
		return llm.ReasoningEffortHigh
	case ReasoningTierLow:
		return llm.ReasoningEffortLow
	case ReasoningTierNone:
		return llm.ReasoningEffortNone
	default:
		return ""
	}
}
```

3. In the `IsReasoningTarget` doc comment, replace `ApplyAdaptiveReasoning` with `ApplyAdaptiveEffort`.

In `internal/agent/prompt/builder.go`, replace `BuildWithReasoningTier` with:

```go
// BuildWithAdaptiveEffort assembles a request and applies the turn's decided effort before
// provider-specific cache-control handling. activated is the per-run set of
// tool_search-promoted (or preloaded) deferred tool names (nil hides all).
func (b *PromptBuilder) BuildWithAdaptiveEffort(history []llm.Message, reg *tools.Registry, provider string, cfg llm.Config, budget Budget, effort llm.ReasoningEffort, activated map[string]struct{}) llm.Request {
	req := b.buildBase(history, reg, cfg, budget, activated)
	ApplyAdaptiveEffort(&req, provider, cfg, effort)
	injectCacheControl(&req, provider)
	return req
}
```

and in `BuildWithReasoningOverride`'s doc comment replace "the symmetric sibling of BuildWithReasoningTier" with "the symmetric sibling of BuildWithAdaptiveEffort".

- [ ] **Step 5: Run the prompt package**

Run: `W 'go vet ./internal/agent/prompt/ 2>&1 | head -40'`
Expected: compile errors only in the test files Step 6 lists.

- [ ] **Step 6: Update the existing tests mechanically**

Apply exactly these rules, then run `W 'time go vet -tags reasoning_live ./internal/agent/ ./internal/agent/prompt/ ./internal/llm/... && time go vet ./internal/agent/ ./internal/agent/prompt/ ./internal/llm/...'` until clean:

- `ApplyAdaptiveReasoning(<req>, <provider>, <cfg>, <tier>)` → `ApplyAdaptiveEffort(<req>, <provider>, <cfg>, <tier>.Effort())` (in `reasoning_chatgpt_test.go`, `reasoning_policy_agnostic_test.go`, `reasoning_policy_test.go`). Where `<tier>` is a table field of an invalid tier, `.Effort()` returns `""` and the "not applied" expectation still holds.
- `b.BuildWithReasoningTier(<h>, <reg>, <p>, <cfg>, <budget>, <tier>, <act>)` and `builder.BuildWithReasoningTier(...)` → `...BuildWithAdaptiveEffort(<h>, <reg>, <p>, <cfg>, <budget>, <tier>.Effort(), <act>)` (in `reasoning_policy_edges_test.go`, `reasoning_policy_test.go`, `internal/llm/openai_compat/adaptive_reasoning_e2e_test.go`, `internal/llm/openai_compat/adaptive_reasoning_live_e2e_test.go`). Rename `TestApplyAdaptiveReasoningLeavesConfiguredMaxTokens` to `TestApplyAdaptiveEffortLeavesConfiguredMaxTokens` and update its doc comment.
- In `reasoning_router_test.go`, the comment on line ~173 names `ApplyAdaptiveReasoning`: rename to `ApplyAdaptiveEffort`.
- `got, ok := c.Classify(ctx, X)` where `got` is compared to a tier or printed → `verdict, ok := c.Classify(ctx, X)` and use `verdict.Tier` where `got` was used (`TestReasoningClassifier_RoutesByProximity`, `TestReasoningClassifier_QueryEmbedFailureFallsBack`, `TestReasoningClassifier_ConcurrentColdStartSingleFlightsAnchorBuild`, and the golden loop in `reasoning_classifier_equivalence_test.go`). `_, ok := c.Classify(...)` sites stay as they are.
- In `reasoning_classifier_live_test.go` (tag `reasoning_live`), the gate must keep measuring the production path, which answers a standalone greeting without the classifier. Replace the line `tier, ok := c.Classify(context.Background(), tc.prompt)` with:

```go
		tier, ok := prompt.ReasoningTierNone, true
		if !prompt.IsTrivialGreeting(tc.prompt) {
			var verdict prompt.ReasoningVerdict
			verdict, ok = c.Classify(context.Background(), tc.prompt)
			tier = verdict.Tier
		}
```

- In `internal/agent/llm_agent_buildreq_internal_test.go`, replace the body from `tier := prompt.ReasoningTierHigh` to the end of the non-tier block with:

```go
	effort := prompt.ReasoningTierHigh.Effort()

	// A decided effort → must equal BuildWithAdaptiveEffort output.
	gotTier := a.buildRequest(budget, effort)
	wantTier := a.builder.BuildWithAdaptiveEffort(a.history, a.registry, a.cfg.Provider, a.cfg, budget, effort, a.activated)
	if !reflect.DeepEqual(gotTier, wantTier) {
		t.Fatalf("effort branch: buildRequest != BuildWithAdaptiveEffort\n got=%+v\nwant=%+v", gotTier, wantTier)
	}

	// No effort → must equal plain Build output.
	gotPlain := a.buildRequest(budget, "")
	wantPlain := a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget, a.activated)
	if !reflect.DeepEqual(gotPlain, wantPlain) {
		t.Fatalf("no-effort branch: buildRequest != Build\n got=%+v\nwant=%+v", gotPlain, wantPlain)
	}
```

and in its doc comment replace "BuildWithReasoningTier when adaptiveTierOK" with "BuildWithAdaptiveEffort when an effort was decided". (This file compiles only after Step 8.)

- [ ] **Step 7: Write the failing teacher tests**

Create `internal/agent/llm_agent_teacher_test.go`:

```go
package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// teacherClient answers the router prompt with text, fails to open, or waits for the
// deadline.
type teacherClient struct {
	text    string
	openErr error
	hang    bool
	calls   int
}

func (c *teacherClient) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
	c.calls++
	if c.hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if c.openErr != nil {
		return nil, c.openErr
	}
	ch := make(chan llm.Chunk, 1)
	ch <- llm.Chunk{Text: c.text}
	close(ch)
	return ch, nil
}

func teacherAgent(client llm.Client) *LlmAgent {
	return NewLlmAgent(LlmAgentConfig{
		Client:    client,
		LLM:       llm.Config{Model: "m", Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", TotalTimeoutSec: 1},
		Registry:  tools.NewRegistry(),
		SessionID: "teacher-session",
	})
}

// Every attempt is counted with its outcome, whatever the caller then decides (spec,
// "Teacher usage"): a timeout followed by a seed decision is still a teacher attempt.
func TestAskTeacherNamesEveryOutcomeAndCountsIt(t *testing.T) {
	recorded, reader := newTestAgentMetrics(t)
	previous := metrics
	metrics = recorded
	t.Cleanup(func() { metrics = previous })

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name    string
		ctx     context.Context
		client  *teacherClient
		tier    prompt.ReasoningTier
		outcome teacherOutcome
	}{
		{name: "success", ctx: context.Background(), client: &teacherClient{text: `{"tier":"high"}`}, tier: prompt.ReasoningTierHigh, outcome: teacherSuccess},
		{name: "invalid", ctx: context.Background(), client: &teacherClient{text: "probably high"}, outcome: teacherInvalid},
		{name: "error", ctx: context.Background(), client: &teacherClient{openErr: errors.New("401 unauthorized")}, outcome: teacherError},
		{name: "timeout", ctx: context.Background(), client: &teacherClient{hang: true}, outcome: teacherTimeout},
		{name: "canceled", ctx: canceled, client: &teacherClient{hang: true}, outcome: teacherCanceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			tier, outcome := teacherAgent(test.client).askTeacher(test.ctx, "che tempo fa domani a Cuneo?")
			if tier != test.tier || outcome != test.outcome {
				t.Fatalf("askTeacher = %q, %q; want %q, %q", tier, outcome, test.tier, test.outcome)
			}
		})
	}

	sum, ok := findOTelMetric(t, reader, "aura.agent.teacher.attempt").Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatal("aura.agent.teacher.attempt is not an int64 sum")
	}
	seen := map[string]int64{}
	for _, point := range sum.DataPoints {
		for _, outcome := range []string{"success", "invalid", "error", "timeout", "canceled"} {
			if hasOTelLabel(point.Attributes.ToSlice(), "outcome", outcome) {
				seen[outcome] += point.Value
			}
		}
	}
	for _, outcome := range []string{"success", "invalid", "error", "timeout", "canceled"} {
		if seen[outcome] != 1 {
			t.Errorf("teacher attempts with outcome %s = %d, want 1 (all: %v)", outcome, seen[outcome], seen)
		}
	}
}
```

- [ ] **Step 8: Extract the teacher and re-plumb the effort**

Replace `internal/agent/llm_agent_reasoning.go` with:

```go
package agent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningtrace"
)

// tierClassifier is the seed bank a turn is read against (prompt.ReasoningClassifier).
type tierClassifier interface {
	Classify(ctx context.Context, text string) (prompt.ReasoningVerdict, bool)
}

// resolveClassifier prefers the shared injected classifier (production, anchors built
// once); it falls back to a per-agent one built from Embedder when only that is supplied
// (tests/standalone). nil when neither is wired, as an interface: a nil
// *prompt.ReasoningClassifier stored in one would read as a classifier.
func resolveClassifier(cfg LlmAgentConfig) tierClassifier {
	if cfg.Classifier != nil {
		return cfg.Classifier
	}
	if classifier := prompt.NewReasoningClassifier(cfg.Embedder); classifier != nil {
		return classifier
	}
	return nil
}

// adaptiveReasoningTier classifies the CONVERSATION, so it gates on the generalized
// IsReasoningTarget — the same predicate ApplyAdaptiveEffort and ApplyFixedReasoning
// use. It was OpenRouter-only, upstream of the identical restriction in
// ApplyAdaptiveEffort: on every other backend the tier was never even COMPUTED, so
// lifting the downstream gate alone changed nothing. Measured live on Ollama 0.33.2 with
// gemma4:31b-cloud, 2026-08-31 — with only the downstream gate widened, a turn produced
// no tier decision at all.
func (a *LlmAgent) adaptiveReasoningTier(ctx context.Context) (prompt.ReasoningTier, bool) {
	if !a.cfg.AdaptiveReasoning || !prompt.IsReasoningTarget(a.cfg.Provider, a.cfg.BaseURL) {
		return "", false
	}
	user := prompt.LastGenuineUserContent(a.history)
	if strings.TrimSpace(user) == "" {
		return prompt.ReasoningTierLow, true
	}

	// Fast path: the local embedding classifier (embedding sidecar, ~10ms) replaces
	// the per-turn LLM router round-trip. On any embed failure it returns false;
	// when a classifier is wired, degrade to static low reasoning instead of
	// spending a second network call every turn.
	if a.classifier != nil {
		if prompt.IsTrivialGreeting(user) {
			return prompt.ReasoningTierNone, true
		}
		if verdict, ok := a.classifier.Classify(ctx, user); ok {
			reasoningtrace.Record("adaptive_reasoning_classifier_decision", map[string]any{
				"thread_id": a.sessionID,
				"tier":      verdict.Tier,
				"margin":    verdict.Margin,
				"source":    "embedding",
			})
			return verdict.Tier, true
		}
		reasoningtrace.Record("adaptive_reasoning_classifier_miss", map[string]any{
			"thread_id": a.sessionID,
			"fallback":  "static_low",
		})
		return prompt.ReasoningTierLow, true
	}
	if tier, outcome := a.askTeacher(ctx, user); outcome == teacherSuccess {
		return tier, true
	}
	return prompt.ReasoningTierLow, true
}

// teacherOutcome is how one synchronous teacher attempt ended.
type teacherOutcome string

const (
	teacherSuccess  teacherOutcome = "success"
	teacherTimeout  teacherOutcome = "timeout"
	teacherInvalid  teacherOutcome = "invalid"
	teacherError    teacherOutcome = "error"
	teacherCanceled teacherOutcome = "canceled"
)

// askTeacher asks the router prompt once, synchronously, for the tier of user's request: the
// teacher of the turn-recall spec, on the turn's own client and route, bounded by
// reasoningRouterTimeout. Every attempt is counted with its outcome.
func (a *LlmAgent) askTeacher(ctx context.Context, user string) (tier prompt.ReasoningTier, outcome teacherOutcome) {
	defer func() { recordTeacherAttempt(string(outcome)) }()
	routeCtx, cancel := context.WithTimeout(ctx, a.reasoningRouterTimeout())
	defer cancel()
	routeCtx, llmEnd := llmCallBoundary.Start(routeCtx)
	var boundaryErr error
	defer llmEnd.PanicSafe(&boundaryErr)
	enabled := false
	req := llm.Request{
		Model:       a.cfg.Model,
		Messages:    []llm.Message{{Role: llm.RoleSystem, Content: prompt.ReasoningRouterSystemPrompt}, {Role: llm.RoleUser, Content: user}},
		Temperature: 0,
		MaxTokens:   32,
		Reasoning:   llm.ReasoningConfig{Enabled: &enabled},
		SessionID:   a.sessionID,
		ToolChoice:  "none",
	}
	reasoningtrace.Record("adaptive_reasoning_router_request", map[string]any{
		"thread_id":  a.sessionID,
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
		"reasoning":  req.Reasoning,
		"user":       user,
	})

	ch, err := a.streamWithOpenRetry(routeCtx, req, "adaptive_reasoning_router")
	if err != nil {
		boundaryErr = err
		recordLLMError(llmErrorKind("reasoning_router_open", err))
		reasoningtrace.Record("adaptive_reasoning_router_error", map[string]any{"error": err.Error()})
		return "", teacherFailure(routeCtx, err)
	}
	var b strings.Builder
	for c := range ch {
		if c.Err != nil {
			boundaryErr = c.Err
			recordLLMError(llmErrorKind("reasoning_router_stream", c.Err))
			reasoningtrace.Record("adaptive_reasoning_router_error", map[string]any{"error": c.Err.Error()})
			return "", teacherFailure(routeCtx, c.Err)
		}
		if c.Usage != nil {
			recordUsage(*c.Usage)
		}
		b.WriteString(c.Text)
	}
	raw := strings.TrimSpace(b.String())
	tier = prompt.ParseReasoningRouterTier(raw)
	if !tier.Valid() {
		reasoningtrace.Record("adaptive_reasoning_router_invalid", map[string]any{"raw": raw})
		return "", teacherInvalid
	}
	reasoningtrace.Record("adaptive_reasoning_router_decision", map[string]any{"raw": raw, "tier": tier})
	return tier, teacherSuccess
}

// teacherFailure names why the teacher gave no answer: its own deadline, the turn's
// cancellation, or anything else.
func teacherFailure(ctx context.Context, err error) teacherOutcome {
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return teacherTimeout
	case errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled):
		return teacherCanceled
	}
	return teacherError
}

func (a *LlmAgent) reasoningRouterTimeout() time.Duration {
	const maxReasoningRouterTimeout = 2 * time.Second
	total := time.Duration(a.cfg.TotalTimeoutSec) * time.Second
	if total <= 0 {
		return maxReasoningRouterTimeout
	}
	if total < maxReasoningRouterTimeout {
		return total
	}
	return maxReasoningRouterTimeout
}
```

Before relying on it, run `W 'grep -rn fallback_tier internal --include=*_test.go'`: if a test asserts the dropped `fallback_tier` trace field, change that assertion to the fields above and say so in the report.

In `internal/agent/llm_agent.go`:
- change the `classifier` field to `classifier tierClassifier` and update its comment's second sentence to "When present, the turn reading uses its verdict; the teacher covers turns it is unsure of.";
- in `Run`, replace the three declarations `var adaptiveTier prompt.ReasoningTier`, `var adaptiveTierSet bool`, `var adaptiveTierOK bool` with `var adaptiveEffort llm.ReasoningEffort` and `var adaptiveEffortSet bool`, replace the block

```go
			} else if a.reasoningOverride == "" && !adaptiveTierSet {
				adaptiveTier, adaptiveTierOK = a.adaptiveReasoningTier(ic.Ctx)
				adaptiveTierSet = true
			}
```

with

```go
			} else if a.reasoningOverride == "" && !adaptiveEffortSet {
				if tier, ok := a.adaptiveReasoningTier(ic.Ctx); ok {
					adaptiveEffort = tier.Effort()
				}
				adaptiveEffortSet = true
			}
```

  and the `prepareReasoningRequest(spanCtx, budget, modelRound, adaptiveTier, adaptiveTierOK)` call with `prepareReasoningRequest(spanCtx, budget, modelRound, adaptiveEffort)`;
- replace `buildRequest` with:

```go
func (a *LlmAgent) buildRequest(budget prompt.Budget, effort llm.ReasoningEffort) llm.Request {
	// Fixed per-turn override (37E): force the selected effort and bypass the adaptive
	// decision. ApplyFixedReasoning gates on the generalized reasoning target
	// (OpenRouter OR llama.cpp, D-08); off-target it no-ops, so a non-reasoning backend
	// simply gets a plain build with the override inert.
	if a.reasoningOverride != "" {
		return a.builder.BuildWithReasoningOverride(a.history, a.registry, a.cfg.Provider, a.cfg, budget, a.reasoningOverride, a.activated)
	}
	if effort != "" {
		return a.builder.BuildWithAdaptiveEffort(a.history, a.registry, a.cfg.Provider, a.cfg, budget, effort, a.activated)
	}
	return a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget, a.activated)
}
```

In `internal/agent/llm_agent_request.go`, change `prepareReasoningRequest`'s last two parameters `tier prompt.ReasoningTier, tierSet bool` to `effort llm.ReasoningEffort`, its first line to `request := a.buildRequest(budget, effort)`, and its doc comment to "builds the model request for one round at the turn's decided effort, then runs the BeforeModel hooks over it." Drop the `prompt` import if it becomes unused.

- [ ] **Step 9: Add the teacher counter**

In `internal/obs/catalog.go`:
- add `AgentTeacherAttemptsID InstrumentID = "agent_teacher_attempts"` to the ID block (after `AgentPrefixDriftID`);
- in `descriptors`, right after the `AgentPrefixDriftID` entry: `count(AgentTeacherAttemptsID, "aura.agent.teacher.attempt", "aura_agent_teacher_attempt_total", []AttributeKey{AttributeOutcome}, "Total synchronous reasoning-teacher attempts by outcome."),`
- add `"invalid"` to the `AttributeOutcome` finite set (after `"retry_scheduled"`).

In `internal/obs/catalog_test.go`, insert into `catalogGolden` right after the `agent_prefix_drift|…` line:

```
agent_teacher_attempts|aura.agent.teacher.attempt|aura_agent_teacher_attempt_total|counter|1|outcome|Total synchronous reasoning-teacher attempts by outcome.|
```

In `internal/agent/metrics.go`, add the field `teacherAttemptsTotal metric.Int64Counter` to `agentMetrics`, `teacherAttemptsTotal: mustInt64Counter(meter, obs.AgentTeacherAttemptsID),` to `newAgentMetrics`, and:

```go
func recordTeacherAttempt(outcome string) { metrics.recordTeacherAttempt(outcome) }

func (m *agentMetrics) recordTeacherAttempt(outcome string) {
	label := obs.NormalizeAttribute(obs.AttributeOutcome, outcome)
	m.teacherAttemptsTotal.Add(context.Background(), 1, metric.WithAttributes(boundedAttr(obs.AttributeOutcome, label)))
}
```

- [ ] **Step 10: Run the task's tests and the ones it touched**

Run: `W 'time go vet ./internal/agent/ ./internal/agent/prompt/ ./internal/llm/... ./internal/obs/ && time go vet -tags reasoning_live ./internal/agent/ ./internal/agent/prompt/ && time go test -race -count=1 -run "Reasoning|Adaptive|Teacher|BuildReq|Classif|Greeting|Effort|ModelRound|Metrics" ./internal/agent/ ./internal/agent/prompt/ ./internal/llm/openai_compat/ && time go test -race -count=1 -run TestCatalog ./internal/obs/'`
Expected: PASS, including the unchanged `TestLlmAgent_AdaptiveReasoning*` tests in `llm_agent_reasoning_test.go` (they pin that the decisions did not move).

- [ ] **Step 11: Commit**

```bash
W 'git add internal/agent/llm_agent_teacher_test.go && LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -q -F <wsl path of the message file> -- internal/agent internal/obs/catalog.go internal/obs/catalog_test.go internal/llm/openai_compat/adaptive_reasoning_e2e_test.go internal/llm/openai_compat/adaptive_reasoning_live_e2e_test.go'
```

(`-- internal/agent` stages only the files this task changed under it; confirm with `git status --short internal/agent` that nothing unrelated is dirty there before committing.)

Message subject: `refactor(agent): expose the seed margin and make the teacher a counted function`.

---
### Task 5: The turn reading — one decision path for the effort and the tools

**Files:**
- Create: `internal/agent/turn_recall.go` (port types, effort sources, `TurnReading`, `TurnDecision`, `TurnContextKey`, `routeKey`, `turnPolicyVersion`)
- Create: `internal/agent/llm_agent_turn_reading.go` (`readTurn` and its helpers)
- Modify: `internal/agent/llm_agent.go` (field `turnReading`; run loop calls `readTurn` once)
- Modify: `internal/agent/llm_agent_construct.go` (`LlmAgentConfig.TurnReading`)
- Modify: `internal/agent/llm_agent_reasoning.go` (delete `adaptiveReasoningTier`)
- Modify: `internal/llm/reasoning_clamp.go` (`ReasoningEffort.Known`)
- Modify: `internal/obs/catalog.go`, `internal/obs/catalog_test.go`, `internal/agent/metrics.go` (decision-source counter)
- Create: `internal/agent/turn_recall_test.go`, `internal/agent/llm_agent_turn_reading_test.go`
- Modify: `internal/agent/reasoning_tier_live_test.go` (tag `reasoning_live`), `internal/llm/reasoning_clamp_test.go`

**Interfaces:**
- Consumes: `tierClassifier`, `askTeacher`, `teacherOutcome`, `buildRequest(budget, effort)`, `prompt.ReasoningVerdict`, `prompt.IsTrivialGreeting`, `ReasoningTier.Effort`, `prompt.ReasoningPolicyFingerprint` (Task 4).
- Produces (exact; Task 6 and Task 7 use them):
  - `const EffortSourceUser, EffortSourceTeacher, EffortSourceMemory, EffortSourceSeeds, EffortSourceGreeting, EffortSourceFallback = "user", "teacher", "memory", "seeds", "greeting", "fallback"`
  - `type TurnRecaller interface { RecallTurns(ctx context.Context, request TurnRecallRequest) (TurnRecall, error) }`
  - `type TurnRecallRequest struct { Text, ContextKey, RouteKey, PolicyVersion, SourceRef string; DeferredTools []string; IncludeLabels bool }`
  - `type RecalledTurn struct { Distance float64; SourceRef, Effort, RequestedEffort, EffortSource, ContextKey, RouteKey, PolicyVersion, OriginRef string; Tools []string }` — same fields and order as `arcadedb.RecalledTurn`.
  - `type TurnRecall struct { UserLabels, TeacherLabels, ToolTurns []RecalledTurn }`
  - `type TurnReading struct { Recaller TurnRecaller; ContextKey, SourceRef string; Standalone bool; OnDecision func(TurnDecision) }`
  - `type TurnDecision struct { Effort, EffortRequested llm.ReasoningEffort; EffortSource, RouteKey, PolicyVersion, OriginRef string }`
  - `func TurnContextKey(prior []llm.Message, currentBlocks string) string`
  - `LlmAgentConfig.TurnReading TurnReading`
  - unexported, used by the Task 7 harness: `func (a *LlmAgent) readTurn(ctx context.Context) (TurnDecision, turnRead)`; `type turnRead struct { seedTier prompt.ReasoningTier; seedMargin float64; seedOK bool; teacherTier prompt.ReasoningTier; teacher teacherOutcome; label, toolTurn RecalledTurn; preloaded []string; recallMiss string; recallDuration, seedDuration, teacherDuration time.Duration }`
  - `func (e llm.ReasoningEffort) Known() bool`
  - `obs.AgentTurnDecisionsID` (`aura_agent_turn_decision_total`, attribute `outcome` = the source, or `undecided`).

- [ ] **Step 1: Write the failing key and route tests**

Create `internal/agent/turn_recall_test.go` (`call` is the package's existing tool-call builder in `llm_agent_promote_internal_test.go`):

```go
package agent

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestTurnContextKeySeparatesDifferentHistories(t *testing.T) {
	codeAsk := []llm.Message{{Role: llm.RoleUser, Content: "rewrite this function in Go with tests"}, {Role: llm.RoleAssistant, Content: "Here is the plan."}}
	thanks := []llm.Message{{Role: llm.RoleUser, Content: "thanks for the help"}, {Role: llm.RoleAssistant, Content: "You're welcome."}}
	if TurnContextKey(codeAsk, "") == TurnContextKey(thanks, "") {
		t.Fatal("two different histories share a context key")
	}
	if TurnContextKey(nil, "") != TurnContextKey([]llm.Message{}, "") {
		t.Fatal("a known empty history has more than one key")
	}
	if TurnContextKey(nil, "catalog: a.pdf\n") == TurnContextKey(nil, "") {
		t.Fatal("the current message's context blocks are not in the key")
	}
	if !strings.HasPrefix(TurnContextKey(nil, ""), "ctx1:") {
		t.Fatal("the key does not name its format")
	}
}

// Tool-call ids correlate a call with its result and differ between conversations that
// did the same thing; the arguments and the result are the task.
func TestTurnContextKeyIgnoresToolCallIDsButNotArguments(t *testing.T) {
	history := func(id, args string) []llm.Message {
		return []llm.Message{
			{Role: llm.RoleUser, Content: "che tempo fa a Cuneo?"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call(id, "web_search", args)}},
			{Role: llm.RoleTool, ToolCallID: id, Content: "Sereno, 21 gradi."},
			{Role: llm.RoleAssistant, Content: "Sereno."},
		}
	}
	if TurnContextKey(history("call-a", `{"q":"meteo Cuneo"}`), "") != TurnContextKey(history("call-b", `{"q":"meteo Cuneo"}`), "") {
		t.Fatal("tool-call ids changed the key")
	}
	if TurnContextKey(history("call-a", `{"q":"meteo Cuneo"}`), "") == TurnContextKey(history("call-a", `{"q":"meteo Bra"}`), "") {
		t.Fatal("different tool arguments share a key")
	}
}

func TestRouteKeyNamesTheRouteWithoutItsCredentials(t *testing.T) {
	base := llm.Config{
		Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", Model: "z-ai/glm-5.3-flash",
		SupportedReasoningEfforts: []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh},
		APIKey:                    "sk-secret-value",
	}
	key := routeKey(base)
	if strings.Contains(key, "sk-secret-value") || !strings.HasPrefix(key, "route1:") {
		t.Fatalf("route key %q leaks the credential or misses its format", key)
	}
	same := []func(*llm.Config){
		func(c *llm.Config) { c.APIKey = "sk-another" },
		func(c *llm.Config) { c.Headers = map[string]string{"X-Title": "Aura"} },
		func(c *llm.Config) { c.BaseURL = "https://user:pass@openrouter.ai/api/v1/?key=abc" },
		func(c *llm.Config) { c.Temperature = 0.9 },
	}
	for index, edit := range same {
		cfg := base
		edit(&cfg)
		if routeKey(cfg) != key {
			t.Errorf("credential or sampling change %d moved the route key", index)
		}
	}
	different := []func(*llm.Config){
		func(c *llm.Config) { c.Model = "google/gemini-3.8-flash" },
		func(c *llm.Config) { c.BaseURL = "http://127.0.0.1:11434/v1" },
		func(c *llm.Config) { c.Provider = "ollama" },
		func(c *llm.Config) { c.SupportedReasoningEfforts = []llm.ReasoningEffort{llm.ReasoningEffortHigh} },
		func(c *llm.Config) { c.ReasoningMandatory = true },
	}
	for index, edit := range different {
		cfg := base
		edit(&cfg)
		if routeKey(cfg) == key {
			t.Errorf("route change %d kept the route key", index)
		}
	}
}

func TestTurnPolicyVersionFollowsTheSeedPolicy(t *testing.T) {
	if turnPolicyVersion != policyVersion() || !strings.HasPrefix(turnPolicyVersion, "policy1:") {
		t.Fatalf("turnPolicyVersion = %q", turnPolicyVersion)
	}
}
```

(`llm.Config.Headers` is `map[string]string`, `internal/llm/config.go:166`.)

In `internal/llm/reasoning_clamp_test.go`, append:

```go
func TestReasoningEffortKnownIsTheLadder(t *testing.T) {
	for _, effort := range []ReasoningEffort{ReasoningEffortNone, ReasoningEffortLow, ReasoningEffortMedium, ReasoningEffortHigh, ReasoningEffortXHigh, ReasoningEffortMax} {
		if !effort.Known() {
			t.Errorf("%q.Known() = false", effort)
		}
	}
	for _, effort := range []ReasoningEffort{"", "turbo", ReasoningEffortMinimal} {
		if effort.Known() {
			t.Errorf("%q.Known() = true", effort)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `W 'go test -race -count=1 -run "TestTurnContextKey|TestRouteKey|TestTurnPolicyVersion" ./internal/agent/ ; go test -race -count=1 -run TestReasoningEffortKnown ./internal/llm/'`
Expected: FAIL to compile — `TurnContextKey`, `routeKey`, `turnPolicyVersion`, `Known` undefined.

- [ ] **Step 3: Write the port, the keys and `Known`**

In `internal/llm/reasoning_clamp.go`, add after `reasoningLadder`:

```go
// Known reports whether e is on the ladder: an effort read back from storage that is not is
// a miss, never a guess.
func (e ReasoningEffort) Known() bool {
	return slices.Contains(reasoningLadder, e)
}
```

Create `internal/agent/turn_recall.go`:

```go
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
)

// Effort sources a turn's decision is persisted with (migration 0137's CHECK). Only user and
// teacher decisions are reusable labels. The other four record how a turn was decided and are
// never copied, so the memory cannot reinforce its own guesses.
const (
	EffortSourceUser     = "user"
	EffortSourceTeacher  = "teacher"
	EffortSourceMemory   = "memory"
	EffortSourceSeeds    = "seeds"
	EffortSourceGreeting = "greeting"
	EffortSourceFallback = "fallback"
)

// TurnRecaller is the agent's port onto the identity's past turns. internal/agent does not
// import the memory store: the runner binds it per identity (arcadedb.Client.RecallTurns).
type TurnRecaller interface {
	RecallTurns(ctx context.Context, request TurnRecallRequest) (TurnRecall, error)
}

// TurnRecallRequest is one read of the identity's past turns for the turn being decided.
type TurnRecallRequest struct {
	Text          string
	ContextKey    string
	RouteKey      string
	PolicyVersion string
	SourceRef     string
	DeferredTools []string
	IncludeLabels bool
}

// RecalledTurn is one past user turn within the recall radius. Its fields are
// arcadedb.RecalledTurn's, in the same order: the runner converts one into the other.
type RecalledTurn struct {
	Distance        float64
	SourceRef       string
	Effort          string
	RequestedEffort string
	EffortSource    string
	ContextKey      string
	RouteKey        string
	PolicyVersion   string
	OriginRef       string
	Tools           []string
}

// TurnRecall holds each pool nearest first.
type TurnRecall struct {
	UserLabels    []RecalledTurn
	TeacherLabels []RecalledTurn
	ToolTurns     []RecalledTurn
}

// TurnReading is what the runner hands one dispatched user turn so the agent can read it
// against the identity's past turns. The zero value reads the turn from seeds and the
// teacher alone, which is what a resumed run, a branch re-run and a headless agent get.
type TurnReading struct {
	Recaller TurnRecaller
	// ContextKey is TurnContextKey of what the model reads before this message; "" makes the
	// turn ineligible for recall (an unversioned input, or no dispatched message).
	ContextKey string
	// SourceRef is the dispatched user turn, which recall must not return to itself.
	SourceRef string
	// Standalone is true when nothing conversational precedes the message, the only case in
	// which a greeting takes the greeting fast path.
	Standalone bool
	// OnDecision receives the decision once, before the first request.
	OnDecision func(TurnDecision)
}

// TurnDecision is how the turn's effort was decided, as the runner persists it on the user
// turn (migration 0137). Effort is what is sent after the clamp, EffortRequested the
// decision before it; both are empty when this route takes no effort.
type TurnDecision struct {
	Effort          llm.ReasoningEffort
	EffortRequested llm.ReasoningEffort
	EffortSource    string
	RouteKey        string
	PolicyVersion   string
	OriginRef       string
}

// contextKeyFormat versions the encoding below. It is part of every key, so a change of
// format never lets an old key equal a new one.
const contextKeyFormat = "ctx1"

// TurnContextKey digests everything the model reads before the current user message: the
// system prompt, every prior message in order with its tool calls and results, and the
// context blocks the current message arrives with. The message's own text is left out so a
// paraphrase can match, and so are tool-call ids, which only pair a call with its result
// (spec 2026-10-06, "Compatibility and label provenance"). Equal context gives equal keys;
// any difference in what the model reads gives different ones.
func TurnContextKey(prior []llm.Message, currentBlocks string) string {
	type toolCall struct {
		Name      string
		Arguments string
	}
	type message struct {
		Role    string
		Content string
		Calls   []toolCall `json:",omitempty"`
	}
	encoded := struct {
		System  string
		Prior   []message
		Current string
	}{System: SystemPrompt, Prior: make([]message, 0, len(prior)), Current: currentBlocks}
	for _, m := range prior {
		msg := message{Role: m.Role, Content: m.Content}
		for _, tc := range m.ToolCalls {
			msg.Calls = append(msg.Calls, toolCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments})
		}
		encoded.Prior = append(encoded.Prior, msg)
	}
	// Marshalling a struct of strings cannot fail.
	raw, _ := json.Marshal(encoded)
	sum := sha256.Sum256(raw)
	return contextKeyFormat + ":" + hex.EncodeToString(sum[:])
}

// routeKey names the route an effort is decided for: the backend and its endpoint, the
// model, and the efforts the model publishes. A label from another route is never reused.
// The API key, headers, sampling, and any userinfo or query in the base URL stay out.
func routeKey(cfg llm.Config) string {
	efforts := make([]string, len(cfg.SupportedReasoningEfforts))
	for index, effort := range cfg.SupportedReasoningEfforts {
		efforts[index] = string(effort)
	}
	return "route1:" + digest(
		llm.ReasoningTarget(cfg.Provider, cfg.BaseURL).String(), cfg.Provider, endpointIdentity(cfg.BaseURL),
		cfg.Model, strings.Join(efforts, ","), strconv.FormatBool(cfg.ReasoningMandatory),
	)
}

func endpointIdentity(baseURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host) + strings.TrimRight(parsed.Path, "/")
}

// turnPolicyVersion versions everything a label is decided under: the seed bank, its tier
// mapping, the greeting allowlist and the teacher prompt (prompt.ReasoningPolicyFingerprint),
// and the selection rules of readTurn. A label decided under another version is never reused.
var turnPolicyVersion = policyVersion()

// selectionRules names readTurn's precedence; change it with the table.
const selectionRules = "composer>greeting>label(user>teacher)>seeds(margin)>teacher>seeds|fallback"

func policyVersion() string {
	return "policy1:" + digest(
		prompt.ReasoningPolicyFingerprint(), selectionRules, strconv.FormatFloat(teacherMargin, 'g', -1, 64),
	)
}

func digest(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
```

(`teacherMargin` is declared in Step 6's file; this file compiles together with it.)

- [ ] **Step 4: Write the failing decision tests**

Create `internal/agent/llm_agent_turn_reading_test.go`:

```go
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type fakeClassifier struct {
	verdict prompt.ReasoningVerdict
	ok      bool
	calls   int
}

func (c *fakeClassifier) Classify(context.Context, string) (prompt.ReasoningVerdict, bool) {
	c.calls++
	return c.verdict, c.ok
}

// readingClient answers each Stream with the next scripted text; "" fails to open.
type readingClient struct {
	answers   []string
	requests  []llm.Request
	onRequest func()
}

func (c *readingClient) Stream(_ context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	if c.onRequest != nil {
		c.onRequest()
	}
	c.requests = append(c.requests, req)
	answer := "Fatto."
	if index := len(c.requests) - 1; index < len(c.answers) {
		answer = c.answers[index]
	}
	if answer == "" {
		return nil, errors.New("teacher unavailable")
	}
	ch := make(chan llm.Chunk, 2)
	ch <- llm.Chunk{Text: answer}
	ch <- llm.Chunk{FinishReason: "stop"}
	close(ch)
	return ch, nil
}

type fakeRecaller struct {
	recall   TurnRecall
	err      error
	hang     bool
	requests []TurnRecallRequest
}

func (r *fakeRecaller) RecallTurns(ctx context.Context, request TurnRecallRequest) (TurnRecall, error) {
	r.requests = append(r.requests, request)
	if r.hang {
		<-ctx.Done()
		return TurnRecall{}, ctx.Err()
	}
	return r.recall, r.err
}

type readingTool struct {
	name     string
	deferred bool
}

func (t readingTool) Spec() tools.Spec {
	return tools.Spec{Name: t.name, Summary: t.name + " summary", Description: t.name + " does one thing.",
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: t.deferred}
}

func (readingTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

type readingSetup struct {
	text       string
	override   llm.ReasoningEffort
	classifier tierClassifier
	teacher    []string
	reading    TurnReading
	cfg        func(*llm.Config)
}

func newReadingAgent(t *testing.T, setup readingSetup) (*LlmAgent, *readingClient) {
	t.Helper()
	client := &readingClient{answers: setup.teacher}
	cfg := llm.Config{Model: "m", Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1",
		TotalTimeoutSec: 30, AdaptiveReasoning: true, MaxTokens: 4096}
	if setup.cfg != nil {
		setup.cfg(&cfg)
	}
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	for _, name := range []string{"calendar_add", "notes_add", "pdf_merge", "weather_lookup", "web_search"} {
		reg.Register(readingTool{name: name, deferred: true})
	}
	reg.Register(readingTool{name: "clock_now"})
	text := setup.text
	if text == "" {
		text = "che tempo fa domani a Cuneo?"
	}
	a := NewLlmAgent(LlmAgentConfig{
		Client: client, LLM: cfg, Registry: reg, SessionID: "reading-session", PreviewCap: 2048, RunDir: t.TempDir(),
		UserTurns:   []llm.Message{{Role: llm.RoleUser, Content: text}},
		TurnReading: setup.reading, ReasoningOverride: setup.override,
	})
	a.classifier = setup.classifier
	return a, client
}

func memoryReading(recaller TurnRecaller) TurnReading {
	return TurnReading{Recaller: recaller, ContextKey: "ctx1:k", SourceRef: "postgres://aura/conversations/now/turns/1"}
}

func recalledLabel(source, effort string, distance float64) RecalledTurn {
	return RecalledTurn{
		Distance: distance, SourceRef: "postgres://aura/conversations/past-" + source + "/turns/1",
		Effort: effort, RequestedEffort: effort, EffortSource: source,
		ContextKey: "ctx1:k", PolicyVersion: turnPolicyVersion,
	}
}

func TestReadTurnFixedEffortIsTheUsersAndStillPreloadsTools(t *testing.T) {
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierNone, Margin: 0.5}, ok: true}
	recaller := &fakeRecaller{recall: TurnRecall{ToolTurns: []RecalledTurn{{Distance: 0.02, SourceRef: "postgres://aura/conversations/past/turns/1", Tools: []string{"web_search"}}}}}
	a, client := newReadingAgent(t, readingSetup{override: llm.ReasoningEffortHigh, classifier: classifier, reading: memoryReading(recaller)})

	decision, read := a.readTurn(context.Background())
	if decision.EffortSource != EffortSourceUser || decision.EffortRequested != llm.ReasoningEffortHigh || decision.Effort != llm.ReasoningEffortHigh {
		t.Fatalf("decision = %+v, want the composer's high", decision)
	}
	if len(recaller.requests) != 1 || recaller.requests[0].IncludeLabels {
		t.Fatalf("recall requests = %+v, want one tool-only read", recaller.requests)
	}
	if _, ok := a.activated["web_search"]; !ok || !slices.Equal(read.preloaded, []string{"web_search"}) {
		t.Fatalf("preloaded = %v, activated = %v; want web_search", read.preloaded, a.activated)
	}
	if classifier.calls != 0 || len(client.requests) != 0 {
		t.Fatalf("a fixed effort embedded (%d) or asked the teacher (%d)", classifier.calls, len(client.requests))
	}
}

func TestReadTurnStandaloneGreetingSkipsMemoryAndEmbedding(t *testing.T) {
	recaller := &fakeRecaller{}
	classifier := &fakeClassifier{ok: true}
	reading := memoryReading(recaller)
	reading.Standalone = true
	a, _ := newReadingAgent(t, readingSetup{text: "Buongiorno!", classifier: classifier, reading: reading})
	decision, _ := a.readTurn(context.Background())
	if decision.EffortSource != EffortSourceGreeting || decision.EffortRequested != llm.ReasoningEffortNone {
		t.Fatalf("decision = %+v, want greeting/none", decision)
	}
	if len(recaller.requests) != 0 || classifier.calls != 0 {
		t.Fatalf("a standalone greeting read memory (%d) or embedded (%d)", len(recaller.requests), classifier.calls)
	}

	fixed, _ := newReadingAgent(t, readingSetup{text: "Buongiorno!", override: llm.ReasoningEffortLow, reading: reading})
	if decision, _ := fixed.readTurn(context.Background()); decision.EffortSource != EffortSourceUser || decision.EffortRequested != llm.ReasoningEffortLow {
		t.Fatalf("decision = %+v, want the composer's choice to beat the greeting path", decision)
	}
}

// "ok" after a request acknowledges that request: without Standalone it is read like any turn.
func TestReadTurnContextualAcknowledgementIsNotAGreeting(t *testing.T) {
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierHigh, Margin: 0.3}, ok: true}
	a, _ := newReadingAgent(t, readingSetup{text: "ok", classifier: classifier, reading: memoryReading(&fakeRecaller{})})
	decision, _ := a.readTurn(context.Background())
	if decision.EffortSource != EffortSourceSeeds || decision.EffortRequested != llm.ReasoningEffortHigh {
		t.Fatalf("decision = %+v, want the seed verdict for a contextual ok", decision)
	}
}

func TestReadTurnReusesACompatibleLabelUserBeforeTeacher(t *testing.T) {
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierNone, Margin: 0.5}, ok: true}
	user := recalledLabel(EffortSourceUser, "high", 0.06)
	teacher := recalledLabel(EffortSourceTeacher, "low", 0.02)
	recaller := &fakeRecaller{recall: TurnRecall{UserLabels: []RecalledTurn{user}, TeacherLabels: []RecalledTurn{teacher}}}
	a, client := newReadingAgent(t, readingSetup{classifier: classifier, reading: memoryReading(recaller)})

	decision, read := a.readTurn(context.Background())
	if decision.EffortSource != EffortSourceMemory || decision.EffortRequested != llm.ReasoningEffortHigh || decision.OriginRef != user.SourceRef {
		t.Fatalf("decision = %+v, want the user label's high from %s", decision, user.SourceRef)
	}
	if read.label.SourceRef != user.SourceRef || classifier.calls != 0 || len(client.requests) != 0 {
		t.Fatalf("label %+v, classifier calls %d, teacher calls %d", read.label, classifier.calls, len(client.requests))
	}

	onlyTeacher := &fakeRecaller{recall: TurnRecall{TeacherLabels: []RecalledTurn{teacher}}}
	b, _ := newReadingAgent(t, readingSetup{classifier: classifier, reading: memoryReading(onlyTeacher)})
	if decision, _ := b.readTurn(context.Background()); decision.EffortSource != EffortSourceMemory || decision.EffortRequested != llm.ReasoningEffortLow {
		t.Fatalf("decision = %+v, want the teacher label's low", decision)
	}
}

func TestReadTurnReclampsALabelAndRefusesAnUnknownEffort(t *testing.T) {
	label := recalledLabel(EffortSourceUser, "xhigh", 0.03)
	recaller := &fakeRecaller{recall: TurnRecall{UserLabels: []RecalledTurn{label}}}
	a, _ := newReadingAgent(t, readingSetup{reading: memoryReading(recaller), cfg: func(c *llm.Config) {
		c.SupportedReasoningEfforts = []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh}
	}})
	decision, _ := a.readTurn(context.Background())
	if decision.EffortRequested != llm.ReasoningEffortXHigh || decision.Effort != llm.ReasoningEffortHigh {
		t.Fatalf("decision = %+v, want xhigh requested and high applied on this route", decision)
	}

	unknown := &fakeRecaller{recall: TurnRecall{UserLabels: []RecalledTurn{recalledLabel(EffortSourceUser, "turbo", 0.01)}}}
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierLow, Margin: 0.4}, ok: true}
	b, _ := newReadingAgent(t, readingSetup{classifier: classifier, reading: memoryReading(unknown)})
	if decision, read := b.readTurn(context.Background()); decision.EffortSource != EffortSourceSeeds || read.recallMiss != "no_compatible_label" {
		t.Fatalf("decision = %+v, miss %q; want an unknown remembered effort to be a named miss", decision, read.recallMiss)
	}
}

func TestReadTurnSeedsAndTeacherFollowTheMargin(t *testing.T) {
	for _, test := range []struct {
		name       string
		classifier *fakeClassifier
		teacher    []string
		source     string
		effort     llm.ReasoningEffort
		asked      int
	}{
		{name: "seeds above the margin", classifier: &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierHigh, Margin: 0.075}, ok: true}, source: EffortSourceSeeds, effort: llm.ReasoningEffortHigh},
		{name: "teacher below the margin", classifier: &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierLow, Margin: 0.05}, ok: true}, teacher: []string{`{"tier":"high"}`}, source: EffortSourceTeacher, effort: llm.ReasoningEffortHigh, asked: 1},
		{name: "seeds when the teacher fails", classifier: &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierLow, Margin: 0.05}, ok: true}, teacher: []string{""}, source: EffortSourceSeeds, effort: llm.ReasoningEffortLow, asked: 1},
		{name: "seeds when the teacher answers nonsense", classifier: &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierNone, Margin: 0.01}, ok: true}, teacher: []string{"maybe"}, source: EffortSourceSeeds, effort: llm.ReasoningEffortNone, asked: 1},
		{name: "static low when the embedding fails", classifier: &fakeClassifier{ok: false}, source: EffortSourceFallback, effort: llm.ReasoningEffortLow},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, client := newReadingAgent(t, readingSetup{classifier: test.classifier, teacher: test.teacher})
			decision, _ := a.readTurn(context.Background())
			if decision.EffortSource != test.source || decision.EffortRequested != test.effort {
				t.Fatalf("decision = %+v, want %s/%s", decision, test.source, test.effort)
			}
			if len(client.requests) != test.asked {
				t.Fatalf("teacher asked %d times, want %d", len(client.requests), test.asked)
			}
		})
	}
}

func TestReadTurnWithoutAClassifierUsesTheSameTeacher(t *testing.T) {
	a, client := newReadingAgent(t, readingSetup{teacher: []string{`{"tier":"none"}`}})
	if decision, read := a.readTurn(context.Background()); decision.EffortSource != EffortSourceTeacher ||
		decision.EffortRequested != llm.ReasoningEffortNone || read.teacher != teacherSuccess {
		t.Fatalf("decision = %+v (teacher %q), want teacher/none", decision, read.teacher)
	}
	if client.requests[0].ToolChoice != "none" || len(client.requests[0].Tools) != 0 {
		t.Fatal("the teacher request is not the tool-free router request")
	}

	b, _ := newReadingAgent(t, readingSetup{teacher: []string{""}})
	if decision, read := b.readTurn(context.Background()); decision.EffortSource != EffortSourceFallback ||
		decision.EffortRequested != llm.ReasoningEffortLow || read.teacher != teacherError {
		t.Fatalf("decision = %+v (teacher %q), want fallback/low with the failure recorded", decision, read.teacher)
	}
}

// Requested and applied are kept apart: a mandatory-reasoning model turns a decided none into
// low on the wire, and the decision still says none.
func TestReadTurnClampsEveryEffortAndKeepsTheRequest(t *testing.T) {
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierNone, Margin: 0.5}, ok: true}
	a, _ := newReadingAgent(t, readingSetup{classifier: classifier, cfg: func(c *llm.Config) {
		c.SupportedReasoningEfforts = []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh}
		c.ReasoningMandatory = true
	}})
	decision, _ := a.readTurn(context.Background())
	if decision.EffortRequested != llm.ReasoningEffortNone || decision.Effort != llm.ReasoningEffortLow {
		t.Fatalf("decision = %+v, want none requested and low applied", decision)
	}
}

func TestReadTurnReadsWithoutMemoryWhenRecallHangs(t *testing.T) {
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierLow, Margin: 0.3}, ok: true}
	a, _ := newReadingAgent(t, readingSetup{classifier: classifier, reading: memoryReading(&fakeRecaller{hang: true})})
	started := time.Now()
	decision, read := a.readTurn(context.Background())
	if elapsed := time.Since(started); elapsed > recallTimeout+time.Second {
		t.Fatalf("readTurn took %v with a hung memory, want about recallTimeout (%v)", elapsed, recallTimeout)
	}
	if decision.EffortSource != EffortSourceSeeds || read.recallMiss != "recall_error" {
		t.Fatalf("decision = %+v, miss %q; want the seeds and a recorded recall error", decision, read.recallMiss)
	}
}

func TestReadTurnPreloadsOnlyRegisteredDeferredTools(t *testing.T) {
	recaller := &fakeRecaller{recall: TurnRecall{ToolTurns: []RecalledTurn{
		{Distance: 0.01, SourceRef: "postgres://aura/conversations/a/turns/1", Tools: []string{"clock_now", "tool_search", "unmounted_tool"}},
		{Distance: 0.03, SourceRef: "postgres://aura/conversations/b/turns/1", Tools: []string{"web_search", "calendar_add", "web_search"}},
		{Distance: 0.05, SourceRef: "postgres://aura/conversations/c/turns/1", Tools: []string{"notes_add"}},
	}}}
	a, _ := newReadingAgent(t, readingSetup{override: llm.ReasoningEffortLow, reading: memoryReading(recaller)})
	_, read := a.readTurn(context.Background())
	if !slices.Equal(read.preloaded, []string{"calendar_add", "web_search"}) || read.toolTurn.SourceRef != "postgres://aura/conversations/b/turns/1" {
		t.Fatalf("preloaded %v from %s, want the first candidate with eligible tools, sorted", read.preloaded, read.toolTurn.SourceRef)
	}
	for _, name := range []string{"clock_now", "tool_search", "unmounted_tool", "notes_add"} {
		if _, ok := a.activated[name]; ok {
			t.Errorf("%s was activated", name)
		}
	}

	many := &fakeRecaller{recall: TurnRecall{ToolTurns: []RecalledTurn{{Distance: 0.01, SourceRef: "postgres://aura/conversations/d/turns/1",
		Tools: []string{"web_search", "weather_lookup", "pdf_merge", "notes_add", "calendar_add"}}}}}
	b, _ := newReadingAgent(t, readingSetup{override: llm.ReasoningEffortLow, reading: memoryReading(many)})
	if _, read := b.readTurn(context.Background()); len(read.preloaded) != preloadMax {
		t.Fatalf("preloaded %v, want at most %d", read.preloaded, preloadMax)
	}
}

func TestReadTurnRecallRequestCarriesTheTurnsKeys(t *testing.T) {
	recaller := &fakeRecaller{}
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierLow, Margin: 0.3}, ok: true}
	a, _ := newReadingAgent(t, readingSetup{classifier: classifier, reading: memoryReading(recaller)})
	a.readTurn(context.Background())
	if len(recaller.requests) != 1 {
		t.Fatalf("recall requests = %d, want 1", len(recaller.requests))
	}
	got := recaller.requests[0]
	want := TurnRecallRequest{
		Text: "che tempo fa domani a Cuneo?", ContextKey: "ctx1:k", RouteKey: routeKey(a.cfg),
		PolicyVersion: turnPolicyVersion, SourceRef: "postgres://aura/conversations/now/turns/1",
		DeferredTools: []string{"calendar_add", "notes_add", "pdf_merge", "weather_lookup", "web_search"}, IncludeLabels: true,
	}
	if got.Text != want.Text || got.ContextKey != want.ContextKey || got.RouteKey != want.RouteKey ||
		got.PolicyVersion != want.PolicyVersion || got.SourceRef != want.SourceRef ||
		!slices.Equal(got.DeferredTools, want.DeferredTools) || got.IncludeLabels != want.IncludeLabels {
		t.Fatalf("recall request = %+v, want %+v", got, want)
	}
}

func TestReadTurnWithoutAContextKeyAsksNoMemory(t *testing.T) {
	recaller := &fakeRecaller{}
	reading := memoryReading(recaller)
	reading.ContextKey = ""
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierLow, Margin: 0.3}, ok: true}
	a, _ := newReadingAgent(t, readingSetup{classifier: classifier, reading: reading})
	if _, read := a.readTurn(context.Background()); len(recaller.requests) != 0 || read.recallMiss != "context_ineligible" {
		t.Fatalf("recall requests = %d, miss %q; want none and context_ineligible", len(recaller.requests), read.recallMiss)
	}
}

// A route that takes no adaptive effort decides nothing but still preloads.
func TestReadTurnOnARouteWithoutEffortStillPreloads(t *testing.T) {
	recaller := &fakeRecaller{recall: TurnRecall{ToolTurns: []RecalledTurn{{Distance: 0.01, SourceRef: "postgres://aura/conversations/e/turns/1", Tools: []string{"web_search"}}}}}
	a, _ := newReadingAgent(t, readingSetup{reading: memoryReading(recaller), cfg: func(c *llm.Config) { c.AdaptiveReasoning = false }})
	decision, read := a.readTurn(context.Background())
	if decision.EffortSource != "" || decision.EffortRequested != "" || decision.RouteKey == "" {
		t.Fatalf("decision = %+v, want no effort but the route named", decision)
	}
	if !slices.Equal(read.preloaded, []string{"web_search"}) || len(recaller.requests) != 1 || recaller.requests[0].IncludeLabels {
		t.Fatalf("preloaded %v after requests %+v, want a tool-only read", read.preloaded, recaller.requests)
	}
}

// The preload must reach round 1 (spec, "How they load"), and the decision is handed over
// once, before that request.
func TestRunPreloadsBeforeTheFirstRequestAndReportsTheDecisionOnce(t *testing.T) {
	var order []string
	recaller := &fakeRecaller{recall: TurnRecall{ToolTurns: []RecalledTurn{{Distance: 0.01, SourceRef: "postgres://aura/conversations/f/turns/1", Tools: []string{"weather_lookup"}}}}}
	reading := memoryReading(recaller)
	var decisions []TurnDecision
	reading.OnDecision = func(d TurnDecision) {
		decisions = append(decisions, d)
		order = append(order, "decision")
	}
	a, client := newReadingAgent(t, readingSetup{override: llm.ReasoningEffortLow, reading: reading})
	client.onRequest = func() { order = append(order, "request") }
	budget, err := NewBudget(BudgetOptions{})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	if _, err := collectInternal(a.Run(InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7()), Budget: budget})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(decisions) != 1 || decisions[0].EffortSource != EffortSourceUser {
		t.Fatalf("decisions = %+v, want exactly one", decisions)
	}
	if len(client.requests) == 0 {
		t.Fatal("no request was sent")
	}
	names := make([]string, 0, len(client.requests[0].Tools))
	for _, def := range client.requests[0].Tools {
		names = append(names, def.Function.Name)
	}
	if !slices.Contains(names, "weather_lookup") {
		t.Fatalf("round-1 tools = %v, want the preloaded weather_lookup", names)
	}
	if len(order) < 2 || order[0] != "decision" || order[1] != "request" {
		t.Fatalf("order = %v, want the decision reported before the first request", order)
	}
}

func TestReadTurnCountsItsDecisionSource(t *testing.T) {
	recorded, reader := newTestAgentMetrics(t)
	previous := metrics
	metrics = recorded
	t.Cleanup(func() { metrics = previous })

	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierHigh, Margin: 0.3}, ok: true}
	a, _ := newReadingAgent(t, readingSetup{classifier: classifier})
	a.readTurn(context.Background())

	sum, ok := findOTelMetric(t, reader, "aura.agent.turn.decision").Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 || !hasOTelLabel(sum.DataPoints[0].Attributes.ToSlice(), "outcome", "seeds") || sum.DataPoints[0].Value != 1 {
		t.Fatalf("turn decisions = %+v, want one seeds decision", sum.DataPoints)
	}
}
```


- [ ] **Step 5: Run them to verify they fail**

Run: `W 'go test -race -count=1 -run "TestReadTurn|TestRunPreloads" ./internal/agent/'`
Expected: FAIL to compile — `readTurn`, `TurnReading` field, `recallTimeout`, `preloadMax` undefined.

- [ ] **Step 6: Write the turn reading**

Create `internal/agent/llm_agent_turn_reading.go`:

```go
package agent

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/redact"
)

const (
	// teacherMargin: below it the seed bank's verdict is uncertain and the teacher is asked.
	// Calibrated on the 58-case gate on 2026-10-06 (prd.md §6), where seeds plus the teacher
	// below it scored 58/58; not independently validated (spec, "Constants").
	teacherMargin = 0.075
	// recallTimeout bounds the whole recall: client acquisition, space checks, the embedding
	// and every pool query. Proposed, not measured (spec, "Constants").
	recallTimeout = 500 * time.Millisecond
	// preloadMax is how many remembered deferred tools load before round 1: the most a tool
	// turn used in the lab VM's small sample. Every one adds its schema to the request.
	preloadMax = 3
)

// turnRead is what one reading saw, for its log line and the frozen evaluation: the seed
// verdict and the teacher's answer behind the decision, the label and the tool turn recall
// offered, and why recall offered nothing when it did not.
type turnRead struct {
	seedTier        prompt.ReasoningTier
	seedMargin      float64
	seedOK          bool
	teacherTier     prompt.ReasoningTier
	teacher         teacherOutcome
	label           RecalledTurn
	toolTurn        RecalledTurn
	preloaded       []string
	recallMiss      string
	recallDuration  time.Duration
	seedDuration    time.Duration
	teacherDuration time.Duration
}

// readTurn runs once per turn, before the first request: it decides the effort and preloads
// the deferred tools similar past turns ran (spec 2026-10-06, "Effort decision" and "Tool
// preload"). The first matching row of the spec's table wins:
//
//	composer effort > standalone greeting > compatible label (user, then teacher) >
//	seeds with margin >= teacherMargin > teacher > seeds; static low when nothing answers.
func (a *LlmAgent) readTurn(ctx context.Context) (TurnDecision, turnRead) {
	user := prompt.LastGenuineUserContent(a.history)
	decision := TurnDecision{RouteKey: routeKey(a.cfg), PolicyVersion: turnPolicyVersion}
	var read turnRead
	target := prompt.IsReasoningTarget(a.cfg.Provider, a.cfg.BaseURL)
	adaptive := a.cfg.AdaptiveReasoning && target
	greeting := a.turnReading.Standalone && prompt.IsTrivialGreeting(user)

	var recall TurnRecall
	if !greeting {
		started := time.Now()
		recall, read.recallMiss = a.recallTurns(ctx, user, decision.RouteKey, adaptive && a.reasoningOverride == "")
		read.recallDuration = time.Since(started)
		read.preloaded, read.toolTurn = a.preloadTools(recall.ToolTurns)
	}
	switch {
	case a.reasoningOverride != "":
		decision.EffortSource, decision.EffortRequested = EffortSourceUser, a.reasoningOverride
		if target {
			decision.Effort = a.cfg.ClampReasoningEffort(a.reasoningOverride)
		}
	case !adaptive:
		// This route takes no adaptive effort: nothing to decide, and the context key and
		// any preloaded tools still count.
	case greeting:
		decision.decide(a.cfg, llm.ReasoningEffortNone, EffortSourceGreeting)
	default:
		a.decideAdaptive(ctx, user, recall, &decision, &read)
	}
	a.logTurnRead(decision, read)
	recordTurnDecision(decision.EffortSource)
	return decision, read
}

func (d *TurnDecision) decide(cfg llm.Config, requested llm.ReasoningEffort, source string) {
	d.EffortRequested, d.Effort, d.EffortSource = requested, cfg.ClampReasoningEffort(requested), source
}

func (a *LlmAgent) decideAdaptive(ctx context.Context, user string, recall TurnRecall, d *TurnDecision, read *turnRead) {
	if label, ok := reusableLabel(recall); ok {
		read.label = label
		d.decide(a.cfg, llm.ReasoningEffort(label.RequestedEffort), EffortSourceMemory)
		d.OriginRef = label.SourceRef
		return
	}
	if read.recallMiss == "" {
		// Memory answered, and no row it returned is a label this turn may reuse.
		read.recallMiss = "no_compatible_label"
	}
	if strings.TrimSpace(user) == "" {
		d.decide(a.cfg, llm.ReasoningEffortLow, EffortSourceFallback)
		return
	}
	if a.classifier == nil {
		if tier, ok := a.teach(ctx, user, read); ok {
			d.decide(a.cfg, tier.Effort(), EffortSourceTeacher)
			return
		}
		d.decide(a.cfg, llm.ReasoningEffortLow, EffortSourceFallback)
		return
	}
	started := time.Now()
	verdict, ok := a.classifier.Classify(ctx, user)
	read.seedDuration = time.Since(started)
	if !ok {
		d.decide(a.cfg, llm.ReasoningEffortLow, EffortSourceFallback)
		return
	}
	read.seedTier, read.seedMargin, read.seedOK = verdict.Tier, verdict.Margin, true
	if verdict.Margin >= teacherMargin {
		d.decide(a.cfg, verdict.Tier.Effort(), EffortSourceSeeds)
		return
	}
	if tier, ok := a.teach(ctx, user, read); ok {
		d.decide(a.cfg, tier.Effort(), EffortSourceTeacher)
		return
	}
	d.decide(a.cfg, verdict.Tier.Effort(), EffortSourceSeeds)
}

func (a *LlmAgent) teach(ctx context.Context, user string, read *turnRead) (prompt.ReasoningTier, bool) {
	started := time.Now()
	tier, outcome := a.askTeacher(ctx, user)
	read.teacherTier, read.teacher, read.teacherDuration = tier, outcome, time.Since(started)
	return tier, outcome == teacherSuccess
}

// reusableLabel is the nearest user label, otherwise the nearest teacher label. A remembered
// effort that is not on the ladder is a miss, never a guess.
func reusableLabel(recall TurnRecall) (RecalledTurn, bool) {
	for _, pool := range [][]RecalledTurn{recall.UserLabels, recall.TeacherLabels} {
		for _, turn := range pool {
			if llm.ReasoningEffort(turn.RequestedEffort).Known() {
				return turn, true
			}
		}
	}
	return RecalledTurn{}, false
}

// recallTurns asks memory once, bounded by recallTimeout. A failure is one warning and the
// turn is read without memory; it never fails the turn.
func (a *LlmAgent) recallTurns(ctx context.Context, user, route string, labels bool) (TurnRecall, string) {
	reading := a.turnReading
	switch {
	case reading.Recaller == nil:
		return TurnRecall{}, "no_memory"
	case reading.ContextKey == "":
		return TurnRecall{}, "context_ineligible"
	case strings.TrimSpace(user) == "":
		return TurnRecall{}, "no_text"
	}
	ctx, cancel := context.WithTimeout(ctx, recallTimeout)
	defer cancel()
	recall, err := reading.Recaller.RecallTurns(ctx, TurnRecallRequest{
		Text: user, ContextKey: reading.ContextKey, RouteKey: route, PolicyVersion: turnPolicyVersion,
		SourceRef: reading.SourceRef, DeferredTools: a.deferredToolNames(), IncludeLabels: labels,
	})
	if err != nil {
		slog.Warn("turn recall failed; reading the turn without memory",
			"thread_id", redact.Line(a.sessionID), "err", redact.Line(err.Error()))
		return TurnRecall{}, "recall_error"
	}
	return recall, ""
}

// preloadTools adds the remembered tools of the nearest tool turn that still has any to the
// promoted set, the same set a tool_search result promotes into. It loads schemas only: no
// remembered call is executed and no past argument or authorization is reused.
func (a *LlmAgent) preloadTools(turns []RecalledTurn) ([]string, RecalledTurn) {
	for _, turn := range turns {
		names := a.preloadable(turn.Tools)
		if len(names) == 0 {
			continue
		}
		for _, name := range names {
			a.activated[name] = struct{}{}
		}
		return names, turn
	}
	return nil, RecalledTurn{}
}

// preloadable keeps the names still registered as deferred tools, without tool_search, sorted
// for a deterministic round-1 tools array, and at most preloadMax of them.
func (a *LlmAgent) preloadable(names []string) []string {
	if a.registry == nil {
		return nil
	}
	var out []string
	for _, name := range names {
		if name == searchTool || slices.Contains(out, name) {
			continue
		}
		if tool, ok := a.registry.Get(name); ok && tool.Spec().Deferred {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out[:min(len(out), preloadMax)]
}

func (a *LlmAgent) deferredToolNames() []string {
	if a.registry == nil {
		return nil
	}
	var names []string
	for _, tool := range a.registry.All() {
		if spec := tool.Spec(); spec.Deferred && spec.Name != searchTool {
			names = append(names, spec.Name)
		}
	}
	slices.Sort(names)
	return names
}

// logTurnRead is the one line per turn the spec's Observability section names. The label and
// the tool turn can be different past turns, so each has its own origin and distance.
func (a *LlmAgent) logTurnRead(d TurnDecision, read turnRead) {
	slog.Info("adaptive reasoning: turn read",
		"thread_id", redact.Line(a.sessionID),
		"source", d.EffortSource, "requested", string(d.EffortRequested), "effort", string(d.Effort),
		"seed_tier", string(read.seedTier), "seed_margin", read.seedMargin,
		"teacher", string(read.teacher), "teacher_tier", string(read.teacherTier),
		"label_origin", redact.Line(read.label.SourceRef), "label_distance", read.label.Distance,
		"tool_turn_origin", redact.Line(read.toolTurn.SourceRef), "tool_turn_distance", read.toolTurn.Distance,
		"recall_miss", read.recallMiss, "recall_ms", read.recallDuration.Milliseconds(),
		"seed_ms", read.seedDuration.Milliseconds(), "teacher_ms", read.teacherDuration.Milliseconds(),
		"preloaded", read.preloaded, "route", d.RouteKey, "policy", d.PolicyVersion)
}
```

In `internal/agent/llm_agent_construct.go`, add to `LlmAgentConfig` (after `BackgroundCalls`):

```go
	// TurnReading is what the runner hands a dispatched user turn: the identity's memory, the
	// turn's context key and source, and where its effort decision goes. The zero value reads
	// the turn from seeds and the teacher alone.
	TurnReading TurnReading
```

and `turnReading: cfg.TurnReading,` to the `LlmAgent` literal. In `internal/agent/llm_agent.go`, add the field after `reasoningOverride`:

```go
	// turnReading carries the dispatched turn's memory binding and decision sink (readTurn).
	turnReading TurnReading
```

Replace the run-loop declarations `var adaptiveEffort llm.ReasoningEffort` / `var adaptiveEffortSet bool` with `var turnEffort llm.ReasoningEffort` / `var turnRead bool`, and the Task 4 block with:

```go
			} else if !turnRead {
				decision, _ := a.readTurn(ic.Ctx)
				turnEffort, turnRead = decision.EffortRequested, true
				if a.turnReading.OnDecision != nil {
					a.turnReading.OnDecision(decision)
				}
			}
```

and pass `turnEffort` to `prepareReasoningRequest`. Update the comment above that block: "The turn is read once, before its first request (readTurn): a fixed composer effort still reads memory for tools; buildRequest applies the override, else the decided effort." Delete `adaptiveReasoningTier` from `internal/agent/llm_agent_reasoning.go` (keep `tierClassifier`, `resolveClassifier`, `teacherOutcome`, `askTeacher`, `teacherFailure`, `reasoningRouterTimeout`).

Add the decision counter: in `internal/obs/catalog.go`, `AgentTurnDecisionsID InstrumentID = "agent_turn_decisions"` (after `AgentTeacherAttemptsID`), the descriptor right after the teacher one: `count(AgentTurnDecisionsID, "aura.agent.turn.decision", "aura_agent_turn_decision_total", []AttributeKey{AttributeOutcome}, "Total turn effort decisions by source."),`, and the outcome values `"user", "teacher", "memory", "seeds", "greeting", "fallback", "undecided"` (append after `"invalid"`; `success`/`error` etc. stay). In `internal/obs/catalog_test.go`, after the `agent_teacher_attempts|…` golden line:

```
agent_turn_decisions|aura.agent.turn.decision|aura_agent_turn_decision_total|counter|1|outcome|Total turn effort decisions by source.|
```

In `internal/agent/metrics.go`: field `turnDecisionsTotal metric.Int64Counter`, constructor line `turnDecisionsTotal: mustInt64Counter(meter, obs.AgentTurnDecisionsID),`, and:

```go
func recordTurnDecision(source string) { metrics.recordTurnDecision(source) }

func (m *agentMetrics) recordTurnDecision(source string) {
	if source == "" {
		source = "undecided"
	}
	label := obs.NormalizeAttribute(obs.AttributeOutcome, source)
	m.turnDecisionsTotal.Add(context.Background(), 1, metric.WithAttributes(boundedAttr(obs.AttributeOutcome, label)))
}
```

- [ ] **Step 7: Keep the live tier on the production path**

In `internal/agent/reasoning_tier_live_test.go` (tag `reasoning_live`), the agent had `Client` nil because a classifier hit skipped the router. An uncertain verdict now asks the teacher, so give it a client that refuses (the reading then keeps the seed verdict) and read the turn the production way. Replace the agent's `Client`-less construction with `Client: refusingTeacher{},`, add:

```go
// refusingTeacher answers no router request, so an uncertain turn keeps its seed verdict and
// this test still measures the seed bank alone.
type refusingTeacher struct{}

func (refusingTeacher) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	return nil, errors.New("no teacher in the seed-bank live test")
}
```

set `TurnReading: TurnReading{Standalone: true},` on the same config (each case is the first message of a fresh conversation, as production dispatches it), and replace the whole `for _, tc := range cases { ... }` loop with:

```go
	for _, tc := range cases {
		a.history = []llm.Message{{Role: llm.RoleSystem, Content: SystemPrompt}, {Role: llm.RoleUser, Content: tc.prompt}}
		decision, read := a.readTurn(context.Background())
		if decision.EffortSource != EffortSourceSeeds && decision.EffortSource != EffortSourceGreeting {
			t.Errorf("readTurn(%q) source = %q, want seeds or greeting (classifier path failed live)", tc.prompt, decision.EffortSource)
			continue
		}
		if decision.EffortRequested != tc.want.Effort() {
			t.Errorf("readTurn(%q) = %q, want %q", tc.prompt, decision.EffortRequested, tc.want.Effort())
			continue
		}
		t.Logf("ok: %q -> %s via %s (margin %.3f, teacher %q)", tc.prompt, decision.EffortRequested, decision.EffortSource, read.seedMargin, read.teacher)
	}
```

Replace the file's header comment `(adaptiveReasoningTier) WITHOUT calling the LLM router` with `(readTurn) without a teacher answer: the client refuses every router request, so an uncertain verdict keeps the seed bank's tier`. Add `"errors"` to its imports.

- [ ] **Step 8: Run the task's tests and the ones it touched**

Run: `W 'time go vet ./internal/agent/ ./internal/llm/ ./internal/obs/ && time go vet -tags reasoning_live ./internal/agent/ && time go test -race -count=1 -run "TestReadTurn|TestRunPreloads|TestTurnContextKey|TestRouteKey|TestTurnPolicyVersion|Reasoning|Adaptive|Teacher|ModelRound|Metrics" ./internal/agent/ && time go test -race -count=1 -run "TestReasoningEffortKnown|Clamp" ./internal/llm/ && time go test -race -count=1 -run TestCatalog ./internal/obs/'`
Expected: PASS, including every existing `TestLlmAgent_AdaptiveReasoning*` test and `TestReasoningRouterDoesNotConsumePrimaryModelRound` (no classifier: the teacher decides, as before).

- [ ] **Step 9: Commit**

```bash
W 'git add internal/agent/turn_recall.go internal/agent/llm_agent_turn_reading.go internal/agent/turn_recall_test.go internal/agent/llm_agent_turn_reading_test.go && LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -q -F <wsl path of the message file> -- internal/agent internal/llm/reasoning_clamp.go internal/llm/reasoning_clamp_test.go internal/obs/catalog.go internal/obs/catalog_test.go'
```

Message subject: `feat(agent): read each turn once to decide its effort and preload remembered tools`.

---
### Task 6: The runner binds memory to the dispatched turn and persists its decision

**Files:**
- Create: `internal/runner/runner_persist_pause.go` (pure move out of `runner_persist.go`)
- Create: `internal/runner/runner_turn_recall.go` (`TurnDecisionStore`, `TurnRecallStore`, `readTurnContext`, the recall binding, `recordTurnDecision`)
- Modify: `internal/runner/interfaces.go` (`ConversationStore.AppendTurnSeq`)
- Modify: `internal/runner/runner_deps.go` (`Deps.TurnDecisions`, `Deps.TurnRecall`)
- Modify: `internal/runner/runner.go` (fields; `turnLocked`; `appendUserTurn`; `buildAgent`)
- Modify: `internal/runner/runner_persist.go` (`turnTracker` fields; `persistAssistantAnswer`)
- Modify: `cmd/aura/chat_boot_memory.go` (`tenantTurnRecall`, `wireChatTurnRecall`), `cmd/aura/chat_boot.go` (one wiring line), `cmd/aura/cachefakes.go` (`memConvStore.AppendTurnSeq`)
- Modify (fakes and call sites): `internal/runner/fakes_test.go`, `internal/runner/runner_memory_projection_test.go`, `internal/runner/runner_budget_test.go`, `internal/agui/server_run_steer_e2e_test.go`, `cmd/aura/cmdfakes_test.go`
- Create: `internal/runner/runner_turn_recall_test.go`, `cmd/aura/chat_boot_memory_test.go` (or append to it if it exists)

**Interfaces:**
- Consumes: `conversations.TurnDecision`, `(*conversations.Store).AppendTurnSeq`, `(*conversations.Store).RecordTurnDecision` (Task 1); `arcadedb.TurnRecallRequest`, `arcadedb.TurnRecall`, `arcadedb.RecalledTurn`, `(*arcadedb.Client).RecallTurns` (Task 3); `agent.TurnReading`, `agent.TurnDecision`, `agent.TurnRecaller`, `agent.TurnRecallRequest`, `agent.TurnRecall`, `agent.RecalledTurn`, `agent.TurnContextKey`, `agent.EffortSource*`, `LlmAgentConfig.TurnReading` (Task 5).
- Produces:
  - `type TurnDecisionStore interface { RecordTurnDecision(ctx context.Context, conversationID string, seq int, d conversations.TurnDecision) error }`
  - `type TurnRecallStore interface { RecallTurns(ctx context.Context, request arcadedb.TurnRecallRequest) (arcadedb.TurnRecall, error) }`
  - `Deps.TurnDecisions TurnDecisionStore`, `Deps.TurnRecall TurnRecallStore`
  - `ConversationStore.AppendTurnSeq(ctx context.Context, p conversations.AppendTurnParams) (int, error)`

- [ ] **Step 1: Move the pause persistence out of `runner_persist.go`**

`internal/runner/runner_persist.go` is at 597 lines. Move `persistPause`, `flushPause`, `assistantAskUserToolCalls` and `pauseOptionsJSON` (with their doc comments, byte for byte) into a new `internal/runner/runner_persist_pause.go` with `package runner` and the imports they use, then fix both files' imports:

Run: `W 'goimports -w internal/runner/runner_persist.go internal/runner/runner_persist_pause.go && time go vet ./internal/runner/ && time go test -race -count=1 -run "Pause|Multipause|AskUser" ./internal/runner/ && wc -l internal/runner/runner_persist.go internal/runner/runner_persist_pause.go'`
Expected: PASS, and `runner_persist.go` well under 600. Nothing else changes in this step.

- [ ] **Step 2: Write the failing runner tests**

Create `internal/runner/runner_turn_recall_test.go`:

```go
package runner

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

type recordedDecision struct {
	convID   string
	seq      int
	decision conversations.TurnDecision
}

type recordingDecisionStore struct {
	mu      sync.Mutex
	records []recordedDecision
	err     error
}

func (s *recordingDecisionStore) RecordTurnDecision(_ context.Context, convID string, seq int, d conversations.TurnDecision) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, recordedDecision{convID: convID, seq: seq, decision: d})
	return s.err
}

func (s *recordingDecisionStore) snapshot() []recordedDecision {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.records)
}

type recordingRecallStore struct {
	mu       sync.Mutex
	recall   arcadedb.TurnRecall
	requests []arcadedb.TurnRecallRequest
}

func (s *recordingRecallStore) RecallTurns(_ context.Context, request arcadedb.TurnRecallRequest) (arcadedb.TurnRecall, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, request)
	return s.recall, nil
}

func (s *recordingRecallStore) snapshot() []arcadedb.TurnRecallRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

type deferredProbeTool struct{ name string }

func (p deferredProbeTool) Spec() tools.Spec {
	return tools.Spec{Name: p.name, Summary: "Deferred probe.", Description: "Deferred probe.",
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: true}
}

func (deferredProbeTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	return tools.NewResult(ctx, "ok")
}

// reasoningRoute is a route that takes an adaptive effort; the test runner's default
// config is not a reasoning target.
func reasoningRoute() llm.Config {
	return llm.Config{Model: "test-model", Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1",
		ContextWindow: 1000000, MaxOutputTokens: 32768, AdaptiveReasoning: true}
}

func decisionRunner(t *testing.T, cfg llm.Config, turns ...agenttest.FakeTurn) (*Runner, *fakeConvStore, *agenttest.FakeClient, *recordingDecisionStore) {
	t.Helper()
	client := agenttest.NewFakeClient(turns...)
	r, conv, _ := newTestRunnerCfg(t, client, cfg)
	decisions := &recordingDecisionStore{}
	r.turnDecisions = decisions
	return r, conv, client, decisions
}

func userSeqs(conv *fakeConvStore, convID string) []int {
	conv.mu.Lock()
	defer conv.mu.Unlock()
	var seqs []int
	for _, turn := range conv.turns[convID] {
		if turn.Role == llm.RoleUser {
			seqs = append(seqs, turn.Seq)
		}
	}
	return seqs
}

func TestTurnRecordsTheDecisionOnTheDispatchedUserTurn(t *testing.T) {
	r, conv, _, decisions := decisionRunner(t, reasoningRoute(), agenttest.ToolCallTurn(textResponseCall("call-1", "fatto")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortHigh)

	if _, err := drain(r.Turn(ctx, convID, new("riscrivi questa funzione in Go con i test"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	records := decisions.snapshot()
	seqs := userSeqs(conv, convID)
	if len(records) != 1 || len(seqs) != 1 || records[0].convID != convID || records[0].seq != seqs[0] {
		t.Fatalf("decisions %+v, user seqs %v; want one decision on the dispatched user turn", records, seqs)
	}
	d := records[0].decision
	if d.EffortSource != agent.EffortSourceUser || d.EffortRequested != "high" || d.Effort != "high" ||
		d.ContextKey != agent.TurnContextKey(nil, "") || !strings.HasPrefix(d.RouteKey, "route1:") ||
		!strings.HasPrefix(d.PolicyVersion, "policy1:") || d.OriginRef != "" {
		t.Fatalf("decision = %+v", d)
	}
}

func TestEachTurnRecordsItsDecisionOnItsOwnUserTurn(t *testing.T) {
	r, conv, _, decisions := decisionRunner(t, reasoningRoute(),
		agenttest.ToolCallTurn(textResponseCall("call-1", "una poesia")),
		agenttest.ToolCallTurn(textResponseCall("call-2", "a poem")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortLow)
	for _, text := range []string{"scrivi una poesia sul mare", "ora traducila in inglese"} {
		if _, err := drain(r.Turn(ctx, convID, new(text))); err != nil {
			t.Fatalf("turn %q: %v", text, err)
		}
	}
	records := decisions.snapshot()
	seqs := userSeqs(conv, convID)
	if len(records) != 2 || len(seqs) != 2 || records[0].seq != seqs[0] || records[1].seq != seqs[1] {
		t.Fatalf("decisions %+v, user seqs %v; want one per user turn, in order", records, seqs)
	}
	if records[0].decision.ContextKey == records[1].decision.ContextKey {
		t.Fatal("the second turn carries the first turn's context key")
	}
}

func TestBranchRerunRecordsNoDecision(t *testing.T) {
	r, _, _, decisions := decisionRunner(t, reasoningRoute(),
		agenttest.ToolCallTurn(textResponseCall("call-1", "prima risposta")),
		agenttest.ToolCallTurn(textResponseCall("call-2", "risposta rifatta")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortLow)
	if _, err := drain(r.Turn(ctx, convID, new("spiegami la differenza tra TCP e UDP"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if _, err := drain(r.TurnBranch(ctx, convID, 0)); err != nil {
		t.Fatalf("branch re-run: %v", err)
	}
	if records := decisions.snapshot(); len(records) != 1 {
		t.Fatalf("decisions = %+v, want only the dispatched turn's", records)
	}
}

func TestFailedTurnRecordsNoDecision(t *testing.T) {
	r, _, _, decisions := decisionRunner(t, reasoningRoute(), agenttest.FakeTurn{Err: errFake})
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortHigh)
	if _, err := drain(r.Turn(ctx, convID, new("analizza questo log di errore"))); err == nil {
		t.Fatal("the turn was expected to fail")
	}
	if records := decisions.snapshot(); len(records) != 0 {
		t.Fatalf("a failed turn recorded %+v", records)
	}
}

func TestPausedTurnRecordsItsDecisionAtThePause(t *testing.T) {
	r, conv, _, decisions := decisionRunner(t, reasoningRoute(), agenttest.ToolCallTurn(askUserCall("call-1", "Procedo?", "approval")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortLow)
	if _, err := drain(r.Turn(ctx, convID, new("cancella i file temporanei nella cartella download"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	records := decisions.snapshot()
	seqs := userSeqs(conv, convID)
	if len(records) != 1 || records[0].seq != seqs[0] || records[0].decision.EffortSource != agent.EffortSourceUser {
		t.Fatalf("decisions %+v, user seqs %v; want the decision written at the pause flush", records, seqs)
	}
}

func TestExplicitNoneIsRecordedAsNone(t *testing.T) {
	r, _, _, decisions := decisionRunner(t, reasoningRoute(), agenttest.ToolCallTurn(textResponseCall("call-1", "ok")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortNone)
	if _, err := drain(r.Turn(ctx, convID, new("elenca i file nella cartella"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	if records := decisions.snapshot(); len(records) != 1 || records[0].decision.EffortRequested != "none" || records[0].decision.Effort != "none" {
		t.Fatalf("decisions = %+v, want an explicit none, not NULL", records)
	}
}

// A route without adaptive effort still writes the context key and the route, so a later
// read knows the turn existed under that route and context.
func TestTurnWithoutAnEffortStillRecordsItsContext(t *testing.T) {
	r, _, _, decisions := decisionRunner(t, llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		agenttest.ToolCallTurn(textResponseCall("call-1", "ok")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(context.Background(), convID, new("elenca i file nella cartella"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	records := decisions.snapshot()
	if len(records) != 1 {
		t.Fatalf("decisions = %+v, want one", records)
	}
	if d := records[0].decision; d.ContextKey == "" || d.RouteKey == "" || d.EffortSource != "" || d.Effort != "" || d.EffortRequested != "" {
		t.Fatalf("decision = %+v, want the context and route without an effort", d)
	}
}

func TestTurnSurvivesAFailedDecisionWrite(t *testing.T) {
	r, conv, _, decisions := decisionRunner(t, reasoningRoute(), agenttest.ToolCallTurn(textResponseCall("call-1", "fatto")))
	decisions.err = errFake
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortLow)
	if _, err := drain(r.Turn(ctx, convID, new("scrivi un haiku"))); err != nil {
		t.Fatalf("a failed decision write failed the turn: %v", err)
	}
	conv.mu.Lock()
	defer conv.mu.Unlock()
	if !slices.ContainsFunc(conv.turns[convID], func(turn conversations.AppendTurnParams) bool {
		return turn.Role == llm.RoleAssistant && turn.Content == "fatto"
	}) {
		t.Fatalf("turns = %+v, want the answer persisted", conv.turns[convID])
	}
}

func TestTurnBindsTheIdentitysMemoryAndPreloadsItsTools(t *testing.T) {
	r, conv, client, _ := decisionRunner(t, reasoningRoute(), agenttest.ToolCallTurn(textResponseCall("call-1", "fatto")))
	r.registry.Register(deferredProbeTool{name: "recall_probe"})
	recall := &recordingRecallStore{recall: arcadedb.TurnRecall{ToolTurns: []arcadedb.RecalledTurn{{
		Distance: 0.02, SourceRef: "postgres://aura/conversations/past/turns/1", Tools: []string{"recall_probe"},
	}}}}
	r.turnRecall = recall
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx := WithReasoningOverride(context.Background(), llm.ReasoningEffortLow)
	const text = "prenota il solito tavolo per venerdì"
	if _, err := drain(r.Turn(ctx, convID, new(text))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	owner, err := conv.Get(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	requests := recall.snapshot()
	if len(requests) != 1 {
		t.Fatalf("recall requests = %d, want 1", len(requests))
	}
	got := requests[0]
	seqs := userSeqs(conv, convID)
	if got.IdentityID != owner.IdentityID || got.Text != text || got.ContextKey != agent.TurnContextKey(nil, "") ||
		got.SourceRef != reasoningSourceRef(convID, seqs[0]) || got.IncludeLabels ||
		!slices.Contains(got.DeferredTools, "recall_probe") {
		t.Fatalf("recall request = %+v", got)
	}
	if !slices.ContainsFunc(client.Requests[0].Tools, func(def llm.ToolDef) bool { return def.Function.Name == "recall_probe" }) {
		t.Fatal("the first request does not carry the remembered recall_probe")
	}
}

func TestReadTurnContextKeysWhatTheModelReadsBeforeTheMessage(t *testing.T) {
	visible := "che tempo fa domani a Cuneo?"
	always := "<skills>meteo: usa weather_lookup</skills>"
	memory := "<memory_context>vive a Cuneo</memory_context>"
	cfg := conversations.ContextConfig{AlwaysBlock: always,
		TransientContext: &conversations.TransientContext{Content: memory, BeforeCurrentUser: true}}
	history := []llm.Message{
		{Role: llm.RoleUser, Content: always},
		{Role: llm.RoleUser, Content: memory},
		{Role: llm.RoleUser, Content: visible},
	}
	instructions := []llm.Message{{Role: llm.RoleUser, Content: always}}

	got := readTurnContext(history, turnInput{visibleUserMsg: &visible}, cfg, nil)
	if got.key != agent.TurnContextKey(instructions, "") || !got.standalone {
		t.Fatalf("turn context = %+v, want the always-block in the key and the turn standalone", got)
	}

	// The memory block is rebuilt every turn; it must not move the key.
	other := "<memory_context>ha un cane</memory_context>"
	moved := slices.Clone(history)
	moved[1].Content = other
	movedCfg := cfg
	movedCfg.TransientContext = &conversations.TransientContext{Content: other, BeforeCurrentUser: true}
	if readTurnContext(moved, turnInput{visibleUserMsg: &visible}, movedCfg, nil).key != got.key {
		t.Fatal("the per-turn memory block moved the context key")
	}

	model := "<knowledge_base>\nmanuale.pdf\n</knowledge_base>\n\nUser message:\n" + visible
	withBlocks := readTurnContext(history, turnInput{visibleUserMsg: &visible, modelUserMsg: &model}, cfg, nil)
	if withBlocks.key != agent.TurnContextKey(instructions, strings.TrimSuffix(model, visible)) || withBlocks.key == got.key {
		t.Fatalf("catalog blocks are not in the key: %+v", withBlocks)
	}

	if readTurnContext(history, turnInput{visibleUserMsg: &visible}, cfg, []string{"attachment-1"}).key != "" {
		t.Fatal("a turn with attachments got a context key")
	}
	rewritten := "in breve: " + visible + " (rispondi in una riga)"
	if readTurnContext(history, turnInput{visibleUserMsg: &visible, modelUserMsg: &rewritten}, cfg, nil).key != "" {
		t.Fatal("a model message that does not end with the typed text got a context key")
	}
	if readTurnContext(history, turnInput{}, cfg, nil) != (turnContext{}) {
		t.Fatal("a run without a dispatched message got a context")
	}
}

func TestReadTurnContextIsStandaloneOnlyWithoutConversation(t *testing.T) {
	visible := "ok"
	fresh := []llm.Message{{Role: llm.RoleSystem, Content: "persisted system turn"}, {Role: llm.RoleUser, Content: visible}}
	if got := readTurnContext(fresh, turnInput{visibleUserMsg: &visible}, conversations.ContextConfig{}, nil); !got.standalone || got.key != agent.TurnContextKey(nil, "") {
		t.Fatalf("fresh turn context = %+v, want standalone with the empty-history key", got)
	}

	afterCode := []llm.Message{
		{Role: llm.RoleUser, Content: "riscrivi questa funzione in Go con i test"},
		{Role: llm.RoleAssistant, Content: "Ecco il piano."},
		{Role: llm.RoleUser, Content: visible},
	}
	afterThanks := []llm.Message{
		{Role: llm.RoleUser, Content: "grazie dell'aiuto"},
		{Role: llm.RoleAssistant, Content: "Prego!"},
		{Role: llm.RoleUser, Content: visible},
	}
	code := readTurnContext(afterCode, turnInput{visibleUserMsg: &visible}, conversations.ContextConfig{}, nil)
	thanks := readTurnContext(afterThanks, turnInput{visibleUserMsg: &visible}, conversations.ContextConfig{}, nil)
	if code.standalone || thanks.standalone {
		t.Fatal("an ok after an exchange was read as standalone")
	}
	if code.key == thanks.key || code.key == agent.TurnContextKey(nil, "") {
		t.Fatal("the same ok after two different exchanges shares a context key")
	}
}
```

`errFake`, `textResponseCall`, `askUserCall`, `drain`, `mustCreate`, `newConvID` and `newTestRunnerCfg` are the package's existing test helpers (`fakes_test.go`, `runner_test.go`).

- [ ] **Step 3: Run them to verify they fail**

Run: `W 'go test -race -count=1 -run "TestTurnRecords|TestEachTurn|TestBranchRerun|TestFailedTurn|TestPausedTurn|TestExplicitNone|TestTurnWithoutAnEffort|TestTurnSurvives|TestTurnBinds|TestReadTurnContext" ./internal/runner/'`
Expected: FAIL to compile — `turnDecisions`, `turnRecall`, `readTurnContext`, `turnContext` undefined.

- [ ] **Step 4: Write the binding**

Create `internal/runner/runner_turn_recall.go`:

```go
package runner

import (
	"context"
	"log/slog"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/redact"
)

// TurnDecisionStore persists how a user turn's effort was decided (migration 0137).
// *conversations.Store satisfies it.
type TurnDecisionStore interface {
	RecordTurnDecision(ctx context.Context, conversationID string, seq int, d conversations.TurnDecision) error
}

// TurnRecallStore reads an identity's past turns; the composition root binds the tenant
// memory clients behind it.
type TurnRecallStore interface {
	RecallTurns(ctx context.Context, request arcadedb.TurnRecallRequest) (arcadedb.TurnRecall, error)
}

// turnContext is what the dispatched message's reading needs from the loaded history.
type turnContext struct {
	key        string
	standalone bool
}

// readTurnContext keys the dispatched message by what the model reads before it: the
// always-block (governing instructions), every prior message, and the context blocks the
// message was composed with. The per-turn memory block is left out because it is rebuilt
// from memory on every turn, and a leading persisted system turn because the agent sends
// its own. A message with attachments, or whose model text does not end with what was
// typed, gets no key: its input is not versioned, so it is never a reusable label.
func readTurnContext(history []llm.Message, input turnInput, cfg conversations.ContextConfig, attachments []string) turnContext {
	if input.visibleUserMsg == nil || len(attachments) > 0 {
		return turnContext{}
	}
	visible := *input.visibleUserMsg
	current := -1
	for index, message := range slices.Backward(history) {
		if message.Role == llm.RoleUser && message.Content == visible {
			current = index
			break
		}
	}
	if current < 0 {
		return turnContext{}
	}
	blocks := ""
	if input.modelUserMsg != nil {
		prefix, ok := strings.CutSuffix(*input.modelUserMsg, visible)
		if !ok {
			return turnContext{}
		}
		blocks = prefix
	}
	transient := ""
	if cfg.TransientContext != nil {
		transient = cfg.TransientContext.Content
	}
	prior := make([]llm.Message, 0, current)
	standalone := true
	for index, message := range history[:current] {
		switch {
		case index == 0 && message.Role == llm.RoleSystem:
		case message.Role == llm.RoleUser && transient != "" && message.Content == transient:
		case message.Role == llm.RoleUser && cfg.AlwaysBlock != "" && message.Content == cfg.AlwaysBlock:
			prior = append(prior, message)
		default:
			standalone = false
			prior = append(prior, message)
		}
	}
	return turnContext{key: agent.TurnContextKey(prior, blocks), standalone: standalone}
}

// turnReading binds the dispatched user turn to the identity's memory and to the tracker
// its decision is written from. A run without a dispatched user row (resume, branch re-run)
// gets the zero reading: it reads no memory and writes no decision.
func (r *Runner) turnReading(ctx context.Context, tr *turnTracker, turn turnContext) agent.TurnReading {
	if tr.userTurnSeq <= 0 {
		return agent.TurnReading{}
	}
	reading := agent.TurnReading{
		ContextKey: turn.key,
		SourceRef:  reasoningSourceRef(tr.convID, tr.userTurnSeq),
		Standalone: turn.standalone,
		OnDecision: func(d agent.TurnDecision) { tr.decision = &d },
	}
	if identityID := identityctx.IdentityID(ctx); r.turnRecall != nil && identityID != "" {
		reading.Recaller = identityTurnRecaller{store: r.turnRecall, identityID: identityID}
	}
	return reading
}

// identityTurnRecaller is the agent's recall port bound to one identity.
type identityTurnRecaller struct {
	store      TurnRecallStore
	identityID string
}

func (r identityTurnRecaller) RecallTurns(ctx context.Context, request agent.TurnRecallRequest) (agent.TurnRecall, error) {
	recall, err := r.store.RecallTurns(ctx, arcadedb.TurnRecallRequest{
		IdentityID: r.identityID, Text: request.Text, ContextKey: request.ContextKey,
		RouteKey: request.RouteKey, PolicyVersion: request.PolicyVersion, SourceRef: request.SourceRef,
		DeferredTools: request.DeferredTools, IncludeLabels: request.IncludeLabels,
	})
	if err != nil {
		return agent.TurnRecall{}, err
	}
	return agent.TurnRecall{
		UserLabels:    agentRecalledTurns(recall.UserLabels),
		TeacherLabels: agentRecalledTurns(recall.TeacherLabels),
		ToolTurns:     agentRecalledTurns(recall.ToolTurns),
	}, nil
}

func agentRecalledTurns(turns []arcadedb.RecalledTurn) []agent.RecalledTurn {
	out := make([]agent.RecalledTurn, len(turns))
	for index, turn := range turns {
		out[index] = agent.RecalledTurn(turn)
	}
	return out
}

// recordTurnDecision writes the turn's decision onto its user row once, when the round
// reaches a durable stop (its answer or its pause). The answer is already committed and the
// decision is provenance, so a failed write is a warning, never a failed turn.
func (r *Runner) recordTurnDecision(ctx context.Context, tr *turnTracker) {
	if r.turnDecisions == nil || tr.userTurnSeq <= 0 || tr.decisionRecorded {
		return
	}
	tr.decisionRecorded = true
	decision := conversations.TurnDecision{ContextKey: tr.contextKey}
	if d := tr.decision; d != nil {
		decision.Effort, decision.EffortRequested = string(d.Effort), string(d.EffortRequested)
		decision.EffortSource, decision.RouteKey = d.EffortSource, d.RouteKey
		decision.PolicyVersion, decision.OriginRef = d.PolicyVersion, d.OriginRef
	}
	if decision == (conversations.TurnDecision{}) {
		return
	}
	if err := r.turnDecisions.RecordTurnDecision(ctx, tr.convID, tr.userTurnSeq, decision); err != nil {
		slog.Warn("turn decision write failed; the turn is unaffected",
			"conv", redact.Line(tr.convID), "seq", tr.userTurnSeq, "err", redact.Line(err.Error()))
	}
}
```

In `internal/runner/runner_persist.go`, add to `turnTracker` after `llmRuntime`:

```go
	// userTurnSeq is the seq of the user row this run dispatched, 0 for a resumed run or a
	// branch re-run; contextKey and decision are that turn's reading, written back to the
	// row once (recordTurnDecision).
	userTurnSeq      int
	contextKey       string
	decision         *agent.TurnDecision
	decisionRecorded bool
```

and in `persistAssistantAnswer`, after `r.commitSourceTurn(ctx, tr, ev)` and before `r.offerConversationProjection(ctx)`:

```go
	r.recordTurnDecision(ctx, tr)
```

In `internal/runner/runner_persist_pause.go`, in `flushPause`, after the `CommitPause` error check and before `return nil`:

```go
	r.recordTurnDecision(ctx, tr)
```

In `internal/runner/interfaces.go`, after `AppendTurn` in `ConversationStore`:

```go
	// AppendTurnSeq is AppendTurn returning the seq the store assigned, so the runner can
	// address the user row it dispatched (the turn's effort decision is written back to it).
	AppendTurnSeq(ctx context.Context, p conversations.AppendTurnParams) (int, error)
```

In `internal/runner/runner_deps.go`, add to `Deps` after `ReasoningDeletion`:

```go
	// TurnDecisions persists how each dispatched user turn's effort was decided (migration
	// 0137). nil leaves the decision in the turn's log line only.
	TurnDecisions TurnDecisionStore
	// TurnRecall reads the identity's past turns for the effort decision and the tool
	// preload. nil reads every turn from seeds and the teacher alone.
	TurnRecall TurnRecallStore
```

and `turnDecisions: d.TurnDecisions,` / `turnRecall: d.TurnRecall,` to the `Runner` literal in `New`. In `internal/runner/runner.go`, add the fields after `reasoningDeletion`:

```go
	turnDecisions         TurnDecisionStore
	turnRecall            TurnRecallStore
```

Replace `appendUserTurn` with:

```go
// appendUserTurn persists the user message as the next turn, together with whatever was
// attached to it (migration 0116), and returns the seq the store gave it: the turn's effort
// decision is written back to exactly that row. The ids ride the context because the HTTP
// layer is where they are known and validated; a request that attached nothing carries none
// and the column stays NULL.
func (r *Runner) appendUserTurn(ctx context.Context, convID, content string) (int, error) {
	seq, err := r.Conv.AppendTurnSeq(ctx, conversations.AppendTurnParams{
		ConversationID: convID, Role: llm.RoleUser, Content: content,
		AttachmentIDs: assets.TurnAttachments(ctx),
	})
	if err != nil {
		return 0, err
	}
	r.offerConversationProjection(ctx)
	return seq, nil
}
```

In `turnLocked`, replace

```go
		if input.visibleUserMsg != nil {
			if err := r.appendUserTurn(ctx, convID, *input.visibleUserMsg); err != nil {
				yield(nil, err)
				return
			}
```

with

```go
		userTurnSeq := 0
		if input.visibleUserMsg != nil {
			seq, err := r.appendUserTurn(ctx, convID, *input.visibleUserMsg)
			if err != nil {
				yield(nil, err)
				return
			}
			userTurnSeq = seq
```

then replace

```go
		la, ic, cancelAgent, err := r.buildAgent(
			ctx, convID, requestID, agentHistory,
		)
```

with

```go
		tr := &turnTracker{convID: convID, llmRuntime: turnRuntime, userTurnSeq: userTurnSeq}
		turn := readTurnContext(history, input, cfg, assets.TurnAttachments(ctx))
		tr.contextKey = turn.key
		la, ic, cancelAgent, err := r.buildAgent(
			ctx, convID, requestID, agentHistory, r.turnReading(ctx, tr, turn),
		)
```

and delete the later line `tr := &turnTracker{convID: convID, llmRuntime: turnRuntime}` (keep the comment block above it, which still describes the loop). The fast-reply path keeps its own tracker: it builds no agent and decides nothing.

Give `buildAgent` a last parameter `reading agent.TurnReading` and add `TurnReading: reading,` after `BackgroundCalls:` in its `LlmAgentConfig` literal; extend its doc comment with: "reading binds the dispatched user turn to memory and to its decision sink (runner_turn_recall.go); the zero value is a run without a dispatched turn."

- [ ] **Step 5: Give every conversation fake `AppendTurnSeq`**

`internal/runner/fakes_test.go`, after `AppendTurn`:

```go
func (f *fakeConvStore) AppendTurnSeq(_ context.Context, p conversations.AppendTurnParams) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p = f.assignTurnSeqLocked(p)
	if err := f.appendTurnLocked(p); err != nil {
		return 0, err
	}
	return p.Seq, nil
}
```

`internal/runner/runner_memory_projection_test.go`, after `postCommitConversationStore.AppendTurn` (it must keep recording the user turn the runner now appends through `AppendTurnSeq`):

```go
func (s *postCommitConversationStore) AppendTurnSeq(ctx context.Context, p conversations.AppendTurnParams) (int, error) {
	seq, err := s.fakeConvStore.AppendTurnSeq(ctx, p)
	if err != nil {
		return 0, err
	}
	s.source.record(ctx, p)
	return seq, nil
}
```

and in the same file change the four `r.appendUserTurn(...)` / `failedRunner.appendUserTurn(...)` calls to `if _, err := ...`.

`internal/runner/runner_budget_test.go`: `r.buildAgent(context.Background(), newConvID(t), uuid.New(), nil, agent.TurnReading{})`, adding the `internal/agent` import.

`internal/agui/server_run_steer_e2e_test.go`, after `AppendTurn`:

```go
func (f *steerE2EConvStore) AppendTurnSeq(_ context.Context, p conversations.AppendTurnParams) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.Seq <= 0 {
		p.Seq = len(f.turns[p.ConversationID]) + 1
	}
	f.appendLocked(p)
	return p.Seq, nil
}
```

`cmd/aura/cachefakes.go` (a production file: `aura cache-audit` drives a real round over it), after `AppendTurn`:

```go
func (m *memConvStore) AppendTurnSeq(_ context.Context, p conversations.AppendTurnParams) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p = m.assignTurnSeqLocked(p)
	m.appendTurnFieldsLocked(p)
	return p.Seq, nil
}
```

`cmd/aura/cmdfakes_test.go`, after `AppendTurn`:

```go
func (f *cmdConvFake) AppendTurnSeq(_ context.Context, p conversations.AppendTurnParams) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p = f.assignTurnSeqLocked(p)
	f.appendTurnFieldsLocked(p)
	return p.Seq, nil
}
```

(`ctxRecordingConv` in `runner_interrupted_test.go` embeds `*fakeConvStore` and only overrides `AppendTurn` for the interrupted-round write, so it needs nothing.)

- [ ] **Step 6: Run the task's tests and the ones it touched**

Run: `W 'time go vet ./internal/runner/ ./internal/agui/ ./cmd/aura/ && time go test -race -count=1 -run "TestTurn|TestEachTurn|TestBranchRerun|TestFailedTurn|TestPausedTurn|TestExplicitNone|TestReadTurnContext|TestConversationProjection|TestRunnerBudget|TestRecordInterrupted|Pause" ./internal/runner/ && time go test -race -count=1 -run Steer ./internal/agui/'`
Expected: PASS, including the new tests and the existing turn, projection, budget and pause tests (`TestTurn_AppendUserTurnError` still sees the injected append error through `AppendTurnSeq`).

- [ ] **Step 7: Wire production**

In `cmd/aura/chat_boot_memory.go`, append:

```go
// tenantTurnRecall reads an identity's past turns through Existing: an identity that has
// never written memory has nothing to recall, and a read must not provision a database.
type tenantTurnRecall struct {
	clients *arcadedb.TenantClients
}

func (s tenantTurnRecall) RecallTurns(ctx context.Context, request arcadedb.TurnRecallRequest) (arcadedb.TurnRecall, error) {
	client, ok, err := s.clients.Existing(ctx, request.IdentityID)
	if err != nil || !ok {
		return arcadedb.TurnRecall{}, err
	}
	return client.RecallTurns(ctx, request)
}

// wireChatTurnRecall gives every dispatched turn its decision sink and, when memory is
// configured, its recall.
func wireChatTurnRecall(deps *runner.Deps, clients *arcadedb.TenantClients, decisions runner.TurnDecisionStore) {
	deps.TurnDecisions = decisions
	if clients != nil {
		deps.TurnRecall = tenantTurnRecall{clients: clients}
	}
}
```

In `cmd/aura/chat_boot.go`, after `wireChatReasoningMemory(&deps, reasoningMemory)`:

```go
	wireChatTurnRecall(&deps, memoryClients, convStore)
```

Add the test to `cmd/aura/chat_boot_memory_test.go` (create it with `package main` if it does not exist):

```go
func TestWireChatTurnRecallBindsMemoryOnlyWhenConfigured(t *testing.T) {
	decisions := &conversations.Store{}
	var without runner.Deps
	wireChatTurnRecall(&without, nil, decisions)
	if without.TurnDecisions == nil || without.TurnRecall != nil {
		t.Fatalf("without memory: decisions %v, recall %v; want decisions only", without.TurnDecisions, without.TurnRecall)
	}
	var with runner.Deps
	wireChatTurnRecall(&with, new(arcadedb.TenantClients), decisions)
	if _, ok := with.TurnRecall.(tenantTurnRecall); !ok {
		t.Fatalf("with memory: recall = %T, want tenantTurnRecall", with.TurnRecall)
	}
}
```

(imports: `testing`, `internal/arcadedb`, `internal/conversations`, `internal/runner`).

Run: `W 'time go vet ./cmd/aura/ && time go test -race -count=1 -run "TestWireChatTurnRecall|CacheAudit" ./cmd/aura/ && wc -l cmd/aura/chat_boot.go internal/runner/runner.go internal/runner/runner_persist.go'`
Expected: PASS; every file under 600 lines.

- [ ] **Step 8: Prove the write lands on the real user row**

The fakes prove the runner addresses the seq it was given; only the real store proves that seq is the user row it appended. Create `internal/runner/runner_turn_decision_integration_test.go`:

```go
//go:build db_integration

package runner

import (
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/jackc/pgx/v5"
)

func TestTurnWritesTheDecisionToTheRealUserRow(t *testing.T) {
	pool := migratedRunnerPool(t)
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("call-1", "fatto")))
	r, convStore, _ := newIntegrationRunner(t, pool, client)
	r.turnDecisions = convStore
	convID := newIntegrationConversation(t, pool, convStore)
	ctx := WithReasoningOverride(ownerCtx(), llm.ReasoningEffortHigh)

	const text = "riscrivi questa funzione in Go con i test"
	if _, err := drain(r.Turn(ctx, convID, new(text))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	var (
		rows                                  int
		role, content, requested, source, key string
		effort                                *string
	)
	asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ownerCtx(),
			`SELECT count(*) FROM aura.conversation_turns WHERE conversation_id = $1 AND reasoning_effort_source IS NOT NULL`,
			convID).Scan(&rows); err != nil {
			return err
		}
		return tx.QueryRow(ownerCtx(),
			`SELECT role, content, reasoning_effort, reasoning_effort_requested, reasoning_effort_source, recall_context_key
			   FROM aura.conversation_turns WHERE conversation_id = $1 AND reasoning_effort_source IS NOT NULL`,
			convID).Scan(&role, &content, &effort, &requested, &source, &key)
	})
	// The integration runner's route is not a reasoning target: the composer's choice is
	// recorded as requested, and nothing was sent, so the applied effort stays NULL.
	if rows != 1 || role != "user" || content != text || effort != nil || requested != "high" ||
		source != agent.EffortSourceUser || key != agent.TurnContextKey(nil, "") {
		t.Fatalf("rows %d: role %q content %q effort %v requested %q source %q key %q", rows, role, content, effort, requested, source, key)
	}
}
```

Run recipe **P** with `-run "TestAppendTurnSeq|TestRecordTurnDecision|TestProjectionAndDump" ./internal/conversations/` and again with `-run TestTurnWritesTheDecisionToTheRealUserRow ./internal/runner/`.
Expected: PASS. If the key differs from `agent.TurnContextKey(nil, "")`, print the loaded history: the real loader injected a message the fake does not, and `readTurnContext` must classify it, not the test.

- [ ] **Step 9: Commit**

```bash
W 'git add internal/runner/runner_persist_pause.go internal/runner/runner_turn_recall.go internal/runner/runner_turn_recall_test.go internal/runner/runner_turn_decision_integration_test.go cmd/aura/chat_boot_memory_test.go && LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -q -F <wsl path of the message file> -- internal/runner cmd/aura/chat_boot_memory.go cmd/aura/chat_boot_memory_test.go cmd/aura/chat_boot.go cmd/aura/cachefakes.go cmd/aura/cmdfakes_test.go internal/agui/server_run_steer_e2e_test.go'
```

Message subject: `feat(runner): bind memory to the dispatched turn and persist its decision`. Body: the pause move is a pure move; the decision is written at the round's first durable stop and never by a resumed or branch re-run.

---
### Task 7: The frozen evaluation

**Files:**
- Create: `internal/agent/testdata/turn_recall_frozen_2026-10-07.json` (the frozen set)
- Create: `docs/verification/turn-recall-frozen-eval.md` (protocol and the set's SHA-256)
- Create: `internal/agent/turn_recall_eval_set_test.go` (untagged: the set's shape, its freeze, the report statistics)
- Create: `internal/agent/turn_recall_eval_test.go` (tag `turn_recall_eval`: the paid replay)

**Interfaces:**
- Consumes: `LlmAgent.readTurn`, `turnRead` (`seedTier`, `seedOK`, `teacher`, `label`, `recallMiss`, `recallDuration`), `TurnReading`, `TurnRecaller`, `TurnContextKey`, `EffortSource*` (Task 5); `arcadedb.Client.RecallTurns`, `arcadedb.TurnDecision`, `ConversationTurnProjection.Decision` (Tasks 2-3); `LlmAgentConfig.Classifier`; `openai_compat.New`; `arcadedb.NewMemoryEmbedder(config.LoadEmbed(), ...)`.
- Produces: nothing later tasks call. Task 8 cites the report.

The set measures the effort decision only; the tool preload is measured end to end in Task 8. Every family sits entirely in one split, so a label learned in calibration can never answer a final turn. "Hard" means the only accepted effort is `high`; the release criterion is about hard turns that memory sends to `none`.

- [ ] **Step 1: Write the frozen set**

Create `internal/agent/testdata/turn_recall_frozen_2026-10-07.json`:

```json
{
  "frozen_on": "2026-10-07",
  "rubric": {
    "none": "answerable from general knowledge or the conversation without tools or deliberation: greetings, thanks, acknowledgements, stable facts, one-step arithmetic, short translations",
    "low": "one direct lookup or tool action whose answer needs no planning: weather, news, prices, a reminder, a calendar read, a message, a simple explanation",
    "high": "multi-step work where under-reasoning returns a confident wrong answer: proofs, debugging, design, data analysis, refactoring, plans with constraints"
  },
  "conversations": [
    {"id": "cal-weather-1", "split": "calibration", "lang": "it", "turns": [
      {"id": "cal-weather-1.1", "family": "weather", "text": "domani a Bra piove?", "accepted": ["low"]}]},
    {"id": "cal-weather-2", "split": "calibration", "lang": "it", "turns": [
      {"id": "cal-weather-2.1", "family": "weather", "text": "mi dici le previsioni per sabato a Mondovì?", "accepted": ["low"]}]},
    {"id": "cal-weather-3", "split": "calibration", "lang": "en", "turns": [
      {"id": "cal-weather-3.1", "family": "weather", "text": "what's the forecast for Turin this weekend?", "accepted": ["low"]}]},

    {"id": "cal-script-1", "split": "calibration", "lang": "it", "turns": [
      {"id": "cal-script-1.1", "family": "photo-rename-script", "text": "mi serve uno script bash che rinomini le foto della cartella in base alla data di scatto", "accepted": ["low", "high"]}]},
    {"id": "cal-script-2", "split": "calibration", "lang": "en", "turns": [
      {"id": "cal-script-2.1", "family": "photo-rename-script", "text": "write a bash script that renames every photo in a folder by the date it was taken", "accepted": ["low", "high"]}]},
    {"id": "cal-script-3", "split": "calibration", "lang": "it", "turns": [
      {"id": "cal-script-3.1", "family": "photo-rename-script", "text": "fammi uno script che rinomini le immagini usando la data EXIF", "accepted": ["low", "high"]}]},

    {"id": "cal-fact-1", "split": "calibration", "lang": "it", "turns": [
      {"id": "cal-fact-1.1", "family": "stable-fact", "text": "qual è la capitale dell'Australia?", "accepted": ["none", "low"]}]},
    {"id": "cal-fact-2", "split": "calibration", "lang": "en", "turns": [
      {"id": "cal-fact-2.1", "family": "stable-fact", "text": "what's the capital of Australia?", "accepted": ["none", "low"]}]},
    {"id": "cal-fact-3", "split": "calibration", "lang": "it", "turns": [
      {"id": "cal-fact-3.1", "family": "stable-fact", "text": "come si chiama la capitale australiana?", "accepted": ["none", "low"]}]},

    {"id": "cal-reminder-1", "split": "calibration", "lang": "it", "turns": [
      {"id": "cal-reminder-1.1", "family": "reminder", "text": "ricordami di ritirare la giacca in tintoria venerdì alle 18", "accepted": ["low", "none"]}]},
    {"id": "cal-reminder-2", "split": "calibration", "lang": "en", "turns": [
      {"id": "cal-reminder-2.1", "family": "reminder", "text": "remind me to pick up my jacket from the dry cleaner on Friday at 6pm", "accepted": ["low", "none"]}]},
    {"id": "cal-reminder-3", "split": "calibration", "lang": "it", "turns": [
      {"id": "cal-reminder-3.1", "family": "reminder", "text": "venerdì alle sei fammi ricordare la giacca in tintoria", "accepted": ["low", "none"]}]},

    {"id": "cal-translate-1", "split": "calibration", "lang": "it", "turns": [
      {"id": "cal-translate-1.1", "family": "translation", "text": "traduci in inglese: il treno è in ritardo di dieci minuti", "accepted": ["none", "low"]}]},
    {"id": "cal-translate-2", "split": "calibration", "lang": "it", "turns": [
      {"id": "cal-translate-2.1", "family": "translation", "text": "come si dice in inglese 'il treno ha dieci minuti di ritardo'?", "accepted": ["none", "low"]}]},

    {"id": "cal-percent-1", "split": "calibration", "lang": "it", "turns": [
      {"id": "cal-percent-1.1", "family": "percentage", "text": "quanto fa il 17% di 2350?", "accepted": ["none", "low"]}]},
    {"id": "cal-percent-2", "split": "calibration", "lang": "en", "turns": [
      {"id": "cal-percent-2.1", "family": "percentage", "text": "what is 17 percent of 2350?", "accepted": ["none", "low"]}]},

    {"id": "cal-greeting-1", "split": "calibration", "lang": "it", "turns": [
      {"id": "cal-greeting-1.1", "family": "greeting", "text": "buonasera!", "accepted": ["none"]}]},
    {"id": "cal-greeting-2", "split": "calibration", "lang": "en", "turns": [
      {"id": "cal-greeting-2.1", "family": "greeting", "text": "hey there!", "accepted": ["none"]}]},

    {"id": "cal-thanks-1", "split": "calibration", "lang": "it", "turns": [
      {"id": "cal-thanks-1.1", "family": "restaurant", "text": "trovami una trattoria vegetariana a Cuneo per stasera", "accepted": ["low"]},
      {"id": "cal-thanks-1.2", "family": "thanks-after-task", "text": "grazie, perfetto", "accepted": ["none", "low"]}]},
    {"id": "cal-thanks-2", "split": "calibration", "lang": "en", "turns": [
      {"id": "cal-thanks-2.1", "family": "restaurant", "text": "find me a vegetarian place in Turin for tonight", "accepted": ["low"]},
      {"id": "cal-thanks-2.2", "family": "thanks-after-task", "text": "thanks, that's great", "accepted": ["none", "low"]}]},

    {"id": "fin-proof-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-proof-1.1", "family": "irrationality-proof", "text": "dimostra che la radice quadrata di 2 è irrazionale", "accepted": ["high"]}]},
    {"id": "fin-proof-2", "split": "final", "lang": "en", "turns": [
      {"id": "fin-proof-2.1", "family": "irrationality-proof", "text": "prove that the square root of 2 is irrational", "accepted": ["high"]}]},
    {"id": "fin-proof-3", "split": "final", "lang": "it", "turns": [
      {"id": "fin-proof-3.1", "family": "irrationality-proof", "text": "fammi vedere perché √2 non può essere scritta come frazione", "accepted": ["high"]}]},

    {"id": "fin-sales-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-sales-1.1", "family": "sales-analysis", "text": "ho un CSV con le vendite mensili di tre negozi: come capisco se il calo di marzo è stagionale o un problema vero?", "accepted": ["high"]}]},
    {"id": "fin-sales-2", "split": "final", "lang": "en", "turns": [
      {"id": "fin-sales-2.1", "family": "sales-analysis", "text": "I have monthly sales for three shops in a CSV: how do I tell whether the March drop is seasonal or a real problem?", "accepted": ["high"]}]},

    {"id": "fin-docker-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-docker-1.1", "family": "docker-137", "text": "il mio container docker si chiude con codice 137 ogni notte verso le 3, come scopro perché?", "accepted": ["high"]}]},
    {"id": "fin-docker-2", "split": "final", "lang": "en", "turns": [
      {"id": "fin-docker-2.1", "family": "docker-137", "text": "my docker container keeps exiting with code 137 under load, how do I find the cause?", "accepted": ["high"]}]},

    {"id": "fin-schema-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-schema-1.1", "family": "booking-schema", "text": "progetta le tabelle per un gestionale di prenotazioni dei campi da tennis con abbonamenti e penali per le disdette", "accepted": ["high"]}]},
    {"id": "fin-schema-2", "split": "final", "lang": "en", "turns": [
      {"id": "fin-schema-2.1", "family": "booking-schema", "text": "design the tables for a tennis court booking system with memberships and late-cancellation fees", "accepted": ["high"]}]},

    {"id": "fin-refactor-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-refactor-1.1", "family": "nested-if-refactor", "text": "questa funzione Go ha tre livelli di if annidati e ripete la gestione degli errori: rifattorizzala senza cambiarne il comportamento e dimmi quali test scriveresti", "accepted": ["high"]}]},
    {"id": "fin-refactor-2", "split": "final", "lang": "en", "turns": [
      {"id": "fin-refactor-2.1", "family": "nested-if-refactor", "text": "this Go function nests three levels of ifs and repeats its error handling: refactor it without changing behaviour and tell me which tests you'd write", "accepted": ["high"]}]},

    {"id": "fin-negation-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-negation-1.1", "family": "negated-depth", "text": "non voglio una spiegazione lunga: il 2028 è bisestile?", "accepted": ["none", "low"]}]},
    {"id": "fin-compound-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-compound-1.1", "family": "conditional-plan", "text": "guarda il meteo di domenica a Cuneo e se non piove segnami in calendario la gita in bici", "accepted": ["low", "high"]}]},
    {"id": "fin-nocap-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-nocap-1.1", "family": "missing-capability", "text": "prenotami un volo per Lisbona per martedì mattina", "accepted": ["low", "none"]}]},
    {"id": "fin-attach-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-attach-1.1", "family": "attached-pdf-merge", "text": "unisci questi due PDF in un unico file", "accepted": ["low", "none"], "attachment": true}]},

    {"id": "fin-news-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-news-1.1", "family": "strike-news", "text": "cosa si sa dello sciopero dei treni di venerdì?", "accepted": ["low"]}]},
    {"id": "fin-news-2", "split": "final", "lang": "en", "turns": [
      {"id": "fin-news-2.1", "family": "strike-news", "text": "what's the latest on Friday's rail strike?", "accepted": ["low"]}]},
    {"id": "fin-price-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-price-1.1", "family": "fuel-price", "text": "quanto costa oggi un litro di gasolio in Piemonte?", "accepted": ["low"]}]},
    {"id": "fin-calendar-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-calendar-1.1", "family": "agenda-read", "text": "ho impegni mercoledì mattina?", "accepted": ["low", "none"]}]},
    {"id": "fin-message-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-message-1.1", "family": "send-message", "text": "scrivi a Giulia su Telegram che la cena è confermata per le nove", "accepted": ["low", "none"]}]},
    {"id": "fin-schedule-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-schedule-1.1", "family": "weekly-schedule", "text": "ogni domenica sera alle 21 mandami l'elenco della spesa della settimana", "accepted": ["low"]}]},

    {"id": "fin-tcp-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-tcp-1.1", "family": "tcp-udp", "text": "spiegami la differenza tra TCP e UDP con un esempio concreto", "accepted": ["low", "high"]}]},
    {"id": "fin-tcp-2", "split": "final", "lang": "en", "turns": [
      {"id": "fin-tcp-2.1", "family": "tcp-udp", "text": "explain the difference between TCP and UDP with a concrete example", "accepted": ["low", "high"]}]},

    {"id": "fin-sheet-1", "split": "final", "lang": "it", "turns": [
      {"id": "fin-sheet-1.1", "family": "file-reference", "text": "ieri ti ho caricato il file spese_2026.xlsx", "accepted": ["none", "low"]},
      {"id": "fin-sheet-1.2", "family": "chart-from-file", "text": "fammi un grafico delle spese per categoria da quel file", "accepted": ["low", "high"]}]},

    {"id": "fin-ok-code", "split": "final", "lang": "it", "turns": [
      {"id": "fin-ok-code.1", "family": "ok-setup-code", "text": "riscrivi questa funzione Go aggiungendo la gestione degli errori e i test", "accepted": ["high"]},
      {"id": "fin-ok-code.2", "family": "ok-ack", "text": "ok", "accepted": ["high"]}]},
    {"id": "fin-ok-thanks", "split": "final", "lang": "it", "turns": [
      {"id": "fin-ok-thanks.1", "family": "ok-setup-thanks", "text": "grazie per l'aiuto di oggi, mi hai risolto la giornata", "accepted": ["none"]},
      {"id": "fin-ok-thanks.2", "family": "ok-ack", "text": "ok", "accepted": ["none"]}]}
  ]
}
```

51 turns: 22 calibration, 29 final, 13 of them hard (all final). `ok-ack` is the one family whose turns disagree, on purpose: the same word after a code request and after thanks. Their context keys differ, so a label for one is never reused for the other.

- [ ] **Step 2: Write the set's tests (untagged, no network)**

Create `internal/agent/turn_recall_eval_set_test.go`:

```go
package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

const (
	frozenEvalSetPath   = "testdata/turn_recall_frozen_2026-10-07.json"
	frozenEvalProtocol  = "../../docs/verification/turn-recall-frozen-eval.md"
	frozenEvalHashLabel = "Dataset SHA-256: `"
)

type evalTurn struct {
	ID         string   `json:"id"`
	Family     string   `json:"family"`
	Text       string   `json:"text"`
	Accepted   []string `json:"accepted"`
	Attachment bool     `json:"attachment,omitempty"`
}

type evalConversation struct {
	ID    string     `json:"id"`
	Split string     `json:"split"`
	Lang  string     `json:"lang"`
	Turns []evalTurn `json:"turns"`
}

type evalSet struct {
	FrozenOn      string             `json:"frozen_on"`
	Rubric        map[string]string  `json:"rubric"`
	Conversations []evalConversation `json:"conversations"`
}

func (t evalTurn) hard() bool { return slices.Equal(t.Accepted, []string{"high"}) }

func (t evalTurn) accepts(effort llm.ReasoningEffort) bool {
	return slices.Contains(t.Accepted, string(effort))
}

func loadFrozenEvalSet(t *testing.T) (evalSet, []byte) {
	t.Helper()
	raw, err := os.ReadFile(frozenEvalSetPath)
	if err != nil {
		t.Fatalf("read the frozen set: %v", err)
	}
	var set evalSet
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatalf("decode the frozen set: %v", err)
	}
	return set, raw
}

func TestFrozenEvalSetIsWellFormed(t *testing.T) {
	set, _ := loadFrozenEvalSet(t)
	ids := map[string]bool{}
	familySplit := map[string]string{}
	langs := map[string]map[string]bool{"calibration": {}, "final": {}}
	turns, hard := 0, 0
	for _, conv := range set.Conversations {
		if conv.Split != "calibration" && conv.Split != "final" {
			t.Errorf("%s: split %q", conv.ID, conv.Split)
		}
		langs[conv.Split][conv.Lang] = true
		for _, turn := range conv.Turns {
			turns++
			if ids[turn.ID] {
				t.Errorf("duplicate id %s", turn.ID)
			}
			ids[turn.ID] = true
			if split, seen := familySplit[turn.Family]; seen && split != conv.Split {
				t.Errorf("family %s is in both splits", turn.Family)
			}
			familySplit[turn.Family] = conv.Split
			if strings.TrimSpace(turn.Text) == "" || len(turn.Accepted) == 0 {
				t.Errorf("%s: empty text or accepted set", turn.ID)
			}
			for _, effort := range turn.Accepted {
				if _, ok := set.Rubric[effort]; !ok {
					t.Errorf("%s: accepted %q is not in the rubric", turn.ID, effort)
				}
			}
			if turn.hard() {
				hard++
			}
		}
	}
	for split, seen := range langs {
		if !seen["it"] || !seen["en"] {
			t.Errorf("split %s lacks a language: %v", split, seen)
		}
	}
	if turns != 51 || hard != 13 {
		t.Errorf("turns = %d, hard = %d; the frozen set has 51 and 13", turns, hard)
	}
}

// The set measures what the seed bank does NOT already contain: no turn may be a copy of a
// seed, a router prompt line or a gate case in the prompt package.
func TestFrozenEvalSetDoesNotCopyTheSeedBank(t *testing.T) {
	set, _ := loadFrozenEvalSet(t)
	sources, err := filepath.Glob("prompt/*.go")
	if err != nil || len(sources) == 0 {
		t.Fatalf("prompt sources: %v (%d files)", err, len(sources))
	}
	var corpus strings.Builder
	for _, path := range sources {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		corpus.Write(raw)
	}
	text := strings.ToLower(corpus.String())
	for _, conv := range set.Conversations {
		for _, turn := range conv.Turns {
			if len(turn.Text) > 3 && strings.Contains(text, strings.ToLower(turn.Text)) {
				t.Errorf("%s copies the prompt package: %q", turn.ID, turn.Text)
			}
		}
	}
}

// The protocol names the set's digest, so an edit to the set without a new protocol entry
// fails here, not in a report nobody re-reads.
func TestFrozenEvalSetMatchesItsProtocol(t *testing.T) {
	_, raw := loadFrozenEvalSet(t)
	protocol, err := os.ReadFile(frozenEvalProtocol)
	if err != nil {
		t.Fatalf("read the protocol: %v", err)
	}
	sum := sha256.Sum256(raw)
	if want := frozenEvalHashLabel + hex.EncodeToString(sum[:]) + "`"; !strings.Contains(string(protocol), want) {
		t.Fatalf("the protocol does not name the set's digest %s", hex.EncodeToString(sum[:]))
	}
}

// wilson is the 95% Wilson score interval for successes out of n.
func wilson(successes, n int) (float64, float64) {
	if n == 0 {
		return 0, 0
	}
	const z = 1.959963984540054
	p, total := float64(successes)/float64(n), float64(n)
	denominator := 1 + z*z/total
	center := (p + z*z/(2*total)) / denominator
	half := z * math.Sqrt(p*(1-p)/total+z*z/(4*total*total)) / denominator
	return center - half, center + half
}

// percentile is the nearest-rank percentile of values, q in (0, 1].
func percentile(values []time.Duration, q float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return sorted[max(int(math.Ceil(q*float64(len(sorted))))-1, 0)]
}

func TestEvalStatistics(t *testing.T) {
	if lo, hi := wilson(8, 10); math.Abs(lo-0.4902) > 0.0005 || math.Abs(hi-0.9433) > 0.0005 {
		t.Errorf("wilson(8, 10) = (%.4f, %.4f), want (0.4902, 0.9433)", lo, hi)
	}
	if lo, hi := wilson(0, 0); lo != 0 || hi != 0 {
		t.Errorf("wilson(0, 0) = (%v, %v)", lo, hi)
	}
	var values []time.Duration
	for ms := 10; ms >= 1; ms-- {
		values = append(values, time.Duration(ms)*time.Millisecond)
	}
	if p50, p95 := percentile(values, 0.50), percentile(values, 0.95); p50 != 5*time.Millisecond || p95 != 10*time.Millisecond {
		t.Errorf("p50 = %v, p95 = %v; want 5ms and 10ms", p50, p95)
	}
	if percentile(nil, 0.5) != 0 {
		t.Error("percentile of nothing is not zero")
	}
}
```

- [ ] **Step 3: Write the protocol**

Create `docs/verification/turn-recall-frozen-eval.md`:

```markdown
# Turn recall — frozen evaluation protocol

What it measures: whether reusing remembered effort labels (plan 2 of the turn recall spec,
`docs/superpowers/specs/2026-10-06-turn-recall-design.md`) changes the effort decision for the
worse, and what it saves. It measures the effort only; the tool preload is measured end to
end on the lab VM (plan 2, Task 8).

## The set

`internal/agent/testdata/turn_recall_frozen_2026-10-07.json`, frozen on 2026-10-07: 51 user
turns in 46 conversations, Italian and English, split into calibration (22 turns) and final
(29 turns, 13 hard). Each turn lists the efforts the rubric accepts; a hard turn accepts only
`high`. Each family sits entirely in one split, so no calibration label can answer a final
turn. No turn copies the prompt package (`TestFrozenEvalSetDoesNotCopyTheSeedBank`).

Dataset SHA-256: `<the digest computed in Step 3>`

`TestFrozenEvalSetMatchesItsProtocol` fails if the set changes without this line. Changing the
set means a new file with a new date and a new entry below, never an edit in place.

## Arms

Every turn is read three ways, in conversation order, on the production path (`readTurn`):

1. **seeds** — the seed bank's verdict alone, whatever its margin.
2. **seeds + teacher** — the production decision with memory empty.
3. **recall** — the production decision with the identity's memory: a fresh ArcadeDB database
   per trial, into which every earlier turn of the trial was projected with the decision the
   recall arm made, exactly as the runner persists it.

The context key of a turn is `TurnContextKey` of its conversation's earlier turns (each
answered by a placeholder assistant message); a turn with an attachment has none, and a turn
with no earlier turn is standalone.

## Metrics

Per split and arm: accuracy (decided effort in the accepted set) with its 95% Wilson interval;
hard→none count; the decided-effort × accepted-set table. For the recall arm also: decision
sources, teacher share, teacher outcomes, memory precision (memory decisions in the accepted
set), recall latency p50/p95, and whole-reading latency p50/p95.

## Release criterion

On the final split, in every trial: no hard turn that the recall arm decided from memory as
`none` unless the seeds + teacher arm also decided it `none`. Memory may not add a hard→none.
Everything else is reported, not gated.

## Running

Paid: every uncertain turn asks the teacher, in two arms. Run only with the operator's OK.
Calibration may be run as often as needed. The final split runs once per frozen set and
constants; a changed constant (`teacherMargin`, `recallRadius`) needs a fresh calibration run
before the final.

    W 'source scripts/lib/disposable_stack.sh && export ARCADEDB_URL=http://127.0.0.1:2480 \
       ARCADEDB_PASSWORD="$(read_secret ARCADEDB_PASSWORD)" AURA_EMBED_BASE_URL=http://127.0.0.1:8081 \
       TURN_EVAL_LLM_PROVIDER=openrouter TURN_EVAL_LLM_BASE_URL=https://openrouter.ai/api/v1 \
       TURN_EVAL_LLM_MODEL=<the production primary model> TURN_EVAL_LLM_API_KEY="$(read_secret OPENROUTER_API_KEY)" \
       TURN_EVAL_SPLIT=calibration TURN_EVAL_TRIALS=3 TURN_EVAL_REPORT=/mnt/d/Aura/docs/verification/turn-recall-eval-<date>-<split>.md && \
       go test -tags turn_recall_eval -count=1 -timeout 60m -run TestTurnRecallFrozenEval ./internal/agent/'

## Results

| Date | Split | Model | Trials | Report | Criterion |
|---|---|---|---|---|---|
```

Compute the digest and write it into the line: `W 'sha256sum internal/agent/testdata/turn_recall_frozen_2026-10-07.json'`, then replace `<the digest computed in Step 3>` with the printed hex using the Edit tool.

Run: `W 'time go test -race -count=1 -run "TestFrozenEval|TestEvalStatistics" ./internal/agent/'`
Expected: PASS. If `TestFrozenEvalSetDoesNotCopyTheSeedBank` names a turn, rewrite that turn's text (keep its family and accepted set) and recompute the digest.

- [ ] **Step 4: Write the paid replay**

Create `internal/agent/turn_recall_eval_test.go`:

```go
//go:build turn_recall_eval

// The frozen turn-recall evaluation (docs/verification/turn-recall-frozen-eval.md). Paid:
// every uncertain turn asks the configured teacher. Run only with the operator's OK.
package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/google/uuid"
)

type evalEnv struct {
	cfg        llm.Config
	client     llm.Client
	classifier *prompt.ReasoningClassifier
}

type evalArm struct {
	effort llm.ReasoningEffort
	source string
}

type evalRecord struct {
	trial          int
	split          string
	turn           evalTurn
	seeds          evalArm
	baseline       evalArm
	recall         evalArm
	teacher        teacherOutcome
	labelDistance  float64
	recallDuration time.Duration
	reading        time.Duration
}

func requireEvalEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s is unset under CI", key)
		}
		t.Skipf("%s is unset; see docs/verification/turn-recall-frozen-eval.md", key)
	}
	return value
}

func newEvalEnv(t *testing.T) evalEnv {
	t.Helper()
	cfg := llm.Config{
		Provider: requireEvalEnv(t, "TURN_EVAL_LLM_PROVIDER"), BaseURL: requireEvalEnv(t, "TURN_EVAL_LLM_BASE_URL"),
		Model: requireEvalEnv(t, "TURN_EVAL_LLM_MODEL"), APIKey: requireEvalEnv(t, "TURN_EVAL_LLM_API_KEY"),
		AdaptiveReasoning: true, TotalTimeoutSec: 60, MaxTokens: 4096,
	}
	if !prompt.IsReasoningTarget(cfg.Provider, cfg.BaseURL) {
		t.Fatalf("%s at %s is not a reasoning target: nothing would be decided", cfg.Provider, cfg.BaseURL)
	}
	embed := config.LoadEmbed()
	embedder := &embeddings.Client{BaseURL: embed.BaseURL, Client: &http.Client{Timeout: 30 * time.Second}, Dimensions: embed.Dimensions}
	t.Cleanup(embedder.Client.CloseIdleConnections)
	return evalEnv{cfg: cfg, client: openai_compat.New(cfg), classifier: prompt.NewReasoningClassifier(embedder)}
}

func (e evalEnv) agent(history []llm.Message, reading TurnReading) *LlmAgent {
	return NewLlmAgent(LlmAgentConfig{
		Client: e.client, LLM: e.cfg, Registry: tools.NewRegistry(), SessionID: "turn-recall-eval",
		UserTurns: history, Classifier: e.classifier, TurnReading: reading,
	})
}

// evalMemory is one trial's identity memory: a fresh database, dropped at the end.
func evalMemory(t *testing.T) *arcadedb.Client {
	t.Helper()
	base, password := requireEvalEnv(t, "ARCADEDB_URL"), os.Getenv("ARCADEDB_PASSWORD")
	user := os.Getenv("ARCADEDB_USER")
	if user == "" {
		user = "root"
	}
	admin, err := arcadedb.New(arcadedb.Config{BaseURL: base, Database: "unused", User: user, Password: password})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	database := fmt.Sprintf("aura_turn_recall_eval_%d", time.Now().UnixNano())
	if _, err := admin.CreateDatabase(context.Background(), database); err != nil {
		t.Fatalf("create %s: %v", database, err)
	}
	t.Cleanup(func() {
		if _, err := admin.DropDatabase(context.Background(), database); err != nil {
			t.Errorf("drop %s: %v", database, err)
		}
	})
	client, err := arcadedb.New(arcadedb.Config{BaseURL: base, Database: database, User: user, Password: password})
	if err != nil {
		t.Fatalf("memory client: %v", err)
	}
	client = client.WithEmbedder(arcadedb.NewMemoryEmbedder(config.LoadEmbed(), func() string { return "" }))
	if err := client.EnsureMemorySchema(context.Background()); err != nil {
		t.Fatalf("memory schema: %v", err)
	}
	return client
}

type evalRecaller struct {
	client   *arcadedb.Client
	identity string
}

func (r evalRecaller) RecallTurns(ctx context.Context, request TurnRecallRequest) (TurnRecall, error) {
	recall, err := r.client.RecallTurns(ctx, arcadedb.TurnRecallRequest{
		IdentityID: r.identity, Text: request.Text, ContextKey: request.ContextKey, RouteKey: request.RouteKey,
		PolicyVersion: request.PolicyVersion, SourceRef: request.SourceRef,
		DeferredTools: request.DeferredTools, IncludeLabels: request.IncludeLabels,
	})
	if err != nil {
		return TurnRecall{}, err
	}
	convert := func(turns []arcadedb.RecalledTurn) []RecalledTurn {
		out := make([]RecalledTurn, len(turns))
		for index, turn := range turns {
			out[index] = RecalledTurn(turn)
		}
		return out
	}
	return TurnRecall{UserLabels: convert(recall.UserLabels), TeacherLabels: convert(recall.TeacherLabels), ToolTurns: convert(recall.ToolTurns)}, nil
}

// learn projects the turn the way the runner persists it: the user row with its decision,
// then its answer.
func learn(t *testing.T, memory *arcadedb.Client, identity, convID string, seq int, text, key string, d TurnDecision) {
	t.Helper()
	turn := func(seq int, role, content string, decision arcadedb.TurnDecision) arcadedb.ConversationTurnProjection {
		sum := sha256.Sum256([]byte(content))
		return arcadedb.ConversationTurnProjection{
			IdentityID: identity, ConversationID: convID, Seq: seq, Role: role, Content: content,
			ContentHash: hex.EncodeToString(sum[:]), OccurredAt: time.Now().UTC(),
			SourceRef: "postgres://aura/conversations/" + convID + "/turns/" + strconv.Itoa(seq), Decision: decision,
		}
	}
	projection := arcadedb.ConversationProjection{IdentityID: identity, ConversationID: convID, Turns: []arcadedb.ConversationTurnProjection{
		turn(seq, "user", text, arcadedb.TurnDecision{
			ContextKey: key, Effort: string(d.Effort), EffortRequested: string(d.EffortRequested), EffortSource: d.EffortSource,
			RouteKey: d.RouteKey, PolicyVersion: d.PolicyVersion, OriginRef: d.OriginRef,
		}),
		turn(seq+1, "assistant", "(answered)", arcadedb.TurnDecision{}),
	}}
	if err := memory.ApplyConversationProjection(context.Background(), projection); err != nil {
		t.Fatalf("project %s seq %d: %v", convID, seq, err)
	}
}

func replayTrial(t *testing.T, env evalEnv, set evalSet, split string, trial int) []evalRecord {
	t.Helper()
	ctx := context.Background()
	memory := evalMemory(t)
	identity := uuid.NewString()
	var records []evalRecord
	for _, conv := range set.Conversations {
		if split != "all" && conv.Split != split {
			continue
		}
		convID := uuid.NewString()
		var prior []llm.Message
		for index, turn := range conv.Turns {
			seq := 2*index + 1
			key, standalone := "", false
			if !turn.Attachment {
				key, standalone = TurnContextKey(prior, ""), len(prior) == 0
			}
			sourceRef := "postgres://aura/conversations/" + convID + "/turns/" + strconv.Itoa(seq)
			history := append(append([]llm.Message(nil), prior...), llm.Message{Role: llm.RoleUser, Content: turn.Text})

			baseline, baseRead := env.agent(history, TurnReading{ContextKey: key, SourceRef: sourceRef, Standalone: standalone}).readTurn(ctx)
			started := time.Now()
			decision, read := env.agent(history, TurnReading{
				Recaller: evalRecaller{client: memory, identity: identity}, ContextKey: key, SourceRef: sourceRef, Standalone: standalone,
			}).readTurn(ctx)
			reading := time.Since(started)

			record := evalRecord{
				trial: trial, split: conv.Split, turn: turn,
				baseline: evalArm{effort: baseline.EffortRequested, source: baseline.EffortSource},
				recall:   evalArm{effort: decision.EffortRequested, source: decision.EffortSource},
				teacher:  read.teacher, labelDistance: read.label.Distance,
				recallDuration: read.recallDuration, reading: reading,
			}
			if baseRead.seedOK {
				record.seeds = evalArm{effort: baseRead.seedTier.Effort(), source: EffortSourceSeeds}
			} else if baseline.EffortSource == EffortSourceGreeting {
				record.seeds = evalArm{effort: baseline.EffortRequested, source: EffortSourceGreeting}
			}
			records = append(records, record)
			learn(t, memory, identity, convID, seq, turn.Text, key, decision)
			prior = append(history, llm.Message{Role: llm.RoleAssistant, Content: "(answered)"})
		}
	}
	return records
}

func TestTurnRecallFrozenEval(t *testing.T) {
	env := newEvalEnv(t)
	set, _ := loadFrozenEvalSet(t)
	split := os.Getenv("TURN_EVAL_SPLIT")
	if split == "" {
		split = "calibration"
	}
	if split != "calibration" && split != "final" && split != "all" {
		t.Fatalf("TURN_EVAL_SPLIT = %q, want calibration, final or all", split)
	}
	trials := 3
	if raw := os.Getenv("TURN_EVAL_TRIALS"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			t.Fatalf("TURN_EVAL_TRIALS = %q", raw)
		}
		trials = parsed
	}
	var records []evalRecord
	for trial := 1; trial <= trials; trial++ {
		records = append(records, replayTrial(t, env, set, split, trial)...)
	}
	report := renderEvalReport(env.cfg.Model, split, trials, records)
	if path := os.Getenv("TURN_EVAL_REPORT"); path != "" {
		if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
			t.Fatalf("write the report: %v", err)
		}
	}
	t.Log("\n" + report)
	for _, violation := range memoryHardToNone(records) {
		t.Errorf("memory added a hard→none: trial %d, %s %q", violation.trial, violation.turn.ID, violation.turn.Text)
	}
}

// memoryHardToNone is the release criterion: a final hard turn the recall arm decided from
// memory as none while the memoryless arm did not.
func memoryHardToNone(records []evalRecord) []evalRecord {
	var out []evalRecord
	for _, record := range records {
		if record.split == "final" && record.turn.hard() && record.recall.source == EffortSourceMemory &&
			record.recall.effort == llm.ReasoningEffortNone && record.baseline.effort != llm.ReasoningEffortNone {
			out = append(out, record)
		}
	}
	return out
}

func renderEvalReport(model, split string, trials int, records []evalRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Turn recall frozen evaluation — %s, split %s, %d trial(s)\n\n", model, split, trials)
	fmt.Fprintf(&b, "Generated %s by TestTurnRecallFrozenEval.\n\n", time.Now().UTC().Format(time.RFC3339))
	arms := []struct {
		name string
		pick func(evalRecord) evalArm
	}{
		{"seeds", func(r evalRecord) evalArm { return r.seeds }},
		{"seeds + teacher", func(r evalRecord) evalArm { return r.baseline }},
		{"recall", func(r evalRecord) evalArm { return r.recall }},
	}
	for _, part := range []string{"calibration", "final"} {
		var subset []evalRecord
		for _, record := range records {
			if record.split == part {
				subset = append(subset, record)
			}
		}
		if len(subset) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s (%d readings)\n\n| Arm | Accuracy | 95%% Wilson | Hard→none |\n|---|---|---|---|\n", part, len(subset))
		for _, arm := range arms {
			correct, hardNone := 0, 0
			for _, record := range subset {
				decided := arm.pick(record)
				if record.turn.accepts(decided.effort) {
					correct++
				}
				if record.turn.hard() && decided.effort == llm.ReasoningEffortNone {
					hardNone++
				}
			}
			lo, hi := wilson(correct, len(subset))
			fmt.Fprintf(&b, "| %s | %d/%d | %.3f–%.3f | %d |\n", arm.name, correct, len(subset), lo, hi, hardNone)
		}
		b.WriteString("\n" + evalRecallDetail(subset))
	}
	return b.String()
}

func evalRecallDetail(records []evalRecord) string {
	sources, outcomes, table := map[string]int{}, map[string]int{}, map[string]int{}
	memoryHits, memoryCorrect := 0, 0
	var recallTimes, readingTimes []time.Duration
	for _, record := range records {
		sources[record.recall.source]++
		if record.teacher != "" {
			outcomes[string(record.teacher)]++
		}
		table[string(record.recall.effort)+" × "+strings.Join(record.turn.Accepted, "|")]++
		if record.recall.source == EffortSourceMemory {
			memoryHits++
			if record.turn.accepts(record.recall.effort) {
				memoryCorrect++
			}
		}
		recallTimes = append(recallTimes, record.recallDuration)
		readingTimes = append(readingTimes, record.reading)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Recall arm sources: %v. Teacher share: %d/%d. Teacher outcomes: %v.\n\n",
		sources, sources[EffortSourceTeacher], len(records), outcomes)
	fmt.Fprintf(&b, "Memory precision: %d/%d. Recall latency p50 %v, p95 %v. Whole reading p50 %v, p95 %v.\n\n",
		memoryCorrect, memoryHits, percentile(recallTimes, 0.5), percentile(recallTimes, 0.95),
		percentile(readingTimes, 0.5), percentile(readingTimes, 0.95))
	b.WriteString("| Decided × accepted | Readings |\n|---|---|\n")
	keys := make([]string, 0, len(table))
	for key := range table {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		fmt.Fprintf(&b, "| %s | %d |\n", key, table[key])
	}
	b.WriteString("\n| Trial | Turn | Seeds | Seeds + teacher | Recall (source) | Label distance |\n|---|---|---|---|---|---|\n")
	for _, record := range records {
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s (%s) | %.3f |\n", record.trial, record.turn.ID,
			record.seeds.effort, record.baseline.effort, record.recall.effort, record.recall.source, record.labelDistance)
	}
	return b.String()
}
```

Run: `W 'time go vet -tags turn_recall_eval ./internal/agent/ && time go test -race -count=1 -run "TestFrozenEval|TestEvalStatistics" ./internal/agent/'`
Expected: vet clean, PASS. Do not run `TestTurnRecallFrozenEval` here: it is paid.

- [ ] **Step 5: Commit**

```bash
W 'git add internal/agent/testdata/turn_recall_frozen_2026-10-07.json docs/verification/turn-recall-frozen-eval.md internal/agent/turn_recall_eval_set_test.go internal/agent/turn_recall_eval_test.go && LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks commit -q -F <wsl path of the message file> -- internal/agent/testdata/turn_recall_frozen_2026-10-07.json docs/verification/turn-recall-frozen-eval.md internal/agent/turn_recall_eval_set_test.go internal/agent/turn_recall_eval_test.go'
```

Message subject: `test(agent): freeze the turn recall evaluation set and its paid replay`.

- [ ] **Step 6: Run the calibration split (paid, operator OK first)**

Ask the operator in this session before running; name the model and that it runs three trials of 22 turns with up to two teacher calls each. With the OK, run the protocol's command with `TURN_EVAL_SPLIT=calibration`, read the report, and record its row under the protocol's Results.
Expected: the report exists and every arm has 66 readings. A recall arm with no memory decision at all means `recallRadius` separates the paraphrases: report the measured label distances to the operator before touching the constant (a change re-runs calibration and goes to the PRD amendment in Task 8).

- [ ] **Step 7: Run the final split once (paid, operator OK first)**

With the operator's OK, run with `TURN_EVAL_SPLIT=final`.
Expected: `TestTurnRecallFrozenEval` passes (no memory-added hard→none in any trial); record the row. A failure blocks the release: report the named turns, do not edit the set.

Commit the report(s) and the Results rows: subject `docs(verification): record the turn recall frozen evaluation`.

---
### Task 8: Gates, rollout, end-to-end on the lab VM, PRD amendment

**Files:**
- Modify: `scripts/critical_mutation_gate.py`, `scripts/critical_mutation_gate_test.py`, `.github/workflows/ci.yml` (mutation scope `turn_reading`)
- Modify: `scripts/coverage_package_policy.json` only if the coverage gate reports a denominator change it requires to be acknowledged
- Modify: `prd.md` (§6 adaptive reasoning, §10 memory: the amendment, written after the measurements)
- Create: `docs/verification/turn-recall-vm-e2e-<date>.md` (the VM run's evidence)

**Interfaces:**
- Consumes: everything above, deployed through the updater.
- Produces: the measured record the PRD amendment cites.

Nothing in this task changes production code. A defect found here goes back to the task that owns the code, with its own failing test first.

- [ ] **Step 1: Give the decision path a mutation scope**

`internal/agent/llm_agent_turn_reading.go` decides every turn's effort and preload; it is held to the 70% killed boundary like the files in `GO_SCOPES`, following the precedent of `12dbb8d15` (scopes `pausable` and `elicitation_route`). go-mutesting runs a bare `go test` of the package once per mutant, so first measure that unit once:

Run: `W 'time go test -count=1 ./internal/agent/ >/dev/null'`
Expected: the wall time is printed. If it exceeds 30 s, stop and report it to the operator with the estimate (time × mutants, about one mutant per decision branch) before adding the scope; a scope that cannot finish inside a CI job is worse than none.

Then, in `scripts/critical_mutation_gate.py`, add to `GO_SCOPES`:

```python
    "turn_reading": "internal/agent/llm_agent_turn_reading.go",
```

and `"turn_reading",` to `REQUIRED_SCOPE_IDS`. In `scripts/critical_mutation_gate_test.py`, after `test_elicitation_boundaries_are_scoped`:

```python
    def test_turn_reading_is_scoped(self) -> None:
        # The turn reading decides every turn's effort and the tools it preloads.
        self.assertEqual(
            critical_mutation_gate.GO_SCOPES["turn_reading"],
            "internal/agent/llm_agent_turn_reading.go",
        )
```

In `.github/workflows/ci.yml`, add a group to the Go mutation matrix after `elicitation`:

```yaml
          - group: turn-reading
            scopes: turn_reading
            timeout: 60
```

The 60 is a ceiling for the first, unmeasured run; the job comment above the matrix requires each timeout to be at least 2.7 times its group's cold total, so Step 5 replaces it with the measured value.

Run: `W 'time python3 -m unittest scripts/critical_mutation_gate_test.py scripts/mutation_workflow_test.py'`
Expected: PASS (`mutation_workflow_test` checks the matrix scopes equal `GO_SCOPES`).

Commit: `ci(mutation): score the turn reading` with the measured suite time in the body, pathspecs `scripts/critical_mutation_gate.py scripts/critical_mutation_gate_test.py .github/workflows/ci.yml`.

- [ ] **Step 2: Measure coverage on the disposable stack**

Run: `W 'time bash scripts/coverage_docker.sh'`
Expected: `ok: owned coverage NN.N% >= 85%` and no package-policy failure. `internal/agent`, `internal/runner`, `internal/conversations` and `internal/llm` are target packages (85% each); `internal/arcadedb` is delegated to the live `arcadedb_integration` profile, which CI's Agent Memory job measures. A target package below 85% gets tests in the task that owns its code, never a policy edit. If Docker is not reachable from WSL, the CI `knowledge-integration-test` job is the authority: say so and read its `coverage-report.json` in Step 4.

- [ ] **Step 3: Push**

Run: `W 'LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks push origin master'`
Expected: the lefthook banner and every pre-push gate green in the output (no banner means no gate ran: stop). Record the pushed SHA.

- [ ] **Step 4: CI green on that SHA**

Run: `gh run list --commit <full SHA> --json name,status,conclusion,databaseId` until the `CI` run is `completed`; then `gh run view <id> --json jobs --jq '.jobs[] | [.name,.conclusion] | @tsv'`.
Expected: every job `success`, including the coverage gate, the Agent Memory (`arcadedb_integration`) job, `db_integration`, the mutation groups with `turn-reading`, and `Publish Aura edge image`. Print the SHA next to the verdict. Do not push anything else to master while it runs: a push cancels it. A red job is fixed at its cause, whatever its origin.

- [ ] **Step 5: Set the measured mutation timeout**

Read the `turn-reading` group's duration from the run (`gh run view <id> --json jobs`) and its score from the mutation gate's report. Set `timeout:` to `ceil(2.7 × measured minutes)` in `ci.yml`.
Expected: score ≥ 70% killed. Survivors are read one by one (mutation autopsy) and killed by a test in `llm_agent_turn_reading_test.go` that names the behaviour, never by a test written against the mutant's text. Commit `ci(mutation): time the turn reading group` (and any test), push with the Step 3 recipe, and repeat Step 4.

- [ ] **Step 6: Wait for the updater on the lab VM**

The VM converges through its updater only (no manual pull, copy or compose edit). The updater applies the new image after about 15 minutes without chat activity.
Run, read-only, through the WSL ssh script recipe of the lab-VM reference: `sudo docker inspect aura --format '{{index .Config.Labels "org.opencontainers.image.revision"}} {{.State.StartedAt}}'` and `sudo docker ps --format '{{.Names}} {{.Status}}'`.
Expected: the revision equals the pushed SHA, and every container started after the aura flip (the updater restarts caddy, ingest and cloudflared about 2 minutes after aura). The aura boot log shows no ArcadeDB version refusal (plan 1's 26.10.1 floor).

- [ ] **Step 7: Prepare the VM run (no paid call yet)**

Write `docs/verification/turn-recall-vm-e2e-<date>.md` with the run's frame before any turn: the image SHA, the primary model from `GET /api/composer/reasoning-capabilities`, and the candidate list below, in this order. The candidates are declared before measuring so the one that is used cannot be chosen after the fact.

Turn A candidates (each needs a deferred tool, and each names the chat as destination so no `ask_user` pause interrupts it):
1. `che temperatura ci sarà sabato mattina a Bra? dimmelo qui in chat`
2. `cerca quanto costa questa settimana l'olio extravergine al litro e scrivimelo qui`
3. `trovami gli orari del museo egizio di domenica e dimmeli qui in chat`

Paraphrase B for each candidate, in the same order:
1. `sabato mattina a Bra che temperature sono previste? rispondi qui`
2. `quanto viene al litro l'olio extravergine in questi giorni? scrivilo qui`
3. `domenica il museo egizio a che ora apre e chiude? dimmelo qui`

The warm/cold workload of Step 9 uses five fresh families, never one of the candidates above (those are warm after Step 8). Each is a cold turn and its warm paraphrase, each in a new conversation. Its expected outcome, declared here: a web tool runs and the answer gives the asked-for item instead of a refusal or a question back.
- W1: `che film danno stasera al cinema di Cuneo? dimmelo qui in chat` / `stasera al cinema a Cuneo cosa c'è in programmazione? rispondi qui`
- W2: `cerca le ultime notizie sulla fiera del tartufo di Alba e riassumile qui` / `cosa si dice in questi giorni della fiera del tartufo di Alba? riassumilo qui`
- W3: `a che ora apre domani l'ufficio postale di Borgo San Dalmazzo? dimmelo qui` / `domani l'ufficio postale di Borgo San Dalmazzo che orari fa? scrivilo qui`
- W4: `quanto costa un abbonamento mensile alla palestra comunale di Cuneo? cercalo e dimmelo qui` / `la palestra comunale di Cuneo quanto chiede al mese? cerca e rispondi qui`
- W5: `che temperatura c'è adesso sul Monviso? dimmelo qui in chat` / `in questo momento sul Monviso quanti gradi ci sono? rispondi qui`

Ask the operator for the OK to run the paid turns (up to 12 in Step 8 and 10 in Step 9 on the primary model), naming the model.

- [ ] **Step 8: Run the end-to-end checks (paid, after the OK)**

Drive every turn through the cockpit API with the operator's own account (`D:\Aura\.env.google`, read by `scripts/musr_live_run_authula_helpers.sh` `fetch_auth_config` / `sign_in`, never printed): `POST /api/conversations`, then `POST /agent/run` over HTTPS reading the AG-UI stream to `RUN_FINISHED`. Read the evidence read-only: the aura log line `adaptive reasoning: turn read` for the turn (source, requested, effort, seed_margin, teacher, label_origin, label_distance, tool_turn_origin, preloaded, recall_miss, recall_ms), `aura.tool_invocations` for the turn's tool calls, and the identity's ArcadeDB database over the container IP for the `ConversationTurn` row.

1. **Turn A learns.** Send candidate 1 in a new conversation. Expected log: `source=teacher` (the seed margin under 0.075), `recall_miss=no_compatible_label` (memory was asked and had nothing), `preloaded=[]`; the tool ledger shows `tool_search` then the deferred tool. If the source is `seeds` (a confident seed verdict) the candidate does not exercise a label: record it and move to candidate 2, then 3. If none of the three reaches the teacher, stop and report: the run then proves only the tool preload, which Steps 2 and 3 still measure.
2. **The label reaches memory.** Poll the `ConversationTurn` row of A's user turn (`source_ref = postgres://aura/conversations/<A>/turns/<seq>`) every 30 s until `effort_source = 'teacher'` and the trace of A's answer carries the tool (`INVOKED` with `status='succeeded'`). Record the lag from A's `RUN_FINISHED`: it is the periodic reconciliation, not the post-commit offer (plan Decisions). Expected: both present within the reconciler's period; if not after 3 periods, stop and report.
3. **Paraphrase B reuses it.** Send B's paraphrase in a new conversation. Expected log: `source=memory`, `label_origin` = A's user-turn ref, `label_distance` ≤ 0.10, `preloaded` contains A's tool, and the tool ledger of B has the tool and no `tool_search` before it.
4. **Deleting A forgets it.** Delete conversation A through the cockpit (`DELETE` on the conversation), wait for the delete reconciliation to remove its projection (poll the A row until it is gone), then send the paraphrase again in a new conversation C. Expected log: `label_origin` is not A's ref (B's memory-sourced decision is not a label either, so C decides from seeds or the teacher), and `tool_turn_origin` is not A's.
5. **A different context does not reuse it.** In a new conversation, send `scrivimi una poesia di quattro righe sull'autunno` first, then B's paraphrase as the second turn. Expected log: `source` is not `memory` and `preloaded=[]`: both pools filter on the context key before top-k, and no earlier turn shares this one's.
6. **Route change and outages (operator-driven only).** Ask the operator whether to switch the primary model in the cockpit and repeat B (expected: no `memory` source, because the route key differs), and whether to stop the embedding sidecar for one turn (expected: the turn answers, one `turn recall failed; reading the turn without memory` warning, `recall_miss=recall_error`, `recall_ms` ≤ 600) and restart it. Do neither without that answer.

Write every turn's log line, ledger rows and ArcadeDB row (secrets elided) into the evidence file. Clean up: delete the test conversations except the ones the operator wants kept.

- [ ] **Step 9: Measure what memory saves (paid, after the OK)**

For W1-W5 (Step 7): send the cold turn, wait the projection lag measured in Step 8.2, then send its warm paraphrase. Record per turn, from the AG-UI stream, the log line, `aura.tool_invocations` and `aura.conversation_turns`: the decision source, the preloaded tools and which of them were actually called, every tool call (and whether `tool_search` was among them), time to first token and to completion, input/output/cached tokens and cost (unknown stays unknown, never zero), and whether the declared outcome was met.
Expected (the spec's workload gate): no warm turn misses an outcome its cold pair met, and neither the slowest warm completion nor the known total warm cost exceeds the cold one (with five pairs the slowest stands in for p95; the evidence file says so). Report p50 and the slowest for each side and the per-pair deltas; fewer `tool_search` calls alone are not a pass. A regression is reported to the operator with the pair, not explained away.

- [ ] **Step 10: Amend the PRD with what was measured**

In `prd.md` §6 (adaptive reasoning), record: the decision table as shipped (`composer > standalone greeting > compatible label (user, teacher) > seeds ≥ 0.075 > teacher > seeds | static low`), the frozen evaluation's calibration and final rows with their report paths, the VM run's results (Step 8 checks 1-6, which ran and which the operator declined), the projection lag, and the warm/cold deltas. In §10 (memory), record the seven `ConversationTurn` routing properties, the three recall pools filtered before top-k, and that only `user` and `teacher` decisions are reusable labels. Each entry cites its date and evidence file, and states what it does not prove: the frozen set's size (51 turns), five warm/cold pairs, one identity on one VM, and the out-of-scope gaps (tools of paused and failed runs never reach the graph; the parked set-read `LIMIT` truncation).

Commit `docs(prd): record the turn recall measurements` with `prd.md` and `docs/verification/turn-recall-vm-e2e-<date>.md`, push with the Step 3 recipe, and confirm CI green on that SHA (Step 4). Update the memory file `project_turn_recall_plans.md`: plan 2 shipped, with its SHAs; plan 3 next.

---

### Task 9 (addendum 2026-10-07): The teacher labels in the background

Added after two things happened. The first VM pass measured no teacher answer within the 2 s synchronous bound (0 of 6), and the operator then chose a background teacher that is provider-agnostic. The binding text is the spec's "Amendment 2026-10-07". This task lands after the final-review fix round, and it builds on that round's code, not on the snapshots in Tasks 4–6. That round put the typed text in `TurnReading`, counted the teacher's silent deadline as `timeout`, logged origins verbatim, and made runs with no dispatched turn decide from seeds.

**The pattern is the auto-title worker; do not invent another one.** Mirror each of these:
- `Runner.maybeAutoTitle` / `persistAutoTitle` (`internal/runner/runner_resume.go`);
- `conversations.GenerateTitle` (`internal/conversations/title.go`);
- `Conversations.SetTitleIfNull`;
- `Deps.TitleTimeout` / `defaultTitleTimeout` (`internal/runner/runner_deps.go`);
- the `r.wg` join in `Stop`.

**Files:**
- Modify the agent files. The decision gains `AskTeacher`, the synchronous teacher leaves `readTurn`, and the teacher becomes an exported standalone function:
  - `internal/agent/llm_agent_turn_reading.go`
  - `internal/agent/llm_agent_reasoning.go`
  - `internal/agent/turn_recall.go`
- Modify the conversations layer (`RecordTeacherLabel`):
  - the queries file that holds `RecordConversationTurnDecision` under `internal/db/queries/`, plus regenerated sqlc;
  - `internal/conversations/store_turn_decision.go`.
- Modify the runner:
  - `internal/runner/runner_turn_recall.go`. Add a new `runner_turn_teacher.go` if it would pass 600 lines.
  - `internal/runner/runner_deps.go`: `Deps.TeacherTimeout` and `defaultTeacherTimeout = 30 * time.Second`.
  - every `TurnDecisionStore` fake.
- Modify the eval (arms and procedure only; the dataset and its digest do not change):
  - `internal/agent/turn_recall_eval_test.go` and its report file;
  - `docs/verification/turn-recall-frozen-eval.md`.
- Tests:
  - `internal/agent/llm_agent_turn_reading_test.go`
  - `internal/agent/llm_agent_teacher_test.go`
  - the `RecordTurnDecision` store tests (db_integration, recipe P)
  - `internal/runner/runner_turn_recall_test.go`

**Interfaces (produced):**
- `agent.TurnDecision.AskTeacher bool`.
- `func agent.AskTeacher(ctx context.Context, client llm.Client, model, user string) (prompt.ReasoningTier, string)`. It returns the tier and one outcome (`success`, `timeout`, `invalid`, `error`, `canceled`), and counts the attempt.
- `func (s *conversations.Store) RecordTeacherLabel(ctx context.Context, conversationID string, seq int, requested string) error`.
- `runner.TurnDecisionStore` gains `RecordTeacherLabel`.
- `runner.Deps.TeacherTimeout time.Duration`.

- [ ] **Step 1: The decision asks instead of waiting (agent)**

`readTurn` no longer calls the teacher. In `decideAdaptive`, every path that called `a.teach` now decides from what it already has, and sets `AskTeacher`:
- **Classifier verdict below `teacherMargin`:** the seed tier's effort, source `seeds`, `AskTeacher = true`.
- **No classifier configured:** static `low`, source `fallback`, `AskTeacher = true`.
- **Everything else leaves `AskTeacher = false`:** the raw embed failing (`Classify` not ok), an empty message, a label reuse, a confident verdict, a greeting, a composer effort, and a non-adaptive route.

`AskTeacher` is set only when `a.turnReading.SourceRef != ""`, which is when there is a row to label.

Remove what becomes dead: `a.teach`, the teacher fields and durations of `turnRead`, and the teacher keys of the "turn read" log line. That line gains `ask_teacher`.

Move the request body of `askTeacher` into the exported `AskTeacher(ctx, client, model, user)`. It keeps the router prompt, the request, the trace records, the stream drain, the `routeCtx.Err()` timeout rule and the attempt counter. It takes its bound from the caller's context, so drop `reasoningRouterTimeout` if nothing else uses it.

Write these tests first and show them failing:
- a sub-margin verdict decides `seeds` with `AskTeacher` true, and makes no LLM call;
- a confident verdict, a label reuse, a greeting and a composer effort each leave it false;
- no classifier gives `fallback` plus `AskTeacher`;
- an empty `SourceRef` never sets it;
- `AskTeacher` returns each outcome against the existing fake clients. Keep the `TestTeacher…` cases, now calling the function.

- [ ] **Step 2: The conditional label write (conversations)**

Add the query next to `RecordConversationTurnDecision`:

```sql
-- name: RecordConversationTurnTeacherLabel :execrows
UPDATE aura.conversation_turns
SET reasoning_effort_requested = sqlc.arg(reasoning_effort_requested),
    reasoning_effort_source = 'teacher'
WHERE conversation_id = sqlc.arg(conversation_id)
  AND seq = sqlc.arg(seq)
  AND role = 'user'
  AND reasoning_effort_source IN ('seeds', 'fallback');
```

Regenerate sqlc the way the repo does; read the Makefile for the target.

`RecordTeacherLabel` behaves like this:
- It validates like `RecordTurnDecision`: a UUID, seq > 0, and a non-empty requested effort.
- It runs in `db.WithCallerIdentityTx`.
- It returns nil when no row matched. Like `SetTitleIfNull`, a row that is already labelled, decided by the user, or deleted is not an error.

Add a db_integration test on recipe P:
- a `seeds` row is upgraded and keeps its `reasoning_effort`;
- a `user`, `memory` or NULL-source row is left untouched;
- a second call is a no-op.

- [ ] **Step 3: The runner worker (runner)**

In `recordTurnDecision`, once a decision whose `AskTeacher` is true has been written successfully, call `r.maybeTeachTurn(ctx, tr)`. It works in this order:
1. Return early if the breaker is open (`r.breaker != nil && r.breaker.Allow() != nil`) or the tracker has no typed text.
2. Take `runtime := r.trackerLLMSnapshot(tr)`, which is the turn's own client and route.
3. Start the worker with `r.wg.Go`:
   ```go
   r.wg.Go(func() {
       ctx := context.WithoutCancel(turnCtx)
       ctx, cancel := context.WithTimeout(ctx, r.teacherTimeout)
       defer cancel()
       tier, outcome := agent.AskTeacher(ctx, runtime.Client, runtime.Config.Model, text)
       // …
   })
   ```
4. On `success`, call `r.turnDecisions.RecordTeacherLabel(ctx, tr.convID, tr.userTurnSeq, string(tier.Effort()))` under a short `WithoutCancel` persistence timeout, as `persistAutoTitle` does: the label stores the tier's effort before the clamp, as migration 0137 defines `reasoning_effort_requested`, and a reuse clamps it again. A failed write logs one warning. (Amended 2026-10-07: an earlier revision clamped here.)
5. Log once:
   ```go
   slog.Info("adaptive reasoning: teacher label", "thread_id", …, "source_ref", reasoningSourceRef(tr.convID, tr.userTurnSeq), "outcome", outcome, "tier", tier, "effort", effort, "teacher_ms", …)
   ```
   The source ref is logged verbatim, as in the turn-read line.

`Deps.TeacherTimeout` defaults to `defaultTeacherTimeout` (30 s), exactly as `TitleTimeout` does. The tracker keeps the typed text from the reading (the final-fix `Text`).

Write these tests first and show them failing:
- a `seeds` decision with `AskTeacher` starts one worker, which records the unclamped label after the decision write. `Stop` joins it, and goleak stays clean;
- a failed round, an `AskTeacher=false` decision, an open breaker and a failed decision write start no worker;
- a teacher timeout or an invalid answer records nothing;
- cancelling a finished turn's context does not cancel the worker.

- [ ] **Step 4: The frozen replay follows the background teacher**

In `turn_recall_eval_test.go`, when `readTurn` returns a decision with `AskTeacher`, call `agent.AskTeacher` with the eval's client, bounded by the same 30 s default.
- A successful answer becomes the label that `learn` writes before the next turn is read: source `teacher`, requested = the tier effort before the clamp.
- A failure writes the seeds/fallback decision unchanged, as on a real row.

Keep the three arms' names and the gate. The seeds+teacher arm now measures two things: the current turn's seeds decision, and how often the background teacher would have labelled. Say so in the report and in `docs/verification/turn-recall-frozen-eval.md`, and record each reading's teacher outcome in the report.

Run the untagged eval tests and `go vet -tags turn_recall_eval ./internal/agent/`. Do not run the paid test.

- [ ] **Step 5: Verify and commit**

1. Run the changed tests.
2. Run `W 'time go test -race -count=1 ./internal/agent/ ./internal/agent/prompt/ ./internal/runner/ ./internal/conversations/'`, and recipe P for the db_integration test.
3. Grep for the tests that pin `TurnDecisionStore` implementers, the turn-read log keys, the `turnRead` fields and the teacher metric, and run them.
4. Commit in atomic pieces: agent; conversations + sqlc; runner; eval.

Do not push. Pushing, CI and the VM rerun belong to the controller: Task 8 Steps 3–9, rerun with this task included.

---
