---
phase: 36-multi-user-identity-isolation-authula-cutover
reviewed: 2026-07-06T00:00:00Z
depth: deep
focus: multi-user identity isolation correctness (owner-scoping, fail-closed posture, RLS, documents/Neo4j, object-store, deletion lifecycle)
git_range: e8bbe1f8..ab7e03a9
files_reviewed: 41
files_reviewed_list:
  - internal/db/tx.go
  - internal/db/migrations/0032_owner_rls.up.sql
  - internal/db/migrations/0027_paused_states_identity.up.sql
  - internal/db/migrations/0029_identity_soft_delete.up.sql
  - internal/db/migrations/0026_local_admin_caps.up.sql
  - internal/db/queries/conversations.sql
  - internal/db/queries/paused_states.sql
  - internal/db/queries/identity.sql
  - internal/db/sqlc/identity.sql.go
  - internal/conversations/store_identity.go
  - internal/askuser/store_identity.go
  - internal/agui/auth.go
  - internal/agui/conversations_api.go
  - internal/agui/conversations_branch_api.go
  - internal/agui/approvals_api.go
  - internal/agui/audit_api.go
  - internal/agui/audit_store.go
  - internal/agui/settings_api.go
  - internal/agui/onboarding_provision.go
  - internal/agui/onboarding_provision_resources.go
  - internal/agui/onboarding_session.go
  - internal/agui/deprovision.go
  - internal/agui/saga_journal.go
  - internal/documents/search.go
  - internal/documents/retrieve.go
  - internal/documents/graphrag.go
  - internal/documents/indexer.go
  - internal/documents/backfill.go
  - internal/documents/service.go
  - internal/documents/types.go
  - internal/objectstore/identity_store.go
  - internal/objectstore/garageadmin/client.go
  - internal/runner/runner_delete.go
  - internal/runner/runner_session.go
  - internal/runner/runner_conversation.go
  - internal/agent/tools/shell_bg_owner.go
  - internal/agent/tools/document_search.go
  - internal/channels/telegram/bot_dispatch_auth.go
  - internal/channels/telegram/bot_dispatch_turn.go
  - internal/mcp/managed_config_identity.go
  - internal/skills/identity_root.go
  - internal/profile/store.go
  - internal/webauth/identity_link.go
  - internal/webauth/session_validate.go
  - internal/cron/handlers/identity_purge.go
  - cmd/aura/serve_webui.go
  - cmd/aura/serve_webui_musr.go
  - cmd/aura/serve_onboarding.go
  - cmd/aura/recovery.go
  - cmd/aura/chat.go
  - internal/config/config_knobs.go
findings:
  critical: 1
  high: 3
  medium: 2
  low: 3
  total: 9
status: issues_found
---

# Phase 36: Code Review Report — Multi-User Identity Isolation

**Reviewed:** 2026-07-06
**Depth:** deep (cross-file: auth boundary → stores → RLS → Neo4j → object-store → composition root)
**Focus:** identity isolation correctness (the entire point of the phase)
**Status:** issues_found (1 Critical, 3 High)

## Summary

The app-layer owner-scoping for the Postgres planes (conversations, approvals) is well-built and
consistent: every AG-UI handler routes owner-scoped reads/mutates through `*ForIdentity`, the SQL
predicates are correct (`WHERE id=$1 AND identity_id=$2`, no stray `OR`), `WithIdentityTx` is a
correct per-tx `SET LOCAL` RLS carrier, the six flag-gated Neo4j scoped queries are genuinely
fail-closed (`$identity <> "" AND EXISTS{…HAS_DOCUMENT…}`), the crypto (HKDF-domain-separated
AES-256-GCM), the path-traversal guard (`RootIdentityDir`/`ValidateIdentity`), the crypto-random
owner-bound background-job IDs, and the conversation-delete owner-gate-BEFORE-teardown ordering are
all sound. The branch-route hole called out in commit `9bb0a9c5` is closed.

