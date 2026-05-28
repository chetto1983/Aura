# Coding Conventions

**Analysis Date:** 2026-05-28

> Aura is mid tabula-rasa rewrite (tag `pre-rewrite-2026-05-27`). The actual Go skeleton is ~633 LOC. The PRIMARY source of conventions is `prd.md` (4401 lines) + `CLAUDE.md` (133 lines). What follows is prescriptive: it is how any new code MUST be written, not a survey of what already exists.

## Source-of-truth files

- `/home/user/Aura/CLAUDE.md` — Behavioral rules (must-read before every change)
- `/home/user/Aura/prd.md` — Full specification, 13 slice plan, env catalog, governance, test discipline
- `/home/user/Aura/internal/agent/loop.go` — Reference idiom (concise package doc, KV-cache discipline comment, terse identifiers)
- `/home/user/Aura/internal/agent/tools/spec.go` — Reference for tool interface + deferred-tool flag
- `/home/user/Aura/internal/agent/tools/manifest.go` — Reference for stable-ordered rendering (cache-stability rationale)

## Language & toolchain

- **Go 1.23** (`/home/user/Aura/go.mod`, module `github.com/chetto1983/aura`).
- Formatter: standard `gofmt` (no override).
- Linter: `go vet ./...` mandatory pre-commit. No staticcheck/golangci-lint config yet — add when introduced via PRD-amendment.
- Builder: `go build ./...` must be green before any commit.
- Tests: see `TESTING.md`.

## Naming patterns

### Files

- **snake_case Go files** with focused scope:
  - `loop.go`, `client.go`, `sandbox.go`, `swarm.go`, `spec.go`, `search.go`, `manifest.go`, `text_response.go`
- One concern per file. Tool implementations live in `internal/agent/tools/<name>.go` (PRD §Tool design).
- When a file would exceed 600 LOC, **split on touch** as `<name>_<concern>.go` (e.g. `llm_agent.go` + `llm_agent_pending.go`). CLAUDE.md §Behavioral rules: NO GOD CLASS.
- Test files: `<unit>_test.go` co-located with the code under test.
- SQL queries (sqlc): `internal/db/queries/<feature>.sql`, one logical group per file.
- Migrations: `internal/db/migrations/0001_<name>.up.sql` and `0001_<name>.down.sql` — zero-padded 4-digit sequence + snake_case name + `.up`/`.down` pair (mandatory).
- Cypher migrations: `internal/knowledge/migrations/0001_<name>.cypher` (audit tracked via Postgres `aura.knowledge_migrations`).

### Identifiers (Go idiomatic)

- Exported types/funcs: `CamelCase` (`Loop`, `NewLoop`, `Registry`, `ManifestEntry`, `ToolCall`, `Coordinator`).
- Unexported: `lowerCamelCase` (`textResponseArgs`, `toolSearchArgs`, `defaultMaxSteps`).
- Constants: `CamelCase` for exported package-level (`MaxSpawnDepth`, `RoleSystem`, `RoleUser`, `RoleAssistant`, `RoleTool`) — see `/home/user/Aura/internal/llm/client.go:14-19` and `/home/user/Aura/internal/swarm/swarm.go:29`.
- Interfaces named for behavior: `Runner`, `Client`, `Coordinator`, `Tool` (single-method or focused-method).
- Receiver names: short, consistent per type (`l *Loop`, `ts *ToolSearch`, `r *Registry`).
- Method ordering inside a struct: `New<X>` constructor first, then exported methods (verb-first when imperative), then unexported helpers.

### Env vars

- **Pattern:** `AURA_<DOMAIN>_<UNIT>` — uppercase, underscore separators, domain singular.
- Examples (from PRD §Caps & Limits indice):
  - `AURA_LLM_BASE_URL`, `AURA_LLM_API_KEY`, `AURA_LLM_MODEL`
  - `AURA_SWARM_MAX_DEPTH`, `AURA_SWARM_MODEL_CHAT`, `AURA_SWARM_MODEL_REASONING`, `AURA_SWARM_MODEL_WORKER`
  - `AURA_SANDBOX_URL`, `AURA_SANDBOX_TIMEOUT_SEC`, `AURA_SANDBOX_SESSION_TTL_SEC`, `AURA_SANDBOX_MAX_CONCURRENT_SESSIONS`, `AURA_SANDBOX_WORKSPACE_MAX_BYTES`, `AURA_SANDBOX_NETWORK_ALLOW_HOSTS`
  - `AURA_CONTEXT_PREVIEW_CAP_BYTES`, `AURA_CONVERSATION_TURN_CAP_BYTES`, `AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS`
  - `AURA_RUN_DIR`, `AURA_RUN_DIR_WARN_THRESHOLD_BYTES`, `AURA_CONFIG_DIR`
  - `AURA_DB_URL`
