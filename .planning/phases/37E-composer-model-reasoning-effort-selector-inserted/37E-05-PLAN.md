---
phase: 37E-composer-model-reasoning-effort-selector-inserted
plan: 05
type: execute
wave: 3
depends_on: ["37E-02"]
files_modified:
  - internal/llm/testdata/openrouter_models.json
  - internal/llm/testdata/llamacpp_props.json
  - internal/llm/model_reasoning_caps.go
  - internal/llm/model_reasoning_caps_test.go
  - internal/llm/llamacpp_caps.go
autonomous: true
requirements: [WEBMODEL-01, WEBMODEL-03]
must_haves:
  truths:
    - "The active model's advertised reasoning capability is auto-detected (D-13), never hard-coded and never a placebo (D-12)"
    - "OpenRouter `/models` is fetched over an injectable transport, parsed defensively (unknown effort tokens dropped), TTL-cached (cold fetch → warm hit → expiry re-fetch), and keyed by normalizeModelID"
    - "A `ReasoningCapabilitySource` seam exposes `AllowedEfforts(ctx) → (efforts, default, detected)`; detected=false yields the safe floor upstream; CI never hits the network"
    - "The local llama.cpp source derives its set from `AURA_LLM_PROVIDER=llamacpp` + the spike-095 ops contract, optionally narrowed by a best-effort `/props` probe"
  artifacts:
    - path: "internal/llm/model_reasoning_caps.go"
      provides: "ModelCapabilityClient + ReasoningCapability + ReasoningCapabilitySource + openRouterReasoningCaps"
      contains: "ReasoningCapabilitySource"
    - path: "internal/llm/llamacpp_caps.go"
      provides: "llamaCppReasoningCaps (provider+ops-contract, /props narrowing)"
      contains: "llamaCppReasoningCaps"
    - path: "internal/llm/testdata/openrouter_models.json"
      provides: "captured /models fixture (reasoning model, mandatory model, no-reasoning model)"
      contains: "supported_efforts"
  key_links:
    - from: "internal/llm/model_reasoning_caps.go"
      to: "GET {BaseURL}/models"
      via: "injectable http.RoundTripper + TTL cache keyed by normalizeModelID"
      pattern: "supported_efforts"
---

<objective>
Ship the net-new capability-detection subsystem (D-13/D-12) — the phase's only new external-dependency vertical. Fetch the active model's advertised `supported_efforts` from OpenRouter `/models` (TTL-cached, warmed at boot, defensively parsed with an effort allowlist clamp), expose a neutral `ReasoningCapabilitySource` seam selected by `llm.ReasoningTarget`, and a local llama.cpp source derived from `AURA_LLM_PROVIDER=llamacpp` + the spike-095 ops contract with an optional best-effort `/props` narrowing. On any detection failure the source returns `detected=false` so the upstream shows the safe floor `{auto,off}`. CI NEVER hits the network — every test uses a captured fixture via an injected `http.RoundTripper`.

Purpose: feeds both the two-stage `handleRun` validator and the capability endpoint (plan 06) and the dynamic UI (plan 07). Depends on the `llm.ReasoningEffort` vocab (incl. `max`) + `normalizeModelID` from plan 02.
Output: `ModelCapabilityClient`, `ReasoningCapabilitySource` + two impls, captured fixtures, all daemon-free tested.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-CONTEXT.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-RESEARCH.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-PATTERNS.md
@.claude/skills/golang-structs-interfaces/SKILL.md
@.claude/skills/golang-error-handling/SKILL.md
@internal/llm/models.go
@internal/llm/openai_compat/client.go
</context>

<tasks>

