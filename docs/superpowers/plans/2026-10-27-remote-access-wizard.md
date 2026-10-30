# Remote access from a phone: the wizard and its reconciler — Implementation Plan

**Status (2026-10-30):** IN PROGRESS. Tasks 1 and 2 are done (`a6405bda6`..`ddeadc01f`) and Task 3 is
in review. Revision 2 (2026-10-30) follows a Codex plan review: claudex-loop runner, gpt-6-astra,
verdict REVISE with 10 findings, all accepted. Its changes:
- a new Task 4 persists the resources' own account and the legacy WARP hostname;
- the old Tasks 4–10 are renumbered 5–11 and amended;
- the dispositions are recorded in `.superpowers/sdd/2026-10-27-remote-access-wizard/codex-round1-dispositions.md`.

Revision 3 (2026-10-30) follows Codex's round 2: REVISE with 6 new findings, all accepted, recorded in
`codex-round2-dispositions.md` in the same folder.

This is part 1 of 3 of the spec. Part 2 (the home certificate) and part 3 (finding F-1) are separate
plans: they share no code with this one.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or
> superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for
> tracking. Commits on master (the operator's standing rule).

**Goal:** the operator sets up remote access from the cockpit in four steps they can walk back at any
time, reaches Aura from a phone through Cloudflare Access with one email PIN a month, and every failure
names its reason. WARP is gone, including from installs that have it.

**Architecture:**
- **The reconciler owns an address.**
  - Three fields record what the Cloudflare resources were made for:
    - `State.OwnedHostname` and `owned_zone_name`;
    - `Resources.AccountID`, stored in its own column `resource_account_id`, never in the desired
      `account_id`.
  - When the desired account, zone or label differs, the next reconcile stops the connector, tears
    the owned set down and provisions the new one.
  - `ErrAddressLocked` and the SQL address lock go.
- **Acceptance belongs to a hostname,** not to a reconcile pass. `State.AcceptedHostname` survives
  tunnel outages, so a down tunnel is `degraded` + reason `tunnel_down` and turns `healthy` by itself.
- **Failures carry a code** (`remotetunnel.Reason`), persisted in place of `last_error` and returned
  by the API. The cockpit maps each code to a sentence, an action and the step that owns it.
- **WARP leaves the reconciler.** One step, `retireWARP`, deletes the WARP resources an install owns
  and clears their IDs. Nothing creates them any more.
- **The cockpit wizard** is the first-run step strip (`StepStrip`, extracted from `OnboardingStepper`)
  with four step cards. After setup it stays, every step green.

**Tech Stack:** Go (`internal/remotetunnel`, `internal/cloudflareapi`, `internal/agui`, `cmd/aura`),
Postgres + sqlc (`internal/db/queries/cloudflare_remote_access.sql`), React + react-i18next + TanStack
Query (`web/src/settings/remoteAccess`), Vitest, Playwright, the lab VM.

**Spec:** `docs/superpowers/specs/2026-10-27-remote-access-from-phone-design.md` (approved
2026-10-27, `a0681e6d3`). It amends `docs/superpowers/specs/2026-09-20-cloudflare-tunnel-design.md`.

## Global Constraints

- **Steps, in this order:** `cloudflare`, `domain`, `connection`, `phone`. The labels are IT *Cloudflare
  · Dominio · Collegamento · Prova dal telefono* and EN *Cloudflare · Domain · Connection · Try it from
  your phone*. Phone width shows "Passo N di 4 · <label>" / "Step N of 4 · <label>".
- **Access application session:** `accessSessionDuration = "730h"`. Read the `session_duration` field
  in Cloudflare's Access applications API reference first, and cite it in the comment. If the API caps
  below 730h, use its maximum and record that in the spec. The organisation's global session is never
  written.
- **Reason codes** (Go `remotetunnel.Reason`, SQL CHECK, TS union), exactly:
  - `""` means no reason;
  - `token_rejected`, `no_accounts`, `account_not_visible`, `domain_invalid`, `domain_elsewhere`;
  - `hostname_taken`, `not_owned`, `no_administrator`, `cloudflare_unreachable`, `tunnel_down`;
  - `old_address_present`, `unknown`.
- **Reason → step:**
  - `cloudflare`: `token_rejected`, `no_accounts`, `account_not_visible`;
  - `domain`: `domain_invalid`, `domain_elsewhere`, `hostname_taken`;
  - `phone`: `tunnel_down`;
  - `connection`: every other code.
- **No upstream text reaches the browser or the database.** Logs keep the cause, as `failed` does
  today.
- **Cockpit look:**
  - tokens from `web/src/styles/theme.css`;
  - `Button` variants `default` / `outline` / `destructive`;
  - cards `rounded-md border border-border bg-surface p-4`;
  - section heading `text-[20px] font-semibold`;
  - no second column next to the Settings menu.
- **Italian copy** carries its accents. Banned in operator text: "tunnel con nome", "SSE",
  "riconciliazione", "proiettato", "membership".
- **Files ≤ 600 LOC.** Go commands run in WSL with `export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`;
  per touched package run `go vet`, `go test` and `go test -race`. Vitest runs on Windows:
  `node node_modules/vitest/vitest.mjs run …` from `web/`.
- **Migration number:** `ls internal/db/migrations/ | tail -1` at landing, plus one (0146 on
  2026-10-30). Then `make sqlc`.

## Review Focus

1. **An outage during the phone test.** If the tunnel drops after the operator confirmed, the step goes
   red; it must never ask for the test again. Test: Task 2
   `TestOutageAfterAcceptanceDegradesAndHealsWithoutANewTest`.
2. **A label change while the zone is still pending.** The pending zone stays: same zone, so the old
   resources are torn down but the zone ID is kept. The new hostname is provisioned when the zone
   activates. Test: Task 5 `TestLabelChangeKeepsThePendingZone`.
3. **An install whose WARP CNAME was already deleted by hand.** `retireWARP` must treat 404 as gone,
   clear the ID and go on. It must never be stuck in `not_owned`. Test: Task 3
   `TestRetireWARPToleratesResourcesAlreadyGone`.
4. **A domain answering NXDOMAIN** (a fresh registration, `chetto1983.it` on 2026-10-27). The NS lookup
   fails, which must mean "not on Cloudflare", so the zone is created. Test: Task 6
   `TestZoneLookupFailureCreatesTheZone`.
5. **A browser left open on an old generation.** *Avanti* after another tab saved must show the
   refresh message, not a raw 409, and keep what was typed. Test: Task 9
   `DomainStep keeps the input on a stale generation`.

## Decisions taken while writing this plan

- **The teardown reuses `deleteResources`' step list,** split into `teardown(ctx, s) error`. It deletes
  owned resources and clears their IDs, and changes neither `Desired` nor the phase. `deleteResources`
  calls it, then sets `disabled`. Ownership checks read `OwnedHostname` and `Resources.AccountID`, never
  `Desired`. That is what lets an address change remove the old set.
- **`warp_label` stays in the table.** It is no longer written, and is read only by `retireWARP` to
  rebuild the legacy WARP hostname. Dropping `warp_label` and the `warp_*` ID columns is a follow-up
  migration, once this release has reached Andrea's appliance (spec decision 1).
- **`last_error` is dropped** in the same migration and replaced by `reason`. A non-empty legacy row
  becomes `unknown`.
- **`PersistedAcceptance`, the `Acceptance` interface and `WithAcceptance` are deleted.** Acceptance is
  `state.AcceptedHostname == publicHostname(state.Desired)`, read where it is used.
- **Abandoned resources** are a `jsonb` array of `{kind, name}` on the row. They are written when a new
  token cannot list the account the resources live in. They are cleared by a new action,
  `POST /api/settings/remote-access/abandoned/dismiss`.
- **The account name** comes from a controller cache (`accountNames map[string]string`), filled by
  `Verify` and by the first `Status` after a token change. Status reads it, so a reload shows the name.
- **The NS check is in the reconciler's `zone()`** behind a `NameServers` interface (`*net.Resolver`
  in production), injected with `WithNameServers`. Configure does not duplicate it.
