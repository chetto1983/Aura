---
phase: 45
slug: harness-correctness
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-08-13
last_updated: 2026-08-13
---

# Phase 45 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded by `/gsd-plan-phase 45` from `45-RESEARCH.md` §Validation Architecture.
> The Per-Task Verification Map and the Probe-Edge Ledger below are **populated by the planner**
> (2026-08-13 revision). Execution fills only the `Status` / `File Exists` / `Verified` columns —
> it does not author rows.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (Go 1.26 toolchain; testify + goleak per project convention) |
| **Config file** | none — repo-root `Makefile` + `scripts/coverage_gate.sh` drive the tiers |
| **Quick run command** | `go vet ./... && go build ./... && go test -race ./internal/agent/... ./internal/agent/tools/... ./internal/arcadedb/...` |
| **Full suite command** | `make quality-full` (WSL, stack up: `make db-migrate memory-up`) |
| **Estimated runtime** | quick ~90–180 s · full ~15–25 min |

**Tier map for the packages this phase touches** (from RESEARCH.md §6 — confirm each against
the file before relying on it):

| Tier | Build tag | Covers | Feeds coverage floor? |
|------|-----------|--------|-----------------------|
| unit | *(none)* | `internal/agent`, `internal/agent/tools`, `internal/arcadedb`, `cmd/arcadedb-mcp` pure logic | yes |
| db integration | `db_integration` | `aura.tool_invocations` assertions via Postgres | yes (`scripts/coverage_gate.sh`) |
| arcadedb integration | `arcadedb_integration` | memory supersede / entity resolution (SC#4) | **no** — runs in CI via `make agent-memory-eval` (`.github/workflows/ci.yml:713,811`) but is excluded from the `db_integration`-only floor |
| live E2E | — | full scenario on the running stack, driven by the real agent | no |

> **Fix-on-touch item this phase owns.** RESEARCH.md §6 measured that CLAUDE.md's claim
> "`arcadedb_integration` runs NOWHERE" is **false** — it runs with `-race` and a coverage
> profile via `make agent-memory-eval`. It simply does not feed the coverage floor. The
> corrected statement lands in CLAUDE.md in plan 45-01 Task 3, and the planner did not plan
> around a blocker that does not exist.

---

## Image Provenance (precondition for every live-tier claim)

MEASURED 2026-08-13: `aura:local` bakes **no** build SHA, so "the image was built from HEAD" was not
checkable — only assertable. `docker/aura/Dockerfile:36` builds with `-ldflags="-s -w"` and no
`-X main.commit=`; `.dockerignore:1` excludes `.git`, so `debug.ReadBuildInfo()` supplies no
`vcs.revision` to `cmd/aura/version.go:48-52`; `commit` defaults to `""` and renders `unknown`; the
Dockerfile has no `LABEL` and no `ARG`. `.goreleaser.yaml:28-32` does stamp the commit, but that is
the release path — `compose.yaml:13-16` builds `aura:local` from `docker/aura/Dockerfile`.

Plan 45-08 Task 1 step 6 closes this (`ARG VCS_REF` + `-X main.commit=${VCS_REF}`, plus the compose
`args` passthrough), so the live-run precondition becomes a mechanical comparison instead of a
timestamp heuristic. Until it lands, no live-tier evidence in this file should be treated as
attributable to a known commit.

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test -race ./internal/<touched-package>/`
- **After every plan wave:** quick run command above (all three packages, `-race`)
- **Before `/gsd-verify-work`:** `make quality-full` green **and** the live E2E scenario driven
  through the running stack (project DoD — a green unit suite closes nothing).
- **Max feedback latency:** 180 s at task granularity.

---

## Per-Task Verification Map

**23 tasks across 8 plans**, populated by the planner 2026-08-13. Execution sets `File Exists` and
`Status` only. Two rows are checkpoints and carry no automated command by design; both are adjacent
to automated rows, so no three consecutive tasks lack automated verification.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 45-01-T1 | 45-01 | 1 | HARN-01, HARN-02 | T-45-01, T-45-02 | Amendment cites the measurement, not a preference; amendment number computed from the file | docs assertion | `grep -c "ReplayReissueExecutes" .planning/ROADMAP.md; grep -q "RoundOrdinal" .planning/ROADMAP.md && grep -q "model_round.go" prd.md && echo OK` | ⬜ | ⬜ pending |
| 45-01-T2 | 45-01 | 1 | HARN-02 | T-45-01 | Phase 46 dependency narrowed against a re-read of `bridge.go`, not against prose | docs assertion | `sed -n '/### Phase 46/,/^\*\*Requirements/p' .planning/ROADMAP.md \| grep -i "depends on" \| grep -qiv "vocabulary" && echo OK` | ⬜ | ⬜ pending |
| 45-01-T3 | 45-01 | 1 | HARN-01 | T-45-03 | Comment-only; redaction logic and preview cap untouched | unit (build) | `go vet ./internal/toolinvocations/ && go build ./... && grep -q "agent-memory-eval" CLAUDE.md && echo OK` | ⬜ | ⬜ pending |
| 45-02-T1 | 45-02 | 2 | HARN-01, HARN-02, HARN-09 | T-45-04, T-45-05, T-45-06 | Fail-closed `errMissingModelRound`; no silent ordinal-0 fallback | unit + `db_integration` | `go vet ./... && go build ./... && go test -race -run 'TestDeriveToolOperationContext' ./internal/agent/ && go test -race ./internal/agent/ ./internal/gateway/ ./internal/idempotency/` · `go test -race -tags=db_integration -run 'RoundOrdinal\|CrossRound' -count=1 ./internal/gateway/` | ⬜ | ⬜ pending |
| 45-02-T2 | 45-02 | 2 | HARN-02, ACC-02 | T-45-07, T-45-08 | Reclaim executes once; unaccounted prior dispatch still DENIED, never fabricated | `db_integration` | `go vet ./internal/gateway/ && go test -race -tags=db_integration -count=1 -v ./internal/gateway/ 2>&1 \| tail -40` | ⬜ | ⬜ pending |
| 45-02-T3 | 45-02 | 2 | HARN-02 | T-45-07 | Deploy-drain hazard (D-06) recorded before deploy, not after | docs assertion | `test "$(awk -F'\|' '/^\| 45-02-T/ && $(NF-1) ~ /pending/ {n++} END {print n+0}' .planning/phases/45-harness-correctness/45-VALIDATION.md)" -eq 0 && echo OK` | ⬜ | ⬜ pending |
| 45-03-T1 | 45-03 | 3 | HARN-03 | T-45-09, T-45-12 | Both replay layers label their result through ONE helper; nil case gains no marker | unit | `go vet ./internal/gateway/ && go test -race -run 'Replay' ./internal/gateway/ && go test -race ./internal/gateway/` | ⬜ | ⬜ pending |
| 45-03-T2 | 45-03 | 3 | HARN-03, ACC-02 | T-45-10 | Replay answerable from the span; derivation unit-tested, not an inline conditional | unit | `go vet ./... && go build ./... && go test -race ./internal/agent/ ./internal/gateway/ && grep -rq "aura.tool.replay_layer" internal/agent/ && echo OK` | ⬜ | ⬜ pending |
| 45-03-T3 | 45-03 | 3 | HARN-02 | T-45-11 | Boot-time fail-closed on incomplete mutating-tool metadata (ASVS V4); emptiness only | unit | `go vet ./internal/gateway/ && go test -race -run 'Validate' ./internal/gateway/ && go build ./... && go test -race ./internal/gateway/ ./internal/agent/mcptools/` | ⬜ | ⬜ pending |
| 45-04-T1 | 45-04 | 3 | HARN-08 | T-45-13, T-45-14, T-45-16 | No randomness source; deterministic ids preserve cache-prefix stability | unit | `go vet ./internal/agent/ && go test -race -run 'TestUniquifyToolCallIDs' ./internal/agent/ && ! grep -qE "uuid\|rand\.\|time\.Now" internal/agent/llm_agent_call_dedup.go && echo OK` | ⬜ | ⬜ pending |
| 45-04-T2 | 45-04 | 3 | HARN-09 | T-45-15, T-45-17 | Dropped duplicate leaves no orphan `tool_call` and gets no synthesized result | unit | `go vet ./internal/agent/ && go test -race -run 'TestDedupeSameMessageCalls\|TestUniquifyToolCallIDs' ./internal/agent/ && go test -race ./internal/agent/` | ⬜ | ⬜ pending |
| 45-04-T3 | 45-04 | 3 | HARN-08, HARN-09 | T-45-13, T-45-15 | Repairs run before ANY downstream consumer (validation, reservation key, history, wire) | unit + LOC gate | `go vet ./... && go build ./... && go test -race ./internal/agent/ ./internal/gateway/ && test "$(wc -l < internal/agent/llm_agent.go)" -lt 600 && echo OK` | ⬜ | ⬜ pending |
| 45-05-T1 | 45-05 | 3 | HARN-06 | T-45-18, T-45-19, T-45-20 | Critic judges every voluntary termination; bounded at 2 vetoes; fail-open preserved | unit | `go vet ./internal/agent/ && go test -race -run 'TestCompletionGate' ./internal/agent/ && go test -race ./internal/agent/` | ⬜ | ⬜ pending |
| 45-05-T2 | 45-05 | 3 | HARN-07 | T-45-21, T-45-22 | `messages[0]` stays byte-stable and volatile-free; exactly one language rule | unit | `go vet ./... && go test -race ./internal/agent/ ./internal/agent/prompt/ && test "$(grep -c "operator's language" internal/agent/prompt.go)" -eq 1 && echo OK` | ⬜ | ⬜ pending |
| 45-06-T1 | 45-06 | 3 | HARN-04 | T-45-23 | Exact-match close keyed on `fact_key`; broad statement unchanged and still reachable | unit + `arcadedb_integration` | `go vet ./internal/arcadedb/ && go test -race ./internal/arcadedb/ && go build ./... && test "$(wc -l < internal/arcadedb/memory.go)" -lt 600` · `go test -race -tags=arcadedb_integration -run 'FactKey' -count=1 ./internal/arcadedb/` | ⬜ | ⬜ pending |
| 45-06-T2 | 45-06 | 3 | HARN-04 | T-45-24, T-45-25, T-45-28 | Refuse-with-candidates on 0 or >1; no recency/similarity/cardinality inference | unit + `arcadedb_integration` | `go vet ./internal/arcadedb/ && go test -race ./internal/arcadedb/ && go build ./...` · `go test -race -tags=arcadedb_integration -run 'Ambig\|Supersede' -count=1 ./internal/arcadedb/` | ⬜ | ⬜ pending |
| 45-06-T3 | 45-06 | 3 | MEM-05 | T-45-26, T-45-27 | Prose object rejected before any statement is issued (ASVS V5); bound set from a measurement | unit + `arcadedb_integration` | `go vet ./internal/arcadedb/ && go test -race -run 'Validate' ./internal/arcadedb/ && go test -race ./internal/arcadedb/ && go build ./...` · `go test -race -tags=arcadedb_integration -run 'Prose' -count=1 ./internal/arcadedb/` | ⬜ | ⬜ pending |
| 45-07-T1 | 45-07 | 4 | HARN-04, MEM-04 | T-45-29, T-45-30 | Published-contract shape frozen at a decision gate BEFORE it is written | **checkpoint:decision** — no automated command by design; bracketed by 45-06-T3 and 45-07-T2, both automated | *(n/a — checkpoint)* | ⬜ | ⬜ pending |
| 45-07-T2 | 45-07 | 4 | HARN-04 | T-45-29, T-45-30 | Refusal is a SUCCESSFUL, effect-free call with a nil error — never `mcp.ToolCallError` | unit + `arcadedb_integration` | `go vet ./cmd/arcadedb-mcp/ && go test -race ./cmd/arcadedb-mcp/ && go build ./... && test "$(wc -l < cmd/arcadedb-mcp/tool_memory.go)" -lt 600` · `go test -race -tags=arcadedb_integration -count=1 ./cmd/arcadedb-mcp/` | ⬜ | ⬜ pending |
| 45-07-T3 | 45-07 | 4 | MEM-04 | T-45-31, T-45-32, T-45-33 | Exact two-identifier equality only; a substring match is NOT rewritten (ASVS V5) | unit + `arcadedb_integration` | `go vet ./cmd/arcadedb-mcp/ && go test -race -run 'TestCanonicalSubject' ./cmd/arcadedb-mcp/ && go test -race ./cmd/arcadedb-mcp/ && go build ./...` · `go test -race -tags=arcadedb_integration -run '^TestAgentMemoryMCPLiveCanonical' -count=1 ./cmd/arcadedb-mcp/` | ⬜ | ⬜ pending |
| 45-08-T1 | 45-08 | 5 | ACC-01 | T-45-38 | Coverage gate uses the disposable `aura_cov`, never the live `aura` database | full matrix + mutation | `go vet ./... && go build ./... && go test -race ./internal/agent/ ./internal/gateway/ ./internal/arcadedb/ ./cmd/arcadedb-mcp/` (then `make quality-full`, `bash scripts/coverage_docker.sh`, `make agent-memory-eval`, `go-mutesting`) | ⬜ | ⬜ pending |
| 45-08-T2 | 45-08 | 5 | ACC-01, ACC-02, HARN-01..09, MEM-04, MEM-05 | T-45-34, T-45-35, T-45-36, T-45-37, T-45-39 | Evidence gathered only against a HEAD-built image, a drained scheduler and a named live model | **checkpoint:human-verify** — live E2E is the only tier that samples SC#5; no automated command exists or should be invented. Bracketed by 45-08-T1 and 45-08-T3, both automated | *(n/a — live E2E, steps a–h)* | ⬜ | ⬜ pending |
| 45-08-T3 | 45-08 | 5 | ACC-01, ACC-02 | T-45-34 | A requirement with no live evidence stays OPEN; snapshot verified locally before the push | docs + gate script | `AURA_QUALITY_CHANGED_FILES="$(git diff --name-only origin/master..HEAD)" AURA_QUALITY_BASE_DATE="$(git log -1 --format=%cs origin/master)" bash scripts/quality_snapshot_gate.sh` | ⬜ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Probe-Edge Ledger

Every probe edge this phase owes, enumerated from the plan files 2026-08-13. A verifier reads
`must_haves`, not `<behavior>` — so an edge that lives only in a task's `<behavior>` block is
**unverifiable after execution**. Ten such edges were found in the checker's revision pass and
lifted into the owning plan's `must_haves.truths`; they are marked **↑lifted**.

**Classification:** `explicit` = asserted by a named test · `backstop` = carried as a
`verification: backstop` scalar because no assertion can sample it · `flagged-assumption` = a known
hazard accepted with a disposition and an owning phase, deliberately NOT closed here.

| # | Plan | Category | Edge | Class | Verified (test / justification) |
|---|------|----------|------|-------|-------------------------------|
| PE-01 | 45-02 | empty / single-element | No parent operation, or parent scope == tool scope → `(ctx, nil)` unchanged; the round is required only on the deriving path | backstop | ⬜ |
| PE-02 | 45-02 | equality / determinism | Same round + identical inputs → byte-equal key; different round → byte-different key | explicit | ⬜ |
| PE-03 | 45-02 | bounds | `"child:" + 64 hex` = 70 bytes, inside `MaxOperationKeyBytes = 200` and the `octet_length BETWEEN 1 AND 200` CHECK | explicit | ⬜ |
| PE-04 | 45-02 | absent input / fail-closed | `modelRoundFromContext` `ok=false` with a differently-scoped parent → `errMissingModelRound`, never a silent ordinal 0 | explicit | ⬜ |
| PE-05 | 45-02 | idempotency | Scheduler reclaim (stable parent op, fresh `RequestID`, ordinal restarting at 1) derives the SAME key → exactly one execution | explicit | ⬜ |
| PE-06 | 45-02 | concurrency | Concurrent swarm workers sharing a `RequestID`, each at their own round 1, still collide on one key | flagged-assumption | Pre-existing and unchanged (D-05, T-45-08 accept). Owned by Phase 51 / SWARM-07. This phase neither regresses nor fixes it |
| PE-07 | 45-03 | empty | `replayResult(nil)` keeps its "the tool did NOT run" preview and must NOT gain `replayedMarker`; `decodeOperationReplay` on nil/empty returns its error with no marker | explicit ↑lifted | ⬜ |
| PE-08 | 45-03 | composition / ordering | A GC'd-sidecar replay carries BOTH `resultExpiredMarker` and `replayedMarker`; neither replaces the other | explicit ↑lifted | ⬜ |
| PE-09 | 45-03 | negative scoping | A NON-mutating tool with all three operation fields empty does NOT panic; a complete mutating tool does NOT panic | explicit ↑lifted | ⬜ |
| PE-10 | 45-03 | negative | A freshly executed call ends its span WITHOUT `aura.tool.replayed=true` | explicit | ⬜ |
| PE-11 | 45-04 | empty / single-element | nil, empty and single-element `[]llm.ToolCall` are identity — no repair, no drop | explicit | ⬜ |
| PE-12 | 45-04 | equality | Ids compared by exact bytes (no trim/case-fold/NFC); `(name, arguments)` compared through `canonicaljson.CanonicalArgs` | explicit | ⬜ |
| PE-13 | 45-04 | idempotency / determinism | The same input slice run twice yields byte-identical output ids (distinct from, and not satisfied by, the no-randomness grep) | explicit ↑lifted | ⬜ |
| PE-14 | 45-04 | collision transitivity | `["abc", "abc", "abc_d2"]` → three DISTINCT ids; the counter keeps incrementing past a later original | explicit ↑lifted | ⬜ |
| PE-15 | 45-04 | ordering | Uniquify at the `consume` return, dedupe at the history append — AND the surviving calls keep their exact relative order | explicit ↑lifted (order half) | ⬜ |
| PE-16 | 45-04 | concurrency / atomicity | Both functions pure and synchronous over one slice; the repaired slice replaces the original before any consumer observes it, so an interruption leaves no partially-repaired batch | explicit | ⬜ |
| PE-17 | 45-05 | bounds exhaustion | `completionAttempts` bounds at exactly 2 vetoes; a third attempt is accepted regardless of the critic's verdict | explicit ↑lifted | ⬜ |
| PE-18 | 45-05 | empty | A tool-only round producing no user-facing text is vacuously HARN-07-compliant | explicit | ⬜ |
| PE-19 | 45-05 | fail-open | A critic error, timeout or unavailable model still lets the turn end | explicit | ⬜ |
| PE-20 | 45-05 | idempotency | `systemMessage()` called twice in one process returns byte-identical strings (the cache-prefix invariant) | explicit | ⬜ |
| PE-21 | 45-05 | equality / definition | Whose "operator's language" applies — the most recent user message overrides the stored preference | backstop | No detector and no enforcement gate exists in Aura or hermes. This is a prompt rule; ACC-01's live conversation is the only tier that samples it, and fabricating an automated check would be worse than none |
| PE-22 | 45-06 | empty / single-element | A `fact_key` naming no still-valid fact is the 0-match refusal (never a silent no-op success); exactly one candidate closes that one | explicit | ⬜ |
| PE-23 | 45-06 | equality | `fact_key` is hex of a length-prefixed SHA-256 over `TrimSpace` of `(Subject, Predicate, Object, Statement)`, compared as an exact string — no folding, no normalization, no fuzzy subject match | explicit | ⬜ |
| PE-24 | 45-06 | ordering | The legacy `supersedes: true` path RESOLVES the candidate set before closing anything | explicit | ⬜ |
| PE-25 | 45-06 | idempotency | The prose check is a pure function — running it twice on the same fact yields the same verdict | explicit | ⬜ |
| PE-26 | 45-06 | atomicity | A rejected write leaves no partial graph state: validation runs before any statement is issued, so no `Entity` vertex and no FACT edge are created | explicit | ⬜ |
| PE-27 | 45-06 | concurrency | Validation adds no shared state and no lock; concurrent writes are arbitrated exactly as before, by the pre-existing `UNIQUE` index on `Entity.name` | explicit | ⬜ |
| PE-28 | 45-06 | negative / no-regression | A short entity-name object Aura writes today is still ACCEPTED; the rune bound sits strictly above the longest legitimate object MEASURED, with the margin recorded | explicit ↑lifted | ⬜ |
| PE-29 | 45-07 | empty | A blank or whitespace-only subject is NOT canonicalized — it falls through to `Fact.validate`, which rejects it. Canonicalization never invents a subject | explicit | ⬜ |
| PE-30 | 45-07 | equality | `TrimSpace` + case-insensitive against the resolved identity UUID and the configured display name; anything else passes through byte-unchanged | explicit | ⬜ |
| PE-31 | 45-07 | ordering | Canonicalization runs BEFORE `arcadedb.Fact{}` is constructed, so bridge, CLI and host-driven writes are all covered | explicit | ⬜ |
| PE-32 | 45-07 | idempotency | `canonicalSubject(canonicalSubject(x)) == canonicalSubject(x)` — the canonical form canonicalizes to itself | explicit ↑lifted | ⬜ |
| PE-33 | 45-07 | negative | A subject that merely CONTAINS the operator's name or UUID is NOT rewritten — substring matching would re-attribute third-party facts to the operator | explicit ↑lifted | ⬜ |

### Tally

| Class | Count |
|-------|-------|
| explicit (asserted in `must_haves.truths`, verified by a named test) | **30** |
| backstop (`verification: backstop` scalar; no assertion can sample it) | **2** — PE-01 (`45-02-PLAN.md:26`), PE-21 (`45-05-PLAN.md:26`) |
| flagged-assumption (accepted hazard, disposition + owning phase recorded) | **1** — PE-06 (D-05 → Phase 51 / SWARM-07) |
| **Total probe edges** | **33** |
| of which lifted from `<behavior>` prose into `must_haves` this revision | **10** — PE-07, PE-08, PE-09, PE-13, PE-14, PE-15, PE-17, PE-28, PE-32, PE-33 |

### Reconciliation with the 19 generated probe items

The 19 probe items were produced by `edge-probe.cjs` and passed inline to the planning prompt; they
were never written to a file, which is why they cannot be re-derived from the repository. They are
**one row per (requirement ID, category) pair** across all twelve requirements — categories are
`empty` / `encoding` / `concurrency` / `idempotency` / `unclassified` — not a per-plan allocation.
Recorded here so the mapping is auditable without the generator:

| # | Requirement | Category | Lands on | Ledger row |
|---|-------------|----------|----------|------------|
| 1 | HARN-01 | unclassified | `45-02-PLAN.md:19` | PE-02, PE-03, PE-05 |
| 2 | HARN-02 | unclassified | `45-02-PLAN.md:20-21` | PE-04, PE-05 |
| 3 | HARN-03 | unclassified | `45-03-PLAN.md:21-25` | PE-07, PE-08, PE-10 |
| 4 | HARN-04 | empty | `45-06-PLAN.md:21` | PE-22 |
| 5 | HARN-04 | encoding | `45-06-PLAN.md:22` | PE-23 |
| 6 | HARN-06 | unclassified | `45-05-PLAN.md:17-20` | PE-17, PE-19 |
| 7 | HARN-07 | empty | `45-05-PLAN.md:24` | PE-18 |
| 8 | HARN-07 | encoding | `45-05-PLAN.md:25-26` **backstop** | PE-21 |
| 9 | HARN-08 | empty | `45-04-PLAN.md:23` | PE-11 |
| 10 | HARN-08 | encoding | `45-04-PLAN.md:27` | PE-12 |
| 11 | HARN-08 | concurrency | `45-04-PLAN.md:29` | PE-16 |
| 12 | HARN-09 | empty | `45-04-PLAN.md:23` | PE-11 |
| 13 | HARN-09 | encoding | `45-04-PLAN.md:28` | PE-12 |
| 14 | MEM-04 | empty | `45-07-PLAN.md:21` | PE-29 |
| 15 | MEM-04 | encoding | `45-07-PLAN.md:22` | PE-30 |
| 16 | MEM-05 | idempotency | `45-06-PLAN.md:24` | PE-25 |
| 17 | MEM-05 | concurrency | `45-06-PLAN.md:26` | PE-27 |
| 18 | ACC-01 | unclassified | `45-08-PLAN.md:17,20` | *(verification plan — no code edge)* |
| 19 | ACC-02 | unclassified | `45-08-PLAN.md:21-22` | *(verification plan — no code edge)* |

**All 19 map onto the plan set with zero unaccounted.** The 33-row ledger above is a strict
superset: it enumerates per edge-instance rather than per (requirement, category) pair, so it also
carries the bounds, ordering, composition, negative-scoping, fail-open and atomicity edges the
generated list does not name — including the ten lifted out of `<behavior>` prose this revision,
which were unverifiable before it.

Two claims in the first revision pass were correct and are recorded here rather than re-litigated:
the backstop count is **2**, not 1 (`45-02-PLAN.md:26` and `45-05-PLAN.md:26` — the latter sat at
:25 before a truth was lifted above it), both correctly shaped flat scalars; and `45-03` carried
three probe edges living only in `<behavior>`, now PE-07/PE-08/PE-09.

No row was cleared by inventing a criterion. PE-06 and PE-21 stay unasserted because neither can be
honestly sampled: PE-06 is a pre-existing concurrency hazard this phase does not touch (D-05 →
Phase 51), and PE-21 has no detector anywhere in Aura or hermes.

---

## Success-Criterion → Evidence Map

Each ROADMAP success criterion, the tier that proves it, and the observation that counts as
proof. A criterion proved only at a lower tier than listed here is **not** proved.

| SC | Statement (abbreviated) | Proving tier | Observation that counts as proof |
|----|-------------------------|--------------|----------------------------------|
| 1 | Same mutating command re-issued in a later round executes twice | `db_integration` + live E2E | Two distinct rows in `aura.tool_invocations` for the two rounds — asserted by SQL, not by log reading |
| 2 | A retried dispatch still executes exactly once | `db_integration` | Exactly one execution row for the reclaimed operation after a simulated dispatch replay |
| 3 | A legitimate replay carries a visible replay marker | unit + live E2E | `replayedMarker` present in the tool result surfaced to the model **and** the OTel span attribute set alongside it (mirroring the `resultExpiredMarker` seam) |
| 4 | Correcting one fact leaves siblings valid | `arcadedb_integration` + live E2E | Post-correction graph read shows exactly one closed validity window among facts sharing subject+predicate |
| 5 | Operator-language replies, no leaked deliberation, no unkept stated intention | live E2E only | Real scenario transcript, scored — cannot be sampled at any automated tier |

### Requirements NOT covered by SC#1–#5

SC#1–#5 evidence HARN-01/02/03/04/06/07 and ACC-02 — and nothing else. Three phase requirements
fall outside that set and are evidenced by their own live steps in plan 45-08 Task 2, because
ACC-01 makes a requirement without live evidence **not done**:

| Requirement | Live step | Observation that counts as proof |
|-------------|-----------|----------------------------------|
| MEM-04 | 45-08-T2 step **f** | Two operator facts written in one conversation — one subject by display name, one by identity UUID — converge on exactly ONE `Entity` vertex in the canonical form, with both FACT edges attached. Graph read quoted |
| MEM-05 | 45-08-T2 step **g** | A sentence-shaped object is REJECTED with an error naming the `statement` field; `Entity` vertex count unchanged across the attempt; Aura then recovers by re-issuing with a short object. Error text quoted verbatim |
| HARN-09 (same-message half) | 45-08-T2 step **h1** | The dedup keys on identical `(name, arguments)` in one assistant message, which IS promptable in ordinary operator language ("check X, and check it again"). The persisted `aura.conversation_turns.tool_calls` jsonb for that turn holds exactly ONE of the two calls, and `aura.tool_invocations` holds exactly one `end` row. Driven, not inferred |
| HARN-08, and both as a cross-check | 45-08-T2 step **h2** | The D-12 provider invariant, joined across `aura.conversation_turns.tool_calls` × `aura.tool_invocations` for the run: every persisted `tool_call` has exactly one matching `end` row, and every `end` row has a matching `tool_call`. Zero orphans in either direction. Plus per-shape counts of repaired ids (`_d<n>`, `call_<12 hex>`). **HARN-08's repair is not inducible** — see the limitation below |

> **The limitation is HARN-08's alone, and it is not extended to HARN-09.**
>
> **HARN-08** repairs duplicate or blank `tool_call_id`s — a *provider*-generated malformation. It
> cannot be requested of the model in operator language, and hand-crafting a request to force it
> would be a self-authored probe, which this phase's own prohibitions forbid as evidence. So if no
> repaired id appears in the run, that is recorded explicitly and HARN-08 closes as **unit-proven
> plus live-invariant-proven**, never as "live-verified".
>
> **HARN-09's same-message half** is a different mechanism: it keys on identical `(name, arguments)`
> within one assistant message, which is straightforwardly promptable through the real agent. It is
> therefore DRIVEN at step h1, in the same class as steps a–g. A missing h1 result is an unattempted
> step, not an inherent limit. HARN-09's cross-round half is separately and fully proved by SC#1.
>
> **What step h must NOT assert.** MEASURED: `internal/db/migrations/0011_tool_invocations.up.sql:45-46`
> declares `CREATE UNIQUE INDEX tool_invocations_once_per_phase_idx ON aura.tool_invocations
> (conversation_id, request_id, tool_call_id, event_kind)`. Any assertion that no `tool_call_id`
> repeats within that tuple returns zero in **every** world — with the repair, without it, and
> before this phase existed. It is unfalsifiable and evidences nothing, and closing a requirement on
> it would be exactly the tautology ACC-01 forbids. The unrepaired symptom is a **missing** row, not
> a duplicate one: two blank or colliding ids collapse onto one `ReservationKey`. The code-sensitive
> replacement is h2's two-directional join invariant, which fails on an orphan `tool_call` (dedup
> misbehaving) and on a `tool_call` with no `end` row (id collapse). That is why h2 reads
> `aura.conversation_turns.tool_calls` (`0005_conversations.up.sql:30`) and not only the ledger.

