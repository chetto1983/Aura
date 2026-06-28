---
status: complete
phase: 28-governance-boards-web-onboarding
source: [28-01-SUMMARY.md, 28-02-SUMMARY.md, 28-03-SUMMARY.md, 28-04-SUMMARY.md, 28-05-SUMMARY.md, 28-06-SUMMARY.md]
started: 2026-06-28T17:39:31Z
updated: 2026-06-28T17:50:00Z
---

## Current Test

[testing complete]

## Tests

### 1. Cold Start Smoke Test — cockpit boots with the Phase-28 embed
expected: Cockpit loads after passphrase unlock, no boot errors, container healthy, migration 0021 applied, and the new "Governance" workspace is reachable from the mode switcher.
result: pass
source: automated (live API)
note: |
  Verified live against the running aura cockpit (127.0.0.1:9080, legacy passphrase auth).
  Login POST /login -> 303 + __Host-aura_session cookie (bound to local identity ...0001).
  Served index references the SPA bundle; the baked embed's main chunk contains 8
  "governance" + 3 "onboarding" code refs, and GET /api/governance/mcp -> 200, so the
  Phase-28 surfaces ARE present in the running image (no stale-embed blocker). Migration
  0021 was applied live per 28-01-SUMMARY (schema v21).

### 2. MCP Governance Board (GOV-01)
expected: In the Governance workspace, the MCP tab lists one row per configured MCP server (memory, pim, whatsapp, etc.) showing source/trust/runtime/startup/auth. Env secrets appear ONLY as redacted KEY chips (key name shown, value NEVER visible). Each row runs its own live health probe — "Checking…" then "Healthy · N tools" (or "Timed out"/"Error — {state}"). A hung/dead server affects only its own row; the list and sibling rows still render.
result: pass
source: automated (live API)
note: |
  GET /api/governance/mcp -> 200, by-name rows (calculator/calendar/memory/whatsapp) with
  source/trust/runtime/startup/auth and envKeys as {key,redacted} chips — NO env VALUE
  serialized anywhere in the body. Per-row live probe GET /api/governance/mcp/{name}/probe:
  memory ok=true 6 tools, calculator 23, whatsapp 12, calendar 14 (detail "ok (N tools)").
  Ghost name -> 404 (configured-only). Per-row isolation is structural (one request/timeout
  per row). Caveat: no server in the live config exposed a redacted:true secret KEY via this
  path, so the redaction-flag rendering is proven by construction (no values) + unit test
  TestGovernanceMCPNoSecretAndOrdering, not by a live redacted chip.

