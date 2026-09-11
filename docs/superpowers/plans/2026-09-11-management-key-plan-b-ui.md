# Management Key — Plan B (UI) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An admin connects OpenRouter, or picks a local route, in a first-run step that cannot be dismissed until they do; Aura restarts once when its services key is minted; the Credit panel and the spend overview explain a key that is missing or has no limit.

**Architecture:** The daemon's onboarding status gains `routeRequired` (an admin, a billing route, no management key); the reconcile result names the masked labels of the keys it minted; the credit route's 409 carries a cause code; the services cap is validated when written. In the SPA the routing pane stops showing the services key and asks for the services cap, a save hands its reconcile runs to the caller, a new `RouteStep` shows what was minted and restarts Aura, `FirstRunSetup` builds its steps from the status and the caller's role, and a `useFirstRunGate` hook opens it from AppShell.

**Tech Stack:** Go 1.27; React 19, TanStack Query 5, i18next, Vitest 5 + Testing Library, Playwright 1.62.

**Spec:** `docs/superpowers/specs/2026-09-10-management-key-onboarding-design.md` (Delivery item B). Plan A is landed; Plans C and D follow.

## Global Constraints

- `master`, one commit per task, imperative subject + why, last line `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`. Each task commit ticks its boxes here.
- Every user-visible string in `en` and `it`. No file above 600 lines (`web/src/AppShell.tsx` is 583: Task 6 moves the first-run gate out).
- Go: `go vet ./internal/agui/ && go build ./... && go test ./internal/agui/`, race in WSL (`MSYS_NO_PATHCONV=1 wsl.exe -- bash -lc 'cd /mnt/d/Repo/Aura && export PATH="$HOME/go/bin:/usr/local/go/bin:$PATH" && go test -race -count=1 ./internal/agui/'`).
- Web, from `web/`: `npx prettier --write <files>`, `npm run typecheck`, `npm run lint`, `npx vitest run <tests>`.
- A test changes only when it encodes behaviour this plan changes on purpose; the commit body names it.
- Wire names: status `routeRequired`; reconcile `minted_labels` (identity id → masked label, only keys minted in that run); credit 409 `cause` ∈ `management_key_unset` | `minting_unavailable` | `not_minted`. A key never reaches the SPA, only OpenRouter's masked label.

## Deviations from the spec

1. The base URL stays visible on the Cloud route: the Cloud button fills it, so the operator still types only the management key, the model and the cap. Hiding it would rewrite the route-memory tests for no change in what anyone types.
2. The Credit panel's cause is a code the daemon derives when asked. The reconciler keeps no per-identity provider error; that is in the daemon log, and the copy says so.
3. The spend overview banner renders Plan A's `uncapped_keys` (spec §"No limit").
4. The services cap is validated when written; Plan A stored any string and failed only at mint time.
5. A cap changed after the services key exists changes nothing at the provider (the reconciler only mints a missing key); the field's help says the cap applies when OpenRouter is first connected.
6. A `?onboarding=1` link now opens the setup once the status is read, so the route step knows whether it is required.

## File map

| Responsibility | Files |
|---|---|
| Status flag, minted labels, 409 cause, cap validation | `internal/agui/{onboarding_api,openrouter_reconcile,credit_api,settings_api_validate,settings_api}.go` |
| Routing form: cap in, services key out, save outcome | `web/src/settings/{modelSettingsDefs,modelSettingsState,settingsApi}.ts`, `ModelSettingsPanel.tsx`, `web/src/i18n/resources.settings.ts` |
| Route step | `web/src/onboarding/{routeStepModel.ts,RouteStep.tsx,onboardingApi.ts}`, `web/src/i18n/resources.onboarding.ts` |
| Wizard composition, dismissal | `web/src/onboarding/{FirstRunSetup,OnboardingDialog}.tsx` |
| Shell gate | `web/src/onboarding/useFirstRunGate.ts`, `web/src/AppShell.tsx` |
| Credit panel, spend overview | `web/src/admin/adminApi.ts`, `web/src/settings/{CreditPanel,SpendOverview}.tsx`, `web/src/i18n/resources.admin.ts` |
| Browser E2E, embedded build | `web/e2e/first-run-route.spec.ts`, `internal/webui/dist` |

---

