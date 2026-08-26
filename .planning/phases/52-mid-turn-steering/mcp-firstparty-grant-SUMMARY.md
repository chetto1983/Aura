---
phase: 52
plan: mcp-firstparty-grant
subsystem: mcp-oauth
tags: [mcp, oauth, authula, arcadedb, readiness, security]
requires:
  - a05c92cfe (Amendment #147 — built-in MCPs isolated by OAuth subject)
provides:
  - webauth.OAuthServer.IssueFirstPartyToken — self-issuance for Aura-owned MCP resources
  - manager.FirstPartyRecipe — the "is this a sidecar Aura ships" predicate
  - mcpoauth.Store.OwnersOf — every grant owner, not just a unique one
  - mcp.AuraIssuedGrant — the single writer of a self-issued grant's client blob
  - firstPartyGrantKeeper — boot reconciliation + pre-expiry renewal
affects:
  - cmd/aura serve boot ordering (grants exist before StartReconnect resolves owners)
  - identity creation (grants provisioned beside the ArcadeDB tenant)
tech-stack:
  added: []
  patterns: [boot-reconcile-then-tick keeper (gateway.Reconciler shape), narrow store interfaces for daemon-free tests]
key-files:
  created:
    - internal/webauth/mcp_oauth_first_party.go
    - internal/webauth/mcp_oauth_first_party_test.go
    - internal/mcp/manager/first_party.go
    - internal/mcp/manager/first_party_test.go
    - internal/mcp/oauth_contract_test.go
    - cmd/aura/mcp_first_party_grants.go
    - cmd/aura/mcp_first_party_grants_test.go
  modified:
    - internal/webauth/mcp_oauth_server.go
    - internal/webauth/mcp_oauth_handlers.go
    - internal/mcp/oauth_contract.go
    - internal/mcpoauth/store.go
    - internal/mcpoauth/store_integration_test.go
    - cmd/aura/mcp_live_mount.go
    - cmd/aura/mcp_live_mount_test.go
    - cmd/aura/serve.go
    - cmd/aura/serve_drain.go
    - cmd/aura/serve_bootstrap.go
    - prd.md (Amendment #149)
decisions:
  - Self-issuance is gated twice — manager.FirstPartyRecipe (source AND catalog URL) at the caller, allowedMCPResource at the issuer
  - A self-issued grant carries NO refresh token (Authula FKs refresh_tokens to sessions; measured 23503)
  - Renewal is a keeper loop, not OAuth refresh (Aura's refresh leg rotates against a login session a daemon has none of)
  - Boot reconciliation runs synchronously before liveMCPMount.StartReconnect
  - First-party mounts tolerate several grant owners; third-party mounts keep the ErrAmbiguousOwner refusal
metrics:
  duration: ~3h
  completed: 2026-08-26
---

# Phase 52 — First-party MCP grant self-issuance Summary

Aura now mints, stores and renews the OAuth grants its own three MCP sidecars need, so calendar, memory and whatsapp mount at boot without a human at a consent screen — closing a live regression that had left the container permanently `unhealthy`.

## What was broken, measured

`a05c92cfe` (Amendment #147) made Calendar, WhatsApp and Memory OAuth resource servers isolated by token subject, but left `AuthorizationHandler` — a browser flow — as the only issuance path. Nothing ever minted a token for Aura itself, so on every boot:

1. `cmd/aura/main.go:356` deferred all five OAuth servers.
2. `liveMCPMount.StartReconnect` resolved each server's owner via `Store.OwnerOf` and `continue`d past every server nobody held a grant for.
3. Linear and Notion mounted (a human had once consented); memory, calendar and whatsapp did not.
4. `agui: readiness probe failed probe=memory error="required memory capability is not mounted"` forever, container `unhealthy`.

## Sequencing choice, and why

`firstPartyGrantKeeper.EnsureNow(ctx)` runs **synchronously in `runServe`, after `bindServeListener` and immediately before `env.liveMCP.StartReconnect(...)`**, and the tick loop starts right after it.

- **Before `StartReconnect`, not after.** `StartReconnect` resolves each deferred server's owner exactly once, in one goroutine, and skips a server with no grant. A grant that lands after that pass is not a late mount — it is a mount that only appears on the *next restart*. Re-triggering the mount after minting was the alternative; it would have meant a second mount trigger racing the first over the same registry, and `liveMCPMount` deliberately has one mounter for that reason.
- **After the listener binds, not earlier in `bootServe`.** Two reasons. The issuer needs `authulaProvider.MCPOAuth()`, which only exists after `buildAuthDeps`. And the sidecars validate the bearer against this process's JWKS, so the port must at least be accepting (the mount's bounded retry covers the gap between `Listen` and `Serve`).
- **Synchronous, but fail-soft.** Minting is in-process Ed25519 signing plus one upsert per (identity, server) — milliseconds — so it costs nothing at boot. A failure logs a WARN and boot continues: a daemon with degraded memory is more useful than no daemon.
- Identity creation calls `EnsureIdentity` beside `ProvisionMemory`, also fail-soft — a grant is recoverable on the next keeper pass; an operator rolled back over a token is not.

## First-party predicate, and why that one

`manager.FirstPartyRecipe(server)` is true only when the server's `Source` **and** its resolved `URL` both match an entry in `BuiltInCatalog()`. Both halves are load-bearing:

- The **name** is not enough — `aura mcp install memory mymem` renames the server, which is exactly the case `mcp.SourceRecipeMemory`'s own comment was written for.
- The **source** is not enough — `~/.aura/mcp/servers.json` is operator-editable, so a hand-planted entry could borrow `recipe:calendar` and point anywhere. Pinning the URL to the catalog's (which is env-resolved at runtime, so it compares like for like) makes such an entry **not** first-party.
- `aura mcp add` cannot reach it at all: it hard-codes `Source: "manual"`.

`mcp.IsSharedAdminGoverned` was deliberately **not** widened — its meaning ("the one deployment-wide shared server") is asserted by `validateManagedServers`, which refuses duplicate memory-recipe sources; widening it to three recipes would have changed that behaviour.

Defence in depth: `IssueFirstPartyToken` independently refuses any resource outside `allowedMCPResource` — the same allow-list `AuthorizationHandler` enforces — and refuses a non-UUID subject, because the resource servers derive database, SQLite directory and account from `sub` verbatim.

Proven by test: `TestFirstPartyMCPServersRefusesEverythingButTheShippedRecipes` seeds a registry with the three recipes plus an `aura mcp add`-shaped entry and a hand-planted `source: "recipe:calendar"` pointing at `attacker.example`, and asserts only the three recipes are self-issuable. `TestIssueFirstPartyTokenRefusesForeignResourcesAndBadSubjects` asserts the issuer-side refusal mints and stores nothing.

## Deviations from plan

### [Rule 1 - Bug] A self-issued grant cannot carry a refresh token

**Found during:** the first live boot after the fix.
**Issue:** the plan (and the first implementation) mirrored `exchangeAuthorizationCode` exactly, including `StoreInitialRefreshToken`. All three grants were refused:

```
webauth: store first-party MCP refresh token: ... insert or update on table
"refresh_tokens" violates foreign key constraint "fk_refresh_tokens_session" (23503)
```

`authula.refresh_tokens.session_id REFERENCES authula.sessions(id) ON DELETE CASCADE`. A refresh token can only exist behind a real login session, and self-issuance has none. Borrowing a cockpit session was rejected: it would tie the agent's memory to a person's browser and kill it at logout (and `MaxSessionsPerUser: 5` means minting sessions would evict the operator's real ones).

**Fix:** `IssuedMCPToken` no longer has a `RefreshToken` field and the issuer never touches `s.tokens.refresh`. The keeper was always the renewal mechanism — Aura's own refresh leg rotates against the same missing session, so a stored refresh token could never have been redeemed either; dropping it only removes a round trip spent discovering that. The stored `token_endpoint` stays, because `restoreTokenSource` refuses a grant without one.
**Commit:** `afd248e43`

### [Rule 2 - Missing critical functionality] The multi-owner mount

**Found during:** design, confirmed against the live identity roster.
**Issue:** `Store.OwnerOf` returns `ErrAmbiguousOwner` when two identities hold a grant for the same server, and `mcpOwnerContext` treats that as "no owner" — i.e. no mount. Minting per identity would therefore have **armed** the exact outage this change fixes: enrol a second person and memory, calendar and whatsapp go dark for everyone. (It is why the keeper filters to `kind = "user"`: the live box has a `service` identity `aura-cli`, and minting for it alone would have re-broken the mount immediately.)
**Fix:** `Store.OwnersOf` returns every owner; `OwnerOf` is now that call plus the uniqueness rule (no duplicated loop). `mcpGrantOwner` sends only first-party servers down the multi-owner path and takes the first candidate — stable across boots because `ListIdentities` orders by `created_at`. Safe because the boot mount only reads the tool schema: each caller's tools run on that caller's own session (`identitySessionPool`) and `IdentityBindingMiddleware` rejects a call arriving on somebody else's. Third-party behaviour is byte-identical.
**Commit:** `8d8522461`

### Interaction with Amendment #148 (landed concurrently)

A parallel session pushed `ec89b5721` mid-task: multi-user provisioning is now contractually disabled, and boot will refuse more than one non-deactivated `kind=user` identity. That makes the multi-owner branch unreachable *at runtime* while #148 holds — it is not dead code (`mcpGrantOwner` runs on every boot; only the `len > 1` sub-case is gated by policy), and #148 itself names "MCP execution whose catalog visibility, credentials, and session state cannot cross identities" as a precondition for re-opening multi-user, which is precisely this. Recorded in Amendment #149 rather than reverted.

### Refactor-on-touch

- `mcpTokenClaims` extracted in `mcp_oauth_server.go`: the browser exchange and self-issuance now share one claim map, so they cannot drift into tokens the sidecars validate differently.
- `mcpGrantOwnerStore` added in `mcp_live_mount.go`: a two-method interface over the grant store, so the first-party branch is provable without a live Postgres (daemon-free unit test, per the CLAUDE.md coverage rule).
- `mcp.AuraIssuedGrant` in `oauth_contract.go`: one writer for the stored client blob, instead of re-deriving `resolvedClient`'s JSON schema outside `internal/mcp`.

No file exceeds 600 LOC (largest touched: `cmd/aura/serve.go` at 520).

## Gates

| Gate | Result |
|------|--------|
| `go vet ./...`, `go build ./...` (Windows + WSL) | clean |
| WSL `go test -race -count=1` on `internal/webauth/... internal/mcpoauth/... internal/mcp/... cmd/aura/` | all ok |
| `golangci-lint run` on the touched packages | 0 issues |
| `bash scripts/coverage_docker.sh` (owned surface, `db_integration`) | **27307/31825 = 85.8%** ≥ 85% |
| `scripts/quality_snapshot_gate.sh` | `ok: checked 4 row(s) against base date 2026-08-26` |
| file-size (≤600 LOC) | 11 files checked, all within |

Required tests, all present and green: (a) a first-party server gets an auto-grant — `TestFirstPartyGrantsMintForEveryUserIdentity`; (b) a non-first-party server is refused — `TestFirstPartyMCPServersRefusesEverythingButTheShippedRecipes` + `TestFirstPartyRecipeRefusesEverythingElse` + `TestIssueFirstPartyTokenRefusesForeignResourcesAndBadSubjects`; (c) reconciliation is idempotent — `TestFirstPartyGrantsSecondPassMintsNothing`; (d) the grant is scoped to the right identity — `TestFirstPartyGrantsAreScopedPerIdentity` + `TestIssueFirstPartyTokenScopesTheSubjectToTheIdentity`.

## What was proven live

Stack rebuilt from HEAD (`docker compose build aura`), the three first-party grants **deleted from Postgres** first so the daemon booted in the genuinely broken state, then `docker compose up -d aura` plus a forced recreate of the three netns-sharing sidecars.

1. **Self-issuance at boot** — `mcp oauth: self-issued a first-party grant` for calendar, memory and whatsapp at `15:32:48`, all expiring `15:47:48`.
2. **All three mounted** — `mcp live mounted` calendar 1 tool, memory 4, whatsapp 15, beside linear 55 and notion 28.
3. **The probe stopped failing** — total `readiness probe failed` count for the whole boot is 2, both at `15:32:53`/`15:32:58`, i.e. *before* the memory mount landed at `15:33:01`. None after.
4. **Container healthy** — `docker inspect aura --format '{{.State.Health.Status}}'` → `healthy` at `15:33:09`; `/readyz` → `{"status":"ready","ready":true,"reasons":[]}`.
5. **`docker exec aura aura mcp authorizations`** lists all five, the three first-party ones with `updated 15:32:48`.
6. **Renewal works** — the keeper re-minted at `15:42:48` (one tick inside the 7-minute window on a 15-minute token), new expiry `15:57:48`, zero `token refresh failed` / `could not keep the first-party grants current` warnings.
7. **The mount survived the original expiry** — at `15:49:18`, past `15:47:48`, `/readyz` still `ready` and health still `healthy`, with the probe calling `memory_search` through the live mount every 30 s. The live token source adopted the renewed grant with no remount.
8. **A real agent turn** — authenticated cockpit login → `POST /api/conversations` → `POST /agent/run` (SSE, `RUN_FINISHED`). The agent called `memory__memory_upsert_fact` (`{"refused":false,"superseded":0}`) and `memory__memory_recall`, which returned the fact with its `fact_key`, `valid_from 2026-08-26 15:50:32` and `retrieval.path: "graph"`.
9. **Ground truth in ArcadeDB** — `mem_b130c94d_a213_463a_a797_ec124104363a`, the operator's own tenant database, is the one whose `FACT_0`/`Entity_0` buckets were written, at `15:50:33` (vector index at `15:50:37`). No other `mem_*` directory was touched — the subject-scoped isolation held.

## What was NOT proven

- **The multi-owner branch on a live database.** The deployment has one active `kind=user` identity, and Amendment #148 now refuses to boot with more. Proven only by unit test (`TestFirstPartyMountSurvivesSeveralGrantOwners`) and by the `db_integration` `OwnersOf` test against a disposable Postgres.
- **Survival of a keeper outage.** If the tick loop stops (or the process is frozen past 15 minutes), the access token dies and every first-party tool call fails until the next successful pass. It self-heals — `persistingTokenSource.reload()` runs before every call — but the failure window was not exercised deliberately.
- **The Calendar and WhatsApp tool surfaces end to end.** Both mounted with the expected tool counts and their grants renewed, but no agent turn drove a calendar or WhatsApp tool. Only Memory was driven by a real agent.
- **Multi-identity tenancy under load.** One identity, one turn. Amendment #147's two-subject tier was not re-run.
- **Mutation testing.** Not run for this change (`go-mutesting` needs a throwaway worktree; the phase-close campaign owns it).
- **`docs/aura-quality-snapshot.md` per-row re-attestation.** The gate is green (the matched rows already carry today's date from the concurrent OAuth work), and the per-row bump is a phase-close obligation, not a mid-turn one. The fresh aggregate — 85.8% owned surface on the `db_integration` matrix — is recorded in PRD Amendment #149 so the phase-close update has a real number to cite.
- **CI.** Nothing was pushed; the branch was merged into local `master` only. `origin/master` is unchanged at `ec89b5721` plus this merge locally.

## Commits

| Commit | Subject |
|--------|---------|
| `8d8522461` | fix(mcp): keep Aura's own sidecars mounted when several identities hold a grant |
| `168bd5d09` | fix(mcp): self-issue the grants Aura's own MCP sidecars need to mount |
| `afd248e43` | fix(mcp): stop trying to store a refresh token a self-issued grant cannot have |
| `4ef6f8ed9` | test(mcp): prove a self-issued grant is one restoreTokenSource will adopt |
| `15cc39083` | merge: self-issued first-party MCP grants |
| `434a43229` | docs(prd): record that Aura mints its own sidecars' MCP grants (Amendment #149) |

`compose.yaml` carries one uncommitted orchestrator change (`WHATSAPP_MIGRATION_TENANT_ID` on the whatsapp service). It was preserved through the merge and left uncommitted, as instructed.
