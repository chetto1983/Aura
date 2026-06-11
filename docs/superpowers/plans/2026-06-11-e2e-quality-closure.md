# Phase 20 E2E and Quality Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Close the post-audit E2E evidence gap on the current branch by running or explicitly blocking every real Aura E2E gate, repairing the documented quality snapshot freshness gate, reconciling stale quality rows, and producing an indexed audit closure record.
**Architecture:** Treat evidence as a first-class deliverable. `scripts/quality_snapshot_gate.sh` enforces freshness, `docs/audit/e2e-closure-2026-06-11.md` records measured command output and operator-only gates, `docs/aura-quality-snapshot.md` becomes the reconciled source of truth, and `docs/audit/audit-index.json` links the closure record. Product behavior changes are out of scope unless a small test or CI seam is required to make the quality gate reliable.
**Tech Stack:** Go 1.26.x, bash, PowerShell or WSL, Docker Compose, Postgres, Neo4j, SearXNG, OpenRouter/DeepSeek, GitHub Actions, Python 3 JSON probes, go-mutesting.

---

## File Map

- `scripts/quality_snapshot_gate.sh` - new CI/local gate for the contract already documented in `docs/aura-quality-snapshot.md`.
- `.github/workflows/ci.yml` - wire the quality snapshot gate and add a small Windows shell-surface lane if Wave B is executed.
- `docs/audit/e2e-closure-2026-06-11.md` - new evidence record for all live, CI-backed, manual, and blocked gates.
- `docs/aura-quality-snapshot.md` - update stale E2E rows and distinguish true E2E gates from future memory-scale benchmarks.
- `docs/audit/audit-index.json` - add the new closure document to the document index and update score/rationale if the phase changes audit posture.
- `internal/eval/*_test.go`, `internal/runner/live_e2e*.go`, `internal/cron/e2e_test.go` - existing live E2E harnesses to execute.
- `scripts/deepseek_reasoning_probe.py` - existing direct provider reasoning probe to include in evidence without printing reasoning text.
- `docs/audit/testing-strategy.md` and `docs/audit/action-plan.md` - reference source for A-24 quality gaps; update only if this phase closes items there.

## Scope

- Close current E2E evidence and documentation drift.
- Repair the missing quality snapshot gate because the snapshot already claims it exists.
- Include A-24 quality-hardening work only where it protects E2E/quality closure: Windows shell lane, typed retry coverage, MCP reconnect branch coverage, loop-core mutation evidence.
- Do not implement P2 feature hardening items A-14 through A-23 in this phase.
- Do not implement the reminder agnostic-channel delivery spike in this phase; record it as a separate follow-up because Phase 19 signed it off as non-blocking.
- Do not fill 100K memory benchmark rows with the 5-document Neo4j smoke. Either run a real memory-scale benchmark or explicitly mark those rows as future benchmark work outside current E2E closure.

## Success Criteria

- Every real E2E gate has a dated result in `docs/audit/e2e-closure-2026-06-11.md`.
- No skipped paid/manual gate is counted as pass. A gate is closed only by a passing command, passing operator evidence, or an explicit blocked status naming the missing external dependency.
- `docs/aura-quality-snapshot.md` has no stale E2E wording that contradicts current evidence.
- `scripts/quality_snapshot_gate.sh` exists, has negative tests, and is wired into CI.
- Current branch is committed, pushed, and GitHub Actions are green or have a documented external-service failure.

---

## Task 1: Preflight Current State

- [x] Confirm the branch and current commit.

  ```powershell
  git branch --show-current
  git rev-parse --short HEAD
  git status --short
  ```

  Expected output: branch is `tabula-rasa` or a phase branch targeting `tabula-rasa`; status is clean before edits.

- [x] If using a new branch, create it from current `tabula-rasa` and plan to open a PR back to `tabula-rasa`, because push workflows currently trigger only on `master`, `main`, and `tabula-rasa`.

  ```powershell
  git switch -c phase-20-e2e-quality-closure
  ```

  Expected output: `Switched to a new branch 'phase-20-e2e-quality-closure'`.

- [x] Verify toolchain availability without printing secrets.

  ```powershell
  go version
  docker version
  python --version
  gh --version
  ```

  Expected output: each command exits 0.

- [x] Verify live-gate environment names are present before paid gates are attempted.

  ```bash
  set -a
  . ./.env
  set +a
  test -n "${OPENROUTER_API_KEY:-}"
  test -n "${POSTGRES_PASSWORD:-}"
  test -n "${NEO4J_PASSWORD:-}"
  ```

  Expected output: no output and exit 0. If any check fails, record the exact missing variable name in the closure doc and stop before declaring E2E closed.

