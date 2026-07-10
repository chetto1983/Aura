# Phase 37E: Composer Reasoning-Effort Selector - Pattern Map

**Mapped:** 2026-07-10
**Files analyzed:** 24 (create + modify) across 5 waves
**Analogs found:** 22 / 24 (2 net-new subsystems have partial/anti-pattern analogs only)

> Source of truth: **RESEARCH.md Pass-2 Addendum governs** (7-level set `auto·off·low·mid·high·extra·max`, capability auto-detection). Every file:line below was read + verified against HEAD 2026-07-10. Wave-affinity hints: **W1 PRD-amendment · W2 override-seam+wire · W3 capability-backend+persistence · W4 composer-UI**.

---

## File Classification

| Target file | C/M | Role | Data flow | Closest analog | Match | Wave |
|-------------|-----|------|-----------|----------------|-------|------|
| `prd.md` / `ROADMAP.md` / `REQUIREMENTS.md` | M | docs | — | 37B/C/D-01 amendment commits | exact | W1 |
| `internal/llm/client.go` (`ReasoningEffortMax`) | M | model/type | transform | existing `ReasoningEffort` const block | exact | W2 |
| `internal/llm/reasoning_target.go` | C | utility | transform (pure classifier) | `prompt.IsOpenRouterReasoningTarget` | role-match | W2 |
| `internal/agent/prompt/reasoning_policy.go` (`ApplyFixedReasoning`, generalize target) | M | service | transform | `ApplyAdaptiveReasoning` + `ReasoningTier.reasoning()` | exact (sibling) | W2 |
| `internal/agent/prompt/builder.go` (`BuildWithReasoningOverride`) | M | service | transform | `BuildWithReasoningTier` | exact (sibling) | W2 |
| `internal/llm/openai_compat/client.go` (wire branch) | M | service | streaming/wire | `buildWireRequest` + `buildWireReasoning` | exact | W2 |
| `internal/agent/llm_agent.go` (`reasoningOverride` field + branch) | M | service | request-response | `buildRequest` tier branch | exact | W2 |
| `internal/agent/llm_agent_reasoning.go` (skip-when-fixed) | M | service | request-response | `adaptiveReasoningTier` | exact | W2 |
| `internal/runner/runner_reasoning.go` | C | utility | event-driven (ctx) | `WithThreadLockHeld`/`threadLockHeld` | exact | W2 |
| `internal/runner/runner.go` (`buildAgent` read override) | M | service | request-response | `buildAgent` gateway.WithResponder | role-match | W2 |
| `internal/llm/config.go` (`AURA_LLM_PROVIDER`) | M | config | transform | `applyEnvOverrides` Model/BaseURL | exact | W2 |
| `internal/settings/settings.go` (AllowedKeys) | M | config | CRUD | `AURA_LLM_MODEL`/`AURA_LLM_BASE_URL` rows | exact | W2 |
| `internal/llm/model_reasoning_caps.go` | C | service | request-response + TTL cache | `models.go` (anti-pattern) + `openai_compat.Client` HTTP | partial (net-new) | W3 |
| `internal/agui/composer_api.go` (register caps route + handler) | M | controller/route | request-response | `handleComposerSkills` + `registerComposerRoutes` | exact | W3 |
| `internal/agui/server.go` (`SetReasoningCapabilitySource`, `parseEffortSymbol`, two-stage validate + persist) | M | controller | request-response | `SetSettingsStore` + `handleRun` skill decode | exact | W3 |
| `internal/agui/server_run_request.go` (`Effort` field) | M | model/DTO | transform | `Skill` field (both structs) | exact | W3 |
| `internal/agui/types.go` (widen interface) | M | interface | — | `RenameForIdentity` method row | exact | W3 |
| `internal/db/queries/conversations.sql` (update query) | M | migration/query | CRUD | `RenameConversationForIdentity` | exact | W3 |
| `internal/conversations/store_identity.go` (`UpdateReasoningEffortForIdentity`) | M | service/store | CRUD | `RenameForIdentity` | exact | W3 |
| `internal/conversations/store.go` (`Conversation.ReasoningEffort`) | M | model | transform | `Conversation` struct + `Model` field | exact | W3 |
| `internal/conversations/store_helpers.go` (`conversationFromRow` map metadata) | M | service | transform | `conversationFromRow` (drops metadata today) | exact | W3 |
| `web/src/chat/composer/useReasoningEffort.ts` | C | hook | state (per-conv persisted) | `usePinnedSkill.ts` (per-turn — DIFFERS) | role-match | W4 |
| `web/src/chat/composer/useReasoningCapabilities.ts` | C | hook | request-response (fetch+degrade) | `useComposerSkills.ts` | exact | W4 |
| `web/src/chat/composer/api.ts` (`fetchReasoningCapabilities`) | M | client | request-response | `fetchComposerSkills` | exact | W4 |
| `web/src/chat/auraRunBody.ts` (fold effort) | M | utility | transform | `skill` fold | exact | W4 |
| `web/src/chat/sseAdapter.ts` (`StreamRunOptions.effort`) | M | model/type | — | `skill?: string` | exact | W4 |
| `web/src/chat/ExternalStoreChat.tsx` (wire hook + props) | M | component | event-driven | `pinnedSkill`/`onPinSkill` wiring | exact | W4 |
| `web/src/chat/Composer.tsx` (selector control) | M | component | request-response | 37D `/`-picker + i18n `useTranslation` | role-match | W4 |
| i18n `en`+`it` (7 labels + aria) | M | config | — | 37B/C/D i18n parity keys | exact | W4 |

