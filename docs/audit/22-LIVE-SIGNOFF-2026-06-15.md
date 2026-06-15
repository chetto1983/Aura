# Phase 22 — Agent Perimeter Hardening: Sign-Off Evidence

> **Plan:** 22-05 (phase close-out) · **Date:** 2026-06-15
> **Branch:** master · **Evidence HEAD:** `036575b5` (after the 22-05 ledger commit)
> **Operator:** Davide (dvdmarchetto@gmail.com)
> **Finding ledger:** [`22-finding-ledger.md`](22-finding-ledger.md) — every AG-001..064 disposed.

This document has **two parts**, kept strictly separate (CLAUDE.md no-skip-as-green
is binding — nothing below is a fabricated pass):

- **Part A — AUTOMATED EVIDENCE (DONE):** commands actually executed in the master
  working tree during 22-05, with their real output / exit status.
- **Part B — PENDING OPERATOR SIGN-OFF:** the destructive coverage gate, the WSL
  quality bar (lint/vuln/mutation), and the full live-stack pass. These are
  **operator-coordinated** (a Postgres-wiping gate + a live multi-service daemon)
  and were deliberately **not** run from this checkout — parallel Codex/superpowers
  sessions are active on this machine and a coverage-gate PG wipe would corrupt their
  work. Each item lists the exact command + acceptance criteria; status: `pending`.

---

## Part A — AUTOMATED EVIDENCE (executed 2026-06-15, HEAD `036575b5`)

All commands run from `D:\Aura` on `go version go1.26.4 windows/amd64`.

| # | Command | Result | Notes |
|---|---------|--------|-------|
| A1 | `go build ./...` | **PASS** (exit 0) | Whole module builds clean. |
| A2 | `go vet ./...` | **PASS** (exit 0) | No vet diagnostics. |
| A3 | `go test ./...` (untagged, no DB) | **1 FAIL, rest PASS** | The **only** failing package is `cmd/aura`, solely `TestProductionContainerArtifactsMatchFatImageContract` — a **pre-existing, unrelated** compose drift (see below). Every `internal/...` package passes, including the 22-05-touched `agent`, `agent/tools`, `agent/mcptools`. |
| A4 | `go test -race ./internal/agent/... ./internal/swarm/...` | **PASS** (exit 0) | Race-clean across `agent`, `agent/agenttest`, `agent/mcptools`, `agent/prompt`, `agent/tools`, `agent/workflow`, `swarm`. Toolchain: `BASH_ENV=~/.aura-toolchain.sh` (Windows binutils-shadow workaround). |
| A5 | `bash scripts/cache_invariant_audit.sh` | **PASS** (exit 0) | In-memory `FakeClient`, no Postgres. 22 identical `messages[0]`, `messages[1]` (profile/skills), and skill manifest-in-Description hashes — KV-cache stable-prefix discipline holds **after** the 22-05 skill-schema edit (the schema feeds the skill tool Description in `messages[0]`). |

### A3 detail — the single failure is pre-existing and out of scope

```
--- FAIL: TestProductionContainerArtifactsMatchFatImageContract (0.00s)
    container_artifacts_test.go:92: compose.yaml missing
    "AURA_LLM_MODEL: ${AURA_LLM_MODEL:-deepseek/deepseek-v4-flash:exacto}"
FAIL    github.com/chetto1983/aura/cmd/aura
```

This is the `:nitro` vs `:exacto` compose-vs-test drift introduced by commit
`136325dc` (default `AURA_LLM_MODEL`). It was already documented as out-of-scope in
22-03 and 22-04, and is logged in
`.planning/phases/22-bug-fix/deferred-items.md`. **No file in plan 22-05 touches
`compose.yaml` or `cmd/aura`**, so it is left for a compose/container fix plan — not
masked, not fixed here.

### A4 detail — passing packages

```
ok  internal/agent            19.410s
ok  internal/agent/agenttest   1.216s
ok  internal/agent/mcptools    1.304s
ok  internal/agent/prompt      1.260s
ok  internal/agent/tools       7.811s
ok  internal/agent/workflow    1.248s
ok  internal/swarm             3.492s
```

### A5 detail — cache invariant output

```
ok (cache invariant gate): 22 identical messages[0] hashes (0daddf93…21332b1)
ok (cache invariant gate): 22 identical messages[1] profile/skills hashes (69a5c1b0…2a4c14537)
ok (cache invariant gate): 22 identical skill manifest-in-Description hashes (4ec31b47…91912889)
```

### Per-wave automated proof already on record

Each AG-### regression test is named in [`22-finding-ledger.md`](22-finding-ledger.md)
and was shown fail-before/pass-after in the wave SUMMARYs
(`22-01`..`22-04`). The 22-05 additions:
`TestSkillSchemaIsHonestNotDishonest`, `TestMountManagedServer_HTTPBranchInfersFromBareURL`,
`TestMountManagedServer_StdioBranchFailure`,
`TestToolInvocation_ForensicShapeIsRawRedactionRoutedToStore` — all PASS in A3/A4.

---

## Part B — PENDING OPERATOR SIGN-OFF (NOT run in this checkout)

