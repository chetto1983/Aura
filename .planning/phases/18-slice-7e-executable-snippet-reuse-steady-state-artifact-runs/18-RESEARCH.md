# Phase 18: Slice 7e executable snippet reuse — steady-state artifact runs sotto i 40s — Research

**Researched:** 2026-06-06
**Domain:** Agentic loop latency reduction via persistent executable-snippet reuse (Go substrate; skills system; sandbox vs host execution posture; tool-invocation telemetry)
**Confidence:** HIGH (all findings ground-truthed against on-disk code + PRD + Phase-11 summaries; no external API/version research needed — this is an internal-substrate phase)

> **NOTE — there is no CONTEXT.md for Phase 18 yet.** `/gsd-discuss-phase 18` has not run. This research therefore documents the design space and flags the decisions the discuss/plan phase must lock. The `## User Constraints` section is intentionally empty pending discussion. The single most load-bearing open decision (execution posture: host `shell_exec` vs sandbox `sandbox_exec`) is called out throughout and is the explicit PRD follow-up at prd.md line ~2190.

---

## Summary

**Phase 18 is NOT a greenfield build — 7e-core snippets already shipped in Phase 11 wave 7 (plan 11-07, commits `0272a973`/`f315163b`/`7ccaa43e`, marked `requirements-completed: [CAP-08]`).** The on-disk reality: `internal/skills/snippet.go` (SaveSnippet/UseSnippet/by-path resolution), `internal/skills/snippet_usage.go` (usage sidecar + TTL sweep logic), `internal/cron/handlers/skill_ttl.go` (the `skill_ttl_sweep` cron TaskKind), `cmd/aura/skills_snippet.go` (`aura skills snippet {save|exec}` CLI), the ro `/skills` bind mount in `compose.yaml`, and the live `TestSnippetExec` (`sandbox_integration db_integration`) all exist and were proven live. The migration `0010_skill_audit` carries the `skill_ttl_sweep` kind-CHECK widening; `0011_snippet_runs` was deliberately skipped (D-19/A4).

**The problem Phase 18 must actually solve is an architectural mismatch, not a missing feature.** Phase 11 shipped snippets on the **sandbox** (`AURA_SKILL_EXPORT_DIR` → ro `/skills` mount → `sandbox_exec python3 /skills/<name>/<name>.py`). Then, *after* 11-07, amendments #51/#52/#53 pivoted the entire skills self-extension LOOP to the **host terminal** (`shell_exec`, host `<export>/.agents/skills` install root, eval registry seam-free with `shell_exec`+`fs_*` and NO `sandbox_exec`). So today: a snippet's `action=use` hands the model a `sandbox_exec` invocation against `/skills/...`, while the production loop and the D-35 eval gate run on the host. prd.md line ~2190 (#52/D-41) explicitly left snippet execution posture "INVARIATA … per ora" and named the re-evaluation an OPEN follow-up. **Phase 18 is that follow-up.** Two further gaps compound it: (1) the model has NO in-loop way to SAVE a snippet — `action=create`/`update` only author *instruction* skills via `WriteMutation`; `SaveSnippet` is CLI-only — so the model cannot close the discover→save→reuse loop autonomously; (2) `action=restore`/`action=archive` are still `notYetWired` stubs.

**The driver is measured (registration commit `0c7267b0`):** the chat-surface xlsx E2E spends 151–233s dominated by 29–30 LLM roundtrips re-authoring the build script from scratch each run. Cache is healthy (90–95% hit) — NOT the bottleneck. The fix is to collapse the re-authoring loop to ~5 calls (load skill once → run a stored, parametrized artifact builder by path) and get steady-state runs under 40s. Critically, **the uncommitted in-flight `internal/toolinvocations` ledger (migration `0011_tool_invocations`, wired in `runner_persist.go`) is the measurement substrate** that makes "<40s / ≤~5 calls" a machine-checkable acceptance: every tool dispatch writes append-only start/end facts with `duration_ms`, correlated by `conversation_id`/`request_id`/`tool_call_id`.