**LOC-cap flags (CLAUDE.md ≤600):** `ExternalStoreChat.tsx` (~519) and `sseAdapter.ts` (at cap) are NEAR the cap — 37E MUST extract new state into `useReasoningEffort.ts`/`useReasoningCapabilities.ts`, never inline (mirrors the `usePinnedSkill.ts`/`auraRunBody.ts` extractions). `reasoning_policy.go` (119 LOC) and `builder.go` (119 LOC) have headroom for the sibling funcs. `model_reasoning_caps.go` is net-new — keep the OpenRouter client and the llama.cpp `/props` source in one file only if it stays ≤600, else split `llamacpp_caps.go`.

---

## Pattern Assignments

### `internal/llm/client.go` — add `ReasoningEffortMax` (model/type)

**Analog:** the existing `ReasoningEffort` const block (SAME file, `client.go:135-142`). `xhigh` ALREADY exists (`extra→xhigh` needs no change); only `max` is net-new.

```go
const (
	ReasoningEffortXHigh   ReasoningEffort = "xhigh"   // ← already present → maps UI "extra"
	ReasoningEffortHigh    ReasoningEffort = "high"
	ReasoningEffortMedium  ReasoningEffort = "medium"
	ReasoningEffortLow     ReasoningEffort = "low"
	ReasoningEffortMinimal ReasoningEffort = "minimal" // unused by 37E, leave as-is
	ReasoningEffortNone    ReasoningEffort = "none"
	// ADD: ReasoningEffortMax ReasoningEffort = "max"  // OpenRouter's own token → serializes 1:1
)
```

`ReasoningConfig.Empty()` (client.go:156) already returns false for any non-empty `Effort`, so a `max`/`xhigh` override flows through the existing seam with no other change. `Request.Reasoning` (client.go:109) is per-call — the per-turn override contract already exists.

---

### `internal/llm/reasoning_target.go` — neutral target classifier (CREATE, pure)

**Analog:** `prompt.IsOpenRouterReasoningTarget` (`reasoning_policy.go:47-53`) — lift its exact string logic into a neutral `internal/llm` classifier so BOTH `prompt` and `openai_compat` import it without a layering smell.

```go
// current OpenRouter-only gate to generalize (reasoning_policy.go:47-53):
func IsOpenRouterReasoningTarget(provider, baseURL string) bool {
	if !strings.EqualFold(provider, "openrouter") {
		return false
	}
	base := strings.ToLower(strings.TrimSpace(baseURL))
	return base == "" || strings.Contains(base, "openrouter.ai")
}
```

New `internal/llm/reasoning_target.go`: `type ReasoningTargetKind int` with `{None, OpenRouter, LlamaCpp}` + `func ReasoningTarget(provider, baseURL string) ReasoningTargetKind`. Then `prompt.IsOpenRouterReasoningTarget` becomes `ReasoningTarget(...) == llm.ReasoningTargetOpenRouter` (behavior-preserving), and a NEW `prompt.IsReasoningTarget` returns true for OpenRouter **or** LlamaCpp (used by the FIXED path only — the adaptive path stays OpenRouter-only, D-04). **OQ-1: key LlamaCpp on `Provider == "llamacpp"` (explicit), NOT a baseURL heuristic** (the DGX/vLLM local path also emits `reasoning`).

