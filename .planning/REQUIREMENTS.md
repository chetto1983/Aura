# Requirements: Aura — v2.0.0 Industrial Hardening & Multi-User Production

**Defined:** 2026-06-29
**Core Value:** Substrate agentico domain-neutral — un runtime Go che esegue un agentic loop multi-tool affidabile, ora **production-grade e multi-user**: ogni identità guida una sandbox full-capability isolata (host reale mai esposto), ogni tool call passa per una policy auditata, e l'intero audit industriale 2026-06-21 è chiuso a un onesto **10/10**.

**Source of truth:** the 51-finding audit in `docs/audit/` (F-001..F-052, F-044 absent) + the v2.0.0 research in `.planning/research/` (SUMMARY + STACK/FEATURES/ARCHITECTURE/PITFALLS) + the online corroboration `docs/research/senior-dev-agent-hardening-2026.md`.

**Locked decisions:** (a) F-001 resolved via per-user full-capability sandbox over Docker — capability never stripped, host never exposed; (b) multi-user = identity isolation only, **NO RBAC/OAuth/roles**; (c) Authula default cutover; (d) full industrialization to a literal, *honest* 10/10; (e) every hardening behavior is a no-op under `dev`/`local_trusted` — the operator's daily full-host experience is unchanged.

---

## v2.0.0 Requirements

### Runtime Profiles & Config Validation (PROF)

- [x] **PROF-01**: Operator can select a runtime profile (`dev`, `local_trusted`, `single_user_hardened`, `server_production`) and `aura config validate --profile <p>` reports every unmet requirement and fails non-zero. *(F-026)*
- [x] **PROF-02**: Copying `.env.example` to `.env` preserves the default destructive-shell approval gate (empty `AURA_SHELL_DESTRUCTIVE_PATTERNS` means "use defaults", only `off` disables); tests cover unset/empty/`off`/custom/copied-sample. *(F-002)*
- [x] **PROF-03**: `server_production` validation fails when object-store/Garage credentials, RPC secret, bucket, or endpoint are sample/default values; passes with supplied secrets. *(F-007)*
- [x] **PROF-04**: Invalid integer/boolean env values fail fast (error) under hardened/production profiles and warn (with diagnostics) under dev — never silently fall back for security/reliability knobs. *(F-016)*
- [x] **PROF-05**: `AURA_RUN_DIR` is normalized to an absolute path at config load, or rejected in validation/constructors; sidecars resolve identically across restarts and working directories. *(F-041)*
- [x] **PROF-06**: `server_production` validation rejects single-replica object-store topology (Garage `replication_factor = 1`) and documents it as development-only. *(F-018)*

### Agent-Loop Correctness, HITL & Persistence (LOOP)

- [x] **LOOP-01**: A terminal `text_response` is mutually exclusive with mutating/runnable sibling tools — such a model step is rejected or replanned before any sibling executes. *(F-003)*
- [x] **LOOP-02**: Batch pause resume atomically claims all pauses before injecting answers (single transaction or idempotency keys); concurrent duplicate batch resume yields exactly one answer per pause with no orphan tool turns. *(F-004)*
- [x] **LOOP-03**: Single pause resume couples claim + answer append through one transaction or a repairable idempotency ledger; an append failure after claim leaves the pause retryable or creates a recoverable resume-injection record. *(F-029)*
- [x] **LOOP-04**: A pause is never exposed without durable, wire-valid assistant tool-call history — pause tool-call turn and pause row persist atomically before the pause is consumable. *(F-030)*
- [x] **LOOP-05**: Conversation sidecars are loaded only from paths reconstructed from conversation ID + sequence and fenced to the sidecar root; outside-root, traversal, and symlink reads are rejected. *(F-005)*
- [x] **LOOP-06**: The advertised outside-workspace `send_file` approval flow is wired to a resume hook (authorizing one path/session/expiry) or the tool returns a deterministic unsupported error — no infinite ask loop. *(F-009)*
- [x] **LOOP-07**: `fs_write` uses the atomic write helper (temp + rename), preserving mode/permissions; a mid-write crash never leaves a truncated target. *(F-010)*
- [x] **LOOP-08**: A mutating tool that panics after a side effect preserves its mutating classification through panic recovery, so the completion gate / `sideEffected` is armed. *(F-031)*
- [x] **LOOP-09**: Crash-orphaned sidecars inside live conversation directories are reconciled against committed DB rows (age-grace), without removing referenced sidecars. *(F-040)*
- [x] **LOOP-10**: Conversation search reaches spilled (sidecar) content — via a searchable preview/index — or the exclusion is explicitly documented and asserted. *(F-048)*
- [x] **LOOP-11**: Repeated `Stop` calls on a hung worker do not accumulate blocked waiter goroutines (single lifecycle-owned done channel). *(F-045)*

### ToolGateway, Policy & Ledger (GATE)

