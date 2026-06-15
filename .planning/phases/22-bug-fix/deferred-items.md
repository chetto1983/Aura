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
