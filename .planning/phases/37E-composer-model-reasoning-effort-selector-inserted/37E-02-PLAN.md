---
phase: 37E-composer-model-reasoning-effort-selector-inserted
plan: 02
type: execute
wave: 2
depends_on: ["37E-01"]
files_modified:
  - internal/llm/client.go
  - internal/llm/reasoning_target.go
  - internal/llm/reasoning_target_test.go
  - internal/llm/config.go
  - internal/settings/settings.go
  - internal/llm/openai_compat/client.go
  - internal/llm/openai_compat/client_reasoning_wire_test.go
autonomous: true
requirements: [WEBMODEL-01, WEBMODEL-03]
must_haves:
  truths:
    - "The `max` UI level has a home in the vocabulary: `llm.ReasoningEffortMax` exists and serializes to the OpenRouter wire as `max`"
    - "A neutral `llm.ReasoningTarget(provider,baseURL)` classifier positively identifies OpenRouter, llama.cpp, and none — keyed on Provider==\"llamacpp\" for the local path"
    - "The operator can select the backend via `AURA_LLM_PROVIDER` (env + settings)"
    - "On a llama.cpp target the wire emits `chat_template_kwargs:{enable_thinking:false}` (off) or `thinking_budget_tokens:512/2048/8192/16384/-1` (low/mid/high/extra/max) and NO OpenRouter reasoning object; on OpenRouter the wire is byte-unchanged"
    - "The UI-level→wire mapping (D-03) is exhaustive and real-knob-only: off→none, low→low, mid→medium, high→high, extra→xhigh, max→max; no placebo level and no `reasoning.max_tokens` cap (D-12)"
  artifacts:
    - path: "internal/llm/reasoning_target.go"
      provides: "ReasoningTargetKind + ReasoningTarget(provider,baseURL)"
      contains: "func ReasoningTarget"
    - path: "internal/llm/client.go"
      provides: "ReasoningEffortMax const"
      contains: "ReasoningEffortMax"
    - path: "internal/llm/openai_compat/client.go"
      provides: "target-aware buildWireRequest + llama.cpp fields on wireRequest"
      contains: "ThinkingBudgetTokens"
  key_links:
    - from: "internal/llm/openai_compat/client.go"
      to: "internal/llm/reasoning_target.go"
      via: "buildWireRequest switches on llm.ReasoningTarget(c.cfg.Provider, c.cfg.BaseURL)"
      pattern: "ReasoningTarget\\("
    - from: "internal/llm/config.go"
      to: "AURA_LLM_PROVIDER env"
      via: "applyEnvOverrides sets cfg.Provider"
      pattern: "AURA_LLM_PROVIDER"
---

<objective>
Ship the provider-neutral effort ENGINE at the llm layer: the `max` vocabulary const (D-02/D-09a), a neutral backend classifier that both the agent and wire layers can import without a layering smell, the `AURA_LLM_PROVIDER` knob that positively identifies a llama.cpp backend (OQ-1), and the net-new llama.cpp wire branch (D-08, spike-095). The OpenRouter wire shape stays byte-unchanged (spike-096: OFF already works; `max`/`xhigh` serialize automatically once the const exists).

Purpose: everything downstream (override seam, capability detection, endpoint) depends on these symbols. This is the foundation wave.
Output: `ReasoningEffortMax`, `llm.ReasoningTarget`, `AURA_LLM_PROVIDER`, the llama.cpp wire branch — all covered by DAEMON-FREE pure `go test` (the wire test is coverage-load-bearing for the ≥85% floor).
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-CONTEXT.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-RESEARCH.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-PATTERNS.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-VALIDATION.md
@.claude/skills/spike-findings-Aura/SKILL.md
@internal/llm/client.go
@internal/agent/prompt/reasoning_policy.go
@internal/llm/openai_compat/client.go
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Add ReasoningEffortMax const + the neutral ReasoningTarget classifier (pure, tested first)</name>
  <files>internal/llm/client.go, internal/llm/reasoning_target.go, internal/llm/reasoning_target_test.go</files>
  <read_first>
    - internal/llm/client.go:135-158 (the `ReasoningEffort` const block — `xhigh` already present, `max` net-new — + `ReasoningConfig.Empty`)
    - internal/agent/prompt/reasoning_policy.go:47-53 (`IsOpenRouterReasoningTarget` — the exact string logic to LIFT into the neutral classifier)
    - 37E-RESEARCH.md §Seam Map §2 + §P2.4 (vocab reconciliation) + 37E-PATTERNS.md "reasoning_target.go" section
  </read_first>
  <behavior>
    - Test: ReasoningTarget("openrouter","") == ReasoningTargetOpenRouter; ReasoningTarget("openrouter","https://openrouter.ai/api/v1") == OpenRouter.
    - Test: ReasoningTarget("llamacpp", any) == ReasoningTargetLlamaCpp (keyed on Provider, case-insensitive).
    - Test: ReasoningTarget("vllm","http://dgx:8000") == ReasoningTargetNone (must NOT misfire on the local vLLM path).
    - Test: string(ReasoningEffortMax) == "max".
  </behavior>
  <action>
    Add `ReasoningEffortMax ReasoningEffort = "max"` to the const block in client.go (leave `minimal` untouched — unused by 37E). Create internal/llm/reasoning_target.go: `type ReasoningTargetKind int` with iota consts `ReasoningTargetNone`, `ReasoningTargetOpenRouter`, `ReasoningTargetLlamaCpp`, and `func ReasoningTarget(provider, baseURL string) ReasoningTargetKind`. OpenRouter branch = the EXACT current `IsOpenRouterReasoningTarget` logic (EqualFold provider=="openrouter" AND baseURL empty-or-contains-"openrouter.ai"). LlamaCpp branch = `strings.EqualFold(provider,"llamacpp")` (explicit Provider key, NOT a baseURL heuristic — OQ-1; the DGX/vLLM local path also emits reasoning). Everything else → None. Write reasoning_target_test.go as a table test per the behavior block FIRST (RED), then implement (GREEN).
  </action>
  <acceptance_criteria>
    - `grep -n 'ReasoningEffortMax ReasoningEffort = "max"' internal/llm/client.go` matches.
    - internal/llm/reasoning_target.go defines `ReasoningTargetKind` + the three consts + `ReasoningTarget`.
    - `go test ./internal/llm/ -run TestReasoningTarget` passes; table covers openrouter/llamacpp/vllm/none.
    - Provider "llamacpp" matches case-insensitively; a vLLM base URL does NOT classify as LlamaCpp.
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/llm/ -run 'TestReasoningTarget|TestReasoningEffort' -race && go vet ./internal/llm/</automated>
  </verify>
  <done>The `max` effort exists and a neutral, provider-keyed target classifier is available to both `prompt` and `openai_compat`.</done>
