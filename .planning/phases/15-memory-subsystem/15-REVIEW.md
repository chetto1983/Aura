---
phase: 15-memory-subsystem
reviewed: 2026-06-12T00:00:00Z
depth: standard
files_reviewed: 18
files_reviewed_list:
  - cmd/aura/main.go
  - cmd/aura/main_test.go
  - cmd/aura/mcp_test.go
  - cmd/aura/memory.go
  - cmd/aura/memory_integration_test.go
  - cmd/aura/memory_test.go
  - internal/agent/mcptools/memory_integration_test.go
  - internal/agent/memory_recall_integration_test.go
  - internal/config/config.go
  - internal/config/config_mcp.go
  - internal/config/config_mcp_default_on_test.go
  - internal/mcp/manager/catalog.go
  - internal/mcp/manager/catalog_test.go
  - docker/agent-memory/Dockerfile
  - docker/agent-memory/.dockerignore
  - compose.yaml
  - .github/workflows/ci.yml
  - docs/aura-quality-snapshot.md
findings:
  critical: 1
  warning: 7
  info: 6
  total: 14
status: resolved
---

> **Resolution (2026-06-12):** CR-01 fixed in commit 4d9b6b35 (inject gates on the unfiltered managed doc; regression TestMemoryDefaultOn_RespectsProfileExclusion). WR-01..07 fixed in the follow-up fix(memory) commit: port validation + hostile-port tests, -race on the CI memory tier, tier gate pinned to the gating URL via isolated managed config, pip constraints.txt (113 pins) wired into the Dockerfile and the :local image rebuilt healthy, t.Errorf in handler goroutines, env-cleared 8091 assertions, quality-snapshot globs re-pointed at the real memory surface. Info findings remain advisory.

# Phase 15: Code Review Report

**Reviewed:** 2026-06-12
**Depth:** standard
**Files Reviewed:** 18
**Status:** issues_found

## Summary

Phase 15 registers the `agent-memory` MCP sidecar as a default-on trusted recipe (inject-unless-disabled seam in `internal/config/config_mcp.go`), adds the `aura memory` operator CLI dispatching RAW `memory_*` wire tools over streamable-HTTP, vendors the fork into `docker/agent-memory` with a compose build, and wires a live `memory_integration` CI tier. The verb-to-tool mapping, the unit fake, the no-skip-as-green gates, and the disable/env-override legs of the precedence ladder are correct and well-tested.

The review found one Critical defect: the precedence ladder's rule 1 ("an explicit/operator entry wins — do not override a customized URL") is keyed on the **active-profile-filtered** `policies` map rather than on the managed document, so an explicit memory install that is excluded from the active profile (or trust-blocked) silently drops to rule 4 and gets replaced by the catalog recipe at the catalog URL. Supporting issues: an unvalidated port env interpolated into the recipe URL, a missing `-race` flag on the new CI tier (the only integration tier without it), a gate-env/config-env mismatch in the CLI live tier, and a Dockerfile that claims reproducibility while floating every dependency.

## Critical Issues

### CR-01: Default-on inject overrides operator profile exclusion and customized URL — precedence rule 1 keyed on the wrong map

**File:** `internal/config/config_mcp.go:23-38` (with `internal/config/config.go:314-357`, `internal/mcp/manager/runtime.go:49-78`)
**Issue:** `injectDefaultOnMemory` documents "1. An explicit/operator entry already **in policies** wins (do not override a customized URL)." But `policies` is built from `RunnableManagedServers(managed)`, which iterates only `doc.ProfileServerNames(doc.ActiveProfileName())` and also drops trust-blocked entries. The disable check at line 30 looks at the unfiltered `managed.MCPServers` — but the explicit-entry check at line 24 does not. Consequences, both reachable through shipped CLI flows:

1. **Customized URL silently replaced.** Operator runs `aura mcp install memory` (lands in the `default` profile, possibly with a hand-edited URL in servers.json), then `aura mcp profile create work && aura mcp profile use work`. Memory is now absent from the active profile, so it is not in `policies`; `managed.MCPServers["memory"]` exists with `Enabled == nil`, so the disable guard at line 30 does not fire; the inject falls through and mounts `LookupCatalog("memory").Server` — the catalog URL (`http://127.0.0.1:8091/mcp/`), not the operator's customized one. `aura memory` and the agent loop now talk to whatever listens on 8091.
2. **Profile exclusion bypassed.** For every other MCP server, removing it from the active profile unmounts it. For `memory`, the catalog recipe is re-injected anyway — the only honored opt-out is `Enabled=false`, which is a different operator gesture than profile curation. The same applies to a trust-blocked memory entry (`normalizedTrustForServer == TrustBlocked` filters it from runnables; the inject restores it as `trusted_recipe`).