---

### `internal/agent/prompt/reasoning_policy.go` — `ApplyFixedReasoning` (MODIFY, sibling of `ApplyAdaptiveReasoning`)

**Analog:** `ApplyAdaptiveReasoning` (SAME file, `:38-43`) + `ReasoningTier.reasoning()` exclude rule (`:78-88`, `boolPtr:119`).

```go
// ApplyAdaptiveReasoning (reasoning_policy.go:38-43) — the shape to mirror:
func ApplyAdaptiveReasoning(req *llm.Request, provider string, cfg llm.Config, tier ReasoningTier) {
	if !cfg.AdaptiveReasoning || !IsOpenRouterReasoningTarget(provider, cfg.BaseURL) || !tier.Valid() {
		return
	}
	req.Reasoning = tier.reasoning(cfg.ShowReasoning)
}

// exclude rule to REUSE verbatim (reasoning_policy.go:78-88) — D-10 parity:
func (t ReasoningTier) reasoning(showReasoning bool) llm.ReasoningConfig {
	exclude := boolPtr(!showReasoning)
	switch t {
	case ReasoningTierHigh:
		return llm.ReasoningConfig{Effort: llm.ReasoningEffortHigh, Exclude: exclude}
	...
	}
}
```

New `ApplyFixedReasoning(req *llm.Request, provider string, cfg llm.Config, effort llm.ReasoningEffort)`: gate on the GENERALIZED `IsReasoningTarget` (OpenRouter OR llama.cpp), set `req.Reasoning = llm.ReasoningConfig{Effort: effort, Exclude: boolPtr(!cfg.ShowReasoning)}`. **Do NOT gate on `cfg.AdaptiveReasoning`** — the fixed override is orthogonal to the adaptive toggle. The exclude derivation is byte-identical to `ReasoningTier.reasoning()` (one family, one exclude rule).

---

### `internal/agent/prompt/builder.go` — `BuildWithReasoningOverride` (MODIFY, sibling of `BuildWithReasoningTier`)

**Analog:** `BuildWithReasoningTier` (SAME file, `:99-104`).

```go
func (b *PromptBuilder) BuildWithReasoningTier(history []llm.Message, reg *tools.Registry, provider string, cfg llm.Config, budget Budget, tier ReasoningTier, activated map[string]struct{}) llm.Request {
	req := b.buildBase(history, reg, cfg, budget, activated)
	ApplyAdaptiveReasoning(&req, provider, cfg, tier)
	injectCacheControl(&req, provider)
	return req
}
```

New `BuildWithReasoningOverride(..., effort llm.ReasoningEffort, ...)`: same body but calls `ApplyFixedReasoning(&req, provider, cfg, effort)` between `buildBase` and `injectCacheControl`.

---

### `internal/llm/openai_compat/client.go` — llama.cpp wire branch (MODIFY)

**Analog:** `buildWireRequest` (`:220-241`) + `buildWireReasoning` (`:243-253`) + `wireRequest` struct (`:71-82`). The OpenRouter shape is UNCHANGED (spike 096: OFF already works); the llama.cpp branch is net-new.

```go
// wireRequest (client.go:71-82) — ADD two optional fields:
type wireRequest struct {
	...
	Reasoning   *wireReasoning `json:"reasoning,omitempty"`
	// ADD: ChatTemplateKwargs   map[string]any `json:"chat_template_kwargs,omitempty"`
	// ADD: ThinkingBudgetTokens *int           `json:"thinking_budget_tokens,omitempty"`
}

// buildWireRequest (client.go:229-241) currently UNCONDITIONALLY emits the OpenRouter object:
	Reasoning: buildWireReasoning(req.Reasoning),

// buildWireReasoning (client.go:243-253) — the current OpenRouter serialization to BRANCH FROM:
func buildWireReasoning(r llm.ReasoningConfig) *wireReasoning {
	if r.Empty() { return nil }
	return &wireReasoning{Effort: string(r.Effort), MaxTokens: r.MaxTokens, Exclude: r.Exclude, Enabled: r.Enabled}
}
```