</task>

<task type="auto">
  <name>Task 2: Add the AURA_LLM_PROVIDER env override + settings AllowedKeys entry</name>
  <files>internal/llm/config.go, internal/settings/settings.go</files>
  <read_first>
    - internal/llm/config.go:309-357 (`applyEnvOverrides` — Model/BaseURL set here, Provider currently NOT env-settable)
    - internal/settings/settings.go:46-48 (`AllowedKeys` rows for `AURA_LLM_MODEL`/`AURA_LLM_BASE_URL`)
    - 37E-RESEARCH.md OQ-1 + 37E-PATTERNS.md "AURA_LLM_PROVIDER knob" section
  </read_first>
  <action>
    In config.go `applyEnvOverrides`, add `if v := os.Getenv("AURA_LLM_PROVIDER"); v != "" { cfg.Provider = v }` alongside the existing Model/BaseURL env branches (follow the exact existing style + the env-const naming convention used in that file). In settings.go `AllowedKeys`, add a `"AURA_LLM_PROVIDER"` row (`Kind: KindString, Label: "Primary LLM provider (openrouter|llamacpp)"`) mirroring the `AURA_LLM_MODEL` row so the operator can switch the whole backend from the Settings page (D-01 consistency). Do NOT change the default Provider (stays "openrouter").
  </action>
  <acceptance_criteria>
    - `grep -q 'AURA_LLM_PROVIDER' internal/llm/config.go` and `grep -q 'AURA_LLM_PROVIDER' internal/settings/settings.go` both succeed.
    - With `AURA_LLM_PROVIDER=llamacpp` set, `config.Load` yields `cfg.Provider == "llamacpp"`; unset leaves the prior default.
    - The settings AllowedKeys map contains the new key with a KindString entry (existing settings tests still pass).
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/llm/ ./internal/settings/ -race && go build ./...</automated>
  </verify>
  <done>`AURA_LLM_PROVIDER` is settable via env + Settings; llama.cpp is positively identifiable at request time (feeds Task 3 + the capability source).</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Target-aware wire branch — llama.cpp thinking fields, OpenRouter unchanged (daemon-free table test)</name>
  <files>internal/llm/openai_compat/client.go, internal/llm/openai_compat/client_reasoning_wire_test.go</files>
  <read_first>
    - internal/llm/openai_compat/client.go:71-82 (`wireRequest`), :84-89 (`wireReasoning`), :220-253 (`buildWireRequest` + `buildWireReasoning`)
    - .planning/spikes/095-llama-cpp-reasoning-effort-wire-contract/ (the validated llama.cpp contract — off + budgets)
    - 37E-RESEARCH.md §Seam Map §3 + Backend Wire Contract + §P2.1 (7-level budgets) + 37E-PATTERNS.md "llama.cpp wire branch" section
  </read_first>
  <behavior>
    - Table test `TestBuildWireRequestReasoningTarget` (PURE, no daemon, no network):
    - Provider="openrouter": each effort (none/low/medium/high/xhigh/max) → wireRequest.Reasoning.Effort == string(effort); ChatTemplateKwargs nil; ThinkingBudgetTokens nil (UNCHANGED shape). Empty reasoning → Reasoning nil.
    - Provider="llamacpp", off (ReasoningEffortNone) → ChatTemplateKwargs["enable_thinking"]==false; Reasoning nil; ThinkingBudgetTokens nil.
    - Provider="llamacpp", low/mid/high/extra/max → ThinkingBudgetTokens == *512/*2048/*8192/*16384/*-1 respectively; Reasoning nil; ChatTemplateKwargs nil.
    - Provider="llamacpp", empty reasoning (auto) → no reasoning fields at all.
  </behavior>
  <action>
    Add two optional fields to the `wireRequest` struct: `ChatTemplateKwargs map[string]any \`json:"chat_template_kwargs,omitempty"\`` and `ThinkingBudgetTokens *int \`json:"thinking_budget_tokens,omitempty"\``. Make `buildWireRequest` target-aware: switch on `llm.ReasoningTarget(c.cfg.Provider, c.cfg.BaseURL)`. For `ReasoningTargetOpenRouter`/`ReasoningTargetNone` keep today's `Reasoning: buildWireReasoning(req.Reasoning)` UNCHANGED (xhigh/max serialize automatically via `Effort: string(r.Effort)`). For `ReasoningTargetLlamaCpp`, translate `req.Reasoning.Effort`: none → set `ChatTemplateKwargs{"enable_thinking": false}`; low/medium/high/xhigh/max → set `ThinkingBudgetTokens` to a pointer to the corresponding const; empty Effort → set nothing; ALWAYS leave `Reasoning` nil on the llama.cpp branch (the OpenRouter object is a NO-OP on llama-server, spike 095). Define the budget consts in `openai_compat` (e.g. `llamaCppBudgetLow=512, Mid=2048, High=8192, Extra=16384, Max=-1`) with a comment that they are spike-095-validated and promotable to `AURA_LLM_LLAMACPP_THINKING_BUDGET_*` config later. Write the table test FIRST per the behavior block. Keep client.go ≤600 LOC (extract a small `buildLlamaCppReasoning` helper if needed).
  </action>
  <acceptance_criteria>
    - `go test ./internal/llm/openai_compat/ -run TestBuildWireRequestReasoningTarget -race` passes with all rows above.
    - The OpenRouter path is byte-identical to today: a snapshot/assert of the OpenRouter wireRequest for each effort still emits `reasoning.effort` and NO `thinking_budget_tokens`.
    - The llama.cpp `max` row emits `thinking_budget_tokens: -1`; `off` emits `chat_template_kwargs.enable_thinking=false` and NO `reasoning` object.
    - The test is pure (no `//go:build docker_integration`/live tag) so it counts toward the coverage floor.
    - `go build ./...` green; client.go still ≤600 LOC.
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/llm/openai_compat/ -run TestBuildWireRequestReasoningTarget -race && go vet ./internal/llm/openai_compat/</automated>
  </verify>
  <done>Effort takes effect on BOTH backends at the wire layer; the coverage-load-bearing daemon-free test proves the llama.cpp branch and the unchanged OpenRouter shape.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| agent loop → provider wire (`openai_compat`) | The reasoning config crosses into the provider request; a wrong branch could smuggle an unbounded budget. |
| operator config → backend selection (`AURA_LLM_PROVIDER`) | The provider knob decides which wire shape is emitted. |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37E-02-BUDGET | Denial of Service | llama.cpp `thinking_budget_tokens` | mitigate | Budgets are FIXED consts selected by a fixed effort symbol (512/2048/8192/16384/-1); no request-supplied N reaches the wire — `-1`/unlimited is reachable ONLY via the `max` symbol which is itself capability-gated upstream (plan 06). Table test asserts the exact const per level. |
| T-37E-02-MISFIRE | Tampering (wrong-target) | `ReasoningTarget` on a vLLM/DGX path | mitigate | LlamaCpp is keyed on explicit `Provider=="llamacpp"`, never a baseURL heuristic; a vLLM base URL classifies as `None` (tested), so the llama.cpp branch cannot misfire and drop the reasoning object on a non-llama backend. |
| T-37E-02-COVLOSS | (coverage) | daemon-gated wire test | mitigate | The wire branch is proven by a PURE table test (no docker/live tag) so it contributes to the ≥85% floor (CLAUDE.md gate rule); a live-only test would leave it uncovered. |

No new external network calls, no new packages, no auth surface in this plan.
</threat_model>

<verification>
- `go test ./internal/llm/... ./internal/settings/... -race` green.
- OpenRouter wire byte-unchanged; llama.cpp branch emits the spike-095 fields per level.
- No god class: client.go ≤600 LOC.
</verification>

<success_criteria>
- `ReasoningEffortMax`, `llm.ReasoningTarget`, `AURA_LLM_PROVIDER`, and the llama.cpp wire branch exist and are covered by daemon-free tests.
- The OpenRouter path is a no-op change (spike-096); only the llama.cpp path is net-new (spike-095).
- WEBMODEL-01 (effort takes effect on both backends) + WEBMODEL-03 (real wire knob, no fabricated field) advanced at the engine layer.
</success_criteria>

<output>
Create `.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-02-SUMMARY.md` when done.
</output>