---

## Task 2: Repair the Quality Snapshot Gate

- [x] Add `scripts/quality_snapshot_gate.sh`.

  Required behavior:

  - `set -euo pipefail` and `cd "$(git rev-parse --show-toplevel)"`.
  - Read `docs/aura-quality-snapshot.md`.
  - Parse the main table with columns `Metric`, `Target`, `Last measured`, `Last value`, `Owner phase`, `CI gate path`.
  - Determine changed files from `git diff --name-only "$BASE"... "$HEAD"` where:
    - `BASE` defaults to `origin/${GITHUB_BASE_REF}` for pull requests.
    - `BASE` falls back to the merge-base with `origin/${GITHUB_REF_NAME}` or `HEAD~1` for local use.
    - `HEAD` defaults to `HEAD`.
  - Also support test overrides:
    - `AURA_QUALITY_CHANGED_FILES` as newline-delimited file paths.
    - `AURA_QUALITY_BASE_DATE` as an ISO date.
    - `AURA_QUALITY_SNAPSHOT` as an alternate snapshot file path.
  - A row matches when any changed file matches any glob listed in its `CI gate path` column.
  - A row is fresh when `Last measured` contains an ISO date greater than or equal to the base commit date.
  - If a matched row has no ISO date or is older than the base date, exit 1 with:

    ```text
    quality snapshot row '<metric>' stale - owner <owner phase> must re-measure and update before merge (amendment #20)
    ```

  - If the markdown table cannot be parsed, exit 2 with a clear parse error.
  - If no changed files match a row, exit 0 with `ok: quality snapshot gate found no matching measured rows`.

- [x] Add `scripts/quality_snapshot_gate_test.sh` or equivalent shell self-test.

  Required cases:

  - Unmatched changed path exits 0.
  - Matched row with no ISO date exits 1.
  - Matched row with old date exits 1.
  - Matched row with fresh date exits 0.
  - Malformed snapshot table exits 2.

  Verification command:

  ```bash
  bash scripts/quality_snapshot_gate_test.sh
  ```

  Expected output: final line `ok: quality snapshot gate tests passed`.

- [x] Wire the gate into `.github/workflows/ci.yml`.

  Recommended placement: `build-and-lint` after checkout and before expensive tooling.

  Verification command:

  ```bash
  bash scripts/quality_snapshot_gate.sh
  ```

  Expected output: exits 0 locally for the current branch or fails on rows that this phase must reconcile.

---

## Task 3: Create the E2E Closure Evidence Document

- [x] Add `docs/audit/e2e-closure-2026-06-11.md`.

  Required sections:

  - `Commit Under Test` with full commit SHA and branch.
  - `Environment` with OS, shell, Docker state, Go version, and whether WSL/Git Bash was used.
  - `CI-Backed Gates` with command, exit status, and key output line for each deterministic gate.
  - `Paid Live Gates` with command, exit status, runtime, and measured assertion values.
  - `Operator-Only Gates` with evidence source, operator account constraints, and result.
  - `Non-E2E Benchmark Rows` for memory-scale 100K recall/p95 if not executed.
  - `Final Closure Decision` listing closed gates and blocked gates.

- [x] Use this status vocabulary exactly:

  ```text
  PASS
  FAIL
  BLOCKED_EXTERNAL_DEPENDENCY
  NOT_E2E_FUTURE_BENCHMARK
  ```

  This prevents skip-as-green language from creeping back into the docs.

- [x] Update `docs/audit/audit-index.json` to include `e2e-closure-2026-06-11.md` in `documents`.

  Verification command:

  ```powershell
  python -m json.tool docs\audit\audit-index.json | Out-Null
  ```

  Expected output: no output and exit 0.

---

## Task 4: Run Deterministic and CI-Backed Gates

- [x] Run the P0/P1 closure regression set again on the current commit.

  ```powershell
  go test ./internal/agent ./internal/llm ./internal/runner ./internal/conversations ./internal/agui ./internal/cron ./internal/agent/workflow ./internal/agent/tools ./internal/skills ./internal/mcp
  ```

  Expected output: `ok` for every package.

- [x] Run the full unit race tier.

  ```powershell
  go test -race -count=1 ./...
  ```

  Expected output: `ok` for every package; no data race report.

- [x] Run cache and boundary gates.

  ```bash
  bash scripts/cache_invariant_audit.sh
  bash scripts/cache_invariant_negative_test.sh
  bash scripts/agui_boundary_check.sh
  bash scripts/ssrf_smoke.sh
  ```

  Expected output: each script exits 0; cache audit reports identical request hashes; the negative test proves drift fails; SSRF smoke reports `PASS`.

