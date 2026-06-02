---
phase: 06-kv-cache-builder
plan: 02
subsystem: agent/prompt
tags: [kv-cache, prompt-builder, cache-control, provider-aware, fingerprint]
requires:
  - "internal/agent/llm_agent.go (the inline llm.Request{} call site extracted)"
  - "internal/canonicaljson.Marshal (deterministic serializer)"
  - "internal/agent/tools.Registry.RenderToolDefs (cache-stable tool ordering)"
provides:
  - "internal/agent/prompt.PromptBuilder.Build — single wire-Request assembly chokepoint (D-01)"
  - "internal/agent/prompt.PrefixHash — index-set content fingerprint (D-06a)"
  - "internal/agent/prompt.injectCacheControl — dormant provider-aware cache_control seam (D-03)"
  - "internal/llm.Request.ToolsCacheControl — wire-shape cache_control field (D-03)"
affects:
  - "internal/agent/llm_agent.go (Run now routes through builder.Build)"
  - "internal/llm/client.go (Request struct + corrected design comment)"
tech-stack:
  added: []
  patterns:
    - "single assembly chokepoint (PromptBuilder) for messages[0] byte-identity"
    - "provider-keyed branch reading cfg.Provider (never hardcoded)"
    - "canonicaljson -> sha256 fingerprint idiom (reused from canonicalArgs)"
key-files:
  created:
    - internal/agent/prompt/hash.go
    - internal/agent/prompt/hash_test.go
    - internal/agent/prompt/builder.go
    - internal/agent/prompt/cache_anthropic.go
    - internal/agent/prompt/builder_test.go
  modified:
    - internal/llm/client.go
    - internal/agent/llm_agent.go
decisions:
  - "PrefixHash returns a stable empty-digest for an empty/nil index set (no error) — sha256 of zero bytes; tested for nil==empty stability."
  - "The cache_control seam carries only a tools-side marker (Request.ToolsCacheControl='ephemeral') under anthropic; the Anthropic-native block-array wire translation is deferred to Slice 13."
  - "Builder held as a stateless *prompt.PromptBuilder field on LlmAgent, mirroring the existing constructor-initialized field pattern."
metrics:
  duration: ~25m
  completed: 2026-06-02
---

# Phase 6 Plan 02: PromptBuilder Chokepoint + Cache-Control Seam Summary

Extracted the inline `llm.Request{}` construction in `LlmAgent.Run` into a single named `PromptBuilder.Build` chokepoint (D-01), added the index-set `PrefixHash` content fingerprint (D-06a), and wired the dormant provider-aware `cache_control` seam (D-03/D-03a) with `llm.Request.ToolsCacheControl` — all while preserving the byte-identity of `messages[0]`.

## What Was Built

**Task 1 — `PrefixHash` (commit beba14a6):**
`internal/agent/prompt/hash.go` exposes `PrefixHash(msgs []llm.Message, indices []int) (string, error)`. It SHA-256-hashes the messages at the given index set (in supplied order), serializing each via the deterministic `canonicaljson.Marshal` (sorted keys, `json.Number`) rather than a hand-rolled marshaller. Indices `< 0` or `>= len(msgs)` are skipped, so `{0,1,2}` is forward-compatible: it equals `{0}` while only `messages[0]` is present today and transparently extends when Slices 10/11e land. Documented as a content fingerprint, not a security primitive (ASVS V6).

Table-driven tests prove: determinism across repeated calls, invariance under history growth (only index 0 hashed), forward-compat (`{0,1,2}` == `{0}` for a single-message history), canonical key-order equality, content sensitivity, and nil-vs-empty index-set stability.