---

## Deploy Hazard — drain the scheduler before deploying (D-06)

Recorded here at planning time so it cannot be lost between planning and deploy; plan 45-02 Task 3
confirms it and plan 45-08 Task 2 gates the live run on it.

Adding `RoundOrdinal` to the child fingerprint changes **every** child key's hash output. Any
operation still `in_progress` under the old `tool-child-v1` key at deploy time becomes unreachable,
and a scheduler reclaim of it would derive a `tool-child-v2` key and **execute the mutation once
more**. **The scheduler must be drained before this phase is deployed.**

Two things are explicitly NOT affected, and why:

- **Approval-resume** — the withheld attempt never reaches Layer B, so no operation-registry key
  exists to be orphaned.
- **Layer A's `ReservationKey`** — `{ConversationID, RequestID, ToolCallID}` is untouched by this
  change; only the Layer B child operation key changes shape.

**Key length is not a concern:** the format stays `"child:" + 64 hex chars` = **70 bytes**, well
inside `MaxOperationKeyBytes = 200` and inside migration `0043_idempotency_operations.up.sql`'s
`octet_length BETWEEN 1 AND 200` CHECK. The field addition changes the hash input, not the digest
width.

---

## ACC-02 evidence surfaces — the fourth one, and why it carries nothing

`REQUIREMENTS.md:183` names four surfaces: OpenTelemetry traces, `aura.tool_invocations`,
`aura.conversation_turns` **and `aura.context_rot_events`**. CONTEXT.md D-22 names only the first
three plus the ArcadeDB graph, and CONTEXT.md is authoritative on method — so the omission is
deliberate, and here is the reason in one line:

