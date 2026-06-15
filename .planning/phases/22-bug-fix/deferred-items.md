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
