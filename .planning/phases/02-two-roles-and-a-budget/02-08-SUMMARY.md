---
phase: 02-two-roles-and-a-budget
plan: 08
subsystem: ui
tags: [react, rbac, credit, i18n, onboarding, assistant-ui, tanstack-query]

# Dependency graph
requires:
  - phase: 02-two-roles-and-a-budget
    provides: "Plan 02-01's capability narrowing (D-01: every identity holds governance.write, the '*' wildcard retired at migration 0121), plan 02-05's credit_exhausted refusal sentinel (cmd/aura/llm_client.go), plan 02-07's GET/POST /api/admin/identities/{id}/credit and DELETE /api/admin/identities/{id} routes and their DTO shapes"
provides:
  - "web/src/admin/adminApi.ts — IDENTITY_CREATE/IDENTITY_DELETE, fetchIdentityCredit/setIdentityCredit/removeIdentity, and hasCapability with its wildcard branch DELETED"
  - "web/src/admin/useAdmin.ts — isAdmin re-derived from identity.create; useIdentityCredit/useSetIdentityCredit/useRemoveIdentity; useGrantCapability/useRevokeCapability removed"
  - "web/src/settings/IdentityRoster.tsx — the read-only roster, the typed-email removal dialog, and the per-row credit disclosure (replaces CapabilityAdminPanel.tsx)"
  - "web/src/settings/CreditPanel.tsx — cap + reset interval + the ledger-sourced spend gauge, with the CRED-09 exemption composition"
  - "web/src/onboarding — a three-phase wizard (credentials -> review -> complete); CapabilityPicker.tsx deleted"
  - "web/src/chat/ExternalStoreChat_messages.tsx — the MessagePrimitive.Error slot now carries copy, and distinguishes credit_exhausted from a generic turn failure"
  - "internal/agui/audit_api.go — meDTO.Name, so the chat lane has a name to put in the refusal"
affects: [02-09, 02-10]

# Actuals (#2632)
actuals:
  tokens: 40116   # chars/4 over `git diff 61c30e4a6..HEAD -- web/src internal/agui/audit_api*.go` (160463 chars)
  tasks: 3
  commits: 3      # ce48df5e8, 77e9bd9d9, 2e040d4fb — measured via git log --grep '^feat(2-8)'
  plan_head_before: 61c30e4a6

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "A form field that mirrors a server-owned number is `undefined` until touched and falls back to the server's value for its DISPLAY (`capEdit ?? credit.cap.toFixed(2)`), rather than being seeded from a useEffect. The effect version is a setState cascade on every refetch (react-compiler set-state-in-effect, which oxlint enforces here) AND quietly makes the field the source of truth for a figure the server owns."
    - "A stepper/strip that walks the model's own exported list (`PHASES`) instead of a parallel hand-kept copy: the two had already fallen out of step, and a strip rendering a phase the wizard cannot reach is worse than no strip."
    - "Money formatting is KIND-dependent, not one function: a cap is cap-space (always two decimals) and a spend is ledger-space (grows decimals until its leading significant digit is visible). Collapsing the second into the first is the defect migration 0124 and 61c30e4a6 were paid for."
    - "A disclosure whose child owns a query disabled on an empty id costs no request while closed — a roster of twenty identities fans out zero credit GETs until one is opened."

key-files:
  created:
    - web/src/settings/IdentityRoster.tsx
    - web/src/settings/__tests__/IdentityRoster.test.tsx
    - web/src/settings/CreditPanel.tsx
    - web/src/settings/__tests__/CreditPanel.test.tsx
    - web/src/chat/__tests__/chatTestHarness.tsx
    - web/src/chat/__tests__/ExternalStoreChat_errors.test.tsx
  deleted:
    - web/src/settings/CapabilityAdminPanel.tsx
    - web/src/settings/__tests__/CapabilityAdminPanel.test.tsx
    - web/src/onboarding/CapabilityPicker.tsx
    - web/src/onboarding/__tests__/CapabilityPicker.test.tsx
  modified:
    - internal/agui/audit_api.go
    - internal/agui/audit_api_test.go
    - web/src/admin/adminApi.ts
    - web/src/admin/useAdmin.ts
    - web/src/settings/IdentityAccessPanel.tsx
    - web/src/onboarding/OnboardingWizard.tsx
    - web/src/onboarding/OnboardingStepper.tsx
    - web/src/onboarding/onboardingWizardModel.ts
    - web/src/onboarding/onboardingApi.ts
    - web/src/onboarding/ReviewStep.tsx
    - web/src/chat/ExternalStoreChat_messages.tsx
    - web/src/chat/__tests__/ExternalStoreChat.test.tsx
    - web/src/i18n/resources.admin.ts
    - web/src/i18n/resources.onboarding.ts