<task type="auto">
  <name>Task 1: Capture the real /models and /props fixtures (Wave-0 for this vertical)</name>
  <files>internal/llm/testdata/openrouter_models.json, internal/llm/testdata/llamacpp_props.json</files>
  <read_first>
    - 37E-RESEARCH.md §P2.2 A (the operator-verified `data[].reasoning.{supported_efforts,default_effort,default_enabled,mandatory}` + `supported_parameters` shape) and §P2.2 B (`/props` fields incl. `chat_template_caps`)
    - 37E-VALIDATION.md Wave 0 Requirements (fixtures) + OQ-6
  </read_first>
  <action>
    Capture `internal/llm/testdata/openrouter_models.json` from the real API, trimmed to 3-4 models so the parse test covers every branch: `curl -s https://openrouter.ai/api/v1/models | jq '.data |= .[0:0] + [ ... ]'` — KEEP one model that advertises graduated `reasoning.supported_efforts` (e.g. includes low/medium/high/xhigh/max), one with `reasoning.mandatory:true` (reasoning cannot be disabled), and one with NO `reasoning` object (non-reasoning model). Preserve the EXACT nesting (`data[].reasoning.supported_efforts`, `default_effort`, `default_enabled`, `mandatory`, and top-level `supported_parameters`) — do NOT hand-invent JSON (OQ-6). Capture `internal/llm/testdata/llamacpp_props.json` from the pinned spike-095 `gemma-4-E2B-it-qat` server (`--jinja`): `curl -s $LLAMA/props > llamacpp_props.json` — retain `chat_template`, `chat_template_caps`, `modalities`. If a live llama-server is unavailable, construct the fixture from the official llama.cpp server README `/props` schema and note the exact `chat_template_caps` key in a top-of-file comment as [ASSUMED-pending-live-capture]. These are TEST DATA, not production code (permitted post-amendment).
  </action>
  <acceptance_criteria>
    - `test -f internal/llm/testdata/openrouter_models.json` and it contains `supported_efforts` and a `mandatory` field and a model with no `reasoning` key.
    - `test -f internal/llm/testdata/llamacpp_props.json` and it contains `chat_template_caps`.
    - Both files are valid JSON (`jq . <file>` exits 0).
  </acceptance_criteria>
  <verify>
    <automated>jq -e '.data | length >= 3' internal/llm/testdata/openrouter_models.json && jq -e '.chat_template_caps' internal/llm/testdata/llamacpp_props.json && echo OK</automated>
  </verify>
  <done>Real-shape fixtures land so the parse tests are written against actual bytes, not invented JSON.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: ModelCapabilityClient — fetch, defensive parse, TTL cache, ReasoningCapabilitySource seam</name>
  <files>internal/llm/model_reasoning_caps.go, internal/llm/model_reasoning_caps_test.go</files>
  <read_first>
    - internal/llm/models.go:22-38 (the hard-coded `modelCapabilityTable` anti-pattern to REPLACE, and `normalizeModelID` to REUSE verbatim as the cache key)
    - internal/llm/openai_compat/client.go:129-142 (the `Authorization: Bearer` + `cfg.Headers` wiring to mirror for a GET)
    - internal/llm/testdata/openrouter_models.json (Task 1 fixture)
    - 37E-RESEARCH.md §P2.2 A (the DTO shapes + `ModelCapabilityClient` signature + `ReasoningCapabilityFor`) + §P2.2 "Unifying seam" + Threat "malicious supported_efforts"
  </read_first>
  <behavior>
    - `TestParseModelReasoningCaps` (fixture via injected `http.RoundTripper`): the graduated model → `SupportedEfforts` == the clamped set (low/medium/high/xhigh/max as `[]ReasoningEffort`); an UNKNOWN token in the JSON (e.g. "turbo") is DROPPED (allowlist clamp to {max,xhigh,high,medium,low,none}); `mandatory` and `default_effort` parsed; the no-reasoning model → detected/ok=false or empty efforts.
    - `TestModelCapabilityCacheTTL`: cold fetch (RoundTripper called once), immediate second call → NO 2nd HTTP call (warm hit); after advancing the injected clock past TTL → re-fetch (2nd HTTP call). Assert call counts.
    - `TestReasoningCapKey`: `AURA_LLM_MODEL="deepseek/deepseek-v4-flash:nitro"` normalizes to the `/models` base key `deepseek/deepseek-v4-flash`.
    - `AllowedEfforts` on a fetch error / absent model → `(nil-or-floor, "", detected=false)`.
  </behavior>
  <action>
    Create internal/llm/model_reasoning_caps.go (package llm): `ReasoningCapability` struct (`SupportedEfforts []ReasoningEffort`, `DefaultEffort ReasoningEffort`, `DefaultEnabled bool`, `Mandatory bool`, `SupportedParams []string`); the wire DTO `openRouterModelsResponse` matching the Task-1 fixture nesting; `ModelCapabilityClient` with `cfg Config`, `httpClient *http.Client`, `ttl time.Duration`, injectable `now func() time.Time`, `mu sync.Mutex`, `cache map[string]ReasoningCapability` keyed by `normalizeModelID`, `fetchedAt`, `ok`; `NewModelCapabilityClient(cfg, ttl)`; `ReasoningCapabilityFor(ctx, model) (ReasoningCapability, bool, error)` that fetches `GET {cfg.BaseURL}/models` (mirror the Bearer+Headers wiring; cap the response body defensively; parse with `json.Decoder`) on a cold/expired cache and returns the active model's capability. Map `supported_efforts` tokens through a STRICT allowlist `{max,xhigh,high,medium,low,none}` → `[]ReasoningEffort`, DROPPING unknowns (Threat: malicious upstream). Define the `ReasoningCapabilitySource` interface (`AllowedEfforts(ctx) (efforts []ReasoningEffort, deflt ReasoningEffort, detected bool)`) and `openRouterReasoningCaps` wrapping the client (honors `mandatory` → excludes `none`/off; sets default from `default_effort`). Write tests FIRST with a fake `http.RoundTripper` returning the fixture. Keep the file ≤600 LOC (split to `llamacpp_caps.go` in Task 3 if needed). NO per-turn network call — the TTL cache serves everyone from memory.
  </action>
  <acceptance_criteria>
    - `go test ./internal/llm/ -run 'TestParseModelReasoningCaps|TestModelCapabilityCacheTTL|TestReasoningCapKey' -race` passes.
    - Unknown effort tokens are dropped; only the allowlist set survives.
    - Cache: exactly ONE HTTP call for cold+warm; a second call after TTL expiry (injected clock).
    - No test performs real network I/O (all via injected RoundTripper + fixture).
    - `ReasoningCapabilitySource.AllowedEfforts` returns `detected=false` on fetch failure.
    - model_reasoning_caps.go ≤600 LOC.
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/llm/ -run 'TestParseModelReasoningCaps|TestModelCapabilityCacheTTL|TestReasoningCapKey|TestAllowedEfforts' -race && go vet ./internal/llm/</automated>
  </verify>
  <done>The active model's advertised capability is auto-detected, defensively parsed, TTL-cached, and exposed via a neutral seam — no hard-coded table, no network in CI.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: llama.cpp capability source — provider+ops-contract with best-effort /props narrowing</name>
  <files>internal/llm/llamacpp_caps.go, internal/llm/model_reasoning_caps_test.go</files>
  <read_first>
    - internal/llm/testdata/llamacpp_props.json (Task 1 fixture)
    - .planning/spikes/095-llama-cpp-reasoning-effort-wire-contract/ (the ops contract: `--jinja` on, `--reasoning-budget` off → full graduated set)
    - 37E-RESEARCH.md §P2.2 B + OQ-4 (widen llama.cpp fallback to the full graduated set when provider=llamacpp is EXPLICIT) + Assumptions A7
  </read_first>
  <behavior>
    - `TestLlamaCppReasoningCaps`: with `AURA_LLM_PROVIDER=llamacpp` and NO reachable `/props` → `AllowedEfforts` returns the full graduated set `{none,low,medium,high,xhigh,max}` with `detected=true` (explicit provider = operator asserting the spike-095 launch config, OQ-4 resolution: widen).
    - With a `/props` fixture whose `chat_template_caps` thinking flag is present-and-false → narrow to `{none}` (off only) + detected=true.
    - Parse of `/props` is defensive: unknown/missing `chat_template_caps` → trust the provider+ops-contract full set (never panic).
  </behavior>
  <action>
    Create internal/llm/llamacpp_caps.go: `llamaCppPropsResponse` DTO (`ChatTemplate`, `ChatTemplateCaps map[string]any`, `Modalities map[string]bool`); `llamaCppReasoningCaps` implementing `ReasoningCapabilitySource`. `AllowedEfforts`: base set = the full graduated `{none,low,medium,high,xhigh,max}` (spike-095 validated) with `detected=true` whenever `Provider=="llamacpp"` is explicitly configured (OQ-4 → widen; do NOT require `/props`). Optionally probe `GET {BaseURL}/props` (best-effort, short timeout, injected transport): if `chat_template_caps` exposes a thinking/reasoning capability flag AND it is false, narrow to `{none}`. If `/props` is unreachable or the flag name is unknown, keep the full set. Default effort = "" (auto). Write the test FIRST using the fixture + a fake transport. Keep model_reasoning_caps.go + llamacpp_caps.go each ≤600 LOC.
  </action>
  <acceptance_criteria>
    - `go test ./internal/llm/ -run TestLlamaCppReasoningCaps -race` passes both rows (widen-on-explicit-provider; narrow-on-false-flag).
    - Unreachable/unknown `/props` → the full graduated set (provider+ops-contract fallback), never a panic.
    - `detected=true` when provider is explicitly `llamacpp`.
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/llm/ -run 'TestLlamaCppReasoningCaps' -race && go build ./...</automated>
  </verify>
  <done>The local backend advertises the real spike-095 graduated set from explicit config, with `/props` as best-effort narrowing — no placebo, no live dependency in CI.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| OpenRouter `/models` (new external dependency) → capability cache | Untrusted-ish upstream JSON becomes the allowed-effort set. |