- [x] Bring up Postgres and run the DB-backed integration tier.

  ```bash
  set -a
  . ./.env
  set +a
  make db-up
  go run ./cmd/aura db migrate
  go test -tags db_integration -race -count=1 -p 1 ./internal/db/... ./internal/cron/... ./internal/agui/...
  ```

  Expected output: migrations are idempotent; tagged packages pass under race.

- [x] Run the web live tier against SearXNG.

  ```bash
  docker compose up -d searxng
  export SEARXNG_URL=http://127.0.0.1:18080/search
  go test -race -count=1 -tags web_integration ./internal/web/
  bash scripts/web_search_smoke.sh
  ```

  Expected output: web integration tests pass and `web_search_smoke` reports at least one ranked result.

- [x] Run the knowledge smoke.

  ```bash
  set -a
  . ./.env
  set +a
  make neo4j-migrate
  make smoke
  ```

  Expected output: `ok: recall@5 = 5/5, p95 = N ms` with `N <= 30` on the operator host. Record the measured `N`.

- [x] Run the skills deterministic tiers.

  ```bash
  set -a
  . ./.env
  set +a
  mkdir -p "${AURA_SKILL_EXPORT_DIR:-/tmp/aura-skills-export}" "${AURA_RUN_DIR:-/tmp/aura-run}"
  go test -race -count=1 ./internal/skills/ ./internal/agent/tools/ ./internal/cron/...
  go test -run '^$' -fuzz=FuzzSkillValidator -fuzztime=60s ./internal/skills/
  go test -tags db_integration -race -count=1 -p 1 ./internal/skills/ ./internal/cron/...
  go test -tags db_integration -race -count=1 -p 1 -run TestSnippetExec ./internal/skills/
  ```

  Expected output: all deterministic skill tiers pass; fuzz run completes with no failure.

---

## Task 5: Run Paid and Operator-Live E2E Gates

- [x] Run the basic live LLM smoke.

  ```bash
  set -a
  . ./.env
  set +a
  bash scripts/llm_smoke.sh
  ```

  Expected output: `llm_smoke: PASS` with non-zero token and USD footer.

- [x] Run runner live E2E.

  ```bash
  set -a
  . ./.env
  set +a
  export AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
  export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
  go test -tags live_e2e -run TestLiveE2E -timeout 600s -v ./internal/runner/
  ```

  Expected output: all `TestLiveE2E_*` scenarios pass.

- [x] Run KV cache live E2E.

  ```bash
  set -a
  . ./.env
  set +a
  go test -tags live_e2e -run TestKVCacheWarmingE2E -timeout 600s -v ./internal/eval/
  ```

  Expected output: `TestKVCacheWarmingE2E` passes; record the peak post-first-turn cache ratio and cached token evidence in the closure doc and quality snapshot.

- [x] Run CoT/tool-use live eval.

  ```bash
  set -a
  . ./.env
  set +a
  go test -tags cot_eval -run TestCoTEval -timeout 600s -v ./internal/eval/
  ```

  Expected output: `TestCoTEval` passes all asserted dimensions.

- [x] Run swarm live E2E if self-send channels are configured.

  ```bash
  set -a
  . ./.env
  set +a
  test -n "${AURA_EVAL_SELF_MAIL:-}"
  test -n "${AURA_EVAL_SELF_PHONE:-}"
  test -n "${AURA_EVAL_WA_CHAT_SELF:-}"
  go test -tags cot_eval -run TestSwarmE2E -timeout 600s -v ./internal/eval/
  ```

  Expected output: `TestSwarmE2E` passes with at least two workers, mail and WhatsApp read-back, timing below 1.5x, and judge score at least 0.90.

- [x] Run scheduler North-Star E2E.

  ```bash
  set -a
  . ./.env
  set +a
  export AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
  go test -tags cot_eval -run TestSchedulerNorthStarE2E -timeout 300s -v ./internal/cron/
  ```

  Expected output: natural prompt schedules the expected persisted `reminder` and `agent_job` rows.

- [x] Run AG-UI live E2E.

  ```bash
  set -a
  . ./.env
  set +a
  export AGUI_SMOKE_LIVE=1
  bash scripts/agui_smoke.sh
  ```

  Expected output: output contains `agui_smoke: PASS (leg=live, thread` and REASONING events appear before answer text.

- [x] Run the real chat xlsx gate.

  ```bash
  set -a
  . ./.env
  set +a
  bash scripts/chat-e2e-gate.sh
  ```

  Expected output: `VERDICT: PASS` and `SCORE: 6/6 = 100%`.