- **`StepStrip` lives in `web/src/components/StepStrip.tsx`.** `OnboardingStepper` becomes a caller of
  it, so the strip has one implementation.
- **`/events` is not shown.** The phone step's "checked N minutes ago" is `last_checked_at`, and the
  reason says what went wrong. A history would add a screen the operator did not ask for. The route
  stays for diagnostics.
- **The token link.** Task 9 Step 3 reads Cloudflare's documentation on pre-filled token creation
  links. If such a link is documented, `CloudflareStep` opens it with the permissions below; if not,
  it lists them. The permissions are:
  - Account Settings Read;
  - Zone Read and Zone Edit;
  - DNS Edit;
  - Cloudflare Tunnel Edit;
  - Access: Apps and Policies Edit;
  - Access: Organizations, Identity Providers, and Groups Edit.

  Gateway and Zero Trust posture permissions leave the list. An existing token keeps them, and
  `retireWARP` uses them once.
- **The home certificate card** (spec decision 6) is part 2's: this plan's `PhoneStep` leaves room for
  it below the status card, and adds nothing for it.

Decisions added by revision 2 (Codex round 1, `RW-*` ids):
- **Ownership is persisted, never read from the desired columns** (RW-001, RW-005, RW-006).
  - Task 4 adds three columns, which only provisioning, teardown and abandon write; `SaveDesired`
    never does:
    - `resource_account_id`;
    - `owned_zone_name`;
    - `legacy_warp_hostname`.
  - The owned set is recorded before the first resource is created, not at `publish`.
- **Teardown stops the connector first** (RW-002). The stored tunnel credential is cleared and the
  projection goes off before any deletion. A tunnel deletion refused while connections remain is a
  retryable failure (`cloudflare_unreachable`), never a dead end. After a restart the connector stays
  off, because `rebuild` has no credential to project.
- **An account change detaches the zone and the OTP provider** after cleanup, without deleting them
  (RW-007). `ZoneID` survives only when both the account and the zone match.
- **WARP retirement runs in every phase** (RW-008, RW-009).
  - It runs before `Reconcile`'s early returns for `error` and disabled installs, never enables
    anything, and never clears an error it did not cause.
  - If a WARP resource answers 401/403 to a token that sees the account, the resource is recorded in
    `abandoned` and its ID is cleared. The public reconcile goes on.
- **Abandon retires the old connector** (RW-003). It clears the stored tunnel token in the same
  transaction, and the pass it wakes projects off (RW-014, below). At startup, the projection refuses
  a credential whose tunnel ID differs from the row's `tunnel_id`.
- **The first step saves without a domain** (RW-004, corrected by RW-011).
  - The Cloudflare step calls `configure` with the token and the account, plus the status's current
    `zone_name`, `public_label` and `enabled`.
  - On a first setup those are empty and false. The server verifies the token and that the account is
    visible, stores both, and calls no zone API. The reconciler stays `disabled`, and the status reads
    `api_token_set: true`, so the wizard opens *Dominio*.
  - On a configured install they are the current address. A replaced token keeps the address, the
    enabled state and the acceptance.
  - The Domain step saves `enabled: true` with the zone and label.
  - The `prerequisite_required` answer for an empty domain goes.

Decisions added by revision 3 (Codex round 2):
- **A retirement failure on a disabled or `error` install never goes through `failed`** (RW-012).
  `SaveDesired` reads `error + disabled + retained references` as deletion intent, so writing `error`
  there would turn a later re-enable into a deletion of the public resources. The failure is logged,
  and the phase and the reason stay as they were.
- **Retirement withdraws a route before its protection** (RW-013). Cloudflare leaves a published route
  without an Access application open to the internet, and the old ingress lives until `publish`.
  - The WARP app and policy are deleted only once the WARP CNAME is gone.
  - If the CNAME is denied, the CNAME, the policy and the app go to `abandoned` together, left
    untouched in Cloudflare.
  - The posture check is independent of them.
- **The daemon stops the old connector, not the request** (RW-014). Every `reconcile` pass with no
  `tunnel_id` projects off, before anything can return early, and a failure stops the pass. So a
  commit followed by a lost or failed projection is repaired while the new zone is still pending, and
  nothing is provisioned until the old connector is off.

## File structure

- `internal/db/migrations/0146_remote_access_reasons.{up,down}.sql` (Task 1, landed) and
  `0147_remote_access_ownership.{up,down}.sql` (Task 4; number at landing).
- `internal/remotetunnel/reconciler.go` and `cmd/aura/serve_remote_access.go`: teardown stops the
  connector (Task 5), a pass with no tunnel projects off, and startup refuses a credential for another
  tunnel (Task 7).
- `internal/db/queries/cloudflare_remote_access.sql`, regenerated `internal/db/sqlc/cloudflare_remote_access.sql.go`.
- `internal/remotetunnel/model.go`: `Reason`, `AbandonedResource`, and the new `State` fields.
- `internal/remotetunnel/reasons.go` (new): `reasonOf`, `ErrHostnameTaken`, `ErrDomainElsewhere`.
- `internal/remotetunnel/store.go`: no address lock; `Abandon`, `DismissAbandoned`.
- `internal/remotetunnel/reconciler.go`: acceptance by hostname, reason, outage self-heal.
- `internal/remotetunnel/acceptance.go`: `AcceptExternal` only.
- `internal/remotetunnel/readdress.go` (new): `addressChanged`, the teardown call in `reconcile`.
- `internal/remotetunnel/warp_retire.go` (new): `retireWARP`.
- `internal/remotetunnel/steps_access_dns.go`: one route, the session constant, `ErrHostnameTaken`.
- `internal/remotetunnel/steps_zone_tunnel.go`: no WARP validation; the NS check.
- `internal/remotetunnel/delete.go`: `teardown` split out; ownership by `OwnedHostname`.
- `internal/cloudflareapi/access.go`: `GatewayEmailPolicy`, `ListPosture`, `EnsureGatewayPosture`
  deleted.
