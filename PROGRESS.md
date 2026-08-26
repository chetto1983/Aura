# OAuth MCP concern progress

Last updated: 2026-08-26

This file is the operational checkpoint for the current concern. Re-read it after every
context compaction together with `AGENTS.md` and `CLAUDE.md`. Repository code, tests, and
live state remain the source of truth.

## Objective

Make Calendar, WhatsApp, and ArcadeDB reusable MCP resource servers that use standard MCP
OAuth and derive tenancy only from the access-token subject. Aura-specific policy stays in
the Aura control plane. Legacy, dark, and dead tenant-routing code must be removed.

## Decisions already confirmed

- MCP resource servers are Aura-agnostic: no `X-Aura-*` headers, no
  `_meta.aura.user_identifier`, and no Aura claims.
- Each remote OAuth server gets one Aura client session per authenticated identity. The
  session bearer determines the standard OAuth `sub`; callers cannot select a tenant in
  tool arguments or MCP metadata.
- No new operator-facing `.env` variables are needed for the three built-in servers.
- Use WSL for implementation validation.

## Persistent green checkpoints

- Migration `0102` real round-trip: GREEN. This was completed before the current OAuth
  delta and is unaffected by it. Do not rerun it merely because context compacted.
- Previous concern real-agent E2E: 10.0/10. It proves the earlier document/approval flow,
  not the new OAuth session-routing delta. The current concern still needs one real-agent
  E2E after the live OAuth stack is green.
- WhatsApp fork before the session-pool work: Ruff, 100 pytest tests, and generic OAuth
  Docker image build were green.
- Calendar fork before the session-pool work: Docker build and 222 tests were green.
- Earlier Aura OAuth/proxy batch: full `go vet ./...`, full `go build ./...`, touched unit
  tests, and touched `-race` tests were green.
- Current per-identity session-pool and legacy-removal batch: `mcptools`, `internal/mcp`,
  and `cmd/aura` unit plus `-race` tests are GREEN; calendar/WhatsApp tagged tiers compile;
  full `go vet ./...` and full `go build ./...` are GREEN.
- Official Go MCP SDK check on 2026-08-25: `v1.7.0` is already the signed latest release
  and provides full protocol `2026-07-28` support. `go get ...@latest` retained `v1.7.0`.
- Static bearer delivery now uses `StreamableClientTransport.OAuthHandler`, as required by
  the official SDK. The retired RoundTripper bearer injection was removed; full `go vet
  ./...`, full `go build ./...`, and `internal/mcp` unit plus race tests are GREEN.
- Migration `0102` still runs automatically through `aura-migrate` before Aura starts.
  Repeated live Compose recreations on 2026-08-26 completed that service successfully.
- Aura no longer deadlocks refreshing its own built-in OAuth grants during boot. Under
  `aura serve`, OAuth MCP mounts start only after the AG-UI listener binds; CLI paths stay
  synchronous. Targeted `cmd/aura`, `internal/mcp`, `internal/agent/mcptools`, and
  `internal/webauth` unit/race/vet/build gates are GREEN.
- The embedded authorization server is on Authula `v1.42.0`. Aura overrides Authula's
  broken unverified signed-JWT parse with JWKS-backed verification; valid Ed25519 subject
  tokens pass and tampered tokens fail in tests. Live refresh-token rotation is GREEN.
- Calendar/PIM accepts the standard Aura Ed25519 access token through the real
  `JsonWebTokenHandler` pipeline. The custom IdentityModel provider implements both Verify
  overloads and its capability probe accepts the one-key call IdentityModel performs.
  Three scoped verifier tests are GREEN.
- Live production-like Compose is GREEN for OAuth mounting: Calendar (1 tool plus UI),
  Linear (55 tools), Memory (4 tools), and WhatsApp (15 tools plus UI) all open sessions
  from stored identity-scoped grants. Deployed local image IDs at this checkpoint:
  Aura `sha256:ee34191ea...`, Calendar `sha256:25b666558...`, WhatsApp
  `sha256:b6e6d77ab...`, ArcadeDB MCP `sha256:1eabc0351...`.
