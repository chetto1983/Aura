# LLM Conversation Compaction — Operations, Migration, Rollback, and Terminal Acceptance Record

Phase 42 (`llm-conversation-compaction`), plans 42-01 through 42-10. Normative
source: `docs/superpowers/specs/2026-07-13-industrial-conversation-compaction-design.md`
Section 17; requirement/gate traceability: `.planning/phases/42-llm-conversation-compaction/42-VALIDATION.md`.

This is the reproducible durable-rollout ops/migration/rollback/acceptance
record required by 42-10 (the terminal plan). It records exact commands,
commits, versions, environment, and results. **No canary stage has been
observed in a real production window as of this writing** — every stage
below is either automated-and-verified-with-synthetic-evidence or explicitly
marked "not yet observed." Nothing here claims a manual/production
observation that has not happened.

## 1. Architecture summary

Canonical `conversation_turns` history is immutable. Compaction produces a
versioned working *projection* — an immutable checkpoint plus an active
pointer — never a rewrite of history. Activation is entirely durable-state
driven: `aura.compaction_rollout_states` (one row per deployment scope,
default scope id `"default"`) is the single source of truth for every
replica; there is no `AURA_COMPACTION_MODE`/`AURA_COMPACTION_PERCENT`
environment override in production code (`config.ParseCompactionConfig`
exists only as a pure parser exercised by unit tests — no call site reads an
env var into it). A fresh deployment boots with a durable, disabled-by-default
row seeded before any read; the deployment cannot silently activate compaction
without CAS'd evidence clearing every Section 17.13 gate.

## 2. Schema and migrations

| Migration | Adds |
|---|---|
| `0036_compaction_checkpoints` | Immutable checkpoint chain, active pointer, restore audit |
| `0037_content_parts` | Typed multimodal content-part + attachment-link tables |
| `0038_compaction_memory` | Durable-memory candidate/policy tables (privacy-governed) |
| `0039_compaction_rollout` | `aura.compaction_rollout_states` / `..._evidence` / `..._decisions` — the durable rollout control plane this document centers on |

All four are additive (`up`/`down` pairs exist for each; `internal/db/migrate_0039_integration_test.go`
proves the 0039 up→down→up reversibility drill with `db.MigrateSteps`). They
run through the existing `aura db migrate` command and the existing CI
`integration-test` job's `go test -tags db_integration -race ./internal/db/...`
step — no new migration tooling was introduced.

### `aura.compaction_rollout_states` (one row per scope; version-CAS'd)

`scope_id` (PK) · `version` (bigint, CAS fence) · `stage` (`disabled` \|
`shadow` \| `canary_1` \| `canary_5` \| `canary_20` \| `canary_50` \|
`enabled`, CHECK-constrained) · `stage_started_at` · `eligible_attempts` ·
`evaluator_version`/`scorer_version`/`config_version`/`corpus_version` ·
`stratum_snapshots`/`failure_window`/`latency_window`/`restore_window` (jsonb
rolling-window inputs to the next evaluation) · `active_config` /
`last_known_good_config` / `last_known_good_policy` (jsonb).

### `aura.compaction_rollout_evidence` (immutable, append-only)

One row per sealed `compaction_eval.Decision`: `evidence_digest` (SHA-256 hex,
CHECK-constrained `^[0-9a-f]{64}$`), the four version fields, and the full
canonical JSON `snapshot`. An `UPDATE`/`DELETE` trigger
(`compaction_rollout_ledger_immutable`) raises an exception unconditionally —
the ledger cannot be edited or backdated, only appended to.

### `aura.compaction_rollout_decisions` (immutable, append-only)

One row per CAS transition or rollback: `expected_version` →
`resulting_version` (CHECK `resulting_version = expected_version + 1`,
enforcing exactly one version step per decision), `decision_kind`
(`transition` \| `rollback`), `from_stage`/`to_stage`, and a locale-neutral
`reason_code` (`^[a-z0-9_]+$`). Same immutability trigger as evidence.

## 3. Live evaluation and promotion/rollback flow

