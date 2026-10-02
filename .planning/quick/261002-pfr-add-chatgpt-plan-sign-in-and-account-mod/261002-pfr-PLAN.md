---
phase: quick-chatgpt-plan
plan: 01
type: execute
wave: 1
depends_on: []
autonomous: false
status: in_progress
requirements: [CRED-07, E2E-05]
files_modified:
  - internal/chatgptplan/service.go
  - internal/chatgptplan/store.go
  - internal/chatgptplan/oauth.go
  - internal/chatgptplan/files_unix.go
  - internal/chatgptplan/files_windows.go
  - internal/chatgptplan/service_test.go
  - internal/llm/chatgpt/client.go
  - internal/llm/chatgpt/request.go
  - internal/llm/chatgpt/stream.go
  - internal/llm/chatgpt/models.go
  - internal/llm/chatgpt/client_test.go
  - internal/llm/chatgpt/models_test.go
  - internal/agui/settings_chatgpt.go
  - internal/agui/settings_chatgpt_test.go
  - internal/agui/settings_chatgpt_idempotency_test.go
  - internal/agui/server.go
  - internal/agui/settings_api.go
  - internal/agui/settings_llm_models.go
  - internal/runner/runner_identity_llm.go
  - internal/runner/runner_identity_chatgpt.go
  - internal/runner/runner_identity_chatgpt_test.go
  - internal/llm/pricing_source.go
  - internal/llm/chatgpt_profile.go
  - internal/llm/chatgpt_profile_test.go
  - cmd/aura/serve_chatgpt.go
  - cmd/aura/chatgpt-browser.mjs
  - cmd/aura/serve_chatgpt_browser.go
  - cmd/aura/serve_chatgpt_browser_test.go
  - cmd/aura/serve_browser_live.go
  - internal/chatgptplan/browser.go
  - internal/chatgptplan/browser_service.go
  - internal/chatgptplan/browser_test.go
  - internal/agui/browser_live.go
  - internal/agui/browser_live_chatgpt_test.go
  - internal/sandbox/usersandbox/docker_backend_exec.go
  - internal/sandbox/usersandbox/docker_backend_exec_test.go
  - cmd/aura/chat_boot.go
  - cmd/aura/serve_webui.go
  - cmd/aura/serve_settings.go
  - cmd/aura/llm_client.go
  - cmd/aura/serve_provisioning_openrouter.go
  - cmd/aura/serve_chatgpt_test.go
  - web/src/settings/ChatGPTPlanConnection.tsx
  - web/src/settings/chatgptPlanApi.ts
  - web/src/settings/useChatGPTPlanConnection.ts
  - web/src/settings/modelSettingsDefs.ts
  - web/src/settings/ModelSettingsPanel.tsx
  - web/src/settings/settingsApi.ts
  - web/src/settings/SettingsWorkspace.tsx
  - web/src/settings/SettingField.tsx
  - web/src/settings/ModelPicker.tsx
  - web/src/settings/useModelCatalog.ts
  - web/src/settings/__tests__/ChatGPTPlanConnection.test.tsx
  - web/src/settings/__tests__/ModelSettingsPanel.chatgpt.test.tsx
  - web/src/i18n/resources.settings.ts
  - docs/chatgpt-plan.md
  - prd.md
must_haves:
  truths:
    - "Settings offers ChatGPT beside the existing providers, with a login button and account model selection (D-01)."
    - "Connecting, refreshing and disconnecting affect only the current Aura identity."
    - "Saving an available ChatGPT model makes the next real agent turn use the ChatGPT plan through Responses (D-02)."
    - "Missing credentials, revoked access and usage limits produce actionable errors without borrowing an OpenRouter or another identity's credential."
  artifacts:
    - path: internal/chatgptplan/service.go
      provides: "Identity-scoped OAuth lifecycle and protected persistent credentials"
    - path: internal/llm/chatgpt/client.go
      provides: "Official SDK Responses implementation of llm.Client"
    - path: internal/llm/chatgpt/models.go
      provides: "Current account-specific model catalog"
    - path: web/src/settings/ChatGPTPlanConnection.tsx
      provides: "Login, connection status and disconnect UI"
  key_links:
    - from: web/src/settings/ChatGPTPlanConnection.tsx
      to: /api/settings/chatgpt/login
      via: "Authenticated login mutation and OAuth browser navigation"
    - from: sandbox-local /auth/callback
      to: internal/chatgptplan/service.go
      via: "One-use state binds callback to the initiating Aura identity"
    - from: internal/runner/runner_identity_llm.go
      to: internal/llm/chatgpt/client.go
      via: "Provider-specific identity resolution before OpenRouter credit policy"
    - from: web/src/settings/useModelCatalog.ts
      to: /api/settings/llm-models
      via: "chatgpt provider catalog with owner-scoped OAuth access token"