**Task 2 — `PromptBuilder` + seam (commit e23af55a):**
- `internal/llm/client.go`: added `Request.ToolsCacheControl string`; rewrote the stale "the wire layer is unaware" comment (now false) — the field is wire-shape but the injection *decision* lives in the prompt builder (D-03a, deep-refactor-on-touch). Package doc kept consistent.
- `internal/agent/prompt/builder.go`: `PromptBuilder.Build` reproduces the previous inline construction byte-for-byte (Messages = history as-is, Tools = `reg.RenderToolDefs()` with its cache-load-bearing alphabetical order untouched, scalars from cfg), then calls `injectCacheControl` last.
- `internal/agent/prompt/cache_anthropic.go`: `injectCacheControl(req, provider)` is a pure no-op unless `provider == "anthropic"`, in which case it sets only the tools-side ephemeral marker. Never attaches `cache_control` to history. Provider read from config, never hardcoded.
- `internal/agent/llm_agent.go`: `Run` now calls `a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg)`; the inline `llm.Request{}` is gone. `messages[0]` byte-identity preserved.

Tests cover SC#1u (byte-identical `[0]` over 20 turns + monotonic growth + no-mutation) and SC#3 (anthropic sets the marker, openrouter carries none, never on history, no-op under openrouter).

## Verification

| Check | Result |
|-------|--------|
| `go test ./internal/agent/prompt/ -run TestPrefixHash` | PASS |
| `go test ./internal/agent/prompt/ -run 'TestBuildPrefixStable\|TestCacheControlSeam'` | PASS |
| `go test -race ./internal/agent/prompt/ ./internal/agent/` | PASS (both packages) |
| `go vet ./...` | clean |
| `go build ./...` | succeeds (no import cycle — `internal/agent/prompt` importing `tools`+`llm` is cycle-free, proving D-01a) |
| `golangci-lint run ./internal/agent/... ./internal/llm/...` | 0 issues |
| `grep ToolsCacheControl internal/llm/client.go` | 3 matches (field + 2 comment refs) |
| `grep "prompt\." internal/agent/llm_agent.go` | builder field + constructor call |
| inline `llm.Request{` in llm_agent.go | removed (NONE) |
| File sizes | all touched files < 600 LOC (largest: llm_agent.go 328) |

Race was run on Windows via `BASH_ENV=~/.aura-toolchain.sh` (w64devkit binutils-shadow fix); both touched packages are green under the race detector.

## Threat Model Coverage

- **T-06-01 (Tampering — messages[0] assembly):** mitigated. `Build` reproduces the byte-stable construction; `TestBuildPrefixStable` asserts byte-identity over 20 turns + no in-place mutation of `history[0]`. Runtime gate (06-05) enforces it on the real loop.
- **T-06-03 (Info Disclosure — cache_control seam):** mitigated. The seam carries only a literal `"ephemeral"` marker, never message content/keys; `injectCacheControl` is a no-op under OpenRouter; `TestCacheControlSeam` asserts OpenRouter requests carry no cache_control and history is never marked.

## Deviations from Plan

None — plan executed exactly as written. The `ToolsCacheControl` field models the tools-side breakpoint marker only (the plan's `<behavior>` and PATTERNS §cache_anthropic explicitly scope the Anthropic-native block-array translation to Slice 13); history-message marking is asserted absent.

## Known Stubs

The `cache_anthropic.go` seam is intentionally dormant (D-03): under the day-1 `openrouter` default it is a no-op, and even under `anthropic` it sets only a tools-side string marker — the full Anthropic-native wire translation (system block + last tool def array) is deferred to Slice 13's `LLMRouter`. This is the planned dormant-seam design, not an unwired UI stub; SC#3 tests assert the dormant behavior. No data-flow stubs that block this plan's goal.

## Self-Check: PASSED

- internal/agent/prompt/hash.go — FOUND
- internal/agent/prompt/hash_test.go — FOUND
- internal/agent/prompt/builder.go — FOUND
- internal/agent/prompt/cache_anthropic.go — FOUND
- internal/agent/prompt/builder_test.go — FOUND
- internal/llm/client.go — FOUND (modified)
- internal/agent/llm_agent.go — FOUND (modified)
- commit beba14a6 — FOUND
- commit e23af55a — FOUND