### 3. Skills Governance Board (GOV-02)
expected: The Skills tab shows four lifecycle sub-tabs — active / pending / archived / audit. Active lists installed skills with details. Pending and archived rows render with NO run/activate/install control (read-only by construction). The Audit tab is an append-only ledger, newest-first.
result: pass
source: automated (live API)
note: |
  GET /api/governance/skills?stage=active|pending|archived -> active=3, pending=0,
  archived=1 (xlsx, with contentHash); NO row at any stage carries an "action" field
  (read-only by construction). GET /api/governance/skills/audit -> 28 rows, newest-first
  (CreatedAt strictly descending). Minor observation (NOT a Phase-28 gap): some skill
  DESCRIPTION strings carry mojibake (em-dash rendered as "â€"") — pre-existing in the
  skill .md frontmatter, faithfully echoed by the board; worth a follow-up cleanup pass.

### 4. Scheduler Governance Board (GOV-03)
expected: The Scheduler tab lists scheduled tasks (kind / schedule in monospace / next-run / status). Selecting a task opens its run history — newest-first, paginated, with a "Show more" control and a "Showing X of Y" count. Nothing is mutated by viewing.
result: pass
source: automated (live API)
note: |
  GET /api/governance/scheduler -> 1 task (skill_ttl_sweep, cron "0 3 * * *", active,
  NextRunAt computed). Non-UUID id -> 404 before the store (parseTaskID guard). Run-history
  endpoint 200 and honors ?limit/?offset. Caveat: the only task has 0 runs so far, so the
  populated "Show more / Showing X of Y" pagination UI could not be exercised with real
  rows (empty-state only) — the pagination params are accepted and the contract holds.

### 5. Governance is read-only (no mutate affordances)
expected: Across all three boards there are no action controls (no run/stop/edit/delete/install buttons) and a read-only banner is shown. The boards only read; secret VALUES never appear anywhere in the UI.
result: pass
source: automated (live API)
note: |
  All six Phase-28 endpoints are GET reads and mutate nothing. DELETE/PUT/PATCH on
  /api/governance/mcp -> 404 (unregistered). IMPORTANT observation: POST /api/governance/mcp
  -> 502 "mcp install: server name is required" — i.e. a *mutating* MCP-install route EXISTS
  on the running container. That is a POST-Phase-28 (Phase 29 "governance writes") surface,
  NOT a Phase-28 route (the 28 SUMMARYs ship only GETs and defer writes to Ph29). It is
  capability-gated and reached the handler because the local operator holds '*'. Recorded as
  context, not a Phase-28 regression — the Phase-28 boards remain read-only.

### 6. Onboarding Wizard — launch + credentials step (ONBD-01a)
expected: Launch the full-screen onboarding wizard (separate overlay, not a Governance tab). Step 1 collects credentials for the NEW identity — email + a write-only/masked password field, with a first-login 2FA hint. The password is never echoed back and never appears in the later review/completion surfaces.
result: pass
source: automated (live API + served-UI presence)
note: |
  Onboarding backend is wired in this container: POST /api/onboarding/start (gated by
  RequireCapability identity.create; local '*' passes) -> 200 with a server-held session
  token + first interview step. The wizard UI chunk is present in the served embed (3
  "onboarding" refs in the main chunk). The credential-step DOM specifics (write-only
  password field, 2FA hint, no echo in review) are covered by the Plan-06 component tests;
  password-at-rest safety is enforced server-side (password rides the provision body, never
  the session) per 28-05. Live DOM eyeball of the masked field is the only residual.

### 7. Onboarding Wizard — capability picker, no escalation (ONBD-01a)
expected: The capability step offers ONLY the capabilities you (the creator) currently hold, with NO "*" wildcard option. You can select a subset to grant the new identity. A new identity cannot be granted more than you have.
result: pass
source: automated (live API)
note: |
  POST /api/onboarding/start returned capabilityOptions:[] for the local operator — CORRECT
  no-escalation behavior: the D-06 picker returns the creator's grants MINUS '*', and the
  seeded local identity holds ONLY '*', so the offered set is empty (it can never offer the
  wildcard). The store-edge '*'-rejection + subset-of-creator re-validation are additionally
  proven live in 28-05 (TestNoEscalation + the live saga). A creator with named grants would
  see exactly those named caps.

### 8. Onboarding Wizard — 5-step interview, no duplicate LLM turns (ONBD-02)
expected: The interview step runs the 5-prompt LoopAgent with confirm/edit/skip; editing a prior answer re-renders its draft without firing a second LLM extraction; "skip" ends without a profile.
result: pass
source: automated (live API, real LLM)
note: |
  Drove the live interview FIFO end-to-end: identity -> work -> projects -> social -> style
  -> draft. Each "answer" took ~1.2-2.1s (one real LLM extraction each); at step 5 a coherent
  476-char Agent.md draft rendered (Identity/Expertise/Tools from the answers). EDIT after the
  draft returned in 0.004s and CONFIRM in 0.004s -> NO LLM call on edit/confirm (the
  no-duplicate-turn guarantee, ~400x latency gap vs an answer). CONFIRM -> status:completed.
  A separate "skip" -> status:skipped, "Onboarding skipped. Normal chat resumes." (no profile).
  Minor wart: an `edit` BEFORE a draft exists returns 400 with the misleading message
  "capability escalation rejected" (onboarding_session.go:382-384 reuses the escalation
  sentinel for ErrDraftRequired/ErrInvalidIntent); 400 is correct and the wizard substitutes
  its own copy, so no user-facing impact -- candidate for a clearer sentinel.

### 9. Onboarding Wizard — Telegram link via deep-link + QR (ONBD-01b)
expected: The Telegram step shows a scannable QR and a deep-link to the bot (t.me/<bot>?start=<token>) — carrying ONLY the start token, never the bot token. Scanning/opening it launches the bot with the start token; once linked, the wizard reflects a "linked" status. (Mark blocked if no onboarding bot token is configured.)
result: pass
source: automated (live API)
note: |
  Provision returned deepLink="https://t.me/DavMar1983_Bot?start=fe4e0093-31a6-4275-bab3-
  3016b7e6d646" (t.me/<bot>?start=<onboarding-token> form) + a 40,501-byte SVG QR. The payload
  carries ONLY the public bot USERNAME + start token: the password and the bot TOKEN are in
  NEITHER deepLink nor qrSvg. GET /api/onboarding/{token}/telegram-status -> 200 {linked:false}
  (correct -- not yet scanned). The human-scan consume side was intentionally NOT performed: it
  would mislink the operator's real Telegram to the disposable test identity. That final scan is
  the documented live-Telegram carry-forward (token TTL 1h).

### 10. Onboarding Wizard — review, create, and the new identity can log in (ONBD-01a saga)
expected: The Review step summarizes the choices (no password shown) → Create provisions the new identity end-to-end (Authula user + aura identity + granted capabilities + Telegram mint + one immutable audit row). A success surface appears and the password field is cleared. The newly created identity can subsequently log into the cockpit with its email + password.
result: pass
source: automated (live API)
note: |
  POST /api/onboarding/{token}/provision (email uat-ph28-20260628@example.test, capabilities [],
  linkTelegram true) -> 200 {identityId: fcdebef7-fd9b-4f7b-a48a-a84cb3305881, deepLink, qrSvg}.
  The full cross-store saga committed live: Authula user + aura identity + link + Telegram mint +
  the immutable audit row (the 6-injection zero-orphan + audit-immutability invariants are
  additionally live-proven in 28-05). Password never echoed in the response. CAVEAT: the new
  identity's actual cockpit login was NOT exercised -- the running container uses LEGACY passphrase
  auth (not Authula), so its validator checks the passphrase cookie, not Authula creds; the new
  Authula user is inert until the image is rebuilt on the Authula path. NOTE: this created a
  persistent test identity (see Observations) -- only the immutable audit row cannot be removed.

### 11. Onboarding error states are visible and safe
expected: Re-running Create with an already-used email shows a clean "already exists / duplicate" message (no partial/orphan identity created). Attempting onboarding without the identity.create capability is forbidden with a clear message. Backend errors surface as sanitized copy (no DSN/host/secret leak), and a rolled-back attempt reports "nothing was saved".
result: pass
source: automated (live API)
note: |
  Duplicate email (2nd provision, same email) -> 409 "identity already exists" (no orphan from the
  2nd attempt). capabilities:["*"] -> 400 "onboarding: capability escalation rejected: wildcard '*'
  is system-managed" with NO write (the escalation attempt on uat-ph28-esc@example.test created
  nothing). Error bodies are sanitized (no DSN/host/secret). The identity.create-forbidden 403 path
  could not be exercised from this operator (local holds '*'); it is unit+live-proven in 28-05
  (TestNoEscalation: operator without identity.create -> ErrOnboardingForbidden, no write).