However, the isolation posture has serious gaps in **what is actually reachable/wired in the
shipped daemon** versus what the tests exercise. The two most important: (1) the documents-plane
isolation is entirely gated behind a config flag that **defaults OFF**, so a second identity
provisioned with default config reads the operator's documents; and (2) the per-identity
object-store plane and the entire de-provisioning lifecycle are **constructed only in tests** — the
`cmd/aura` daemon never wires them, so the two-identity acceptance E2E passes while the running
binary enforces neither. Several fail-open-to-`local` paths (Telegram scoping, deactivation) do not
honor the mandated fail-closed posture.

---

## Critical Issues

### CR-01: Documents-plane isolation is default-OFF and decoupled from provisioning — a second identity reads the operator's documents

**File:** `internal/config/config_knobs.go:120`, `internal/documents/search.go:34,52`, `internal/documents/retrieve.go:60,111-122`, `internal/agent/tools/document_search.go:81-89`

**Issue:** The Neo4j documents plane is the ONLY isolation for uploaded documents (there is no RLS
on Neo4j). Its fail-closed scoping is selected entirely by `AURA_MUSR_ISOLATION`, which defaults to
`"false"` (`config_knobs.go:120`). When the flag is off, `Searcher.Search` / `Service.seedHits`
select the **unscoped** query variants (`sparseSearchQuery`, `vectorSeedQuery`, …) that return
chunks from *every* identity's documents. Nothing couples "a second identity now exists" to "the
flag must be on": onboarding/provisioning (`onboardingProvisionRoute`) is gated on
`identity.create`, not on the isolation flag, and provisioning succeeds with the flag off.

Unlike the conversation/approval planes (always owner-scoped via `*ForIdentity`, flag-independent),
the document plane provides **zero** isolation in the default configuration.

**Exploit:** Fresh deployment, default config (`AURA_MUSR_ISOLATION` unset ⇒ off). Admin creates
user B (a supported, UI-exposed action). B logs into the web cockpit and asks the agent to
"summarize my documents." `document_search` runs with B's identity, but with the flag off the
identity is ignored and the unscoped query returns operator A's (and every other identity's) indexed
chunks verbatim. Cross-identity document content disclosure with no exotic preconditions.

**Fix:** Make the coupling enforced in code rather than relying on the D-13 runbook ordering.
Options (pick one, defense-in-depth prefers both):
- Refuse to provision a 2nd non-`local` identity while `AURA_MUSR_ISOLATION` is off (fail the
  provision saga pre-validate with a clear "enable isolation before adding users" error), and/or
- Auto-enable scoped retrieval whenever more than one live identity exists, treating the flag only
  as an emergency *disable*, e.g.:
```go
// documents: derive the effective scoping mode, defaulting to fail-closed once multi-user.
func (s *Service) scopedMode(ctx context.Context) bool {
    return s.MUSRIsolation || s.multiIdentity // multiIdentity set when >1 live identity exists
}
```
At minimum, boot must log a loud WARN (or refuse serve) when `>1` identity exists and the flag is
off. Document-plane isolation must not be a silent, default-off opt-in.

---

## High Issues

### HI-01: Object-store isolation plane + de-provisioning lifecycle are wired only in tests — the daemon enforces neither

**File:** `cmd/aura/serve_onboarding.go:247-261` (deps built without `ObjectStore`/`Filesystem`/`Journal`), `internal/objectstore/identity_store.go` (`Resolve` never called by `cmd/aura`), `internal/agui/deprovision.go` (`NewDeprovisioner` never constructed in `cmd/aura`), `internal/cron/handlers/identity_purge.go` (`Purger` never wired)

