---
phase: 37D-composer-skill-picker
plan: 02
type: execute
wave: 2
depends_on: ["37D-01"]
files_modified:
  - internal/agui/composer_api.go
  - internal/agui/composer_api_test.go
  - internal/agui/governance_seam.go
  - internal/agui/governance_api_test.go
  - internal/agui/server_run_request.go
  - internal/agui/server_run_request_test.go
  - internal/agui/server.go
  - internal/agui/server_skill_run_test.go
  - internal/agent/tools/skill_read.go
  - internal/agent/tools/skill_test.go
  - cmd/aura/serve_governance.go
  - cmd/aura/serve_webui_composer.go
  - cmd/aura/serve_webui.go
autonomous: true
requirements: [WEBSKILL-01, WEBSKILL-02]
must_haves:
  truths:
    - "GET /api/composer/skills returns 200 {skills:[{name,description,type}, ...]} projected from the active-skills loader snapshot for any authenticated identity; 503 when the skills provider is nil (client degrades to empty per D-09)"
    - "GET /api/composer/skills is mounted behind plain RequireAuth (bare aguiHandler on the parent mux), NOT behind governance.read — a non-admin identity WITHOUT the governance.read grant gets its NON-empty list (200), where the governance board route would 403"
    - "runAgentRequest.Aura carries a Skill string decoded from aura.skill on BOTH the typed struct and the ext-decode struct in server_run_request.go"
    - "when req.Aura.Skill resolves to a known skill, handleRun prepends the exact tools.UseAuthorityFrame + body + separator to the MODEL user message (via the existing TurnWithModelUserMessage split), leaving the persisted/visible turn as the raw user text; an unknown/empty pinned name is a no-op (never a passthrough, never a 5xx)"
    - "for every name in SkillsBoardProvider.ActiveSkills(), SkillBody(name) returns ok=true — the picker list is a subset of the resolvable set (no divergence, Pitfall 2)"
    - "the authority-frame literal is REUSED verbatim (tools.UseAuthorityFrame), not re-invented in internal/agui"
  artifacts:
    - path: "internal/agui/composer_api.go"
      provides: "handleComposerSkills + registerComposerRoutes(mux) mounting GET /api/composer/skills on the agui Server mux"
      contains: "handleComposerSkills"
    - path: "internal/agui/composer_api_test.go"
      provides: "daemon-free endpoint suite: active rows + RequireAuth-not-capability (non-admin gets its list, not 403) + nil-provider 503 + list-subset-of-resolvable guard"
      contains: "TestComposerSkills"
    - path: "cmd/aura/serve_webui_composer.go"
      provides: "composerSkillsRoute const + registerComposerRoutes(mux, aguiHandler, auth) bare RequireAuth-only mount"
      contains: "/api/composer/skills"
    - path: "internal/agui/server_skill_run_test.go"
      provides: "pinned-skill run suite: framed body prepended to model msg, raw visible turn persisted, unknown-name no-op"
      contains: "PinnedSkill"
  key_links:
    - from: "internal/agui/server.go"
      to: "internal/agui/composer_api.go"
      via: "s.registerComposerRoutes(mux) in Mux()"
      pattern: "registerComposerRoutes"
    - from: "internal/agui/server.go"
      to: "internal/agent/tools"
      via: "tools.UseAuthorityFrame prepend in handleRun"
      pattern: "UseAuthorityFrame"
    - from: "cmd/aura/serve_webui.go"
      to: "cmd/aura/serve_webui_composer.go"
      via: "registerComposerRoutes(mux, aguiHandler, auth) in newServeHandler"
      pattern: "registerComposerRoutes"
    - from: "internal/agui/server.go"
      to: "internal/agui/governance_seam.go"
      via: "s.governance.Skills.SkillBody(name) resolves the pinned body"
      pattern: "SkillBody"
  prohibitions:
    - "MUST NOT mount GET /api/composer/skills behind agui.RequireCapability(governanceReadCapability) — that 403s ordinary identities (the D-03 trap); mount bare like imageProxyRoute/voiceCapabilitiesRoute so it inherits the whole-mux RequireAuth"
    - "MUST NOT introduce a second skills loader/store — reuse s.governance.Skills (ActiveSkills for the list, SkillBody for the pinned body) so the picker, the governance board, and the runtime skill tool read ONE loader.List() snapshot (WEBSKILL-02 / Pitfall 2)"
    - "MUST NOT accept the client-supplied skill name as a filesystem path — resolve ONLY via loader Get/SkillBody (the validated snapshot key set); an unknown name is a no-op, never a path read, never a passthrough of client text into the model message"
    - "MUST NOT re-declare the authority-frame literal in internal/agui — export tools.UseAuthorityFrame from internal/agent/tools and reuse it verbatim (DRY; the string is the contract)"
    - "MUST NOT wire NewSkillToolForIdentity or any per-identity skill filtering — D-04 verdict is a global snapshot; per-identity scoping is deferred"
    - "MUST NOT let composer_api.go or serve_webui.go exceed 600 LOC — the endpoint handler lives in the new composer_api.go, the parent-mux mount in the new serve_webui_composer.go"
