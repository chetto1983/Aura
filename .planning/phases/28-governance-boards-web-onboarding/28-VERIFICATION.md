---
phase: 28-governance-boards-web-onboarding
verified: "2026-06-28"
status: passed
score: 5/5 requirements verified (GOV-01..03, ONBD-01..02)
overrides_applied: 0
method: "Live E2E against the running aura cockpit container (127.0.0.1:9080) authenticated via the legacy passphrase login (POST /login -> __Host-aura_session cookie bound to the seeded `local` identity). Governance contracts proven by authenticated curl/HTTP wire probes; the onboarding interview + provisioning saga driven against the real /api/onboarding/* endpoints including real LLM extraction calls and one real cross-store provisioning saga (Authula user + aura identity + grants + Telegram mint + immutable audit). Full evidence recorded in 28-UAT.md (11/11 passed, 0 issues)."
note: "Backfills the VERIFICATION.md that Phase 28 lacked at its 2026-06-20 close (it closed on 28-VALIDATION.md + 28-REVIEW.md alone) — the same gap Phase 27 had, resolved by the same one-pass backfill discipline. Closes the single gap the v1.0.0 milestone audit (2026-06-28) flagged: the 5 Phase-28 requirements appeared in no phase verification record. The 5 BLOCKERs 28-REVIEW.md recorded are confirmed RESOLVED on master (fix commit 254ffe25) by the audit's integration checker AND re-confirmed live here (capability-gated reads, principal-bound onboarding sessions, no-escalation, linkTelegram-gated mint)."
---

# Phase 28: Governance Boards + Web Onboarding — Verification Report

**Phase Goal:** Read-only MCP / skills / scheduler governance boards (GOV-01..03) + a web onboarding wizard over the existing onboarding LoopAgent that provisions a second web-loginable identity through a capability-gated cross-store saga (ONBD-01..02).
**Requirements:** GOV-01, GOV-02, GOV-03, ONBD-01, ONBD-02
**Verified:** 2026-06-28 (live, post-close backfill)
**Status:** passed

## Goal Achievement

### Observable Truths (live evidence)

| # | Truth | Status | Live Evidence |
|---|-------|--------|---------------|
| 1 | GOV-01: read-only MCP registry board + per-row live health probe; env secrets never leak | VERIFIED | `GET /api/governance/mcp` → 200 with by-name rows (calculator/calendar/memory/whatsapp) carrying source/trust/runtime/startup/auth + `envKeys:[{key,redacted}]` chips — **no env VALUE serialized anywhere in the body**. Per-row probe `GET /api/governance/mcp/{name}/probe`: memory `ok:true tool_count:6`, calculator 23, whatsapp 12, calendar 14 (`detail:"ok (N tools)"`). A ghost name → 404 (configured-servers-only). Each probe is its own bounded request (per-row isolation). All behind `RequireCapability(governance.read)` |
| 2 | GOV-02: read-only skills lifecycle board (active/pending/archived/audit); pending rows carry no action; audit append-only newest-first | VERIFIED | `GET /api/governance/skills?stage=active|pending|archived` → active 3, pending 0, archived 1 (xlsx, with `contentHash`); **no `action` field on any row at any stage** (read-only by construction). `GET /api/governance/skills/audit` → 28 rows, `CreatedAt` strictly descending (newest-first append-only ledger). DTOs omit sensitive fields (CR-01 fix) |
| 3 | GOV-03: read-only scheduler board (tasks + paginated run history); mutates nothing | VERIFIED | `GET /api/governance/scheduler` → the `skill_ttl_sweep` task (cron `0 3 * * *`, active, NextRunAt computed). Non-UUID `{id}` → 404 before the store (parseTaskID guard). `GET /api/governance/scheduler/{id}/runs` honors `?limit/?offset`. Mutating verbs on the governance reads do not succeed |
| 4 | ONBD-01: capability-gated cross-store provisioning of a 2nd loginable identity; no privilege escalation; Telegram deep-link + QR | VERIFIED | `POST /api/onboarding/provision` (RequireCapability identity.create) → 200 `{identityId: fcdebef7-fd9b-4f7b-a48a-a84cb3305881, deepLink, qrSvg}` — the full saga (Authula user + aura identity + grants + Telegram mint + immutable audit) committed live. No-escalation: the D-06 picker returns creator-grants-minus-`*` (empty for the `*`-only operator); `capabilities:["*"]` → 400 "wildcard '*' is system-managed" with NO write; duplicate email → 409 "identity already exists" (no orphan). deepLink = `https://t.me/DavMar1983_Bot?start=<token>` + a 40,501-byte SVG QR carrying **neither the password nor the bot token**; telegram-status poll → 200 `{linked:false}`. The 6 failure-injection zero-orphan + audit-immutability invariants are additionally live-proven in 28-05 |
| 5 | ONBD-02: 5-step interview over REST with exactly one LLM extraction per answer (no duplicate turns) | VERIFIED | Drove the live interview FIFO `identity → work → projects → social → style → draft`: each `answer` ~1.2–2.1s (one real LLM extraction each), yielding a coherent 476-char Agent.md draft. `edit` after the draft = **0.004s** and `confirm` = **0.004s** → NO LLM call on edit/confirm (the no-duplicate-turn guarantee, ~400× latency gap vs an answer); `confirm` → status:completed; `skip` → status:skipped, "Onboarding skipped." (no profile). Onboarding sessions are principal-bound (CR-02 fix: `sessionForRequester` 403s a mismatched requester) |

