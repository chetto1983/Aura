---
phase: 28-governance-boards-web-onboarding
reviewed: 2026-06-20T14:47:13Z
depth: standard
files_reviewed: 89
files_reviewed_list:
  - cmd/aura/mcp_status.go
  - cmd/aura/serve.go
  - cmd/aura/serve_auth.go
  - cmd/aura/serve_governance.go
  - cmd/aura/serve_onboarding.go
  - cmd/aura/serve_webui.go
  - docs/cockpit-overhaul/05-authula-auth-SPEC.md
  - internal/agui/governance_api.go
  - internal/agui/governance_api_test.go
  - internal/agui/governance_seam.go
  - internal/agui/governance_seam_test.go
  - internal/agui/onboarding_api.go
  - internal/agui/onboarding_api_test.go
  - internal/agui/onboarding_provision.go
  - internal/agui/onboarding_provision_integration_test.go
  - internal/agui/onboarding_provision_test.go
  - internal/agui/onboarding_qr.go
  - internal/agui/onboarding_session.go
  - internal/agui/onboarding_session_test.go
  - internal/agui/server.go
  - internal/cron/store_runs.go
  - internal/cron/store_runs_test.go
  - internal/db/migrations/0021_identity_audit.down.sql
  - internal/db/migrations/0021_identity_audit.up.sql
  - internal/db/queries/agent_job_runs.sql
  - internal/db/queries/identity_audit.sql
  - internal/db/sqlc/agent_job_runs.sql.go
  - internal/db/sqlc/identity_audit.sql.go
  - internal/db/sqlc/models.go
  - internal/db/sqlc/querier.go
  - internal/identity/audit_store.go
  - internal/identity/audit_store_integration.go
  - internal/identity/audit_store_test.go
  - internal/identity/store.go
  - internal/mcp/probe.go
  - internal/mcp/probe_test.go
  - internal/skills/stage_reader.go
  - internal/skills/stage_reader_test.go
  - internal/webauth/authula.go
  - internal/webauth/authula_multiuser_test.go
  - prd.md
  - web/e2e/governance.spec.ts
  - web/e2e/onboarding.spec.ts
  - web/src/AppShell.tsx
  - web/src/governance/BoardLayout.tsx
  - web/src/governance/GovernanceWorkspace.tsx
  - web/src/governance/McpBoard.tsx
  - web/src/governance/McpServerDetail.tsx
  - web/src/governance/SchedulerBoard.tsx
  - web/src/governance/SkillDetail.tsx
  - web/src/governance/SkillsBoard.tsx
  - web/src/governance/TaskRunHistory.tsx
  - web/src/governance/__tests__/BoardLayout.test.tsx
  - web/src/governance/__tests__/GovernanceWorkspace.test.tsx
  - web/src/governance/__tests__/McpBoard.test.tsx
  - web/src/governance/__tests__/McpServerDetail.test.tsx
  - web/src/governance/__tests__/SchedulerBoard.test.tsx
  - web/src/governance/__tests__/SkillDetail.test.tsx
  - web/src/governance/__tests__/SkillsBoard.test.tsx
  - web/src/governance/__tests__/governanceApi.test.ts
  - web/src/governance/__tests__/helpers.test.ts
  - web/src/governance/governanceApi.ts
  - web/src/governance/governanceView.tsx
  - web/src/i18n/resources.governance.ts
  - web/src/i18n/resources.onboarding.ts
  - web/src/i18n/resources.ts
  - web/src/onboarding/CapabilityPicker.tsx
  - web/src/onboarding/CredentialStep.tsx
  - web/src/onboarding/InterviewStep.tsx
  - web/src/onboarding/OnboardingStepper.tsx
  - web/src/onboarding/OnboardingWizard.tsx
  - web/src/onboarding/OnboardingWizardNav.tsx
  - web/src/onboarding/ReviewStep.tsx
  - web/src/onboarding/TelegramLinkStep.tsx
  - web/src/onboarding/__tests__/CapabilityPicker.test.tsx
  - web/src/onboarding/__tests__/CredentialStep.test.tsx
  - web/src/onboarding/__tests__/InterviewStep.test.tsx
  - web/src/onboarding/__tests__/OnboardingStepper.test.tsx
  - web/src/onboarding/__tests__/OnboardingWizard.test.tsx
  - web/src/onboarding/__tests__/ReviewStep.test.tsx
  - web/src/onboarding/__tests__/TelegramLinkStep.test.tsx
  - web/src/onboarding/__tests__/onboardingApi.test.ts
  - web/src/onboarding/__tests__/onboardingWizardModel.test.ts
  - web/src/onboarding/onboardingApi.ts
  - web/src/onboarding/onboardingWizardModel.ts
  - web/src/shell/modes.ts
  - web/src/test/setup.ts
  - web/stryker.config.json
  - web/stryker.onb.json