**Issue:** Grepping the whole tree, `objectstore.NewIdentityStore`, `IdentityStore.Resolve(ctx)`,
`NewDeprovisioner`, and the `ObjectStore`/`Filesystem`/`Journal` onboarding ports are referenced
**only** from `*_test.go` files and the two-identity harness. `buildOnboardingService`
(`serve_onboarding.go:247-261`) sets `Capabilities/Extractor/Profiles/AuraLeg/Telegram/Recovery/
Authula/BotUsername` and leaves `deps.ObjectStore`, `deps.Filesystem`, and `deps.Journal` nil, so
`provisionResourceLegs` skips the Garage-bucket and per-identity-dir legs (they are nil-guarded).
No `cmd/aura` code constructs the per-identity credential resolver or calls `Resolve` on the asset
upload/download path.

Consequences in the shipped binary:
- **Object-store (D-08) is inert.** Provisioned identities get no bucket/key row. The asset data
  path continues to use the single shared S3 store/bucket for everyone. Per D-10's own reasoning
  (metadata-layer scoping is "bypassable"), relying on the shared bucket + catalog metadata is
  exactly the posture Phase 36 was meant to replace — so isolation of uploaded objects is not
  actually enforced.
- **De-provisioning (D-27) is dormant.** There is no route, CLI subcommand, or scheduled sweep that
  reaches `Deactivate`/`Purge`/`PurgeExpired`; `IdentityPurgeHandler.Purger` is never set. An
  identity cannot be disabled or purged.
- **False confidence from the acceptance gate.** `two_identity_e2e_test.go` exercises the resolver +
  Garage Admin API *directly*, not through the daemon, so the E2E is green while the daemon wires
  none of it.

**Fix:** Wire the resolver into the asset service (`Resolve(ctx)` → per-identity `S3Store`) and set
`deps.ObjectStore`, `deps.Filesystem`, `deps.Journal` in `buildOnboardingService`; construct the
`Deprovisioner` and register/seed `KindIdentityPurge` in the scheduler composition. If any of this
is intentionally deferred to a later cutover step, the phase's SUMMARY/acceptance must state plainly
that object-store isolation and de-provisioning are **not enforced by this build**, and the E2E must
drive the resolver through the daemon rather than sidestepping it.

### HI-02: Deactivation does not block re-login — the auth path never checks `deactivated_at`

**File:** `internal/db/queries/identity.sql:11-14` (`GetIdentityByID` / `GetIdentityByName` — no `deactivated_at` filter), `internal/agui/auth.go:223` (existence re-check), `internal/agui/deprovision.go:122-151` (`Deactivate` comment claims "blocking login")

**Issue:** `Deactivate` stamps `deactivated_at`, kills existing Authula sessions, and terminates
jobs, and its doc-comment claims it is "blocking login." But killing *existing* sessions does not
prevent re-authentication, and **nothing in the auth path inspects `deactivated_at`**:
`GetIdentityByID`/`GetIdentityByName` select the column but filter only on `id`/`name`
(`identity.sql:11-14`), the `RequireAuth` existence re-check (`auth.go:223`) treats any existing row
as valid, and the `agui.Identity` projection does not even carry the flag. A grep confirms
`deactivated_at` is read nowhere in `auth.go`, `session_validate.go`, or the identity store's
capability path. The Authula user is only removed at **Purge** (after the 7-day default grace
window), so during the grace window a deactivated user simply logs in again (email+password still
valid) → `ResolveIdentityID` → `GetIdentityByID` succeeds → full access restored, with all
capabilities intact (Deactivate does not revoke grants).

This is a fail-open authorization control: a revoked/soft-deleted principal retains authenticated
access for the whole grace window. (Currently latent because the deactivation trigger itself is
unwired — see HI-01 — but the auth-layer defect is real and will bite the moment HI-01 is fixed.)

**Fix:** Reject deactivated identities at the auth boundary. Surface `deactivated_at` on the
`identityChecker` projection and deny in `RequireAuth`:
```go
id, err := deps.Identities.GetIdentityByID(r.Context(), identityID)
if err != nil || id.Deactivated { // Deactivated == deactivated_at IS NOT NULL
    deps.redirectToLogin(w, r); return
}
```
Prefer this over adding `AND deactivated_at IS NULL` to `GetIdentityByID` directly, since the
deprovision saga's `ResolveTarget` and the admin roster legitimately need to see deactivated rows.