- Memory readiness reuses the authorized subject that opened the live OAuth mount instead
  of inventing an ungranted synthetic subject. Its bounded functional search remains
  tenant-scoped. Targeted unit/race/vet/build gates are GREEN; the real container answers
  `/readyz` with HTTP 200 and Docker reports Aura healthy.
- Aura's persisted OAuth token sources reload a newer stored rotating refresh token before
  refresh and serialize same-identity/server refreshes. The two-source stale-token regression,
  `internal/mcp` unit/race/vet/build gates, and the live Calendar flow are GREEN; the former
  `invalid_grant` followed by "this identity has not authorized this server" no longer occurs.
- Calendar/PIM now authenticates SMTP only when the server advertises the standard MailKit
  `SmtpCapabilities.Authentication` capability. The scoped .NET 10 suite is GREEN (225/225),
  the cached Docker build produced local digest `sha256:25b666558...`, Compose ran
  `aura-migrate` automatically, and the production-like fake-SMTP container captured the real
  agent's message.
- The Cockpit PIM connect flow now uses the canonical account id returned by account creation
  for Google/device-code follow-up instead of reusing the submitted slug. The regression was
  observed RED (`work` instead of `tenant__work`) and is GREEN; all 17 scoped
  `CalendarConnect` tests pass.
- The real Cockpit PIM Playwright case is GREEN in Chromium (44.9s): UI account creation,
  approval pause/resume, canonical Calendar account call, SMTP delivery, final agent
  `content_stop`, and cleanup all completed. The test waits for the resumed `/agent/run`
  response body to finish, so cleanup no longer causes the former `context canceled`.
- Deferred Memory OAuth mounts now attach their live `MountedServer` to the same thread-safe
  memory-context provider captured by the Runner at boot. The provider exists only when the
  `recipe:memory` server is mounted or explicitly deferred, so installations without Memory
  retain a nil provider and do not gain per-turn warnings. Scoped `cmd/aura` unit, race, vet,
  and build gates are GREEN.
- The cached Aura Docker build produced local manifest list `sha256:3ae312856b92cddb66dfeae84be6836fac7f7a1f8147c26a320d26b993fef5d6`.
  Compose automatically recreated and ran `aura-migrate` to exit 0 before the daemon and
  loopback-networked sidecars started; Aura, Calendar/PIM, WhatsApp, and ArcadeDB MCP are healthy.
- The complete real Cockpit MCP spec is GREEN in native WSL Node 24 (2/2, 45.5s): built-in
  Calendar/Memory/WhatsApp OAuth health plus PIM approval/send/fake-SMTP delivery. Aura logs show
  the final `content_stop` and zero occurrences of `memory MCP is not mounted`,
  `conversation memory context unavailable`, or `invalid_grant`; fake SMTP captured message id 4.
  WSL must prepend `/home/davide/node24/bin` or it resolves `npx` to Windows Node and loses the
  WSL-only `AURA_E2E_REAL_AGENT` environment variable, causing a false skip.
- The deferred tool-search fixture was regenerated mechanically from the live
  `aura toolpipe --manifest-json` surface and filtered to `deferred:true`: 98 mounted tools total,
  84 deferred (Linear 55, Memory 4, WhatsApp 15, native 10). It contains zero retired identity
  arguments and zero stale distrust framing. The real-corpus retrieval gate now measures the
  current names/surface and is GREEN at top-1 100% (19/19), recall@3 100% (19/19), with its
  90%/100% floors unchanged. `internal/agent/tools` and `internal/agui` unit/race/vet/build gates
  are GREEN.
- `docs/HANDOFF.md`, `docs/mcp-manager.md`, and the PIM proxy comments now describe the standard
  identity-scoped bearer/`sub` contract rather than the retired proprietary tenant wire. The
  operational files and generated fixture contain no retired wire literals.