- [x] Run synthetic skills North-Star E2E as a structural guard.

  ```bash
  set -a
  . ./.env
  set +a
  docker compose up -d searxng
  export SEARXNG_URL=http://127.0.0.1:18080/search
  go test -race -tags cot_eval -run TestSkillsE2E -timeout 900s -v ./internal/eval/
  ```

  Expected output: `TestSkillsE2E` passes with judge score at least 0.90 and fresh xlsx artifact evidence.

- [x] Run snippet reuse live E2E.

  ```bash
  set -a
  . ./.env
  set +a
  docker compose up -d searxng
  export SEARXNG_URL=http://127.0.0.1:18080/search
  export AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
  export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
  go test -tags 'cot_eval db_integration' -run TestSnippetReuseE2E -timeout 540s -v ./internal/eval/
  ```

  Expected output: `TestSnippetReuseE2E` passes with `endEvents <= 6`, wall-clock under 40 seconds, and fresh xlsx artifact evidence.

- [x] Run Telegram/multimodal operator E2E if tokens and sidecars are configured.

  ```bash
  set -a
  . ./.env
  set +a
  test -n "${TELEGRAM_BOT_TOKEN:-}"
  test -n "${AURA_E2E_CHAT_ID:-}"
  docker compose up -d aura-stt aura-tts aura-ocr-vl markitdown
  bash scripts/telegram_e2e.sh
  ```

  Expected output: score is at least 90 and each response-asserted send/multimodal/setup check is recorded.

- [x] Run the DeepSeek reasoning exposure probe.

  ```bash
  set -a
  . ./.env
  set +a
  python scripts/deepseek_reasoning_probe.py --timeout 120
  ```

  Expected output: JSON has `"ok":true`, `"reasoning_exposed_non_stream":true`, `"reasoning_exposed_stream":true`, and `"adaptive_tiers_ok":true`.

---

## Task 6: Reconcile the Quality Snapshot

- [x] Update the KV cache row with the `TestKVCacheWarmingE2E` date and measured hit-rate evidence.

  Acceptance: row states the observed peak post-first-turn cache ratio and cites the exact command.

- [x] Update skills rows so the top matrix and detail table agree.

  Acceptance:

  - The real `chat-e2e-gate.sh` result is the closing user-surface score.
  - The synthetic `TestSkillsE2E` result is described as a structural guard.
  - `validator.go` and `writer.go` mutation wording matches the current `skills.yml` hard/advisory split.
  - No stale sentence says the skills E2E gate is unmeasured.

- [x] Update snippet reuse rows with current coverage and mutation status.

  Acceptance:

  - The live run records fresh `endEvents`, wall-clock, and artifact evidence.
  - Coverage status is either measured in this phase or explicitly blocked by the missing stack/tool.
  - Advisory acceptance for `writer_activate.go` survivors is written as a decision, not as uncertainty.

- [x] Update AG-UI, Telegram, MCP manager, and scheduler wording only where this phase produced fresh evidence.

  Acceptance: no row implies an operator-only live check ran in CI.

- [x] Handle GraphRAG and vector 100K rows honestly.

  Required decision:

  - If a real 100K benchmark harness exists by execution time, run it and record measured recall and p95.
  - If no real 100K harness exists, do not populate those rows from `make smoke`; move the E2E closure doc to `NOT_E2E_FUTURE_BENCHMARK` for those rows and create a follow-up action for a memory-scale benchmark phase.

- [x] Run markdown and JSON sanity checks.

  ```powershell
  python -m json.tool docs\audit\audit-index.json | Out-Null
  Select-String -Path docs\aura-quality-snapshot.md -Pattern "operator-run|pending|YYYY-MM-DD" -CaseSensitive
  ```

  Expected output: JSON command exits 0; text search returns only intentional historical context or nothing. Any current E2E row returned by the search must be edited before closure.

---

## Task 7: A-24 Quality Hardening Wave

Execute this wave after E2E evidence is stable. If time or external gates block Wave A, leave this wave for the next plan.

- [ ] Add a Windows shell-surface CI lane.

  Required workflow shape:

  ```yaml
  windows-shell-test:
    name: Windows shell surface
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with:
          go-version-file: go.mod
          cache: true
      - name: Assert Git Bash is available
        shell: pwsh
        run: |
          $bash = Get-Command bash -ErrorAction SilentlyContinue
          if (-not $bash) { throw "bash not found on windows-latest" }
          bash --version
      - name: Shell tool tests
        shell: pwsh
        run: go test -race -count=1 ./internal/agent/tools/ -run "TestShellExec|TestBackgroundShell|TestReadToolOutput|TestFsEdit"
  ```

  Verification: GitHub Actions job passes on `windows-latest`.