Make `buildWireRequest` target-aware: `switch llm.ReasoningTarget(c.cfg.Provider, c.cfg.BaseURL)`:
- `OpenRouter`/`None` → today's `Reasoning: buildWireReasoning(req.Reasoning)` UNCHANGED (xhigh/max serialize automatically once the const exists — `Effort: string(r.Effort)`).
- `LlamaCpp` → translate `req.Reasoning.Effort` → `chat_template_kwargs:{enable_thinking:false}` (off) or `thinking_budget_tokens:N` (512/2048/8192/16384/-1 for low/mid/high/extra/max), leave `Reasoning` nil (the OpenRouter object is a NO-OP on llama-server, spike 095). Put the effort→budget consts in `openai_compat`.

**Coverage-load-bearing:** this branch MUST be covered by a daemon-free `TestBuildWireRequestReasoningTarget` table test — a container/live-gated test contributes ZERO coverage in the gate (CLAUDE.md rule).

---

### `internal/agent/llm_agent.go` + `llm_agent_reasoning.go` — override-vs-auto seam (MODIFY)

**Analog:** `buildRequest` tier branch (`llm_agent.go:445-450`) + `adaptiveReasoningTier` OpenRouter gate (`llm_agent_reasoning.go:23-26`).

```go
// buildRequest (llm_agent.go:445-450) — the adaptive-vs-plain selector to extend:
func (a *LlmAgent) buildRequest(budget prompt.Budget, tier prompt.ReasoningTier, tierOK bool) llm.Request {
	if tierOK {
		return a.builder.BuildWithReasoningTier(a.history, a.registry, a.cfg.Provider, a.cfg, budget, tier, a.activated)
	}
	return a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget, a.activated)
}

// adaptiveReasoningTier (llm_agent_reasoning.go:23-26) — SKIP this when a fixed override is set:
func (a *LlmAgent) adaptiveReasoningTier(ctx context.Context) (prompt.ReasoningTier, bool) {
	if !a.cfg.AdaptiveReasoning || !prompt.IsOpenRouterReasoningTarget(a.cfg.Provider, a.cfg.BaseURL) {
		return "", false
	}
	...
```

Add a `reasoningOverride llm.ReasoningEffort` field to the `LlmAgent` struct + `LlmAgentConfig`. In `Run`, when the override is set (fixed), SKIP `adaptiveReasoningTier` and route `buildRequest` to a new fixed branch calling `BuildWithReasoningOverride`; when unset/auto, today's adaptive path is BYTE-IDENTICAL.

---

### `internal/runner/runner_reasoning.go` + `runner.go` — ctx-thread the override (CREATE + MODIFY)

**Analog:** `WithThreadLockHeld`/`threadLockHeld` (`runner.go:48-60`) — the exact ctx-value pattern to mirror.

```go
// runner.go:48-60 — the ctx-value pattern:
type threadLockHeldKey struct{}
func WithThreadLockHeld(ctx context.Context) context.Context {
	return context.WithValue(ctx, threadLockHeldKey{}, true)
}
func threadLockHeld(ctx context.Context) bool {
	held, _ := ctx.Value(threadLockHeldKey{}).(bool)
	return held
}
```

New `runner_reasoning.go`: `WithReasoningOverride(ctx, llm.ReasoningEffort) context.Context` + `reasoningOverride(ctx) (llm.ReasoningEffort, bool)`. In `buildAgent` (`runner.go:537-565`), read `reasoningOverride(ctx)` and pass it into `agent.LlmAgentConfig{... ReasoningOverride: eff}` (the config already threads per-turn scope like `gateway.WithResponder(boundedCtx)` at `:550`).

---

### `internal/llm/config.go` + `internal/settings/settings.go` — `AURA_LLM_PROVIDER` knob (MODIFY)

**Analog:** `applyEnvOverrides` Model/BaseURL branches (`config.go:310-315`) + `AllowedKeys` rows (`settings.go:47-48`).

```go
// config.go:310-315 — the env-override pattern (Provider is currently NOT settable via env):
if v := os.Getenv(envModel); v != "" { cfg.Model = v }
if v := os.Getenv(envBaseURL); v != "" { cfg.BaseURL = v }
// ADD: if v := os.Getenv("AURA_LLM_PROVIDER"); v != "" { cfg.Provider = v }
```