---

<objective>
Build the backend spine of the picker: the lean authenticated read route `GET /api/composer/skills` (global active-skills snapshot behind plain `RequireAuth`, D-03/D-04) and the D-01 pinned-skill wire path (one `skill` field on the existing `aura` run-request envelope, applied server-side via Mechanism A — prepending the exact `useAuthorityFrame + body` string `skill action=use` emits to the MODEL user message through the existing `TurnWithModelUserMessage` seam, ZERO runner change, no new tool, no new skills store). All logic is pure request/response over the already-wired skills provider and is proven by a daemon-free unit suite that satisfies the owned-surface ≥85% gate.

Purpose: Deliver the API contract (route shape, auth tier, the aura.skill envelope, deterministic first-skill application) the web consumers (37D-03/04) and the e2e (37D-05) build against, with a divergence guard so the listed set equals the invocable set.
Output: `internal/agui/composer_api.go` (+ test), `SkillBody` on `SkillsBoardProvider` + its adapter, the `Skill` field on `runAgentRequest.Aura`, the Mechanism-A prepend in `handleRun`, the exported `tools.UseAuthorityFrame`, `cmd/aura/serve_webui_composer.go`, and the one-line register wiring.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/ROADMAP.md
@.planning/phases/37D-composer-skill-picker/37D-RESEARCH.md
@.planning/phases/37D-composer-skill-picker/37D-PATTERNS.md
@internal/agui/governance_api.go
@internal/agui/governance_seam.go
@internal/agui/server_run_request.go
</context>