- OAuth-aware CLI diagnostics now resolve the operator identity before opening a remote session.
  The exact production symptom is GREEN in the rebuilt container: `aura mcp tools calendar`
  lists its tool, `doctor calendar` reports one tool, and `status` probes Linear at 55 tools.
  Calendar refreshed its expired stored grant without `aura mcp login`; no static bearer was added.
  The regression test was observed RED for `status`/`doctor`/`tools` and is GREEN; scoped
  `cmd/aura` unit/race/vet/build gates pass.
- Default-on recipe injection now recognizes an explicitly installed recipe by `Source` even when
  the operator renamed it. This prevents a second randomly selected Memory capability. The new
  aliased-memory test was observed RED then GREEN, the former flaky readiness test passed 20/20
  under race, and scoped `internal/mcp/manager` unit/race/vet/build gates pass.
- The rebuilt Aura image has local manifest list
  `sha256:128d92924a0ca74c6f390b5c9b09a985720429b9d57151bb44e2144884eb5087`.
  Build context was 3.27 MB and Docker cache remained enabled. Compose ran `aura-migrate`
  automatically to exit 0. Because the three MCP sidecars share `network_mode: service:aura`,
  any Aura container recreation must recreate Calendar, WhatsApp, and ArcadeDB MCP in the same
  operation; otherwise they remain attached to the retired loopback namespace while Docker can
  still report their old containers healthy. The production-like local OAuth image overrides are
  `aura-pim-mcp:oauth-production`, `aura-whatsapp:oauth-production`, and
  `aura-arcadedb-mcp:local` until fresh GHCR artifacts are published.
- The production-like three-resource cross-subject tier is GREEN under `-race` and
  `goleak` (3/3, 2.37s) through real loopback sidecars and Aura's real Authula signing
  keys. Calendar exposed only each token subject's canonical account after its measured
  configuration reload; WhatsApp exposed only each tenant SQLite marker after the real
  gateway started both WhatsMeow runtimes; Memory provisioned two real ArcadeDB databases,
  returned only each subject's fact, and ignored forged `_meta.tenant`. All disposable
  Calendar accounts, WhatsApp stores, ArcadeDB databases, and tenant users were removed;
  `aura mcp doctor whatsapp` reopened 15 tools after the cleanup-sidecar recreation.
  Scoped `internal/webauth` unit/race/vet/build and tagged compile gates are GREEN.
- The isolation tier deliberately composes only Aura's production token plugin, but the
  full Authula-provider lifecycle is now independently GREEN under `-race` and `goleak`.
  The regression was first RED on Authula v1.42.0's direct in-memory rate-limit cleaner
  plus two `database/sql` workers. Aura now composes Authula's official in-memory
  `secondary-storage` plugin, which has a joinable `Close`, through the rate limiter's
  published service seam; no fork or custom provider was added. `Provider.Close` also
  closes the owned event bus and Bun database idempotently. The real rate limiter allowed
  request one, denied request two, then released every worker. Scoped unit/race/vet/build
  and full `webauth_integration -race` gates are GREEN; disposable Authula users and Aura
  identities both count zero after cleanup.
- The real Cockpit graph regression is GREEN in Chromium (1/1, 4.5s) against the live
  operator ArcadeDB database: authenticated overview rendered the MCP-created `Entity` /
  `FACT` fixture, evidence lists were non-empty, selecting a node and expanding its real
  `#cluster:position` RID returned HTTP 200 cumulatively, and the inspector exposed the
  generated ArcadeDB `SELECT`. Browser intents carried neither `session` nor `seed_id`, so
  the server derived the same authenticated identity that `aura memory remember` used.
  The first run was correctly RED because the database was empty; the spec now creates its
  fixture through the production `aura memory` MCP CLI and always removes it by unique
  source run in `finally`. Post-test direct counts are `Entity=0`, `FACT=0`. Scoped Prettier,
  ESLint, TypeScript, and the single Playwright spec are GREEN.