- [x] **GATE-01**: Every tool call (host shell/fs, MCP-bridged, sandbox, swarm) passes through one in-process policy decision (`allow`/`deny`/`approve`) recorded durably; no tool executes without a recorded policy decision. Table-driven over `internal/scoring` risk tiers; fail-OPEN under `dev`/`local_trusted`, fail-CLOSED for mutating tools under hardened/production. *(F-001 gateway, F-020)*
- [x] **GATE-02**: Configured command hooks default to fail-CLOSED (or require an explicit `AURA_COMMAND_HOOK_FAIL_POLICY`); timeout/crash/non-zero hook behavior matches the configured policy and cannot silently allow a denied command. *(F-006)*
- [x] **GATE-03**: Mutating tools require a successful durable pre-execution ledger reservation (started → succeeded/failed) under hardened/production; a failed reservation blocks the mutating tool; read-only tools degrade per policy. *(F-011, F-020)*
- [x] **GATE-04**: Mutating tools carry an idempotency key (ConversationID + RequestID + ToolCallID); retries do not double-apply side effects, and the durable state machine supports recovery. *(F-020)*

### Multi-User Identity Isolation & Auth (MUSR)

- [x] **MUSR-01**: Conversation and approval stores expose owner-scoped methods; AG-UI/API list/get/search/mutate surfaces filter by the authenticated principal — identity B can never list, get, delete, archive, or resolve identity A's data (404/403). Proven by a two-identity live E2E. *(F-028)* — 36-04 (Postgres kernel + owner-scoped surfaces + branch routes owner-scoped at 36-12), 36-05 (documents), 36-06 (Garage), 36-10 (audit UI); the two-identity cross-deny live E2E lands at 36-12 (`cmd/aura/two_identity_e2e_test.go`, five tags, flag-on) and gates the `musr-e2e` CI job. **Live run DONE (36-18):** CI run 28799334452 (2026-07-06, HEAD `207200c8`) — the five-tag `musr-e2e` two-identity cross-deny E2E ran live **268s under -race** (no-skip-as-green); documents-plane isolation activated (backfill 0-doc no-op → `AURA_MUSR_ISOLATION=true` live).
- [x] **MUSR-02**: New Web conversations are created under `identityctx.IdentityID(ctx)` (with `local` only as the CLI/no-principal fallback); a B-created conversation is owned by B and runs successfully. *(F-028)* — 36-04 (`defaultConversationOwner` keys on the principal); the B-owns-and-runs leg is proven in the 36-12 two-identity E2E.
- [x] **MUSR-03**: Background shell jobs use random unguessable IDs bound to session/actor; poll and kill require a matching session/actor (or an explicit admin capability). Session B cannot poll or kill session A's job. *(F-032)*
- [x] **MUSR-04**: Background shell jobs have a default TTL + owner/session/task IDs + age metrics; TTL expiry records status and terminates the process group. *(F-012)*
- [x] **MUSR-05**: All conversation deletion (AG-UI, Telegram `/clear`, CLI) routes through a runner lifecycle method that cancels active work, expires pending pauses, evicts session tools, and handles background jobs before deleting persistence. *(F-039)*
- [x] **MUSR-06**: Authula becomes the default auth provider (cutover from passphrase) with provisioning + break-glass shipped first; capability-per-route enforced; long-lived tokens are never accepted in URLs/query strings (headers/secure cookies only; query tokens reserved for short-lived setup bootstrap). *(F-050)* — 36-01 (break-glass first), 36-08 (provisioning saga + D-15), 36-10 (capability-per-route), 36-11 (no-token-in-URL static gate); the provision→login→isolated-run + break-glass happy path is proven in the 36-12 `TestProvisionLoginIsolatedRun`. **Boot wiring DONE (36-14):** the serve-time provisioning adapters (ObjectStore/Filesystem/Journal in `cmd/aura/serve_provisioning.go`, `KindIdentityPurge` dispatch entry, migration 0033 scheduler-kind widen, deactivation auth-gate) are wired at the composition root. **Live run DONE (36-18):** CI run 28799334452 (2026-07-06) `musr-e2e` proves `TestProvisionLoginIsolatedRun` green live 268s.

### Per-User Full-Capability Sandbox (SBX)

