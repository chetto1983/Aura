---
last_mapped_commit: 5adb3d49b9b8cd7ea4f872fbdb7199b4021c9f5c
---

# Codebase Concerns

**Analysis Date:** 2026-08-13

**Method note:** every item below is tagged either **VERIFIED** (I read the code/config/CI
definition myself and confirmed the claim at `last_mapped_commit`) or **CLAIMED** (a doc —
`CLAUDE.md`, `docs/aura-quality-snapshot.md`, `docs/audit/README.md`, a `docs/superpowers/`
plan — asserts it; I checked a sample against the code where stated, but did not re-derive
every number). Where a doc's claim and the code disagree, both are shown.

## Severity summary

| # | Concern | Severity | Evidence |
|---|---|---|---|
| 1 | `internal/agent/tools` (the largest, most security-sensitive package) is fully excluded from `golangci-lint` under a stale "pre-rewrite skeleton" label | **High** | verified — 76 issues surface when the exclusion is removed |
| 2 | `arcadedb_integration` tier (11 files) compiles but is never executed by CI/Makefile/scripts | **High** | verified |
| 3 | Coverage gate enforces an *aggregate* floor across `internal/...`, not a true per-package floor, despite CLAUDE.md's "per-package floor" wording | **Medium** | verified |
| 4 | `CalendarMCPAdminToken` well-known default (`changeme-aura-pim-local`) is not rejected under strict profiles, unlike sibling secrets | **Medium** | verified |
| 5 | `docker_integration` tier (10 files) runs live in CI but contributes zero to the coverage floor | **Medium** (self-documented, mitigated) | verified |
| 6 | Fixed 50-turn conversation-history hard cap, independent of the model's context window | **Medium** | verified in code; fix in progress, unmerged |
| 7 | Memory-recall preload is a self-acknowledged prompt-injection/memory-poisoning surface with no dedicated threat-model doc | **Medium** | verified default is off; gap is the missing ADR |
| 8 | `docs/aura-quality-snapshot.md` HNSW/Neo4j baseline section contradicts the RETIRED row above it | **Low** | verified (doc-only) |
| 9 | 5 external-blocked release gates (`docs/audit/README.md`) | **Low** (not a code defect) | claimed, doc is current |
| 10 | Several files sit at/within ~10 lines of the 600-LOC cap | **Low** | verified |

---

## Tech Debt

**`internal/agent/tools` and `internal/llm/client.go` are permanently excluded from lint under a label that no longer describes them:**
- Issue: `.golangci.yml` excludes both paths from every enabled linter (`errcheck`, `govet`,
  `staticcheck`, `unused`, `gosec`, `revive`, `dupl`, `modernize`) with the comment
  `# pre-rewrite skeleton — owning slice rewrites`.
- Files: `.golangci.yml:53-54` (lint `exclusions.paths`) and `.golangci.yml:81-82`
  (formatter `exclusions.paths`, duplicated).
- Impact: **VERIFIED.** `internal/agent/tools` is not a skeleton — it is the largest owned
  Go surface in the repo (46 non-test files, 8,621 LOC: `shell_exec`, the sandbox router
  bridge, `fs_read/write/edit/grep/glob`, `send_file`, `skill_*`, `tool_search`/deferred-tool
  manifest, `document_open`, `web_fetch`/`web_search`). Re-running `golangci-lint` against it
  with the exclusion removed (config copy, same linter set) surfaces **76 issues**: 4 `gosec`
  (`internal/agent/tools/read_tool_output.go:90,93` and `send_file_sandbox.go:105` — G304
  potential file inclusion via variable; `internal/agent/tools/result.go:242` — G301 directory
  mode 0755), 1 `errcheck` (`read_tool_output.go:107` unchecked `f.Close()`), 1 `unused`
  (`document_extract_xlsx.go:17` — `relationshipsNS` constant never referenced), 1
  `staticcheck` (`bm25.go:152` — De Morgan simplification), and 47 `revive` +
  22 `modernize` findings (mostly missing doc-comments on exported `Spec`/`Execute` methods
  across nearly every tool file). None of these are proven exploitable — the `gosec` G304
  hits are the common "path built from a variable" pattern golangci-lint always flags and
  may be false positives here — but they are **currently invisible to CI and to the
  project's own `//nolint` discipline** (every other suppression in the repo carries an
  inline justification comment; these have none because the linter never runs on them at
  all). `internal/llm/client.go` (163 LOC, defines the provider-neutral `Client`/`Message`/
  `ToolCall` types used by `internal/agent`, `internal/runner`, `internal/swarm`,
  `internal/cron/handlers`) reports **0 issues** when un-excluded — the label is equally
  stale there but currently harmless.