findings:
  critical: 5
  warning: 1
  info: 0
  total: 6
status: issues_found
---

# Phase 28: Code Review Report

**Reviewed:** 2026-06-20T14:47:13Z
**Depth:** standard
**Files Reviewed:** 89
**Status:** issues_found

## Summary

Reviewed the Phase 28 backend routes, onboarding saga/session code, governance boards, SQL/query changes, frontend data layers/components, tests, and config. Generated web UI build artifacts were skipped as generated output: `internal/webui/dist/assets/OnboardingWizard-BDluN6lM.js`, `internal/webui/dist/index.html`, and `internal/webui/dist/sw.js`.

The main risks are authorization and data-boundary failures: governance endpoints expose raw domain rows containing sensitive fields, onboarding session tokens are not bound to the authenticated principal after start, capability grants bypass the canonical validator, and onboarding session state is mutable without per-session serialization.

## Critical Issues

### CR-01: BLOCKER - Governance APIs Expose Sensitive Cross-Identity Data

**File:** `internal/agui/governance_api.go:274`

**Issue:** `handleSkillsAudit`, `handleSchedulerList`, and `handleSchedulerRuns` serialize store/domain structs directly. Those structs include fields the UI does not need and that are sensitive in a multi-user cockpit: `skills.AuditRow.PausedStateToken`, `cron.Task.Payload`, `cron.Task.IdentityID`, `cron.Run.PausedStateToken`, summaries, and errors. The parent mux mounts every governance route with `RequireAuth` only at `cmd/aura/serve_webui.go:313`, so any logged-in provisioned identity can read global operational data and approval tokens, even without a governance/admin capability.

**Fix:**
Project safe DTOs and gate governance reads with an explicit capability, or scope them by current identity. Do not marshal domain structs directly.

```go
type schedulerTaskRow struct {
    ID         string        `json:"id"`
    Kind       cron.TaskKind `json:"kind"`
    Status     string        `json:"status"`
    NextRunAt  time.Time     `json:"nextRunAt"`
    StepBudget int           `json:"stepBudget"`
}

type schedulerRunRow struct {
    ID                string    `json:"id"`
    TaskID            string    `json:"taskId"`
    Status            string    `json:"status"`
    StartedAt         time.Time `json:"startedAt"`
    CompletedWithHash string    `json:"completedWithHash,omitempty"`
    Summary           string    `json:"summary,omitempty"`
    LastError         string    `json:"lastError,omitempty"`
}
```

Then register governance routes through `RequireCapability(..., "governance.read")` or filter every provider call by the authenticated `principalIdentityID`.

### CR-02: BLOCKER - Onboarding Sessions Are Bearer Tokens Not Bound To The Principal

**File:** `internal/agui/onboarding_api.go:179`

**Issue:** `start` stores `creatorIdentityID`, but later `step`, `provision`, and `telegram-status` handlers pass only the URL token to the service. The service loads the session by token and trusts the stored creator at `internal/agui/onboarding_provision.go:130`. In Authula multi-user mode, a leaked session token lets another authenticated user drive or provision from a session started by a more privileged creator. A user with `identity.create` but fewer grants can also use a high-grant creator's token because no current-principal equality check occurs.

**Fix:**
Pass the authenticated identity into every session operation and reject mismatches before reading or mutating session state.

```go
type OnboardingService interface {
    Step(ctx context.Context, requesterID, token string, in OnboardingStepRequest) (OnboardingStepResponse, error)
    Provision(ctx context.Context, requesterID, token string, in OnboardingProvisionRequest) (OnboardingProvisionResponse, error)
    TelegramStatus(ctx context.Context, requesterID, token string) (OnboardingTelegramStatus, error)
}

func (s *onboardingService) getSessionForCreator(token, requesterID string) (*sessionEntry, error) {
    entry, ok := s.sessions.get(token)
    if !ok {
        return nil, errOnboardingSessionNotFound
    }
    if entry.creatorIdentityID != requesterID {
        return nil, errOnboardingForbidden
    }
    return entry, nil
}
```

Add handler tests using two principals and one token to assert `403`.

### CR-03: BLOCKER - Provisioning Can Persist Invalid Capability Grants

**File:** `cmd/aura/serve_onboarding.go:118`

**Issue:** The onboarding aura leg bypasses `identity.Store.GrantCapability` and calls the generated `q.GrantCapability` directly. The only backstop is `cap == "*"`, while `validateOnboardingProvision` checks only length and `validateNoEscalation` allows any requested name when the creator has wildcard. The `aura.capability_grants` table has no database CHECK, so malformed capabilities like `""`, uppercase names, or strings outside `identity.capNameRe` can be inserted by the local wildcard creator.

**Fix:**
Single-source capability validation and use it before every raw grant path, ideally with a database CHECK for defense in depth.

