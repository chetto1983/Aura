---
phase: 37E-composer-model-reasoning-effort-selector-inserted
plan: 03
type: execute
wave: 2
depends_on: ["37E-01"]
files_modified:
  - internal/db/queries/conversations.sql
  - internal/conversations/store.go
  - internal/conversations/store_identity.go
  - internal/conversations/store_helpers.go
  - internal/conversations/store_reasoning_effort_test.go
  - internal/agui/types.go
autonomous: true
requirements: [WEBMODEL-01]
must_haves:
  truths:
    - "A conversation's chosen effort symbol writes to `aura.conversations.metadata` jsonb via an owner-scoped update (0 rows = not owned) — NO migration is added"
    - "Reopening a conversation surfaces the stored effort: `Conversation.ReasoningEffort` is populated from `metadata` (empty/absent → \"\", which hydrates as auto)"
    - "The agui `ConversationStore` interface exposes the effort update method"
    - "New conversations with no persisted effort default to `auto` (D-07): the adaptive policy runs unchanged — zero regression for users who never touch the selector"
  artifacts:
    - path: "internal/db/queries/conversations.sql"
      provides: "UpdateConversationReasoningEffortForIdentity :execrows (jsonb_set)"
      contains: "UpdateConversationReasoningEffortForIdentity"
    - path: "internal/conversations/store_identity.go"
      provides: "Store.UpdateReasoningEffortForIdentity (owner-scoped)"
      contains: "UpdateReasoningEffortForIdentity"
    - path: "internal/conversations/store.go"
      provides: "Conversation.ReasoningEffort projection field"
      contains: "ReasoningEffort"
  key_links:
    - from: "internal/conversations/store_helpers.go"
      to: "aura.conversations.metadata"
      via: "conversationFromRow parses reasoning_effort out of r.Metadata jsonb"
      pattern: "reasoning_effort"
    - from: "internal/agui/types.go"
      to: "internal/conversations/store.go"
      via: "ConversationStore interface widened with UpdateReasoningEffortForIdentity"
      pattern: "UpdateReasoningEffortForIdentity"
---

<objective>
Ship the per-conversation persistence for the effort symbol (D-06, Claude parity: persisted + restored on reopen) using the EXISTING `aura.conversations.metadata` jsonb column — NO migration (the column exists since `0005_conversations.up.sql`; migration numbering is unchanged). Add the owner-scoped write query + store method (mirroring `RenameForIdentity`), surface the value on the read projection (currently `conversationFromRow` DROPS metadata), and widen the agui `ConversationStore` interface so `handleRun` (plan 06) can call it.