- Fix approach: remove both exclusion lines (lint + formatter blocks), triage the 76
  findings (the 4 `gosec` + 1 `errcheck` first), add inline `//nolint` justifications for
  any accepted false positives, and let `revive`/`modernize` autofix the rest.

**`scripts/coverage_gate.sh` enforces one aggregate percentage, not "the ≥85% per-package floor" CLAUDE.md describes it as:**
- Issue: CLAUDE.md's Coverage section states *"The ≥85% per-package floor is enforced by
  the gate on every run, not a standing attestation."* **VERIFIED false as literally
  written**: `scripts/coverage_gate.sh:47-105` runs one `go test -coverprofile` over all of
  `./internal/...` (minus `internal/db/sqlc`, `internal/agent/agenttest`,
  `internal/llm/client.go`), sums `COVERED_STATEMENTS`/`TOTAL_STATEMENTS` across every
  filtered row, and compares that **single combined number** against `AURA_COVERAGE_MIN`
  (default 85). There is no per-package loop, no per-package minimum, and no per-package
  failure path anywhere in the 136-line script.
- Files: `scripts/coverage_gate.sh:47-105`; the claim is in `CLAUDE.md` (Quality tooling
  section, "Coverage" gate bullet).
- Impact: a package can sit far below 85% (CLAUDE.md's own coverage-campaign history cites
  `internal/objectstore` at 69.6% as of the 2026-07-18 measurement — **CLAIMED, not
  re-verified for current HEAD**) and never fail the gate as long as the rest of
  `internal/...` compensates. Anyone reading only CLAUDE.md's summary would believe every
  owned package individually clears 85%; the gate does not check that.
- Fix approach: either correct the CLAUDE.md wording to "aggregate floor" (cheap, honest),
  or add a genuine per-package check to `coverage_gate.sh` (more work, changes gate
  semantics — needs its own decision, not a drive-by fix).

**`make arcadedb-integration` does not run the `arcadedb_integration`-tagged Go tests:**
- Issue: the target name strongly implies it runs the Go tier carrying that build tag. It
  does not.
- Files: `Makefile:247-248` — `arcadedb-integration: db-migrate memory-up` followed by
  `$(MAKE) agent-memory-eval`, which invokes `scripts/agent_memory_eval.py` (a Python
  deterministic + live MCP evaluator, schema `aura.agent-memory-eval/v1`). The comment at
  `Makefile:243-246` explicitly says this is "compatibility name for operators' muscle
  memory" and that the MRS evaluator is "the single current memory gate."
- Impact: a developer running `make arcadedb-integration` expecting the 11
  `arcadedb_integration`-tagged Go test files (LOCOMO suite, `memory_integration_test.go`,
  etc. — see Test Coverage Gaps below) to execute will see the Python MRS gate pass/fail
  instead and reasonably conclude the Go tier ran. It did not.
- Fix approach: either rename the target (e.g. `agent-memory-eval` already exists as the
  real name — deprecate the alias) or add a second target that actually runs
  `go test -tags arcadedb_integration ./internal/arcadedb/... ./cmd/arcadedb-mcp/... ./cmd/aura/...`
  against the live stack.