- `internal/agui/remote_access_api.go` and `internal/agui/idempotency_http.go`: the DTO, `reason` in
  error bodies, and the dismiss route.
- `cmd/aura/serve_remote_access.go`, `cmd/aura/serve_remote_access_wiring.go`: Configure, Status, the
  account cache, abandon on token change, `WithNameServers(net.DefaultResolver)`.
- `web/src/components/StepStrip.tsx` (new) and `web/src/onboarding/OnboardingStepper.tsx`.
- `web/src/settings/remoteAccess/`:
  - new: `remoteAccessSteps.ts`, `CloudflareStep.tsx`, `DomainStep.tsx`, `ConnectionStep.tsx`,
    `PhoneStep.tsx`, `ReasonNotice.tsx`;
  - rewritten: `RemoteAccessWizard.tsx`, `RemoteAccessPanel.tsx`, `remoteAccessApi.ts`;
  - deleted: `RemoteAccessStatus.tsx`, `HostnamesStep.tsx`, `VerifyStep.tsx`, `ZoneStep.tsx`,
    `TokenStep.tsx`, and their tests.
- `web/src/i18n/resources.remoteAccess.ts`: rewritten, both locales.
- `web/e2e/remote-access.spec.ts` (rewritten), `web/e2e/remote-access-live.spec.ts`,
  `web/e2e/support/remoteAccessLive.ts` and `scripts/cloudflare_tunnel_live_e2e.sh` (WARP removed).

---

### Task 1: The row carries a reason, an owned hostname and an accepted hostname

**Status:** DONE (`a6405bda6`, `bd3cffe81`).

**Files:**
- Create: `internal/db/migrations/0146_remote_access_reasons.up.sql`, `.down.sql`
- Modify: `internal/db/queries/cloudflare_remote_access.sql`, `internal/remotetunnel/model.go`, `internal/remotetunnel/store.go`
- Test: `internal/remotetunnel/store_integration_test.go`, `internal/remotetunnel/store_test.go`

**Interfaces:**
- Produces:
  - `type Reason string` with the 12 constants of Global Constraints (`ReasonTokenRejected`, …,
    `ReasonUnknown`);
  - `type AbandonedResource struct { Kind, Name string }` with JSON `kind` and `name`;
  - `State` gains `OwnedHostname, AcceptedHostname string`, `Reason Reason` and
    `Abandoned []AbandonedResource`, and loses `LastError`;
  - `Desired` loses `WARPLabel`, and `Resources` gains `LegacyWARPHostname string` (read-only, from
    `warp_label` + `zone_name` when any `warp_*` ID is set);
  - `(*Store).Abandon(ctx, generation int64, list []AbandonedResource) (State, error)` clears every
    resource ID, `tunnel_name`, `owned_hostname` and `accepted_hostname`;
  - `(*Store).DismissAbandoned(ctx) error`.

- [ ] **Step 1: Write the failing integration tests** (`db_integration` tag, in the existing file)

```go
func TestSaveDesiredAcceptsAnAddressChangeWithResources(t *testing.T) // seed public_dns_id="d1"; SaveDesired with zone "new.it" → nil error, row zone_name "new.it", public_dns_id still "d1"
func TestSaveDesiredKeepsAcceptanceOnlyForTheSameHostname(t *testing.T) // accepted_hostname "aura.a.it"; SaveDesired same zone/label → kept; label "x" → ""
func TestAdvancePersistsReasonAndOwnedHostname(t *testing.T) // Advance{Reason: ReasonTunnelDown, OwnedHostname: "aura.a.it"} → Load returns both
func TestReasonCheckRejectsAnUnknownCode(t *testing.T)     // raw UPDATE reason='boom' → SQLSTATE 23514
func TestAbandonClearsResourcesAndListsThem(t *testing.T)   // Abandon(gen, [{tunnel aura-…}]) → all ids "", abandoned == list; DismissAbandoned → []
func TestMigrationMapsLegacyErrorToUnknown(t *testing.T)    // row with last_error set before up → reason "unknown" after
```

- [ ] **Step 2: Run them to see them fail**

Run (WSL): `go test -tags db_integration ./internal/remotetunnel/ -run 'SaveDesired|Advance|Reason|Abandon|Migration'`.
Expected: compile errors on `Reason`, `OwnedHostname` and `Abandon`.

- [ ] **Step 3: Write the migration.** In the up file:
  - add `owned_hostname text NOT NULL DEFAULT ''` and `accepted_hostname text NOT NULL DEFAULT ''`;
  - add `abandoned jsonb NOT NULL DEFAULT '[]'`;
  - add `reason text NOT NULL DEFAULT '' CHECK (reason IN (…the 12 codes and ''…))`;
  - backfill: `owned_hostname = public_label||'.'||zone_name WHERE public_dns_id <> ''`,
    `accepted_hostname = owned_hostname WHERE observed_healthy`, and
    `reason = 'unknown' WHERE last_error <> ''`;
  - `DROP COLUMN last_error`.

  The down file restores `last_error` and drops the four columns.
- [ ] **Step 4: Rewrite the queries.**
  - `SaveCloudflareRemoteAccessDesired`:
    - drop the address-lock condition and the `warp_label` parameter;
    - keep the deleting branch;
    - `accepted_hostname = CASE WHEN zone_name = arg AND public_label = arg THEN accepted_hostname ELSE '' END`;
    - `reason` follows the deleting rule `last_error` had.
  - `AdvanceCloudflareRemoteAccess`: gains `owned_hostname`, `accepted_hostname`, `reason`,
    `abandoned`, and loses `last_error`.
  - New `:one` queries `AbandonCloudflareRemoteAccess` and `DismissCloudflareRemoteAccessAbandoned`.
  - Run `make sqlc`.
- [ ] **Step 5: Update `model.go` and `store.go`.**
  - Delete `ErrAddressLocked` and `desiredFailure`'s lock branch. A no-row result is now always
    `ErrStaleGeneration`.
  - `Advance` writes `Reason` as is: it is a code, never text.
- [ ] **Step 6: Run the tests until they pass**, plus `go vet ./internal/remotetunnel/ ./internal/db/...`.
  Expected: PASS. Every other package that read `LastError` or `ErrAddressLocked` fails to compile.
  Fix those callers in the task that owns them (Tasks 2, 7), and for now stub them only to restore
  the build: `LastError` reads become `string(state.Reason)`.
- [ ] **Step 7: Commit** `feat(remote-access): persist a reason code and the hostnames resources belong to`.

### Task 2: A phone test once per address; an outage heals by itself

**Status:** DONE (`2e49dd85f`, `ddeadc01f`). The review added the following:
- non-timeout network errors are retryable;
- an outage runs at a 10s pace and keeps its red state;
- `ErrDomainInvalid` is distinct from `ErrConfiguration`;
- Cloudflare's `degraded` tunnel counts as serving.

