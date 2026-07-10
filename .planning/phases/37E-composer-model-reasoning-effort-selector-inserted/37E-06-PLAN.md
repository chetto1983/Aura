---
phase: 37E-composer-model-reasoning-effort-selector-inserted
plan: 06
type: execute
wave: 4
depends_on: ["37E-03", "37E-04", "37E-05"]
files_modified:
  - internal/agui/server_run_request.go
  - internal/agui/server_run_request_test.go
  - internal/agui/server.go
  - internal/agui/server_reasoning_effort_test.go
  - internal/agui/composer_api.go
  - internal/agui/composer_api_reasoning_test.go
  - cmd/aura/serve_webui.go
autonomous: true
requirements: [WEBMODEL-01, WEBMODEL-02, WEBMODEL-03]
must_haves:
  truths:
    - "`/agent/run` accepts an optional symbolic `aura.effort`; a non-enum value → 400; a real-enum-but-not-advertised level → 400 (two-stage governance, D-05/D-13); absent/`auto` → today's adaptive default"
    - "A fixed, validated level threads via `runner.WithReasoningOverride` AND is persisted (owner-scoped) to `conversations.metadata`"
    - "`GET /api/composer/reasoning-capabilities` (RequireAuth) returns the active model's allowed UI symbols + default + backend + detected; a nil source returns the safe fallback `{levels:[auto,off],detected:false}` (200, not 503) and NEVER leaks the model id/base URL/key"
  artifacts:
    - path: "internal/agui/server_run_request.go"
      provides: "Effort field on both aura decode structs"
      contains: "Effort"
    - path: "internal/agui/server.go"
      provides: "parseEffortSymbol + two-stage handleRun validation + SetReasoningCapabilitySource + persist"
      contains: "parseEffortSymbol"
    - path: "internal/agui/composer_api.go"
      provides: "GET /api/composer/reasoning-capabilities handler + route"
      contains: "reasoning-capabilities"
  key_links:
    - from: "internal/agui/server.go"
      to: "internal/runner/runner_reasoning.go"
      via: "handleRun sets ctx = runner.WithReasoningOverride(ctx, effort) on a fixed level"
      pattern: "WithReasoningOverride"
    - from: "internal/agui/server.go"
      to: "internal/conversations/store_identity.go"
      via: "handleRun persists the symbol via s.conv.UpdateReasoningEffortForIdentity"
      pattern: "UpdateReasoningEffortForIdentity"
    - from: "cmd/aura/serve_webui.go"
      to: "internal/agui/server.go"
      via: "SetReasoningCapabilitySource wired after NewServer"
      pattern: "SetReasoningCapabilitySource"
---