- The current OAuth concern's complete real Cockpit MCP gate is GREEN in native WSL Node 24
  (2/2, 41.9s), which is a functional score of 10.0/10. The first scenario reopened
  Calendar, Memory, and WhatsApp through the production Cockpit authorization and health
  paths. The real-agent scenario created a PIM account, called the live Calendar MCP, paused
  for approval, resumed `/agent/run`, delivered fake-SMTP message id 5, ended with
  `content_stop`, and removed its conversation and account. Fresh postconditions show
  `pim-e2e` absent from Calendar configuration and zero Aura-log occurrences of
  `invalid_grant`, `memory MCP is not mounted`, or `conversation memory context unavailable`.
  Fresh production doctors open Calendar (1 tool), Memory (10 tools), and WhatsApp (15 tools)
  without `aura mcp login`; WhatsApp's optional private bridge probe remains HTTP 401 by
  design while the MCP doctor itself exits successfully.
- Authula provider ownership is now lifecycle-complete without a fork or custom storage
  provider. The regression was observed RED with Authula's direct rate-limit cleanup worker
  plus Bun's `database/sql` workers still alive after close. Aura now composes Authula's
  official joinable in-memory secondary-storage plugin through the published rate-limit
  service seam, and `Provider.Close` idempotently closes plugins, systems, the event bus, and
  its owned database. The real limiter allows request one and denies request two; the full
  `webauth_integration -race` tier and `goleak` are GREEN, and disposable Aura/Authula rows
  both count zero. CI now supplies the exact Authula DSN and runs this tagged tier
  unconditionally under `CI=true`, so a missing environment cannot skip as green.
- Closing validation remained scoped to the touched packages. Fresh WSL `go vet`, `go build`,
  unit, and race gates are GREEN for `cmd/aura`, `cmd/arcadedb-mcp`, `internal/agent/mcptools`,
  `internal/agent/tools`, `internal/agui`, `internal/config`, `internal/mcp`,
  `internal/mcp/manager`, and `internal/webauth`. Prettier, touched-file ESLint, TypeScript,
  and 17/17 CalendarConnect tests are GREEN. The WhatsApp fork is GREEN at Ruff, 101/101
  pytest, Go test/build, and golangci-lint; the Calendar fork is GREEN at 225/225 Release tests
  in the cached .NET 10 container.
- A fresh cache-enabled Aura image was built as local manifest list
  `sha256:8032394ca20848147b4d024564de26fcbbf8148e19aaa38e72e9be66f75c8725` and deployed with
  the three production-like MCP sidecars. `aura-migrate` exited 0 with `ok: no pending
  migrations`; Aura, Calendar, WhatsApp, and ArcadeDB MCP are healthy. On this exact rebuilt
  container the Cockpit MCP spec passed 2/2 and the separately guarded live graph spec passed
  1/1. The combined command's initial graph skip was not counted: it was rerun with
  `AURA_E2E_LIVE_GRAPH=1` and passed. The actual rebuilt-container total is therefore 3/3.
- Fresh post-E2E evidence on the rebuilt container is clean: production doctors open Calendar
  (1 tool), Memory (10), and WhatsApp (15) without login; fake SMTP captured the newest real
  agent message as id 6; Calendar configuration contains zero `pim-e2e` fixtures; Aura logs
  contain one final `content_stop` and zero `invalid_grant`, `memory MCP is not mounted`, or
  `conversation memory context unavailable`. A direct authenticated ArcadeDB query against the
  operator tenant reports `Entity=0` and `FACT=0`, proving the graph fixture cleanup completed.
- The critical Authula shutdown mutation spot-check is GREEN at 7/7 killed = 100% for
  `Provider.Close` under the real `webauth_integration` tag and a disposable migrated
  PostgreSQL database. The first honest run was 6/7: deleting the explicit event-bus close
  survived even though it is not redundant (`CloseSystems` closes only Authula's session and
  verification cleanup systems). A production-behavior assertion now proves the event bus
  rejects publishes after close; nil and partially constructed providers are also pinned.
  Fresh unit/race/vet/build and tagged lifecycle/race gates remain GREEN after that test.