- **Exception (canonical upstream naming preserved):** third-party libs / sidecars keep upstream names:
  - `TELEGRAM_BOT_TOKEN`, `OPENROUTER_API_KEY`, `MULTIMODAL_*`, `LLAMA_*`, `LMCACHE_*`, `POSTGRES_PASSWORD`
- Every new env var MUST land in PRD §Caps & Limits indice with type (`cap` | `operative` | `path` | `secret`) and default. CLAUDE.md §Behavioral rules: NO ENV VAR HARD-CODED in business logic.

### Postgres schema

- All tables live under schema `aura.*` (never default `public.*`). Example: `aura.conversations`, `aura.paused_states`, `aura.scheduler_tasks`, `aura.skill_audit`, `aura.identities`, `aura.capability_grants`, `aura.knowledge_migrations`.
- Table names plural, snake_case.
- Column names snake_case (`tool_call_id`, `proxied_from_child_id`, `computed_risk_tier`, `gate_recommended`).
- Foreign keys: `<entity>_id` with explicit `REFERENCES aura.<table>(id) ON DELETE CASCADE` when ownership cascade is the intent.
- Indexes on (conversation_id, resumed_at) WHERE resumed_at IS NULL pattern for partial-index O(log n) scans.
- Append-only audit tables enforce immutability via Postgres trigger `BEFORE UPDATE/DELETE → RAISE EXCEPTION` (see `aura.skill_audit`).

### sqlc generated code

- Queries live in `internal/db/queries/<feature>.sql`, generated client lands in `internal/db/sqlc/`.
- Query names follow imperative Go method-name shape: `InsertPausedState`, `GetByToken`, `ListPendingForLoop`, `MarkResumed`, `MarkResumedBatch`, `CleanupResumedOlderThan`, `RecordSkillMutation`.
- `sqlc.yaml` config: engine `postgresql`, `emit_interface`, `emit_exact_table_names`, `json_tags` enabled.
- CI golden check: `sqlc generate` output must match committed code (fail if drift).

## Code style

### Comments policy (mandatory)

From `/home/user/Aura/CLAUDE.md`:

> **NO COMMENTS UNLESS WHY IS NON-OBVIOUS.** Identifier names already explain what. Comments only for hidden constraints, workarounds, or surprising behavior.

What this means in practice (see `/home/user/Aura/internal/agent/loop.go` for the reference idiom):

- **Package doc:** every package starts with a `// Package <name>` block that names the package's job in 1–3 short paragraphs. Mandatory.
- **Exported types/funcs:** one-line GoDoc when the name is not self-explanatory or when there is a contract caller MUST respect (e.g. "consumers MUST drain it (or the implementation will leak goroutines)" on `llm.Client.Stream`).
- **Inline comments:** allowed only for: hidden constraints (e.g. `// index 0 is the system message and MUST NOT change between turns` in `loop.go:23-24`), KV-cache invariants, race/concurrency rationale, workaround rationale referencing the memory/feedback that motivated it (e.g. `// See [[feedback_aura_cache_poisoning_sites_2026-05-27]]` in `manifest.go:21`).
- **Forbidden:** comments that restate the next line of code. `// loop over tools` above `for _, t := range tools` is rejected.

### Imports

- Standard library first, blank line, then third-party (sorted by full path).
- Module-internal imports use full path `github.com/chetto1983/aura/internal/<pkg>`.
- See `/home/user/Aura/internal/agent/loop.go:11-18` for the canonical grouping.

### Function design

- Functions stay small enough to fit on one screen. Long `Turn`-style methods (`/home/user/Aura/internal/agent/loop.go:50-105`) are tolerated when they encode a state-machine that fragmenting would obscure, but any helper that exceeds ~40 LOC is a refactor candidate on next touch.
- Single responsibility. Use private helpers (`l.runTool`, `l.toolDefs`) instead of nested closures.
- No `panic` outside genuinely unrecoverable bugs. Always wrap errors with `fmt.Errorf("<context>: %w", err)`. See `loop.go:62, 110-111, 105`.

### Error handling