```go
// settings.go:47-48 — the AllowedKeys rows to mirror:
"AURA_LLM_MODEL":    {Kind: KindString, Label: "Primary LLM model"},
"AURA_LLM_BASE_URL": {Kind: KindString, Label: "Primary LLM base URL"},
// ADD: "AURA_LLM_PROVIDER": {Kind: KindString, Label: "Primary LLM provider (openrouter|llamacpp)"},
```

This lets the operator switch the whole backend (model + base URL + provider) from the Settings page (D-01 consistency) and gives `llm.ReasoningTarget` a positive llama.cpp key (OQ-1).

---

### `internal/llm/model_reasoning_caps.go` — OpenRouter models client + capability source (CREATE, net-new subsystem)

**Analog (anti-pattern to REPLACE, not extend):** `internal/llm/models.go` — a HARD-CODED table (exactly what D-13 forbids). **Reuse `normalizeModelID` verbatim** as the cache key.

```go
// models.go:22-38 — the hard-coded anti-pattern (do NOT add a reasoning column) + the REUSABLE helper:
var modelCapabilityTable = map[string]modelCapabilities{
	"deepseek/deepseek-v4-flash": {vision: false, audio: false},
	"minimax/minimax-m3":         {vision: true, audio: false},
}
func normalizeModelID(model string) string { // ← REUSE as the /models cache key
	model = strings.TrimSpace(model)
	if i := strings.IndexByte(model, ':'); i >= 0 { model = model[:i] }
	return model
}
```

**HTTP/transport analog:** `openai_compat.Client` request construction (`client.go:129-142`) — the `Authorization: Bearer` + `cfg.Headers` attribution pattern. But `openai_compat` is streaming-only; the models fetch is a NEW lightweight `GET {cfg.BaseURL}/models` client (NOT inside `openai_compat`).

```go
// openai_compat/client.go:129-142 — the auth/header wiring to mirror for the GET:
httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", ...)
httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
for k, v := range c.cfg.Headers { httpReq.Header.Set(k, v) }
```

Build `ModelCapabilityClient` (TTL cache keyed by `normalizeModelID`, injectable `now func() time.Time` for expiry tests, fake `http.RoundTripper` for the fixture test) + a `ReasoningCapabilitySource` interface with two impls (`openRouterReasoningCaps`, `llamaCppReasoningCaps` via `/props` + provider-config fallback). See RESEARCH §P2.2 for the exact DTO shapes. **CI never hits the network** — test via captured `testdata/openrouter_models.json` fixture.

---

### `internal/agui/composer_api.go` — `GET /api/composer/reasoning-capabilities` (MODIFY, add route + handler)

**Analog:** `handleComposerSkills` + `registerComposerRoutes` (SAME file, `:14-38`) — plain `RequireAuth` (NOT governance.read), nil-source degrades gracefully.

```go
// composer_api.go:14-38 — the exact route + handler shape to mirror:
const composerSkillsPath = "GET /api/composer/skills"
func (s *Server) registerComposerRoutes(mux *http.ServeMux) {
	mux.HandleFunc(composerSkillsPath, s.handleComposerSkills)
}
func (s *Server) handleComposerSkills(w http.ResponseWriter, _ *http.Request) {
	if s.governance.Skills == nil {
		http.Error(w, "skills unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{"skills": activeSkillRows(s.governance.Skills.ActiveSkills())})
}
```

Add `const composerReasoningCapsPath = "GET /api/composer/reasoning-capabilities"`, register it in `registerComposerRoutes`, add `handleReasoningCapabilities`. **Divergence from the skills handler:** a nil source returns the SAFE FALLBACK `{levels:["auto","off"], detected:false}` (200, NOT 503) — the composer must degrade (37D D-09), and the caps endpoint has a meaningful floor. Handler maps `[]llm.ReasoningEffort` → UI symbols (none→off … max→max), prepends `auto`, omits `off` when the model's reasoning is `mandatory`. DTO in RESEARCH §P2.C (`reasoningCapabilitiesDTO{Levels,Default,Backend,Detected}`).

---

### `internal/agui/server.go` — setter-injection + two-stage validation + persist (MODIFY)

