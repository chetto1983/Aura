---
phase: 46
slug: mcp-trust-and-facade
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-08-17
---

# Phase 46 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from `46-RESEARCH.md` §Validation Architecture. Task rows are filled by the planner.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + build tags (no third-party test framework) |
| **Config file** | none — tag-gated files under `internal/mcp/*_test.go`, `internal/agent/mcptools/*_test.go`, `internal/gateway/*_test.go` |
| **Quick run command** | `go test ./internal/agent/mcptools/... ./internal/gateway/...` (no tags — daemon-free unit tier) |
| **Full suite command** | `AURA_DB_URL=... AURA_DB_MIGRATE_URL=... go test -tags db_integration -p 1 ./internal/...` (mirrors `scripts/coverage_gate.sh`) |
| **Coverage gate** | `bash scripts/coverage_docker.sh` — owned-surface floor ≥85%, `db_integration` tag set only |
| **Estimated runtime** | ~15s quick tier; ~20 min full matrix |

---

## Sampling Rate

- **After every task commit:** `go test ./internal/agent/mcptools/... ./internal/gateway/...`, plus
  `go vet ./...`, `go build ./...`, and `go test -race ./internal/<touched package>/` per CLAUDE.md's
  post-edit validation rule. Mandatory after any edit to `bridge_risk.go`, `bridge.go`,
  `bridge_memory.go`, `classify.go`, `guard.go`.
- **After every plan wave:** `bash scripts/coverage_docker.sh` (full `db_integration` aggregate,
  85% floor) plus, with the stack up, `calendar_integration` / `whatsapp_integration` runs against the
  newly-pinned sidecar images.
- **Before `/gsd-verify-work`:** `make quality-full` green.
- **Phase gate (CLAUDE.md Definition of Done):** the live driven-conversation E2E (D-37) scored >9.8,
  evidence read from OTel traces and `aura.tool_invocations` — never inferred from test output.
  A green suite alone does not close this phase.
- **Max feedback latency:** ~15 seconds (quick tier).

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| _filled by planner_ | | | | | | | | | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