**Primary recommendation:** Resolve the posture in the PRD-amendment Wave-0 plan FIRST (host-primary, mirroring #52/D-41 — make snippet `action=use` hand the model a `shell_exec`/by-path invocation against the host export dir, keep `sandbox_exec` as the deliberate untrusted-code escalation per the prompt). Then wire the two missing loop closers (model-facing snippet save + `restore`/`archive` handlers) and make the chat-surface E2E gate assert the steady-state roundtrip-count + wall-clock floor from the `tool_invocations` ledger. Treat the Neo4j `:UserSnippet` mirror as OUT OF SCOPE (deferred with 7f / Phase 15).

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Snippet authoring (save) | API/Backend (`internal/skills` Writer) | Tool surface (`skill` tool action) | The gated pending→activate path is backend; the model needs an in-loop tool action to trigger it (currently MISSING — CLI-only) |
| Snippet materialization | Filesystem (`AURA_SKILL_EXPORT_DIR`) | — | Activation copies code into the export dir; the execution-tier consumer (host fs vs sandbox mount) is THE open decision |
| Snippet execution | **OPEN: Host shell (`shell_exec`) vs Sandbox container (`sandbox_exec`)** | — | The shipped path is sandbox by-path; the loop/eval pivoted to host. Phase 18 picks the tier (this is the load-bearing decision) |
| Snippet discovery (`action=use`/`list`) | Tool surface (`skill` tool) | BM25 ranker (`tools/bm25.go`) | Already shipped; v1 = BM25, semantic Neo4j deferred (D-21) |
| TTL archive/restore | Scheduler (`internal/cron` TaskKind) | Filesystem (de-materialize) | `skill_ttl_sweep` cron shipped; `restore` handler missing |
| Roundtrip/latency telemetry | API/Backend (`internal/toolinvocations` + runner) | Postgres ledger (`aura.tool_invocations`) | In-flight uncommitted; the measurement substrate for the <40s gate |
| Acceptance measurement (E2E gate) | Eval/test tier (`internal/eval` cot_eval + chat-surface gate) | — | Must read the ledger for roundtrip-count + wall-clock, not r.Reply |

---

## User Constraints (from CONTEXT.md)

> **EMPTY — no CONTEXT.md exists for Phase 18.** `/gsd-discuss-phase 18` has not run. The plan-phase orchestrator MUST either run discuss first or treat the `## Open Questions` and the posture decision below as the items requiring user lock. Do NOT proceed to plans that assume a posture without confirming it — the host-vs-sandbox decision changes nearly every task.

---

## Phase Requirements

> **No REQUIREMENTS.md ID currently maps to Phase 18 work, and this is a real gap that must be addressed in planning.**

- **CAP-08** (REQUIREMENTS.md line 39) is already marked **Pending** but was reported `requirements-completed: [CAP-08]` by the 11-07 summary AND again by 11-09/11-10 — the traceability table (line 123) still says Pending. CAP-08 as written ("save + by-path exec multi-lang con TTL archived … Reusa il sandbox-agent HTTP :2468 seam") was *satisfied by the sandbox-based 11-07 build*. Phase 18 is a **follow-up that re-opens the execution posture** CAP-08 locked.
- **Recommended action for planning:** author a NEW requirement (e.g. `CAP-08.1` "Snippet reuse steady-state — model-facing save + posture resolution + measured <40s/≤~5-call artifact reuse") OR amend CAP-08's wording to reflect the host-primary pivot. The Wave-0 PRD-amendment plan is the place to do this (PRD-first principle).

| Candidate ID | Description | Research Support |
|----|-------------|------------------|
| CAP-08.1 (new) | Model-facing snippet save (in-loop) + execution-posture resolution (host vs sandbox) + measured steady-state artifact reuse <40s / ≤~5 LLM calls | The shipped 7e snippet store (`internal/skills/snippet*.go`), the tool_invocations ledger (measurement), the chat-surface E2E gate (driver), prd.md #52/D-41 open follow-up |

---

## Standard Stack

> This is an internal-substrate phase. There are NO new external packages to add — everything is already in `go.mod` and shipped. The "stack" below is the **in-repo seams** Phase 18 builds on. No `npm install` / `pip install` / `cargo add` — see Package Legitimacy Audit (none).

### Core (already shipped, ground-truthed on disk)
| Component | File | Purpose | Status |
|-----------|------|---------|--------|
| Snippet save/use/by-path | `internal/skills/snippet.go` | `SaveSnippet` (gated pending), `UseSnippet` (by-path resolve), `SnippetInvocation`, language enum (python/shell/js) | Shipped 11-07 |
| Usage sidecar + sweep | `internal/skills/snippet_usage.go` | `.usage.json` (status/last_used_at/use_count) atomic write, `StampUsage`, `SweepExpiredSnippets` | Shipped 11-07 |
| TTL sweep cron handler | `internal/cron/handlers/skill_ttl.go` | `SkillTTLSweepHandler` (D-16), `SnippetSweeper` seam, daily-seeded | Shipped 11-07 |
| Materialize / de-materialize | `internal/skills/materialize.go` | activation→export dir, symlink-strip, de-materialize on archive/delete | Shipped 11-04/11-07 |
| Skill tool (model-facing) | `internal/agent/tools/skill.go` + `skill_read.go` + `skill_write.go` | ONE `skill` tool, action enum `list/info/use/create/update/delete/restore/archive` | Shipped 11-02/05/07/09 |
| Snippet CLI | `cmd/aura/skills_snippet.go` | `aura skills snippet {save\|exec}` (exec stamps usage) | Shipped 11-07 |
| Tool-invocation ledger | `internal/toolinvocations/store.go` + `0011_tool_invocations.{up,down}.sql` | Append-only start/end facts, duration_ms, args/result bytes, exit_code | **In-flight (UNCOMMITTED)** |
| Ledger wiring | `internal/runner/runner_persist.go` (`persistToolInvocation`) | Event-sourced per-tool persist from agent ToolInvocation Events | **In-flight (UNCOMMITTED)** |
| Execution surfaces | `internal/agent/tools/shell_exec.go` (host) + `sandbox_exec.go` (container) | host terminal (primary, #50/D-15c) vs untrusted-code sandbox | Both shipped |
| Host fs surface | `internal/agent/tools/fs_*.go` | fs_read/write/edit/grep/glob, no path fence (#50/D-15c) | Shipped |

### Supporting (reuse — do NOT rebuild)
| Component | File | When to use |
|-----------|------|-------------|
| ActionRouter | `internal/agent/tools/action.go` | Adding `restore`/`archive`/snippet-save router keys |
| BM25 ranker | `internal/agent/tools/bm25.go` | Snippet `list` ranking (D-09/D-21), already used by `rankSkills` |
| Risk scoring | `internal/scoring/scoring.go` (`ComputeSkillTier`) | Save-time gate (RISKY for create; needs_network surface) |
| ask_user pause/resume | `internal/agent/llm_agent_pause.go` + `internal/askuser/store.go` | Snippet-save approval gate (D-02/D-03) — ONLY ask_user pauses (architectural constraint) |
| Audit store | `internal/skills/audit_store.go` + `0010` triggers | D-29 coherence matrix; every snippet mutation audited |
| Conversation cleaner | Phase-8 `ConversationCleaner` | Snippet-produced workspace files ride the conv purge cascade (D-18) |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `tool_invocations` ledger for measurement | OTel spans (already emitted per-tool) | Spans are ephemeral/sampled; the ledger is durable + queryable for the gate assertion. The ledger WINS (it's in-flight precisely for this) |
| Host `shell_exec` execution | Keep sandbox `sandbox_exec` (shipped path) | Sandbox = isolation for model-authored code; host = parity with the rest of the skills loop + no mount-sync. See Posture Decision below |
| Model-facing snippet save tool action | Keep snippet save CLI-only | CLI-only means the model can never close discover→save→reuse in-loop — defeats the latency goal. A tool action is REQUIRED |

**Installation:** None. `go build ./...` already compiles every seam above (the in-flight ledger is uncommitted but compiles — verify with `go build ./internal/toolinvocations/ ./internal/runner/`).

**Version verification:** N/A — no external packages. All work is in-repo Go against the existing module `github.com/chetto1983/aura`.

---

## Package Legitimacy Audit

**No external packages are installed by this phase.** Snippet execution runs interpreters already present (host shell's `python3`/`node`/`sh`, or the sandbox image's baked `python3`+openpyxl/defusedxml/lxml/validators). Snippet `deps:` frontmatter is docs-only (D-20) unless the planner adopts the on-demand `uv` dep model (D-36) — which would still install INSIDE the sandbox/host at runtime, not into Aura's Go module. **slopcheck N/A; ecosystem-registry verification N/A.** The Go module gains zero new dependencies.

| Package | Registry | Disposition |
|---------|----------|-------------|
| (none) | — | No external package installs in this phase |

---

## Architecture Patterns

### System Architecture Diagram

```
                    ┌─────────────────────────────────────────────┐
   user prompt ───► │  Runner.Turn → LlmAgent.Run (the agent loop) │
  "fammi un xlsx…"  └──────────────────┬──────────────────────────┘
                                       │ each LLM roundtrip
                                       ▼
                        ┌──────────────────────────────┐
                        │  PromptBuilder: messages[0]   │  byte-stable manifest (skill tool Desc)
                        │  messages[1] always-block     │  find-skills-aura + (Phase 14 Agent.md)
                        └──────────────┬────────────────┘
                                       ▼
                        ┌──────────────────────────────┐       TODAY: 29–30 roundtrips
                        │   model picks a tool          │       re-authoring the build script
                        └──────────────┬────────────────┘       from scratch each run
                  ┌────────────────────┼─────────────────────┐
                  ▼                    ▼                     ▼
         ┌──────────────┐   ┌──────────────────┐   ┌──────────────────┐
         │  skill tool  │   │  shell_exec      │   │  sandbox_exec    │
         │ list/info/   │   │  (HOST terminal, │   │  (container,     │
         │ use/create/  │   │   #50/D-15c)     │   │   untrusted code)│
         │ .../restore  │   └────────┬─────────┘   └────────┬─────────┘
         └──────┬───────┘            │                      │
                │ action=use         │  ←── POSTURE GAP ──► │
                │ (snippet)          │  use returns a       │  use CURRENTLY returns a
                ▼                    │  sandbox_exec frame  │  /skills mount path
       ┌─────────────────┐          │  but loop runs HOST  │
       │ internal/skills │          ▼                      ▼
       │  SaveSnippet    │   host export dir:        ro /skills mount:
       │  UseSnippet     │   <export>/.agents/skills <export>:/skills:ro
       │  Sweep (TTL)    │   (loader scans this)     (sandbox sees this)
       └─────────────────┘
                                       │
   EVERY tool dispatch ───────────────┼──────────────────────────────────┐
                                       ▼                                   ▼
                        ┌──────────────────────────────┐    ┌──────────────────────────┐
                        │ agent emits ToolInvocation    │───►│ runner_persist.go         │
                        │ start/end Events (in-flight)  │    │ → aura.tool_invocations   │
                        └──────────────────────────────┘    │   (duration_ms, seq, …)   │
                                                             └────────────┬──────────────┘
                                                                          ▼
                                              ┌─────────────────────────────────────────┐
   GOAL (steady-state): load skill once,      │ E2E GATE reads the ledger:               │
   run stored parametrized builder by path,   │  COUNT(LLM roundtrips) ≤ ~5              │
   ~5 calls, < 40s ────────────────────────► │  SUM/MAX(wall-clock) < 40s              │
                                              └─────────────────────────────────────────┘
```

### Recommended Project Structure (deltas only — most files exist)
```
internal/skills/
├── snippet.go              # EXISTS — add a model-facing-save entry if the tool wires save
├── snippet_usage.go        # EXISTS — restore re-materializes (SetUsageStatus "active")
├── materialize.go          # EXISTS — host vs sandbox path decision lands here conceptually
internal/agent/tools/
├── skill.go                # MODIFY — wire restore/archive (drop notYetWired); maybe add snippet-save action
├── skill_read.go           # MODIFY — action=use snippet frame: shell_exec vs sandbox_exec (POSTURE)
├── skill_write.go          # MODIFY — if snippet save becomes a model action, add the seam
internal/toolinvocations/   # EXISTS (in-flight) — commit it; the gate queries it
internal/eval/
├── scenarios_skills.go     # MODIFY — the steady-state reuse scenario (2nd run measured)
├── skills_cot_eval_test.go # MODIFY — register the skill tool in the eval registry (parity gap)
cmd/aura/
├── skills_snippet.go       # EXISTS — CLI save/exec (keep deterministic)
```

### Pattern 1: ActionRouter reserved-key fill-in (restore/archive)
**What:** The `skill` tool's router already reserves `restore`/`archive` via `notYetWired`. Phase 18 replaces those with real handlers dispatching to `Writer.SetUsageStatus`+re-materialize (restore) / `Writer.Archive`+de-materialize (archive).
**When to use:** Wiring the two missing snippet lifecycle actions.
**Example:**
```go
// Source: internal/agent/tools/skill.go (current notYetWired — replace)
"restore": t.actionRestore,  // unarchive: SetUsageStatus("active") + Materialize + audit RESTORE
"archive": t.actionArchive,  // manual archive (tier SAFE, no gate): Archive + Dematerialize + audit MANUAL_ARCHIVE
```

### Pattern 2: by-path use frame — posture-parametrized (the load-bearing change)
**What:** `renderSnippetUse` (skill_read.go) currently hardcodes a `sandbox_exec` invocation. The posture decision changes this literal.
**When to use:** After the posture is locked. If host-primary: hand the model a `shell_exec` command line (`python3 <host-export>/<name>/<name>.py`); if sandbox: keep the current frame.
**Example (current, sandbox):**
```go
// Source: internal/agent/tools/skill_read.go:119-128 (renderSnippetUse)
fmt.Fprintf(&b, "Run this snippet by path: call sandbox_exec with command=%q and args=[%q] ...",
    interpreter, sandboxPath)
```
**Note:** this literal is a "load-bearing literal" — it has an asserting test pattern (per CONTEXT D-08 / `finalizeNudge`); changing it requires updating the test, and (if host) updating `SnippetInvocation`/`SnippetSandboxPath` to emit host paths.

### Pattern 3: Ledger-driven acceptance assertion (no r.Reply)
**What:** Query `aura.tool_invocations` by `conversation_id` to count LLM roundtrips (proxy: distinct `request_id`s, or assistant turns) and sum/max `duration_ms` for the steady-state (2nd) run.
**When to use:** The chat-surface E2E gate's hard floor.
**Example:**
```go
// the gate reads structured ground truth (probe-must-verify-artifact-not-reply)
events, _ := toolStore.ListByConversation(ctx, convID)
// count distinct request_ids in the steady-state run window; assert ≤ ~5
// assert artifact (.xlsx) exists/opens/today's data (existing skills_xlsx_verify pattern)
```

### Anti-Patterns to Avoid
- **Rebuilding the snippet store:** it already ships. Phase 18 is wiring + posture + measurement, not greenfield.
- **A bespoke `run`/`executor.go`:** D-04 forbids it. Execution is always by-path through `shell_exec`/`sandbox_exec`; the code never re-enters context.
- **Returning the sentinel from a non-`ask_user` tool to force a pause:** documented architectural constraint (#51/D-35 root cause, `TestAskUserOnlyPauseConstraint`). Only `ask_user` pauses. A snippet-save gate must route its approval through `ask_user`, exactly like `WriteMutation`.
- **Asserting the E2E on r.Reply:** the gate must read the ledger + the artifact (`feedback_probe_must_verify_artifact_not_reply`).
- **Eval registry that omits the skill tool / the production execution surface:** #52/#53 mandate eval↔production parity. The current eval registry (post-11-10) is seam-free and registers NO skill tool and NO sandbox_exec — so a snippet-reuse scenario CANNOT run in it today. Fixing this parity is in-scope.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Per-tool latency/roundtrip telemetry | A new metrics table or in-memory counter | The in-flight `internal/toolinvocations` ledger (`aura.tool_invocations`) | It's append-only, durable, correlated by request_id/tool_call_id, has duration_ms — purpose-built for this gate |
| Snippet pending→activate→audit | A new write path | `Writer.SaveSnippet` + `Activate` + `Archive` (shipped) | Gated, atomic, audited (D-29), symlink-stripped already |
| TTL archive sweep | A goroutine ticker | `skill_ttl_sweep` cron TaskKind (shipped, D-16) | Phase-10 persistence/HA/missed-catch-up free; D-16 explicitly forbids a goroutine |
| Snippet discovery ranking | A new ranker | `tools/bm25.go` via `rankSkills` (shipped) | Already wired into `action=list`; D-21 = BM25 for v1 |
| Approval pause for snippet save | A new pause mechanism | `ask_user` sentinel via `WriteMutation`-style flow | ONLY ask_user pauses (architectural constraint) |
| Skill-body validation/blocklist | A new validator | `internal/skills/validator.go` (NFKC + literal blocklist, fuzz-proven) | Loader-level scan (D-27/D-28) already covers self-installed + authored bodies |

**Key insight:** every primitive Phase 18 needs already exists. The phase's value is *connecting* them (model-facing save, restore/archive handlers, posture alignment) and *measuring* the result (ledger-driven gate). Building anything new here is a smell.

---

## Runtime State Inventory

> Phase 18 is partly a posture/migration phase (snippet execution tier may move host↔sandbox; restore/archive change on-disk + DB state). The categories below are answered explicitly.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| **Stored data** | Existing materialized snippets live at `AURA_SKILL_EXPORT_DIR` (default `~/.aura/skills/export`) AND the persistent install root `<export>/.agents/skills` (added by 11-09). The ro `/skills` mount maps `AURA_SKILL_EXPORT_DIR:/skills:ro` in `compose.yaml`. Usage sidecars (`.usage.json`) live inside each active skill dir. `aura.skill_audit` rows (every snippet mutation) and `aura.scheduler_tasks` (the seeded daily `skill_ttl_sweep` row). | If posture moves to host: the loader already scans `<export>/.agents/skills` and `cfg.SkillsDir`; snippet `use` paths must point at the HOST dir not `/skills`. Existing materialized snippets stay valid (same export dir) — only the *invocation frame* changes (code edit, not data migration). The `/skills` ro mount becomes vestigial for snippets (still used by `sandbox_exec`-escalated runs). |
| **Live service config** | The `aura-sandbox-agent` container runs with `--token` (bearer) + the `/skills:ro` mount + baked xlsx deps (per 11-07). This config is in `compose.yaml` (in git) — but the RUNNING container at research time predates the image rebuild (11-07 summary: deps NOT yet baked into the live image). | If host-primary posture: the `/skills` mount is no longer the snippet execution path (only the sandbox-escalation path). No container reconfig needed for the host path. If the E2E needs the sandbox path, `docker compose build aura-sandbox-agent` is required (11-07 deferred verification). |
| **OS-registered state** | The daily `skill_ttl_sweep` cron task is a Postgres `scheduler_tasks` row (seeded by `seedSkillTTLSweep` in `serve.go`), NOT an OS-level scheduler. No Windows Task Scheduler / launchd / systemd entries. | None — the sweep is DB-resident, re-seeded idempotently on boot. |
| **Secrets/env vars** | `AURA_SANDBOX_AGENT_TOKEN` (first-boot gen, `.env`-persisted), `AURA_SKILL_EXPORT_DIR`, `AURA_SKILL_SNIPPET_TTL_DAYS` (90), `AURA_SKILLS_DIR`, `AURA_SKILL_BODY_CAP_BYTES`, `AURA_SKILL_INJECTION_BLOCKLIST`, `AURA_SKILL_MANIFEST_CAP_BYTES`. All read by `internal/config/config.go`. | If posture moves to host, no new secret. A possible new knob: a posture flag (`AURA_SKILL_SNIPPET_EXEC=host\|sandbox`) is one design option — but a hard host-primary decision (no flag) is simpler and matches #52/D-41's "no ceremony" stance. Decide in discuss. |
| **Build artifacts / installed packages** | The sandbox image `aura-sandbox-agent:py3` bakes openpyxl/defusedxml/lxml/validators (Dockerfile change shipped, image rebuild deferred). The Go binary embeds the `find-skills-aura` builtin SKILL.md. No stale egg-info/compiled artifacts. | If the E2E uses the host path, the host needs `python3` + openpyxl (the operator's machine / fat image). If sandbox path, rebuild the image. Flag the host-dep question in discuss (host xlsx deps are NOT guaranteed present like the baked sandbox image). |

**The canonical question — after every file is updated, what runtime state still carries the old posture?** Answer: existing materialized snippets in `AURA_SKILL_EXPORT_DIR` were authored under the sandbox posture (their SKILL.md docs frame says "Run it BY PATH in the sandbox … `sandbox_exec`"). If posture flips to host, `renderSnippetDocs` (snippet.go:229) and `renderSnippetUse` (skill_read.go:119) emit a sandbox frame baked into the *materialized SKILL.md body*. A posture flip needs either (a) re-materialization of existing snippets (data migration: re-render docs) or (b) the `use` frame computed fresh at read time (code edit — preferred, since `UseSnippet` already reads the active SKILL.md and could re-derive the host path). Prefer (b): keep the docs body generic and compute the invocation frame at `use` time.

---

## Common Pitfalls

### Pitfall 1: Treating Phase 18 as greenfield 7e
**What goes wrong:** Planning a from-scratch snippet store, duplicating `SaveSnippet`/`UseSnippet`/the TTL sweep.
**Why it happens:** The ROADMAP goal says "[To be planned]" and CAP-08 is listed Pending in the traceability table, masking that 11-07 already shipped it.
**How to avoid:** Read 11-07-SUMMARY.md + `internal/skills/snippet*.go` first. The phase is wiring + posture + measurement.
**Warning signs:** A plan that creates `internal/skills/snippet.go` or `executor.go`.

### Pitfall 2: Shipping snippet `use` against the wrong execution tier
**What goes wrong:** The model gets a `sandbox_exec` frame but the loop runs on the host (or vice-versa) → "unknown tool" / mount-not-present / 502, budget burned (the EXACT failure class of #52/D-41 root cause).
**Why it happens:** The shipped snippet path (sandbox) and the shipped loop (host) disagree; the eval registry omits both the skill tool and sandbox_exec.
**How to avoid:** Lock the posture in the Wave-0 amendment; make `renderSnippetUse` emit the locked tier's invocation; make the eval registry mirror production (register the skill tool; if sandbox posture, register sandbox_exec).
**Warning signs:** The E2E "unknown tool" error; a snippet `use` returning a `/skills/...` path while `shell_exec` is the only execution tool in the registry.

### Pitfall 3: Eval↔production registry divergence
**What goes wrong:** The cot_eval gate passes/fails for reasons that don't reflect production (the seam-free eval registry has NO skill tool, NO sandbox_exec).
**Why it happens:** 11-10 built a seam-free eval registry to match spike 012a (find→add→use via shell), which deliberately had no skill tool — fine for *discovery+install*, but a *snippet reuse* scenario needs the skill tool.
**How to avoid:** #52/#53 rule 4 is explicit: eval registry MUST mirror production. For a snippet-reuse scenario, register the production `skill` tool (with a live loader/writer) + the locked execution surface.
**Warning signs:** A snippet-reuse eval that can't even resolve `skill action=use`.

### Pitfall 4: Snippet save with no in-loop path
**What goes wrong:** The model discovers a reusable pattern but can't persist it without the operator running `aura skills snippet save` — so the next run re-authors from scratch and the loop never collapses.
**Why it happens:** `action=create`/`update` only author *instruction* skills (`WriteMutation`); `SaveSnippet` is CLI-only.
**How to avoid:** Add a model-facing save path (a `skill` action that routes to `SaveSnippet` behind the ask_user gate), OR explicitly scope Phase 18 to *reuse-of-pre-seeded-snippets* and defer model-authored save. Decide in discuss — but a latency win requires the snippet to exist; if the model can't make it, who does?
**Warning signs:** A "collapse the loop" claim with no mechanism for the snippet to come into existence in-loop.

### Pitfall 5: Measuring the wrong run
**What goes wrong:** Asserting <40s on the FIRST run (which includes discover+save) instead of the STEADY-STATE (2nd+) run.
**Why it happens:** The first run necessarily pays the authoring cost; only reuse is fast.
**How to avoid:** The gate must run the scenario TWICE (or pre-seed the snippet), and assert the steady-state run's ledger window. 11-10's eval already enforces artifact freshness via mtime≥run-start — reuse that windowing discipline.
**Warning signs:** A single-run timing assertion.

### Pitfall 6: Forgetting the L2.5 evictor / cache invariant for any messages[1] change
**What goes wrong:** If snippet teaching moves into the always-block, a long conversation could evict it, or messages[0]/messages[1] byte-stability breaks the cache-invariant CI gate.
**Why it happens:** The messages[1] always-block (find-skills-aura) and messages[0] manifest are cache-load-bearing (CAP-04); `scripts/cache_invariant_audit.sh` gates every merge.
**How to avoid:** If Phase 18 touches the always-block or the skill manifest, run `cache_invariant_audit.sh` and the messages[1] byte-stability assertion (added in 11-08).
**Warning signs:** A failing `cache_invariant_audit.sh` after a prompt/manifest edit.

### Pitfall 7: Host execution deps not present (xlsx)
**What goes wrong:** Host `shell_exec python3 builder.py` fails because openpyxl isn't installed on the host (the sandbox image bakes it; the host may not).
**Why it happens:** The sandbox image guarantees deps; the host doesn't.
**How to avoid:** If host-primary, decide the host-dep story (assume the fat image / operator machine has python+openpyxl; or use `uv` on-demand per D-36; or keep xlsx-class snippets on the sandbox-escalation path). Flag in discuss.
**Warning signs:** `ModuleNotFoundError: openpyxl` on a host run.

---

## Code Examples

### Snippet save (shipped — the seam a model-facing action would call)
```go
// Source: internal/skills/snippet.go (Writer.SaveSnippet)
res, err := w.SaveSnippet(ctx, name, "python", code, Frontmatter{Description: "..."}, AuditActor{ActorID: "model"})
// → validates language enum + write-boundary blocklist on the CODE
// → tier = ComputeSkillTier(SkillCreate, code) (RISKY)
// → writes pending/<name>/{SKILL.md, <name>.py} atomically
// → audit D-29 pending tuple (NULL, NULL, true, false)
// → NEVER self-activates (activation = ask_user resume or CLI)
```

### Snippet use (shipped — the posture-sensitive frame)
```go
// Source: internal/skills/snippet.go (Writer.UseSnippet) — returns:
// SnippetUse{ Instructions, SandboxPath:"/skills/<name>/<name>.py", Interpreter:"python3", Language }
// THE POSTURE DECISION: keep SandboxPath OR add a HostPath derived from AURA_SKILL_EXPORT_DIR
```

### TTL sweep handler (shipped — restore is the missing inverse)
```go
// Source: internal/cron/handlers/skill_ttl.go (SkillTTLSweepHandler.Run)
archived, kept, err := h.Sweeper.SweepExpiredSnippets(ctx, h.TTL, now, "auto")
// → Writer.SweepExpiredSnippets: per active snippet past TTL → Archive(ApprovalAuto)
//   → de-materialize from export dir + D-29 auto audit row + SetUsageStatus("archived")
// MISSING: action=restore (unarchive: SetUsageStatus("active") + re-Materialize + audit RESTORE)
```

### Ledger query for the gate (in-flight — the measurement)
```go
// Source: internal/toolinvocations/store.go (Store.ListByConversation)
events, _ := toolStore.ListByConversation(ctx, convID)
// each Event: {ToolName, Event:"start"|"end", DurationMS, Seq, RequestID, ...}
// gate: count distinct RequestID in the steady-state window ≤ ~5; sum/max DurationMS < 40s
```

---

## State of the Art

| Old Approach (shipped 11-07) | Current Direction (Phase 18) | When Changed | Impact |
|------------------------------|------------------------------|--------------|--------|
| Snippet exec via `sandbox_exec` against ro `/skills` mount | Likely host `shell_exec` against `AURA_SKILL_EXPORT_DIR` (mirror #52/D-41 loop) | prd.md #52/D-41 (2026-06-06) left it OPEN | The snippet `use` frame + eval registry change tier |
| Snippet save = CLI-only (`aura skills snippet save`) | Model-facing in-loop save (proposed) | Phase 18 | Closes discover→save→reuse autonomously |
| `restore`/`archive` = `notYetWired` stubs | Real handlers | Phase 18 | Completes the snippet lifecycle |
| No durable per-tool latency telemetry | `aura.tool_invocations` ledger | In-flight (uncommitted) | Makes <40s/≤~5-call machine-checkable |
| Eval registry seam-free (no skill tool, no sandbox_exec) | Eval mirrors production (skill tool + locked exec surface) | Phase 18 (#52/#53 rule 4) | Snippet-reuse scenario becomes runnable |

**Deprecated/outdated (do NOT plan around these):**
- The PRD §Slice 7e Componenti table's `internal/skills/executor.go` and `sandbox.Runner.Execute` — DEAD (D-04/amendment #44). No bespoke run.
- `0011_snippet_runs` (PRD optional table) — deliberately skipped in 11-07 (D-19/A4). The `0011` slot is now taken by `tool_invocations` (in-flight). Any new snippet-run forensics should ride the ledger, NOT a new `snippet_runs` table.
- The PRD §Slice 7e `pattern_analyzer.go` background goroutine — deferred to 7f / SKILL-V2-01 (OUT OF SCOPE, confirmed).
- Neo4j `:UserSnippet` mirror (prd.md line ~3538) — a Slice 11 / Phase 15 memory concern (semantic snippet discovery), deferred with 7f. OUT OF SCOPE for Phase 18.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Phase 18's intended execution posture is host-primary (mirror #52/D-41), not sandbox-keep | Summary / Posture | If sandbox-keep is chosen, the snippet `use` frame stays as-is and the eval must register sandbox_exec instead of relying on shell_exec — most tasks invert |
| A2 | The in-flight `tool_invocations` ledger WILL be committed before Phase 18 executes (per the brief) and is the intended measurement substrate | Validation Architecture | If it's reverted, the gate needs another measurement source (OTel spans, or a bespoke counter) — weaker |
| A3 | A model-facing snippet-save action is desired (the latency win needs an in-loop way to create snippets) | Pitfall 4 | If save stays CLI-only, Phase 18 reduces to reuse-of-pre-seeded-snippets + restore/archive + measurement (smaller scope) |
| A4 | The steady-state target (<40s, ~5 calls) is measured on the 2nd+ run with the snippet already present, not the first authoring run | Pitfall 5 | If the first run must also be <40s, the goal is likely infeasible (authoring cost is irreducible) |
| A5 | Host xlsx deps (python3 + openpyxl) are acceptable to assume present on the host/fat-image if posture is host-primary | Pitfall 7 | If hosts lack openpyxl, xlsx-class snippets may need the sandbox path or on-demand uv (D-36) |
| A6 | CAP-08 should be re-scoped/amended (new CAP-08.1) rather than left as-is, since the shipped CAP-08 was sandbox-based | Phase Requirements | If CAP-08 is considered fully done, Phase 18 has no requirement ID and the planner must create one anyway |
| A7 | The Neo4j `:UserSnippet` mirror is OUT of v1 scope (deferred with 7f/Phase 15) | State of the Art | If in scope, Phase 18 gains a Neo4j ingestion dependency (Phase 15 infra) it should not have |

---

## Open Questions (RESOLVED)

> All 8 questions resolved 2026-06-06 via inline Q&A → locked decisions D-01..D-04 in `18-CONTEXT.md` + plan design (18-01..18-04). Inline markers below.

1. **Execution posture: host `shell_exec` vs sandbox `sandbox_exec` vs both-with-a-flag?**
   - **RESOLVED (D-01):** host-primary for approved snippets; `sandbox_exec` stays as deliberate per-run escalation. PRD amendment in 18-01-T1.
   - What we know: 11-07 shipped sandbox by-path; #52/D-41 pivoted the loop to host and left snippet posture OPEN; the prompt says "Run UNTRUSTED or model-generated code in the sandbox." A snippet IS model-generated code → arguably belongs in the sandbox. BUT the rest of the loop runs on the host, and host parity is the #52/D-41 directive.
   - What's unclear: whether model-authored snippet reuse should be treated as "untrusted" (sandbox) once it's been operator-approved at save time (it's no longer fresh model code — it's a vetted, gated artifact).
   - Recommendation: **host-primary for approved snippets** (they passed the save gate; treat them like instruction skills which run on the host), keeping `sandbox_exec` available for the model to escalate any individual run. Lock this in the Wave-0 PRD amendment. Decide in `/gsd-discuss-phase 18`.

2. **Does the model get an in-loop snippet-save action?**
   - What we know: only CLI today; the latency win needs the snippet to exist.
   - Recommendation: yes — a `skill` action that routes to `SaveSnippet` behind ask_user (the create/update gate shape). Confirm scope in discuss.
   - **RESOLVED (D-02):** yes, UNGATED (no-ceremony directive) — returns a normal ToolResult, never the pause sentinel. Implemented in 18-03-T2.

3. **What exactly do the 29–30 roundtrips spend on?**
   - What we know: registration commit says "re-authoring the build script from scratch every run"; the chat-surface E2E gate (11-08 / `internal/eval`) drives the xlsx North-Star. The secondary lever (machine-profile env pinning, ~7 probe calls) is explicitly OUT of scope.
   - What's unclear: the precise per-call breakdown — the planner should read the run transcripts / the ledger from a live run, OR instrument the existing E2E to dump the per-tool sequence (the ledger does this now). 
   - Recommendation: use the committed `tool_invocations` ledger from one live xlsx run to characterize the 29–30 calls empirically before finalizing the ~5-call target. Probe-before-plan.
   - **RESOLVED (D-03):** Wave-0 characterization task — 18-01-T3 operator checkpoint dumps the per-call ledger breakdown to `docs/phase-18-xlsx-call-breakdown.md`; the ~5-call target is provisional until grounded.

4. **How is the <40s / ≤~5-call acceptance enforced — extend the chat-surface gate, or a new gate?**
   - Recommendation: extend the existing 11-08 chat-surface E2E (real binary), run the scenario twice, assert the steady-state window from the ledger. Keep the artifact-not-reply discipline.
   - **RESOLVED:** as recommended — 18-04-T2 runs the scenario twice via the production `runner.Runner` path (`Deps.ToolInvocations` wired) and asserts the 2nd-run ledger window (distinct request_ids + wall-clock) + fresh artifact.

5. **Is the `skill_ttl_sweep` cron still the right shape?**
   - What we know: it's shipped, daily-seeded, D-16-correct. Phase-9/10 scheduler is complete.
   - Recommendation: yes — unchanged. Phase 18 only adds the `restore` inverse handler.
   - **RESOLVED:** unchanged; `Writer.Restore` (inverse of Archive) lands in 18-03-T1, audited as `AuditActivate`/`ApprovalCLI` (no new constant — 0010 CHECK).

6. **Migration + sqlc surface needed?**
   - What we know: snippet live-state is the sidecar JSON (D-19); `0011_snippet_runs` was skipped; `0011` slot is now `tool_invocations`. 
   - Recommendation: NO new migration for snippets. Snippet-run forensics ride the existing `tool_invocations` ledger (filter by tool_name). The only DB touch is committing the in-flight `0011_tool_invocations` (already authored).
   - **RESOLVED:** as recommended — no snippet migration; 18-01-T2 verify-and-commits the in-flight ledger as-authored (scope boundary: sqlc regen + trivial mechanical fixes only, else STOP).

7. **Which PRD sections need an amendment commit BEFORE code (PRD-first)?**
   - §Slice 7e-core "Idea" + "Acceptance 7e" + "Componenti" + Commit template: re-point execution from `sandbox_exec`/`/skills` to the locked posture (host-primary recommended) — supersede the by-path-in-sandbox wording.
   - §Slice 7e Open questions: add the posture resolution.
   - prd.md line ~2190 (#52/D-41 follow-up): mark the snippet-posture follow-up RESOLVED with the Phase-18 decision.
   - §Slice 11 pre-requisiti line ~3538 (`:UserSnippet` mirror): confirm DEFERRED with 7f (no change, just note).
   - CAP-08 (REQUIREMENTS.md): amend wording OR add CAP-08.1.
   - §Caps & Limits env catalog: if a posture flag is added, register it; otherwise no env change.
   - **RESOLVED:** amendment scope locked into 18-01-T1 (PRD amendment #54: §Slice 7e-core re-pointing, ~2190 follow-up marked RESOLVED, CAP-08.1 in REQUIREMENTS.md, :UserSnippet deferral noted; no posture flag → no env change).

8. **Neo4j `:UserSnippet` mirror — v1 or deferred?**
   - Recommendation: DEFERRED (with 7f / Phase 15). It's semantic discovery infra; v1 discovery is BM25 (D-21). OUT OF SCOPE.
   - **RESOLVED:** deferred with 7f, confirmed in 18-CONTEXT.md `<deferred>`; 18-01-T1 notes the deferral in the PRD without changing it.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain (build/test/race) | All Go work | ✓ (per CLAUDE.md) | Go 1.25+ | — |
| Postgres (migrated through 0010+) | tool_invocations ledger, audit, scheduler, db_integration | ✓ (stack up) | 17 | — |
| sandbox-agent container (`make sandbox-up`, `--token`, `/skills:ro`) | sandbox-posture snippet exec + sandbox_integration tier | ✓ (compose) | rivetdev/sandbox-agent | host shell_exec (if host posture) |
| Host `python3` + openpyxl | host-posture xlsx snippet exec | ✗ (NOT guaranteed on host) | — | sandbox image (baked) OR uv on-demand (D-36) |
| sandbox image rebuilt with baked xlsx deps | sandbox-posture xlsx E2E | ✗ (Dockerfile shipped, image rebuild deferred per 11-07) | — | `docker compose build aura-sandbox-agent` |
| OPENROUTER_API_KEY (DeepSeek-V4) | live cot_eval xlsx E2E (operator-run, NOT CI) | operator-gated | — | no-key structural tests (TestClassify/TestRegistry) |
| SearXNG (web_search for "today's data") | the xlsx scenario pulls live market data | ✓ (compose, socat bridge gotcha) | — | see reference_live_finalize_e2e_socat_bridge |

**Missing dependencies with no fallback:** none that block planning. The host-xlsx-deps gap is a *posture-dependent* concern (only bites if host-primary AND the E2E uses xlsx on the host).

**Missing dependencies with fallback:** host openpyxl → sandbox image (baked) or uv on-demand; rebuilt sandbox image → `docker compose build`.

---

## Validation Architecture

> nyquist_validation is enabled (no `workflow.nyquist_validation: false` in config). This section drives VALIDATION.md.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `goleak` (TestMain) + `rapid` (property) + go-mutesting (mutation) |
| Config file | none (Go convention); build tags gate tiers (`db_integration`, `sandbox_integration`, `cot_eval`) |
| Quick run command | `go test -race ./internal/skills/ ./internal/agent/tools/ ./internal/cron/handlers/ ./internal/toolinvocations/` |
| Full suite command | `make quality-full` (WSL; PG+Neo4j+sandbox up; composed DSNs + AURA_SANDBOX_AGENT_TOKEN + AURA_SKILL_EXPORT_DIR; CI=true no-skip-as-green) |

### Phase Requirements → Test Map
| Req | Behavior | Test Type | Automated Command | File Exists? |
|-----|----------|-----------|-------------------|-------------|
| CAP-08.1 posture | snippet `use` returns the LOCKED execution-tier frame | unit | `go test -run TestSnippetUse ./internal/agent/tools/` | ⚠ exists for sandbox frame — UPDATE for posture |
| CAP-08.1 by-path exec | save→activate→exec by path→output captured | integration | `go test -tags '<host\|sandbox>_integration db_integration' -run TestSnippetExec ./internal/skills/` | ✅ (sandbox variant exists; add/retarget for host) |
| CAP-08.1 model save | model `action=<save>` → ask_user gate → pending | unit | `go test -run TestSnippetSaveAction ./internal/agent/tools/` | ❌ Wave 0 (new) |
| CAP-08.1 restore | `action=restore` unarchives + re-materializes + audit | unit + integration | `go test -run TestActionRestore ./internal/agent/tools/` + db_integration | ❌ Wave 0 (new) |
| CAP-08.1 archive | `action=archive` (SAFE, no gate) de-materializes + audit | unit + integration | `go test -run TestActionArchive ./internal/agent/tools/` | ❌ Wave 0 (new) |
| CAP-08.1 telemetry | ledger records per-tool start/end + duration_ms | unit + integration | `go test ./internal/toolinvocations/` + runner test | ✅ (in-flight: store_test.go + runner_toolinvocations_test.go) |
| CAP-08.1 steady-state | 2nd run ≤ ~5 LLM calls AND < 40s via ledger | E2E (operator) | `go test -tags cot_eval -run TestSkillsE2E ./internal/eval/` (ledger-asserted) | ⚠ exists for xlsx artifact; ADD ledger window assertion |
| CAP-08.1 eval parity | eval registry registers skill tool + locked exec surface | structural (no-key) | `go test -tags cot_eval -run 'TestRegistry' ./internal/eval/` | ⚠ exists (asserts seam-free) — REVISE for skill-tool parity |
| CAP-04 invariant | messages[0]/messages[1] byte-stable if prompt/manifest touched | regression | `bash scripts/cache_invariant_audit.sh` | ✅ |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test -race ./internal/skills/ ./internal/agent/tools/ ./internal/cron/handlers/ ./internal/toolinvocations/`
- **Per wave merge:** the db_integration + (host or sandbox)_integration tiers for the touched surface + `cache_invariant_audit.sh`
- **Phase gate:** `make quality-full` green (owned-surface coverage ≥85% across the full tag matrix) + the live ledger-asserted steady-state E2E (operator-run) + mutation spot-check ≥70% on the new handlers (restore/archive/save) — before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `internal/agent/tools/skill_test.go` — `TestActionRestore`, `TestActionArchive`, (if save action) `TestSnippetSaveAction` — covers the new handlers
- [ ] `internal/agent/tools/skill_read_test.go` — update the snippet `use`-frame assertion for the LOCKED posture (load-bearing literal)
- [ ] `internal/skills/snippet_integration_test.go` — a HOST-posture by-path exec variant (if host-primary) alongside the existing sandbox one
- [ ] `internal/eval/scenarios_skills.go` + `skills_cot_eval_test.go` — register the production skill tool in the eval registry (parity); add the steady-state 2-run + ledger-window assertion
- [ ] ledger query helper in the gate (count distinct request_id; sum/max duration_ms over the steady-state window)
- [ ] Commit the in-flight `internal/toolinvocations` + `0011_tool_invocations` + `runner_persist.go` wiring (already authored; verify `go test ./internal/toolinvocations/ ./internal/runner/` green)

*(Existing infra covers: snippet save/use/sweep unit + sandbox integration, TTL cron, audit immutability, fuzz validator, cache invariant.)*

---

## Security Domain

> security_enforcement is enabled (absent = enabled). Snippets are model-authorable executable code — the isolation gap is load-bearing (CONTEXT D-38).

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V1 Architecture | yes | Posture decision = the trust-boundary decision (host = operator privileges; sandbox = isolated). Document the boundary in the Wave-0 amendment |
| V5 Input Validation | yes | `internal/skills/validator.go` NFKC + literal injection blocklist on snippet CODE at write boundary (D-27) + Loader-level scan (D-28 amended) — already shipped |
| V6 Cryptography | no | No new crypto; bearer token reuse (`AURA_SANDBOX_AGENT_TOKEN`) if sandbox path retained |
| V10 Malicious Code | yes | Symlink-strip at materialization (`materialize.go` Lstat-no-follow); by-path-not-exec-bit (spike 005); approved-at-save gate (D-02/D-29 audit) |
| V12 File/Resource | yes | Snippet files in `AURA_SKILL_EXPORT_DIR`; `SanitizeName` chokepoint before any `filepath.Join`; ro mount (sandbox path) |

### Known Threat Patterns for {snippet execution}

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Model-authored snippet executes hostile code on the HOST (host posture) | Elevation of Privilege | Save-time ask_user gate + scoring tier + injection blocklist; operator-trusted single-user posture (#50/D-15c); offer per-run sandbox escalation |
| Snippet path traversal via crafted name | Tampering | `SanitizeName` single chokepoint (regex `^[a-z0-9-]+$`, reserved-name reject) before every `filepath.Join` (shipped) |
| Symlink in a snippet projects a host path into the exec dir | Tampering / Info Disclosure | `copyTreeNoSymlinks` Lstat-no-follow at materialization (shipped) |
| Stale/poisoned `use` frame points at the wrong tier → 502 / unknown tool | Denial of Service | Posture alignment + eval↔production parity (this phase's core) |
| Audit tampering (hide a self-extension) | Repudiation | `aura.skill_audit` BEFORE UPDATE/DELETE/TRUNCATE triggers + aura_app role separation (shipped); `tool_invocations` append-only triggers (in-flight) |
| TTL sweep archives an in-use snippet | Denial of Service | last_used_at (sidecar) drives staleness; restore handler (this phase) un-archives |

---

## Sources

### Primary (HIGH confidence — on-disk ground truth, read this session)
- `internal/skills/snippet.go`, `snippet_usage.go`, `materialize.go` — the shipped snippet store
- `internal/cron/handlers/skill_ttl.go` — the shipped TTL sweep cron TaskKind
- `internal/agent/tools/skill.go`, `skill_read.go`, `skill_write.go` — the model-facing skill tool (restore/archive notYetWired; no model snippet-save)
- `internal/agent/tools/shell_exec.go`, `sandbox_exec.go`, `fs.go` — the execution surfaces
- `internal/toolinvocations/store.go` + `internal/db/migrations/0011_tool_invocations.up.sql` + `internal/runner/runner_persist.go` — the in-flight measurement ledger
- `cmd/aura/main.go` (buildBaseRegistry), `cmd/aura/shell.go` — the production registry + host-shell entry
- `internal/config/config.go` — env knobs (AURA_SKILL_EXPORT_DIR, AURA_SKILL_SNIPPET_TTL_DAYS, etc.)
- `.planning/phases/11-skills/11-07-SUMMARY.md` — the 7e snippet ship (CAP-08), sandbox posture
- `.planning/phases/11-skills/11-08-PLAN.md` — the chat-surface E2E gate (the driver)
- `.planning/phases/11-skills/11-09-SUMMARY.md`, `11-10-SUMMARY.md` — the 7g deletion + host-pivot + seam-free eval
- `.planning/phases/11-skills/11-CONTEXT.md` — D-01..D-38 (skill system locked decisions; D-04/D-16/D-17/D-19/D-20/D-21/D-36/D-37/D-38 snippet-relevant)
- `prd.md` §Slice 7 (lines ~2114-2280) + §Slice 7e-core (lines ~2389-2639) + amendments #48/#51/#52/#53 (lines ~2116-2202) + line ~2190 (the open posture follow-up) + line ~3538 (`:UserSnippet` mirror, deferred)
- `.planning/REQUIREMENTS.md` CAP-08 (line 39) + traceability (line 123)
- `.planning/ROADMAP.md` Phase 18 (lines 528-537), Phase 11 (lines 32, 381-396)
- `.planning/STATE.md` Phase 18 registration (line 118), Phase 11 decisions (lines 160-166)

### Secondary (MEDIUM — auto-memory cross-references, project-internal)
- `feedback_aura_full_host_terminal_primary.md`, `project_sandbox_pivot_to_code_sandbox_mcp.md` — the host-primary posture rationale
- `reference_cot_eval_harness.md`, `reference_e2e_full_matrix_invocation.md` — the eval/E2E invocation patterns
- `feedback_probe_must_verify_artifact_not_reply.md`, `feedback_inspect_artifact_visually_not_just_pass_status.md` — the artifact-not-reply gate discipline

### Tertiary (LOW — none)
- No external/web sources needed: this is an internal-substrate phase with zero new dependencies.

---

## Metadata

**Confidence breakdown:**
- Shipped 7e state: HIGH — every file read on disk; 11-07 summary corroborates
- Architectural mismatch (host vs sandbox): HIGH — both the shipped sandbox path and the host-pivot amendments read directly; the open follow-up is verbatim in prd.md
- Measurement substrate (ledger): HIGH — the in-flight migration + store + runner wiring all read on disk
- The ~5-call target / 29-30-call breakdown: MEDIUM — the count is from the registration commit; the per-call breakdown should be empirically characterized from a live ledger run before finalizing (Open Question 3)
- Requirement mapping: MEDIUM — CAP-08 status is ambiguous (Pending in table, completed in summaries); a new ID or amendment is recommended

**Research date:** 2026-06-06
**Valid until:** 2026-07-06 (stable — internal substrate; the only volatility is whether the in-flight ledger commits as-is. Re-verify `git status` for `internal/toolinvocations/` + `0011_tool_invocations` + `runner_persist.go` before planning.)