**Setter analog:** `SetSettingsStore` (`settings_api.go:53`).

```go
// settings_api.go:49-53 — the setter-injection pattern (nil → 503/fallback until wired):
func (s *Server) SetSettingsStore(store settingsStore) { s.settings = store }
```

Add `func (s *Server) SetReasoningCapabilitySource(src llm.ReasoningCapabilitySource)`, wired by the daemon composition root (`serve`) after `NewServer`.

**Validation analog:** `handleRun` skill decode + owner gate (`server.go:308-347`).

```go
// server.go:314 — owner-scope gate that MUST precede effort validation (isolation before governance):
if _, err := s.conv.GetForIdentity(ctx, in.ThreadID, scopedIdentityID(ctx)); err != nil { ... 404 ... }

// server.go:342-347 — the skill field decode+resolve mirror (effort is decoded the same way):
if req.Aura.Skill != "" && s.governance.Skills != nil && modelUserMsg != nil {
	if body, ok := s.governance.Skills.SkillBody(req.Aura.Skill); ok { ... }
}
```

Add a pure `parseEffortSymbol(s string) (llm.ReasoningEffort, bool, bool)` (7 symbols → `(effort, isFixed, ok)`; `""`/`auto`→`("",false,true)`; unknown→`("",false,false)`→400) and the two-stage governance in `handleRun` AFTER `GetForIdentity`: Stage 1 syntactic enum → 400; Stage 2 capability (`s.reasoningCaps.AllowedEfforts(ctx)`, in-memory TTL cache, NO per-turn round-trip) → non-advertised level 400. On a fixed level, `ctx = runner.WithReasoningOverride(ctx, effort)`. Persist the symbol (§persistence). Body cap `maxRunBodyBytes` already applies (`server.go:292`). Full stage code in RESEARCH §P2.5.

---

### `internal/agui/server_run_request.go` — `Effort` DTO field (MODIFY)

**Analog:** the `Skill` field in BOTH structs (SAME file, `:16` + `:32`).

```go
// server_run_request.go:9-18 — Aura struct (add Effort mirroring Skill):
type runAgentRequest struct {
	RunAgentInput types.RunAgentInput
	Aura          struct {
		AttachmentIDs []string `json:"attachment_ids"`
		Skill         string   `json:"skill"`
		// ADD: Effort string `json:"effort"`
	}
}
// server_run_request.go:29-34 — the ext decode struct (add Effort there too):
var ext struct {
	Aura struct {
		AttachmentIDs []string `json:"attachment_ids"`
		Skill         string   `json:"skill"`
		// ADD: Effort string `json:"effort"`
	} `json:"aura"`
}
```

Extend `server_run_request_test.go` (the decode test) alongside.

---

### Persistence: `conversations.sql` + `store_identity.go` + `store.go` + `store_helpers.go` + `types.go` (MODIFY)

**Write-query analog:** `RenameConversationForIdentity` (`queries/conversations.sql:93-98`).

```sql
-- conversations.sql:93-98 — the owner-scoped :execrows mutation to mirror:
-- name: RenameConversationForIdentity :execrows
UPDATE aura.conversations
SET title = sqlc.arg(title)
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id);
```

New query (jsonb_set, no migration — the `metadata jsonb` column exists in `0005_conversations.up.sql`):
```sql
-- name: UpdateConversationReasoningEffortForIdentity :execrows
UPDATE aura.conversations
SET metadata = jsonb_set(COALESCE(metadata, '{}'::jsonb), '{reasoning_effort}', to_jsonb(sqlc.arg(effort)::text), true)
WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id);
```

**Store-method analog:** `RenameForIdentity` (`store_identity.go:153-179`) — the `db.WithIdentityTx` + `:execrows` 0-rows-not-owned pattern.

```go
// store_identity.go:153-179 — mirror this for UpdateReasoningEffortForIdentity:
func (s *Store) RenameForIdentity(ctx context.Context, conversationID, identityID, title string) (int64, error) {
	id, err := parseUUID("id", conversationID); ...
	owner, err := parseUUID("identity_id", identityID); ...
	var affected int64
	txErr := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		n, rErr := q.RenameConversationForIdentity(ctx, sqlc.RenameConversationForIdentityParams{ID: id, Title: ..., IdentityID: owner})
		affected = n
		return rErr
	})
	return affected, txErr
}
```