### Requirement → test map (from RESEARCH.md, pre-task)

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|--------------|
| MCP-01 | MCP descriptions carry no distrust prefix | unit | `go test ./internal/agent/mcptools/ -run TestFrameMCPDescription` | ✅ (confirm exact test name at plan time) |
| MCP-02 | Fail-closed default for unannotated/unlisted tool; approval gate; namespacing panic-on-duplicate; schema byte caps | unit | `go test ./internal/agent/mcptools/ -run 'TestClassifyToolRisk\|TestCapSchemaDescriptions'` | ✅ `bridge_risk_test.go` + byte-cap tests |
| MCP-03 | Trust unconditional across every mounted server | unit | `go test ./internal/agent/mcptools/ -run TestNewResult` | ✅ |
| MCP-04 (SC#1, SC#2) | Two always-loaded multiplexed tools; per-action classification survives the merge | unit + live E2E | `go test ./internal/gateway/ -run TestClassify` (extended); live: driven conversation + `aura.tool_invocations` | ❌ Wave 0 |
| MCP-05 (SC#4) | `accountId` never in dispatched args for calendar calls | integration + live E2E | `calendar_integration` tag test asserting no `accountId` on a detail call built from a prior list result | ❌ Wave 0 |
| TOOL-14 | Tiering axis (frequency + count budget) documented and enforced | unit | `go test ./internal/agent/mcptools/ -run TestDefaultDeferred` (new) | ❌ Wave 0 |
| SC#3 | Every mounted server's descriptions render as ordinary text | unit + live spot-check | same as MCP-01 | ✅ (unit) |
| SC#6 | A new unlisted server mounts with no code/config change, fail-closed at Mutating+Destructive | integration | `go test -tags calculator_integration ./internal/mcp/ -run TestCalculatorServerLive` | ✅ (extend with risk-tier assertion) |

**SC#5 is DELETED (D-07).** Do not author a test for it. A plan that tries to prove "results carry
instruction-shaped text and are not acted on" is reintroducing a criterion the operator struck.

---

## Wave 0 Requirements

- [ ] `internal/gateway/classify_multiplexed_comms_test.go` — the two new `multiplexedClassifiers`
      entries (calendar/messages): read action → Safe, mutate → Normal, destructive → Destructive,
      unrecognised action → Risky (fail-safe, mirroring `classifySkill`/`classifyTask`).
- [ ] `internal/agent/mcptools/bridge_deferral_test.go` — D-27's count predicate: ≤3 model-facing
      tools with budget available → not deferred; >3 → deferred; **2-slot global cap exhausted → a
      third individually-qualifying server stays deferred** (the case most likely to be missed).
- [ ] `internal/gateway/guard_test.go` (extend) — D-34's gate: a schema carrying an `action` property
      with NO `multiplexedClassifiers` entry does NOT get `Multiplexed: true` inferred and boots
      cleanly (proves SC#6's fail-closed-not-panic promise for a stranger's server).
- [ ] `internal/agent/mcptools/bridge_risk_test.go` (extend) — action-keyed
      `trustedRecipeActions[calendarRecipeSource]` / `[whatsAppRecipeSource]`, replacing the
      raw-tool-name-keyed cases; **keep `mcp.SourceRecipeMemory`'s raw-name keying unchanged (D-35)**.
- [ ] Mount-time reconciliation (D-33) — an unknown action in a mounted curated tool's `action` enum
      produces a WARN log naming it and does NOT panic boot.
- [ ] `calendar_integration_test.go` / `whatsapp_integration_test.go` (extend) — curated tool action
      count; for calendar, that no `accountId` argument is required by the detail call built from a
      prior list result (MCP-05/SC#4's only integration-tier assertion point).

Five of these six are daemon-free unit tests over already-daemon-free packages (`internal/gateway`,
`internal/agent/mcptools`) and therefore **do** feed the `db_integration`-only 85% coverage gate.
Only the last is daemon-gated (`*_integration` tag, requires the live sidecar) and feeds no coverage.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live manifest shows exactly two always-loaded multiplexed comms tools, not 28 raw ones | MCP-04 / SC#1 | Manifest composition is a runtime property of a specific turn; no unit test can assert what the model actually saw | Drive a real conversation on the running stack; read the rendered manifest from the OTel span / prompt-render log; independently call `tools/list` against the mounted sidecar and confirm the curated set matches |
| Destructive action gates while a read in the same merged tool does not | MCP-04 / SC#2 | End-to-end approval flow spans classifier, gateway, approval ledger and channel | In one conversation, call a read action and a destructive action of the same merged tool; quote both `aura.tool_invocations` rows (approval-pending vs completed-without-approval) |
| `accountId` absent from dispatched args on a live calendar detail call | MCP-05 / SC#4 | Requires a real model turn following an event from listing through to detail | Drive the list→detail sequence live; inspect `aura.tool_invocations.args_raw` for the detail call |
| Descriptions render as ordinary text in a live turn | MCP-03 / SC#3 | Confirmatory visual read; the pure function is already unit-covered | Read the rendered tool descriptions in a live turn transcript |
| Mounting a new server needs no Aura code change | SC#6 | A process/documentation assertion, not a code property | Per D-38's caveat, the evidence must show the MOUNT needed nothing new — the server IS referenced in Aura's tree via the test fixture, so narrate that distinction explicitly |
| Fork image `:<sha>` pin matches the branch the curated `tools/list` was built from | D-23 | Cross-repo; CI cannot check across the repo boundary | Compare the pinned digest in `compose.yaml` against the fork commit that produced it |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 15s (quick tier)
- [ ] Live driven-conversation E2E (D-37) run and scored >9.8, with `aura.tool_invocations` rows quoted here
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
