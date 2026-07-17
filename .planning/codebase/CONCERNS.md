# Codebase Concerns

**Analysis Date:** 2026-07-17

> **How to read this document.** Every entry is labelled with its epistemic status. This matters more
> than usual here: the project's own `docs/audit/risk-register.md` (2026-06-21) still marks **all 36
> risks `Open`**, but phases 31-37 closed many of them and *the register was never updated*. Acting on
> that register verbatim would send you to fix already-fixed code. Every claim below was re-verified
> against source **in this session** and cites `file:line`.
>
> | Label | Meaning |
> |-------|---------|
> | `[OPEN]` | Known, catalogued, verified still present in code today |
> | `[DEFERRED]` | Deliberate, documented decision — a gap to know, not to re-litigate |
> | `[IN-FLIGHT]` | Being built right now; risk is real but not yet shipped |
> | `[NEW]` | Observed in this session, not in any existing audit |
> | `[DISCREPANCY]` | Roadmap and code disagree — both cited |
>
> **Milestone context.** v0.0.0 (Ph 0-21) and v1.0.0 (Ph 22-30) shipped. v2.0.0 is at **14/19 phases,
> 109/122 plans (74%)** (`.planning/STATE.md:1-14`). The 2026-06-21 audit self-scored production
> readiness **4.6/10** (`docs/audit/README.md:36`); **Phase 41 (honest-10/10 closeout) is still OPEN**,
> so 10/10 is a *target, not a status*.

---

## Do NOT resurrect these — verified CLOSED

The risk register says `Open`; the code says otherwise. Confirmed fixed in this session:

| ID | Register claim | Verified reality |
|----|----------------|------------------|
| R-002 | `.env.example` disables destructive shell gate | `internal/config/config_validate.go:214-216` — `off` is a **Fatal** violation under `server_production` |
| R-003 | Terminal `text_response` runs after mutating siblings | `internal/agent/llm_agent_dispatch.go:45-52` — whole mixed step rejected before *any* sibling executes |
| R-008 | Listener failure hidden by healthcheck | `/readyz` implemented (`cmd/aura/serve.go:372-376`) and wired into the Compose healthcheck (`compose.yaml:226`) |
| R-011/R-026 | Background shell jobs lack TTL / predictable IDs | `internal/agent/tools/shell_bg.go:27-28,47-49` — 1h default TTL reaper + `(identity,session)` owner binding (MUSR-03/04) |
| R-012 | `fs_write` non-atomic | `internal/agent/tools/fs.go:123-152` — `atomicWriteFile` temp+`os.Rename`; reused by `fs_edit.go:88` |
| R-020 | Permissive CORS production misconfig | `internal/config/config_validate.go:190-199` — `gateCORS` Fatal under **both** strict tiers |

**The register itself is the tech debt.** It is a 4-week-stale artifact that reads as current. See
*Documentation rot* below — this is the same root cause.

---

## Tech Debt

### `[OPEN]` Tool `Mutating` classification is incomplete — and the runtime knows it

- **Issue:** `skill`, `task`, and `swarm_spawn` have real side effects but are **not** flagged
  `Mutating`. The agent loop compensates by treating the flag as untrustworthy rather than by fixing
  the classification.
- **Files:** `internal/agent/llm_agent_dispatch.go:40-42` — the code documents its own gap verbatim:
  *"the Mutating flag is untrustworthy while skill/task/swarm_spawn stay unflagged (a Phase-35
  classification-hardening gap), so 'allow read-only siblings' is unsafe"*.
- **Impact:** The terminal-exclusivity guard is deliberately **classification-independent** — it
  rejects the *whole* mixed step rather than trusting the flag. Safe today, but it means the flag
  cannot be relied on by any *future* consumer (audit ledger, policy engine, dedup) without first
  closing this. A new gate that trusts `Mutating` would silently under-enforce on three tools.
- **Fix approach:** Flag the three tools, then relax the blanket rejection to the precise rule. Do
  **not** relax the guard first — the guard is currently the only thing holding.