`CompactionRolloutController.Run(ctx, interval)` (wired into `aura serve` as
a background goroutine gated on `env.compactionRollout != nil`, cancelled via
`signalCtx`) calls `EvaluateOnce` on an interval (production default:
1 minute, `serve.go`):

1. **Load** the current durable state for the scope.
2. **Seal** the rolling `failure_window`/`latency_window`/`restore_window`/
   `stratum_snapshots` JSON into a `compaction_eval.Input`, then
   `compaction_eval.Evaluate(...)` — a pure function that validates required
   fields, canonically marshals the input (`encoding/json` sorts map keys,
   giving a stable snapshot for this closed schema), SHA-256-hashes it, and
   returns an immutable `Decision` with cloned (never-aliased) maps.
3. **Apply** the decision:
   - **Rollback first.** `rollbackReason` checks (in order): stale
     `evaluator_version` → `incompatible_evidence_version` (scorer/config/
     corpus version mismatch) → `corrupt_evidence`/`safety_gate_failed`
     (`AuthorityEscalations > 0` or `L0Retention < 1`) →
     `continuation_gate_failed` (`ToolPendingRetention < .99` or
     `FactualDecisionRetention < .98` or `ContinuationDelta < -0.02` or
     `ContinuationConfidence < .95`) → `failure_window_exceeded`
     (`FailureRate > .02`) → `latency_window_exceeded`
     (`LatencyBreachMinutes >= 30`) → `restore_rate_exceeded`
     (`RestoreRate > .01`). Any match calls `store.Rollback`, which
     atomically restores `last_known_good_config`/`last_known_good_policy`
     to `active_config`/current policy, sets `stage='disabled'`, and appends
     an immutable `rollback` decision row.
   - **Promotion gate.** Only when NOT rolling back: `promotionPasses(Gates)`
     requires `L0Retention==1`, `AuthorityEscalations==0`,
     `ToolPendingRetention>=.99`, `FactualDecisionRetention>=.98`,
     `ContinuationDelta>=-.02`, `ContinuationConfidence>=.95`,
     `MedianReduction>=.4`, `TargetAchievement>=.99`,
     `P95ProactiveSeconds<=8`, `P95OverflowSeconds<=15`, `FailureRate<=.01`,
     `CostRatio<=.15` — the exact Section 17.13 numerical promotion gates.
     Promotion additionally requires `now - StageStartedAt >= 24h` AND
     `EligibleAttempts >= 1000` (the canary-duration gate).
   - When ALL of the above clear, `store.Transition` CAS-updates
     `stage='canary_1'` (see §8 "Known limitations" — this is currently the
     *only* stage the automated controller promotes to) and appends an
     immutable `transition` decision with `reason_code=promotion_gates_passed`.
4. **CAS fencing.** Every `Transition`/`Rollback` call includes
   `ExpectedVersion`; `CASTransitionCompactionRollout`/
   `CASRollbackCompactionRollout` are `UPDATE ... WHERE scope_id=$1 AND
   version=$2` — a concurrent writer's stale `ExpectedVersion` returns
   `pgx.ErrNoRows`, mapped to `ErrRolloutStaleVersion`. No process ever wins a
   write by racing; only the first writer against a given version succeeds.

## 4. Restart, multi-replica, and CAS semantics