- [x] **SBX-01**: Under hardened/production profiles, host shell/filesystem tools execute inside a per-identity full-capability sandbox container (full shell/fs/network *inside*, named volumes, per-user `/workspace`) routed by `SandboxRouter.Resolve(identityctx)`; the agent experiences a full host, the real host is never exposed. *(F-001)* — **Mechanism verified + LIVE (WSL, `-race`, 2026-07-07)**: `TestRoute_StrictExecInBox`/`TestSnippetExec_RoutedEndToEnd` prove in-box execution; real npm docx+xlsx skills generated in an aura-sandbox box. Marked `[x]` at Phase-37 operator force-close 2026-07-08 (see 37-VERIFICATION.md override). **Native-dockerd confirmation 2026-07-08** on casaserver: the full `internal/sandbox/usersandbox` docker_integration suite ran `ok 79.29s` (in-box exec + lifecycle + cross-identity deny). Only native `-race` remains unrun (box has no gcc; `-race` was green on WSL 2026-07-07).
- [x] **SBX-02**: The sandbox cannot re-expose the host — Docker-socket mount, `--privileged`, `--network host`, and host bind-mounts are unrepresentable in the sandbox config; a test asserts none can be set. *(F-001 #1 escape vector)*
- [x] **SBX-03**: Per-identity sandboxes have a lifecycle (create / idle-TTL stop / resume / scheduled-delete) with stable identity + persistent per-user storage; cross-identity volume/data leakage is impossible (isolation test). *(F-001)* — **LIVE-verified 2026-07-07 (WSL, `-race`)**: `TestLifecycle_SuspendResumeDelete`, `TestVolume_CrossIdentityDeny`, `TestReap_IdleSuspendAutoResume` all PASS on real Docker containers.
- [x] **SBX-04**: Sandbox egress defaults to **full public internet minus the tenancy boundary** — allow all public egress, DROP only the internal carve-out (RFC1918 private ranges + `169.254.169.254` cloud-metadata + the shared-services Docker bridge). **(AMENDED 2026-07-06 per D-04/D-05/D-06: the original `--network none` default is superseded by this full-internet-minus-internal posture for Claude-Code parity — do not nanny the agent's outbound work; the tenancy boundary is the only carve-out.)** SBX-04's enforced core is **preserved**: an allowlist, *when an operator tightens one*, is *enforced* (nftables), not advisory — a configured allowlist cannot reach a disallowed host (integration test); `runtime: runsc` (gVisor) stays selectable as a `server_production` policy. **gVisor⊥nat note:** the always-on filter-table floor (the internal-block) is gVisor-compatible, but the OpenSandbox FQDN-allowlist (nat-redirect) mode is **runc-only and mutually exclusive with gVisor `runsc`** (issue #934). *(F-036)* — **Composition-root BLOCKER CLOSED by 37-10** (`buildSandboxRouter`→`usersandbox.WithEgress(cfg.Sandbox.EgressImage)`, non-empty default, fail-CLOSED — independently re-verified). Marked `[x]` at Phase-37 operator force-close 2026-07-08 (see 37-VERIFICATION.md override). **LIVE DROP PROOF OBTAINED 2026-07-08** on casaserver (192.168.1.21, native-Linux non-masquerading dockerd): `TestEgress_FloorDropsInternal` (backend floor) AND `TestBuildSandboxRouter_LaunchesEgressFloor` (composition-root via buildSandboxRouter) both PASS — box reaches example.com but is DROPPED from 10.0.0.1 + 169.254.169.254. Only the FQDN-allowlist variant (needs the OpenSandbox-baked image) remains unrun; the always-on floor's tenancy-boundary enforcement is behaviorally verified.
- [x] **SBX-05**: An ADR records the decision: container-per-identity over Docker for the mini-PC; K8s + `kubernetes-sigs/agent-sandbox` + gVisor-as-default reserved for the DGX-Spark multi-node tier; the sandbox config mirrors the agent-sandbox template/claim shape for forward compatibility. *(F-025 partial)* — ADR `docs/adr/0037-per-identity-docker-sandbox.md` shipped + truthed-up on the SBX-04 wiring. Marked `[x]` at Phase-37 operator force-close 2026-07-08; the D-14 32GB concurrency benchmark verdict remains an appliance-only Gate-3/REL-03 must-run (dev WSL capped at 15.47 GiB; 2026-07-07 mechanism run at 9 GB / N=8 was informational only).

### Web Artifact Delivery Lane (WEBART)

Agent-generated files (e.g. a DOCX the model builds inside the sandbox) must reach the web cockpit as an authenticated same-origin download, never a raw container/host path. Today `send_file` emits a host-path `aura.artifact` event that Telegram consumes (`internal/channels/telegram/artifact.go`) but the web chat drops (`web/src/chat/sseAdapter.ts` ignores `aura.artifact`). **Depends on Phase 37 / plan 37-07** (box→host `CopyArtifactsOut` staging): the resolved host path is the Garage-ingest input, so this lane sequences after 37-07's `send_file.go` change. *(product gap surfaced in-session — no audit F-finding)*

- [x] **WEBART-01**: When `send_file` delivers a generated file, Aura stores the bytes in the authenticated identity's Garage object store (via `objectstore.Store`, per-identity `AssetKey`) and creates a thread-scoped `assets.Asset` row owned by that identity (mirroring `assets.Service.IngestTelegramFile`); no raw container/host path is used as the delivery handle.
- [x] **WEBART-02**: The `aura.artifact` event carries `asset_id`, `filename`, `size_bytes`, and `mime_type` (not a filesystem path) on the existing `/agent/run` AG-UI SSE stream; Telegram delivery keeps working (regression test).
- [x] **WEBART-03**: `GET /api/assets/{id}/download` streams the asset from Garage with `Content-Disposition: attachment`, enforcing identity ownership via `assets.Store.GetForIdentity` and inheriting `RequireAuth` (registered in `registerAssetRoutes`); a non-owner request returns 404/403 and no unauthenticated download surface is added.
- [x] **WEBART-04**: The web chat consumes `aura.artifact` in `sseAdapter.ts` and renders an authenticated download button targeting `/api/assets/{id}/download`; the browser never receives a raw container/host path. Graceful degradation: CLI / no-authenticated-identity retains today's host-path behavior and a nil asset service does not break delivery.
- [x] **WEBART-05**: The web cockpit renders a right-side "Artefatti" panel in the `AppShell` chat shell (`ResizablePanelGroup`) that lists every asset delivered via `aura.artifact` in the active thread (filename, `size_bytes`, `mime_type`, type icon), newest-first, with a graceful empty-state; on mobile/tablet it collapses into a drawer/overlay like the navigation without breaking the layout. *(Phase 37B — cockpit-web parity vs Telegram/Claude UI, surfaced in the voice/artifact/skill audit)*
- [x] **WEBART-06**: Each artifact row exposes a download action targeting `GET /api/assets/{id}/download` (identity-scoped, `Content-Disposition: attachment`, per WEBART-03), and a "Scarica tutto" control downloads the thread's assets sequentially (client-side; no server-side zip endpoint unless evidenced); no raw container/host path ever reaches the browser. *(Phase 37B)*
- [x] **WEBART-07**: The panel is a single derived view merging the thread asset list (`GET /api/assets?thread_id=`) with live `aura.artifact` events from `sseAdapter.ts` (no new source of truth); ownership is enforced by `assets.Store.GetForIdentity` — a non-owner request returns 404/403 and no unauthenticated surface is added. *(Phase 37B)*
- [x] **WEBART-08**: Non-regression: the inline `local_artifact` display chip keeps rendering and CLI / no-authenticated-identity degrades to today's host-path behavior. Coverage: React unit tests (panel render + download-all) + a Playwright e2e (artifact appears in the panel + download) + web coverage ≥85%. *(Phase 37B — live-proven 2026-07-09: web/e2e/artifacts.spec.ts 4/4 green on chromium + mobile-chrome against a rebuilt container with real Authula auth; coverage 91.6% ≥85%)*

### Web Voice Lane (WEBVOICE)

Cockpit-web voice parity with Telegram, cloud-only (OpenRouter STT/TTS; no local sidecar — RAM constraint). Reuses the shipped `internal/multimodal` clients. *(product gap — voice/artifact/skill audit)*

- [x] **WEBVOICE-01**: Each assistant message exposes a speaker control that synthesizes its text via a new authenticated `POST /api/tts` (identity-scoped, streaming opus/mp3) over `multimodal.TTSClient`, played by an in-page `<audio>`; a per-conversation "voice mode" preference enables auto-speak (parity with Telegram `ShouldSpeak`). *(Phase 37C)*
- [x] **WEBVOICE-02**: The Composer Mic becomes dictation — record → transcribe via the existing STT pipeline → insert the transcript into the input box (editable before send), instead of attaching a voice note; on failure it falls back to today's attachment behavior. *(Phase 37C)*
- [x] **WEBVOICE-03**: Selectable local↔cloud — TTS/STT default to the local aura-tts (Kokoro) / aura-stt (faster-whisper) sidecars and switch to OpenRouter when `AURA_TTS_MODEL`/`AURA_STT_CLOUD_MODEL` is set (supersedes the original cloud-only D-12 per operator directive); when neither a local base URL nor a cloud model is configured the UI degrades gracefully (speaker hidden / mic in attachment mode) with no errors. *(Phase 37C)*
- [x] **WEBVOICE-04**: No regression of the audio-attachment path; `RequireAuth` on the TTS endpoint; React unit tests (speaker + dictation) + e2e; web + owned-surface Go coverage ≥85%. *(Phase 37C)*

### Composer Skill & Command Picker (WEBSKILL)

A slash "/" menu in the Composer (parity with Claude's skill/command picker) to invoke/attach a skill inline, instead of only the admin Governance board. *(product gap — voice/artifact/skill audit)*

- [x] **WEBSKILL-01**: Typing "/" at the start of a Composer line opens a keyboard-filterable menu listing the skills available to the authenticated identity (via the governance skills API, identity-scoped) with ↑/↓/Enter/Esc and a per-row description. *(Phase 37D)*
- [x] **WEBSKILL-02**: Selecting an entry injects the skill into the turn per the existing runtime contract; no new source of truth for skills (reuses the governance API). *(Phase 37D)*
- [x] **WEBSKILL-03**: Accessible (ARIA combobox/listbox), preserves Composer paste/drop/Enter-to-send, degrades to a no-op when the skills API is empty/unreachable; unit + e2e; coverage ≥85%. *(Phase 37D)*

### Composer Reasoning-Effort Selector (WEBMODEL)

Per-turn reasoning-effort ("thinking") selection in the Composer (parity with Claude's `off · low · mid · high · extra · max` effort control plus Aura's adaptive `auto`). **Effort-only — the model dropdown is dropped**: model selection stays operator-scoped in the Settings page (`AURA_LLM_MODEL`, D-01). This is the resolved 37E scope after the mandatory Wave-1 PRD-amendment (37E-01, D-11) — a scope reduction of the chartered "model + effort" WEBMODEL surface. *(product gap — voice/artifact/skill audit)*

- [x] **WEBMODEL-01**: The Composer exposes a reasoning-effort selector whose levels are auto-detected per active model from the set `auto·off·low·mid·high·extra·max` (never a hard-coded or placebo list — D-12/D-13); the choice is persisted per-conversation (`aura.conversations.metadata` jsonb, no migration) and restored on reopen. *(Phase 37E)*
- [x] **WEBMODEL-02**: `/agent/run` accepts an optional symbolic `effort` override; the server maps the symbol → `llm.ReasoningConfig` and validates it in two stages — (1) syntactic enum, then (2) capability (the level must be in the active model's advertised `supported_efforts`); a non-enum OR non-advertised value → 400; absent/`auto` → today's adaptive default (no regression). Effort takes effect on BOTH OpenRouter AND a local llama.cpp backend (D-08). *(Phase 37E)*
- [x] **WEBMODEL-03**: No bypass of governance: the client sends a symbol, never a raw `ReasoningConfig`/budget/model; the server owns the symbol→config map and the capability gate. Every UI level maps to a REAL spike-validated wire knob (D-12) — no placebo/fabricated field (in particular `reasoning.max_tokens` is NOT resurrected as a hard cap). Unit + e2e; coverage ≥85%. Honest-fidelity caveat (D-09): advertised `supported_efforts` is best-effort — graduated fidelity is backend-dependent (real on budget-capable local llama.cpp; the default DeepSeek-V4-Flash collapses low..max to on/off), so the UI must not sell graduated effort as uniform. *(Phase 37E)*

### Conversation & Artifact Sharing / Export (WEBSHARE)

Share/export a conversation or artifact (parity with Claude's "Condividi"/link), respecting Aura's identity isolation — never an unauthenticated public surface by default. *(product gap — voice/artifact/skill audit)*

- [x] **WEBSHARE-01**: The owner can export a conversation (Markdown/JSON of the thread) downloaded via an identity-scoped endpoint (`GetForIdentity`, `Content-Disposition: attachment`). *(Phase 37F)*
- [x] **WEBSHARE-02**: Sharing is either (a) revocable + capability-gated toward Aura identities, or (b) — if public is chosen — an explicitly opt-in expiring opaque token with a warning, never default; the owner can revoke. *(Phase 37F)*
- [x] **WEBSHARE-03**: No host/container path and no other identity's data reach a recipient; the share act is audited. *(Phase 37F)*
- [x] **WEBSHARE-04**: Unit + e2e + a cross-identity deny test on the shared link; coverage ≥85%. *(Phase 37F)*

### MCP Governance Hardening (MCPH)

- [x] **MCPH-01**: A single canonical transport classifier governs validation, trust normalization, runtime eligibility, mounting, and opening; mixed `url`+`command` entries are rejected unless an explicit type disambiguates and the trust class matches. *(F-027)*
- [x] **MCPH-02**: Empty/blank trust on a remote (Streamable HTTP/URL) MCP entry means BLOCKED, not runnable; explicit trust is required for every runnable remote transport. *(F-013)*
- [x] **MCPH-03**: The governance trust endpoint requires an explicit known class + non-empty reason; empty body, `{}`, blank reason, and unknown class return 400 with no config/audit change. *(F-038)*
- [x] **MCPH-04**: Each MCP mount runs under a bounded per-server timeout and reaps the process on timeout; a hung helper is dropped and registry construction returns within the deadline. *(F-033)*
- [x] **MCPH-05**: Stdio MCP frames are capped at a maximum size; an oversized frame aborts the transport deterministically without large allocation. *(F-034)*
- [x] **MCPH-06**: MCP shutdown bounds HTTP close with a timeout and terminates the stdio process tree (process group/job object) — no hang, no leaked child processes. *(F-035)*
- [x] **MCPH-07**: CLI MCP mutations (add/trust/enable/disable/remove, profiles) route through the audited atomic writer (`mcp_audit`), or are explicitly marked unaudited and disallowed under production. *(F-037)*
- [x] **MCPH-08**: Legacy `AURA_MCP_SERVERS_JSON` is production-disabled (or translated into managed config with explicit trust + audit metadata) unless an explicit compatibility flag is set. *(F-014)*
- [x] **MCPH-09**: HTTP MCP probe/doctor dials and lists tools (under the short probe deadline) — a dead/typoed endpoint reports `OK=false`, not healthy-by-config. *(F-046)*

### Observability, Idempotency & Retention (OBS)

- [ ] **OBS-01**: AG-UI listener startup/runtime failure is fatal to the serving process OR reflected in `/readyz`; the Docker/Compose healthcheck probes `/readyz`; a port conflict fails startup or readiness. *(F-008, F-017)*
- [ ] **OBS-02**: `/readyz` reflects database, listener, migration state, and scheduler state; it fails when any critical serving dependency fails. *(F-008, F-017)*
- [ ] **OBS-03**: OpenTelemetry spans wrap LLM, tool, MCP, pause/resume, DB, and scheduler work; the OTel **metric** path is wired (today only traces are) following the target-architecture identifiers + GenAI semantic conventions. *(F-023)*
- [ ] **OBS-04**: Prometheus alert rules + Grafana dashboards ship in-repo (loop error rate, tool timeout rate, queue lag, LLM latency, MCP timeout rate, resume failures, listener state) and are syntax/JSON-validated in CI. *(F-023)*
- [ ] **OBS-05**: Sidecar/trace retention is a first-class operation — retention config, cleanup command, disk-usage metrics, per-conversation export/delete, with active-conversation exclusion + dry-run. *(F-024)*
- [ ] **OBS-06**: Learning stores (`activelearn` `seen`, `reasoningstore`, `toolselectstore`) have a retention cap (max per label/tool, TTL/compaction, bounded load) + metrics. *(F-049)*

### Security & Supply-Chain (SEC)

- [ ] **SEC-01**: Tool output and traces redact secret-like values before persistence; full reasoning-trace mode requires an explicit production warning/fail-fast + retention config + optional encrypted sink. *(F-021)*
- [ ] **SEC-02**: Permissive/wildcard CORS is replaced by explicit origin allowlists, refused when auth is disabled except under an explicit dev profile, sets `Vary: Origin`, and keeps allowed methods in sync with registered routes. *(F-022)*
- [ ] **SEC-03**: The integration validation console refuses non-loopback bind unless an explicit unsafe flag + authentication are configured (logs a warning). *(F-047)*
- [ ] **SEC-04**: A prompt-injection / tool-policy-bypass regression suite asserts that injected shell/file/network/MCP requests are DENIED under `server_production`. *(F-019 security part)*
- [ ] **SEC-05**: CI publishes an SBOM (syft / cyclonedx-gomod), `govulncheck` is a blocking gate, and all third-party Actions + tool versions are SHA/exact pinned; a workflow-lint gate rejects `@latest`/semver-floating refs. *(F-051)*
- [ ] **SEC-06**: Privileged JSON routes (`/agent/run`, approvals resolve, onboarding, assets, governance writes) use strict decoding — size cap, content-type check, `DisallowUnknownFields`, single-decode EOF, per-route `allowEmpty`. *(F-052)*
- [x] **SEC-07**: Go build/test/vulnerability CI jobs reuse `scripts/go_packages.sh` (no raw `./...`); a CI lint rejects raw `go test ./...` / `govulncheck ./...`. *(F-015)*
- [x] **SEC-08** (pulled forward to Phase 31): The critical CodeQL `go/request-forgery` (SSRF) finding at `internal/mcp/http_client.go` is remediated — outbound MCP HTTP request targets are validated against an allow-list / SSRF guard rather than driven by unvalidated input — and the CodeQL alert resolves to fixed. *(CodeQL-surfaced; not in the F-001..F-052 audit set)*
- [ ] **SEC-09**: The high CodeQL `go/weak-sensitive-data-hashing` finding at `internal/agui/recovery_hash.go` is remediated — sensitive recovery material uses a cryptographically strong, salted KDF/hash rather than a weak/fast hash — and the CodeQL alert resolves to fixed. *(CodeQL-surfaced; not in the F-001..F-052 audit set)*

### Production Operations, Scale & Capability Evaluation (OPS)

- [ ] **OPS-01**: Backup/restore for Postgres + Neo4j + sidecars + object store is automated and **drilled** with measured, documented RPO/RTO; the drill accounts for Neo4j 5.26 Community offline-only backup (`neo4j-admin database dump/load`). *(F-019 ops part)*
- [ ] **OPS-02**: Scheduler shutdown separates stop-admission from in-flight job-work contexts with an explicit drain deadline; SIGTERM during a long job does not immediately cancel it. *(F-042)*
- [ ] **OPS-03**: The systemd stop timeout exceeds the longest configured handler duration (or backups are atomically promoted); a kill during backup never promotes a partial artifact. *(F-043)*
- [ ] **OPS-04**: A load (k6/vegeta) + chaos (toxiproxy: DB outage, MCP timeout storm, object-store outage, process-kill-during-write) harness runs in CI under no-skip-as-green discipline and defines supported concurrency + degradation behavior. *(F-019 load/chaos part)*
- [ ] **OPS-05**: A capability-evaluation suite (golden + adversarial + chaos scenarios for shell/files/MCP/memory/pause-resume/error/workflow classes) publishes a CI pass/fail report; live-LLM tiers are gated per the no-unsolicited-paid-runs rule. *(F-019)*
- [ ] **OPS-06**: ADRs exist for loop semantics, tool policy, memory provenance, MCP trust, deployment profiles, and the sandbox decision; a release-readiness checklist gates security/load/backup/observability/rollback. *(F-025, F-026)*

### Honest 10/10 Release Evidence (REL — cross-cutting)

- [ ] **REL-01**: Every new tier (unit/integration/smoke/load/chaos/injection) actually runs in CI — no skip-as-green; skip helpers `t.Fatal` under `$CI`.
- [ ] **REL-02**: Owned-surface coverage stays ≥85%; mutation testing ≥70% killed on the gateway, identity-isolation, profile-validation, and sandbox files.
- [ ] **REL-03**: The production-readiness score reaches a defensible 10/10 with a written evidence bundle (two-identity live E2E, prompt-injection-denied-under-production, drilled DR with RPO/RTO, honest `/readyz`, effective-behavior profile validation) — score bounded by weakest evidence, not green-check count.

### Industrial Conversation Compaction (IC)

- [x] **IC-01**: Provider capability registry and exact fail-closed token budget model.
- [x] **IC-02**: Semantic-unit selection, atomic recent tail, disjoint manifests, and typed L1 edits.
- [x] **IC-03**: Pure proactive L2.4 decision seam before any allowed L2.5 fallback.
- [x] **IC-04**: Durable stable-ID claims, leases, out-of-transaction inference, and serializable CAS finalize.
- [x] **IC-05**: Branch-aware immutable checkpoint generations, versioned digests, active pointer, and deterministic reconstruction.
- [x] **IC-06**: Safe structured summarization, unresolved-authority ledger, and non-authoritative internal rendering.
- [x] **IC-07**: Unified manual/lifecycle/proactive/overflow coordinator with bounded non-destructive failures.
- [ ] **IC-08**: Typed content parts, authorized immutable links, provider projection, artifact durability and reachability GC.
- [x] **IC-09**: Four-generation recursion cap, deterministic drift gates, and hierarchical canonical rebase.
- [x] **IC-10**: Separate durable-memory candidate, promotion, retrieval, consent, retention, regional and deletion lifecycle.
- [x] **IC-11**: Active/LKG/bounded-rebuild recovery, compatible preview/restore, quarantine and disaster recovery.
- [x] **IC-12**: Owner-gated CLI, REPL, Telegram, AG-UI and accessible web operations parity.
- [x] **IC-13**: Redacted observability, 500+200 evaluation corpus, numerical promotion gates, staged canaries and automatic rollback.
- [x] **IC-14**: Backwards-compatible additive migrations, activation-disabled slices, full CI/security/privacy/rollback evidence, and legacy retirement.

### Code Quality Cleanup (QUAL)

Derived from the maintainability/architecture audit `docs/audit/quality/` (4-slice, ~64 findings: 0 Critical, ~8 High, ~26 Medium, ~26 Low). Most items ride **refactor-on-touch** inside the phases above; the security-overlapping dups are routed to their phase (F-027→36, F-052→38, F-015→38, F-016→31). These QUAL requirements capture the work that does NOT naturally fall inside another phase.

- [x] **QUAL-01** (Wave 0 — URGENT, unblocks CI): split the two >600-LOC files (`cmd/aura/serve_webui.go` 628, `web/src/__tests__/LoginPage.test.tsx` 643) and rebuild+commit `internal/webui/dist` (the `file-size` pre-commit hook and `web-dist-freshness` CI job are currently red).
- [x] **QUAL-02** (Wave 1): delete dead exports / reinvented-stdlib / placeholders — `assets.Status{Created,Embedding,Canceled}`, sidecar-only `settings AURA_MEMORY_EMBED_*` keys, `agui.indexByte`/`stringList`, redundant `channels/deps.go` telebot blank import, redundant `RequestID` re-stamp — each confirmed via `deadcode`/`knip`/repo-wide `rg` before removal.
- [x] **QUAL-03** (Wave 2): extract shared helpers to kill cross-package duplication — `internal/neostore` (store helpers + `GraphClient`), `internal/envutil` (3× env helpers + agent-tool knobs), `internal/agentrender` (`chat_render`↔`eval` ~80 LOC), agent `CanonicalArgs` + `isTransientNetworkErr` primitives, web single `getJSON` + shared `focusTrap` reuse. Parity test per extraction.
- [x] **QUAL-04** (Wave 3): correctness fixes — `askuser/store.go:231` int32 overflow guard (CodeQL candidate), `bootChatEnvWithConfig` single-`Validate` + deferred pool-close (verify no overlay-path pool leak), catalogue hot-path `AURA_*` knobs in config.
- [x] **QUAL-05** (Wave 4): close targeted test gaps — `web/throttle.go`, setup `InvalidateToken`-before-SSE ordering, Telegram `answersFromText` keyword fallback, `truncateTailBytes`, Authula `ensureAuthulaSearchPath` DSN parsing.

---

## Future Requirements (deferred — post-v2.0.0)

### Multi-Tenant / RBAC

- **RBAC-01**: Real role/permission model (admin vs user) — explicitly OUT of v2.0.0 (identity isolation only).
- **RBAC-02**: OAuth multi-provider / multi-tenant SaaS login.
- **RBAC-03**: ~~Per-identity Postgres row-level security~~ — **AMENDED 2026-07-05 (Phase 36):** pulled forward into Phase 36 as *identity isolation*, NOT as part of the RBAC role model. Owner-`id` RLS + app-level `*ForIdentity` (defense-in-depth), kernel/storage-enforced per CONTEXT D-07/D-08 and the spike "storage-enforced, not app-enforced" non-negotiable (a forgotten `WHERE identity_id` must not leak). The RBAC *role model* itself (RBAC-01/02) remains deferred post-v2.0.0. See MUSR-01 and `.planning/phases/36-multi-user-identity-isolation-authula-cutover/36-CONTEXT.md` §D-07.
  - **36-02 note (OQ1/A4):** 36-RESEARCH OQ1/A4 pins the reclassification scope — RLS *for identity isolation* is IN (D-07, defense-in-depth), while RLS *for roles* stays OUT (no role model is introduced). This preserves the intent of RBAC-03's original exclusion; it does NOT reopen the "no RBAC" decision. The additive schema foundation (owner column, saga journal, soft-delete, per-identity object-store key, audit identity indexes, the `AURA_MUSR_ISOLATION` rollout flag) ships in plan 36-02; the RLS `ENABLE` + owner-scoping policy is deferred to plan 36-04, co-located with the `WithIdentityTx` read-path that makes it safe (enabling RLS ahead of that path fail-closes pooled non-tx reads — RESEARCH Pitfall 1).

### DGX-Spark Fleet

- **DGX-01**: K8s (k3s/full) + `kubernetes-sigs/agent-sandbox` CRD adoption (`Sandbox`/`SandboxTemplate`/`SandboxClaim`/`SandboxWarmPool`) for the multi-node appliance fleet.
- **DGX-02**: Neo4j Enterprise (online/differential backup) on the appliance for tighter RPO/RTO.
- **DGX-03**: gVisor/Kata as default isolation runtime; warm pools.
- **DGX-04**: Slice 13 (vLLM + LMCache local-LLM fallback) — GPU-gated.

## Out of Scope

| Feature | Reason |
|---------|--------|
| RBAC / roles / OAuth multi-tenant | Locked decision (c): identity isolation only — minimal industrial form |
| OPA / Rego / Cedar policy engine | ToolGateway is one in-process function; external policy fabric is over-engineering |
| Full Kubernetes on the mini-PC | k3s/k0s break the single-binary/Compose invariant + cost ~1 GB idle; deferred to DGX fleet |
| gVisor/Kata/microVM as default | +10–125% on IO-heavy shell/build work; opt-in `server_production` runtime only |
| Sandbox warm pools | Trade idle compute for latency — counter-productive on one 16-core box |
| Stripping the full-host terminal | Locked decision (a): capability is contained (per-user sandbox), never removed |
| New product features / channels | This is hardening/industrialization, not feature growth |
| Native Windows runtime | Unchanged from v1.0.0 — container/Compose only |

## Traceability

Suggested phase mapping (roadmapper finalizes; phases continue at 31+). Every requirement maps to exactly one phase; all 51 findings (F-001..F-052, F-044 absent) are covered, plus 2 CodeQL-surfaced findings tracked outside the F-series (SEC-08 → Phase 31, SEC-09 → Phase 40).

| Category | REQs | Findings closed | Suggested phase |
|----------|------|-----------------|-----------------|
| QUAL | QUAL-01 (Wave 0), SEC-08 | quality audit `docs/audit/quality/` + F-015 (CI `./...`) + CodeQL SSRF | Phase 31 |
| QUAL | QUAL-02/03/05 | quality audit dead-code + shared-helper extraction + test gaps | Phase 32 |
| PROF | PROF-01..06 (+QUAL-04 env catalog) | F-002, F-007, F-016, F-018, F-026, F-041 | Phase 33 |
| LOOP | LOOP-01..11 (+QUAL-04 pool-leak/int32) | F-003, F-004, F-005, F-009, F-010, F-029, F-030, F-031, F-040, F-045, F-048 | Phase 34 |
| GATE | GATE-01..04 | F-001(gw), F-006, F-011, F-020 | Phase 35 |
| MUSR | MUSR-01..06 (+QUAL Authula DSN) | F-012, F-028, F-032, F-039, F-050 | Phase 36 |
| SBX | SBX-01..05 | F-001(sbx), F-036 | Phase 37 |
| WEBART | WEBART-01..04 | (product gap — agent-generated artifact web-delivery, surfaced in-session; no F-finding) | Phase 37A |
| WEBART | WEBART-05..08 | (product gap — cockpit-web "Artefatti" sidebar parity vs Telegram/Claude UI; voice/artifact/skill audit) | Phase 37B |
| WEBVOICE | WEBVOICE-01..04 | (product gap — cockpit-web voice parity vs Telegram/Claude; voice/artifact/skill audit) | Phase 37C |
| WEBSKILL | WEBSKILL-01..03 | (product gap — composer skill/command "/" picker vs Claude; audit) | Phase 37D |
| WEBMODEL | WEBMODEL-01..03 | (product gap — composer reasoning-effort selector vs Claude; audit; model-selector dropped, effort-only per 37E-01/D-01) | Phase 37E |
| WEBSHARE | WEBSHARE-01..04 | (product gap — conversation/artifact sharing + export vs Claude; audit) | Phase 37F |
| MCPH | MCPH-01..09 (+QUAL-03 trust-norm) | F-013, F-014, F-027, F-033, F-034, F-035, F-037, F-038, F-046 | Phase 38 |
| OBS | OBS-01..06 | F-008, F-017, F-023, F-024, F-049 (+F-020 idempotency) | Phase 39 |
| SEC | SEC-01..06, SEC-09 (+QUAL decode-body) | F-019(sec), F-021, F-022, F-047, F-051, F-052, CodeQL weak-hash | Phase 40 |
| SEC | SEC-07 (F-015), SEC-08 | CI `./...` hygiene + CodeQL SSRF (pulled forward) | Phase 31 |
| OPS | OPS-01..06 | F-019(ops), F-025, F-042, F-043 | Phase 41 |
| REL | REL-01..03 | (cross-cutting evidence bar) | Phase 41 / all |
| IC | IC-01..14 | Industrial provider-portable conversation context lifecycle | Phase 42 |

**Coverage:**

- v2.0.0 requirements: 81 total (PROF 6, LOOP 11, GATE 4, MUSR 6, SBX 5, WEBART 8, WEBVOICE 4, WEBSKILL 3, WEBMODEL 3, WEBSHARE 4, MCPH 9, OBS 6, SEC 9, OPS 6, REL 3, QUAL 5) — the WEB* groups are product-gap requirements (no audit F-finding; findings-mapped count unchanged), the cockpit-web parity cluster from the voice/artifact/skill audit: WEBART 37A/37B artifacts, WEBVOICE 37C voice, WEBSKILL 37D skill-picker, WEBMODEL 37E reasoning-effort selector (effort-only per 37E-01/D-01), WEBSHARE 37F sharing/export
- Security/production audit findings mapped: 51 / 51 (F-001..F-052, F-044 intentionally absent) ✓
- CodeQL-surfaced findings (outside the F-series): 2 / 2 — SEC-08 SSRF (`internal/mcp/http_client.go`) → Phase 31, SEC-09 weak-hash (`internal/agui/recovery_hash.go`) → Phase 40 ✓
- Quality/maintainability audit: `docs/audit/quality/` (4-slice, ~64 findings) → QUAL-01..05 + routed to security phases ✓
- Unmapped findings: 0 ✓

---
*Requirements defined: 2026-06-29*
*Last updated: 2026-06-29 after initial definition (post-research)*