> **Why pending:** these items either **wipe the shared Postgres** (the coverage
> gate runs the migration Reset down/up by design) or need the **WSL quality
> toolchain** (`~/go/bin`, not on this shell's PATH) or a **full live multi-service
> daemon**. Running them here would corrupt concurrent sessions or cannot complete
> deterministically. The operator runs them at close, on a quiet machine, and ticks
> the boxes. **No pass below is asserted** until the operator records the real output.

### B1 — Destructive coverage gate (≥85% owned-surface)  · status: `pending`

```bash
# WSL, stack UP (make neo4j-migrate), shared PG will be RESET + WIPED by design:
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
export POSTGRES_PASSWORD=...   # from .env
export AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable"
make coverage          # → scripts/coverage_gate.sh, owned-surface floor ≥85% (AURA_COVERAGE_MIN)
```

**Acceptance:** owned-surface coverage **≥85%** across the full `db_integration
neo4j_integration` tag matrix; every owned package ≥85% (the 2026-06-13 baseline was
90.3% combined). The 22-05 changes are small (one deleted const + two test swaps + one
new test) and do not lower any package below its prior floor; the operator confirms the
number on the live stack.

### B2 — WSL quality bar (lint · vuln · mutation)  · status: `pending`

```bash
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
golangci-lint run ./...          # 0 issues
govulncheck ./...                # 0 actionable CVEs
# Mutation spot-check ≥70% killed on the three critical files:
go-mutesting --tags 'db_integration' ./internal/agent/llm_agent_parallel.go
go-mutesting ./internal/agent/budget_dedup.go
go-mutesting --tags 'db_integration' ./internal/agent/mcptools/bridge_reconnect.go
# (PASS = killed, FAIL = survived; score = killed/total)
```

**Mutation targets (per 22-05 plan Task 4):**
`internal/agent/llm_agent_parallel.go`, `internal/agent/budget_dedup.go`,
`internal/agent/mcptools/bridge_reconnect.go`.
**Acceptance:** golangci-lint = 0; govulncheck = 0 actionable; mutation **≥70% killed**
on each of the three files, or a documented near-equivalent-survivor autopsy.

### B3 — Full live-stack sign-off  · status: `pending`

Bring up the live stack and exercise the hardened perimeter end-to-end:

```bash
# Stack: PG, Neo4j, mcp-neo4j-cypher, embed sidecar (granite), SearXNG (socat bridge
# SEARXNG_URL=127.0.0.1:18080), + the multimodal sidecars for the OCR pass.
make neo4j-migrate
# Build a fresh binary at HEAD in an isolated worktree (dodge parallel-Codex compile breaks):
#   set -a; source <(tr -d '\r' < .env); set +a
aura serve   # daemon: Telegram + AG-UI + scheduler from one process
```

**Acceptance criteria (map to D-01..D-04a / the ledger rows):**

| Live check | Maps to | Expected |
|------------|---------|----------|
| host `aura chat` tool trace (`· <toolname>`) | AG-001/052 | a panicking/oversized tool surfaces as a per-call error, never a daemon crash; swarm-child output enveloped untrusted |
| `/metrics` scrape | AG-012/013/031 | `aura_agent_turn_total{outcome}`, `llm_call_duration_seconds`, `llm_errors_total`, `tool_errors_total`, `hook_total`, token/cost, `panic_total{site}`, `prefix_drift_total`, `span_export_failures_total` all present |
| CDP Telegram round-trip | AG-004/052 | a real operator turn answers; hook fault contained; no swallowed error |
| GLM-OCR multimodal pass | AG-014/AG-003 (D-03) | a large image/file empirically exercises `AURA_FS_MAX_READ_BYTES`=10 MiB (stat-then-reject + paging hint) |
| MCP timeout / reconnect under a flapping server | AG-005/006/007 (D-06/D-08) | `=0`→default 60s, `-1`=infinite; reconnect single-flight off-lock, breaker after 3 fails, 30s cooldown, 500ms→30s backoff, 10s reconnect timeout — daemon stays responsive |
| reasoning router with embed sidecar down | AG-008 (D-07) | static `ReasoningTierLow` fallback, no ≤8s per-turn router cliff; router stays on + bounded (2s) for the no-classifier path |
| skill self-extension live | AG-011 | `always:false` create activates in-container after validate+audit; an operator alert/audit record is observable; `always:true`/delete stay approval-gated |
| DSN secret boundary | AG-010/047 | `cat $AURA_DB_URL` in a `shell_exec` child cannot read the composed DSN; output redactor masks `postgres://u:p@h` |
| tool-invocation ledger redaction | AG-034 | a secret placed on a `shell_exec` command line lands `[REDACTED]` + capped in `aura.tool_invocations` |

**Output to record at sign-off:** for each row, a ground-truth assertion that does NOT
look at `r.Reply` (a DB row, the `· <toolname>` trace, a `/metrics` line, or a rendered
body), plus a visual body print + mojibake/structure scan — mirroring the Phase-19
sign-off pattern. Append the observed evidence under each row and flip `status: pending`
→ `status: signed-off` with the operator + date.

### B4 — MCP timeout migration note (D-06)

`AURA_MCP_CALL_TIMEOUT_SEC` semantics changed in 22-03 and ship with this phase:

- unset / `0` → **default 60s** call timeout (previously `0` disabled the timeout → hang).
- `-1` → **explicit infinite / no-deadline** call.
- `< -1` or malformed → bridge/mount **fails before** any tools register (fail-loud).

Operators with `AURA_MCP_CALL_TIMEOUT_SEC=0` in their env relied on the old "no
timeout" meaning; after this phase that is the 60s default. Set `-1` to restore an
unbounded call. (Recorded here for the close-out per D-06.)

---

## Sign-off ledger

| Part | Status | Evidence |
|------|--------|----------|
| A — automated (build/vet/test/race/cache) | **DONE** | this doc Part A, HEAD `036575b5` |
| B1 — coverage ≥85% | `pending` | operator runs `make coverage` on the live stack |
| B2 — lint/vuln/mutation | `pending` | operator runs the WSL quality bar |
| B3 — full live stack | `pending` | operator runs `aura serve` + the acceptance matrix |

Gate-3 close requires Part B signed off by the operator. Part A is the automated
floor and is green (modulo the documented pre-existing compose-drift test, out of
scope).