<artifacts_produced>
This plan produces:
- **`internal/agui/composer_api.go`**: `handleComposerSkills(w, r)` (nil `s.governance.Skills` → 503; else `writeJSON(w, {"skills": activeSkillRows(s.governance.Skills.ActiveSkills())})`) + `registerComposerRoutes(mux)` mounting `GET /api/composer/skills` → `handleComposerSkills` on the agui `Server.Mux`.
- **`SkillsBoardProvider.SkillBody(name string) (string, bool)`** added to the interface (`governance_seam.go`), implemented on `skillsBoardAdapter` (`serve_governance.go`) via the loader (`sk, ok := a.loader.Get(name); return sk.Body, ok`) and on the `scriptedSkillsBoard` test fake.
- **`runAgentRequest.Aura.Skill string`** (`json:"skill"`) on BOTH the typed struct and the ext-decode struct in `server_run_request.go`.
- **`tools.UseAuthorityFrame`** — the previously-unexported `useAuthorityFrame` const in `internal/agent/tools/skill_read.go` exported (its doc-comment + both in-file uses AND the same-package white-box test `skill_test.go:228` updated), so the agui server reuses the exact literal.
- **Mechanism-A prepend** in `handleRun` (`server.go`): after `buildTurnUserMessage`, when `req.Aura.Skill != ""` and `s.governance.Skills != nil` and `SkillBody(name)` returns ok, set `modelUserMsg = UseAuthorityFrame + body + "\n\n" + *modelUserMsg` BEFORE the `*userMsg != *modelUserMsg` split test.
- **`cmd/aura/serve_webui_composer.go`**: `const composerSkillsRoute = "GET /api/composer/skills"` + `registerComposerRoutes(mux, aguiHandler, auth)` → `mux.Handle(composerSkillsRoute, aguiHandler)` (bare, RequireAuth-only) + the one-line `registerComposerRoutes(mux, aguiHandler, auth)` call in `serve_webui.go`.
- Tests: `composer_api_test.go`, `server_skill_run_test.go`, and the extended `server_run_request_test.go` for the `skill` decode.
</artifacts_produced>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: GET /api/composer/skills — handler + RequireAuth-only mount + daemon-free suite (WEBSKILL-01)</name>
  <files>internal/agui/composer_api.go, internal/agui/composer_api_test.go, internal/agui/server.go, cmd/aura/serve_webui_composer.go, cmd/aura/serve_webui.go</files>
  <behavior>
    - TestComposerSkills_Active: govServer(GovernanceProviders{Skills: &scriptedSkillsBoard{active: [{Name:"active-one",Description:"a",Type:"instruction"}]}}) + GET /api/composer/skills → 200; body {skills:[...]} contains "active-one" and the keys name/description/type.
    - TestComposerSkills_RequireAuthNotCapability: an identity WITHOUT governance.read still gets a 200 NON-empty list from the composer route, where the governance route (RequireCapability(governanceReadCapability)) would 403 — assert the composer mount inherits plain RequireAuth only (mirror auth_test.go 403/200 differential).
    - TestComposerSkills_Unauth: RequireAuth(s.Mux(), testDeps) + no cookie → 401.
    - TestComposerSkills_NilProvider: govServer(GovernanceProviders{Skills: nil}) → 503 (client degrades to empty, D-09).
  </behavior>
  <read_first>
    - internal/agui/governance_api.go:261-308 — handleSkillsList (the stageActive branch to mirror MINUS the stage switch) + activeSkillRows (reuse VERBATIM) + writeJSON/writeJSONStatus helpers (already in-package).
    - internal/agui/governance_seam.go:33-57 — SkillsBoardProvider.ActiveSkills() + GovernanceProviders bundle (s.governance.Skills is the already-wired provider seam; nil-check → 503).
    - internal/agui/governance_api_test.go:37-59,299-341 — scriptedSkillsBoard fake + govServer(GovernanceProviders{...}) + doGov(t, s, method, path) harness to reuse for the endpoint assertions.
    - internal/agui/auth_test.go:335,506-566 — TestRequireAuth (401 without cookie) + TestRequireCapability (403 without the grant): the differential to prove the composer route is RequireAuth-only, not capability-gated.
    - internal/agui/server.go around Mux() — where s.registerAssetRoutes(mux)/s.registerVoiceRoutes(mux) register agui-side routes; add s.registerComposerRoutes(mux) beside them (read the existing register-call block to place it correctly).
    - cmd/aura/serve_webui_voice.go (whole file) — the EXACT 600-LOC-split + bare-aguiHandler RequireAuth-only precedent (voiceCapabilitiesRoute) to copy for serve_webui_composer.go.
    - cmd/aura/serve_webui.go — the governanceSkillsRoute RequireCapability mount (the 403 trap to AVOID), the plain imageProxyRoute mount (the model to follow), the whole-mux `return agui.RequireAuth(mux, auth)` wrap, and where registerVoiceRoutes(mux, aguiHandler, auth)/registerMUSRRoutes(...) are called in newServeHandler (add registerComposerRoutes beside them).
    - .planning/phases/37D-composer-skill-picker/37D-PATTERNS.md § "NEW internal/agui/composer_api.go" + § "NEW cmd/aura/serve_webui_composer.go" — the handler + mount shapes.
  </read_first>
  <action>
    Create internal/agui/composer_api.go with `handleComposerSkills(w http.ResponseWriter, _ *http.Request)`: if `s.governance.Skills == nil` → `http.Error(w, "skills unavailable", http.StatusServiceUnavailable)`; else `writeJSON(w, map[string]any{"skills": activeSkillRows(s.governance.Skills.ActiveSkills())})` (reuse the existing `activeSkillRows` projection VERBATIM — one source of truth). Add `registerComposerRoutes(mux *http.ServeMux)` mounting `GET /api/composer/skills` → `s.handleComposerSkills` on the agui Server mux, and call `s.registerComposerRoutes(mux)` in `Mux()` beside `s.registerVoiceRoutes(mux)`. Create cmd/aura/serve_webui_composer.go mirroring serve_webui_voice.go: `const composerSkillsRoute = "GET /api/composer/skills"` + `registerComposerRoutes(mux *http.ServeMux, aguiHandler http.Handler, auth agui.AuthDeps)` doing `mux.Handle(composerSkillsRoute, aguiHandler)` (bare aguiHandler — RequireAuth-only, NO RequireCapability), with a package doc-comment noting the anti-403 rationale (like imageProxyRoute/voiceCapabilitiesRoute, NOT the governance.read-gated board route). Add the one-line `registerComposerRoutes(mux, aguiHandler, auth)` call in serve_webui.go's newServeHandler beside registerVoiceRoutes. Create composer_api_test.go implementing the `<behavior>` suite over the scriptedSkillsBoard fake + govServer + doGov harness, including the RequireAuth-not-capability differential (a non-admin identity gets its list, not 403). Refactor-on-touch: keep composer_api.go and serve_webui.go ≤600 LOC; no dead code.
  </action>
  <acceptance_criteria>
    - `grep -q "func (s \*Server) handleComposerSkills" internal/agui/composer_api.go` AND `grep -q "activeSkillRows(s.governance.Skills.ActiveSkills())" internal/agui/composer_api.go`.
    - `grep -q "registerComposerRoutes" internal/agui/server.go` (wired into Mux()).
    - `grep -q "GET /api/composer/skills" cmd/aura/serve_webui_composer.go` AND the mount is bare `mux.Handle(composerSkillsRoute, aguiHandler)` with NO `RequireCapability` on that line.
    - `grep -q "registerComposerRoutes(mux, aguiHandler, auth)" cmd/aura/serve_webui.go`.
    - `go test ./internal/agui/ -run TestComposerSkills` passes (active rows, RequireAuth-not-capability non-empty 200, 401, nil-provider 503).
    - `go build ./...` + `go vet ./internal/agui/ ./cmd/aura/` clean; composer_api.go and serve_webui.go ≤600 LOC.
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/agui/ -run TestComposerSkills && go build ./... && go vet ./internal/agui/ ./cmd/aura/ && echo COMPOSER_ENDPOINT_OK</automated>
  </verify>
  <done>GET /api/composer/skills returns the global active-skills rows for any authenticated identity (200, non-empty for a non-admin), 401 unauth, 503 nil-provider, mounted bare RequireAuth-only on the parent mux; serve_webui.go stays ≤600 LOC.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Pinned-skill wire path — aura.skill decode + SkillBody seam + Mechanism-A prepend in handleRun + exported UseAuthorityFrame (WEBSKILL-02)</name>
  <files>internal/agui/server_run_request.go, internal/agui/server_run_request_test.go, internal/agui/governance_seam.go, internal/agui/governance_api_test.go, internal/agui/server.go, internal/agui/server_skill_run_test.go, internal/agent/tools/skill_read.go, internal/agent/tools/skill_test.go, cmd/aura/serve_governance.go</files>
  <behavior>
    - TestDecodeRunAgentRequest_Skill: a body {"aura":{"attachment_ids":["a1"],"skill":"skill-creator"}} decodes to Aura.Skill=="skill-creator" AND Aura.AttachmentIDs==["a1"] (both fields on both the typed + ext structs).
    - TestRun_PinnedSkill_Applied: serveRunWithPrincipal(body with aura.skill="skill-creator", user content "do the thing"), scriptedRunner recording gotVisibleUserMsg/gotModelUserMsg via TurnWithModelUserMessage → 200; gotVisibleUserMsg == "do the thing" (raw, persisted); gotModelUserMsg contains tools.UseAuthorityFrame AND the resolved skill body AND "do the thing".
    - TestRun_PinnedSkill_UnknownName_NoOp: aura.skill="does-not-exist" (SkillBody returns ok=false) → 200; gotModelUserMsg does NOT contain the authority frame; the model msg equals what buildTurnUserMessage produced (no client text injected as a skill body) — an unknown pinned name is a clean no-op, not a 5xx.
    - TestRun_NoSkill_Unchanged: no aura.skill → behavior byte-identical to today (no split forced by a skill).
    - TestComposerListSubsetOfResolvable: for every skills.Skill in a scriptedSkillsBoard{active}, SkillBody(sk.Name) returns ok=true (Pitfall 2 guard — the listed set is a subset of the resolvable set).
    - All run tests wrap in goleak.VerifyNone.
  </behavior>
  <read_first>
    - internal/agui/server_run_request.go (whole file, 34 lines) — the runAgentRequest struct + decodeRunAgentRequest double-decode (typed RunAgentInput + raw ext.Aura); add `Skill string json:"skill"` to BOTH the typed Aura struct (L11-13) and the ext.Aura struct (L26-28), preserving the existing double-decode idiom.
    - internal/agui/server.go:316-360 — lastUserMessage, buildTurnUserMessage (produces modelUserMsg with attachment/doc-catalog context), and the `turn := s.run.Turn(...)` + `if userMsg != nil && modelUserMsg != nil && *userMsg != *modelUserMsg { ... TurnWithModelUserMessage(...) }` split (L355-360). Insert the pinned-skill prepend AFTER buildTurnUserMessage (L323) and BEFORE the split test (L356) so the framed skill leads and the split fires.
    - internal/agent/tools/skill_read.go:11-15,115-119,129-140 — the `useAuthorityFrame` const + its doc-comment (L11) + uses (rename ALL to exported `UseAuthorityFrame`: const L15, actionUse L119, renderSnippetUse L131) and the exact `useAuthorityFrame+body` output actionUse emits (reuse verbatim).
    - internal/agent/tools/skill_test.go:228 — the THIRD, same-package (white-box) reference to `useAuthorityFrame` (a `strings.HasPrefix(res.Preview, useAuthorityFrame)` assertion); it MUST be renamed to `UseAuthorityFrame` too, or this task's own `go test ./internal/agent/tools/ -run Skill` verify fails to COMPILE. `grep -rn "useAuthorityFrame" internal/` shows exactly these hits (const, doc, two uses, this test).
    - internal/agui/governance_seam.go:33-41 — SkillsBoardProvider (add `SkillBody(name string) (string, bool)` to the interface; update the doc-comment).
    - cmd/aura/serve_governance.go:99-120 — skillsBoardAdapter (add `func (a skillsBoardAdapter) SkillBody(name string) (string, bool) { sk, ok := a.loader.Get(name); return sk.Body, ok }`; the loader.Get + skills.Skill.Body field already exist — loader.go:32,103-109).
    - internal/agui/governance_api_test.go:40-59 — scriptedSkillsBoard fake (add a SkillBody method: look up a `bodies map[string]string` or derive from `active`, returning ok for known names).
    - internal/agui/server_assets_run_test.go (whole file) — TestServerRunPrependsAttachmentBlock structure + the scriptedRunner{events:...} harness recording gotVisibleUserMsg/gotModelUserMsg via TurnWithModelUserMessage (server_test.go:152-167) + serveRunWithPrincipal(t, s, body): copy this structure exactly for server_skill_run_test.go.
    - internal/skills/loader.go:26-34,102-109 — the skills.Skill struct (Body field) + Get(name) (string,bool)-shaped snapshot read that backs SkillBody.
    - .planning/phases/37D-composer-skill-picker/37D-RESEARCH.md § Pattern 3 + § Pattern 4 + § Security Domain (V5 input validation) — the wire path + Mechanism A + the unknown-name-is-no-op / name-is-only-a-loader-key mitigation.
  </read_first>
  <action>
    (a) In server_run_request.go add `Skill string json:"skill"` to the typed `runAgentRequest.Aura` struct AND to the inner `ext.Aura` struct in decodeRunAgentRequest (one field each; keep the double-decode idiom). Extend server_run_request_test.go (create if absent) with TestDecodeRunAgentRequest_Skill asserting both fields decode.
    (b) In internal/agent/tools/skill_read.go rename the unexported `useAuthorityFrame` const to exported `UseAuthorityFrame`, update its doc-comment (L11) to lead with the new exported name, and update ALL in-package references: the two uses in skill_read.go (actionUse L119, renderSnippetUse L131) AND the same-package white-box assertion in skill_test.go:228. No behavior change — same literal. Guard: `grep -rn "useAuthorityFrame" internal/` MUST return zero hits after the rename, else the tools test package will not COMPILE and this task's own `go test ./internal/agent/tools/ -run Skill` verify fails.
    (c) In governance_seam.go add `SkillBody(name string) (string, bool)` to the SkillsBoardProvider interface (doc: resolves a skill body from the SAME loader snapshot ActiveSkills lists — one source of truth for the composer pinned-skill application). Implement it on skillsBoardAdapter (serve_governance.go) as `sk, ok := a.loader.Get(name); return sk.Body, ok`. Add a SkillBody method to the scriptedSkillsBoard fake (governance_api_test.go) returning a canned body for known active names, ok=false otherwise.
    (d) In server.go handleRun, after `modelUserMsg, code, emsg := s.buildTurnUserMessage(...)` (and its error return) and BEFORE the `turn := s.run.Turn(...)` / split block: if `req.Aura.Skill != "" && s.governance.Skills != nil && modelUserMsg != nil`, resolve `body, ok := s.governance.Skills.SkillBody(req.Aura.Skill)`; if ok, set `framed := tools.UseAuthorityFrame + body + "\n\n" + *modelUserMsg` and `modelUserMsg = &framed` (import internal/agent/tools). If the name is unknown (ok=false) or the skill/provider is absent, do NOTHING (no-op — never inject client text, never 5xx). The existing `*userMsg != *modelUserMsg` split then fires so the model sees the framed skill first while the visible/persisted turn stays the raw user text. Keep the pinned-skill logic to a few lines (extract a tiny `applyPinnedSkill(modelUserMsg *string, skill string) *string` helper in composer_api.go if it aids testing/readability, but do NOT add a second loader).
    (e) Create server_skill_run_test.go mirroring server_assets_run_test.go: implement the `<behavior>` run cases (applied, unknown-name no-op, no-skill unchanged) over the scriptedRunner harness + a scriptedSkillsBoard wired via SetGovernanceProviders, plus TestComposerListSubsetOfResolvable (the Pitfall-2 guard). Wrap in goleak.VerifyNone.
    Refactor-on-touch: keep server.go ≤600 LOC (extract the helper if the prepend pushes it near the cap); update touched doc-comments; no dead code.
  </action>
  <acceptance_criteria>
    - `grep -q 'Skill string \`json:"skill"\`' internal/agui/server_run_request.go` on BOTH structs (two matches).
    - `grep -q "UseAuthorityFrame" internal/agent/tools/skill_read.go` (exported) AND `grep -q "UseAuthorityFrame" internal/agui/server.go` (reused, not re-declared) AND `grep -rn "useAuthorityFrame" internal/ | wc -l` returns 0 (the lowercase const, its two uses, AND the skill_test.go:228 white-box assertion all renamed — the tools test package compiles).
    - `grep -q "SkillBody" internal/agui/governance_seam.go` AND `grep -q "func (a skillsBoardAdapter) SkillBody" cmd/aura/serve_governance.go`.
    - handleRun resolves the pinned skill via `s.governance.Skills.SkillBody(` (no second loader, no filesystem path from the client).
    - `go test ./internal/agui/ -run 'TestDecodeRunAgentRequest_Skill|TestRun_PinnedSkill|TestRun_NoSkill|TestComposerListSubsetOfResolvable'` passes (applied-first, raw-visible-persisted, unknown-name no-op, subset guard).
    - `go build ./...` + `go vet ./internal/agui/ ./internal/agent/tools/ ./cmd/aura/` clean; server.go ≤600 LOC.
  </acceptance_criteria>
  <verify>
    <automated>go test ./internal/agui/ -run 'TestDecodeRunAgentRequest_Skill|TestRun_PinnedSkill|TestRun_NoSkill|TestComposerListSubsetOfResolvable' && go test ./internal/agent/tools/ -run Skill && go build ./... && go vet ./internal/agui/ ./internal/agent/tools/ ./cmd/aura/ && echo PINNED_SKILL_OK</automated>
  </verify>
  <done>aura.skill decodes on both structs; handleRun deterministically prepends the reused tools.UseAuthorityFrame + resolved body to the model message (raw visible turn persisted) for a known skill and no-ops on an unknown name; SkillBody is served from the same provider ActiveSkills reads (subset guard green); server.go ≤600 LOC.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| browser → GET /api/composer/skills | An authenticated but non-admin identity must receive its skills list (not a 403); the response carries only non-sensitive skill metadata (name/description/type), never bodies |