### `[OPEN]` Documentation rot, propagating between documents

Verified stale **on disk today**:

| File | Claim | Actual (measured today) |
|------|-------|-------------------------|
| `docs/CODEBASE_MAP.md:9,22` | "49 packages" | **68** (`go list ./internal/...`) |
| `docs/decision-v2-hardware-model-deployment.md:84` | "Granite-embedding **97M** multilingue **384-dim** (`aura-llama-embed`, **CPU**)" | **granite-embedding-311m**, **768** dims, **GPU** (`compose.yaml:79,258,436-438`; `internal/knowledge/migrations/0001_init.cypher:12`) — wrong on all three axes |

`docs/CODEBASE_MAP.md:9` is darkly instructive: it grounds its authority on being generated from the
tree *"not the planning docs (**which are stale**)"* — and has itself gone stale (generated
2026-06-15).

- **Root cause `[NEW]`:** **counts and status rot because no gate checks them.** The only doc-touching
  gates are `scripts/quality_snapshot_gate.sh` / `_prepush.sh`, and they check **freshness dates**,
  not **factual claims**. Every *mechanical* claim (scripts, flags, pinned versions) stayed accurate
  precisely because CI executes those. **What CI runs, stays true; what only humans read, rots.**
- **Impact:** These docs are agent-facing context. A wrong embedding dim routes a real investigation
  at a non-problem.
- **Fix approach:** Either generate the counts (a `make docs-verify` asserting package/migration
  counts) or stop stating them. A number no gate checks is a liability, not documentation.

### `[DISCREPANCY]` `CLAUDE.md` migration floor — fixed but **UNCOMMITTED**

- **Was:** `CLAUDE.md` claimed *"11 migrations shippate 0001-0011"*; the real floor is **0040**
  (`internal/db/migrations/0040_shared_links.up.sql`, 40 migrations / 80 files).
- **Why it was dangerous:** `CLAUDE.md` is injected into **every** agent session. An agent trusting it
  would create a colliding migration **and edit a shipped one**. Not theoretical — Phase 37F's own
  research doc hit exactly this: `.planning/phases/37F-.../37F-RESEARCH.md:15` — *"Migration 0036 is
  TAKEN… **This is the exact numbering trap CLAUDE.md warns about**"*, and `:866` notes a blind 0036
  *"dirties the tracker → every subsequent migration is blocked"*.
- **Status:** **Fixed in the working tree, NOT committed** (`git diff CLAUDE.md`). The fix replaces the
  count with the imperative rule *"il numero non si deduce: `ls internal/db/migrations/ | tail -1`"* —
  the right fix, because it removes the rotting number instead of refreshing it.
- **Action:** **Commit it.** An uncommitted fix to the file that configures every agent session
  protects nobody but the current working tree.

### `[OPEN]` Carry-over debt ledger (from `.planning/v2.0.0-MILESTONE-AUDIT.md:44-68`)