---

<objective>
Add working ChatGPT subscription access to local/self-hosted Aura. Per D-01, preserve the existing Ollama-style provider switch and model picker and add the login control. Per D-02, use the already installed official OpenAI SDK Responses surface while preserving Aura's agent and local tool execution. Per D-03, credentials remain isolated per Aura identity; account-management expansion is outside this request.
</objective>

<context>
@CLAUDE.md
@.planning/STATE.md
@.planning/REQUIREMENTS.md
@internal/secret/sealer.go
@internal/llm/client.go
@internal/llm/openai_compat/client.go
@internal/runner/runner_identity_llm.go
@cmd/aura/identity_llm_resolver.go
@cmd/aura/serve_settings.go
@cmd/aura/serve_webui.go
@web/src/settings/modelSettingsDefs.ts
@web/src/settings/ModelSettingsPanel.tsx

Decisions: D-01 = login button and model choice like Ollama; D-02 = user approved the custom SDK Responses adapter; D-03 = complete route with owner-scoped credentials, no generalized multi-account UI.

Latest deployment requirement (2026-10-02): work in WSL, target the existing VM, and return sign-in to that installation automatically. The user requested inspection of the existing MCP bridge, rejected a manual SSH callback tunnel and the Compose environment change, and authorized commit, push, CI and deployed E2E. `compose.yaml` remains identical to HEAD.

Measured remote solution: reuse Aura's existing per-owner sandbox Chromium and authenticated live browser view. The official OpenAI login page opens in that browser; a private ephemeral loopback listener in the same sandbox receives its callback and transports it through the existing SandboxRouter exec stream to the OAuth service on the VM. The cockpit popup uses only a same-origin reserved `/browser/chatgpt-<random>` route. Existing MCP/GitHub callbacks rely on provider-accepted HTTPS registrations and cannot change OpenAI's OSS loopback registration. No website identity-only client or public callback replacement is used. The validation report records actual VM navigation/frame/cleanup measurements and distinguishes them from unperformed human consent and inference.

Discovery: official OpenAI registration, model/inference, token and preview contracts checked 2026-10-02. Existing dependencies: github.com/openai/openai-go/v3 v3.66.0 and golang.org/x/oauth2 v0.37.0. Inventory shows the existing client targets Chat Completions; the SDK already supplies Responses. Reuse internal/secret.Sealer instead of implementing cryptography. No database migration or package install is required.

Official references:
- https://developers.openai.com/siwc/token-sharing-open-source/sign-in
- https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference
- https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations
- https://developers.openai.com/siwc/token-sharing-open-source/token-reference
- https://developers.openai.com/siwc/token-sharing-open-source/self-hosted-vms

OAuth contract: New(dir, secret); Start(ctx, identityID, redirectURI); Callback(ctx, url.Values); Status(ctx, identityID); AccessToken(ctx scoped with identityctx); Disconnect(ctx, identityID). Browser manager: Start(ctx, owner, attemptID), Cancel(ctx, owner, exactBrowserRoute), Owns(owner, session), Close(). HTTP: authenticated GET /api/settings/chatgpt/status, POST /api/settings/chatgpt/login, DELETE /api/settings/chatgpt/login, DELETE /api/settings/chatgpt. The private helper, rather than the public Aura router, owns /auth/callback. Browser response and SSE metadata never expose OAuth query parameters or tokens. Model catalog uses existing /api/settings/llm-models with provider=chatgpt and fixed https://api.openai.com/v1.