**`aura.context_rot_events` carries no evidence for SC#1–#5.** Its columns are `ts`,
`conversation_id`, `action`, `pairs_dropped`, `tokens_before`, `tokens_after`
(`internal/db/migrations/0005_conversations.up.sql:40-47`) — an L2.5 hard-rolling-buffer audit,
written only when L1+L2 are insufficient and the oldest pair(s) are dropped. It records nothing
about tool execution, replay, turn honesty or memory validity, so none of the five criteria is
observable there.

It is still read once during the live run, as a **confound check** rather than as evidence: rows
for the scenario's `conversation_id` mean history was truncated mid-scenario, and an SC#5 failure
after a truncation may not be attributable to this phase's code. The reading (ideally: no rows) is
recorded by plan 45-08 Task 3.

---

## Wave 0 Requirements

- [ ] Test stubs for the round-ordinal key discrimination (SC#1/#2) before the key shape changes
      — owned by **45-02-T1** (`internal/agent/idempotency_operation_test.go`, new file; RED recorded)
- [ ] Test stubs for the `replayedMarker` seam (SC#3), mirroring the existing `resultExpiredMarker`
      tests — owned by **45-03-T1** (`internal/gateway/reserve_test.go`, pattern-matched on
      `TestReplayResultMissingSidecar`)
- [ ] Test stubs for the exact-fact-identifier supersede path (SC#4) under `arcadedb_integration`
      — owned by **45-06-T1** (`internal/arcadedb/memory_integration_test.go`, mirroring
      `TestSupersessionClosesTheWindowAndKeepsThePastQueryable` including `isolate(t, client)`)
- [x] Pre-flight grep (RESEARCH.md open question 2): does any existing test pin `FingerprintTyped`'s
      exact field set or the `"tool-child-v1"` literal? — **ANSWERED at planning time, 2026-08-13:
      zero matches on the `"tool-child-v1"` literal; all 8 `FingerprintTyped` test occurrences build
      a PARENT struct, never the child literal. No golden test blocks D-01.** Recorded in
      `45-01-PLAN.md` `<assumption_delta_decision>` and in `45-RESEARCH.md` Open Question 2's
      resolution. **45-02-T1 re-runs the identical grep as a task step** (RESEARCH.md's 14-day
      validity window + an active branch make a stale answer possible); if the re-run disagrees, the
      pinning test is updated in the same commit rather than worked around.

*Framework install: not required — `go test` and all tiers already exist.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| SC#5 — operator-language replies, no raw deliberation in user-facing text, every stated intention either ran or was plainly disclaimed | ACC-01, ACC-02, HARN-06, HARN-07 | Judgement over a real conversation; no assertion samples it | Drive the real agent on the running stack through the SC#1–#4 scenario plus steps f/g/h; read the transcript; score >9.8 per project DoD (45-08-T2) |
| MEM-04 single-vertex convergence | MEM-04 | The requirement is about what the graph looks like after the agent chose how to write — an integration test proves the function, not the behaviour | 45-08-T2 step f: two operator writes in one conversation, one by display name and one by UUID; read the graph for exactly one `Entity` vertex |
| MEM-05 rejection is actionable | MEM-05 | A unit test proves the error fires; only a live turn proves the model can act on it | 45-08-T2 step g: quote the rejection naming `statement`, then confirm Aura re-issues correctly |
| HARN-09 same-message dedup | HARN-09 | Promptable, but only a real turn shows what the model actually emitted into the persisted batch | 45-08-T2 step h1: ask for a naturally repeated action in one turn; read `aura.conversation_turns.tool_calls`; assert one surviving call and one `end` row |
| HARN-08 blank/duplicate-id repair | HARN-08 | Fires only on a provider-generated malformed batch, which cannot be requested and must not be hand-crafted | 45-08-T2 step h2: assert the D-12 join invariant across `conversation_turns.tool_calls` × `tool_invocations`, plus repaired-id shape counts; record explicitly if zero repairs fired |
| Mutation spot-check on the changed key-composition + supersede files | ACC-01 | `go-mutesting` is a WSL-only campaign, not a CI step | WSL: `PATH=~/go/bin:$PATH go-mutesting` over `internal/agent/idempotency_operation.go`, `internal/agent/llm_agent_call_dedup.go`, `internal/arcadedb/memory_supersede.go`; record killed/total per file here; floor ≥70% (45-08-T1) |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or an explicit checkpoint justification (23/23 rows)
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 180s
- [ ] SC#1–#4 each proved at the tier named in the evidence map (not a lower one)
- [ ] SC#5 scored >9.8 on a real live scenario
- [ ] MEM-04, MEM-05 proved by live steps f and g
- [ ] HARN-09's same-message half driven at step h1, with the persisted `tool_calls` jsonb quoted
- [ ] HARN-08: the D-12 join invariant asserted at step h2, and whether the repair fired live
      recorded explicitly rather than rounded up — the concession named as HARN-08's alone
- [ ] No requirement closed on a schema-guaranteed assertion (no duplicate-`tool_call_id` claim)
- [ ] Probe-Edge Ledger `Verified` column complete: every `explicit` row names its test; every
      `backstop` / `flagged-assumption` row carries its justification
- [ ] `aura:local` bakes the commit SHA (45-08-T1 step 6) and `aura version` matches
      `git rev-parse HEAD` with a clean `git status --porcelain`
- [ ] The four live-run preconditions quoted with real values before the scenario's first turn,
      each with an artifact that could have come out otherwise
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