### HI-03: Telegram turn-scoping fails OPEN to `local` (admin) — gate keys on sender-id, scope keys on chat-id

**File:** `internal/channels/telegram/bot_dispatch_turn.go:124-134` (`scopeTurnToIdentity` returns ctx unchanged on any resolver miss), `:83` (`startTurn` applies it), `internal/channels/telegram/bot_dispatch.go:115,124,160` (`chatID := msg.Chat.ID`; gate on sender; `runTurn(chatID)`), `internal/channels/telegram/bot_dispatch_auth.go:44-55,70-76` (gate uses `telegramUserIDFromMessage` = `Sender.ID` first)

**Issue:** The reject-unlinked gate resolves the linked account by **sender** id
(`telegramUserIDFromMessage` → `msg.Sender.ID`), but `scopeTurnToIdentity` resolves by **chat** id
(`GetAccountByTelegramID(chatID)` where `chatID = msg.Chat.ID`). For a private DM these are equal,
but the code enforces no `msg.Chat.Type == ChatPrivate` invariant. When the two keys diverge (a
group chat: `Chat.ID` is the negative group id, `Sender.ID` is the member), the gate passes for a
linked sender while the chat-id lookup misses → `scopeTurnToIdentity` returns the ctx **unchanged**
(unscoped). Every downstream resolver then applies its `local` fallback (`sessionIdentity`,
`ownerFromContext`, `defaultConversationOwner`), so the turn runs in the seeded **`local`** identity
— which holds admin capabilities and owns the operator's data.