key-decisions:
  - "Typed-confirmation match semantics (UI-SPEC backstop, held out upstream): TRIMMED, case-SENSITIVE. Trimming forgives the one mistake copy-paste actually makes; exact case stops a careless near-match on an irreversible cross-plane teardown. Both neighbouring cases are asserted."
  - "The gauge for a fresh identity with a cap and no ledger rows renders 0%, NOT the em-dash placeholder the plan floated as the alternative. Measured, not chosen: credit_api.go's creditGetResponse reports `spend: 0` for both 'no rows yet' and 'a genuine zero', so the wire gives the client no way to tell them apart. Inventing an em-dash would be a distinction with no evidence behind it. If the difference ever matters, the fix is a server-side field, not a client-side guess."
  - "The panel renders the server's APPLIED cap, never the typed one: POST returns the half-up-rounded value (5.126 -> 5.13), and both the input and the latency advisory read that. The advisory's direction is computed against the applied figure too, since 5.126 and 5.13 are the same change."
  - "A sub-cent spend keeps enough decimals to stay non-zero (formatUsd grows precision below $0.01). Rendering two decimals would have re-introduced, in the browser, exactly the rounding-to-zero defect migration 0124 widened the column for and 61c30e4a6 fixed in the encoder."
  - "The credit gauge imports CONTEXT_NEAR_FULL_PERCENT/CONTEXT_CRITICAL_PERCENT and gaugeTier from chat/footerMetrics by name rather than declaring 70/90 locally, so the credit gauge and the context gauge cannot drift into two different answers for the same idea."
  - "meDTO gained `Name` (internal/agui/audit_api.go) because the UI-SPEC's binding refusal copy interpolates {{name}} and the SPA had no name to bind: /api/me returned only an id + capability list, authentication is external so the login email never reaches the browser, and the profile display name is a different, optionally-blank field fetched imperatively inside Settings. OPERATOR DECISION at the checkpoint, chosen over rewording the copy. The lookup is best-effort: a miss leaves the name empty and must not fail the capability read that gates every admin surface, and the client falls back to the generic copy rather than interpolating a blank."
  - "The zero-credit refusal carries NO figure. The only numbers available at refusal time are a duplicate of the ledger (which drifts) and the provider's counter (M-07: 30-40s stale, wrong at exactly the moment a human reads it). LibreChat's Error.tsx puts the balance inline; the UI-SPEC considered and rejected that by name."
  - "internal/agui's OnboardingProvisionRequest KEEPS its Capabilities field even though the cockpit stopped sending it. That field is what validateProvisionCapabilities reads to refuse a non-SPA caller naming an administrative capability (ErrOnboardingEscalation); deleting it would delete a tested refusal. The SPA-side types dropped both `capabilities` and `capabilityOptions` because those were the picker's own plumbing."
  - "ADMIN_MODES and the cockpit visibility chain were deliberately NOT changed. 02-UI-SPEC.md:44 already settled the non-admin Settings experience as reused-as-is, and AppShell.tsx:156's `?settings=` deep link means SettingsWorkspace's non-admin branch is reachable without the nav. Two earlier attempts to 'fix' this were reasoning from code against a decision the phase documents had already made, and both were reverted."

patterns-established:
  - "Pattern: derive a form field's displayed value from the query, keep only the EDIT in state. It kills the effect-seeding cascade and makes 'render what the server applied' the default rather than an extra step."
  - "Pattern: before writing a SUMMARY, grep for a non-test caller of every exported symbol the plan created. This phase had shipped five symbols complete, tested and unreachable; this plan's own check (CreditPanel -> IdentityRoster -> IdentityAccessPanel) was run and is recorded in Self-Check."
  - "Pattern: when a binding design contract assumes data the code cannot supply, that is a checkpoint, not a judgment call. The UI-SPEC's {{name}} was measured unavailable and escalated rather than silently reworded."

requirements-completed: [RBAC-11, CRED-03, CRED-06, CRED-09]