**Files:**
- Modify: `internal/remotetunnel/reconciler.go`, `internal/remotetunnel/acceptance.go`, `cmd/aura/serve_remote_access.go` (`engine`: drop `WithAcceptance`)
- Create: `internal/remotetunnel/reasons.go`
- Test: `internal/remotetunnel/reconciler_test.go`, `internal/remotetunnel/acceptance_test.go`, `internal/remotetunnel/reasons_test.go`

**Interfaces:**
- Consumes: Task 1's `State`, `Reason`.
- Produces:
  - `func publicHostname(d Desired) string`;
  - `func reasonOf(err error) Reason`;
  - `var ErrHostnameTaken, ErrDomainElsewhere, ErrNoAccounts, ErrAccountNotVisible error`.

  `failed` sets `s.Reason = reasonOf(err)`.

- [ ] **Step 1: Write the failing tests** (fake Cloudflare from `reconciler_fake_test.go`)

```go
func TestAcceptExternalRecordsTheHostname(t *testing.T)                        // after AcceptExternal: AcceptedHostname == "aura.example.com", Phase healthy
func TestOutageAfterAcceptanceDegradesAndHealsWithoutANewTest(t *testing.T)    // accepted; fake tunnel.Status="down" → Reconcile: Phase degraded, Reason tunnel_down, AcceptedHostname kept; Status="healthy" → Reconcile: Phase healthy, Reason ""
func TestTokenReplacementKeepsAcceptance(t *testing.T)                          // SaveDesired same address → Reconcile with healthy tunnel → healthy, no AcceptExternal call
func TestNoAcceptanceStaysConnecting(t *testing.T)                             // healthy tunnel, AcceptedHostname "" → Phase connecting, Reason ""
func TestReasonOf(t *testing.T) // table: ErrHostnameTaken→hostname_taken, ErrDomainElsewhere→domain_elsewhere, ErrNoAccounts→no_accounts, ErrAccountNotVisible→account_not_visible, ErrConfiguration→domain_invalid, ErrOwnershipConflict→not_owned, ErrMemberLockout→no_administrator, ErrRemotePresent→old_address_present, &cloudflareapi.APIError{Status:403}→token_rejected, &cloudflareapi.APIError{Retryable:true}→cloudflare_unreachable, context.DeadlineExceeded→cloudflare_unreachable, errors.New("x")→unknown
```

- [ ] **Step 2: Run them to see them fail.** `go test ./internal/remotetunnel/ -run 'Accept|Outage|TokenReplacement|NoAcceptance|ReasonOf'`. Expected: FAIL / compile errors.
- [ ] **Step 3: Implement.**
  - `reconcile`'s tail: the tunnel healthy and accepted → `healthy`, `ObservedHealthy=true`, reason
    `""`. Accepted but not healthy → `degraded`, `tunnel_down`, acceptance kept. Not accepted →
    `connecting`.
  - `AcceptExternal` sets `AcceptedHostname`.
  - Delete `PersistedAcceptance`, the `Acceptance` interface and `WithAcceptance`.
  - `failed` sets the reason and keeps its log line.
  - A 401/403 `APIError` maps to `token_rejected`.
- [ ] **Step 4: Run the package's tests and `-race`.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(remote-access): accept a hostname once and heal an outage by itself`.

### Task 3: WARP leaves, and an install that has it loses it

**Status:** implemented (`c99c96c1b`), in review.

**Revision 2:** RW-008 and RW-009 join this task's fix round:
- retirement runs in every phase;
- a WARP resource the token cannot touch goes to `abandoned` without blocking.

Because a reconcile pass now writes `abandoned`, the same round makes a dismiss permanent (the Task 1
ledger minor):
- `Advance` stops writing `abandoned`;
- the reconciler and `Abandon` both append through SQL (`abandoned = abandoned || $1`), so nothing
  leaves the list before the operator dismisses it (RW-016);
- a pass that loaded the row before a dismiss can no longer bring the list back.

**Revision 3:** RW-012 and RW-013 join the next fix round, with these tests:

```go
func TestRetireWARPFailureOnADisabledInstallNeverWritesError(t *testing.T)  // foreign WARP resource, disabled → Phase disabled, Reason "", the cause only in the log
func TestDisabledRetirementFailureNeverBecomesADeletion(t *testing.T)       // db_integration, the real SaveDesired: the row above, then SaveDesired enabled true → Phase validating, public resources untouched
func TestRetireWARPDeniedCNAMEKeepsItsProtection(t *testing.T)              // DNS answers 403 → no delete call on policy or app, all three in abandoned, all three IDs "", posture still retired
func TestRetireWARPDeniedCNAMEThenFailureBeforePublish(t *testing.T)        // the same, then a failure before publish → the fake's Access app for the WARP hostname still exists
```

**Files:**
- Create: `internal/remotetunnel/warp_retire.go`
- Modify: `internal/remotetunnel/steps_access_dns.go`, `steps_zone_tunnel.go`, `delete.go`, `reconciler.go`, `ports.go`, `internal/cloudflareapi/access.go`
- Test: `internal/remotetunnel/warp_retire_test.go`, `reconciler_validation_test.go`, `internal/cloudflareapi/resources_test.go`

**Interfaces:**
- Consumes: `Resources.LegacyWARPHostname` (Task 1).
- Produces:
  - `func (r *Reconciler) retireWARP(ctx context.Context, s *State) error`, called first in
    `reconcile` and in `teardown`;
  - `routes(s)` returns the public route only.

- [ ] **Step 1: Write the failing tests**

```go
func TestRetireWARPDeletesOwnedLegacyResources(t *testing.T)     // fake seeded with warp CNAME (comment=tunnel name), app aura-<uuid>-warp, policy …-warp-members, posture …-gateway → after Reconcile: fake has none of them, all four IDs "", public resources untouched
func TestRetireWARPToleratesResourcesAlreadyGone(t *testing.T)    // WARPDNSID set, fake returns 404 → ID cleared, Reconcile continues to healthy
func TestRetireWARPRefusesAForeignResource(t *testing.T)          // warp app name "someone-else" → Reason not_owned, nothing deleted
func TestValidateDesiredNoLongerNeedsAWARPLabel(t *testing.T)     // Desired without WARP fields validates
func TestApplicationSessionIsOneMonth(t *testing.T)               // PutApplication body SessionDuration == accessSessionDuration ("730h"), also for an app that had "24h"
```

- [ ] **Step 2: Run them to see them fail.**
- [ ] **Step 3: Implement.**
  - `retireWARP` uses the existing ownership predicates:
    - `ownedDNS` against `LegacyWARPHostname`;
    - the `-warp` app and `-warp-members` policy names;
    - `ownedPosture`.
  - It deletes the DNS record, then the policy, the app and the posture, and persists each cleared
    ID, as `deleteResources` does.
  - Delete the warp route and `posture()`, plus `cloudflareapi.GatewayEmailPolicy`, `ListPosture`
    and `EnsureGatewayPosture` with their tests.
  - Keep `GetPosture` and `DeletePosture`, which the retirement uses.
  - `accessSessionDuration = "730h"`, with the doc citation (Global Constraints).