| ID | Item | Verified |
|----|------|----------|
| WR-02 | A typo'd `AURA_PROFILE` fail-**opens** to dev at boot — documented D-03 tradeoff | audit `:53` |
| WR-05 | A malformed custom `AURA_SHELL_DESTRUCTIVE_PATTERNS` regex passes `config validate` but breaks the shell tool **at runtime** — the gate does not compile-check the pattern list | audit `:54` |
| — | `assets.Status{Created,Embedding,Canceled}` kept-but-unwired; a designed 12-state pipeline **not scheduled in any v2.0.0 phase** | audit `:50` |
| — | `cmd/aura` gateway-hook logic at **39.9%** untagged coverage, excluded from the floor by the `cmd/aura` glue carve-out | audit `:61` |
| SEC-08 | CodeQL `go/request-forgery` resolved by **dismissal-as-FP**, not a literal fix (guard *is* implemented; CodeQL can't model `DialContext` classify as a sanitizer) | audit `:47` |

### `[OPEN]` No `SECURITY.md` for the sandbox phase

`/gsd-secure-phase` retro-verification is absent for Phase 37 (`v2.0.0-MILESTONE-AUDIT.md:68`) — the
phase closing **F-001, the headline finding**. Recommended before the Phase-41 closeout.

---

## Known Bugs

**None open that I can verify.** The 2026-06-21 audit reported **P0: 0** (`docs/audit/README.md:40`),
and its P1 correctness findings I sampled (R-003 terminal exclusivity, R-012 atomic writes) are fixed.

`[NEW]` **Zero orphan `TODO`/`FIXME`/`HACK` in non-test code** — a repo-wide grep over
`internal/**/*.go` + `cmd/**/*.go` returns only `cmd/aura/shell_test.go:12,41`, and those are a *test
asserting a TODO placeholder is gone*. The "no TODO orphan" rule (CLAUDE.md Gate 2) genuinely holds.
**The concerns in this codebase are architectural and infrastructural, not littered in the source.**

---

## Security Considerations

### `[OPEN]` `docker_integration` has NO CI job — the most security-critical subsystem is the least CI-tested

**This is the single most important entry in this document.**

- **Risk:** `grep -rn "docker_integration" .github/workflows/` → **zero matches** (workflows present:
  `ci.yml`, `codeql.yml`, `release.yml`, `skills.yml`). The coverage gate runs exactly
  `db_integration neo4j_integration` (`scripts/coverage_gate.sh:25`).
- **Consequence:** 6 of 15 `usersandbox` test files are `//go:build docker_integration` —
  `docker_backend_integration_test.go`, `egress_integration_test.go`, `lifecycle_integration_test.go`,
  `reap_integration_test.go`, `bench_soak_test.go`, `main_test.go`. They **compile and skip in CI, and
  contribute ZERO coverage**. `internal/sandbox/usersandbox` (1,545 LOC non-test) is **not** excluded
  from the 85% owned-surface floor (`scripts/coverage_gate.sh:9-14` excludes only sqlc / `agenttest` /
  `llm/client.go`), so its daemon-gated runtime counts as *uncovered*.
- **Proven cost — WR-01:** the CAP_NET_ADMIN cap-assertion bug stayed latent because its only assertion
  lives in a docker-gated file: `internal/sandbox/usersandbox/egress_integration_test.go:119` — *"Cap
  placement (D-07): NET_ADMIN on the sidecar, NEVER on the box."* The invariant it guards
  (`egress.go:28,71,79`; `docker_backend.go:7,123` — sidecar-only CAP_NET_ADMIN) is a **container-escape
  boundary**, and **no CI run ever executes that assertion.**
- **Current mitigation (partial, and worth crediting):** 9 untagged test files (`spec_test.go`,
  `translate_test.go`, `router_test.go`, `materialize_test.go`, `egress_test.go`, …) cover the *pure*
  logic per the CLAUDE.md daemon-free-unit-test rule. The gap is the **runtime**
  (lifecycle/exec/egress), not the builders. Phase 37's live tiers were verified **off-CI**, by hand,
  on `casaserver` native dockerd 2026-07-08 (`v2.0.0-MILESTONE-AUDIT.md:42`).
- **Recommendation:** a `docker_integration` CI job on a native-Linux runner. Until one exists, every
  sandbox-boundary invariant is protected by *operator memory*, not by the pipeline. This is the exact
  failure mode CLAUDE.md warns about ("skip-as-green"), reproduced at the *job* level rather than the
  test level: the tier isn't falsely green — **it doesn't exist**.

### `[DEFERRED]` RLS is permissive-on-**unset**, fail-closed-on-**mismatch**

- **Risk:** `internal/db/migrations/0032_*.up.sql:14-21,37-48` — policy is
  `USING (NULLIF(current_setting('app.current_identity', true),'') IS NULL OR identity_id = …)`. A
  writer that never sets the session var sees **every row**.
- **Deliberate:** documented as D-06/D-07 in the migration itself, so runner/CLI/Telegram write paths
  keep working. The storage backstop only bites on `WithIdentityTx`-wrapped surfaces; unwrapped paths
  rely on the app-level `GetForIdentity` owner-gate **first** (`v2.0.0-MILESTONE-AUDIT.md:58`).
- **Fix approach:** tighten to fail-closed-on-unset once every writer sets `app.current_identity`.
- **Action for any new work:** **flag any NEW write path that skips `WithIdentityTx`** — it is
  defended by exactly one layer, not two.

### `[DISCREPANCY]` SEC-09 (CodeQL weak-hash) appears **already remediated**, but is tracked as open

- **Roadmap says:** `.planning/ROADMAP.md:692` lists as Phase-40 success criteria *"The high CodeQL
  `go/weak-sensitive-data-hashing` finding at `internal/agui/recovery_hash.go` is remediated with a
  strong salted KDF"*; `.planning/REQUIREMENTS.md:139` has it `[ ]` unchecked.
- **The code says:** `internal/agui/recovery_hash.go` already uses **argon2id** (`:17` version const,
  `:72-81` `hashArgon2id` with `crypto/rand` 16-byte salt, m=64MB/t=1/p=4, `:89` constant-time compare).
  The only SHA-256 left is `HashLookupToken:66-70`, explicitly scoped *"hashes only **high-entropy
  random reset tokens**, not user-chosen secrets"* — which is the **correct** pattern; a KDF is
  unnecessary for high-entropy input.
- **History:** `97d80d071 "fix(auth): harden recovery secret hashing"` landed **2026-06-28** — *before*
  Phase 40 was scheduled. The fix shipped during the v1.0.0 hardening push and **nobody closed the
  requirement**.
- **Unverified — stated explicitly:** I **cannot** confirm from this repo whether the CodeQL alert
  itself resolved to `fixed`. It may still fire on `HashLookupToken` as a false positive — the same
  class as SEC-08's dismissal.
- **Implication:** Phase 40's SEC-09 is likely a **verification task (~minutes)**, not an
  implementation task. Someone planning Phase 40 off the roadmap alone would budget for a KDF rewrite
  that is already written.

### `[OPEN]` MCP governance — Phase 38, unbuilt

`R-009/R-014/R-021/R-027..R-032` (`docs/audit/risk-register.md:13-14,25,31-36`) remain unaddressed:
mixed `url`+`command` transport-trust ambiguity (P1), empty-body trust defaulting to `trusted_local`,
uncapped stdio frames, unbounded mount, CLI writes bypassing the audited writer. Scope confirmed at
`.planning/ROADMAP.md:99,656-668`. I did **not** re-verify each against code — treat as *catalogued,
unverified-today*.

### `[IN-FLIGHT]` Phase 37F public share links — a *deliberate, mitigated* hole in MUSR isolation

- **Status (measured today, more advanced than some docs suggest):** **6 of 19 plans committed**
  (37F-01..06 have SUMMARY.md; `1f1be990a docs(37F-06)` is HEAD), plan 07 next.
  `.planning/STATE.md:6` `stopped_at: Completed 37F-06-PLAN.md`.
- **What exists:** migration `0040_shared_links` **is on disk and shipped**, with the tier/expiry/
  token-hash invariants **DB-enforced** (`0040_shared_links.up.sql:8-13` — *"a public link with no
  expiry, or with no token hash, cannot exist even if a Go bug tries to write one"*; `token_hash bytea`
  holds SHA-256 only, no plaintext, D-13). Domain logic in `internal/share/`: `snapshot.go`,
  `redact.go`, `token.go`, `expiry.go`, `markdown.go`, `jsonfmt.go` + property tests.
- **What does NOT exist:** **no HTTP route.** `grep "/api/share|/s/|shared_links"` over
  `internal/agui/*.go` + `cmd/aura/*.go` → **no matches**. `expiry.go`/`expiry_test.go` are still
  untracked.
- **Read this precisely:** the public tier is **NOT a shipped hole** (unreachable — no route) and
  **NOT shipped functionality** (don't plan against it). It is design-complete, DB-enforced, and
  half-built. Seven fail-closed mitigations are recorded in **ADR 0039** (indexed at
  `.planning/graphs/GRAPH_REPORT.md:11077` with Residuals A-…; referenced
  `.planning/ROADMAP.md:610,654`). `prd.md:3007` records the three tiers: file export (ownership only),
  internal revocable link (default), public opt-in expiring opaque token (**never** the default, minted
  only behind an explicit warning).
- **Known residual (per ADR 0039):** *revoke and expiry do not reach copies already made.* Inherent to
  sharing; accepted, not overlooked.

---

## Performance Bottlenecks

**None verified in this session.** The 2026-06-21 audit reported no P0 and did not surface a measured
performance defect; `docs/aura-quality-snapshot.md` (updated 2026-07-11) carries the live metrics of
record. **I did not run benchmarks — writing a speculative bottleneck here would cost someone a real
investigation.** For measured numbers use the quality snapshot, not this document.

---

## Fragile Areas

### `[OPEN]` `internal/sandbox/usersandbox` (1,545 LOC non-test)

- **Why fragile:** the newest security-critical subsystem, with its runtime invariants asserted **only**
  under a build tag **no CI job runs** (see the `docker_integration` entry).
- **Safe modification:** when touching daemon/container-gated code you **must** add daemon-free unit
  tests for the pure logic (spec/tar builders, path-traversal + symlink guards, nil/disabled early
  returns, structural "not supported" errors) — otherwise the aggregate silently drops below the 85%
  floor and CI fails ~20 min after push. Verify locally with `bash scripts/coverage_docker.sh`.
- **Test coverage:** pure logic covered (9 untagged files); lifecycle/exec/egress runtime **zero in CI**.

### `[OPEN]` The coverage gate is a loaded footgun locally (mitigated, worth knowing)

`scripts/coverage_gate.sh:28-30` documents it: the `db_integration` tier **TRUNCATE/DELETEs shared auth
tables** on setup. On **2026-07-10 it wiped the live deployment's auth tables** (operator identity +
`authula`, no backup). Now mitigated on two layers — `coverage_docker.sh` provisions a **disposable**
`aura_cov` DB and drops it on exit, and `coverage_gate.sh` **refuses** `db_integration` against a DB
named `aura` when `GITHUB_ACTIONS` is unset. **Do not defeat either guard.**

---

## Scaling Limits

`[OPEN]` `R-017` single-replica Garage topology (`docs/audit/risk-register.md:21`) — production
validation + topology docs pending. Not re-verified today.

`[DEFERRED]` **Host-gated live tiers** (`v2.0.0-MILESTONE-AUDIT.md:39-43`, "SBX-REL-03-LIVE-TIERS",
operator force-close 2026-07-08). Still unrun, **infra-gated, NOT code**:
- D-14 **32GB concurrency soak** (WSL capped at 15.47GiB — appliance-only)
- **gVisor `runsc`** smoke
- native `-race` (green on WSL)
- FQDN-allowlist egress image

Must run on a native-Linux 32GB appliance **before** the Phase-41 honest-10/10 closeout.
`.planning/ROADMAP.md:115` flags that Phase 41's load/chaos + DR drills may carry the same
deferred-tier pattern pending an adequate host (DGX Spark). `.planning/STATE.md` records sandbox
box-mode enablement deferred to the native-Linux mini-PC (Docker Desktop egress/gVisor unsuitable).

---

## Dependencies at Risk

**None identified.** Go 1.26.5; `.planning/STATE.md` records the 1.26.4→1.26.5 bump (`7e257d64`)
clearing GO-2026-4970 + the crypto/tls CVE, **govulncheck clean, CI green**. `make vuln` →
`govulncheck ./...` runs as the CI `vulncheck` job.

`[OPEN]` Supply-chain **hardening** is still Phase-40 work (`.planning/ROADMAP.md:691`): SBOM
publication, `govulncheck` **blocking** on high severity, and **SHA-pinning all Actions** are success
criteria, i.e. not yet met.

---

## Missing Critical Features

Open v2.0.0 scope (`.planning/ROADMAP.md:99-108`) — these are *planned*, not defects:

| Phase | Missing | Findings covered |
|-------|---------|------------------|
| **38** | MCP Governance Hardening (`MCPH-01..09`) | F-013/014/027/033/034/035/037/038/046 |
| **39** | Idempotency + Observability Pack (`OBS-01..06`) — OTel metrics, alert YAML + Grafana JSON validated in CI, sidecar/trace retention, bounded learning-store cap | F-008/017/020/023/024/049 |
| **40** | Security & Supply-Chain Pack (`SEC-01..06,09`) — prompt-injection denial regression suite under `server_production`, secret redaction before persistence, SBOM | F-019-sec/021/022/047/051/052 |
| **41** | Production Ops + Capability-Eval + **Honest 10/10 Closeout** (`OPS-01..06`, `REL-01..03`) — drilled DR restore with measured RPO/RTO, scheduler drain vs. systemd stop budget, load+chaos harness in CI, evidence bundle | F-019/025/042/043 |

`[DEFERRED]` From `.planning/STATE.md` "Deferred Items" — **not concerns, recorded for honesty**:

| Item | Rationale |
|------|-----------|
| `LLM-V2-01` vLLM + LMCache (Slice 13) | GPU-gated, DGX Spark bundle path |
| `SKILL-V2-01` cross-conv cluster auto-suggest (7f) | Amendment #13 scope reduction |
| `SWARM-V2-01` full N-deep + DM-by-ID + tier-mapped | Amendment #12 scope reduction |
| Phase 30 — 4 GPU live tiers (rerank/graphrag/retrieval-eval/document-ingest) | Unrunnable on a 4GB-GPU host |
| Phase 37A HV#2 — live Telegram `send_file` round-trip | ~30s operator action; unit regression proves the consumer path |
| Phase 37C — audible-TTS + live-mic perceptual checks | No automated tier, incl. CI, can cover perceptual quality |

---

## Test Coverage Gaps

**Aggregate is strong** — 90.3% owned-surface (re-measured 2026-06-13, full `db_integration
neo4j_integration` matrix), every owned package ≥85% (lowest: `swarm` 85.4%). ~143k LOC of tests vs
~98k non-test. The gaps are **structural**, not statistical:

| Gap | Files | Risk | Priority |
|-----|-------|------|----------|
| **Docker-gated runtime uncovered by ANY CI job** | `internal/sandbox/usersandbox/*_integration_test.go`, `bench_soak_test.go`; `cmd/aura/serve_dispatch_egress_integration_test.go`; `internal/agent/tools/shell_{bg,exec}_sandbox_docker_test.go` | Container-escape / egress invariants (incl. CAP_NET_ADMIN placement, WR-01) never asserted in CI | **High** |
| `cmd/aura` gateway-hook logic at 39.9% | `cmd/aura/*` | Carved out of the floor as CLI glue; behaviourally covered by `db_integration` but **not counted** | Medium |
| Stryker mutation on `artifactMeta.ts`/`downloadAll.ts` (claimed 76.56%/100%) **not independently re-run** in verification | `web/` | Unverified mutation claim | Low |
| Nyquist `wave_0_complete` false for 33/34/35/37/37A | `.planning/phases/*` | Reads as **un-updated planning fields**, not failed runs — every phase shows live execution (`v2.0.0-MILESTONE-AUDIT.md:171-176`) | Low |

**Structural note `[NEW]`:** the codebase honours its own ≤600 LOC rule — the only files over it are
**generated** (`internal/db/sqlc/document_control_plane.sql.go` 1037, `models.go` 744, `assets.sql.go`
722). The largest hand-written file is `internal/config/config.go` at **594**. No god classes.

---

*Concerns audit: 2026-07-17 — every entry verified against source in-session; unverifiable items
labelled as such rather than asserted.*