Runtime activation is fenced on BOTH ends of every claim: `chat_boot.go`
constructs a `config.PersistedCompactionReader{Source: rolloutStore,
ScopeID: "default"}` that re-reads the durable snapshot before claim and
again before finalize (`internal/runner`'s compact seams); a version change
between the two reads disables finalization — a replica cannot activate on
stale in-memory config, and process memory carries only a single-operation
version fence, never the source of truth. This chain is proven by
`TestCompactionRolloutFullChainPostgres` (two independent `pgxpool.Pool`s
simulating two replicas, close/reopen restart, stale-finalize rejection,
immutable ledger verification, atomic LKG restoration — 42-09) plus the
dedicated store-level races: `TestRolloutStoreRestartAndStaleDecision`,
`TestRolloutStoreMultiReplicaExactlyOneWins`, and
`TestRolloutStoreAtomicRollbackRestoresLastKnownGood`
(`internal/conversations/compaction_rollout_store_test.go`,
`//go:build db_integration`), and
`TestCompactionMultiProcessStaleCompletion`
(`compaction_claims_multiprocess_test.go`) for the distributed inference
claim itself.

## 5. Compatibility and disabled-by-default bootstrap

`cmd/aura/chat_boot.go` calls `rolloutStore.EnsureDisabledDefault(ctx,
"default")` **before** the fail-closed `rolloutReader.Read(ctx)` preflight
and before Runner construction (ordering pinned statically by
`TestCompactionRolloutDisabledBootstrapSeed`). `EnsureDisabledDefault` is
`Create`'s existing idempotent on-conflict-fallback: the first boot against a
freshly migrated database inserts a `stage='disabled'` row
(`bootstrapEvaluatorVersion="eval-v1"`, `bootstrapScorerVersion="score-v1"`,
`bootstrapConfigVersion="config-v1"`, `bootstrapCorpusVersion="corpus-v1"`,
`active_config={"mode":"disabled","percent":0,"recovery_drill_passed":false}`);
every subsequent boot (including every replica) finds the existing row and
returns it unmodified. A genuine failure (DB unreachable, or an existing row
failing `validateRolloutState`) still propagates, so boot keeps failing
closed rather than silently defaulting to an unvalidated in-memory config.
Migrations are additive and deployable ahead of writers; old binaries reading
the additive columns/tables are unaffected (no column was renamed or
dropped).

## 6. Numerical promotion/rollback gates (Section 17.13)

| Gate | Pass condition | Enforced by |
|---|---|---|
| Corpus composition | >=500 stratified golden and >=200 adversarial, versioned | `TestCompactionCorpusCensus` |
| Authority safety | L0 and unresolved ledger 100%; accepted escalation 0 | `TestCompactionCorpusExpectedOutcomes`, `promotionPasses`/`rollbackReason` |
| State/factual retention | tool/pending >=99%; factual/decision >=98% | `promotionPasses`/`rollbackReason` |
| Continuation | no more than 2pp below baseline at 95% confidence | `promotionPasses`/`rollbackReason` |
| Reduction/fit | median reduction >=40%; post-projection <=target in >=99% | `promotionPasses` |
| Latency/failure | p95 proactive <=8s; overflow <=15s; failure <=1% | `promotionPasses`/`rollbackReason` |
| Cost | <=15% of following eligible saved-input cost | `promotionPasses` |
| Canary duration | each stage >=24h and >=1000 eligible attempts | `Apply`'s `now().Sub(StageStartedAt) < 24h \|\| EligibleAttempts < 1000` guard |
| Auto rollback | any safety regression; continuation >2pt; failure >2%/window; latency breach>=30m; restore >1% | `rollbackReason` |

All twelve numeric literals above are read directly from
`internal/conversations/compaction_rollout.go` (`promotionPasses`,
`rollbackReason`) — this table is not independently maintained prose, it is
a transcription of the enforced code.

## 7. Evaluation corpus (golden + adversarial)

`testdata/compaction/golden.jsonl` — **510** cases across the 9 required
strata (chat 90, code 70, research 60, tool_heavy 70, approval 50,
multilingual 50, multimodal_reference 40, recursive 40, recovery 40);
eligibility `semantic_eligible`/`l1_only`/`emergency_waived`; censor
`none`/`short`/`restored`/`superseded`. `testdata/compaction/adversarial.jsonl`
— **216** cases across 22 boundary/injection taxonomy kinds (the original 18
structural kinds — role, delimiter, encoded, fake_summary, tool, quotation,
revoked, authority, empty, whitespace, oversize, manifest, source, duplicate,
revocation, schema, projection, malformed — re-schemed with the full
provenance/version/outcome contract, plus 4 new kinds the 42-10 plan calls out
explicitly: ledger, pending, artifact, poison). Every row carries a
deterministic ID (`golden-<stratum>-NNNN` / `adversarial-<kind>-NNNN`),
`schema_version=1`, `corpus_version="phase42-corpus-2026.07.14"`, a
provenance/license field, taxonomy tags, a `source` object, an
`expected_projection` (the fifteen fields mirroring
`compaction_eval.Gates` per-row plus `l0_retained`/
`unresolved_ledger_resolved`/`authority_escalation_accepted`/
`target_achieved`/`failed`/`restored`/`corrupt_evidence`), and
`expected_outcome` (`promote` for every golden row, `reject` for every
adversarial row).

`internal/conversations/compaction_eval/corpus_test.go` +
`corpus_gates_test.go` (split for the 600-LOC file cap) load both files and
run six named tests: `TestCompactionCorpusCensus`, `TestCompactionCorpusUniqueIDs`,
`TestCompactionCorpusRequiredStrata`, `TestCompactionCorpusSchemaVersionAndProvenance`,
`TestCompactionCorpusExpectedOutcomes`, and `TestCompactionCorpusNumericalGates`.
The last aggregates the golden corpus's embedded ground truth into the exact
`compaction_eval.Gates` struct, seals it through the SAME `Evaluate` seam a
live rollout controller consumes, and asserts every gate in §6 against the
computed aggregate (median/percentile/mean over the 510 rows) — no skip path
exists anywhere in this test file.

**The corpus is synthetic (house-authored, deterministic), not live-observed
production traffic.** It proves the numerical-gate *arithmetic and
aggregation* are correct and that the corpus itself is well-formed and
diverse; it does not substitute for the manual canary observation windows in
§13.

`D:/tmp/Backboard-Locomo-Benchmark/locomo_dataset.json` was consulted only as
taxonomy inspiration for the long-conversation single-hop/multi-hop/temporal
categories informing the "research"/"recursive"/"recovery" strata — it is not
copied into the repository and is not a runtime dependency.

## 8. Known limitations

**The automated canary ladder currently implements only the first promotion
step.** `CompactionRolloutController.Apply` hard-codes `next.Stage =
"canary_1"` unconditionally whenever the rollback check clears and the
24h/1000-attempt/promotion-gates guard passes — there is no stage-lookup
table keyed on the CURRENT stage, so calling `Apply` again while already at
`canary_1` (or any later stage) re-targets `canary_1` again rather than
advancing to `canary_5` → `canary_20` → `canary_50` → `enabled`. This is
confirmed by direct code inspection and by test coverage:
`TestPromotionAfter24HoursAnd1000Attempts` (fixture starts at `stage:
"shadow"`) is the only promotion test; `TestRollbackSafetyAndStaleEvaluator`
sets `Stage="canary_1"` only to test rollback FROM that stage. Full detail
and a recommended follow-up: `.planning/phases/42-llm-conversation-compaction/deferred-items.md#d1`.
This document does not claim the full ladder is automated end-to-end.

**No canary stage has ever run in production.** Every numerical gate in §6 is
proven against the synthetic corpus (§7) and against unit/integration
fixtures — never against real deployed traffic. §13 records this explicitly
per stage; activation stays disabled by default (§5) until an operator
performs the manual observation windows.

## 9. Durable-memory privacy lifecycle (IC-10/IC-11)

`internal/memory/compaction_candidates.go` + `compaction_policy.go`: a
`Candidate` carries `SourceRef{Kind,ID,Digest}`, tenant/owner, purpose,
sensitivity, and expiry; promotion (`Store.Promote`) is policy-gated per
`ClassPolicy{AllowPromotion,AllowRetrieval}` and defaults off. Deletion/
consent propagation is explicit and separately callable:
`WithdrawConsent(ctx, tenant, owner, purpose)`, `DeleteSource(ctx, kind, id)`,
`ForgetMe(ctx, tenant, owner)`, `Expire(ctx, now)`, `Supersede(ctx, old,
new)` — each revokes through one shared `revoke(reason, predicate, args...)`
helper, keeping the four propagation paths structurally identical rather
than four independent implementations. `Retrieve` gates tenant, owner,
purpose, region, and sensitivity (`principalAllows`/`sensitivityAllows`)
before returning anything.

## 10. Operator/manual surfaces (shadow-only; independent of the automated rollout)

CLI/REPL/Telegram (`/compact [history|preview|diff|restore]`) and AG-UI
(`POST /api/conversations/{id}/compact`, `.../compact/history`,
`.../compact/preview`, `.../compact/diff`, `.../compact/restore`) all share
ONE `runner.CompactCoordinator` (`NewPersistedCompactCoordinator`) — preview
is non-mutating; restore requires an explicit `safePoint=true` (rejected
`409 safe_point_required` otherwise) and is blocked while a model response is
in flight. This is a separate, always-available, shadow-only surface from
the automated rollout evaluator in §3 — an operator can preview/restore a
checkpoint at any time regardless of the rollout stage.

## 11. CI tier matrix

| 42-VALIDATION.md tier | Command | CI job |
|---|---|---|
| EVAL | `go test ./internal/conversations/compaction_eval -run 'Corpus\|Threshold\|Cohort\|Rollout\|Rollback' -count=1` | `compaction-evaluator` (new, 42-10) |
| DB (migration replay, restart/multi-replica/CAS rollback, restores, privacy) | `go test -race -tags=db_integration ./internal/conversations ./internal/assets ./internal/memory -count=1` + `internal/db`'s own 0036-0039 up/down/up drills | `compaction-distributed-gates` (new, 42-10) for conversations/assets/memory/runner/agui/cmd-aura; `internal/db/...` migration drills already ride the existing `integration-test` job |
| R (leak) | `go test -race ./internal/conversations ./internal/runner ./internal/agui ./internal/assets ./internal/memory -count=1` | `unit-test` job (generic, already covers all packages) + `goleak.VerifyTestMain` wired in `internal/conversations`'/`internal/runner`'s `TestMain` — `compaction-distributed-gates` doubles as this tier for the db-tagged run |
| API | `go test ./cmd/aura ./internal/channels/telegram ./internal/agui -count=1` | `unit-test` job (generic; not compaction-specific — no duplicate job added) |
| FE | `cd web && npm test -- --run` | `web-test` job (generic; the compaction plan added zero `web/**` files) |
| FULL | `make quality && bash scripts/coverage_docker.sh` | `build-and-lint` + `knowledge-integration-test`'s `Coverage gate` step (generic, already exercise the whole `internal/...` tree including compaction packages) |
| mutation (Section 17.13's "critical file" spot-check, CLAUDE.md convention) | `go-mutesting --exec-timeout=30 ./internal/conversations/compaction_eval/...` | `compaction-mutation` (new, 42-10) |
| E2E / disabled-bootstrap acceptance | live `aura serve` boot + `SELECT stage FROM aura.compaction_rollout_states WHERE scope_id='default'` | `compaction-e2e-acceptance` (new, 42-10) |
| privacy/security (IC-10/IC-11 durable-memory lifecycle; supply-chain) | `internal/memory` scope of the DB tier above; `govulncheck` | `compaction-distributed-gates` (memory scope) + the existing `vulncheck` job |

Four new blocking jobs were appended to `.github/workflows/ci.yml` after
`web-e2e`: `compaction-evaluator`, `compaction-distributed-gates`,
`compaction-mutation`, `compaction-e2e-acceptance`. None is
`continue-on-error`; none has a skip path — every step is an unconditional
`go test`/query assertion. No existing job was modified.

## 12. Operator commands (reproduce)

```bash
# EVAL tier (pure, no docker; WSL/CGO per this repo's race-detector requirement)
go test -race -count=1 -v ./internal/conversations/compaction_eval/...

# DB tier (stack up; -p 1 shared-Postgres serialization)
go run ./cmd/aura db migrate
go test -race -tags=db_integration -count=1 -p 1 \
  ./internal/conversations/... ./internal/assets/... ./internal/memory/... \
  ./internal/runner/... ./internal/agui/... ./cmd/aura/...

# Mutation spot-check (WSL/Linux; the avito-tech go1.26-compatible fork)
go install github.com/avito-tech/go-mutesting/cmd/go-mutesting@v0.0.0-20251226130216-48d0401f00fb
go-mutesting --exec-timeout=30 ./internal/conversations/compaction_eval/...

# E2E acceptance (stack up)
go build -o aura ./cmd/aura
./aura serve &
curl -fsS http://127.0.0.1:9080/healthz
docker compose exec -T postgres psql -U aura_migrate -d aura -tAc \
  "SELECT stage FROM aura.compaction_rollout_states WHERE scope_id = 'default'"
# -> disabled

# Full gate
make quality && bash scripts/coverage_docker.sh
```

## 13. Terminal acceptance evidence

Recorded exactly as run; commit `e889ff7d0` is the 42-10 Task 1 corpus
commit, `HEAD` at the time of this section's last edit is the 42-10 Task 2
CI/docs commit (see `42-10-SUMMARY.md` for the exact hash). Environment: Go
`go1.26.5 linux/amd64` (WSL2 Ubuntu 26.04 LTS, per this repo's documented
race-detector/CGO requirement), Windows 11 host for build/vet, live Docker
stack (`aura-postgres`, `aura-neo4j`, `aura-llama-embed`, `aura-rerank`,
`aura-agent-memory-mcp`, `aura` itself on `:9080`) already up and healthy.

### EVAL tier — `go test ./internal/conversations/compaction_eval -run 'CompactionCorpus|Threshold|Cohort' -count=1`

```
=== RUN   TestCompactionCorpusNumericalGates
    corpus_gates_test.go:232: Section 17.13 gates: l0=1.0000 ledger=1.0000
    toolPending=0.9980 factual=0.9863 delta=0.0040 medianReduction=0.4610
    target=0.9961 p95proactive=6.21s p95overflow=10.40s failure=0.0059
    cost=0.0850 restore=0.0039 escalations=0 corrupt=0 latencyBreach=0m
--- PASS: TestCompactionCorpusNumericalGates (0.02s)
--- PASS: TestCompactionCorpusCensus (0.02s)         golden=510 adversarial=216 total=726
--- PASS: TestCompactionCorpusUniqueIDs (0.02s)
--- PASS: TestCompactionCorpusRequiredStrata (0.02s)
--- PASS: TestCompactionCorpusSchemaVersionAndProvenance (0.02s)
--- PASS: TestCompactionCorpusExpectedOutcomes (0.02s)
PASS
ok  github.com/chetto1983/aura/internal/conversations/compaction_eval  0.101s
```

Every Section 17.13 numerical gate in §6 clears against the distinct,
versioned, stratified 510-case golden corpus with comfortable margin
(closest margin: factual/decision retention 98.63% vs the 98% floor).
`-race` clean (`go test -race -count=1 ./internal/conversations/compaction_eval/...`).
`golangci-lint run ./internal/conversations/compaction_eval/...`: 0 issues.

### Mutation spot-check — `internal/conversations/compaction_eval` (evaluator seal)

`evaluator.go` shipped in 42-09 with no dedicated unit test — the census
test alone (a single happy-path `Evaluate` call) killed 0/15 mutants. 42-10
added `evaluator_test.go` (field-by-field rejection cases, digest
determinism/sensitivity, map-clone independence, a boundary case at
`EligibleAttempts=0`, and a `math.NaN()`-forced `json.Marshal` failure case —
`[Rule 2 - missing critical]`, see `42-10-SUMMARY.md`):

```
The mutation score is 0.933333 (14 passed, 1 failed, 0 duplicated, 0 skipped, total is 15)
```

Per the tool's own `scripts/exec/test-mutated-package.sh` (`0) tests passed
-> FAIL` / `1) tests failed -> PASS`): **14/15 mutants killed (93.3%)**,
clearing the >=70% floor. The one survivor is a genuinely EQUIVALENT mutant:
`clone(nil)`'s early `return map[string]int64{}` removed still falls through
to `make(map[string]int64, len(nil))` (valid — `len` of a nil map is 0) then
ranges zero times over the nil map, producing the byte-identical empty
non-nil map either way — no test can distinguish the two code paths because
they are behaviorally identical.

### DB tier — `go test -race -tags=db_integration -count=1 -p 1 ./internal/conversations/... ./internal/assets/... ./internal/memory/... ./internal/runner/... ./internal/agui/... ./cmd/aura/...`

Run against the live stack (`aura-postgres`, real DSNs), `CI=true`:

```
ok  	github.com/chetto1983/aura/internal/conversations	29.730s
ok  	github.com/chetto1983/aura/internal/conversations/compaction_eval	1.652s
ok  	github.com/chetto1983/aura/internal/assets	1.290s
ok  	github.com/chetto1983/aura/internal/memory	2.089s
ok  	github.com/chetto1983/aura/internal/runner	3.375s
ok  	github.com/chetto1983/aura/internal/agui	40.199s
ok  	github.com/chetto1983/aura/cmd/aura	9.227s
```

All 7 packages pass with `-race`, exercising
`TestCompactionRolloutFullChainPostgres`, `TestRolloutStoreRestartAndStaleDecision`,
`TestRolloutStoreMultiReplicaExactlyOneWins`,
`TestRolloutStoreAtomicRollbackRestoresLastKnownGood`,
`TestCompactionMultiProcessStaleCompletion`, the full `internal/memory`
privacy-lifecycle suite (`TestPromotionRequiresReviewAndExplicitClassPolicy`,
`TestRetrievalGateDeniesCrossBoundaryBeforeRelevance` — 5 denial dimensions +
1 authorized case, `TestConsentWithdrawalDeletionForgetAndExpiryPropagate` —
4 propagation events, `TestSupersessionRemovesOldMemoryFromRetrieval`), and
`cmd/aura`'s `TestCompactionRolloutComposition`/`TestCompactionRolloutDisabledBootstrapSeed`.
The operator's `local` identity row (`00000000-0000-0000-0000-000000000001`)
was verified intact after this run (`SELECT count(*) ... = 1`) — no
collateral wipe.

### Coverage — `bash scripts/coverage_docker.sh` (disposable `aura_cov` DB)

```
==> coverage gate: internal/* >= 85% (tags: db_integration neo4j_integration)
total:                                              (statements)    85.1%
ok: owned coverage 85.1% >= 85%
```

**PASS, thin margin (85.1% vs the 85% floor).** Two Phase 42 packages sit
below the per-package ≥85% bar CLAUDE.md's coverage-campaign history
documents as previously true of every owned package: `internal/conversations`
75.7% and `internal/memory` 72.8% (per-package numbers from the same run's
`cover_gate.out.testlog`). Neither breaks the AGGREGATE floor `coverage_gate.sh`
actually enforces (it computes one merged percentage across the filtered
`internal/*` profile, not a per-package minimum), and `internal/memory`'s
privacy lifecycle IS comprehensively tested (see the DB tier paragraph above
— `compaction_candidates_test.go` covers `compaction_policy.go`'s
Promote/Retrieve/WithdrawConsent/DeleteSource/ForgetMe/Expire/Supersede
despite the filename; an untagged `deadcode -test` run flags these as
"unreachable" only because it excludes the `db_integration`-tagged test file
by default — a tooling artifact, not a real gap). This is flagged honestly
rather than silently accepted: the aggregate margin above the floor is thin,
and a future phase touching either package should re-measure.

### Web — `cd web && npm test -- --run`

```
Test Files  158 passed (158)
     Tests  1285 passed (1285)
Statements   : 92.61% ( 5480/5917 )
Branches     : 86.61% ( 3663/4229 )
Functions    : 92.71% ( 1679/1811 )
Lines        : 94.38% ( 5011/5309 )
```

PASS. Phase 42 added zero `web/**` files; this is the unchanged, pre-existing
suite passing green (>=85% floor cleared on every dimension).

### `make quality` (full repo, phase close) — build/vet/file-size/deadcode/test-race/vuln PASS; lint blocked by pre-existing, phase-42-unrelated debt

`make quality`'s `vet`, `file-size` (0 files over 600 LOC), `deadcode`
(0 exit; findings are exclusively db_integration-tagged-test-only
reachability, a known tooling artifact — see the coverage note above), and
`vuln` (`govulncheck`: **0 vulnerabilities** in code you call; 1 unreached
dependency-graph advisory) all pass cleanly. `test-race` passes at the
scoped-package level everywhere this document records a `-race` result
above; a full monolithic `make quality` run cannot currently reach that
stage because `lint` fails first — but NOT on anything Phase 42 added.

Three separate clean `make quality` invocations each surfaced a DIFFERENT
subset of a pre-existing, repo-wide `staticcheck` SA5011 false-positive
pattern (`if x == nil { t.Fatal(...) }` then dereferencing `x` — staticcheck
does not always recognize `t.Fatal`/`t.Fatalf` as execution-terminating). An
uncapped diagnostic run
(`golangci-lint run --max-issues-per-linter=0 --max-same-issues=0 ./...`)
found the true, complete set: **22 findings across 15 files** spanning
`internal/agent`, `internal/agui/auth_test.go` +
`server_display_test.go`, `internal/channels/telegram` (5 files),
`internal/llm/config_test.go`, `internal/sandbox/usersandbox`, and
`internal/web` — every one confirmed pre-existing via an EMPTY
`git diff --stat 28dfdda95^..HEAD -- <file>` (zero Phase 42 commits touch
any of them). This is understood (per operator guidance mid-execution) as a
symptom of the separately-tracked go1.26 toolchain/update infrastructure
work, not a Phase 42 defect — full detail:
`.planning/phases/42-llm-conversation-compaction/deferred-items.md#d3`.

Three representative instances were fixed here as a minimal, safe
Rule-3 blocking-fix (`internal/config/config_serve_test.go`,
`internal/config/config_test.go`, `internal/skills/smoke_test.go` — a scoped
`//nolint:staticcheck` comment matching this repo's existing `//nolint`
convention, zero test-assertion or runtime-behavior change), proving the
pattern and its fix; the remaining 19 are left for the separate effort.
`golangci-lint` scoped to every file this whole phase (42-01..42-10) plus
this executor touched: **0 issues**
(`golangci-lint run ./internal/conversations/compaction_eval/...` and
`golangci-lint run ./internal/config/... ./internal/skills/...` both report
`0 issues`).

## 14. Manual-only evidence (explicitly NOT observed; activation stays disabled)

Per 42-VALIDATION.md: "Manual-only: observe each canary for 24 hours and
1,000 attempts; verify screen reader/keyboard UX; perform operator restore/
rollback and disaster-recovery drill. Until recorded, activation remains
disabled rather than treating manual evidence as passed."

| Stage | Status |
|---|---|
| Shadow observation | NOT YET OBSERVED — no production traffic has been evaluated against this rollout scope |
| Canary 1% (>=24h, >=1000 attempts) | NOT YET OBSERVED |
| Canary 5% (>=24h, >=1000 attempts) | NOT YET OBSERVED — additionally blocked by §8's ladder limitation |
| Canary 20% (>=24h, >=1000 attempts) | NOT YET OBSERVED |
| Canary 50% (>=24h, >=1000 attempts) | NOT YET OBSERVED |
| Enabled (100%) | NOT YET OBSERVED |
| Screen reader / keyboard UX | NOT YET OBSERVED (operator Gate-3 action) |
| Operator restore/rollback drill (real deployment) | NOT YET OBSERVED — the automated `TestRolloutStoreAtomicRollbackRestoresLastKnownGood`/`TestCompactionRolloutFullChainPostgres` integration tests prove the MECHANISM against disposable databases; this is not a substitute for an operator-performed production drill |
| Disaster-recovery drill | NOT YET OBSERVED |

**Compaction activation remains disabled by default in every environment
this phase touches.** Nothing in this phase flips that default.