- [ ] **Step 4: Run** the `remotetunnel` and `cloudflareapi` tests, with `-race` and `make deadcode`.
  Expected: PASS, with no dead WARP symbol.
- [ ] **Step 5: Commit** `feat(remote-access): drop WARP and retire it from existing installs`.

### Task 4: Resources remember their own account, zone and legacy WARP hostname

Revision 2, from RW-001, RW-005 and RW-006. Desired values must never stand in for what the resources
belong to.

**Files:**
- Create: `internal/db/migrations/0147_remote_access_ownership.up.sql`, `.down.sql` (number at landing)
- Modify: `internal/db/queries/cloudflare_remote_access.sql`, `internal/remotetunnel/model.go`, `store.go`
- Test: `internal/db/migrate_0147_integration_test.go`, `internal/remotetunnel/store_integration_test.go`, `store_test.go`

**Interfaces:**
- Produces:
  - `Resources.AccountID` is read from `resource_account_id`;
  - `State.OwnedZoneName string` is read from `owned_zone_name`;
  - `Resources.LegacyWARPHostname` is read from `legacy_warp_hostname`, and is no longer derived from
    `warp_label` + `zone_name`;
  - `Advance` writes all three, and `SaveDesired` writes none of them.

- [ ] **Step 1: Write the failing tests**

```go
// internal/db, on the migrateToVersion(..., <previous>) seam, like migrate_0146_integration_test.go:
func TestMigrate0147BackfillsOwnership(t *testing.T) // table over the row's IDs, always account "A", zone "a.it", label "aura", warp_label "aura-warp":
                                                     //   tunnel_id + warp_dns_id → resource_account_id "A", owned_zone_name "a.it", owned_hostname "aura.a.it", legacy_warp_hostname "aura-warp.a.it";
                                                     //   zone_id only (a pending legacy row) → the three owned fields set, legacy_warp_hostname "";
                                                     //   otp_idp_id only (a partial delete) → the three owned fields set, legacy_warp_hostname "";
                                                     //   warp_posture_id only → the three owned fields set, legacy_warp_hostname "aura-warp.a.it";
                                                     //   no resource ID → all "".
                                                     // Down, then up again, is clean.
// internal/remotetunnel, db_integration:
func TestSaveDesiredNeverMovesTheResourcesAccount(t *testing.T)     // resources in A; SaveDesired account B → Load: Desired.AccountID "B", Resources.AccountID "A"
func TestLegacyWARPHostnameSurvivesADomainChange(t *testing.T)      // warp ids set, legacy "aura-warp.a.it"; SaveDesired zone b.it → Load: LegacyWARPHostname still "aura-warp.a.it"
func TestAdvancePersistsOwnership(t *testing.T)                     // Advance with Resources.AccountID, OwnedZoneName, LegacyWARPHostname → Load returns them
```

- [ ] **Step 2: Run them to see them fail.**
- [ ] **Step 3: Write the migration.**
  - Add `resource_account_id`, `owned_zone_name` and `legacy_warp_hostname`, each
    `text NOT NULL DEFAULT ''`.
  - Backfill only where any resource ID is set. The columns are the same ten that `SaveDesired`'s
    `intent` lists: `zone_id`, `tunnel_id`, `public_dns_id`, `warp_dns_id`, `otp_idp_id`,
    `public_app_id`, `public_policy_id`, `warp_app_id`, `warp_policy_id`, `warp_posture_id`.
    - `resource_account_id = account_id`;
    - `owned_zone_name = zone_name`;
    - `owned_hostname = public_label||'.'||zone_name` where it is still `''`.
  - Backfill `legacy_warp_hostname = warp_label||'.'||zone_name` where `warp_dns_id`, `warp_app_id`,
    `warp_policy_id` or `warp_posture_id` is set.
  - The down file drops the three columns.
- [ ] **Step 4: Queries and store.** `Advance` gains the three columns, and `SaveDesired` is unchanged.
  `stateFromRow` reads them, and `Abandon` clears them with the resource IDs. Run `make sqlc`.
- [ ] **Step 5: Run** the tests until they pass, plus the package and `internal/db` tiers, `-race` and
  `make deadcode`. Expected: PASS.
- [ ] **Step 6: Commit** `feat(remote-access): resources remember the account and address they belong to`.

### Task 5: Change the address at any step

**Files:**
- Create: `internal/remotetunnel/readdress.go`
- Modify: `internal/remotetunnel/delete.go`, `reconciler.go`, `steps_zone_tunnel.go` (record the owned set before the first resource; `account`)
- Test: `internal/remotetunnel/readdress_test.go`, `delete_test.go`, `readdress_integration_test.go` (`db_integration`)

**Interfaces:**
- Consumes: Task 4's persisted ownership fields.
- Produces:
  - `func addressChanged(s State) bool`. It is true when any resource ID exists and either
    `Resources.AccountID` differs from `Desired.AccountID`, or `OwnedHostname` /
    `OwnedZoneName` differs from the desired hostname/zone. An empty owned set with no resources is
    never a change.
  - `func (r *Reconciler) teardown(ctx context.Context, s *State) error`.

- [ ] **Step 1: Write the failing tests**

```go
func TestDomainChangeTearsDownTheOldSetThenProvisionsTheNew(t *testing.T) // healthy on a.it; SaveDesired zone b.it → Reconcile: connector projected off before any delete, old tunnel/app/policy/CNAME gone from fake, ZoneID of b.it, new CNAME aura.b.it, AcceptedHostname "", Phase connecting
func TestLabelChangeKeepsThePendingZone(t *testing.T)                     // zone pending (fake status "pending"), label aura→casa → ZoneID unchanged, Phase waiting_nameservers, no CreateZone call
func TestAccountChangeDeletesInTheOldAccount(t *testing.T)                // token sees both accounts (account-scoped fake zones and OTP providers); Desired.AccountID B → deletions hit account A, ZoneID and OTPProviderID detached (not deleted), provisioning hits B with B's zone and OTP provider
func TestTeardownWaitsForTheConnectorToDrain(t *testing.T)                // fake DeleteTunnel refuses while connected → Reason cloudflare_unreachable, retried; once the fake reports no connections the teardown finishes and provisions
func TestRestartDuringTeardownKeepsTheConnectorOff(t *testing.T)          // teardown interrupted after projecting off → the stored tunnel credential is empty, so `rebuild` has nothing to project; a new reconciler (h.newReconciler()) finishes the teardown and projects nothing for the retiring tunnel
func TestTeardownLeftHalfwayResumes(t *testing.T)                         // fake DeleteDNS fails once retryable → Reason cloudflare_unreachable, next Reconcile finishes the teardown and provisions
func TestPendingZoneWithoutDNSIsNotAnAddressChange(t *testing.T)          // tunnel created, no public DNS yet, owned set recorded → no edit → addressChanged false, no teardown
func TestDeleteStillDetachesZoneAndOTP(t *testing.T)                      // existing Delete behaviour after the split: zone and OTP never deleted, Phase disabled
// db_integration, the real store:
func TestAccountChangeAgainstTheRealStore(t *testing.T)                   // resources in A, SaveDesired account B, reload, Reconcile with the fake cloud → deletions in A
```