Profile discretion: ResolveChatGPTProfile validates the selected slug against the authenticated account catalog during route Prepare. With no operator pin and no catalog context metadata, use Aura's conservative 32768-token working budget explicitly as an application budget, never as a claim about the model's context window. Subscription status has no fabricated USD price; do not use the generic provider catalog/profile fetch for this route.

Execution ownership: OAuth package, SDK provider, frontend, composition root integration have disjoint responsibility. They may start against the contracts concurrently; root wiring and final verification require all three. Preserve unrelated dirty changes. Keep each touched source file at most 600 LOC, splitting concerns where required. This quick plan groups three workstreams as requested by the orchestrator; each workstream can be executed separately.
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Implement protected identity-scoped ChatGPT sign-in</name>
  <files>internal/chatgptplan/service.go, internal/chatgptplan/store.go, internal/chatgptplan/oauth.go, internal/chatgptplan/files_unix.go, internal/chatgptplan/files_windows.go, internal/chatgptplan/service_test.go</files>
  <behavior>
    - Fresh state, nonce and S256 verifier per attempt; wrong, expired or reused state cannot persist credentials.
    - Invalid token signature, issuer, audience, nonce, expiry or missing inference grant fails closed.
    - A successful callback uses the issued client ID and updates only the initiating identity; failed replacement leaves the active connection intact.
    - Restart preserves host ID and encrypted credentials; concurrent refresh rotates credentials atomically; disconnect prevents later access.
  </behavior>
  <action>Implement D-03 using the existing secret.Sealer. Store below filepath.Join(filepath.Dir(cfg.SkillsDir), "chatgpt-plan") with owner-only atomic files and identity-safe filenames; persist host UUID separately. Implement the declared service API with bounded requests and injectable HTTP seams. Follow official public-client OAuth: dynamic_agent_client first registration, stable ext_agent_host_id, agent_name_hint=Aura, independent random state/nonce/PKCE S256, documented scopes/resource, exact saved redirect URI, callback-issued client_id for exchange. Verify ID tokens against trusted OpenAI discovery/JWKS before accepting direct-inference scopes. Preserve issued client ID, verified identity, expiry/scopes and token rotation together; serialize refresh and prevent in-flight refresh resurrecting disconnected credentials. Status exposes connection/account metadata only. Redact credential material in all errors.</action>
  <verify><automated>go test ./internal/chatgptplan -count=1 -timeout=60s</automated></verify>
  <done>Service lifecycle works across restart, rejects unauthorized callbacks, never exposes tokens and isolates two identities under concurrent access.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Wire ChatGPT models and SDK Responses into live identity routing</name>
  <files>internal/llm/chatgpt/client.go, internal/llm/chatgpt/request.go, internal/llm/chatgpt/stream.go, internal/llm/chatgpt/models.go, internal/llm/chatgpt/client_test.go, internal/llm/chatgpt/models_test.go, internal/agui/settings_chatgpt.go, internal/agui/settings_chatgpt_test.go, internal/agui/server.go, internal/agui/settings_api.go, internal/agui/settings_llm_models.go, internal/runner/runner_identity_llm.go, internal/runner/runner_identity_chatgpt_test.go, internal/llm/pricing_source.go, internal/llm/chatgpt_profile_test.go, cmd/aura/serve_chatgpt.go, cmd/aura/serve_agui.go, cmd/aura/serve_webui.go, cmd/aura/serve_settings.go, cmd/aura/llm_client.go, cmd/aura/identity_llm_resolver.go, cmd/aura/serve_chatgpt_test.go</files>
  <behavior>
    - Live provider/model save publishes a runtime consumed by the next turn; the ChatGPT branch never loads an OpenRouter key or credit cap.
    - Two identities resolve their own ChatGPT token sources; missing/revoked credentials cannot fall back to another key.
    - Catalog preserves server order, visible display names and selected slugs; unavailable/hidden models are not offered.
    - SDK streaming maps text, reasoning summaries, tools and token usage; only response.completed finishes successfully.
    - Failed/incomplete/premature streams and subscription limits produce terminal errors; cancellation closes the stream without leaks.
  </behavior>
  <action>Implement D-02 as llm.Client using SDK Responses.NewStreaming with the owner-scoped AccessToken source; refresh when required at request time. Keep fixed https://api.openai.com/v1; translate full message/tool-result history to Responses input, system content to instructions/developer items, and function tools into an Aura namespace. Send store=false and stream=true; omit unsupported sampling/output-limit/state fields listed in preview docs. Map tool call IDs/names/argument deltas back to Aura; include authorized text/image/file projections and explicitly refuse unsupported audio/video inputs. Require response.completed; preserve usage and structured limit/auth errors. FetchModels uses the same scoped token, interprets the documented models array, visibility=list, slug/display_name and available capability/context metadata, and bounds payload size. Do not derive subscription prices from OpenRouter USD rates: unknown monetary costs remain unknown; context/capabilities come from catalog metadata. Add authenticated status/login/disconnect routes with existing capability/CSRF conventions. Reuse the existing owner sandbox browser and authenticated BrowserLivePage with a reserved random session. Launch a private callback listener in the same sandbox, carry its events over SandboxRouter ExecStream, and let the OAuth service validate pending state. Expose no public callback route or additional port. Compare-cancel exact attempt routes, join bounded cleanup on replacement and shutdown, invalidate cancelled grants before commit, and redact OAuth query values from no-store browser metadata. Register chatgpt in validation/factories and route memory; resolve it before the existing OpenRouter key policy, invalidating cached snapshots on account/model changes. Wire the same service into AGUI and the shared identity resolver used by turns, Telegram, cron and delegation; unavailable service fails closed.</action>
  <verify><automated>go test ./internal/llm/chatgpt ./internal/runner ./internal/agui ./internal/llm ./cmd/aura -run 'ChatGPT|Chatgpt|Resolver' -count=1 -timeout=60s</automated></verify>
  <done>Authenticated account catalog, model save, token renewal and tool-capable Responses inference run through Aura's existing turn path; callback is reachable without granting anonymous settings access.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Add the login control and model selection, then prove the complete flow</name>
  <files>web/src/settings/ChatGPTPlanConnection.tsx, web/src/settings/chatgptPlanApi.ts, web/src/settings/modelSettingsDefs.ts, web/src/settings/ModelSettingsPanel.tsx, web/src/settings/settingsApi.ts, web/src/settings/SettingsWorkspace.tsx, web/src/settings/ModelPicker.tsx, web/src/settings/useModelCatalog.ts, web/src/settings/__tests__/ChatGPTPlanConnection.test.tsx, web/src/settings/__tests__/ModelSettingsPanel.chatgpt.test.tsx, web/src/i18n/resources.settings.ts, docs/chatgpt-plan.md, prd.md</files>
  <behavior>
    - ChatGPT provider displays Continue with ChatGPT, busy/failure/connected status and disconnect; login follows the returned authorization URL.
    - Successful callback refreshes status/catalog, selection displays account model names and saves slugs with the same route-memory behavior as Ollama.
    - Provider/account changes discard stale catalog responses; disconnected accounts do not display a selectable stale list.
    - Existing OpenRouter/local/Ollama selections and saves retain their behavior.
  </behavior>
  <action>Per D-01, extend ProviderChoice/PROVIDER_OPTIONS, provider resolution, icons and route defaults with chatgpt. Reuse RouteToggle and ModelPicker with fixed OpenAI route; render the connection component only for ChatGPT. Use the declared endpoints with existing authenticated fetch conventions. Keep connection state separate from provider save; after returning from OAuth, refresh status and model catalog, surface meaningful access/usage errors, and retain selected slug when it still exists. Add localized labels and accessible status/error/button states. Extend catalog display-name support while leaving existing model IDs valid. Document the sandbox browser login path, durable storage location, ChatGPT plan usage/limits and loopback placement. Remote cockpit users complete login inside the authenticated live view; abandoned popups and late startup responses trigger exact-attempt cleanup. Handle already-approved startup without URL navigation. Run the stack and a real signed-in agent turn, including one local tool call and another turn after provider/model reload. Capture actual model slug and response.completed evidence without credentials. Update PRD only from measured evidence, explicitly recording what was not exercised. If account consent is unavailable, record the precise remaining live verification instead of marking OAuth complete.</action>
  <verify><automated>From web/: npm exec -- vitest run src/settings/__tests__/ChatGPTPlanConnection.test.tsx src/settings/__tests__/ModelSettingsPanel.chatgpt.test.tsx</automated><human-check>Complete OpenAI account consent when requested, then observe the connected status and account model choices on the live Aura Settings screen.</human-check></verify>
  <done>Login and model selection work in Settings, regressions pass, and the quick summary records a completed real turn or clearly identifies the outstanding user authentication gate.</done>