coverage:
  - id: D1
    description: "isAdmin is derived from identity.create, not governance.write, so a member holding the four user capabilities does not see the admin surface; the client no longer expands the '*' wildcard; loading and error both fail closed."
    requirement: "RBAC-11"
    verification:
      - kind: unit
        ref: "web/src/admin/__tests__/useAdmin.test.tsx"
        status: pass
      - kind: unit
        ref: "web/src/admin/__tests__/adminApi.test.ts"
        status: pass
      - kind: unit
        ref: "web/src/settings/__tests__/SettingsWorkspace.test.tsx"
        status: pass
    human_judgment: false
  - id: D2
    description: "The roster renders role=list Card rows with read-only Admin/Member badges, the '(you)' suffix, break-all font-mono overflow, an unconditionally-disabled self-removal with its always-visible aura identity caption, and the in-flight 'Removing {{name}}...' state."
    requirement: "RBAC-11"
    verification:
      - kind: unit
        ref: "web/src/settings/__tests__/IdentityRoster.test.tsx"
        status: pass
    human_judgment: false
  - id: D3
    description: "The removal confirm button stays disabled until the typed text matches the identity's exact email; a trailing space still matches (trimmed) and a case difference does not (case-sensitive); a part-way saga failure renders its own retry-safe copy."
    requirement: "RBAC-11"
    verification:
      - kind: unit
        ref: "web/src/settings/__tests__/IdentityRoster.test.tsx"
        status: pass
    human_judgment: false
  - id: D4
    description: "The credit panel renders cap (inputMode=decimal, min=0, step=0.01, mono), the three-option reset select defaulting to monthly, and the role=progressbar gauge with aria-valuenow and the '{{spend}} / {{cap}} · {{percent}}%' readout, all from the ledger response."
    requirement: "CRED-06"
    verification:
      - kind: unit
        ref: "web/src/settings/__tests__/CreditPanel.test.tsx"
        status: pass
    human_judgment: false
  - id: D5
    description: "The gauge's fill tier is bg-accent below 70, bg-warning at 70-89 and bg-danger at 90+, using footerMetrics' own constants; spend equal to cap reads 100 + danger and one cent below reads 99."
    requirement: "CRED-06"
    verification:
      - kind: unit
        ref: "web/src/settings/__tests__/CreditPanel.test.tsx"
        status: pass
    human_judgment: false
  - id: D6
    description: "A sub-cent spend of 0.000004158 renders as $0.00000416, not $0.00; a fresh identity with a cap and no ledger rows renders 0% and no em-dash."
    requirement: "CRED-06"
    verification:
      - kind: unit
        ref: "web/src/settings/__tests__/CreditPanel.test.tsx"
        status: pass
    human_judgment: false
  - id: D7
    description: "Saving 5.126 renders the server's applied 5.13; the latency advisory picks 25s for an increase and 5s for a decrease; a save failure renders the destructive alert copy."
    requirement: "CRED-03"
    verification:
      - kind: unit
        ref: "web/src/settings/__tests__/CreditPanel.test.tsx"
        status: pass
    human_judgment: false
  - id: D8
    description: "A non-billing backend renders the Empty composition with 'No spending cap to show' and no zero balance, no gauge and no Save control anywhere in the panel."
    requirement: "CRED-09"
    verification:
      - kind: unit
        ref: "web/src/settings/__tests__/CreditPanel.test.tsx"
        status: pass
      - kind: other
        ref: "plan verify: grep -rn '\\$0\\.00' web/src/settings/CreditPanel.tsx prints nothing"
        status: pass
    human_judgment: false
  - id: D9
    description: "The wizard's phase list is exactly three entries and contains no 'capabilities'; CapabilityPicker.tsx and its test do not exist; the stepper renders Credentials/Review/Telegram and cannot render a phase the wizard can't reach; the review step states the uniform grant and the zero starting credit as prose with no badge list."
    requirement: "RBAC-11"
    verification:
      - kind: unit
        ref: "web/src/onboarding/__tests__/onboardingWizardModel.test.ts"
        status: pass
      - kind: unit
        ref: "web/src/onboarding/__tests__/OnboardingStepper.test.tsx"
        status: pass
      - kind: unit
        ref: "web/src/onboarding/__tests__/OnboardingWizard.test.tsx"
        status: pass
      - kind: unit
        ref: "web/src/onboarding/__tests__/ReviewStep.test.tsx"
        status: pass
      - kind: other
        ref: "plan verify: test ! -f CapabilityPicker.tsx && test ! -f its test; grep -c capabilities onboardingWizardModel.ts = 0"
        status: pass
    human_judgment: false
  - id: D10
    description: "The chat error slot renders the credit_exhausted refusal naming the identity with role=alert + text-danger and no digit or $ anywhere in it, and the generic turn-failure copy otherwise."
    requirement: "CRED-09"
    verification:
      - kind: unit
        ref: "web/src/chat/__tests__/ExternalStoreChat_errors.test.tsx"
        status: pass
      - kind: unit
        ref: "internal/agui/audit_api_test.go (meDTO.Name best-effort lookup)"
        status: pass
    human_judgment: false
  - id: D11
    description: "Every exported symbol this plan created has a non-test production caller — the defect this phase shipped five times."
    verification:
      - kind: other
        ref: "grep chain: CreditPanel -> settings/IdentityRoster.tsx -> settings/IdentityAccessPanel.tsx -> SettingsWorkspace; useIdentityCredit/useSetIdentityCredit -> CreditPanel.tsx; fetchIdentityCredit/setIdentityCredit -> useAdmin.ts; GetIdentityByID -> handleMe"
        status: pass
    human_judgment: false
  - id: D12
    description: "No dependency was added and no file exceeds the 600-LOC cap."
    verification:
      - kind: other
        ref: "git diff --exit-code -- web/package.json web/package-lock.json (clean); bash scripts/check-file-size.sh (3043 tracked files, all within cap)"
        status: pass
    human_judgment: false