### Task 1: The daemon tells the wizard what it needs

**Files:** `internal/agui/onboarding_api.go`, `openrouter_reconcile.go`, `credit_api.go`, `settings_api_validate.go`, `settings_api.go`; tests in `onboarding_api_handlers_test.go`, `openrouter_reconcile_test.go`, `credit_api_test.go`, `settings_api_branches_test.go`, `settings_api_authz_test.go`.

**Produces:** `OnboardingStatus.RouteRequired` (`routeRequired`); `OpenRouterKeysResult.MintedLabels` (`minted_labels`); credit 409 `{"error": "identity has no OpenRouter key yet", "cause": <code>}`; `validateSettingKeyValue(key, value string) error`.

- [x] **Step 1: Failing tests.** `TestHandleOnboardingStatusRouteRequired` (admin + billing route + no management key → true; key set, local route, member, no minter → false); `TestReconcileReportsTheLabelsItMinted` (first run maps `admin-1` → `sk-or-v1-...hash-1`, second run maps nothing); `TestAdminGetCreditExplainsAMissingKey` (no minter → `minting_unavailable`, key unset → `management_key_unset`, key set → `not_minted`); `TestValidateServicesCap` (`10`, `0.5`, empty accepted; `0`, `0.001`, `-1`, `ten` refused; other keys untouched); `TestAdminCannotStoreAServicesCapTheProviderRefuses` (PUT `ten` → 400, nothing written).
- [x] **Step 2: Run, watch them fail** (`go test ./internal/agui/` → build failure on the new names).
- [x] **Step 3: Implement.**

```go
// onboarding_api.go
	// RouteRequired is true for an admin while the route bills and no management key is set:
	// no identity's key can be minted yet, so the first-run route step cannot be skipped.
	RouteRequired bool `json:"routeRequired"`

// in handleOnboardingStatus, before writeJSON:
	status.RouteRequired = s.routeStepRequired(r.Context(), identityID)

func (s *Server) routeStepRequired(ctx context.Context, identityID string) bool {
	if s.keyMinter == nil || s.idAdmin == nil {
		return false
	}
	admin, err := s.idAdmin.HasCapability(ctx, identityID, identity.CapIdentityCreate)
	if err != nil {
		slog.Warn("onboarding status: admin check failed", "err", err)
		return false
	}
	if !admin {
		return false
	}
	skip, err := s.keyMinter.readiness(ctx)
	if err != nil {
		slog.Warn("onboarding status: minting readiness unreadable", "err", err)
		return false
	}
	return skip == skipManagementKeyUnset
}
```

```go
// openrouter_reconcile.go: new field, and reconcileIdentity keeps the minted label
	MintedLabels map[string]string `json:"minted_labels,omitempty"`
...
	minted, created, err := s.keyMinter.ensure(ctx, identityID, identityID)
	...
	if created {
		res.IdentitiesMinted = append(res.IdentitiesMinted, identityID)
		if res.MintedLabels == nil {
			res.MintedLabels = map[string]string{}
		}
		res.MintedLabels[identityID] = minted.Label
		return nil
	}
```

```go
// credit_api.go: the 409 body gains "cause": s.noKeyCause(r.Context())
func (s *Server) noKeyCause(ctx context.Context) string {
	if s.keyMinter == nil {
		return noKeyMintingUnavailable
	}
	if skip, err := s.keyMinter.readiness(ctx); err == nil && skip == skipManagementKeyUnset {
		return skipManagementKeyUnset
	}
	return noKeyNotMinted
}
```

```go
// settings_api_validate.go; handlePutSetting calls it right after validateSettingValue
func validateSettingKeyValue(key, value string) error {
	if key != servicesCapSetting || strings.TrimSpace(value) == "" {
		return nil
	}
	limit, err := openrouterprovision.NewUSDCapFromString(value)
	if err != nil || limit <= 0 {
		return errInvalidServicesCap
	}
	return nil
}
```

- [x] **Step 4: Run, watch them pass;** race in WSL.
- [x] **Step 5: Commit** `feat(agui): tell the first-run setup what the route step needs`.

---

### Task 2: The routing form asks for the services cap, never the services key