</task>

</tasks>

<threat_model>
| Boundary | Description |
|----------|-------------|
| Browser to settings | Owner session and existing authorization govern login/catalog/disconnect. |
| Private sandbox callback to stored account | One-use pending state and authenticated owner browser bind callback to the initiating owner. |
| OpenAI to Aura | JWKS verification, scopes, bounded parsing and fixed trusted endpoints govern external data. |
| Credentials to filesystem/runner | Encrypted durable owner records and identity-scoped token sources prevent cross-user access. |

| Threat ID | STRIDE | Component | Disposition | Mitigation |
|-----------|--------|-----------|-------------|------------|
| T-CP-01 | S/T | OAuth callback | mitigate | Random one-use expiring state, S256 PKCE, nonce and verified issuer/audience/signature. |
| T-CP-02 | I/E | Credential store and resolver | mitigate | Sealer encryption, owner-only atomic files, trusted identity scoping, no deployment-key fallback. |
| T-CP-03 | D/T | OAuth/catalog/stream | mitigate | Time/payload bounds, serialized rotation, cancellation, terminal-event validation. |
| T-CP-04 | R/I | Status/error reporting | mitigate | Safe metadata only and errors/logs that exclude codes/tokens/verifiers. |
| T-CP-SC | T | Dependencies | accept | Reuse pinned SDK, oauth2 and existing secret.Sealer; no new package install. |
</threat_model>