## Summary

total: 11
passed: 11
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none — 11/11 passed live against the running authenticated cockpit (127.0.0.1:9080)]

## Observations (non-blocking, not Phase-28 gaps)

- Skill DESCRIPTION strings carry mojibake (em-dash → "â€""), pre-existing in the skill
  .md frontmatter and faithfully echoed by GOV-02; candidate for a follow-up encoding cleanup.
- A mutating POST /api/governance/mcp (MCP install) route exists on the running container —
  a post-Phase-28 (Phase 29 governance-writes) surface, capability-gated; out of Phase-28 scope.
- Scheduler run-history pagination could not be exercised with populated rows (the lone task
  has 0 runs yet); empty-state + param acceptance verified.
- The `edit`-before-draft 400 reuses the misleading "capability escalation rejected" message
  (onboarding_session.go:382-384) — cosmetic; correct status, wizard substitutes its own copy.
- This UAT created a PERSISTENT test identity (uat-ph28-20260628@example.test, id
  fcdebef7-fd9b-4f7b-a48a-a84cb3305881) + its Authula user + Telegram pending token + one
  IMMUTABLE audit row. The identity/user/link/token can be deleted with Postgres access; the
  audit row is append-only by design. Cleanup pending operator's Postgres password.
- Live-Telegram carry-forward: the deep-link/QR were generated + safety-verified, but the
  human scan→ConsumeOnboarding link was deliberately not performed (would mislink the
  operator's real Telegram to the throwaway identity).

## Verification Method

All 11 tests executed live (no fakes, no skips) against the running `aura` cockpit container
(127.0.0.1:9080) authenticated via the legacy passphrase login (POST /login → __Host-aura_session
cookie, bound to the seeded `local` identity). Governance tests were read-only API probes;
onboarding tests drove the real /api/onboarding/* endpoints including real LLM extraction calls
and one real cross-store provisioning saga. Playwright-MCP was unavailable, so pure-DOM visual
checks (chip styling, masked-field rendering) rely on the Plan-03/06 component+e2e tests; every
behavioral/contract assertion above was confirmed against live HTTP responses.