duration: two sessions — Tasks 1 and 2 in the prior session (Task 2 finished inline after two gsd-executor stalls), Task 3 inline in this one
completed: 2026-09-10
status: complete
---

# Phase 2 Plan 8: The Cockpit Half

**The admin can now see who exists, remove one after typing their email, set a cap and watch the spend against it — and an identity out of credit is told who to ask instead of being shown an empty red line.**

## Performance

- **Tasks:** 3 (Task 1: admin derivation + data layer; Task 2: roster + removal; Task 3: credit panel + wizard + refusal copy)
- **Files created:** 6 · **deleted:** 4 · **modified:** 14
- **Commits:** 3 — `ce48df5e8`, `77e9bd9d9`, `2e040d4fb`

## Accomplishments

- **Closed the regression this phase introduced into its own cockpit.** `useAdmin.ts` derived `isAdmin` from `governance.write`, and under D-01 every identity holds it — the moment 02-01 landed, every user would have seen the admin surface. The server still refuses them, so it was never a breach; it was a cockpit offering controls it would then reject, which is worse than either alternative. `isAdmin` now comes from `identity.create`, and the client's `'*'` expansion is deleted along with it.
- **The grant/revoke controls are gone, not hidden.** `CapabilityAdminPanel.tsx` and `CapabilityPicker.tsx` are deleted files, with their hooks and their i18n keys. RBAC-03 grants everything at provisioning and RBAC-06 refuses the administrative pair through the API, so neither control had anything left to do.
- **The credit panel refuses to round anything away.** A sub-cent spend keeps its leading significant digit; a cap typed at excess precision settles to the value the store and the provider actually hold; and a non-billing deployment gets the CRED-09 exemption rather than a fabricated zero balance.
- **The chat error slot went from an empty red paragraph to copy that distinguishes two different failures** — a turn that broke upstream, and a turn refused for want of credit.

## Deviations

**1. `internal/agui/audit_api.go` gained `meDTO.Name` — a backend change outside this plan's file list.**
The UI-SPEC's binding refusal copy interpolates `{{name}}`, and measurement showed the SPA had no name to bind: `/api/me` carried an id and a capability list, authentication is external so the login email never reaches the browser, and the profile display name is a different, optionally-blank field fetched imperatively inside Settings. Rather than silently reword binding copy — the exact re-derivation this phase's `.continue-here.md` lists as a blocking anti-pattern — this was put to the operator as a checkpoint. **Operator chose to extend `/api/me`.** The lookup is best-effort by design: a failed row read leaves the name empty rather than failing the capability read that gates every admin surface, and the client falls back to the generic copy instead of interpolating a blank.

**2. `IdentityRoster.tsx` and `OnboardingStepper.tsx` were edited although Task 3's file list names neither.**
`CreditPanel` needed a production caller — this phase has shipped five symbols that were complete, tested and unreachable, and shipping a sixth was not an option. It mounts as a per-row disclosure in the roster; the query is disabled while closed, so a roster of twenty identities fans out zero credit GETs. `OnboardingStepper` kept its own hand-written copy of the phase list, which would have rendered a Capabilities step the wizard could no longer reach; it now walks `PHASES` itself.

**3. `ExternalStoreChat.test.tsx` was split.**
Adding the refusal cases took it to 603 LOC. The error-slot cases moved to `ExternalStoreChat_errors.test.tsx` over a new shared `chatTestHarness.tsx`, rather than a second copy of the SSE body builder that could drift from the first.