Purpose: the write + read seam the frontend hydrates from and the run handler persists to. Independent of the wire engine → parallel with plan 02.
Output: `UpdateConversationReasoningEffortForIdentity` query, `Store.UpdateReasoningEffortForIdentity`, `Conversation.ReasoningEffort`, and a `db_integration` round-trip test.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-CONTEXT.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-RESEARCH.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-PATTERNS.md
@.claude/skills/golang-database/SKILL.md
@internal/conversations/store_identity.go
@internal/conversations/store_helpers.go
@internal/db/queries/conversations.sql
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add the owner-scoped jsonb_set update query + regenerate sqlc + store method</name>
  <files>internal/db/queries/conversations.sql, internal/conversations/store_identity.go</files>
  <read_first>
    - internal/db/queries/conversations.sql:93-98 (`RenameConversationForIdentity :execrows` — the owner-scoped mutation to mirror)
    - internal/conversations/store_identity.go:153-179 (`RenameForIdentity` — the `db.WithIdentityTx` + `:execrows` 0-rows-not-owned pattern)
    - internal/db/migrations/0005_conversations.up.sql (confirm the `metadata jsonb` column exists — DO NOT add a migration)
    - 37E-RESEARCH.md §Seam Map §5 + 37E-PATTERNS.md "Persistence" section (the exact jsonb_set query)
  </read_first>
  <action>
    Add to conversations.sql: `-- name: UpdateConversationReasoningEffortForIdentity :execrows` — `UPDATE aura.conversations SET metadata = jsonb_set(COALESCE(metadata, '{}'::jsonb), '{reasoning_effort}', to_jsonb(sqlc.arg(effort)::text), true) WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id);`. Regenerate the sqlc client (run the repo's sqlc generate step — `make sqlc` or the documented generate command; do NOT hand-edit generated code). Add `func (s *Store) UpdateReasoningEffortForIdentity(ctx context.Context, conversationID, identityID, effort string) (int64, error)` in store_identity.go mirroring `RenameForIdentity`: parse both UUIDs, run inside `db.WithIdentityTx(ctx, s.pool, identityID, ...)` calling the generated `UpdateConversationReasoningEffortForIdentity`, return the affected-rows count (0 = not owned). Confirm NO file under internal/db/migrations/ is added.
  </action>
  <acceptance_criteria>
    - conversations.sql contains `UpdateConversationReasoningEffortForIdentity` with `jsonb_set` and the `identity_id = sqlc.arg(identity_id)` predicate.
    - The generated sqlc method exists and `store_identity.go` defines `UpdateReasoningEffortForIdentity` using `db.WithIdentityTx`, returning `(int64, error)`.
    - `git status --porcelain internal/db/migrations/` is EMPTY (no migration added, D-06).
    - `go build ./...` green.
  </acceptance_criteria>
  <verify>
    <automated>go build ./... && grep -q "UpdateConversationReasoningEffortForIdentity" internal/db/queries/conversations.sql && [ -z "$(git status --porcelain internal/db/migrations/)" ] && echo OK</automated>
  </verify>
  <done>An owner-scoped, parameterized effort write exists with no schema migration.</done>
</task>

<task type="auto">
  <name>Task 2: Surface the stored effort on the read projection (conversationFromRow maps metadata)</name>
  <files>internal/conversations/store.go, internal/conversations/store_helpers.go, internal/agui/types.go</files>
  <read_first>
    - internal/conversations/store.go:96-110 (the `Conversation` struct + `Model` field — where `ReasoningEffort` is added)
    - internal/conversations/store_helpers.go:22-41 (`conversationFromRow` — currently DROPS `r.Metadata`)
    - internal/agui/types.go:29-59 (the `ConversationStore` interface — `RenameForIdentity` at :49)
    - 37E-RESEARCH.md §Seam Map §5 (read path) + 37E-PATTERNS.md "Persistence" section
  </read_first>
  <action>
    Add `ReasoningEffort string` to the `Conversation` struct (store.go), documented as the per-conversation effort symbol ("" = auto). In `conversationFromRow` (store_helpers.go), parse `r.Metadata` (jsonb bytes) defensively — if it unmarshals to an object with a `reasoning_effort` string key, set `Conversation.ReasoningEffort` to it; on nil/empty/invalid metadata leave it "". Do NOT change the read queries (they already SELECT `metadata`). Widen the agui `ConversationStore` interface (types.go) with `UpdateReasoningEffortForIdentity(ctx context.Context, conversationID, identityID, effort string) (int64, error)` next to `RenameForIdentity`. Keep store.go/store_helpers.go ≤600 LOC.
  </action>
  <acceptance_criteria>
    - `Conversation` has a `ReasoningEffort string` field; `conversationFromRow` populates it from metadata JSON key `reasoning_effort`.
    - Absent/nil/garbage metadata → `ReasoningEffort == ""` (never panics on malformed jsonb).
    - `internal/agui/types.go` `ConversationStore` declares `UpdateReasoningEffortForIdentity(...) (int64, error)`; `go build ./...` green (the concrete Store already satisfies it via Task 1).
  </acceptance_criteria>
  <verify>
    <automated>go build ./... && go vet ./internal/conversations/ ./internal/agui/ && grep -q "ReasoningEffort" internal/conversations/store.go && grep -q "UpdateReasoningEffortForIdentity" internal/agui/types.go</automated>
  </verify>
  <done>Opening a conversation carries its stored effort on the existing DTO; the interface is ready for the run handler.</done>
</task>

<task type="auto">
  <name>Task 3: db_integration round-trip test (write → read back; cross-identity deny)</name>
  <files>internal/conversations/store_reasoning_effort_test.go</files>
  <read_first>
    - Existing `internal/conversations/*_test.go` with `//go:build db_integration` (the identity-scoped store test setup — pool, seed identity, seed conversation)
    - CLAUDE.md coverage-gate rule (this integration test runs under the `db_integration` gate tier — it MUST actually execute, not skip-as-green under $CI)
    - 37E-RESEARCH.md §Validation "persistence round-trip" row + Threat "effort crossing user isolation"
  </read_first>
  <action>
    Create `store_reasoning_effort_test.go` with `//go:build db_integration`. `TestReasoningEffortRoundTrip`: seed an identity + a conversation; call `UpdateReasoningEffortForIdentity(ctx, convID, ownerID, "high")` → assert affected==1; `GetForIdentity` → assert `Conversation.ReasoningEffort == "high"`; update again to "auto" → read back "auto" (switch-back is remembered, OQ-3); update to "" → read back "". `TestReasoningEffortForeignIdentityDenied`: a DIFFERENT identity calls the update on the owner's conversation → affected==0 AND the owner's stored value is unchanged (owner-scope + RLS backstop, mirrors RenameForIdentity). Follow the existing db_integration setup helpers; use `envOrSkip` so it t.Fatal's under $CI when the DB env is unset (no skip-as-green).
  </action>
  <acceptance_criteria>
    - `go test -tags db_integration ./internal/conversations/ -run TestReasoningEffort` passes against a throwaway DB (never the live `aura` DB — use the coverage-DB harness).
    - Round-trip asserts high→read high, auto→read auto, ""→read "".
    - Cross-identity update returns 0 affected and leaves the owner's value intact.
    - Sub-second "PASS" with the DB env UNSET fails under $CI (skip-tell guard), proving it actually runs.
  </acceptance_criteria>
  <verify>
    <automated>go test -tags db_integration ./internal/conversations/ -run TestReasoningEffort -race -count=1</automated>
  </verify>
  <done>Persistence is proven end-to-end at the store layer, including owner isolation.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| identity A → conversation owned by identity B | A cross-identity write must not touch B's stored effort. |
| upstream metadata bytes → SPA | A persisted value later rendered by the client. |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37E-03-ISO | Information Disclosure / Tampering | effort write on a foreign thread | mitigate | `UpdateReasoningEffortForIdentity` predicates on `identity_id` inside `db.WithIdentityTx` (RLS backstop); 0 rows = not owned. Cross-identity deny test asserts affected==0 and unchanged value. |
| T-37E-03-XSS | Tampering / XSS | persisted `metadata.reasoning_effort` | mitigate | Written only via parameterized `jsonb_set` (no string concat); the value is a validated symbol upstream (plan 06); the read projection surfaces it as a controlled selector value, never raw HTML. `conversationFromRow` parses defensively (garbage → ""). |
| T-37E-03-MIG | Tampering (schema) | accidental migration | mitigate | Verify asserts `internal/db/migrations/` is unmodified (D-06 — jsonb reuse, no new migration). |

No new external network, no new package, no new auth route in this plan.
</threat_model>

<verification>
- `go build ./...` + `go test ./internal/conversations/ ./internal/agui/ -race` green (untagged).
- `go test -tags db_integration ./internal/conversations/ -run TestReasoningEffort` green against a throwaway DB.
- No migration file added.
</verification>

<success_criteria>
- Effort persists per-conversation to `metadata` jsonb (no migration) and restores on read; owner-scoped; interface ready for plan 06.
- WEBMODEL-01 persistence requirement satisfied at the store layer.
</success_criteria>

<output>
Create `.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-03-SUMMARY.md` when done.
</output>