The phase's own tests (`TestMemoryDefaultOn_RespectsExplicitInstall`, `_RespectsDisable`) only exercise the default-profile path, so this hole is untested.

**Fix:** Key rule 1 and the opt-out on the managed document, not the filtered policies map:

```go
func injectDefaultOnMemory(policies map[string]mcp.ManagedServer, managed mcp.ManagedConfig, envOverridden map[string]mcp.ServerConfig) {
	if _, ok := policies[memoryRecipeName]; ok {
		return
	}
	if _, ok := envOverridden[memoryRecipeName]; ok {
		return
	}
	// ANY explicit entry in the managed doc wins — disabled, trust-blocked, or
	// excluded from the active profile all mean "the operator decided"; never
	// shadow a customized entry with the catalog recipe.
	if _, ok := managed.MCPServers[memoryRecipeName]; ok {
		return
	}
	recipe, ok := mcpmanager.LookupCatalog(memoryRecipeName)
	if !ok {
		return
	}
	policies[memoryRecipeName] = recipe.Server
}
```

If the product intent is instead "default-on even across profiles", that must be decided explicitly (PRD amendment) and the docstring's "do not override a customized URL" promise must be made true either way — the current behavior violates it. Add a regression test: explicit install + non-default active profile → memory NOT injected (or injected with the operator URL, per the decided semantics).

## Warnings

### WR-01: `AURA_AGENT_MEMORY_MCP_PORT` interpolated unvalidated into the recipe URL — can silently retarget the default-on mount off-loopback

**File:** `internal/mcp/manager/catalog.go:15-21`
**Issue:** `memoryRecipeURL` does `fmt.Sprintf("http://127.0.0.1:%s/mcp/", port)` with the raw env value. A non-numeric value is not rejected: `AURA_AGENT_MEMORY_MCP_PORT=8091@evil.example` yields `http://127.0.0.1:8091@evil.example/mcp/`, which parses with host `evil.example` (userinfo trick) — the default-on trusted-recipe mount and every `aura memory` write would silently leave loopback. The env is operator-controlled, but the whole point of the hardcoded `127.0.0.1` is that the recipe is loopback-by-construction; one env var should be able to change the port, not the host. The same value also reaches the test gates (`memoryEndpointOrGate`) and CI docs as a port, reinforcing the numeric contract.
**Fix:** Validate before interpolating:

```go
func memoryRecipeURL() string {
	port := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_PORT"))
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		port = "8091"
	}
	return "http://127.0.0.1:" + port + "/mcp/"
}
```

### WR-02: memory_integration CI tier is the only live tier run without `-race`

**File:** `.github/workflows/ci.yml:552`
**Issue:** `go test -tags memory_integration -count=1 -p 1 ./internal/agent/mcptools/ ./cmd/aura/ ./internal/agent/` omits `-race`. Every sibling tier runs the race detector (`db_integration` line 187, `web_integration` line 303, knowledge line 361, multimodal line 429, telegram line 476). The memory tier exercises exactly the concurrency-sensitive surface (streamable-HTTP transport read/write loops, the agent loop's tool dispatch, the recording fake's mutex) where the detector earns its keep, and CLAUDE.md's Gate-2 discipline mandates race runs per touched package.
**Fix:** `run: go test -tags memory_integration -race -count=1 -p 1 ./internal/agent/mcptools/ ./cmd/aura/ ./internal/agent/`

### WR-03: CLI live tier's gate env does not configure the code under test — coincidental coupling to port 8091 and to the operator's real managed config