| llama-server `/props` → local capability | Best-effort narrowing input. |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37E-05-UPSTREAM | Tampering | garbage/hostile `supported_efforts` | mitigate | Every token is clamped through a STRICT `{max,xhigh,high,medium,low,none}` allowlist and unknowns dropped, so a hostile/buggy upstream can never inject a non-vocab effort into the validator's allowed set. Tested. Response body-size-capped. TLS + operator's own key (same trust boundary as /chat/completions). |
| T-37E-05-AVAIL | Denial of Service / Availability | `/models` unavailable/slow | mitigate | Fetch is TTL-cached + warmed at boot, NEVER per-turn; on failure `AllowedEfforts` returns `detected=false` → safe floor upstream + Stage-2 rejects graduated levels; short timeout + body cap. Degrades, never breaks (D-13 fallback). |
| T-37E-05-STALE | (accepted) | cache lag | accept | Advertised support is best-effort (D-13); a long TTL may lag a capability change by hours — acceptable, the wire knob is still real (D-12); documented, not a vuln. |

CI never hits the network (injected RoundTripper + fixtures). No new package installs.
</threat_model>

<verification>
- `go test ./internal/llm/ -race` green; all capability tests pass with fixtures, zero network.
- Fixtures valid JSON with the required branches.
- No god class (each file ≤600 LOC).
</verification>

<success_criteria>
- The active model's reasoning capability is auto-detected (D-13), never placebo (D-12); OpenRouter parse is defensive; TTL cache serves without per-turn fetches; the llama.cpp source uses explicit provider + spike-095 ops contract.
- The `ReasoningCapabilitySource` seam is ready for plan 06 (validator + endpoint).
- WEBMODEL-01/03 capability foundation covered by daemon-free tests.
</success_criteria>

<output>
Create `.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-05-SUMMARY.md` when done.
</output>