**Read-projection analog (metadata DROPPED today):** `conversationFromRow` (`store_helpers.go:22-41`) + `Conversation` struct (`store.go:98-110`). Add `ReasoningEffort string` to `Conversation` and parse it out of `r.Metadata` jsonb in `conversationFromRow` (empty/absent → `""`, hydrates as `auto`). The read queries ALREADY SELECT `metadata` — no query change on read.

**Interface analog:** `RenameForIdentity` row in the `ConversationStore` interface (`types.go:49`) — add `UpdateReasoningEffortForIdentity(ctx, convID, identityID, effort string) (int64, error)`.

---

### Frontend send-payload + hydrate (MODIFY + CREATE)

**Fold analog:** `buildAuraRunBody` skill fold (`auraRunBody.ts:8-19`).

```ts
// auraRunBody.ts:8-14 — the fold to extend (add effort, omit 'auto'):
const aura = {
  ...(opts.attachmentIds !== undefined && opts.attachmentIds.length > 0 ? { attachment_ids: opts.attachmentIds } : {}),
  ...(opts.skill !== undefined && opts.skill.length > 0 ? { skill: opts.skill } : {}),
  // ADD: ...(opts.effort && opts.effort !== 'auto' ? { effort: opts.effort } : {}),
};
```

**Options-type analog:** `skill?: string` in `StreamRunOptions` (`sseAdapter.ts:471-472`) → add `readonly effort?: string;`.

**Send-call + Composer-props analog:** the `pinnedSkill` spread + props (`ExternalStoreChat.tsx:151`, `:169`, `:506-513`).

```tsx
// ExternalStoreChat.tsx:151 — send spread (mirror for effort, but do NOT clear after send):
...(pinnedSkill !== null ? { skill: pinnedSkill.name } : {}),
// ExternalStoreChat.tsx:169 — pinned skill CLEARS after send; EFFORT MUST NOT (per-conversation):
if (pinnedSkill !== null) setPinnedSkill(null);   // ← effort has NO analog to this line
// ExternalStoreChat.tsx:506-513 — Composer props to mirror:
<Composer uploads={uploads} skills={skills} pinnedSkill={pinnedSkill} onPinSkill={setPinnedSkill} .../>
```

**State-hook analog (per-turn — DIFFERS):** `usePinnedSkill.ts` is per-turn; `useReasoningEffort.ts` is per-conversation persisted — hydrate `effort` from the conversation DTO on `threadId` change (read the `reasoning_effort` field, default `auto`), expose `effort`/`setEffort`, do NOT clear on send, clamp a stored value not in `levels` back to `auto`.

**Read-hook analog:** `useComposerSkills.ts` + `fetchComposerSkills` (`api.ts:25-32`) — the mount-fetch + degrade-to-fallback pattern for the NEW `useReasoningCapabilities.ts` + `fetchReasoningCapabilities`.

```ts
// useComposerSkills.ts:7-23 — the mounted-guard fetch to mirror (degrade on throw):
export function useComposerSkills(): readonly ComposerSkillRow[] {
  const [skills, setSkills] = useState<readonly ComposerSkillRow[]>([]);
  useEffect(() => { let active = true;
    void fetchComposerSkills().then((rows) => { if (active) setSkills(rows); }).catch(() => { if (active) setSkills([]); });
    return () => { active = false; };
  }, []);
  return skills;
}
// api.ts:25-32 — fetch client degrading to a safe fallback on ANY throw:
export async function fetchComposerSkills(): Promise<readonly ComposerSkillRow[]> {
  try { const body = await getJSON<{ skills?: readonly ComposerSkillRow[] }>(COMPOSER_SKILLS_PATH); return body.skills ?? []; }
  catch { return []; }
}
```

`useReasoningCapabilities.ts` degrades to `{levels:['auto','off'], detected:false}`. The selector renders `levels` DYNAMICALLY (D-13 core — NOT a hard-coded 7). `Composer.tsx` adds a compact ARIA selector near Send (`useTranslation` already imported at `Composer.tsx:14`); must not break the `/`-picker combobox or Enter-send/paste/drop.

---

## Shared Patterns