- **Error wrapping always.** Pattern: `fmt.Errorf("<package or operation>: %w", err)`. Examples: `"llm stream step %d: %w"`, `"text_response args: %w"`, `"tool_search args: %w"`, `"unknown tool %q"`.
- **Sentinel errors for control flow** (PRD §Slice 1.5):
  - `ErrAwaitingUserInput{Question, Options, Kind, Priority, ToolCallID}` — returned by `ask_user` tool, intercepted by the loop to drive pause/resume. The loop does NOT append a fake tool result; it persists `PausedState` in `aura.paused_states` and yields an escalation Event.
  - `ActionRouter.Dispatch` propagates `ErrAwaitingUserInput` UNCHANGED (do not wrap, do not coerce to `ToolResult`). PRD §Pattern condiviso, sentinel passthrough contract (audit round 1 P0).
- **HTTP errors:** wrap as `HTTPError{StatusCode int, RetryAfterSec int, Body string}` (PRD §Slice 1, line 634). This keeps retry signal serialized for the caller even when retry policy is deferred.
- **No silent error swallow.** A returned `error` is either propagated or explicitly logged + accounted for.

### Logging & magic numbers

- No `fmt.Println` for non-CLI output. Use the configured logger (added by Slice 0.9). CLI-facing messages on `os.Stderr`.
- No hard-coded paths or ports in business logic. Everything routes through `internal/config/` or env vars. See `/home/user/Aura/CLAUDE.md` §Behavioral rules.

## Architectural patterns (mandatory)

### Deferred-tool pattern

Source: `/home/user/Aura/CLAUDE.md` §Tool design, `/home/user/Aura/internal/agent/tools/spec.go:1-12`, `/home/user/Aura/internal/agent/tools/manifest.go`.

- Big tools (long description, complex JSON schema, examples) set `Deferred: true` on their `Spec`. They appear in the LLM manifest as `Name + Summary` only.
- Small tools (e.g. `text_response`, `tool_search`, `ask_user`) set `Deferred: false`. They appear in full.
- The built-in `tool_search` (non-deferred) is the hook the LLM uses to fetch full specs of deferred tools on demand. Query format: `select:Name1,Name2` or free-text keyword.
- **Why:** protects the prompt cache (no per-turn manifest bloat) and lets the registry scale to N tools.
- **File layout:** `internal/agent/tools/<name>.go`; metadata constant inline in the file.

### ActionRouter helper (multi-action tools)

Source: `/home/user/Aura/prd.md` §Pattern condiviso (lines 3945-3978).

- Introduced in **Slice 6b** (not Slice 5 — YAGNI for single-action tools). Reused in Slice 7.
- File: `internal/agent/tools/action.go` (~90 LOC).
- Shape: tool LLM-facing accepts `action` enum + args → dispatch on action → private Go handler → returns `ToolResult` or wrapped error.
- **Sentinel passthrough:** if a handler returns `ErrAwaitingUserInput`, `Dispatch` returns it UNCHANGED. Test in `action_test.go` asserts byte-identical propagation.

### Risk-Based governance (tier classification)

Source: `/home/user/Aura/prd.md` §Risk-Based Governance (lines 3981+).

- Four tiers, qualitative (not numeric): `SAFE` | `NORMAL` | `RISKY` | `DESTRUCTIVE`.
- Mapping hard-coded in `internal/scoring/`:
  - `reminder`, `backup_postgres`, `backup_neo4j` → `SAFE`
  - `agent_job` → `NORMAL` (bumps to `DESTRUCTIVE` if payload matches `/\b(rm|delete|drop|purge|truncate)\b/`)
  - `skill.create`, `skill.update`, `skill.install` → `RISKY`
  - `skill.delete` → `DESTRUCTIVE`
- Modifiers bump UP only (never DOWN): `every_minute`/`every_hour` schedule, `silent: true`, missing notifier, frequency increase > 10×, `tier=reasoning`. Saturate at `DESTRUCTIVE`.
- Pipeline: `SAFE`/`NORMAL` → execute immediately; `RISKY`/`DESTRUCTIVE` → park as `pending_approval`, agent must re-emit `ask_user(kind=approval)`.
- Audit row written for every mutation (including rejects) with `computed_risk_tier`, `gate_recommended`, `gate_taken`, `approval_source` enum (`{ask_user, cli, auto}`).

### Transaction wrapping for atomicity

Source: `/home/user/Aura/prd.md` §Slice 7c (line 1818).

- Mutations that span FS + DB MUST be wrapped in a Go-level tx: if INSERT into `aura.skill_audit` fails, the FS-move (pending → active) is rolled back via inverse rename.
- Postgres trigger `BEFORE UPDATE/DELETE` on append-only audit tables raises exception (DB-level immutability enforcement).

### KV-cache discipline (stable prefix)

Source: `/home/user/Aura/internal/agent/loop.go:5-8, 22-24`, `manifest.go:19-21`.