| browser → POST /agent/run (aura.skill) | An untrusted client-supplied skill NAME crosses into handleRun; it must be treated as a loader key only, never a filesystem path or a passthrough into the model message |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37D-01 | Denial of Service (self) | GET /api/composer/skills mount | mitigate | Mounted bare `mux.Handle(composerSkillsRoute, aguiHandler)` → inherits whole-mux RequireAuth ONLY, NOT governance.read; proven by TestComposerSkills_RequireAuthNotCapability (non-admin gets 200 non-empty, not 403) |
| T-37D-02 | Tampering / Information Disclosure (path traversal via skill name) | handleRun pinned-skill resolution | mitigate | The client sends a NAME only; the server resolves it via `s.governance.Skills.SkillBody(name)` → `loader.Get(name)` against the validated snapshot key set; an unknown name is a no-op; the name is never joined to a path (the loader already symlink-strips + body-caps + blocklist-scans at load) |
| T-37D-03 | Tampering (prompt-injection via skill body) | Mechanism-A authority-frame prepend | mitigate | Bodies already pass the loader's load-time NFKC+literal injection blocklist scan; the reused `tools.UseAuthorityFrame` is the SAME frame `action=use` emits — no new trust surface, effect byte-identical to the existing runtime contract |
| T-37D-04 | Denial of Service (oversized payload) | aura.skill field on POST /agent/run | mitigate | handleRun already caps the body via `http.MaxBytesReader(w, r.Body, maxRunBodyBytes)` before decode; the field is a short bounded name |
| T-37D-SC | Tampering | npm/pip/cargo installs | accept | 37D installs NO external packages (RESEARCH § Package Legitimacy Audit: N/A); no slopcheck surface |
</threat_model>

<verification>
- `go test ./internal/agui/ ./internal/agent/tools/ ./cmd/aura/` passes with the new suites (daemon-free).
- `go build ./...` + `go vet ./internal/agui/ ./internal/agent/tools/ ./cmd/aura/` clean.
- Full-matrix `-race` on the touched packages runs green in WSL at the wave boundary (goleak.VerifyNone — no goroutine leaks).
- File-size hook: composer_api.go, server.go, serve_webui.go all ≤600 LOC.
</verification>

<success_criteria>
- `GET /api/composer/skills` answers the global active-skills snapshot for any authenticated identity behind plain RequireAuth (401 unauth, 503 nil-provider), proven NOT to require governance.read.
- The pinned skill rides one `skill` field on the existing `aura` envelope and is applied server-side via Mechanism A (reused tools.UseAuthorityFrame + resolved body prepended to the model message, raw visible turn persisted), with an unknown name a clean no-op and the listed set a subset of the resolvable set — no runner change, no new tool, no new skills source of truth.
</success_criteria>

<output>
Create `.planning/phases/37D-composer-skill-picker/37D-02-SUMMARY.md` when done.
</output>