**`docs/aura-quality-snapshot.md`'s HNSW/Neo4j baseline section is stale relative to the RETIRED row it annotates:**
- Issue: the "HNSW configuration baseline (amendment #20 cross-ref)" section (lines 76-80)
  states *"Aura uses `vector.hnsw.m = 32` ... for every `:Chunk`, `:Entity`, `:Community`,
  and `:AgentInsight` vector index"* and *"the GraphRAG recall@5 row above implicitly
  depends on M=32"* — but the referenced row, "GraphRAG retrieval recall@5 @ 100K corpus",
  is marked **`(RETIRED)`** in the main table two sections above (line 51), and the Neo4j
  label schema (`:Chunk`/`:Entity`/`:Community`/`:AgentInsight`) belongs to the graph store
  removed per the 2026-08-02 "graph-store removal" entry in the same file's changelog
  paragraph and `docs/adr/0038-graph-store-license-neo4j-gplv3-vs-arcadedb-apache.md`.
- Files: `docs/aura-quality-snapshot.md:76-80` (stale section) vs `docs/aura-quality-snapshot.md:51` (RETIRED row it references).
- Impact: doc-only — a reader following the cross-reference lands on a baseline for a
  subsystem that no longer exists, with no "historical/retired" label (unlike the
  explicitly-marked "Sandbox-Agent Pivot Detail" and "Phase 7 web-tools detail" sections
  further down the same file, which both carry a `> The original ... is historical` note).
- Fix approach: prepend the same historical/retired framing used elsewhere in the file, or
  delete the section.