<objective>
Ship the server governance + capability endpoint (D-05/D-12/D-13, WEBMODEL-02/03). Decode the optional `aura.effort` symbol (mirror `aura.skill`), run TWO-STAGE validation in `handleRun` after the owner-scope gate — Stage 1 syntactic 7-symbol enum (non-enum → 400), Stage 2 capability (a fixed level not in the active model's advertised set → 400; when detection failed, only `off`/`auto` pass) — then thread the validated fixed level via `runner.WithReasoningOverride` and persist the symbol owner-scoped. Add `GET /api/composer/reasoning-capabilities` (mirror the 37D composer skills route) returning only the allowed UI symbols + default + backend + detected, degrading to the safe floor on a nil source. Wire `SetReasoningCapabilitySource` at the composition root.

Purpose: the no-bypass control (WEBMODEL-03) + the capability surface the Composer consumes (plan 07). Depends on plans 03 (persist interface), 04 (WithReasoningOverride), 05 (ReasoningCapabilitySource).
Output: `parseEffortSymbol`, two-stage `handleRun`, the capabilities endpoint, setter-injection — all daemon-free tested with a FAKE capability source.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-CONTEXT.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-RESEARCH.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-PATTERNS.md
@.claude/skills/golang-security/SKILL.md
@internal/agui/server.go
@internal/agui/server_run_request.go
@internal/agui/composer_api.go
@internal/agui/settings_api.go
@cmd/aura/serve_webui.go
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Decode aura.effort + parseEffortSymbol (7 symbols) + two-stage handleRun validation + thread + persist</name>
  <files>internal/agui/server_run_request.go, internal/agui/server_run_request_test.go, internal/agui/server.go, internal/agui/server_reasoning_effort_test.go</files>
  <read_first>
    - internal/agui/server_run_request.go:9-39 (the `Skill` field in BOTH structs — mirror for `Effort`) + server_run_request_test.go:16-20 (the decode test to extend)
    - internal/agui/server.go:291-392 (`handleRun`: `GetForIdentity` owner gate :314, skill decode/resolve :342-347, body cap :292) + settings_api.go:263 (`effectiveSettingValue` for the active model)
    - internal/runner/runner_reasoning.go (plan 04 — `WithReasoningOverride`) + internal/conversations interface (plan 03 — `UpdateReasoningEffortForIdentity`)
    - 37E-RESEARCH.md §P2.5 (two-stage validation exact code) + §Seam Map §1/§4/§5 + Threat "enum-injection" / "capability bypass"
  </read_first>
  <behavior>
    - `parseEffortSymbol(s) (llm.ReasoningEffort, isFixed bool, ok bool)`: ""/"auto" → ("",false,true); off→(None,true,true); low/mid/high/extra/max → (Low/Medium/High/XHigh/Max,true,true); anything else → ("",false,false).
    - `TestHandleRunEffort` (httptest + fake Runner + fake ReasoningCapabilitySource): invalid `aura.effort="turbo"` → 400 "invalid reasoning effort"; absent/`auto` → 200, NO override threaded, NO capability rejection; a foreign thread still 404s BEFORE effort validation (isolation before governance).
    - `TestHandleRunEffortCapability`: fixed level advertised by the fake source → 200 + override threaded + persisted; fixed level NOT advertised → 400 "not supported"; `mandatory` model + `off` → 400; `detected=false` + a graduated level → 400 (only off/auto pass, safe floor); `off`/`auto` always pass.
  </behavior>
  <action>
    Add `Effort string \`json:"effort"\`` to BOTH aura structs in server_run_request.go (mirror `Skill` exactly); extend server_run_request_test.go to assert it decodes alongside skill/attachment_ids. Add a pure `parseEffortSymbol` (7 symbols → the tuple above; keep the UI symbol `mid`/`extra`/`max` → internal `medium`/`xhigh`/`max` translation here). In `handleRun`, AFTER the existing `GetForIdentity` owner-scope gate: Stage 1 — `effort, isFixed, ok := parseEffortSymbol(req.Aura.Effort)`; `!ok` → `http.Error(w, "invalid reasoning effort", 400)`. Stage 2 — if `isFixed && s.reasoningCaps != nil`: `allowed, _, detected := s.reasoningCaps.AllowedEfforts(ctx)` (in-memory TTL cache, NO per-turn round-trip); if `detected` and `effort` not in `allowed` → 400 "effort not supported by the active model"; if `!detected` and `effort != ReasoningEffortNone` → 400 "effort not verifiable; only off/auto available". On a validated fixed level, `ctx = runner.WithReasoningOverride(ctx, effort)`. Persist the ORIGINAL symbol (incl. `auto`, so switch-back is remembered — OQ-3) via `s.conv.UpdateReasoningEffortForIdentity(ctx, in.ThreadID, scopedIdentityID(ctx), req.Aura.Effort)` (ignore-or-log 0-rows, ownership already gated). Add the `reasoningCaps llm.ReasoningCapabilitySource` field + `SetReasoningCapabilitySource` setter on Server (mirror `SetSettingsStore`). Write tests FIRST with a fake source. Keep server.go ≤600 LOC (extract the two-stage helper to a sibling file if needed).
  </action>
  <acceptance_criteria>
    - server_run_request.go has `Effort` on both structs; the decode test asserts it.
    - `go test ./internal/agui/ -run 'TestHandleRunEffort|TestParseEffortSymbol|TestDecodeRunAgentRequest' -race` passes all rows: non-enum→400, unadvertised→400, mandatory+off→400, detected=false+graduated→400, off/auto→pass, foreign-thread 404 precedes effort.
    - A validated fixed level threads `WithReasoningOverride` and calls `UpdateReasoningEffortForIdentity`; `auto`/absent threads nothing but still persists the symbol.
    - Stage 2 uses the cached source (no per-request network).
    - server.go ≤600 LOC.
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/agui/ -run 'TestHandleRunEffort|TestParseEffortSymbol|TestDecodeRunAgentRequest' -race && go vet ./internal/agui/</automated>
  </verify>
  <done>The no-bypass control is live: symbol-only, two-stage validated, threaded, and persisted — never a client-supplied ReasoningConfig or placebo (WEBMODEL-03).</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: GET /api/composer/reasoning-capabilities endpoint + safe fallback</name>
  <files>internal/agui/composer_api.go, internal/agui/composer_api_reasoning_test.go</files>
  <read_first>
    - internal/agui/composer_api.go:14-38 (`composerSkillsPath` + `registerComposerRoutes` + `handleComposerSkills` — the plain-RequireAuth read route to mirror; nil provider degrades)
    - 37E-RESEARCH.md §P2.C (the `reasoningCapabilitiesDTO` + mapping: efforts→UI symbols, prepend auto, omit off when mandatory; nil source → safe fallback 200; identity-scoping) + Threat "capability endpoint info-leak"
  </read_first>
  <behavior>
    - `TestReasoningCapabilitiesEndpoint` (httptest + fake source): source advertising {none,low,high} → response `{levels:["auto","off","low","high"],default:"auto",backend:"openrouter",detected:true}` (efforts→UI symbols, auto prepended).
    - mandatory model (off excluded upstream) → `off` NOT in levels.
    - nil source → `{levels:["auto","off"],detected:false}` with HTTP 200 (NOT 503).
    - response body NEVER contains the model id, base URL, or API key.
  </behavior>
  <action>
    Add `const composerReasoningCapsPath = "GET /api/composer/reasoning-capabilities"`, register it in `registerComposerRoutes`, and add `handleReasoningCapabilities`. Define `reasoningCapabilitiesDTO{Levels []string; Default string; Backend string; Detected bool}`. The handler: if `s.reasoningCaps == nil` → write `{levels:["auto","off"],default:"auto",detected:false}` (200, safe fallback — the composer must degrade, 37D D-09). Else call `AllowedEfforts(ctx)`, map `[]llm.ReasoningEffort` (none→off, low→low, medium→mid, high→high, xhigh→extra, max→max) → UI symbols, PREPEND `auto`, OMIT `off` when the model's reasoning is mandatory (already reflected by `none` absence in `allowed`), set `default:"auto"`, `backend` from `llm.ReasoningTarget`, `detected` from the source. Return ONLY these fields — never the model id, base URL, or key. Write the test FIRST. Keep composer_api.go ≤600 LOC.
  </action>
  <acceptance_criteria>
    - `go test ./internal/agui/ -run TestReasoningCapabilitiesEndpoint -race` passes all rows.
    - The route is registered on `registerComposerRoutes` behind plain RequireAuth (no governance.read).
    - nil source → 200 with `{levels:[auto,off],detected:false}`, never 503.
    - The JSON response contains no model id / base URL / key (assert absence).
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/agui/ -run 'TestReasoningCapabilitiesEndpoint' -race && grep -q "reasoning-capabilities" internal/agui/composer_api.go</automated>
  </verify>
  <done>The Composer can fetch exactly the levels the active model advertises; the endpoint degrades safely and leaks nothing.</done>
</task>

<task type="auto">
  <name>Task 3: Wire the capability source + client at the composition root</name>
  <files>cmd/aura/serve_webui.go</files>
  <read_first>
    - cmd/aura/serve_webui.go (the `serve` composition — where `NewServer` is created and the other `Set*` injections happen; the `/agent/run` RequireAuth mount)
    - internal/llm/model_reasoning_caps.go + llamacpp_caps.go (plan 05 — `NewModelCapabilityClient`, the two `ReasoningCapabilitySource` impls) + internal/llm/reasoning_target.go
    - 37E-RESEARCH.md §P2.C (setter-injection wiring) + §P2.2 (warm-at-boot recommendation)
  </read_first>
  <action>
    In the daemon composition root, after `NewServer`, construct the `ReasoningCapabilitySource` selected by `llm.ReasoningTarget(cfg.Provider, cfg.BaseURL)`: for OpenRouter build a `NewModelCapabilityClient(cfg, ttl)` (recommend a 6–24h TTL const) wrapped in `openRouterReasoningCaps`; for LlamaCpp build `llamaCppReasoningCaps`; for None pass nil (endpoint degrades to the floor). Call `server.SetReasoningCapabilitySource(src)`. Optionally warm the cache once at boot (fire-and-forget goroutine or a bounded call) so the first `handleRun`/endpoint hit is memory-served — never block startup on it. Do NOT change the `/agent/run` auth mount; effort rides the existing RequireAuth+capability route.
  </action>
  <acceptance_criteria>
    - `serve_webui.go` calls `SetReasoningCapabilitySource` with the target-selected source (or nil on None).
    - `go build ./...` green; `aura serve` starts without the capability fetch blocking startup (boot does not hang if OpenRouter is slow/unreachable).
    - No change to the `/agent/run` auth mount.
  </acceptance_criteria>
  <verify>
    <automated>go build ./... && grep -q "SetReasoningCapabilitySource" cmd/aura/serve_webui.go && go vet ./cmd/aura/</automated>
  </verify>
  <done>The capability subsystem is live in the running daemon; the endpoint and the Stage-2 validator share one cached source.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| client → `POST /agent/run` (`aura.effort`) | Untrusted symbol; the injection surface for governance bypass. |
| authenticated identity → capability endpoint | Could leak the operator's model choice/config. |
| identity A → conversation B (persist) | Cross-identity effort write. |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37E-06-ENUM | Tampering / EoP | `aura.effort` enum-injection | mitigate | `parseEffortSymbol` accepts ONLY {auto,off,low,mid,high,extra,max}; anything else (raw provider string, JSON object, `minimal`) → 400. The client sends a symbol, never a ReasoningConfig. This IS the WEBMODEL-03 no-bypass control; 400 path unit-tested. |
| T-37E-06-CAP | EoP / Tampering | capability-bypass (client claims an unadvertised level) | mitigate | Stage-2 rejects any fixed level not in the active model's advertised set (400); `detected=false` collapses to off/auto-only. The client selector is advisory; the server is authoritative (D-12/D-13). Tested. |
| T-37E-06-LEAK | Information Disclosure | capabilities endpoint | mitigate | RequireAuth-gated; returns ONLY allowed UI symbols + default + backend + detected — never the model id, base URL, or key (test asserts absence). Capability is process-global/operator-scoped, so no per-identity leak. |
| T-37E-06-ISO | Info Disclosure / Tampering | persist on a foreign thread | mitigate | Persist uses the owner-scoped `UpdateReasoningEffortForIdentity` AFTER `GetForIdentity` 404s a foreign thread; the write predicates on identity_id (plan 03 cross-identity deny test backs this). |
| T-37E-06-DOS | DoS | body-size / malformed | mitigate | `handleRun` already caps the body at `maxRunBodyBytes` (1 MiB) and 400s a malformed decode; the effort field inherits this. Stage-2 uses the cached source (no per-turn fetch), so a flood cannot amplify into OpenRouter calls. |

ASVS V5 (input validation, two-stage) + V1/V10 (malicious-input-from-a-dependency, handled in plan 05 by the allowlist clamp + fail-safe). No new package installs.
</threat_model>

<verification>
- `go test ./internal/agui/ -race` green (all effort + capability endpoint tests with fake source).
- `go build ./...` + `go vet ./cmd/aura/` green; daemon boots without blocking on the capability fetch.
- server.go / composer_api.go ≤600 LOC.
</verification>

<success_criteria>
- `/agent/run` two-stage-validates `aura.effort` (non-enum→400, unadvertised→400, off/auto→pass), threads + persists a fixed level; the capability endpoint serves the allowed set and degrades safely.
- WEBMODEL-02 (server-side validated override) + WEBMODEL-03 (no bypass) + WEBMODEL-01 (capability surface) covered.
</success_criteria>

<output>
Create `.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-06-SUMMARY.md` when done.
</output>
