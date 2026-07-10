---
phase: 37E-composer-model-reasoning-effort-selector-inserted
plan: 04
type: execute
wave: 3
depends_on: ["37E-02"]
files_modified:
  - internal/agent/prompt/reasoning_policy.go
  - internal/agent/prompt/reasoning_policy_test.go
  - internal/agent/prompt/builder.go
  - internal/agent/llm_agent.go
  - internal/agent/llm_agent_reasoning.go
  - internal/agent/llm_agent_reasoning_override_test.go
  - internal/runner/runner_reasoning.go
  - internal/runner/runner.go
autonomous: true
requirements: [WEBMODEL-01, WEBMODEL-03]
must_haves:
  truths:
    - "A fixed effort override threads request→agent via ctx (`runner.WithReasoningOverride`) → `LlmAgentConfig.ReasoningOverride` → `ApplyFixedReasoning`, which sets `req.Reasoning{Effort,Exclude}` on a reasoning target (OpenRouter OR llama.cpp)"
    - "The fixed path derives `exclude` from `cfg.ShowReasoning` byte-identically to `ReasoningTier.reasoning()` — the selector controls effort, NOT visibility (D-10)"
    - "When no override is set (auto), the adaptive path is byte-identical to today (OpenRouter-only classifier runs; D-04 zero regression)"
  artifacts:
    - path: "internal/agent/prompt/reasoning_policy.go"
      provides: "ApplyFixedReasoning + generalized IsReasoningTarget"
      contains: "func ApplyFixedReasoning"
    - path: "internal/agent/prompt/builder.go"
      provides: "BuildWithReasoningOverride"
      contains: "BuildWithReasoningOverride"
    - path: "internal/runner/runner_reasoning.go"
      provides: "WithReasoningOverride + reasoningOverride ctx accessors"
      contains: "WithReasoningOverride"
  key_links:
    - from: "internal/runner/runner.go"
      to: "internal/agent/llm_agent.go"
      via: "buildAgent reads reasoningOverride(ctx) → LlmAgentConfig.ReasoningOverride"
      pattern: "ReasoningOverride"
    - from: "internal/agent/llm_agent.go"
      to: "internal/agent/prompt/builder.go"
      via: "Run skips adaptive tier when override set and calls BuildWithReasoningOverride"
      pattern: "BuildWithReasoningOverride"
---