**Historical WR-01 defect resolved, evidence retained but worth noting for future readers:**
- `docs/ARCHITECTURE.md:222` and `docs/superpowers/plans/2026-08-03-one-path-sandbox-only.md`
  both describe a CAP_NET_ADMIN capability-assertion bug that "stayed latent" because the
  `docker_integration` tier never ran in CI. **VERIFIED FIXED**: `internal/sandbox/usersandbox/egress.go:106`
  grants `CapAdd: []string{capNetAdmin}` only to the egress sidecar spec;
  `internal/sandbox/usersandbox/egress_test.go` (`TestEgress_CapOnSidecarOnly`, daemon-free)
  and `egress_integration_test.go` (`inspectCapAdd`, live-Docker) both assert the box itself
  carries `CapAdd: []string{}` and only the sidecar carries `NET_ADMIN`. The
  `sandbox-docker-integration` CI job (`.github/workflows/ci.yml:1637-1706`) now runs this
  tier on every push/PR (native-Linux `ubuntu-latest`, the only host where the egress DROP
  assertion is meaningful per the job's own comment). No action needed; recorded here only
  because the "WR-01" citation is scattered across three docs and could otherwise read as
  an open item.

**The "one path: every tool runs in the box" collapse (`docs/superpowers/plans/2026-08-03-one-path-sandbox-only.md`, status PROPOSED) has already landed:**
- **VERIFIED**: `internal/sandbox/usersandbox/router.go:1-12` states in its own header
  comment "There is no host arm behind it any more"; `internal/agent/tools/document_open.go`
  now holds a `Router *usersandbox.SandboxRouter` field and writes through
  `t.Router.WriteFileStream` (line 180) instead of `os.Create` on the aura container's own
  volume — the exact defect the plan describes ("Clienti.xlsx" not found on disk) is fixed
  by construction. The plan document's `Status: PROPOSED` header is stale relative to the
  code; worth a doc pass but not a code concern.

## Known Bugs

No currently-open, code-verified bugs were found. The two historically-documented defects
referenced across `docs/ARCHITECTURE.md`, `docs/superpowers/plans/2026-08-03-one-path-sandbox-only.md`,
and `CLAUDE.md` (the CAP_NET_ADMIN cap-assertion bug and the two-filesystem `document_open`
defect) are both **VERIFIED FIXED** in the current tree — see Tech Debt above for the
evidence trail. If a future audit re-reads those three docs and assumes either bug is still
open, it is not.

## Security Considerations

**`internal/agent/tools` (the tool-execution surface) has zero static-analysis coverage:**
- Risk: the package implementing `shell_exec`, the sandbox routing bridge, all `fs_*` file
  operations, `send_file`, and `document_open` — i.e. every place untrusted model output
  turns into a filesystem or subprocess action — has never had `gosec`/`staticcheck`/
  `revive` run against it (see Tech Debt above; 4 `gosec` findings and 1 unchecked
  `errcheck` surfaced immediately on the first real run).
- Files: `internal/agent/tools/*.go` (46 files); the gap is `.golangci.yml:53,81`.
- Current mitigation: the package has substantial hand-written test coverage and the
  sandbox-routing fail-closed invariant (`errBackendUnavailable` denies rather than falls
  back — `internal/sandbox/usersandbox/router.go:36-40`) is well tested in
  `docker_integration`. Static analysis is a different, complementary signal that is
  currently absent.
- Recommendations: remove the exclusion (see Tech Debt fix approach); triage the G304/G301
  findings specifically, since path-handling bugs in exactly this package are the highest-
  value target for a sandbox-escape or path-traversal attempt.

**`CalendarMCPAdminToken` ships a well-known default that strict-profile validation does not reject:**
- Risk: `internal/config/config.go:529` sets
  `CalendarMCPAdminToken: envDefault("AURA_PIM_MCP_ADMIN_TOKEN", "changeme-aura-pim-local")`.
  This Bearer token is injected server-side on every forward to the `aura-pim-mcp` sidecar's
  `/admin` API (`internal/agui/connect_pim_api.go:260` — `req.Header.Set("Authorization",
  "Bearer "+s.calendarMCPToken)`; `cmd/aura/integrations_proxy.go:37,48-52` — the same
  literal default, `pimAdminTokenDefault`, duplicated as a second source of truth).
  **VERIFIED**: unlike the two secrets that ARE checked —
  `internal/config/config_validate.go:218-230` (`gateObjectStoreCreds`, rejects the sample
  object-store access/secret key under `p.Strict()`) and the neighbouring
  `gateGarageRPCSecret` (rejects an empty Garage RPC secret under `p.Strict()`) — there is
  **no `gate*` function anywhere in `internal/config/config_validate.go` that checks
  `CalendarMCPAdminToken` against its default**, in any profile.
- Files: `internal/config/config.go:237,529`; `internal/config/config_validate.go` (absence);
  `cmd/aura/integrations_proxy.go:35-52`.
- Current mitigation: `compose.yaml:242,983` both use
  `${AURA_PIM_MCP_ADMIN_TOKEN:?AURA_PIM_MCP_ADMIN_TOKEN required in .env}`, so a
  docker-compose deployment cannot boot with the default — and the sidecar itself is bound
  to `127.0.0.1:${AURA_PIM_MCP_PORT:-8093}:8080` (`compose.yaml:994`), so it is not
  internet-reachable by default either. The exposure window is a non-compose deployment
  (bare `aura serve` binary with a manually-composed environment) or an
  `AURA_IN_CONTAINER` deployment reaching `aura-pim-mcp:8080` on the shared Docker network
  without the compose `:?` guard.
- Recommendations: add a `gateCalendarMCPAdminToken(p RuntimeProfile)` alongside the
  existing two, rejecting `"changeme-aura-pim-local"` under `p.Strict()`, for defense in
  depth beyond the compose-file guard.

**Memory-recall preload is a self-acknowledged prompt-injection/memory-poisoning surface with no dedicated threat-model artifact:**
- Risk: `internal/runner/runner_context.go:96-110` (`memoryRecall`) runs a
  `memory_search`-equivalent over the current user message and, when
  `AURA_MEMORY_PRELOAD_ENABLED=true`, prepends the result into the assistant-visible
  context under a `<memory_recall>` block (`internal/runner/runner_context.go:14-17,71-75`)
  *before* the model sees the current turn. Because the underlying facts can originate from
  anything the agent has previously written to memory (including content extracted from
  ingested documents or prior tool output), a poisoned earlier memory write could be
  recalled and treated as "your own knowledge" by a later turn — a memory-poisoning /
  indirect-prompt-injection vector.
- Files: `internal/runner/runner_context.go` (the injection seam);
  `internal/config/config.go:66-72,434-436` (the flag, default **false** — VERIFIED);
  `internal/config/config_knobs.go:101-103`.
- Current mitigation: the flag defaults to `false` in code (`envutil.BoolDefault("AURA_MEMORY_PRELOAD_ENABLED", false)`),
  so the surface is opt-in at the code level. Whether a given deployed `.env` sets it to
  `true` is outside this document's scope (forbidden-file rule — `.env` contents are never
  read).