This is exactly the anti-pattern the phase set out to prevent: a channel caller whose identity fails
to resolve is silently upgraded to `local` instead of being denied. Impact if reached: a linked
non-admin user drives an agent turn in the operator/admin context — reading `local`'s conversation
threads, running tools/shell as `local`, and (with CR-01's flag off) searching all documents.

**Reachability:** requires the bot to be added to a group and a group message (reply-to-bot /
mention / command under Telegram privacy mode) to reach `runTurn`. Atypical for a personal-assistant
appliance, but the code neither enforces DM-only nor fails closed.

**Fix:** Make scoping fail closed and unify the key with the gate:
```go
func (t *Telegram) startTurn(...) {
    ctx, ok := t.scopeTurnToIdentityStrict(daemonCtx, chatID) // resolve by the SAME key the gate used
    if !ok { // unresolved ⇒ do NOT run as local; drop the turn
        slog.Warn("telegram: unscoped turn refused (no linked identity)", "chat", chatID)
        return
    }
    ...
}
```
Additionally reject non-private chats up front (`if msg.Chat.Type != tele.ChatPrivate { return }`)
and key the gate and the scope on the identical id.

---

## Medium Issues

### ME-01: `document_search` uses the raw ctx identity with no `local` fallback — CLI operator retrieval breaks after the isolation flip

**File:** `internal/agent/tools/document_search.go:81-89` (`IdentityID: identityctx.IdentityID(ctx)`), contrast `internal/agent/tools/shell_bg_owner.go:54` and `internal/runner/runner_session.go:38` (which map empty → `local` UUID), `internal/documents/backfill.go:17` (operator docs owned by the `local` UUID)

**Issue:** Every other plane maps an empty/no-principal ctx to the seeded `local` id (CLI path,
D-25). The documents tool instead threads the raw `identityctx.IdentityID(ctx)`, which is `""` on
the CLI (`aura chat` runs turns off a `context.Background()` with no principal stamped). With
`AURA_MUSR_ISOLATION` on, `Search`/`seedHits` short-circuit on empty identity and return nothing
(`search.go:34`, `retrieve.go:60`). But the operator's documents are owned by the `local` **UUID**
(`OperatorIdentity` in `backfill.go:17`), not by `""`. So after the flip, the operator's CLI
document search returns zero results — an availability regression and an inconsistency with the
`local`-fallback convention used everywhere else. (The direction is safe — fail closed, not a
leak — but it silently breaks the documented CLI-as-`local` path.)

**Fix:** Resolve the tool's identity through the same `local`-fallback used by the runner/tools,
e.g. thread the runner's resolved owner into the tool ctx, or:
```go
IdentityID: resolveOwnerIdentity(identityctx.IdentityID(ctx)), // "" → local UUID
```
so the operator's `local`-owned documents remain reachable from the CLI while web principals stay
scoped to themselves.

### ME-02: RLS is permissive-on-unset, so any future owner-scoped read that forgets `*ForIdentity` on a plain pool/`WithTx` silently leaks

**File:** `internal/db/migrations/0032_owner_rls.up.sql:14-49`, `internal/db/tx.go:41-77`

**Issue:** The 0032 policy is `NULLIF(current_setting('app.current_identity',true),'') IS NULL OR
identity_id = …` — i.e. **permissive when the GUC is unset**. This is deliberate and documented
(the runner turn-loop, CLI, Telegram, and the 403-vs-404 existence probe all read on the unset-var
pool), and today the AG-UI web handlers are disciplined: every owner-scoped read/mutate goes through
`GetForIdentity`/`ListForIdentity`/`GetByTokenForIdentity`, which use `WithIdentityTx` (GUC set).
So the app-layer filter is the real defense and the review found no current web handler bypassing
it. The residual risk: the "kernel backstop that returns 0 foreign rows even if the filter is
dropped" (per the `store_identity.go` doc-comment) is **only active when the GUC is set**. A future
handler that reads an owner-scoped table via the plain pool or `db.WithTx` (GUC unset) gets no RLS
protection and leaks. The backstop narrative overstates the guarantee.

**Fix:** No change required now, but (a) document precisely that RLS backstops *only* the
`WithIdentityTx` paths, and (b) plan the tightening to fail-closed-on-unset (with an explicit
allowlist for the genuine no-principal writers) once all write paths set the GUC, per the file's own
TODO. Consider a lint/test asserting no owner-scoped table is queried on the bare pool outside the
known set.

---

## Low / Informational

### LO-01: D-06 mutate returns 403 vs 404 and thereby confirms existence of a foreign conversation

**File:** `internal/agui/conversations_api.go:103-109` (`writeForeignMutateStatus`)

**Issue:** A rename/archive/delete on a foreign-but-existing conversation returns 403 (vs 404 for
absent), by design (D-06). This is an existence oracle across identities. Mitigated to negligible by
unguessable v7 UUID conversation ids (enumeration infeasible). Accepted design tradeoff; noted for
completeness.

**Fix:** None required. If existence-hiding is ever prioritized over the D-06 "known-foreign =
403" decision, return 404 for both.

### LO-02: `IdentityStore.Resolve` and `scopedIdentityID`/`handleMe` map empty identity → shared/`local`

**File:** `internal/objectstore/identity_store.go:81-108,149-153`, `internal/agui/auth.go:330-335`, `internal/agui/audit_api.go:86-89`

**Issue:** Empty identity resolves to the shared bucket / `local` operator. This is safe **only**
because, on the authenticated web path, `RequireAuth`+`session_validate.go` guarantee a non-empty,
existing principal (verified: `Validate` never returns `("", true)`; `RequireAuth` re-checks
existence). The fallbacks are correctly confined to the loopback-dev/no-secret and CLI paths. Listed
so the fixer keeps the invariant intact: any change that lets a web/channel request reach these with
an empty identity converts the fallback into a `local`/shared-bucket impersonation (cf. HI-03).

**Fix:** None now; add a regression test asserting an authenticated request with a blank principal is
rejected rather than scoped to `local`.

### LO-03: `documentUpsertQuery` MERGEs `(:User {identifier:""})` when ingest identity is empty, orphaning the doc from everyone post-flip

**File:** `internal/documents/indexer.go:36-41,248` , `internal/documents/backfill.go:160-167`

**Issue:** An ingest with an empty `IdentityID` (a CLI/legacy path that does not thread the `local`
UUID) attaches the document to `:User{identifier:""}`. Post-flip scoped retrieval fails closed on
`$identity <> ""`, so no one — including the operator — can retrieve it; and the orphan backfill
(`WHERE NOT EXISTS { (:User)-[:HAS_DOCUMENT]->(d) }`) will not re-home it because it already has an
(empty) owner edge. Availability, not a leak. Overlaps ME-01's root cause (missing `local`
fallback on the documents path).