**Files:** `web/src/settings/modelSettingsDefs.ts` (`SettingsKey`: `OPENROUTER_API_KEY` out, `AURA_OPENROUTER_SERVICES_CAP_USD` in; `PRIMARY_SETTINGS`: the services-key row replaced by a `cloudOnly` cap row placed BEFORE the management key, so the management key's PUT reconciles with the cap already stored); `modelSettingsState.ts` (`OPENROUTER_API_KEY` leaves `HOT_LLM_PROFILE_KEYS`); `resources.settings.ts` (`fields.openRouterKey` out; `fields.openRouterServicesCap` = "Services key monthly cap (USD)" / "Limite mensile chiave dei servizi (USD)", `help.openRouterServicesCap` in); tests `ModelSettingsPanel.openrouter.test.tsx`, `ModelSettingsPanel.test.tsx`.

- [ ] **Step 1: Tests.** `ModelSettingsPanel.openrouter.test.tsx`: both views save the cap, then the management key, each as its own PUT; the two rows show only on Cloud; the services key is never rendered. `ModelSettingsPanel.test.tsx` "toggles the cloud provider, edits the secret field" is rewritten to edit the management key (the services key is minted by Aura and refused by the API).
- [ ] **Step 2: Run, watch them fail.**
- [ ] **Step 3: Implement** the defs, state and copy changes above.
- [ ] **Step 4: Run, watch them pass;** typecheck, lint.
- [ ] **Step 5: Commit** `feat(web): ask for the services cap, never the services key`.

---

### Task 3: A save hands back what the reconciler did

**Files:** `web/src/settings/settingsApi.ts` (`OpenRouterKeysResult`; `SettingWriteResult.openrouter_keys?`; `putLLMProfile` returns `LLMProfileWriteResult {updated, restart_required, openrouter_keys?}`); `modelSettingsState.ts` (`SaveOutcome {openRouterKeys}`; `save(onComplete?: (outcome: SaveOutcome) => …)` collects every write's `openrouter_keys` in write order; no dirty keys → `{openRouterKeys: []}`); `ModelSettingsPanel.tsx` (`onComplete?: (outcome?: SaveOutcome) => …`, new `skippable?: boolean`, default true, hides Skip); test `ModelSettingsPanel.openrouter.test.tsx`.

- [ ] **Step 1: Tests:** onComplete receives `{openRouterKeys: [capRun, managementKeyRun]}`; `skippable={false}` hides Skip and keeps Continue.
- [ ] **Step 2: Run, watch them fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, watch them pass.**
- [ ] **Step 5: Commit** `feat(web): hand the reconciler's runs to whoever saved the routing form`.

---

### Task 4: The route step

**Files:** create `web/src/onboarding/routeStepModel.ts` (`summarizeOpenRouterKeys(runs, ownIdentityId) → {ownLabel, servicesLabel, errors}`: labels from whichever run minted them, errors from the LAST run only, since the management key's PUT runs after the cap's), `web/src/onboarding/RouteStep.tsx` (`{required, onRequiredChange, onDone}`: the routing pane with `skippable={!required}`; after a save, re-read the status when the step was required (a failed read counts as not required); errors or still required → stay and explain; services key minted → `requestRestart` then `watchRestart` with a `reload` that moves to "ready" instead of reloading the page; own key minted → "ready"; else `onDone`); `onboardingApi.ts` (`OnboardingStatus.routeRequired`); `resources.onboarding.ts` (`profile.steps.route`, `profile.route.*`); `web/e2e/auth.ts` fixture gains `routeRequired: false`; typed status mocks in `AppShell.{shell,usage,promptDrafts}.test.tsx` and `onboardingApi.test.ts` gain the field; tests `routeStepModel.test.ts`, `RouteStep.test.tsx`.

- [ ] **Step 1: Tests.** Model: empty runs; labels picked by own id from any run; last run's errors only. Step (panel, restart and status mocked): optional save that minted nothing → `onDone`, no status read, no restart; minted services key → labels shown, one restart, "Continue" → `onDone`; provider errors → alert, no `onDone`; still required → "needs the OpenRouter management key" alert; restart unsupported → warning + Continue.
- [ ] **Step 2: Run, watch them fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, watch them pass;** typecheck.
- [ ] **Step 5: Commit** `feat(web): the first-run route step mints the keys and restarts Aura once`.

---

### Task 5: The setup builds its steps and cannot be closed while the route is required

**Files:** `web/src/onboarding/FirstRunSetup.tsx` (props `profileRequired = true`, `routeRequired = false`; steps = profile and Telegram while the profile is owed, the route step for an admin or whenever required; route-only run closes after the route step; `onClose` withheld from the dialog while required); `OnboardingDialog.tsx` (`onClose` optional, no Close button without it); `resources.onboarding.ts` (`profile.steps.runtime`, `profile.runtime` out); test `FirstRunSetup.test.tsx` (useCapabilities mocked; status mock added).

- [ ] **Step 1: Tests:** a member gets "Step 1 of 2" and no route step; an admin with only the route required sees "Step 1 of 1", no Close, no Skip, and the setup closes after the save without submitting a profile; the existing walk keeps passing for an admin.
- [ ] **Step 2: Run, watch them fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, watch them pass.**
- [ ] **Step 5: Commit** `feat(web): keep the first-run setup open until an admin picks the route`.

---

### Task 6: The shell opens the setup for the route step alone

**Files:** create `web/src/onboarding/useFirstRunGate.ts` (`useFirstRunGate({linkRequested, onOpen, clearLink}) → {open, status, close}`: opens once per session when the status says the profile is owed or the route is required, or when a `?onboarding=1` link asked, in which case it opens once the status is read or fails and clears the link); `web/src/AppShell.tsx` (the two onboarding effects, `profileOnboardingOpen` and `autoOpenedOnboarding` go; `FirstRunSetup` gets `profileRequired={status?.required ?? true}` and `routeRequired={status?.routeRequired ?? false}`); test `useFirstRunGate.test.tsx`.

- [ ] **Step 1: Tests:** opens for a required profile and for the route alone, carrying the status; stays closed when nothing is owed; the link opens it with the status and clears the URL; `close` closes.
- [ ] **Step 2: Run, watch them fail.**
- [ ] **Step 3: Implement;** `wc -l web/src/AppShell.tsx` under 600.
- [ ] **Step 4: Run** the hook test and `AppShell.shell.test.tsx`.
- [ ] **Step 5: Commit** `refactor(web): open first-run setup from one gate, for the route step too`.

---

### Task 7: The Credit panel and the spend overview explain a missing or unlimited key

**Files:** `web/src/admin/adminApi.ts` (`CreditNoKey {identity_id, no_key: true, cause}` in `CreditResponse`; `fetchIdentityCredit` maps a 409 to it, other failures throw `httpErrorFrom`; `SpendOverviewOverAllocation.uncapped_keys`); `web/src/settings/CreditPanel.tsx` (no-key state with the cause copy; one `CreditEmpty` shared with the exempt state); `SpendOverview.tsx` (the banner adds the uncapped count when above zero); `resources.admin.ts` (`credit.noKeyHeading`, `credit.noKeyCause.{management_key_unset,minting_unavailable,not_minted}`, `overview.uncappedKeys`); tests `adminApi.test.ts`, `CreditPanel.test.tsx`, `SpendOverview.test.tsx`.

- [ ] **Step 1: Tests:** a 409 resolves to the no-key shape with its cause, a 502 rejects; the panel shows the heading and each cause's copy (unknown → `not_minted`) and never the load error; the banner reports uncapped keys.
- [ ] **Step 2: Run, watch them fail.**
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run, watch them pass.**
- [ ] **Step 5: Commit** `feat(web): explain a missing or unlimited OpenRouter key`.

---

### Task 8: Browser E2E, embedded build, gates, push

- [ ] **Step 1:** `web/e2e/first-run-route.spec.ts` against mocked routes (status `routeRequired`, admin `/api/me`, settings list and writes with `openrouter_keys`, restart 202, one refused `/healthz`): no Close and no Skip, the cap then the management key are written, the minted labels show, the setup closes, the management key never renders.
- [ ] **Step 2:** `npm run build` in `web/` (writes `internal/webui/dist`); `docker compose build aura && docker compose up -d --no-deps aura`; run the spec against the rebuilt stack.
- [ ] **Step 3:** web gates (`npm run typecheck`, `npm run lint`, `npm run format:check`, `npm test`), `make quality` in WSL.
- [ ] **Step 4:** commit the spec and the dist, push, watch CI green.