- [ ] **Step 2: Run them to see them fail.**
- [ ] **Step 3: Implement.**
  - **The owned set.** The zone/tunnel step records `Resources.AccountID`, `OwnedZoneName` and
    `OwnedHostname` (persisted through `Advance`) before the first resource is created, not at
    `publish`.
  - **The split.**
    - `teardown` = clear the stored tunnel credential (`TunnelCredentials`), so that `rebuild` cannot
      restart the retiring tunnel after a reboot; project the connector off; `retireWARP` (ruling R5);
      delete the owned resources and clear their IDs and `TunnelName`; then clear the owned set.
    - On an account change, `teardown` also detaches `ZoneID` and `OTPProviderID` after cleanup.
    - `deleteResources` = projection reset + `teardown` + detach the zone and the OTP provider +
      `disabled`.
  - **The tunnel deletion.** A tunnel deletion that Cloudflare refuses while connections remain is a
    retryable failure.
  - **Ownership.** The checks use `Resources.AccountID`, `OwnedZoneName` and `OwnedHostname`, never
    `Desired`. The old `Resources.AccountID != Desired.AccountID` guard is replaced by
    `addressChanged`.
  - **The zone.** `ZoneID` survives only when both the account and the zone name are unchanged.
  - **The order.** In `reconcile`, after `retireWARP`: `if addressChanged(*s) { teardown }`, then the
    existing steps. A row that is `settled` (Task 2) but has `addressChanged` is not skipped.
- [ ] **Step 4: Run the package's tests, with `-race` and the `db_integration` tag** for
  `delete_retry_integration_test.go` and the new integration test. Expected: PASS.
- [ ] **Step 5: Commit** `feat(remote-access): change domain, label or account without a dead end`.

### Task 6: A domain active in another Cloudflare account is refused

**Files:**
- Modify: `internal/remotetunnel/steps_zone_tunnel.go`, `reconciler.go` (`WithNameServers`), `cmd/aura/serve_remote_access.go`
- Test: `internal/remotetunnel/zone_elsewhere_test.go`

**Interfaces:**
- Produces:
  - `type NameServers interface { LookupNS(ctx context.Context, name string) ([]*net.NS, error) }`;
  - `func WithNameServers(n NameServers) Option`.

- [ ] **Step 1: Write the failing tests**

```go
func TestZoneActiveElsewhereIsRefusedBeforeCreation(t *testing.T) // ListZones empty, LookupNS → ["jo.ns.cloudflare.com.","yahir.ns.cloudflare.com."] → Reason domain_elsewhere, Phase error, CreateZone never called
func TestZoneOnAnotherProviderIsCreated(t *testing.T)            // LookupNS → ["dns.register.it."] → CreateZone called, Phase waiting_nameservers
func TestZoneLookupFailureCreatesTheZone(t *testing.T)           // LookupNS → &net.DNSError{IsNotFound:true} → CreateZone called
func TestZoneAlreadyInThisAccountSkipsTheLookup(t *testing.T)    // ListZones returns it → LookupNS never called
```

- [ ] **Step 2: Run them to see them fail.**
- [ ] **Step 3: Implement** in `zone()` before `CreateZone`. The match is a case-insensitive
  `strings.HasSuffix(strings.TrimSuffix(host, "."), ".ns.cloudflare.com")`. Wire
  `WithNameServers(net.DefaultResolver)` in `engine`.
- [ ] **Step 4: Run** the package's tests and `-race`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(remote-access): refuse a domain that is active in another Cloudflare account`.

### Task 7: A new token that cannot see the old resources never blocks

**Files:**
- Modify: `cmd/aura/serve_remote_access.go` (`Configure`, `rebuild`), `internal/remotetunnel/reconciler.go`, `internal/agui/remote_access_api.go`, `internal/agui/idempotency_http.go`, `cmd/aura/serve_remote_access_wiring.go`
- Test: `cmd/aura/serve_remote_access_test.go`, `internal/remotetunnel/abandon_projection_test.go`, `internal/agui/remote_access_api_test.go`, `internal/agui/mutation_coverage_test.go`

**Interfaces:**
- Consumes:
  - `(*Store).Abandon`, `(*Store).DismissAbandoned` (Task 1);
  - the ownership fields that `Abandon` clears (Task 4).
- Produces:
  - `func abandonedOf(s remotetunnel.State) []remotetunnel.AbandonedResource`, with kinds `tunnel`,
    `dns`, `access_app`, `access_policy` and `posture`, and names as Cloudflare shows them;
  - the action `dismiss-abandoned` on `POST /api/settings/remote-access/abandoned/dismiss`;
  - `func tunnelOfCredential(token string) (string, error)`, the tunnel ID a stored credential belongs
    to.

- [ ] **Step 1: Write the failing tests**

```go
func TestConfigureWithATokenBlindToTheOldAccountAbandonsAndSaves(t *testing.T) // state with resources in account A; new token lists only B → Configure returns nil, state resources and ownership cleared, Abandoned lists tunnel aura-<uuid>, dns aura.a.it, access_app aura-<uuid>-public, access_policy aura-<uuid>-public-members; no Cloudflare delete call made
func TestAbandonClearsTheCredentialInTheSameCommit(t *testing.T)               // same setup → CLOUDFLARE_TUNNEL_TOKEN is "" after the commit; a failing commit leaves the secret, the row and the list as they were
func TestAbandonWhileTheZoneIsPending(t *testing.T)                            // tunnel created, phase waiting_nameservers, blind token → abandoned lists the tunnel
func TestSuccessiveTokenReplacementsKeepTheList(t *testing.T)                  // a posture entry from retirement, then an abandon, then another → all entries present until dismiss
func TestRestartRefusesACredentialForAnotherTunnel(t *testing.T)               // stored credential for tunnel X, row tunnel_id Y (a restart between tunnel creation and credential replacement) → rebuild projects off, logs the mismatch, returns nil
func TestConfigureWithATokenThatSeesTheOldAccountAbandonsNothing(t *testing.T)
// internal/remotetunnel:
func TestPassWithoutATunnelProjectsOffBeforeAnythingElse(t *testing.T)         // no tunnel_id, projection previously on, invalid domain → the off Apply happened, then domain_invalid
func TestLostProjectionIsRepairedWhileTheZoneIsPending(t *testing.T)           // abandon committed, projection never applied, new zone pending → the pass ends waiting_nameservers with the projection off
func TestFailedOffProjectionStopsTheProvisioning(t *testing.T)                 // Apply fails → no zone, tunnel or Access call in this pass
func TestDismissAbandonedRoute(t *testing.T)                                  // POST …/abandoned/dismiss as admin → 202, Abandoned empty; as member → 403
```