- `Messages[0]` is the system message. **Never mutate** between turns. The loop only appends.
- Tool manifest rendered in stable alphabetical order by Name. Any reshuffle invalidates the provider-side prompt cache.
- Slice 4 will add provider-aware cache_control on Anthropic + invariant tests (5-turn hash stability, monotonic growth, no in-place mutation).

## Behavioral rules (CLAUDE.md, apply to every change)

Verbatim from `/home/user/Aura/CLAUDE.md` §Behavioral rules:

- **NEVER SUPPOSE.** Read code before editing. If uncertain about API contract, stop and ask.
- **READ BEFORE EDIT.** Re-read a file you haven't touched in the last 5 messages.
- **3-STRIKE RULE.** Same failing approach max 3 times. On strike 3, stop and ask (or escalate via PRD-amendment, PRD §Q&A escalation).
- **NEVER MODIFY TESTS TO MAKE THEM PASS** unless the test itself is broken. Fix the code or rewrite the test with explicit justification in commit message.
- **SCOPE CONTROL.** Do exactly what was asked. No unrequested features, refactors, or improvements.
- **FOLLOW EXISTING PATTERNS.** Never invent new approaches when codebase patterns exist.
- **NO GOD CLASS.** Never create a file >600 LOC. Refactor on touch (split into `<name>_<concern>.go`).
- **REUSABLE CODE.** Never duplicate; extract a helper.
- **DEEP REFACTOR ON TOUCH.** Every file you edit gets dead-code removal + dupl-folding + LOC ≤600 + comments-updated in the SAME commit.
- **GIT PUSH DISCIPLINE.** Never `git push` (or any remote-mutating command) unless explicitly requested in the current turn. A previous approval does not carry over.
- **NO COMMENTS UNLESS WHY IS NON-OBVIOUS.** Identifier names explain what; comments only for hidden constraints, workarounds, or surprising behavior.
- **NO TEST ASILO NIDO.** Tests follow PRD §Test discipline rigorosa. See `TESTING.md`.

## Commit discipline

- **One slice = one commit** (or N for sub-slices with documented atomicity in PRD).
- **Atomic.** Smoke green before commit.
- **Message:** imperative subject + body explaining *why* (not *what* — the diff already shows what) + `Co-Authored-By:` trailer per project convention.
- **PRD-amendment commit FIRST** when implementation reveals an architectural gap (PRD §Q&A revision protocol). Never silent deviation.
- Example template (PRD §Slice 3, lines 1293-1305):

  ```
  slice 3: swarm coordinator — spawn, bus broadcast+DM, tier model

  Implements swarm.Coordinator with in-memory child registry, shared
  bus (channel-buffered, backpressure-bounded), tier→model mapping,
  payload summarizer at return-to-parent, AURA_SWARM_MAX_DEPTH=3 enforced.
  Exposes swarm.spawn / swarm.talk / swarm.join tools (deferred).

  Smoke: aura swarm-demo spawns 3 workers, broadcasts, DMs, joins all.

  LOC: +XXX src / +YY test.

  Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
  ```

## Post-edit validation (Gate 2)

After every Go file edit, in this order:

1. `go vet ./...`
2. `go build ./...`
3. `go test ./internal/<package>/` if tests exist
4. `go test -race ./internal/<package>/` for any package touched

Fix issues before moving on. CLAUDE.md §Post-edit validation.

## Slice Q&A gates (3 sequential gates, mandatory)

Source: `/home/user/Aura/prd.md` §Slice Q&A discipline (line 1490+).

1. **Gate 1 — Definition of Ready (PRE):** pre-reqs completed, open questions closed, acceptance machine-checkable, smoke runnable, file targets ≤600 LOC with refactor-on-touch documented, test plan documented, Risk-Based tier assigned (if tool-introducing slice), migration scripted + down.sql, env vars cataloged, RAM delta estimated, commit message template written.
2. **Gate 2 — Implementation Q&A (DURING):** `go vet/build/test/-race` green per touched package, refactor-on-touch executed, no asilo-nido tests, no orphan TODO, no panic, no hard-coded env, 3-strike rule respected, no test-modify-to-pass.
3. **Gate 3 — Definition of Done (POST, pre-merge):** all acceptance ticked, smoke E2E green, integration tests passing under build tags, regression green, coverage ≥75% unit / ≥60% integration, mutation testing spot-check ≥70% killed on critical files, no goroutine leak, no data race, PRD updated, conforming commit message, branch ready for merge.

No code commit lands without all three green.

---

*Convention analysis: 2026-05-28*