### Setter-injection at the composition root
**Source:** `SetSettingsStore` (`settings_api.go:53`), `SetTelegramBotProbe` (`:57`)
**Apply to:** `SetReasoningCapabilitySource` on the agui `Server`; wired in `serve` after `NewServer` alongside the other `Set*` calls. Nil → safe fallback, never 5xx.

### Owner-scoped mutation (403-vs-404 via :execrows)
**Source:** `RenameForIdentity` (`store_identity.go:153`) + `db.WithIdentityTx` + `RenameConversationForIdentity :execrows` (`conversations.sql:93`)
**Apply to:** `UpdateReasoningEffortForIdentity` — owner id = `scopedIdentityID(ctx)`; 0 rows = not owned. Add a cross-identity deny test.

### Composer read route behind plain RequireAuth (degrade, never 5xx to the picker)
**Source:** `registerComposerRoutes`/`handleComposerSkills` (`composer_api.go:21-38`) + `fetchComposerSkills` degrade (`api.ts:25`)
**Apply to:** the reasoning-capabilities endpoint + its fetch hook. RETURN ONLY allowed symbols + default + backend kind — never the model id, base URL, or API key (Threat §P2.G).

### exclude parity (D-10 — effort not visibility)
**Source:** `ReasoningTier.reasoning()` `exclude := boolPtr(!showReasoning)` (`reasoning_policy.go:79`, `boolPtr:119`)
**Apply to:** `ApplyFixedReasoning` — reuse the EXACT exclude derivation; the selector never touches CoT visibility.

### Lifted state hook to hold `ExternalStoreChat.tsx`/`sseAdapter.ts` under 600 LOC
**Source:** `usePinnedSkill.ts` + `auraRunBody.ts` (extracted in 37D)
**Apply to:** `useReasoningEffort.ts` + `useReasoningCapabilities.ts` — never inline into the near-cap files.

---

## No Analog Found

| File | Role | Data flow | Reason |
|------|------|-----------|--------|
| `internal/llm/model_reasoning_caps.go` (OpenRouter `/models` fetch + TTL cache) | service | request-response + cache | **No existing OpenRouter `/models` fetch anywhere in the codebase.** `models.go`/`prices.go` are hard-coded seed tables (the D-13 anti-pattern). Only partial analogs: `normalizeModelID` (reuse as key) + `openai_compat.Client` auth/header wiring (streaming-only, different verb). Net-new external-dependency vertical — build against a captured fixture (RESEARCH §P2.2). |
| `internal/llm` `llamaCppReasoningCaps` (`/props` probe) | service | request-response | No existing llama-server `/props` client. Derive capability from `AURA_LLM_PROVIDER=llamacpp` + spike-095 ops contract; `/props` is a best-effort narrowing (Wave-0 fixture `testdata/llamacpp_props.json`). |

---

## Cross-cutting notes for the planner

- **STALE decision to reconcile (OQ-5):** CONTEXT D-09a ("`Max` is NOT added; off/low/mid/high/auto only") is VOID — the revised D-02 (7 levels) supersedes it. The W1 PRD-amendment MUST delete/supersede D-09a across ROADMAP/REQUIREMENTS/prd so the plan ships the 7-level capability-gated set. A planner reading RESEARCH's pass-1 `<user_constraints>` D-02/D-09a literally would ship the WRONG set — the Pass-2 Addendum governs.
- **DRIFT:** the live e2e test is at `internal/llm/openai_compat/adaptive_reasoning_live_e2e_test.go`, NOT `internal/agent/prompt/...` as CONTEXT canonical_refs states.
- **Symbol vs. effort:** UI/wire symbol `mid`/`extra`/`max` ≠ internal `llm.ReasoningEffort` `medium`/`xhigh`/`max`. `parseEffortSymbol` owns the translation.
- **Sizing:** RESEARCH recommends a possible split at the capability-endpoint boundary (37E-a backend engine+caps, 37E-b composer UI). Planner's call against the per-phase plan-count norm.

## Metadata

**Analog search scope:** `internal/{llm,llm/openai_compat,agent,agent/prompt,agui,runner,conversations,db,settings}`, `web/src/chat/{,composer}`
**Files read (analog extraction):** 20 (14 Go, 6 web) + line-verified against RESEARCH's HEAD seam map
**Pattern extraction date:** 2026-07-10

## PATTERN MAPPING COMPLETE
