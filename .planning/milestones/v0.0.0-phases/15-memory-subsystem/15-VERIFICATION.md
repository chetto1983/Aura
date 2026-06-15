---
phase: 15-memory-subsystem
verified: 2026-06-12T12:00:00Z
status: passed
score: 5/5 must-haves verified
overrides_applied: 0
human_verification:
  - test: "CR-01 profile-exclusion bypass: create a non-default MCP profile, do 'aura mcp install memory' in the default profile, then switch to the non-default profile and start Aura — observe whether the agent still receives the 16 memory__* tools (they should NOT be injected if the operator curated memory out of the active profile)"
    expected: "When memory is installed under profile 'default' but the active profile is 'work' (which does not include memory), the catalog recipe must NOT be re-injected — only 'aura mcp disable memory' (Enabled=false) should suppress injection. Current code checks managed.MCPServers[memory].Enabled only; a profile-excluded entry has Enabled=nil, so the inject fires. Whether this is a defect or intended 'always-on' product behavior requires an explicit product decision and possibly a PRD amendment."
    why_human: "The injectDefaultOnMemory function checks Enabled==false for opt-out but does NOT check whether 'memory' is in the active profile's server names. Verifying this requires creating a real managed config with two profiles and observing whether the inject correctly skips an entry that is installed but excluded from the active profile — not testable by unit test grep."
  - test: "CI memory_integration job actually passes on the main branch (not just compiles): confirm the GitHub Actions memory-integration-test job completed green in recent history"
    expected: "The job runs docker compose up --build aura-agent-memory-mcp, brings up Neo4j + embed, and executes 'go test -tags memory_integration' with AURA_AGENT_MEMORY_MCP_URL exported. All five tests (TestMemoryLiveMount, TestMemoryCLI, TestMemoryReasoningTrace, TestMemoryDedupNewEntityActionNone, TestMemoryLoopRecall) pass in CI."
    why_human: "Live integration tier cannot be run without Docker stack. Runtimes documented in SUMMARY (0.28/0.38/0.37/0.29/8.69s) prove these were not skips, but CI green status on the committed branch requires human confirmation against GitHub Actions run history."
---

# Phase 15: Memory Subsystem Verification Report

