---
phase: 42
slug: llm-conversation-compaction
status: replacement-approved
nyquist_compliant: true
wave_0_complete: true
created: 2026-07-13
---

# Phase 42 — Industrial Conversation Compaction Validation

No legacy COMPACT test is acceptance evidence. Every task has a runnable automated command; production activation additionally requires the explicitly manual 24-hour/1,000-attempt canary observations.

## Test tiers

| Tier | Command | Purpose |
|---|---|---|
| U | `go test ./internal/llm ./internal/conversations ./internal/config -count=1` | pure budgets, selectors, manifests, schema, ladder, rebase |
| R | `go test -race ./internal/conversations ./internal/runner ./internal/agui ./internal/assets ./internal/memory -count=1` | concurrency, cancellation, leaks, coordinator |
| DB | `go test -race -tags=db_integration ./internal/db ./internal/conversations ./internal/assets ./internal/memory -count=1` | migrations, claims/CAS, durable rollout restart/multi-replica/stale-decision/atomic rollback, restores, authz/privacy |
| API | `go test ./cmd/aura ./internal/channels/telegram ./internal/agui -count=1` | all operations surfaces, ownership, bounded bodies |
| FE | `cd web && npm test -- --run` | accessible history/preview/diff/restore and state distinctions |
| EVAL | `go test ./internal/conversations/compaction_eval -run 'Corpus|Threshold|Cohort|Rollout|Rollback' -count=1` | corpus, statistics, rollout and rollback |
| FULL | `make quality && bash scripts/coverage_docker.sh` | build/vet/lint/file-size/vuln/full tags and >=85% owned coverage |

## Requirement and numerical-gate map

| Requirement | Plans | Required tiers and assertions |
|---|---|---|
| IC-01 | 01 | U: exact Section 17.1 arithmetic; unknown/invalid/non-positive/fixed-exhausted; fallback 15%+256; 200/1000 calibration; no double count |
| IC-02 | 01,04 | U/R/DB: complete semantic units, <=20% atomic tail, disjoint manifests, typed reconstructable L1, all malformed lifecycle fixtures |
| IC-03 | 05 | U/R: trigger >=0.80/headroom; target <=0.55; savings >=max(4096,10%); reduction >=20%; exact waiver enum; L2.4 before L2.5 |
| IC-04 | 02 | DB/R: stable operation ID, uniqueness, independent-process duplicate/stale/lease/death/restore plus named governance-after-capture invalidation, manual-over-unstarted-auto, and manual-no-preemption-of-active-inference races |
| IC-05 | 02 | U/DB: RFC8785+SHA256 v1, branch isolation, atomic pointer, exactly-once classification, byte-stable reconstruction, migration goldens |
| IC-06 | 03 | U/EVAL: structured schema, no tools, L0/ledger 100%, role/delimiter/encoded/quotation/poison rejection, reduction checks |
| IC-07 | 03,05 | R/API: identical triggers, boundary preference, one overflow attempt/rebuild, no mid-stream activation, failure leaves pointer |
| IC-08 | 04 | DB/U: real provider request projection, explicit reference fallback, authz, missing/corrupt/migration/GC reachability |
| IC-09 | 05 | U/EVAL: depth four, fifth rebase, chunks <=60%, ledger/artifact coverage 100%, entailment >=0.98, similarity >=0.90, LKG on failure |
| IC-10 | 06 | DB/R: idempotent candidates, promotion off, cross-identity/region denial, secret rejection, expiry/supersession/deletion/consent/forget propagation |
| IC-11 | 02,03,05 | DB/API: active -> LKG -> bounded rebuild -> unavailable; quarantine; compatible budget preview; failed restore atomicity; DR drill |
| IC-12 | 07,10 | API/FE/FULL: CLI/REPL/Telegram/AG-UI/web parity, IDOR, body bounds, sanitization, localization, keyboard/screen reader, visual distinctions, blocking CI |
| IC-13 | 01,03,05,08,09,10 | EVAL/DB/FULL: exhaustive current-adapter table/preflight; redacted metrics; 500+200 corpus; immutable evidence; live scheduled composition; full-chain Postgres restart/replica rollback test; all thresholds below |
| IC-14 | 01-10 | FULL/DB/API/FE: additive migrations and downs, sqlc generation, backwards readers, durable scoped rollout state, CAS/restart/multi-replica/stale-decision/atomic LKG rollback, activation disabled per slice, race/leak/mutation/integration/E2E/privacy/security, exact evidence |

## Numerical promotion gates

| Gate | Automated evidence | Pass condition |
|---|---|---|
| Corpus composition | EVAL corpus census | >=500 stratified golden and >=200 adversarial, versioned |
| Authority safety | EVAL safety report | L0 and unresolved ledger 100%; accepted escalation 0 |
| State/factual retention | EVAL retention report | tool/pending >=99%; factual/decision >=98% |
| Continuation | EVAL paired confidence report | no more than 2 percentage points below baseline at 95% confidence |
| Reduction/fit | EVAL projection report | median reduction >=40%; post-projection <=target in >=99% |
| Latency/failure | EVAL histogram report | p95 proactive <=8s; overflow <=15s; failure <=1% |
| Cost | EVAL cohort/stratum report | <=15% of following 20-turn median saved-input cost; >=5 eligible turns; censor short; each >=100-attempt stratum passes |
| Canary duration | signed stage artifact (manual external observation) | deterministic 1/5/20/50%; each >=24h and >=1000 attempts |
| Auto rollback | EVAL + drill | any safety regression; continuation >2pt; failure >2%/15m; latency breach/30m; restore >1% |

## Execution sampling and terminal gates

- After every task: run its `<automated>` command; no skipped tier counts green.
- After each wave: run U plus the touched R/DB/API/FE tier.
- Before verification: run EVAL and FULL, DB migration up/down/up on disposable DB, sqlc diff, independent-handle multi-replica CAS races, close/reopen restart recovery, stale-decision rejection and atomic LKG rollback drills, race/goleak, mutation on budget/selector/validator/ladder/controller, privacy/security scans, and web E2E.
- Manual-only: observe each canary for 24 hours and 1,000 attempts; verify screen reader/keyboard UX; perform operator restore/rollback and disaster-recovery drill. Until recorded, activation remains disabled rather than treating manual evidence as passed.
- Terminal evidence records command, commit, tool/model/scorer/prompt/corpus/calibration versions, timestamp, result, environment, and any manual-only status.

## Sign-off

- [x] IC-01..IC-14 mapped to plans and tiers
- [x] Every Section 17 numerical gate mapped
- [x] Migration/runtime-state/recovery/rollback/security/privacy/evaluation gates explicit
- [x] Every plan task has a runnable automated verify command
- [x] Legacy COMPACT rows removed