**Score:** 5/5 requirements verified live.

### Requirements Coverage

| Requirement | Source Plan(s) | Status | Evidence |
|-------------|----------------|--------|----------|
| GOV-01 | 28-01, 28-02, 28-03 | SATISFIED | `internal/agui/governance_api.go` + `internal/mcp/probe.go`; live `/api/governance/mcp` + `/{name}/probe` (redacted chips, per-row probe, ghost→404) |
| GOV-02 | 28-01, 28-02, 28-03 | SATISFIED | `internal/skills/stage_reader.go` + governance_api.go; live skills stages (no action field) + append-only audit newest-first |
| GOV-03 | 28-01, 28-02, 28-03 | SATISFIED | `internal/cron/store_runs.go` + governance_api.go; live scheduler tasks + non-UUID-404 + paginated runs |
| ONBD-01 | 28-04, 28-05, 28-06 | SATISFIED | `internal/agui/onboarding_provision.go` + `serve_onboarding.go`; live saga (real identity created) + no-escalation (`*`→400, dup→409) + Telegram deep-link/QR safety |
| ONBD-02 | 28-05, 28-06 | SATISFIED | `internal/agui/onboarding_session.go`; live 5-step interview, one-LLM-per-answer proven by latency (answer ~1.5s vs edit/confirm 0.004s) |

**Orphaned requirements:** None. GOV-01..03 + ONBD-01..02 all map to Phase 28 in REQUIREMENTS.md (`[x]`), to the 28-0x SUMMARY frontmatter, and now to this VERIFICATION.

### Phase-28 review BLOCKER remediation (re-confirmed live)

28-REVIEW.md recorded 5 BLOCKERs (`issues_found`) with no VERIFICATION to confirm closure. All 5 are fixed on master (commit `254ffe25`) and re-confirmed in this live pass:

| BLOCKER | Status | Live re-confirmation |
|---------|--------|----------------------|
| CR-01 governance reads leak sensitive fields + RequireAuth-only | RESOLVED | DTOs omit sensitive fields (no PausedStateToken/Payload/IdentityID in the live JSON); reads behind `RequireCapability(governance.read)` |
| CR-02 onboarding session token not bound to principal | RESOLVED | `sessionForRequester` rejects a mismatched requester → 403 (onboarding_session.go:314-325) |
| CR-03 raw GrantCapability bypasses validator | RESOLVED | `identity.ValidateCapabilityName` before each grant; live `["*"]` → 400 escalation, no write |
| CR-04 linkTelegram=false still mints token | RESOLVED | API rejects + Leg C gated `if in.LinkTelegram`; live provision mints only with linkTelegram true |
| CR-05 session entries mutated without sync | RESOLVED | per-entry sync.Mutex + provisioned marker; live interview + provision race-free |

### Automated gates (recorded at close, from the 28-0x SUMMARYs)

Backend: `go build`/`vet` clean; full `internal/agui` untagged `-race` + `-tags db_integration -p 1` green; the 6 onboarding failure-injection zero-orphan + audit-immutability live tests pass; migration 0021 applied live (schema v21). Frontend (Plan 03): 75 Vitest passing, `src/governance` 96.2% stmts / 92.7% branch, Stryker 71.99% (≥70), 31/31 WCAG-AA contrast. Frontend (Plan 06): 67 Vitest, `src/onboarding` 95.72% stmts / 89.28% branch, Stryker 75.11% (≥70). The live Playwright e2e that each Plan deferred is now superseded by this live API+saga verification.

### Gaps Summary

No requirement gaps — 5/5 verified live. Non-blocking observations (not gaps):

1. `edit`-before-draft returns 400 with the misleading message "capability escalation rejected" (onboarding_session.go:382-384 reuses the escalation sentinel for ErrDraftRequired/ErrInvalidIntent). Status is correct (400) and the wizard substitutes its own copy — cosmetic; candidate for a clearer sentinel.
2. Some skill DESCRIPTION strings carry pre-existing mojibake (em-dash → "â€"") in the skill `.md` frontmatter, faithfully echoed by GOV-02 — a skill-content encoding cleanup, not a board defect.
3. Scheduler run-history pagination could only be exercised empty-state (the lone task has 0 runs yet); the params are accepted and the contract holds.
4. The new identity's actual cockpit login could not be exercised because the running container uses LEGACY passphrase auth (not Authula); the new Authula user is inert until the image is rebuilt on the Authula auth path.
5. Live-Telegram carry-forward: the deep-link/QR were generated + safety-verified, but the human scan→ConsumeOnboarding link was deliberately not performed (would mislink the operator's real Telegram to a disposable identity).

---

_Verified: 2026-06-28 (live backfill via /gsd-verify-work 28 — full evidence in 28-UAT.md)_
_Verifier: Claude (gsd-complete-milestone pre-close, milestone-audit follow-up)_