<objective>
Ship the per-turn override SEAM (RESEARCH's crux): carry a FIXED `llm.ReasoningEffort` from the HTTP layer (set by plan 06) into `req.Reasoning`, orthogonally to message content, via a ctx value → config field → new builder seam. A fixed level BYPASSES the adaptive classifier and forces `req.Reasoning` on ANY reasoning target (generalized `IsReasoningTarget` = OpenRouter OR llama.cpp, D-08); `auto`/absent leaves today's adaptive path byte-identical (D-04). `exclude` is derived from `cfg.ShowReasoning` exactly as the tier path does (D-10 — effort not visibility).

Purpose: connects the effort symbol to the wire engine (plan 02). Depends on `llm.ReasoningTarget` + `ReasoningEffortMax` from plan 02.
Output: `ApplyFixedReasoning`, `BuildWithReasoningOverride`, generalized `IsReasoningTarget`, the `LlmAgent.reasoningOverride` field, and `runner.WithReasoningOverride` — all covered by daemon-free pure tests.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-CONTEXT.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-RESEARCH.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-PATTERNS.md
@.claude/skills/golang-concurrency/SKILL.md
@internal/agent/prompt/reasoning_policy.go
@internal/agent/prompt/builder.go
@internal/agent/llm_agent.go
@internal/agent/llm_agent_reasoning.go
@internal/runner/runner.go
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: ApplyFixedReasoning + generalized IsReasoningTarget + BuildWithReasoningOverride (pure, tested first)</name>
  <files>internal/agent/prompt/reasoning_policy.go, internal/agent/prompt/reasoning_policy_test.go, internal/agent/prompt/builder.go</files>
  <read_first>
    - internal/agent/prompt/reasoning_policy.go:38-53 (`ApplyAdaptiveReasoning` + `IsOpenRouterReasoningTarget`) and :78-88 + boolPtr:119 (`ReasoningTier.reasoning()` exclude rule to REUSE)
    - internal/agent/prompt/builder.go:99-119 (`BuildWithReasoningTier` / `buildBase` — the sibling to mirror)
    - internal/llm/reasoning_target.go (plan 02 — `llm.ReasoningTarget` + `ReasoningTargetLlamaCpp`)
    - 37E-RESEARCH.md §Seam Map §1 (edit 1f) + §2 + 37E-PATTERNS.md ApplyFixedReasoning/BuildWithReasoningOverride sections
  </read_first>
  <behavior>
    - Refactor `IsOpenRouterReasoningTarget` to delegate to `llm.ReasoningTarget(...) == llm.ReasoningTargetOpenRouter` — behavior-preserving (existing reasoning_policy tests still pass).
    - Test `TestApplyFixedReasoning`: on an OpenRouter target, effort=high → req.Reasoning == {Effort: high, Exclude: boolPtr(!ShowReasoning)}; effort=max → {Effort: max, ...}.
    - Test: on a llama.cpp target (Provider="llamacpp"), effort=medium → req.Reasoning{Effort: medium, Exclude...} (the fixed path fires for llama.cpp too via IsReasoningTarget).
    - Test: on a non-reasoning target (vllm) → req.Reasoning unchanged (Empty).
    - Test: exclude honors ShowReasoning — ShowReasoning=true → Exclude points to false; false → true (D-10 parity, identical to ReasoningTier.reasoning()).
    - Test: ApplyFixedReasoning does NOT gate on cfg.AdaptiveReasoning (works even when adaptive is off).
  </behavior>
  <action>
    Add `func IsReasoningTarget(provider, baseURL string) bool` returning true when `llm.ReasoningTarget(provider,baseURL)` is OpenRouter OR LlamaCpp; refactor `IsOpenRouterReasoningTarget` to delegate to `llm.ReasoningTarget == OpenRouter` (keep the exported name — callers unchanged). Add `func ApplyFixedReasoning(req *llm.Request, provider string, cfg llm.Config, effort llm.ReasoningEffort)`: if `effort == "" || !IsReasoningTarget(provider, cfg.BaseURL)` return; else set `req.Reasoning = llm.ReasoningConfig{Effort: effort, Exclude: boolPtr(!cfg.ShowReasoning)}`. Do NOT gate on `cfg.AdaptiveReasoning` (the fixed override is orthogonal). In builder.go add `func (b *PromptBuilder) BuildWithReasoningOverride(history, reg, provider, cfg, budget, effort llm.ReasoningEffort, activated) llm.Request` = the `BuildWithReasoningTier` body but calling `ApplyFixedReasoning(&req, provider, cfg, effort)` between `buildBase` and `injectCacheControl`. Write reasoning_policy_test.go per the behavior block FIRST. Keep both files ≤600 LOC (they have headroom, ~119 LOC each).
  </action>
  <acceptance_criteria>
    - `go test ./internal/agent/prompt/ -run 'TestApplyFixedReasoning|TestIsReasoningTarget|TestReasoningTier' -race` passes.
    - `ApplyFixedReasoning` sets Effort+Exclude on OpenRouter AND llama.cpp targets, no-ops off-target, and ignores `cfg.AdaptiveReasoning`.
    - `exclude` derivation is byte-identical to `ReasoningTier.reasoning()` (same `boolPtr(!showReasoning)`).
    - `IsOpenRouterReasoningTarget` still returns the same results as before the refactor (regression tests green).
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/agent/prompt/ -race && go vet ./internal/agent/prompt/</automated>
  </verify>
  <done>The fixed effort→ReasoningConfig projection exists as the symmetric sibling of the adaptive path, with D-10 exclude parity and D-08 dual-target gating.</done>
</task>

<task type="auto">
  <name>Task 2: ctx-thread the override through the runner into LlmAgentConfig</name>
  <files>internal/runner/runner_reasoning.go, internal/runner/runner.go</files>
  <read_first>
    - internal/runner/runner.go:48-60 (`WithThreadLockHeld`/`threadLockHeld` — the exact ctx-value pattern to mirror) and :537-565 (`buildAgent` — where `gateway.WithResponder` threads per-turn scope into the config)
    - internal/agent/llm_agent.go (struct ~L102, `LlmAgentConfig` ~L130 — where the new field lands)
    - 37E-RESEARCH.md §Seam Map §1 (edits 1c/1d) + 37E-PATTERNS.md runner_reasoning.go section
  </read_first>
  <action>
    Create internal/runner/runner_reasoning.go: an unexported `reasoningOverrideKey struct{}`, `func WithReasoningOverride(ctx context.Context, effort llm.ReasoningEffort) context.Context`, and `func reasoningOverride(ctx context.Context) (llm.ReasoningEffort, bool)` (mirror `WithThreadLockHeld`/`threadLockHeld`). In `buildAgent` (runner.go), read `reasoningOverride(ctx)` and pass the effort into `agent.LlmAgentConfig{... ReasoningOverride: eff}` (add the field pass-through; the struct field itself is added in Task 3). When absent, pass the zero value ("") — the agent treats "" as auto (no override).
  </action>
  <acceptance_criteria>
    - runner_reasoning.go defines `WithReasoningOverride` + `reasoningOverride` using a private ctx key (no string-keyed ctx value).
    - `buildAgent` populates `LlmAgentConfig.ReasoningOverride` from `reasoningOverride(ctx)`.
    - `go build ./...` green after Task 3 adds the field; round-trip `WithReasoningOverride(ctx, "high")` → `reasoningOverride` returns `("high", true)`; empty ctx → `("", false)`.
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/runner/ -race && go vet ./internal/runner/</automated>
  </verify>
  <done>The override rides ctx from the run handler (plan 06) into agent construction, composing cleanly with the existing TurnWithModelUserMessage split.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Skip-when-fixed branch in LlmAgent — fixed override bypasses the adaptive classifier</name>
  <files>internal/agent/llm_agent.go, internal/agent/llm_agent_reasoning.go, internal/agent/llm_agent_reasoning_override_test.go</files>
  <read_first>
    - internal/agent/llm_agent.go:102-130 (struct + `LlmAgentConfig`), :259-263 (adaptive tier compute in `Run`), :445-450 (`buildRequest` adaptive-vs-plain selector)
    - internal/agent/llm_agent_reasoning.go:23-26 (`adaptiveReasoningTier` OpenRouter gate — SKIP this when fixed)
    - 37E-RESEARCH.md §Seam Map §1 (edit 1e) + 37E-PATTERNS.md "override-vs-auto seam" section
  </read_first>
  <behavior>
    - Test `TestReasoningOverride` (fake llm client capturing the built request): agent configured with `ReasoningOverride="high"` on an OpenRouter provider → the sent request has `Reasoning.Effort=="high"` and `adaptiveReasoningTier` is NOT consulted (classifier never invoked — assert via a spy classifier or by asserting the fixed effort survives regardless of message content).
    - Test: `ReasoningOverride="high"` on a llama.cpp provider → request has `Reasoning.Effort=="high"` (fixed path fires for llama.cpp; adaptive path would NOT have).
    - Test: `ReasoningOverride=""` (auto) on OpenRouter → byte-identical to today's adaptive path (the classifier runs, tier applied as before) — a golden/parity assertion.
  </behavior>
  <action>
    Add `reasoningOverride llm.ReasoningEffort` to the `LlmAgent` struct + `ReasoningOverride llm.ReasoningEffort` to `LlmAgentConfig`; wire it in the constructor. In `Run`, when `a.reasoningOverride != ""` (fixed), SKIP the `adaptiveReasoningTier` computation and route `buildRequest` to a new fixed branch that calls `a.builder.BuildWithReasoningOverride(a.history, a.registry, a.cfg.Provider, a.cfg, budget, a.reasoningOverride, a.activated)`. When `a.reasoningOverride == ""`, the existing adaptive path runs UNCHANGED (byte-identical). Extend `buildRequest` (or add a sibling) to accept the fixed effort; keep the adaptive-vs-plain selector intact for the auto case. Write the test FIRST. Keep llm_agent.go ≤600 LOC (extract to llm_agent_reasoning.go if needed).
  </action>
  <acceptance_criteria>
    - `go test ./internal/agent/ -run TestReasoningOverride -race` passes all three rows.
    - Fixed override produces `Reasoning.Effort` matching the configured effort on BOTH providers and does NOT invoke the adaptive classifier.
    - The auto path (override "") is proven byte-identical to the pre-change adaptive path (parity assertion).
    - `go build ./...` green; no god class.
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/agent/ -run 'TestReasoningOverride' -race && go vet ./internal/agent/ && go build ./...</automated>
  </verify>
  <done>A fixed effort forces `req.Reasoning` and bypasses the classifier; auto is zero-regression (D-04).</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| HTTP-supplied override (ctx) → agent request builder | A per-turn effort crosses from the request scope into the LLM request. |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37E-04-VIS | Information Disclosure | CoT visibility via the selector | mitigate | `ApplyFixedReasoning` sets `exclude` ONLY from `cfg.ShowReasoning` (D-10), reusing the tier's exact rule; the override carries no visibility control, so it cannot force-expose reasoning the operator hid. Tested. |
| T-37E-04-REG | Tampering (regression) | adaptive path on auto | mitigate | The override is applied ONLY when non-empty; the auto path is asserted byte-identical (parity test), so `auto` cannot silently change today's behavior. |
| T-37E-04-CTX | Tampering | ctx-key collision | mitigate | The override uses a private unexported struct key (not a string), so no unrelated ctx value can be read as an effort. |

The effort VALUE is validated server-side upstream (plan 06 enum + capability gate); this plan trusts an already-validated `llm.ReasoningEffort`. No new network/package/auth surface.
</threat_model>

<verification>
- `go test ./internal/agent/... ./internal/agent/prompt/... ./internal/runner/... -race` green.
- Fixed override fires on OpenRouter AND llama.cpp; auto path byte-identical.
- No god class (all touched files ≤600 LOC).
</verification>

<success_criteria>
- The effort symbol reaches `req.Reasoning` via ctx→config→`ApplyFixedReasoning`, gated on the generalized reasoning target (D-08), with D-10 exclude parity and D-04 zero-regression auto.
- WEBMODEL-01 (effort takes effect) + WEBMODEL-03 (server-owned map, no client ReasoningConfig) advanced at the agent layer.
</success_criteria>

<output>
Create `.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-04-SUMMARY.md` when done.
</output>