**4. `onboardingApi.ts` dropped `capabilities` and `capabilityOptions`; the Go wire kept its field.**
Both SPA fields were the deleted picker's own plumbing and had no remaining reader. `OnboardingProvisionRequest.Capabilities` stays server-side because `validateProvisionCapabilities` reads it to refuse a non-SPA caller naming an administrative capability — deleting the field would delete that tested refusal. Flagged for 02-10 if the phase wants it retired properly.

**Total deviations:** 4. **Impact:** one was an operator checkpoint on binding copy; the other three were required for the plan's own output to be reachable, consistent and within the LOC cap. None expanded scope.

## Issues Encountered

- **Two `gsd-executor` dispatches stalled at the 600s watchdog on this plan in the prior session**, both around the full frontend suite (`npm test` = vitest --coverage, 246 files, ~144s, very large output). Task 2 was finished inline without incident and Task 3 was executed inline for the same reason. The recorded mitigation — targeted `npx vitest run <paths>` inside a subagent, full suite only from the main session — held.
- **The operator's concurrent session committed this plan's staged index into its own `chore(sqlc)` commit.** Caught immediately (`git log` showed 26 files in a commit that should have had one), and repaired by splitting: `46ca18742` is the sqlc chore with its original message and its one file, `2e040d4fb` is this plan's work. Nothing was lost. This is the same two-writers hazard the phase's `.continue-here.md` records for subagents, in a different form.
- **`go test -race` refuses to run on the Windows host** (`-race requires cgo`); run via WSL with `CGO_ENABLED=1`, per CLAUDE.md's toolchain table.
- **A `golangci-lint` "parallel golangci-lint is running" collision** failed the first pre-commit attempt; the retry was clean.

## User Setup Required

None — no new environment variable, no new dependency (`git diff --exit-code -- web/package.json web/package-lock.json` is clean, and the plan's own verify command asserts it).

## Next Phase Readiness

- **02-09 (the account-wide reconciliation surface) can mount above the roster** inside the same `identities` admin-gated section, as §Admin spend dashboard specifies. `IdentityAccessPanel.tsx` is the composition point.
- **The per-identity Credit panel is the binding CRED-03/CRED-06 surface and is unchanged by 02-09** — the Overview is reconciliation data from a different source (`GET /api/v1/keys`, lifetime), and the UI-SPEC's Copywriting Contract already disambiguates the two spend figures by wording ("Lifetime spend" vs "Spend"), not by a footnote.
- **`useCapabilities()` now returns `identityName`** — 02-09 can use it without a second fetch.
- **The accent reservation is closed at exactly two items for this phase**: the Save-cap button and the credit gauge's normal tier. The UI-SPEC rules the Overview's sparkline to `--color-info` for precisely this reason; 02-09 must not take a third.
- **Still open for 02-10:** `internal/agui`'s `POST/DELETE /api/admin/identities/{id}/capabilities` routes and their handlers remain mounted with no client, and `meDTO`'s neighbouring comment still describes the retired wildcard behaviour. Both are dark surface this plan did not have the file list to touch.

## Self-Check: PASSED

- All 6 created files confirmed on disk; all 4 deleted files confirmed absent (the plan's own `test ! -f` verify).
- **Production-caller grep run for every exported symbol** (the check this phase's handoff demands): `CreditPanel` → `settings/IdentityRoster.tsx`; `useIdentityCredit`/`useSetIdentityCredit` → `settings/CreditPanel.tsx`; `fetchIdentityCredit`/`setIdentityCredit` → `admin/useAdmin.ts`; `GetIdentityByID` → `handleMe`. No orphans.
- `npm run typecheck` — clean. `npm run lint` (oxlint --type-aware --max-warnings=0) — clean.
- `npm run test` — **248 test files, 2077 tests, 0 failures.** Coverage: statements 91.47%, branches 85.30%, functions 90.89%, lines 93.46%.
- `go vet ./...` and `go build ./...` — clean.
- `CGO_ENABLED=1 go test -race ./internal/agui/ ./internal/identity/` (WSL) — both ok.
- `bash scripts/check-file-size.sh` — 3043 tracked source files, all within the 600-LOC cap.
- Plan verify commands: `grep -rn '\$0\.00' CreditPanel.tsx` → nothing; `grep -c capabilities onboardingWizardModel.ts` → 0; `git diff --exit-code -- web/package.json web/package-lock.json` → clean.

---
*Phase: 02-two-roles-and-a-budget*
*Completed: 2026-09-10*
