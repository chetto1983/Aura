# Phase 22 — Deferred / Out-of-Scope Items

Discovered during execution; NOT fixed in their discovering plan (scope boundary).

## 22-04

- **Pre-existing, unrelated to internal/agent:** `cmd/aura` test
  `TestProductionContainerArtifactsMatchFatImageContract` fails — `compose.yaml`
  has `AURA_LLM_MODEL: ${AURA_LLM_MODEL:-deepseek/deepseek-v4-flash:nitro}` but
  the container-artifact contract test expects `...:exacto`. Introduced by commit
  `136325dc chore(llm): default AURA_LLM_MODEL to deepseek-v4-flash:nitro`
  (compose vs test drift). Out of scope for 22-04 (agent perimeter hardening);
  belongs to a compose/container fix. No file in plan 22-04 touches compose.yaml
  or cmd/aura.

## 22-05

- **Still present (same pre-existing failure as 22-04):** `go test ./...` at
  22-05 close shows the ONLY failing package is `cmd/aura`, solely from
  `TestProductionContainerArtifactsMatchFatImageContract`
  (`container_artifacts_test.go:92: compose.yaml missing "AURA_LLM_MODEL:
  ${AURA_LLM_MODEL:-deepseek/deepseek-v4-flash:exacto}"` — the `:nitro` vs
  `:exacto` drift from `136325dc`). Out of plan-05 scope (no plan-05 file touches
  `compose.yaml` or `cmd/aura`). All `internal/...` packages pass, including the
  touched `agent`, `agent/tools`, `agent/mcptools`. Belongs to a compose/container
  fix plan.
- **Unrelated dead code in `internal/skills` (out of internal/agent + internal/swarm
  scope, D-09):** `deadcode ./...` flags `internal/skills/manifest.go:53 BM25Corpus`,
  `internal/skills/snippet.go:117 SnippetInvocation`, and
  `internal/skills/validator.go:113 ValidateNameAgainstDir` as unreachable. These
  are NOT AG-### findings (AG-028/044 are the only audit dead-code items, both
  closed) and live outside the Phase-22 audited surface. Logged here, not fixed —
  belongs to an `internal/skills` cleanup.

## Resolution (2026-06-15 — operator directive "fix all findings, even if not yours")

- **RESOLVED — compose `:nitro`/`:exacto` drift:** the operator confirmed the model
  is *fully `.env`-configurable, never hardcoded* and that `:nitro` is the current
  fallback in `compose.yaml`. Rather than re-pin a tag, the fix de-hardcodes the
  contract test: `cmd/aura/container_artifacts_test.go` now asserts the env-override
  PATTERN `AURA_LLM_MODEL: ${AURA_LLM_MODEL:-<non-empty>}` via regexp instead of an
  exact model tag. `internal/llm/config.go` and `prices.go` are intentionally left
  untouched — the built-in fallback there is the documented base tier overridden by
  `.env`/`AURA_LLM_MODEL`, and the test no longer couples CI to whichever tag it
  carries. `go test ./...` is now fully green.

- **NOT A DEFECT — `internal/skills` `deadcode` flags:** investigated each. All three
  are tested and intended, not dead:
  - `SnippetInvocation` — consumed by spike `.planning/spikes/012a-*/main.go:450` and
    covered by `snippet_test.go`.
  - `ValidateNameAgainstDir` — covered by `validator_test.go`; documented as the
    installer name+dir chokepoint (intended API).
  - `BM25Corpus` — covered by `manifest_test.go`; referenced in `internal/agent/tools/skill.go`
    as the overflow-`list` ranker backing.
  `deadcode` reports them (and ~37 other entries: `agenttest` mocks, the
  interface-dispatched `ParallelAgent`/`SequentialAgent` workflow runtime, the
  `telegram` channel runtime, the MCP client/manager) only because it traces from
  `cmd/aura` `main` alone and cannot see the channel/swarm/test entry surfaces.
  Deleting tested code to satisfy the tool would be a regression, so no deletion.
