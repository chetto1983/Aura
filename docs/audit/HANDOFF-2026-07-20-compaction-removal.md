# HANDOFF — compaction engine removal → Wave 1

**Written:** 2026-07-20, end of the compaction-removal session (context handoff for a fresh start).
**Read next:** `docs/audit/consolidated-fix-plan-2026-07-20.md` (truth-source), memory `[[aura-fix-plan-wave0-status]]`.

---

## 1. TL;DR — what just shipped

The **dark Phase-42 `llm-conversation-compaction` engine is REMOVED** (PRD Amendment #86, operator-ratified). Merged + pushed to `master`: **`9a188ce3..22f043f7`** (5 commits). The anti-rot core is now **L4 extractive graph memory** (Neo4j `memory_search` + runner `ArchivalRecaller`), proven live.

Commits (all on `origin/master`):
| SHA | What |
|---|---|
| `22610817` | PRD: Phase 42 → REMOVED (superseded-by-#86) |
| `e5b557f0` | Go/db engine deleted (58 files, 2 dirs) + 26 de-wire sites + **migration `0042_drop_compaction`** (11 tables + 3 trigger fns, FK-safe, reversible `.down`) + sqlc regen |
| `28c12351` | orphaned web UI (`CompactionHistory`/`useCompactions`/`resources.compaction`) removed + embed bundle rebuilt |
| `826a92e8` | docs purged (ARCHITECTURE/TECHNICAL_OVERVIEW/CAPABILITIES + 3 deleted docs) + CI re-scoped |
| `22f043f7` | quality snapshot re-attested (4 rows, metric-neutral) |

## 2. ⚠️ OPEN LOOSE ENDS — do these FIRST in the fresh session

1. **PUSH the local commit `60b88d56`** (`docs(audit): mark compaction engine-removal SHIPPED`). It is committed on local `master` but **NOT pushed** (held so it wouldn't cut across the `22f043f7` CI run). `git push origin master` once CI on `22f043f7` is green. Pre-push hook runs build/deadcode/web-static/quality-snapshot (all passed before; docs-only should be trivial). Push from Windows (`deadcode` on PATH, creds configured) or WSL.
2. **VERIFY CI GREEN on `22f043f7`.** First result (run `29763292878`): **CodeQL ✓, Skills ✓, `race-db-integration-gates` (the `-race` matrix) ✓ — the CI workflow was RED on TWO Web-E2E Playwright flakes ONLY**, both confirmed **unrelated to the removal** (neither spec references compaction; my commits touched no e2e spec or screenshot baseline):
   - `chat-calm-prism.spec.ts:338` — screenshot diff **4817px = ratio 0.01 (1%)** on `calm-prism-desktop-dark.png` → antialiasing/font-render flake.
   - `governance.spec.ts:187` — `getByText('0 9 * * *')` (a scheduler cron on the governance board) "hidden" but *"14× locator resolved"* → visibility race; governance UI, not compaction.
   A **`gh run rerun 29763292878 --failed`** was triggered (watcher `bgqdji5jt`). **If the re-run is green → flakes confirmed, CI clean → push the local docs commits.** If it fails identically, these are PRE-EXISTING Web-E2E flakes on master (not this change's regression) — triage separately; do NOT block Wave 1 on them, and do NOT let them mask a real failure. The `-race` matrix already passed, so the Go removal is CI-validated.

## 3. Environment state (as left)

- **aura container REDEPLOYED** with the new image `aura:local` (id `3f890e50f5a5`, built ~17:09). `0042` was applied to the **live** DB by `aura-migrate` on redeploy → **0 compaction tables remain** (they were dark/empty, no data lost). Cockpit healthy at `http://localhost:9080`.
- **Live E2E conversation `019f8085-2df1-7396-a95f-c85d8f4dd4e8`** ("qual è la capitale d'Italia") was created in the live DB during the E2E — harmless real convo, leave or delete.
- A **Playwright browser** may still be open on the cockpit. Close it if lingering.
- Coverage gate left **disposable** DBs (`aura_cov` + `aura-neo4j-cov`) — `scripts/coverage_docker.sh` auto-drops them on exit; verify none linger with `docker ps -a | grep cov`.
- Full stack is up (postgres, neo4j, llama-embed:8081, agent-memory-mcp, etc.).

## 4. Verification already done (don't repeat)

- `go build` ✓ `go vet` ✓ · touched-package unit tests ✓ · web `tsc`/**1571 vitest**/eslint/prettier/build ✓
- **Coverage 85.5% ≥ 85%** — WSL full `db_integration neo4j_integration` matrix (`scripts/coverage_docker.sh`, authoritative Linux run). The Windows run failed ONLY on `internal/knowledge` graphview (`.sh` fork/exec — a Windows-native limitation, NOT a regression; see `[[aura-runs-container-ubuntu-only]]`).
- **`0042` up/down/up reversibility** — `TestMigrateSteps_DownUpReversible` passed in the matrix.
- Pre-push gates green incl. **both dead-code detectors** (Go `deadcode` + web `knip` → zero orphans).
- **Real E2E (>9.8, not smoke)** on the redeployed container: live turn → "Roma" · **L4 recall LIVE** (`memory__memory_get_facts`, 0.08s) · BUG-8 gauge **13.2k/1%** (= persisted `input_tokens=13246`) · cache **93%** · graph **3 nodes/2 edges** (patched Cypher client) · **0 errors / 0 compaction refs / 0 panics** · no compaction UI in nav.

## 5. Method notes (why to trust the removal + reuse the approach)

- Boundary was **grep-caller-evidence + adversarial re-check**, not filename-match (2 mapping workflows, 9 mappers). Caught **5 map misclassifications and KEPT them**: `scripts/microcompact_smoke.sh`, `internal/conversations/context_branch_test.go`, `internal/channels/telegram/testdata/statuspane_microcompact_pointer.golden`, `internal/llm/internal_context.go`, `internal/llm/openai_compat/client_compaction_test.go` — all are microcompact/L1/branch-ladder KEEP, not the dark engine.
- KEPT surfaces (verified live-callered): `context.go` L1/L2/L2.5 ladder + `context_rot_events` (amendment #22), BUG-8 gauge, L4 recall, `EstimatorCapability`+`ConservativeTokenUpperBound` seed.
- Migration files `0036/0038/0039` stay on disk (append-only); `0037_content_parts` is independent, untouched.

## 6. NEXT WORK — Wave 1 (P1): "wire the cables + turn on the dashboard"

All items CONFIRMED-verified in the plan doc. **1.9 = N/A (removed). 2.3 = DONE (removed).** Recommended order:

- **① START: 1.10 llama.cpp token estimator** — natural follow-on; `capabilities.go` is now trimmed to the `EstimatorCapability`+`ConservativeTokenUpperBound` seed. Add `DeclaredErrorTokens` on the llamacpp capability + `ProviderErrorReserveTokens` subtracted in `hardCap()`. No migration. Closes the loop the removal opened.
- **② Deadline/liveness cluster** (one phase, no migrations): **1.1** wallclock finalize (`context.WithoutCancel`), **1.4** `/readyz` scheduler freshness probe, **1.6** MCP reconnect-on-use + bounded ping, **1.3** SSE heartbeat + `Last-Event-ID` resume.
- **③ Wiring cluster** (dedicated phase each — carry schema/PRD cost): **1.2** missed-reminders catch-up-once *(migration)*, **1.7** approval relay-liveness sweep *(migration + PRD)*, **1.8** memory delete/update/list `memory_manage` *(PRD; ties into the L4 core)*, **1.11** OpenRouter middle-out fail-safe, **1.5** ingestion-drop observability.
- **④ Policy track: AG-016** three-tier gate redesign — **PRD amendment before code**.

Migration numbering: next free slot after `0042` is **`0043`** — but always `ls internal/db/migrations/ | tail -1` at landing, never deduce. Upstream memory API ref cloned at `d:/tmp/agent-memory` (for 1.8).

## 7. Standing discipline (memory)

Real E2E >9.8, not smoke (`[[e2e-real-not-smoke]]`) · run gates in WSL/container, Windows-native failures are env artifacts (`[[aura-runs-container-ubuntu-only]]`) · never point `db_integration` at live `aura`/neo4j — use disposable DBs (`[[coverage-gate-nukes-live-db]]`, `[[coverage-gate-nukes-neo4j]]`) · coverage floor 85% on the `db_integration neo4j_integration` matrix.