**Phase Goal:** Memory Subsystem — adopt the forked neo4j-labs/agent-memory MCP sidecar off-the-shelf (amendment #61/#62), superseding the bespoke 11a/11b/11d/11e build. Owned-surface = Go wiring (default-on trusted recipe mount, fail-soft) + `aura memory` operator CLI + reproducible compose build + live memory_integration validation tier.
**Verified:** 2026-06-12T12:00:00Z
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | PRD amendment #62 records UX-06..09 re-scopes and lands before any Go code | VERIFIED | Commit `05e680e4` (doc-only, no `.go` files) precedes all Go commits. `prd.md` contains the Amendment #62 block under §Slice 11. REQUIREMENTS.md and ROADMAP.md re-stated. |
| 2 | `memory` recipe is a trusted streamable-HTTP managed recipe in BuiltInCatalog() — default-on with fail-soft (no `aura mcp install` required) | VERIFIED | `internal/mcp/manager/catalog.go:131-142` shows the `memory` CatalogEntry (Type=streamable_http, Trust=trusted_recipe, URL from AURA_AGENT_MEMORY_MCP_PORT). `internal/config/config_mcp.go:23-38` implements `injectDefaultOnMemory`. `TestMemoryDefaultOn`, `_RespectsDisable`, `_RespectsExplicitInstall`, `_EnvServersOverrideWins` all PASS. Unit suite green (37 packages, 0 failures). |
| 3 | `aura memory <verb>` operator CLI round-trips raw `memory_*` tools via managed-server resolution, bypassing the agent loop | VERIFIED | `cmd/aura/memory.go` implements `runMemory`/`runMemoryCommand`/`memoryVerbToTool` (254 LOC, ≤600). `case "memory": runMemory(os.Args[2:])` in `main.go:48`. Usage string includes `memory <sub>`. `TestMemoryVerbMapping` (16 verbs) and `TestMemoryVerbMappingNegativeCases` (7 cases) PASS. All dispatched tool names are RAW wire names (no `memory__` prefix confirmed by grep). |
| 4 | Reproducible compose build — `docker compose build aura-agent-memory-mcp` from git, fork vendored at c1c2d65 | VERIFIED | `docker/agent-memory/Dockerfile` exists and contains `pip install --no-cache-dir -e ".[mcp,google,openai]"` from vendored src/, commit c1c2d65 recorded in Dockerfile header (Pitfall 5 documented). `compose.yaml` contains `context: ./docker/agent-memory`, `image: aura-agent-memory-mcp:local`, `pull_policy: never`. Fork `src/` directory tree verified present (100+ files). |
| 5 | Live `memory_integration` tier proves 16-tool mount, `aura memory` seed/read + reasoning trace, agent recall loop (tool_search → memory__memory_search → text_response), and dedup non-merge — with CI no-skip-as-green gate | VERIFIED | Four test files exist with `//go:build memory_integration`: `internal/agent/mcptools/memory_integration_test.go` (TestMemoryLiveMount), `cmd/aura/memory_integration_test.go` (TestMemoryCLI, TestMemoryReasoningTrace, TestMemoryDedupNewEntityActionNone), `internal/agent/memory_recall_integration_test.go` (TestMemoryLoopRecall). All t.Fatal under $CI when URL unset. CI job `memory-integration-test` in `.github/workflows/ci.yml:478-556` exports AURA_AGENT_MEMORY_MCP_URL=http://127.0.0.1:8091/mcp/, builds the sidecar, and runs the tier. SUMMARY documents live runtimes (0.28/0.38/0.37/0.29/8.69s — not skip tells). KV-cache invariant confirmed (D-04, 22 unchanged hashes). Advisory recall@5=1.000/p95=44.55ms appended to `docs/aura-quality-snapshot.md`. |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `prd.md` | Amendment #62 block | VERIFIED | Present under §Slice 11; all four UX-0[6789] re-scoped; env catalog (AURA_AGENT_MEMORY_MCP_PORT/IMAGE/URL); 384d-already-live note |
| `.planning/REQUIREMENTS.md` | UX-06..09 re-stated | VERIFIED | All four IDs re-scoped with `[ ]` checkbox markers preserved, citing amendment #62 |
| `.planning/ROADMAP.md` | Phase 15 Success Criteria re-derived | VERIFIED | Five re-derived criteria matching actual deliverables; bespoke retained as superseded reference |
| `internal/mcp/manager/catalog.go` | `memory` CatalogEntry (streamable_http, trusted_recipe) | VERIFIED | Lines 127-142, URL from AURA_AGENT_MEMORY_MCP_PORT (default 8091), 157 LOC |
| `internal/config/config.go` | default-on inject-unless-disabled seam | VERIFIED | `injectDefaultOnMemory(policies, managed, envServers)` at line 352, after env delete loop |
| `internal/config/config_mcp.go` | `injectDefaultOnMemory` helper | VERIFIED | 38 LOC; precedence: policies → envOverridden → Enabled=false → catalog inject |
| `internal/config/config_mcp_default_on_test.go` | TestMemoryDefaultOn + 3 variants | VERIFIED | 4 tests pass (default-on, disable, explicit install, env override) |
| `cmd/aura/memory.go` | `runMemory` verb router | VERIFIED | 254 LOC; 16 verbs + trace subverbs; RAW tool names; 20s timeout; imports mcp.OpenServer |
| `cmd/aura/main.go` | `case "memory"` + `usage()` entry | VERIFIED | `case "memory": runMemory(os.Args[2:])` at line 48; `memory <sub>` in usage() at line 93 |
| `cmd/aura/memory_test.go` | TestMemoryVerbMapping + negative cases | VERIFIED | 16-row table PASS; 7 negative cases PASS; TestMemoryNotConfigured PASS |
| `docker/agent-memory/Dockerfile` | python:3.11-slim + pip install from vendored src/ | VERIFIED | Pitfall-5 comment; installs -e ".[mcp,google,openai]"; EXPOSE 8080; gcc installed |
| `docker/agent-memory/src/` | Fork vendored at c1c2d65 | VERIFIED | Full Python package tree present (100+ files) |
| `compose.yaml` | aura-agent-memory-mcp with build.context | VERIFIED | `context: ./docker/agent-memory`, `image: aura-agent-memory-mcp:local`, `pull_policy: never` |
| `internal/agent/mcptools/memory_integration_test.go` | TestMemoryLiveMount | VERIFIED | 122 LOC; build tag memory_integration; t.Fatal under $CI; mounts via MountManagedServer; asserts count==rawCount, all Deferred, all memory__* |
| `cmd/aura/memory_integration_test.go` | TestMemoryCLI + TestMemoryReasoningTrace + TestMemoryDedupNewEntityActionNone | VERIFIED | 229 LOC; build tag memory_integration; t.Fatal under $CI; drives real runMemoryCommand |
| `internal/agent/memory_recall_integration_test.go` | TestMemoryLoopRecall | VERIFIED | 188 LOC; build tag memory_integration; t.Fatal under $CI; real LlmAgent + FakeClient; tool_search → memory__memory_search → text_response path |
| `.github/workflows/ci.yml` | memory-integration-test job | VERIFIED | Job at line 478; exports AURA_AGENT_MEMORY_MCP_URL; builds sidecar; runs tier at line 552 |
| `docs/aura-quality-snapshot.md` | Advisory memory section | VERIFIED | Section "Memory (Phase 15, agent-memory MCP)" with recall@5=1.000 p95=44.55ms marked ADVISORY |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `internal/config/config.go loadMCPServers` | `injectDefaultOnMemory` | After env delete loop at line 352 | VERIFIED | `injectDefaultOnMemory(policies, managed, envServers)` called before early-return check |
| `injectDefaultOnMemory` | `mcpmanager.LookupCatalog("memory").Server` | Inject into policies map | VERIFIED | config_mcp.go:33-37 performs the lookup and injection |
| `cmd/aura/main.go switch` | `runMemory(os.Args[2:])` | `case "memory":` | VERIFIED | main.go:48 |
| `cmd/aura/memory.go runMemoryCommand` | `mcp.OpenServer(ctx, "memory", server).CallTool` | `effectiveManagedMCPServer("memory")` → OpenServer → CallTool | VERIFIED | callMemoryTool at memory.go:220-240; uses RAW tool names |
| `compose.yaml aura-agent-memory-mcp build.context` | `docker/agent-memory/Dockerfile` | build context directory | VERIFIED | compose.yaml:101-104 |
| `memory_integration tests` | `AURA_AGENT_MEMORY_MCP_URL` | t.Fatal under $CI when unset | VERIFIED | All three test files check the env var; t.Fatal pattern confirmed |
| `.github/workflows/ci.yml` | `memory_integration` tier | env export + tier run | VERIFIED | Line 509 exports URL; line 552 runs the tests |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|-------------------|--------|
| `cmd/aura/memory.go callMemoryTool` | `text` from `cli.CallTool` | live `mcp.OpenServer` → streamable-HTTP → sidecar | Yes (live sidecar, proven by integration tests) | FLOWING |
| `internal/config/config_mcp.go injectDefaultOnMemory` | `policies["memory"]` | `mcpmanager.LookupCatalog("memory").Server` | Yes (catalog entry is substantive, not stub) | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Unit suite clean (37 packages) | `go test ./...` | 37 packages PASS, 0 failures | PASS |
| Build clean | `go build ./...` | No output (success) | PASS |
| Catalog contains memory recipe | `go test ./internal/mcp/manager/ -run TestCatalogIncludesMemoryStreamableHTTPRecipe` | PASS | PASS |
| Default-on seam works (fresh machine) | `go test ./internal/config/ -run TestMemoryDefaultOn` | PASS | PASS |
| Disable seam works | `go test ./internal/config/ -run TestMemoryDefaultOn_RespectsDisable` | PASS | PASS |
| Verb mapping (16 verbs RAW names) | `go test ./cmd/aura/ -run TestMemoryVerbMapping` | PASS (all 16 sub-tests) | PASS |
| Negative verb cases | `go test ./cmd/aura/ -run TestMemoryVerbMappingNegativeCases` | PASS (7 cases) | PASS |
| TestMemoryNotConfigured (disabled) | `go test ./cmd/aura/ -run TestMemoryNotConfigured` | PASS | PASS |
| Race detection (config) | `go test -race ./internal/config/ -run MemoryDefaultOn` | PASS | PASS |

### Probe Execution

No probes declared in PLAN files for this phase. The `memory_integration` live tier is the functional equivalent — it was run locally by the executor (documented runtimes in 15-05-SUMMARY.md) and is gated in CI. Live re-execution requires Docker stack (deferred to human verification item 2).

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| UX-06 | 15-01 | Doc-RAG ingestion → DEFERRED (amendment #62) | SATISFIED | REQUIREMENTS.md marks `[ ]` with "DEFERRED to a future phase" and cites amendment #62; PRD amendment #62 records the deferral |
| UX-07 | 15-01 | Entity resolution owned by adopted package; Leiden deferred (#27) | SATISFIED | REQUIREMENTS.md UX-07 re-stated "Entity resolution owned by the adopted package (spike 034); Leiden community detection deferred (#27)" |
| UX-08 | 15-03, 15-04, 15-05 | Advisory recall@5/p95 snapshot (re-scoped from hard gate) | SATISFIED | `docs/aura-quality-snapshot.md` contains advisory section (recall@5=1.000, p95=44.55ms, marked ADVISORY); operator CLI provides the recall surface measured |
| UX-09 | 15-02, 15-05 | Agent-written reasoning/insight recalled on demand via the package's reasoning-trace tools; no messages[2], no journal cron | SATISFIED | `TestMemoryReasoningTrace` proves trace start/step/complete/recall via graph_query; no messages[2] injection (D-04 confirmed by cache_invariant_audit.sh 22 unchanged hashes); `TestMemoryLoopRecall` proves tool_search → memory__memory_search → text_response |

All four requirement IDs mapped in PLAN frontmatter are accounted for. REQUIREMENTS.md traceability table shows UX-08 and UX-09 as Complete, UX-06 and UX-07 as Pending (which is correct — they are re-scoped/deferred, not delivered).

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `compose.yaml` | 95-96 | Stale "Phase 15 spike" comment (two dangling lines before the service) | Info | Does not affect behavior; documented as IN-04 in code review |
| `internal/mcp/manager/catalog.go` | 15-21 | AURA_AGENT_MEMORY_MCP_PORT env interpolated without numeric validation | Warning | Non-numeric values (e.g. `8091@evil.example`) yield a URL with host `evil.example` (userinfo trick). Documented as WR-01 in code review. No TBD/FIXME/XXX found. |
| `cmd/aura/memory_test.go` | 91-105 | `t.Fatalf` in httptest handler goroutine (wrong goroutine for FailNow) | Warning | Can misreport failures on the marshal/encode error path. Documented as WR-05 in code review. |
| `internal/config/config_mcp.go` | 23-38 | Explicit-entry check (rule 1) keyed on filtered `policies` map, not managed doc | Critical (CR-01) | Profile-exclusion bypass: memory is re-injected as the catalog recipe even when the operator excluded it from the active profile. Documented as CR-01 in code review. The unit tests (`TestMemoryDefaultOn_RespectsExplicitInstall`) only exercise the default-profile path. |

No TBD, FIXME, or XXX markers found in any Phase 15 modified files (grep confirmed empty across all 7 primary files).

### CR-01 Assessment

The code review identified a Critical defect: `injectDefaultOnMemory` checks `managed.MCPServers["memory"].Enabled` for the disable guard, but checks `policies["memory"]` (built from `RunnableManagedServers`, which filters by active profile) for the "explicit entry wins" guard. This means:

- If memory is installed under profile `default` and the active profile is `work` (no memory), `policies["memory"]` is absent, `managed.MCPServers["memory"].Enabled` is nil → inject fires → catalog recipe mounts silently, bypassing profile curation.
- For all other MCP servers, removing from the active profile unmounts them. For memory, the catalog recipe overrides the profile decision.

This is a behavioral defect in the default-on seam. However:
1. It affects only operators who use multi-profile MCP configuration (Phase 16 feature).
2. The shipped CLI flows in Phase 15 only exercise the single-profile default path.
3. The unit tests verify the intended default-profile semantics correctly.
4. The SUMMARY's declared "5/5 must-haves verified" were about the default-on mount on a fresh machine — which does work correctly.
5. Whether "always-on across profiles" is the intended product behavior or a defect requires a product decision.

This is documented here and in 15-REVIEW.md (CR-01). It does not prevent the Phase 15 goal from being achieved for the stated single-user `local` deployment model (D-10), but it is a WARNING-level deviation from the code review's contract. No override is appropriate because the fix path is clear (check managed doc, not policies map).

### Human Verification Closed

#### 1. CR-01 Profile-Exclusion Bypass — Product Decision

**Test:** Create two MCP profiles. Install memory under the `default` profile. Switch to a `work` profile that does not include memory. Start `aura chat`. Observe whether `aura tools` shows `memory__*` tools.

**Expected:** If the product intent is "memory is always-on regardless of profile curation", document this as a product decision (PRD amendment) and remove the misleading "do not override a customized URL" docstring. If the intent is "profile curation respects memory like any other server", apply the CR-01 fix from 15-REVIEW.md (check `managed.MCPServers`, not `policies`).

**Why human:** Requires Docker stack + multi-profile MCP configuration. More importantly, requires a product decision on semantics. The code is internally inconsistent (docstring says "explicit wins" but implementation does not honor profile exclusion).

#### 2. CI memory-integration-test Job Green Status

**Test:** Check GitHub Actions run history for the `memory-integration-test` job on the `tabula-rasa` branch (post Phase 15 commits).

**Expected:** Job passes: docker compose build succeeds, sidecar becomes healthy, all 5 live tests pass, AURA_AGENT_MEMORY_MCP_URL is exported and the tier does NOT skip.

**Why human:** Live integration cannot be re-run in verification without Docker stack. The executor documented live runtimes (0.28/0.38/0.37/0.29/8.69s — confirmed non-skip from the timing), but CI green status on the committed branch requires GitHub Actions history.

### Gaps Summary

No gaps block the Phase 15 goal. All 5 must-have truths are VERIFIED in the codebase. The phase delivers its stated contract: default-on trusted recipe mount, operator CLI, reproducible compose build, and a live memory_integration tier with no-skip-as-green CI gate.

The two human verification items are now closed:
1. CR-01 profile-exclusion semantics resolved by option (b) on 2026-06-12; commit `4d9b6b35` changed default-on memory injection to respect explicit/profile-excluded managed entries, with `TestMemoryDefaultOn_RespectsProfileExclusion` green.
2. CI memory integration confirmed on 2026-06-15 via GitHub Actions run `27536453185` on `tabula-rasa` (head `5f70703f9b417985ee0af818afdb7ca3c80e206d`): job `Memory MCP (memory_integration tier, live agent-memory sidecar)` passed, including step `Live memory_integration tier (16-tool mount + CLI + recall + dedup)`.

The code review (15-REVIEW.md, status: issues_found) documents 1 Critical (CR-01) and 7 Warnings which are advisory for this verification. None of the warnings prevent the phase goal from being achieved.

---

_Verified: 2026-06-12T12:00:00Z_
_Verifier: Claude (gsd-verifier)_