- The two external resource-server forks are committed and pushed on `main`. WhatsApp commit
  `a463da57dd7afd243b2db8827dd26891b661df7e` published immutable index
  `ghcr.io/chetto1983/whatsapp-mcp:sha-a463da5@sha256:006602da0893ada0eb80c812384032e262c1c7f0642866bf194041078e86130f`.
  Calendar commit `b1d584a4645c41b8e23e176af258befd9e598de5` published immutable index
  `ghcr.io/chetto1983/aura-pim-mcp:b1d584a4645c41b8e23e176af258befd9e598de5@sha256:ecb4a77957c012e61da1aebd69b17213e584e10215718b1195174216e6461af0`.
  Calendar's final cached .NET 10 suite is GREEN at 225/225; its touched IMAP provider also
  no longer emits the nullable-content warning. Retired `CALENDAR_MCP_ADMIN_TOKEN`,
  `X-Admin-Token`, proprietary Aura identity wires, stale secret manifest, and Aura-named
  embedded UI tokens are absent from the generic server and deployment examples.
- `origin/master` advanced by 14 Phase-52 commits during this concern. Their only overlap with
  the local OAuth delta is `prd.md` plus `docs/aura-quality-snapshot.md`; no MCP production code
  overlaps. The remote already used PRD amendment number 146, so the measured OAuth decision was
  renumbered to amendment 147 before integration.

## Current repository state

- Aura branch: `master`, HEAD `8bba3fac8c7594539ba11eb3be568bf4fcd2730f`, with the
  current concern uncommitted and `origin/master` 14 commits ahead. The overlap is inspected:
  preserve both Phase-52 remote documentation and the OAuth amendment/quality attestations.
- WhatsApp fork: `.planning/tmp/whatsapp-mcp-tenant`, HEAD
  `a463da57dd7afd243b2db8827dd26891b661df7e`, clean and pushed.
- Calendar fork: `.planning/tmp/aura-pim-mcp-tenant`, HEAD
  `b1d584a4645c41b8e23e176af258befd9e598de5`, clean and pushed.

## Implemented, not yet closed

- Generic OAuth validation exists in all three MCP servers.
- Aura authorization server exposes standard metadata, authorization, token, DCR, and JWKS
  endpoints.
- Calendar admin proxy restores/refreshes the identity-scoped OAuth grant.
- WhatsApp pairing gateway uses the internal bridge boundary separately from MCP OAuth.
- Compose carries built-in public OAuth resource/issuer wiring and no static MCP bearer.
- `internal/agent/mcptools` now has an identity session pool and identity-binding middleware.
- Live MCP mounting owns post-listener reconnects and closes sessions cleanly at shutdown.
- Calendar's production bearer verifier is generic OAuth/OIDC code; it contains no Aura
  tenant-routing contract.
- The live PIM fake-SMTP route and canonical Cockpit account-connect route are implemented and
  verified. External fork artifacts are published; the Aura and ArcadeDB-MCP artifacts remain.

## Exact next actions

1. Cross-subject isolation, Cockpit graph, and the real-agent MCP E2E are GREEN on the rebuilt
   production-like container; documentation and scoped closing gates are complete.
2. Quality snapshot re-attestation is complete: six matched rows are dated 2026-08-26, the
   changed-file gate is GREEN, `internal/agui` combined coverage is 85.5%, and Authula close
   mutation is 7/7 killed.
3. Run the small post-pin Aura package gate, commit the OAuth concern, integrate the 14 remote
   Phase-52 commits with both documentation histories preserved, then publish/pin the ArcadeDB
   MCP image from the final code commit. Deploy all three immutable GHCR artifacts, rerun the
   production doctors and real Cockpit E2E, commit/push Aura, and check CI once without waiting
   30 minutes.

## Required WSL toolchain

```sh
export PATH=/home/davide/.local/bin:/home/davide/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export CGO_ENABLED=1
cd /mnt/d/Aura
```