- Recommendations: no dedicated ADR or `/gsd-secure-phase` threat-model document for this
  seam was found under `docs/adr/` (0042 covers provenance/erasure, not
  injection/poisoning trust boundaries) or `docs/superpowers/`. Before defaulting this on
  in any profile, the ingestion→memory trust boundary and fail-open/fail-closed scoping
  need an explicit write-up, per the gap already tracked in prior session notes.

**`docker_integration` tier (sandbox lifecycle + egress) runs live in CI but is invisible to the coverage floor:**
- Risk: `internal/sandbox/usersandbox` (DockerBackend lifecycle/exec/egress) and the routed
  branches in `internal/agent/tools` are exercised by real containers in
  `.github/workflows/ci.yml:1637-1706` (job `sandbox-docker-integration`), but
  `scripts/coverage_gate.sh` only ever runs with `AURA_COVERAGE_TAGS=db_integration`
  (`scripts/coverage_gate.sh:29`; confirmed identical in `ci.yml` and `skills.yml`), so this
  tier contributes **zero** measured statements toward the 85% floor.
- Files: `internal/sandbox/usersandbox/*.go`; `.github/workflows/ci.yml:1637-1706`;
  `scripts/coverage_gate.sh:29`.
- Current mitigation: this is openly documented in three places (`CLAUDE.md`,
  `docs/ARCHITECTURE.md:206-222`, and the job's own inline comments), and the project rule
  ("daemon-gated code needs daemon-free unit tests for its pure logic") is followed in
  practice — `egress_test.go` and `router_tools_test.go`-style daemon-free tests exist
  alongside every daemon-gated file in the package. Rated Medium rather than High because
  the behavioural signal (does the box actually work) IS proven live on every push; only
  the *coverage number* undercounts it.
- Recommendations: none required beyond continuing the daemon-free-test discipline already
  in place; flagged here for completeness since the instructions asked this surface be
  quantified precisely.

## Performance Bottlenecks

**Conversation history retrieval uses a fixed 50-turn cap unrelated to the model's context window:**
- Problem: `internal/conversations/context.go:33` defines
  `defaultHistoryHardCapTurns = 50` — a row-count fetch cap, not a token-budget cap. A model
  with a large context window and a long conversation gets the same 50 most-recent turns as
  a model with a small one, regardless of how many tokens that leaves unused.
- Files: `internal/conversations/context.go:33,107-131` (`hardCap()`).
- Cause: **CLAIMED (in-progress work, not yet merged at `last_mapped_commit`)** — the active
  branch (`feat/history-token-budget`) carries
  `docs/superpowers/plans/2026-08-09-history-token-budget.md`, which states the current
  50-row cap "has no relationship to the model's context window" and that a token-budget
  walk would recover the wasted portion; the plan's own framing puts the range at
  "22–72% of every long conversation" discarded. That specific percentage is the plan
  author's claim, not independently re-derived here. **VERIFIED**: the target replacement
  file `internal/conversations/context_budget.go` does not exist yet at
  `last_mapped_commit` (`git log` shows no history for that path) — the fix is designed but
  unimplemented.
- Improvement path: implement the plan (`internal/conversations/context_budget.go`,
  `HistoryBudget()` derived from `AURA_MODEL_CONTEXT_WINDOW` via `min(correctnessCap,
  ContextWindow/2)`), already scoped with a small-window-behaviour-preserving test
  requirement in the plan's own constraints section.

**No current live retrieval-latency baseline for the ArcadeDB-backed memory/document path at large corpus size:**
- Problem: `docs/aura-quality-snapshot.md` marks both "GraphRAG retrieval recall@5 @ 100K
  corpus" and "Vector search p95 latency @ 100K corpus" **`(RETIRED)`** (lines 51-52),
  dated 2026-08-02 — the capability they measured (the Neo4j-backed GraphRAG document
  path) was deleted along with the graph store.
- Files: `docs/aura-quality-snapshot.md:51-52`.
- Cause: intentional removal (`docs/adr/0038-graph-store-license-neo4j-gplv3-vs-arcadedb-apache.md`),
  not a regression.
- Improvement path: the replacement retrieval path (ArcadeDB-native full-text + vector
  fusion, per `arcadedb-native-retrieval-proven` prior work) has its own measured numbers
  for the memory substrate (`docs/aura-quality-snapshot.md` "Agent Memory" section, p50/p95
  operator-path latency), but there is no equivalent 100K-scale document-corpus benchmark
  recorded in this file as of 2026-08-13 — a gap worth closing before claiming parity with
  the retired baseline, not evidence that the new path is slow.

## Fragile Areas

**`internal/agent/tools` — largest owned surface, and until fixed (see Tech Debt), the only one with zero static-analysis coverage:**
- Files: `internal/agent/tools/*.go` (46 non-test files, 8,621 LOC — the single largest
  directory of non-test Go source in the repo by a wide margin; `internal/agent` itself,
  its parent, adds another 7,889 LOC across 42 files).
- Why fragile: it is both the highest-traffic surface (every tool call passes through it)
  and, until the lint exclusion is removed, the one place `gosec`/`revive`/`staticcheck`
  cannot catch a regression before merge.
- Safe modification: the package has extensive tests (`docker_integration`-tagged for the
  daemon-gated paths, unit-tagged for the pure logic) — run
  `go test ./internal/agent/tools/...` and, for anything touching `shell_exec`/`fs_*`/
  `send_file`, the `docker_integration` tier locally before pushing.
- Test coverage: strong at the unit level; the routed (sandbox) branches are only proven
  live in CI's `sandbox-docker-integration` job (see Security Considerations above).

**`internal/agui` — largest package by total LOC, spans auth/SSE/governance/files:**
- Files: `internal/agui/*.go` (72 non-test files, 14,019 LOC — SSE gateway, cookie/Authula
  auth (`auth.go`, `auth_cookie.go`), governance read/write API, asset/file download,
  conversation branching, deprovisioning).
- Why fragile: breadth of concerns in one package (auth boundary + streaming protocol +
  governance mutation surface) means a change in one area can have non-obvious blast radius
  in another; the AG-UI boundary gate (`scripts/agui_boundary_check.sh`, run in CI at
  `.github/workflows/ci.yml` "AG-UI boundary gate (D-17, SC2)") exists specifically because
  of this.
- Safe modification: follow the existing per-concern file split (already granular — average
  ~195 LOC/file); do not add cross-cutting logic to `server.go` or `client.go` without
  checking the boundary gate.

**Files sitting at or within ~10 lines of the 600-LOC cap — the next non-trivial edit will force a split:**
- Go (non-test): `internal/arcadedb/client.go` (595), `internal/config/config.go` (592),
  `internal/mcp/client.go` (583), `internal/agent/llm_agent.go` (579),
  `internal/conversations/store.go` (574), `internal/agui/onboarding_provision.go` (570).
- Go (test, same 600-LOC cap applies per `scripts/check-file-size.sh`):
  `internal/agui/server_run_resume_test.go` (600, at the cap exactly),
  `internal/agent/tools/skill_write_test.go` (596),
  `internal/conversations/context_boundary_test.go` (593),
  `internal/agent/workflow/loop_test.go` (590), `internal/agent/budget_test.go` (589).
- TypeScript/TSX: `web/src/chat/Composer.tsx` (600, at the cap exactly),
  `web/src/chat/ExternalStoreChat.tsx` (598).
- No file in the repository currently exceeds 600 LOC (verified via
  `git ls-files '*.go' '*.ts' '*.tsx' | xargs wc -l`, excluding `internal/db/sqlc/`
  generated code and `node_modules`/`dist`); the cap is holding project-wide. This list is
  provided so the next person touching any of these files knows a split is imminent rather
  than discovering it mid-edit.

## Scaling Limits

**The 85% coverage floor is an aggregate, so a badly-covered package can hide indefinitely (see Tech Debt above for the primary write-up):**
- Current capacity: any single package can sit arbitrarily low as long as
  `internal/...`'s combined covered/total statement ratio clears 85%.
- Limit: there is no code-level alarm for a specific package regressing to 0% coverage if
  the rest of the tree compensates.
- Scaling path: add a per-package minimum to `scripts/coverage_gate.sh`, or accept the
  aggregate model and correct CLAUDE.md's wording — this is a policy decision, not
  something to silently change.

## Dependencies at Risk

**GHCR published package version cannot be deleted via API (EXT-004, `docs/audit/README.md`):**
- Risk: version `845339375` (digest
  `sha256:764b4b3e58ebb1e627c54b6c10a0e9889b43caa53cb06728d12803ca53415628`) is untagged
  but GitHub refuses API deletion once a package version passes 5,000 downloads; all known
  tags pointing to it are already absent.
- Impact: an orphaned, untagged, unremovable container image sits in the public registry
  indefinitely — not a live vulnerability by itself, but supply-chain hygiene debt that
  the project cannot resolve unilaterally.
- Migration plan: per `docs/audit/README.md`, closure requires GitHub Support to delete the
  protected version; owner is listed as "GitHub Support," i.e. external and out of the
  team's control.

## Missing Critical Features

**Five release-gating items remain externally blocked (`docs/audit/README.md`, dated 2026-07-31 — read directly, not re-verified against a live environment):**
- EXT-001 — Calendar MCP has no authorized test account; direct calls fail closed.
- EXT-002 — Email MCP has no authorized sender/recipient account.
- EXT-003 — WhatsApp bridge is healthy but unpaired (`waiting_qr`).
- EXT-004 — the GHCR image above (see Dependencies at Risk).
- EXT-005 — `send_file` reaches its contextual guard but has no authorized channel delivery
  receipt to prove end-to-end artifact delivery.
- Blocks: per the doc's own verdict table, "Release readiness: **NO-GO** while any row
  remains," and all five require operator action (connecting real accounts/devices) or
  GitHub Support, not code changes. The doc states the machine check is
  `PYTHONPATH=scripts python3 scripts/audit_closure_gate.py` — not re-run as part of this
  mapping pass.

## Test Coverage Gaps

**`arcadedb_integration` tier — 11 test files, compile-checked but never executed anywhere in the pipeline:**
- What's not tested (in CI, or by any Makefile target, or by any script): the whole LOCOMO
  memory-quality suite and the live-ArcadeDB adapter tests.
- Files (all `//go:build arcadedb_integration`, confirmed via `git grep`):
  `internal/arcadedb/locomo_analyzer_test.go`, `internal/arcadedb/locomo_dense_test.go`,
  `internal/arcadedb/locomo_facts_test.go`, `internal/arcadedb/locomo_native_test.go`,
  `internal/arcadedb/locomo_test.go`, `internal/arcadedb/memory_integration_test.go`,
  `internal/arcadedb/memory_vector_live_test.go`, `internal/arcadedb/testclient_test.go`,
  `cmd/arcadedb-mcp/memory_live_integration_test.go`,
  `cmd/aura/memory_latency_live_test.go`,
  `cmd/aura/serve_deprovision_memory_integration_test.go`.
- Risk: the correctness of the memory substrate's SQL and its live-ArcadeDB behaviour
  (temporal facts, purge/deprovision, embedding search, LOCOMO quality regressions) is
  proven only when a developer remembers to run it by hand.
- **Precision correction to CLAUDE.md's claim.** CLAUDE.md states this tier is "not even
  `go vet`-compiled." **VERIFIED FALSE as of `last_mapped_commit`**: running
  `go test -run '^$' -tags arcadedb_integration ./internal/arcadedb/... ./cmd/arcadedb-mcp/...`
  compiles cleanly (`ok ... [no tests to run]`), and `bash scripts/tagged_tier_compile.sh --plan`
  lists `internal/arcadedb`, `cmd/arcadedb-mcp`, and `cmd/aura` under the
  `arcadedb_integration` tag — this compile-check DOES run in CI, inside the
  "Compile every discovered Aura tier" step of the `build-and-lint` job
  (`.github/workflows/ci.yml`, `scripts/tagged_tier_compile.sh`). What CLAUDE.md gets
  right, and what remains the real, precisely-quantified gap: **no CI job, Makefile
  target, or script actually executes these tests with real assertions** — `grep -rn
  "arcadedb_integration" .github/workflows/*.yml` returns zero matches for an execution
  invocation (only the dynamically-discovering compile gate touches the tag by name), and
  `make arcadedb-integration` runs a different, Python-based evaluator instead (see Tech
  Debt above). CLAUDE.md also states "10 test files carry it" — the current count,
  verified above, is **11**.
- Priority: High. Recommend either a dedicated `make arcadedb-integration-tests` target
  wired into a CI job with a live ArcadeDB (mirroring how `sandbox-docker-integration` was
  added for `docker_integration`), or explicit removal of the dead test files if the LOCOMO
  suite is considered superseded by `aura.agent-memory-eval/v1`.

**`internal/agent/tools` — no static-analysis coverage (see Security Considerations for the full write-up):**
- What's not tested: static bug classes (`gosec`/`staticcheck`/`revive`/`dupl`/`modernize`)
  across the tool-execution surface.
- Files: `internal/agent/tools/*.go`.
- Risk: see Security Considerations.
- Priority: High.

**TODO/FIXME/HACK/XXX markers — essentially absent (positive finding, recorded for completeness):**
- `grep -rn "TODO\|FIXME\|HACK\|XXX" --include="*.go" internal cmd` (excluding `_test.go`)
  returns exactly **1** hit, and it is a string literal inside a tool description example
  (`internal/agent/tools/search_files.go:71` — `"pattern":"TODO"` as a usage example, not a
  deferred-work marker). Including test files adds 3 more hits, all incidental (a phone
  number placeholder, a comment about a fixed historical bug, and a test string asserting
  "TODO" does NOT appear in shell output). **VERIFIED**: the project's "no TODO orphan"
  discipline (CLAUDE.md "NOT MY WORK... fix on touch. Never Skip.") holds in practice — no
  deferred-work markers exist anywhere in the owned Go source.

**`nolint` suppressions (85 total) — all carry inline justification (positive finding):**
- **VERIFIED**: of 85 `//nolint` occurrences across `internal/` and `cmd/`, all but 4 carry
  a trailing `// reason` comment. The 4 without one are self-evident `exec.Command`/
  `os.Unsetenv` calls (`internal/cron/handlers/backup.go:209` — `pg_dump`;
  `internal/mcp/client.go:155` — MCP server subprocess spawn;
  `internal/procgroup/procgroup_windows.go:37` — `taskkill`;
  `cmd/aura/config.go:151` — `os.Unsetenv("OPENROUTER_API_KEY")`), where the suppressed
  rule (`gosec`/`errcheck`) is apparent from context. No bare, unexplained suppression of a
  non-obvious rule was found.