- [ ] Add typed `retryableStreamOpenError` tests.

  Target file: `internal/agent/llm_agent_stream_retry_test.go`.

  Required cases:

  - `net.Error` with `Timeout() == true` is retryable.
  - `*url.Error` wrapping timeout is retryable.
  - `*url.Error` wrapping `io.ErrUnexpectedEOF` is retryable.
  - Context cancellation is not retryable.
  - Known text markers remain covered.

  Verification:

  ```powershell
  go test ./internal/agent -run TestRetryableStreamOpenError
  ```

- [ ] Add MCP reconnect branch coverage.

  Target package: `internal/agent/mcptools`.

  Required cases from `docs/audit/testing-strategy.md`:

  - `ListTools` transport error reconnects once and refreshes specs.
  - `Close()` handles nil client.
  - Double-fault returns the second error without stale specs.
  - Reconnect whose post-open `ListTools` fails is surfaced.

  Verification:

  ```powershell
  go test ./internal/agent/mcptools -run "TestReconnect|TestClose"
  ```

- [ ] Fold duplicate `truncateTailBytes` helpers if still duplicated.

  Required behavior:

  - One shared helper covers `n <= 0`, exact boundary, and UTF-8 rune boundary advance.
  - `shell_exec` and completion critic both call the same helper.

  Verification:

  ```powershell
  go test ./internal/agent ./internal/agent/tools -run "Test.*Truncate|Test.*Digest|TestShellExec_StderrTailReservedUnderCap"
  ```

- [ ] Run loop-core mutation spot checks and record the result.

  Commands:

  ```bash
  go install github.com/avito-tech/go-mutesting/cmd/go-mutesting@latest
  "$(go env GOPATH)/bin/go-mutesting" internal/agent/llm_agent.go
  "$(go env GOPATH)/bin/go-mutesting" internal/agent/llm_agent_finalize.go
  "$(go env GOPATH)/bin/go-mutesting" internal/agent/tools/shell_exec.go
  "$(go env GOPATH)/bin/go-mutesting" internal/agent/mcptools/bridge_reconnect.go
  ```

  Acceptance: scores are recorded in `docs/aura-quality-snapshot.md`. Any score under 70 percent must have either a code fix or a documented equivalent-mutant rationale following the precedent in the scheduler and AG-UI rows.

---

## Task 8: Final Verification, Commit, Push, CI

- [ ] Run formatters and local high-signal checks.

  ```powershell
  gofmt -w internal
  go test ./internal/agent ./internal/agent/tools ./internal/agent/mcptools ./internal/llm ./internal/mcp
  bash scripts/quality_snapshot_gate.sh
  python -m json.tool docs\audit\audit-index.json | Out-Null
  ```

  Expected output: tests pass, gate exits 0, JSON parses.

- [ ] Review diff.

  ```powershell
  git diff --stat
  git diff -- docs/aura-quality-snapshot.md docs/audit/e2e-closure-2026-06-11.md docs/audit/audit-index.json scripts/quality_snapshot_gate.sh .github/workflows/ci.yml
  ```

  Expected output: no unrelated file churn.

- [ ] Commit with a message that reflects the phase outcome.

  ```powershell
  git add docs scripts .github internal
  git commit -m "close e2e quality evidence gate"
  ```

  Expected output: commit created.

- [ ] Push and monitor CI.

  ```powershell
  git push
  gh run list --branch "$(git branch --show-current)" --limit 10
  ```

  Expected output: CI, CodeQL, and Skills workflows complete successfully or any failure is triaged to a concrete external dependency.

---

## Self-Review Checklist

- [ ] No live gate is marked pass because it skipped.
- [ ] No E2E row in `docs/aura-quality-snapshot.md` contradicts `docs/audit/e2e-closure-2026-06-11.md`.
- [ ] The missing quality snapshot gate is now real and tested.
- [ ] The 5-document Neo4j smoke is not misrepresented as a 100K memory benchmark.
- [ ] Operator-only gates name their required credentials or paired accounts.
- [ ] The final commit contains only phase-related changes.
- [ ] GitHub Actions status is recorded in the final handoff.

## Execution Options

1. Recommended: execute Wave A first with `superpowers:executing-plans`, commit/push/CI, then decide whether to execute Wave B in the same branch.
2. Faster: execute only the documentation and quality gate repair, leaving paid live E2E runs for the operator.
3. Full closure: execute Wave A and Wave B, including paid live gates, Windows CI, and mutation evidence, before declaring Phase 20 closed.