**Fix:** Normalize an empty ingest identity to the `local` UUID before the `MERGE (:User)`, matching
the retrieval/backfill owner convention.

---

## Verified sound (do not "fix")

These isolation-critical mechanisms were traced and found correct — flagging so the fixer avoids
churn:

- **Owner-scoped SQL** (`conversations.sql`, `paused_states.sql`): `WHERE id=$1 AND identity_id=$2`
  / `WHERE identity_id=$1 AND resumed_at IS NULL` — correct, no stray `OR`.
- **`WithIdentityTx`** (`tx.go:73`): `set_config(..., is_local=>true)` correctly scopes the GUC to
  the tx (no pooled-connection leak); empty id → `''` → policy's no-context branch.
- **Six Neo4j scoped queries** (`search.go`, `retrieve.go`, `graphrag.go`): all carry the
  unconditional `$identity <> "" AND EXISTS { (:User {identifier:$identity})-[:HAS_DOCUMENT]->
  (:Document {id: node.document_id}) }` with no `= "" OR` fallthrough — genuinely fail-closed on
  empty/foreign identity; unscoped variants reachable only with the flag off.
- **Conversation-delete lifecycle** (`runner_delete.go:38-74`): owner gate (`GetForIdentity`) runs
  BEFORE cancel/expire/evict/terminate; a foreign id short-circuits `(0,nil)` with no teardown of
  the victim. `(identity,session)` composite keying (`runner_session.go`) prevents cross-identity
  cancel.
- **Background jobs** (`shell_bg_owner.go`): 128-bit crypto-random ids, owner-bound `(identity,
  session)`, poll/kill gated on owner-match or admin cap; `Caps==nil ⇒ owner-only` fail-closed;
  poll = 404 (hides), kill = 403 (known-foreign).
- **Object-store crypto** (`identity_store.go`): HKDF-SHA256 domain-separated KEK, AES-256-GCM,
  random nonce prepended, fail-closed on malformed secret / missing row (`ErrNoRows` not special-
  cased to shared); secrets never logged.
- **Path-traversal guard** (`profile/store.go:208-243`): charset regex + `..`/slash reject +
  `filepath.Rel` containment assertion; reused by mcp/skills/garageadmin rooting.
- **Capability route mounts** (`serve_webui.go`, `serve_webui_musr.go`): settings PUT/DELETE and all
  `/api/admin/*` behind `governance.write`; branch edit/select and approvals-resolve behind
  `agent.run`; whole origin wrapped in `RequireAuth`; the SPA hide is cosmetic, server gate is the
  boundary.
- **Break-glass recover** (`cmd/aura/recovery.go`): CLI/host-only, reuses the 0023 short-lived
  hashed-token infra, prints plaintext to stdout only (never logged); no standing server route.
- **Authula session validator** (`session_validate.go`): fails closed on every miss/expiry;
  `auraID==""` → `ErrNoSession`; never returns `("", true)`.
- **`defaultConversationOwner`** (`runner_conversation.go:55-68`): validates the ctx principal via
  `GetIdentityByID` (loud fail on stale/bogus), replacing the old "first user" mis-attribution.

---

_Reviewed: 2026-07-06_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