- [ ] **Step 2: Run them to see them fail.**
- [ ] **Step 3: Implement.**
  - **One transaction.** The abandon, the cleared `CLOUDFLARE_TUNNEL_TOKEN`, the secret upsert and
    `SaveDesired` run in the same transaction, through the `commit` closure. A failure leaves none of
    them.
  - **The connector.** `reconcile` projects off when `Resources.TunnelID == ""`, right after its
    configuration check and before `validateDesired`, and returns the error if that fails. The commit
    already wakes the pass, so `Configure` projects nothing itself.
  - **The credential check.** `rebuild` projects a credential only when `tunnelOfCredential` equals
    the row's `tunnel_id`. Read Cloudflare's documentation (or cloudflared's source) for the tunnel
    token's format before decoding it, and cite it in the comment. A credential that cannot be decoded
    is refused like a mismatch.
  - **The route.** Register it in `RemoteAccessRoutes`, `idempotency_http.go` and the
    mutation-coverage test.
- [ ] **Step 4: Run** `cmd/aura`, `internal/remotetunnel` and `internal/agui` tests, with `-race`.
  Expected: PASS.
- [ ] **Step 5: Commit** `feat(remote-access): list what a replaced token can no longer reach`.

### Task 8: The API speaks reasons and steps

**Files:**
- Modify: `internal/agui/remote_access_api.go`, `cmd/aura/serve_remote_access.go`, `internal/remotetunnel/acceptance.go`
- Test: `internal/agui/remote_access_api_test.go`, `cmd/aura/serve_remote_access_test.go`, `cmd/aura/serve_remote_access_actions_test.go`, `internal/remotetunnel/acceptance_test.go`

**Interfaces:**
- Produces the JSON the cockpit reads:
  - `RemoteAccessStatus`: `enabled`, `phase`, `public_hostname`, `api_token_set`, `account_id`,
    `account_name`, `zone_name`, `public_label`, `nameservers`, `reason`, `accepted` (bool),
    `reachable` (bool, `ObservedHealthy`), `last_checked_at`, `abandoned` (array), `generation`;
  - removed: `warp_hostname`, `last_error`, `connector`, `tunnel_token_set` and
    `acceptance_required`;
  - error bodies: `{"error": "<fixed sentence>", "reason": "<code>"}`.
- `RemoteAccessConfiguration` loses `warp_label`. A body that still sends it is rejected by
  `strictDecodeJSON`, which is correct.
- **The first step's save (RW-004).** `configure` with a token, an account, `enabled: false` and an
  empty `zone_name` and `public_label`:
  - verifies the token and that the account is visible;
  - stores both and answers 202;
  - calls no zone API.

  The `prerequisite_required` answer goes. On a configured install, the same call carries the current
  address and `enabled` (RW-011).
- `func remotetunnel.PublicHostname(d Desired) string` is exported. `cmd/aura` uses it in place of its
  two concatenations (`serve_remote_access.go` lines 127 and 180 at `ddeadc01f`).

- [ ] **Step 1: Write the failing tests**

```go
func TestStatusCarriesReasonAcceptanceAndAccountName(t *testing.T)
func TestVerifyWithNoAccountsAnswersNoAccounts(t *testing.T)          // ListAccounts → [] → 400 {"reason":"no_accounts"}
func TestConfigureUnknownAccountAnswersAccountNotVisible(t *testing.T) // 400 {"reason":"account_not_visible"}
func TestStatusNeverReturnsUpstreamText(t *testing.T)                  // fake error "secret-abc" → body contains no "secret-abc"
func TestConfigureWithoutADomainSavesTheTokenAndAccount(t *testing.T)  // real handler + controller: {api_token, account_id, enabled:false, zone_name:"", public_label:""} → 202; secret stored; Desired.AccountID set, Enabled false; no zone call on the fake
func TestReloadBetweenCloudflareAndDomainKeepsTheToken(t *testing.T)   // the save above, then a new controller over the same store and secrets → status api_token_set true, account_id set, zone_name ""
func TestTokenReplacementKeepsTheAddressAndTheAcceptance(t *testing.T)  // real handler + controller: accepted, enabled install; configure with a new token, the same account and the status's address and enabled → zone, label, Enabled and AcceptedHostname unchanged, status accepted true
func TestStatusDuringAnOutageNeverAsksForThePhoneTest(t *testing.T)     // accepted row, degraded + tunnel_down → accepted true, reachable false, body has no acceptance_required
func TestAcceptExternalAcceptsADegradedTunnel(t *testing.T)             // tunnel status "degraded" → accepted, like tunnelServes (Task 2 ledger minor)
```

- [ ] **Step 2: Run them to see them fail.**
- [ ] **Step 3: Implement.**
  - `Verify` returns `remotetunnel.ErrNoAccounts` on an empty list.
  - `Configure` returns `ErrAccountNotVisible`.
  - `writeRemoteAccessError` adds `reason: ReasonOf(err)` and drops the `ErrAddressLocked` case.
  - The account cache is filled from `Verify` and from one `ListAccounts` per token.
  - `AcceptExternal` asks `tunnelServes` instead of comparing the status with `"healthy"`.
- [ ] **Step 4: Run** the tests, with `-race`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(remote-access): status and errors name their reason`.

### Task 9: The cockpit wizard

**Files:**
- Create:
  - `web/src/components/StepStrip.tsx` and `.test.tsx`;
  - `web/src/settings/remoteAccess/remoteAccessSteps.ts` and `.test.ts`;
  - `CloudflareStep.tsx`, `DomainStep.tsx`, `ConnectionStep.tsx`, `PhoneStep.tsx`, `ReasonNotice.tsx`
    and their `__tests__`.
- Modify: `web/src/onboarding/OnboardingStepper.tsx`, `RemoteAccessWizard.tsx`, `RemoteAccessPanel.tsx`, `remoteAccessApi.ts`, `useRemoteAccess.ts` (dismiss mutation), `web/src/i18n/resources.remoteAccess.ts`
- Delete: `RemoteAccessStatus.tsx`, `HostnamesStep.tsx`, `VerifyStep.tsx`, `ZoneStep.tsx`, `TokenStep.tsx` and their tests

**Interfaces:**
- Consumes: Task 8's JSON, and its first-step save.
- Produces:
  - `StepStrip({ steps: readonly { key: string; label: string; state: 'done' | 'active' | 'upcoming' | 'failed' }[], onSelect?: (key: string) => void, label: string })`.
    A `done` or `failed` step is a `<button>` when `onSelect` is set. The `active` step carries
    `aria-current="step"`.
  - `type StepKey = 'cloudflare' | 'domain' | 'connection' | 'phone'`.
  - `currentStep(status): StepKey`, `stepStates(status): Record<StepKey, StepState>` and
    `reasonStep(reason): StepKey`, all in `remoteAccessSteps.ts`.

- [ ] **Step 1: Write the failing unit tests**

```ts
// remoteAccessSteps.test.ts — currentStep, as a table:
//   - no token → cloudflare
//   - token, no zone → domain
//   - waiting_nameservers → domain
//   - provisioning → connection
//   - connecting, not accepted → phone
//   - healthy → phone, with every step 'done'
//   - degraded + tunnel_down → phone, with phone 'failed'
//   - error + domain_elsewhere → domain 'failed'
//   - error + no_accounts → cloudflare 'failed'
// StepStrip.test.tsx:
//   - a done step is a button; clicking it calls onSelect('domain');
//   - the active step has aria-current="step";
//   - at 390px the text "Passo 2 di 4 · Dominio" is rendered and no list is.
// The same file covers OnboardingStepper: its existing tests stay green unchanged.
// RemoteAccessWizard.test.tsx:
//   - healthy status → clicking 'Dominio' opens DomainStep holding zone_name;
//     'Indietro' there opens CloudflareStep.
// DomainStep.test.tsx:
//   - empty domain → Avanti is disabled and "Scrivi il dominio per continuare" is visible;
//   - 409 → the refresh message, with the typed domain kept (Review Focus 5);
//   - waiting_nameservers → both nameservers are listed in mono.
// PhoneStep.test.tsx:
//   - healthy → 'Raggiungibile da fuori casa', the open link, 'Disattiva' and 'Elimina…';
//   - tunnel_down → 'Non raggiungibile' with no phone-test button;
//   - not accepted and not on the public hostname → the open link and the text that the
//     confirmation is only available there.
// CloudflareStep.test.tsx:
//   - abandoned resources → listed by kind and name, with 'Fatto' calling dismiss;
//   - the account select shows account_name;
//   - first setup: Avanti sends configure with the token, the account, enabled false and an
//     empty domain, then the wizard opens Dominio;
//   - configured install: a new token is sent with the status's zone_name, public_label and
//     enabled true, and the wizard stays green.
// ReasonNotice.test.tsx:
//   - every Reason has an IT and an EN sentence and action (iterate the TS union);
//   - no string contains a banned word.
```

- [ ] **Step 2: Run them to see them fail.** From `web/`: `node node_modules/vitest/vitest.mjs run src/components/StepStrip.test.tsx src/settings/remoteAccess src/onboarding`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - **`StepStrip` and the onboarding.** Move `OnboardingStepper`'s markup into `StepStrip` and make
    `OnboardingStepper` call it with `onSelect` undefined.
  - **The wizard.** It keeps `open: StepKey | null` local state: null means `currentStep(status)`.
  - **Saving.** Each step card saves through the existing `configureRemoteAccess` and refetches. The
    Cloudflare step sends the status's current `zone_name`, `public_label` and `enabled`; the Domain
    step sends `enabled: true` with the zone and label.
  - **Copy.** Write the new i18n keys for both locales and delete every key no component reads.
  - **The token link.** Read Cloudflare's documentation on creating an API token from a pre-filled
    link before writing `CloudflareStep`'s link, and follow the decision above.
  - **Look.** Follow the Global Constraints' look rules; the approved mockup is in the spec's
    decision 4.
- [ ] **Step 4: Run** the remote access, components and onboarding tests, then the whole suite. In
  WSL run lint, typecheck, `prettier --check` on touched files and knip. Expected: all green.
- [ ] **Step 5: Commit** `feat(cockpit): a remote access wizard you can walk back`.

### Task 10: The Playwright suites follow

**Files:**
- Modify: `web/e2e/remote-access.spec.ts`, `web/e2e/remote-access-live.spec.ts`, `web/e2e/support/remoteAccessLive.ts`, `scripts/cloudflare_tunnel_live_e2e.sh`

- [ ] **Step 1: Rewrite the mocked suite.** It stays on the existing route-mocking pattern, with these
  tests:
  - `walks forward, back to Dominio, changes the domain and returns to green`;
  - `a no_accounts verify shows its sentence in Cloudflare`;
  - `the phone step accepts only from the public hostname and never sends a client tunnel marker`
    (the existing assertion, kept);
  - `390px Italian layout fits and every control is 44px`.
- [ ] **Step 2: Run** it against the dev server (`npx playwright test e2e/remote-access.spec.ts` from
  WSL with the bundled Chromium, per the memory recipe). Expected: PASS.
- [ ] **Step 3: Update the live suite (RW-010).**
  - Remove WARP: the hostname, the label, the Gateway journey and `team`.
  - `scripts/cloudflare_tunnel_live_e2e.sh` drops its `CLOUDFLARE_TEAM_NAME` prerequisite.
  - Rewrite every assertion that expects a new phone confirmation after a token replacement or a
    refresh. Today these are the *Confirm external access* clicks after the rotation (line 354 at
    `ddeadc01f`). After either one, the phone step stays accepted.
  - The existing restart loop over `aura` and `aura-cloudflared` also asserts that the status
    returns to `healthy` with `accepted: true`, with no confirmation click.
  - Keep every other assertion. The suite is not run here: it runs in Task 11.
- [ ] **Step 4: Commit** `test(e2e): the remote access suites follow the new wizard`.

### Task 11: Gates, push, the lab VM with `chetto1983.it`

- [ ] **Step 1: Gates.** In WSL: `make quality`, then `bash scripts/coverage_docker.sh`. Expected: green,
  with the policy floors held.
- [ ] **Step 2: Push** with the WSL recipe, then every workflow green on the pushed SHA.
- [ ] **Step 3: The VM's update.** After the operator's consent through the updater, the VM runs the
  pushed SHA (`m-rev.sh`).
- [ ] **Step 4: The operator's run, which needs `chetto1983.it` to resolve.**
  - The operator creates the API token in their own account.
  - They walk the wizard from the cockpit, changing the nameservers at register.it.
  - From the phone on mobile data, they open the hostname, receive the PIN, log in to Aura and
    stream a chat reply.
  - The attended live suite (`scripts/cloudflare_tunnel_live_e2e.sh`, Task 10) runs with the operator
    at its checkpoints.

  Record spec acceptance items 1–5 in
  `docs/superpowers/verification/<run date>-remote-access-wizard-lab-vm.md`, with what it does not
  show:
  - 1: the first setup;
  - 2: walk back and change the domain while pending;
  - 3: a token without Account Settings Read, and `piermarsync.it` refused as `domain_elsewhere`;
  - 4: the session read back from Cloudflare's API;
  - 5: the WARP retirement, from the fake or from Andrea's appliance after its update.
- [ ] **Step 5: Records.**
  - The Status lines of this plan and of `docs/superpowers/plans/2026-09-20-cloudflare-tunnel.md`.
  - PRD §12, or wherever remote access is recorded (grep `cloudflare` in `prd.md`): the measured
    behaviour with its date and evidence.
  - The audit memory entry.
- [ ] **Step 6: Commit** `docs(remote-access): record the wizard on the lab VM`.