**File:** `cmd/aura/memory_integration_test.go:46-60` (with `.github/workflows/ci.yml:509`)
**Issue:** `memoryTierGate` arms the tier when `AURA_AGENT_MEMORY_MCP_URL` (or `_PORT`) is set, implying the URL configures the run. It does not: `runMemoryCommand → callMemoryTool → effectiveManagedMCPServer → config.LoadDB()` resolves the endpoint from the **real managed config** (`AURA_MCP_CONFIG`/default path — never overridden in this file, unlike the unit tests' `withTempMCPConfig`) falling through to the catalog recipe, which reads only `_PORT`. Two consequences: (1) setting `AURA_AGENT_MEMORY_MCP_URL=http://127.0.0.1:9999/mcp/` gates the tests "live" while the CLI still dials 8091 — the tests then fail (or worse, pass against the wrong sidecar listening on 8091); CI only works because its exported URL happens to equal the catalog default. (2) Locally the tier reads the operator's servers.json and writes into the operator's real graph through whatever endpoint that config resolves — if the operator disabled memory, every test fails with "not configured" despite the URL env being set, contradicting the gate's skip message. The other two tiers (`internal/agent/mcptools`, `internal/agent`) dial the env URL directly and do not have this problem.
**Fix:** Make the CLI tier hermetic and env-driven like its siblings: in `memoryTierGate` (or a setup helper), point `AURA_MCP_CONFIG` at a temp servers.json containing a `memory` streamable-HTTP entry whose URL is the gated env value (mirroring `withMemoryServerAt`), e.g.:

```go
url := liveMemoryURLFromEnv() // AURA_AGENT_MEMORY_MCP_URL, or http://127.0.0.1:$PORT/mcp/
withMemoryServerAt(t, url)    // temp AURA_MCP_CONFIG, explicit memory entry
```

### WR-04: Dockerfile claims a reproducible build but floats the base image and every dependency

**File:** `docker/agent-memory/Dockerfile:25,40` (with `docker/agent-memory/pyproject.toml:42-60`)
**Issue:** The header sells the image as "built reproducibly from the VENDORED fork" and says the install shape "mirrors docker/markitdown/Dockerfile (the canonical in-repo pinned-pip sidecar)". Only the top-level source is pinned (c1c2d65). Everything else floats: `FROM python:3.11-slim` has no digest, and `pip install -e ".[mcp,google,openai]"` resolves through the fork's `>=`-floor specifiers (`neo4j>=5.20.0`, `pydantic>=2.0.0`, `openai>=1.0.0`, `google-cloud-aiplatform>=1.38.0`, `fastmcp>=2.0.0,<3`…). Two rebuilds weeks apart produce different dependency sets — a future `docker compose up --build` (the exact CI command, line 532) can pull a pydantic/fastmcp/neo4j-driver major-minor that changes sidecar behavior with zero diff in this repo, which is precisely the supply-chain drift T-15-04-02/T-15-04-SC set out to close. markitdown, by contrast, `==`-pins all four top-level deps.
**Fix:** Generate a constraints file from the known-good image (`pip freeze > constraints.txt`, committed next to the Dockerfile) and install with `pip install --no-cache-dir -c constraints.txt -e ".[mcp,google,openai]"`; pin the base by digest (`FROM python:3.11-slim@sha256:…`). Alternatively soften the header comment to state only the source is pinned — but the constraints route matches the project's stated discipline.

### WR-05: `t.Fatalf` called from httptest handler goroutines in the new memory fake

**File:** `cmd/aura/memory_test.go:91-105` (writeMemoryRPC), also reached from the handler at lines 42-73
**Issue:** `writeMemoryRPC` calls `t.Fatalf` on marshal/encode failure, and it executes inside the `httptest.Server` handler — a goroutine other than the test goroutine. Per `testing` docs, `FailNow`/`Fatalf` must be called from the goroutine running the test; from another goroutine it calls `runtime.Goexit` on the wrong goroutine, so the test does not stop and can hang or misreport when the failure path fires. `go vet`'s testinggoroutine check does not catch this shape (the goroutine is launched by net/http, not a `go` literal). The pre-existing `newMCPHTTPTestServer` in mcp_test.go has the same flaw; this phase replicated the pattern into a new file.
**Fix:** In handler context use `t.Errorf` + `http.Error(w, …, 500)` (the client-side call will then fail on the test goroutine), reserving `t.Fatalf` for test-goroutine code:

```go
if err := json.NewEncoder(w).Encode(...); err != nil {
	t.Errorf("encode rpc: %v", err)
	http.Error(w, "encode failure", http.StatusInternalServerError)
}
```

### WR-06: Catalog URL assertions are not env-hermetic — fail spuriously when `AURA_AGENT_MEMORY_MCP_PORT` is exported

**File:** `internal/mcp/manager/catalog_test.go:50` and `cmd/aura/mcp_test.go:75`
**Issue:** `TestCatalogIncludesMemoryStreamableHTTPRecipe` asserts `memory.Server.URL == "http://127.0.0.1:8091/mcp/"` and `TestMCPRecipesListsBuiltins` asserts the same literal in `recipes --json`, but neither clears `AURA_AGENT_MEMORY_MCP_PORT`, which `BuiltInCatalog()` reads at call time. The var is a documented compose/.env knob (compose.yaml line 157, CI line 509), so it plausibly sits in an operator shell (the WSL E2E runbooks `set -a; source .env`) — and then `go test ./internal/mcp/manager/ ./cmd/aura/` fails with no code defect. The config-package tests got this right (`clearMCPEnv` zeroes the port); these two did not.
**Fix:** Add `t.Setenv("AURA_AGENT_MEMORY_MCP_PORT", "")` at the top of both tests (and in `TestCatalogIncludesTrustedRecipesAndCalendarFixture` if it ever asserts the URL).

### WR-07: Quality-snapshot CI gate globs for the Phase-15 rows do not cover the actual Phase-15 surface

**File:** `docs/aura-quality-snapshot.md:25-26,47-58`
**Issue:** The two Phase-15 matrix rows (GraphRAG recall@5, vector p95) carry CI gate paths `internal/memory/**` and `internal/memory/retrieval/**` — directories that do not exist after the agent-memory MCP pivot (the implementation lives in `cmd/aura/memory.go`, `internal/config/config_mcp.go`, `internal/mcp/manager/catalog.go`, `docker/agent-memory/**`). The new advisory memory section carries no gate path at all. Result: `scripts/quality_snapshot_gate.sh` will never demand re-measurement when the memory wiring or the vendored sidecar changes — the amendment-#20 staleness control is structurally blind to this subsystem, while it worked for this PR only because the rows were updated voluntarily.
**Fix:** Update the rows' (and/or the advisory block's) gate path globs to the real surface, e.g. `cmd/aura/memory*.go, internal/config/config_mcp.go, internal/mcp/manager/catalog.go, docker/agent-memory/**` and keep `internal/db/migrations/neo4j/**`.