<verification>
Run targeted tests first, then go vet/build and race tests for all touched Go packages; run web typecheck, targeted Vitest, lint and build using existing scripts. Run project quality/coverage gates appropriate to the touched packages (85% aggregate and package policy; no skipped live tier counts as evidence), with mutation spot-check of callback validation and stream terminal handling. Verify anonymous settings and browser access are denied, cross-owner browser access fails, and the public Aura router does not provide an OAuth callback. On the running stack, complete browser login, fetch the real account catalog, save a visible model, perform a normal turn and one Aura tool round, switch away/back and repeat, then disconnect and verify refusal. Record live evidence and remaining account-dependent checks in 261002-pfr-SUMMARY.md; unit fixtures alone do not satisfy E2E-05. Preserve unrelated workspace changes.
</verification>

<success_criteria>
- D-01: existing provider/model UI supports ChatGPT login and actual account choices.
- D-02: next live agent turn uses SDK Responses with the saved account/model.
- D-03/CRED-07: isolation and failure paths never borrow another credential.
- E2E-05: completed live inference is measured and documented; unperformed consent is reported explicitly.
- Remote deployment: the browser's completed sign-in reaches the VM automatically through a supported OpenAI flow; the existing local-only flow does not satisfy this criterion.
</success_criteria>

## Multi-source coverage audit

| Source | Item | Task | Status |
|--------|------|------|--------|
| GOAL | User-requested ChatGPT subscription access (quick task, no roadmap phase created) | 1–3 | COVERED |
| REQ | CRED-07 existing no-deployment-key fallback constraint | 2 | COVERED |
| REQ | E2E-05 live proof constraint | 3 / verification | COVERED |
| RESEARCH | Registration, verified tokens, protected storage and refresh | 1 | COVERED |
| RESEARCH | Account catalog, SDK Responses, tools and preview restrictions | 2 | COVERED |
| RESEARCH | Loopback behavior and remote self-hosted instructions | 2–3 | COVERED |
| CONTEXT | D-01 login button/model selection like Ollama | 3 | COVERED |
| CONTEXT | D-02 approved custom SDK adapter | 2 | COVERED |
| CONTEXT | D-03 owner isolation and complete integration | 1–2 | COVERED |

<output>
Create .planning/quick/261002-pfr-add-chatgpt-plan-sign-in-and-account-mod/261002-pfr-SUMMARY.md with measured verification and exact outstanding account gates.
</output>
