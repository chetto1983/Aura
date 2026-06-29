---
phase: 28
slug: governance-boards-web-onboarding
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-20
---

# Phase 28 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Source: `28-RESEARCH.md` § Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (backend) + vitest (web) + Playwright (e2e) + Stryker (web mutation) |
| **Config file** | `Makefile` (Go gates) · `web/vitest.config.ts` · `web/stryker.conf.json` · `web/playwright.config.ts` |
| **Quick run command** | `go test ./internal/agui/... ./internal/onboarding/... ./internal/identity/... ./internal/webauth/...` + `cd web && npm run test` |
| **Full suite command** | `make quality-full` + `cd web && npm run test:coverage && npm run test:mutation && npm run test:e2e && node scripts/contrast-check.mjs` |
| **Estimated runtime** | Go unit ~30–60s; Go `db_integration` (saga + audit + migration) ~60–120s on the live stack; web vitest ~20–40s; Playwright (desktop+mobile, governance+onboarding) ~2–4min; Stryker (touched dirs) ~5–10min. Wave-1 (Plan 01) confirms exact numbers when the stack is up. |

---

## Sampling Rate

- **After every task commit:** `go test ./internal/<pkg>/ && go vet ./... && go build ./...` (CLAUDE.md Gate 2); for web tasks `cd web && npx vitest run <touched dir>`.
- **After every plan wave:** `make quality` (vet + build + file-size + lint + test-race + vuln) + `cd web && npx vitest run --coverage && npx playwright test && node scripts/contrast-check.mjs`.
- **Before `/gsd-verify-work`:** `make quality-full` (owned-surface ≥85% across the `db_integration neo4j_integration` matrix, stack up) + web ≥85% vitest + Stryker ≥70% + all Playwright green + contrast 0 failures.
- **Max feedback latency:** ~120s (the `db_integration` saga/audit tier is the slowest per-commit signal; unit + vitest stay well under 60s).

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 28-01-01 | 01 | 1 | GOV-03, ONBD-01a | T-28-01-01 | Append-only `aura.identity_audit` (role grant + row/statement triggers); migration 0021 APPLIED live (not a compile pass); `ListRunsForTask` paginates + mutates nothing; `ListCapabilities` does not filter `*` (handler's job) | integration | `go test -tags db_integration ./internal/cron/ ./internal/identity/ -run 'TestListRunsForTask\|TestIdentityAuditImmutable\|TestListCapabilities'` ; `psql "$AURA_DB_URL" -c '\d aura.identity_audit' \| grep -q reject_identity_audit_mutation` | ❌ W1 | ⬜ pending |
| 28-01-02 | 01 | 1 | GOV-01, GOV-02 | T-28-01-02, T-28-01-03, T-28-01-04 | Structured `probeServer` dials ONLY the caller-supplied configured server (no body-URL — prohibition #5); a hung server times out for ITS row without panic/over-deadline; `RedactSecrets` on Detail/Err; per-stage skills reader uses `os.ReadDir` + `parseFrontmatter` and NEVER the loader mount path (pending bodies never enter LLM context); `Set*` seams off `NewServer` | unit + race | `go test ./internal/mcp/ ./internal/skills/ ./internal/agui/ -run 'TestMCPProbe\|TestStageReader\|TestSetGovernance'` ; `go test -race ./internal/mcp/ ./internal/skills/` | ❌ W1 | ⬜ pending |
| 28-02-01 | 02 | 2 | GOV-01, GOV-02, GOV-03 | T-28-02-01, T-28-02-02, T-28-02-03, T-28-02-04, T-28-02-06 | No raw secret value in MCP responses (only redacted KEY chips); probe bounded 3s + per-row isolated + configured-only (404 on unknown {name}); pending skill rows carry no action field (non-runnable by construction); scheduler {id} non-UUID → 404 pre-store; run history paginates; unwired provider → 503; backend error → sanitized 502 | unit (mock probe + stores) | `go test ./internal/agui/ -run 'TestGovernanceMCP\|TestMCPProbeIsolation\|TestMCPProbeConfiguredOnly\|TestGovernanceSkills\|TestGovernanceScheduler\|TestGovernanceEmptyAndError\|TestGovernanceNilProvider503'` | ❌ W1 | ⬜ pending |
| 28-02-02 | 02 | 2 | GOV-01, GOV-02, GOV-03 | T-28-02-05 | Six reads mounted as specific method+path siblings under the `/api/` carve-out, inheriting whole-mux `RequireAuth` (401 unauth, no bare `/api/`); best-effort provider build leaves routes at 503 and NEVER aborts daemon boot; `NewServer` boots without the providers (seam off constructor) | unit (boot + nil-provider) | `go build ./cmd/aura && go test ./internal/agui/ -run 'TestGovernance\|TestGovernanceNilProvider503'` | ❌ W1 | ⬜ pending |
| 28-03-01 | 03 | 3 | GOV-01, GOV-02, GOV-03 | — | `governanceApi.ts` is same-origin + non-200 (incl. 401) THROWS (auth/error routed by TanStack); `'governance'` in MODES+LIVE_MODES; AppShell `surface === 'governance'` lazy swap; every i18n key in BOTH en+it | unit (vitest) + types | `cd web && npx tsc --noEmit && npx vitest run src/governance/__tests__/GovernanceWorkspace.test.tsx` | ❌ W1 | ⬜ pending |
| 28-03-02 | 03 | 3 | GOV-01, GOV-02, GOV-03 | T-28-02-01, T-28-02-04 | MCP redacted secret chips (value never in DOM), per-row live-probe `Checking…→Healthy·N tools/Timed out` in a `role=status` region (dead row never blanks the board); pending rows render NO run/activate control; React-escaped text only (no `dangerouslySetInnerHTML`, HARDEN-08); arrow-nav + tab roles + 44px targets + focus rings; WCAG-AA contrast | unit (vitest) + e2e + contrast | `cd web && npx vitest run src/governance/__tests__/McpBoard.test.tsx src/governance/__tests__/SkillsBoard.test.tsx src/governance/__tests__/SchedulerBoard.test.tsx && npx playwright test e2e/governance.spec.ts && node scripts/contrast-check.mjs` | ❌ W1 | ⬜ pending |
| 28-04-01 | 04 | 1 | ONBD-01 | T-28-04-01, T-28-04-03 | PRD-amendment lands BEFORE provisioning impl (PRD-first); authz STAYS `capability_grants` (no RBAC/route-scoping/OAuth); ROADMAP Phase-30 absorbed via gsd tooling / targeted Edit (never a full-file Write — anti-pattern #15); 30-SPEC tombstoned | doc grep | `grep -q "absorbed-into-28" .planning/ROADMAP.md && grep -q "Phase 28" prd.md && grep -riq "absorbed" .planning/phases/30-telegram-onboarding-link-qr/30-SPEC.md` | ❌ W1 | ⬜ pending |
| 28-04-02 | 04 | 1 | ONBD-01 | T-28-04-02 | `OperatorUserID` >1-user case no longer aborts the live login path; the live session-validate path resolves identity via `ResolveIdentityID` over `identity_auth_links` (UNIQUE on `authula_user_id`, 1:N-ready); no single-operator regression (seeded `local` stays linked) | integration | `go test -tags db_integration ./internal/webauth/ -run 'TestOperatorUserID\|TestAuthulaMultiUser\|TestIdentityLink'` | ❌ W1 | ⬜ pending |
| 28-05-01 | 05 | 3 | ONBD-02 | T-28-05-07 | Exactly one `LLMAnswerExtractor.Extract` per inbound free-text answer; replay emits NO second LLM turn; `edit` re-renders from the SAME Answers via `ExtractDraft` (no re-prompt); `skip`/empty recorded without a profile write; goroutine-free 15-min idle TTL swept lazily on access (goleak + -race clean) | unit (mock LLM, count calls) + race | `go test ./internal/agui/ -run 'TestOnboarding\|TestNoDuplicatePrompt' && go test -race ./internal/agui/ -run TestSessionTTL` | ❌ W1 | ⬜ pending |
| 28-05-02 | 05 | 3 | ONBD-01a, ONBD-01b | T-28-05-01, T-28-05-02, T-28-05-03, T-28-05-04, T-28-05-05, T-28-05-06 | Ordered cross-store saga (Leg B Authula → Leg A aura tx → Leg C Telegram mint → one immutable audit row on success); each of the 6 failure-injection points (B1/B2/A/C/abandoned/double-submit) leaves NO orphan; no-escalation re-validated server-side (subset ⊆ creator AND no `*`; 403 without `identity.create`; store rejects `*`); password write-only (hashed immediately, never echoed/logged; fixed-message on failure); QR holds only the deep-link URL (bot token never in QR/response/logs); replayed/expired consume rejected by `ConsumeOnboarding` | integration (failure-injection) | `go test -tags db_integration ./internal/agui/ -run 'TestProvisionSaga\|TestNoEscalation\|TestIdentityAuditImmutable\|TestProvisionIdempotent\|TestProvisionNoSecretInLogs\|TestTelegramStatus'` | ❌ W1 | ⬜ pending |
| 28-06-01 | 06 | 4 | ONBD-01, ONBD-02 | T-28-05-03 | Wizard is a full-screen overlay (NOT a MODES/tab); `onboardingApi.ts` same-origin + non-200 (incl. 403) THROWS; CapabilityPicker renders NO `*` option + only the server-returned ('*'-excluded) options + the `*`-hint; CredentialStep password is write-only (never re-displayed) + 2FA hint; every i18n key in BOTH en+it | unit (vitest) + types | `cd web && npx tsc --noEmit && npx vitest run src/onboarding/__tests__/OnboardingWizard.test.tsx src/onboarding/__tests__/CapabilityPicker.test.tsx` | ❌ W1 | ⬜ pending |
| 28-06-02 | 06 | 4 | ONBD-01, ONBD-02 | T-28-05-04, T-28-05-05 | Telegram step renders deep-link button + scannable server-rendered QR SVG + a linked-state that flips via the `/telegram-status` REST poll (bot token never in DOM); interview step maps confirm/edit/skip to the correct intents + draft re-renders on edit; review→Create CTA is CONSTRUCTIVE (not danger-styled); distinct error copy (403 no-cap / duplicate-or-empty email / provisioning-failed-rolled-back); full-screen single-column mobile + 44px targets; WCAG-AA contrast | unit (vitest) + e2e + contrast | `cd web && npx vitest run src/onboarding/__tests__/ReviewStep.test.tsx && npx playwright test e2e/onboarding.spec.ts && node scripts/contrast-check.mjs` | ❌ W1 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*
*File Exists ❌ W1 = the test file is created in Wave 1 (the backend-scaffolding-first wave: Plans 01 + 04). Wave numbers are the plans' frontmatter `wave` values (Plan 01/04 = 1, Plan 02 = 2, Plan 03/05 = 3, Plan 06 = 4); the "Wave 0" terminology in RESEARCH/this scaffold refers to that same scaffolding-first concept, realized as the `wave: 1` plans.*

---

## Wave 0 Requirements

> "Wave 0" = the backend-scaffolding-first wave, realized as the `wave: 1` plans (28-01 + 28-04). These seams + test scaffolds MUST land before the feature waves (02/03/05/06) consume them.

- [ ] `internal/db/queries/agent_job_runs.sql` — `ListRunsForTask :many` (GOV-03 pagination) + regenerate sqlc + `internal/cron/store_runs.go` `ListRunsForTask` wrapper (28-01 T1).
- [ ] `internal/identity/store.go` — `ListCapabilities(ctx, identityID) ([]string, error)` wrapper over the existing sqlc query, `*` NOT filtered at the store (D-06 picker; 28-01 T1).
- [ ] `internal/db/migrations/0021_identity_audit.{up,down}.sql` — append-only `aura.identity_audit` (no UPDATE/DELETE grant + row/statement triggers, modeled on 0010_skill_audit) + sqlc query + `internal/identity/audit_store.go` `InsertIdentityAuditTx`/`ErrAuditImmutable`; **migration APPLIED live** (28-01 T1).
- [ ] `internal/mcp/probe.go` — extract `probeServer(ctx, name, server) ProbeResult` (structured, configured-only, RedactSecrets) from `mcpDoctorAll` (GOV-01 live probe + tool count; 28-01 T2).
- [ ] `internal/skills/stage_reader.go` — per-stage `os.ReadDir` reader for `pending/`+`archived/` via `parseFrontmatter`, never the loader mount path (GOV-02 tabs; 28-01 T2).
- [ ] `internal/agui/server.go` — `SetGovernanceProviders(...)` + `SetOnboardingService(...)` seams off `NewServer` (503 until wired; 28-01 T2).
- [ ] `internal/webauth/authula.go` — relax `OperatorUserID` >1-user guard (enrollment-time only; live path resolves via `ResolveIdentityID`) (28-04 T2).
- [ ] PRD-amendment commit (PROJECT.md + prd.md + ROADMAP via gsd tooling + 30-SPEC tombstone) lands BEFORE the provisioning wave (28-04 T1).
- [ ] `internal/agui/governance_api_test.go` + `onboarding_api_test.go` — handler unit-test skeletons (mock the probe + the LLM client + the stores); incl. `TestGovernanceNilProvider503`.
- [ ] `web/src/governance/__tests__/` + `web/src/onboarding/__tests__/` — Vitest skeletons; `web/e2e/governance.spec.ts` + `onboarding.spec.ts`; Stryker covers the new dirs (≥85% / ≥70%).
- [ ] `web/src/i18n/resources.governance.ts` + `resources.onboarding.ts` — en+it bundles; `scripts/contrast-check.mjs` targets registered for every new board + wizard screen.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Scannable Telegram QR links a live channel end-to-end | ONBD-01b | Requires a real Telegram client scanning the rendered QR against the live bot | Operator scans the wizard QR with a phone, sends `/start`, confirms the channel links once and a replay/expired token is rejected |
| New user TOTP enrollment on first login | ONBD-01a (D-05) | Requires a real Authula login session + an authenticator app to enroll TOTP | Operator logs in as the newly-provisioned identity, enrolls TOTP, confirms second-factor challenge succeeds (no mailer/invite flow exists by design) |

*Planner refines; automate every behavior that has a testable seam. The saga, no-escalation, immutable-audit, no-secret-in-logs, and probe-isolation behaviors are all automated above — only the live Telegram scan + the live TOTP enrollment remain human-only.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 1 (scaffolding-first) dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 1 (scaffolding-first) covers all MISSING (❌) references — backend seams + test skeletons land in 28-01/28-04 before the feature waves consume them
- [x] No watch-mode flags (all `vitest run` / `playwright test` one-shot; no `--watch`)
- [x] Feedback latency < ~120s (slowest signal = the `db_integration` saga/audit tier)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** map populated 2026-06-20 from `28-RESEARCH.md` § Validation Architecture; `wave_0_complete` flips to `true` at execution once the Wave-1 seams + test scaffolds land.