## Info

### IN-01: Usage string printed twice on no-args / unknown-verb errors

**File:** `cmd/aura/memory.go:32-38,42,106`
**Issue:** `runMemoryCommand` returns errors that embed `memoryUsage` (lines 42, 106), and `runMemory` then prints `memoryUsage` again unconditionally — the operator sees the full usage block twice. It also prints usage after pure transport errors (sidecar down), where usage is noise.
**Fix:** Drop `memoryUsage` from the returned errors (keep only the specific message) and let `runMemory` own the single usage print; or print usage only for arg-shape errors.

### IN-02: Multi-arg verbs accept empty/whitespace positional args, inconsistent with `arg()`

**File:** `cmd/aura/memory.go:125-166,180-195`
**Issue:** `arg()` rejects whitespace-only values, but `memoryAddFactArgs`/`memoryAddPreferenceArgs`/`memoryStoreMessageArgs`/`memoryRelationshipArgs`/`trace start|step` only check `len(args)`, so `aura memory add-fact "" predicate object` ships an empty subject to the wire and the operator gets the sidecar's error (or a silently degenerate node) instead of a CLI arg error.
**Fix:** Route the fixed positional args of those verbs through `arg()` (or a TrimSpace check) for consistency.

### IN-03: `aura mcp install memory <custom-name>` double-mounts the sidecar

**File:** `internal/config/config_mcp.go:23-27` (with `cmd/aura/mcp.go:94-123`)
**Issue:** Installing the memory recipe under a custom name leaves no `memory` key in policies or the managed doc, so the inject adds the catalog `memory` entry too — both mount the same sidecar, surfacing the 16 tools twice (`memory__*` and `custom-name__*`), doubling manifest entries and confusing tool_search.
**Fix:** Low priority; either document it or have the inject also skip when any managed entry's `Source == "recipe:memory"`.

### IN-04: Stale "spike" framing + dangling duplicate comment on the compose memory service

**File:** `compose.yaml:95-99`
**Issue:** Line 95 ("Phase 15 spike — Granite 97M multilingual embeddings for agent-memory MCP.") is a dangling leftover attached to no service, immediately followed by a second "Phase 15 spike —" comment. The service is now the production default-on sidecar built from the vendored fork, not a spike — the comment understates its load-bearing status.
**Fix:** Delete line 95 and reword line 96 ("Phase 15 — Neo4j Labs agent-memory MCP sidecar (production default-on recipe), built from the vendored fork…").

### IN-05: CI memory tier runs the sidecar with the placeholder LLM key — LLM-dependent memory paths silently degraded

**File:** `.github/workflows/ci.yml:493-509` (with `compose.yaml:122`)
**Issue:** The memory job exports no `OPENROUTER_API_KEY`, so the sidecar boots with `NAM_LLM_API_KEY=aura-memory-mcp-placeholder-key`. The tier passes because the exercised paths (embedding dedup via local granite, store/search, traces) are LLM-free, but any future fork change that routes e.g. `memory_add_entity` extraction or `memory_get_context` summarization through `NAM_LLM` will fail in CI with confusing 401s — or, worse, degrade silently if the package fail-softs.
**Fix:** Add a comment in the job env documenting the deliberate placeholder posture, and/or assert in the tier that the exercised tools never need the cloud LLM (e.g. grep sidecar logs for auth errors after the run).

### IN-06: Flat 20s timeout on every memory verb including `export`

**File:** `cmd/aura/memory.go:228`
**Issue:** `callMemoryTool` wraps open + call in a single 20s deadline. `memory_export_graph` over a populated operator graph (months of accumulation, per the dedup test's own "shared, accumulating graph" framing) can plausibly exceed 20s, making `aura memory export` fail exactly when it matters (backup before a wipe).
**Fix:** Allow a per-verb or env-tunable timeout (e.g. 120s for `export`), keeping 20s as the fail-fast default for interactive verbs.

---

_Reviewed: 2026-06-12_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