```go
// internal/identity/store.go
func ValidateCapabilityName(capability string) error {
    if capability == Wildcard {
        return ErrWildcardManaged
    }
    if !capNameRe.MatchString(capability) {
        return fmt.Errorf("%w: %q must match %s", ErrInvalidCapability, capability, capNameRe.String())
    }
    return nil
}

// cmd/aura/serve_onboarding.go
for _, cap := range p.Capabilities {
    if err := identity.ValidateCapabilityName(cap); err != nil {
        return "", agui.ErrOnboardingEscalation
    }
    // q.GrantCapability...
}
```

Add tests for empty, uppercase, whitespace, and wildcard capability names through `/api/onboarding/{token}/provision`.

### CR-04: BLOCKER - `linkTelegram=false` Still Creates Telegram Setup Tokens

**File:** `internal/agui/onboarding_provision.go:183`

**Issue:** `Provision` always mints and inserts an onboarding token before checking `in.LinkTelegram`. The response only includes a deep link when `LinkTelegram` is true, but the database still gets a pending Telegram setup row for explicit opt-out requests. This creates hidden, unused link state and makes `LinkTelegram` a presentation flag rather than the write-control the request type documents.

**Fix:**
Only run Leg C when `in.LinkTelegram` is true. If Telegram is optional, require the `TelegramMint` dependency only on the link path.

```go
var onboardingToken string
if in.LinkTelegram {
    if s.telegram == nil {
        return OnboardingProvisionResponse{}, errProvisioningUnavailable
    }
    onboardingToken = uuid.NewString()
    if err := s.telegram.InsertPending(ctx, onboardingToken, identityID, time.Now().UTC().Add(onboardingTokenTTL)); err != nil {
        // compensate A and B
    }
    s.sessions.setOnboardingToken(token, onboardingToken)
}

if in.LinkTelegram && s.botName != "" {
    deepLink := fmt.Sprintf("https://t.me/%s?start=%s", s.botName, onboardingToken)
    // render QR
}
```

Add live and fake-port tests asserting zero `telegram_setup_pending` rows when `LinkTelegram` is false.

### CR-05: BLOCKER - Onboarding Session Entries Are Mutated Without Per-Session Synchronization

**File:** `internal/agui/onboarding_session.go:123`

**Issue:** `sessionStore.get` returns `*sessionEntry` after releasing the store mutex. `Step` then mutates `entry.session` at `internal/agui/onboarding_session.go:352`, and `Provision` reads/mutates the same entry at `internal/agui/onboarding_provision.go:126` and `:212`. Concurrent HTTP requests for the same token can race under Go's race detector, double-run extraction, apply steps out of order, or run two provision attempts from one wizard session. The existing concurrency test only hammers map put/get; it does not protect the returned mutable session.

**Fix:**
Add a per-entry mutex and a consumed/provisioned marker, then lock around session mutation and provision. Delete or mark the session after a successful provision.

```go
type sessionEntry struct {
    mu sync.Mutex
    session *onboarding.Session
    creatorIdentityID string
    provisioned bool
    // ...
}

func (s *onboardingService) Step(ctx context.Context, token string, in OnboardingStepRequest) (OnboardingStepResponse, error) {
    entry, ok := s.sessions.get(token)
    if !ok {
        return OnboardingStepResponse{}, errOnboardingSessionNotFound
    }
    entry.mu.Lock()
    defer entry.mu.Unlock()
    if entry.provisioned {
        return OnboardingStepResponse{}, errOnboardingSessionNotFound
    }
    // mutate entry.session
}
```

Add a `go test -race` case that submits concurrent `Step` and duplicate `Provision` calls for the same token.

## Warnings

### WR-01: WARNING - Missing `identity.create` On Start Is Rendered As Backend Unavailable

**File:** `web/src/onboarding/OnboardingWizard.tsx:89`

**Issue:** The wizard treats `/api/onboarding/start` failures as either `error-auth` for `401` or generic `error` for everything else. A legitimate `403` from the `identity.create` gate therefore renders the backend-unavailable retry UI at lines 212-222 instead of the no-permission copy that the provisioning error model already supports. This hides authorization failures and sends users into pointless retries.

**Fix:**
Add a start status for `403` and map it to the existing no-capability message.

```ts
type StartStatus = 'starting' | 'ready' | 'error' | 'error-auth' | 'error-forbidden';

catch (err) {
  if (isAuthError(err)) setStartStatus('error-auth');
  else if (err instanceof Error && err.message === 'HTTP 403') setStartStatus('error-forbidden');
  else setStartStatus('error');
}
```

Add a wizard test where `startOnboarding` rejects with `Error("HTTP 403")`.

---

_Reviewed: 2026-06-20T14:47:13Z_
_Reviewer: the agent (gsd-code-reviewer)_
_Depth: standard_
