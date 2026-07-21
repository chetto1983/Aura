# HANDOFF — Agent.md → memory supersession (Amendment #87), mid-execution

**Written:** 2026-07-21 (~50% context, starting fresh).
**Read first:** `docs/audit/agentmd-supersession-design-2026-07-21.md` (the ADR + 7-commit plan — truth-source), then this file. Memories: `[[aura-l4-archival-memory-state]]`, `[[aura-fix-plan-wave0-status]]`, `[[debug-by-driving-the-agent]]`, `[[e2e-real-not-smoke]]`, `[[coverage-gate-nukes-neo4j]]`, `[[web-dist-must-build-on-linux]]`.

## What this is
Retire the static `Agent.md` profile; onboarding stores the operator profile into Neo4j via the agent-memory MCP. Operator-locked "no more, no less". Ratified as **PRD Amendment #87** (in `prd.md` at the Slice 10 head + a superseded marker at line ~189). Executed as **7 atomic commits, "prove the new store, then tear down the old"**.

## Done (local `master`, UNPUSHED, all gates green)
| SHA | Commit | Notes |
|---|---|---|
| `cba810e8` | Amendment #87 ratified in prd.md | — |
| `81b65f2c` | Amendment corrected w/ seam-map findings | deterministic mapping, user_identifier guard, prerequisites |
| `d886d10b` | **1/7** idroot guard relocation | `internal/idroot` (RootIdentityDir/ValidateIdentity/ErrInvalidIdentity/DefaultRoot), 6 consumers rewired; 85.7% cov |
| `8a964c55` | **2/7** onboarding renderer/Preferences relocation | `internal/onboarding/draft_render.go` (Preferences/AgentContent/RenderAgentMD/MaxAgentMDBytes); onboarding off profile; 93.8% |
| `f4684106` | **3/7** web onboarding → memory MCP | the security-critical one; 94.1% |

Every commit: `go vet/build/test` green + pre-commit gate (gofmt/file-size/vet/lint 0). **Nothing pushed.**

### Key contracts now in place (from commit 3 — reuse these in commit 4)
- **`internal/onboarding/memory_store.go`**: `ProfileMemoryStore` interface `{ StoreConfirmed(ctx, identityID, Answers, rawDraft) ; StoreSkipped(ctx, identityID) ; Status(ctx, identityID) (OnboardingState, error) }`; pure `MapProfile(Answers) ProfileMemory` (entities-first → facts → controlled-vocab preferences); sentinel predicate consts `PredicateOnboardingCompleted`/`PredicateOnboardingSkipped`; `OnboardingState{Completed,Skipped}`.
- **`cmd/aura/memory_onboarding.go`**: `memoryProfileStore` (fields `call func(ctx,tool,args)(string,error)` + `now func()time.Time`; `newMemoryProfileStore()`). 🔴 **`write()` sets `args["user_identifier"]=identityID` on EVERY call** — `callMemoryToolText` does NOT inject it; a bare call leaks into the memory server's fail-open GLOBAL scope (MUSR cross-tenant). `Status` reads `memory_get_facts(subject=identityID,user_identifier=identityID)` → `{facts:[{predicate}]}`, scans predicates.
- Web wiring done: `agui.OnboardingDeps.Profiles` is `onboarding.ProfileMemoryStore`; `agui/onboarding_provision.go` calls StoreConfirmed/StoreSkipped (persistProfile now takes ctx); `cmd/aura/serve_onboarding.go` status adapter + `buildOnboardingService` use `newMemoryProfileStore()` (fails OPEN to Required on sidecar error). agui is now OFF `internal/profile` entirely.

## NEXT: Commit 4/7 — telegram → same port + live E2E
Mirror commit 3 for `internal/channels/telegram`:
1. **`profile_onboarding.go`**: `store *profile.Store` → `store profileflow.ProfileMemoryStore` (`profileflow` = the onboarding import alias already present). `newProfileOnboarding(store profileflow.ProfileMemoryStore, ...)`. `maybeStart` (lines ~78-83): replace the `p.store.ReadProfile(id)` gate with `st,err := p.store.Status(ctx,id)` → don't-start when `st.Completed||st.Skipped`, error reply on err. `writeCompleted(ps,out)` → `writeCompleted(ctx,ps,out)` calling `StoreConfirmed(ctx, ps.account.IdentityID, ps.session.Answers, ps.session.DraftAgentMD)` (keep the empty-draft guard). `writeSkipped(ps)` → `writeSkipped(ctx,ps)` calling `StoreSkipped`. Update the two callers in `handleCallback` (skip ~187, confirm ~201) to pass `ctx`. Drop `internal/profile` + `internal/idroot` imports.
2. **`bot.go:88`**: `Profile *profile.Store` → `Profile profileflow.ProfileMemoryStore`; drop `internal/profile` import (bot.go uses profile only for `.Store`). `profileflow` already imported.
3. **`cmd/aura/serve_channels.go:84`**: `Profile: profile.NewStore(chat.cfg.ProfileDir)` → `Profile: newMemoryProfileStore()`; drop the profile import if now unused.
4. **Tests (the bulk):** `profile_onboarding_test.go` (~7 sites) + `bot_dispatch_onboarding_test.go` (~6 sites) build `profile.NewStore(t.TempDir())` and assert on filesystem writes + the read-back `maybeStart` gate. Add a `fakeMemoryStore` (in-memory `map[id]state` implementing `profileflow.ProfileMemoryStore`; `Status` returns the recorded state) and replace every `profile.NewStore(t.TempDir())` with it. Fix `writeCompleted`/`writeSkipped` call sites to pass `ctx`. Rework any assertion that read the written Agent.md content to assert the fake's recorded StoreConfirmed/StoreSkipped/completed-state instead. Preserve the "already-onboarded → maybeStart returns false" and "skip writes the skipped sentinel" behaviors.
5. **Verify:** `go vet/build/test -race ./internal/channels/telegram/ ./cmd/aura/`; telegram off `internal/profile`.
6. **LIVE E2E (>9.8, `[[e2e-real-not-smoke]]` + `[[debug-by-driving-the-agent]]`):** stack is up (docker; cockpit `http://localhost:9080`; E2E creds in `.env` `AURA_E2E_AUTHULA_EMAIL`/`PASSWORD`). Rebuild+redeploy `aura:local`, run a full onboarding through the live agent, then assert in `aura-neo4j` that the profile entities+facts+preferences AND the `onboarding_completed` sentinel are scoped to the identity UUID (`user_identifier`), `memory_store_message` captured the raw draft, `/api/onboarding/status` flips Required→Completed, and a later turn recalls a stored fact. Query DB read-only via `docker exec aura-postgres psql -U aura -d aura` and `docker exec aura aura neo4j cypher read "..."`.

## Then commits 5-7 (see ADR §2.4 for the verified specs)
- **5/7** Remove the messages[1] injection leg1: drop leg1 in `internal/runner/runner_context.go` renderContextBlock + narrow the owner-resolve guard to `if r.archivalRecaller != nil`; remove `Runner.contextBlock`/`Deps.ContextBlock`/New() assignment (`runner.go`) + `ContextBlockProvider` type (`interfaces.go`); delete `profileContextProvider` (`cmd/aura/serve_adapters.go:515-534`) + `chat_boot.go:346` wiring + retire the `cache_audit.go` Agent.md-cache-churn scenario (205, 302-310). Leg2 (archivalRecaller, default-off) + leg3 (skills) STAY. **Re-drive `runner_wiring_test.go` GetError/IdentityError via ArchivalRecaller (leg2)** or internal/runner drops below the 85% floor. Delete the two leg1 unit-test files.
- **6/7** Trim `<profile_context>` in `internal/agent/prompt.go:53-60` (remove the messages[1]/Agent.md framing, keep the `<memory>` D-03 block byte-identical); update the two `prompt_test.go` needles ('messages[1]', 'operator-pinned context'). One-time messages[0] hash change — no golden hex pin exists (verified).
- **7/7** Delete `internal/profile/*` + `cmd/aura/profile.go` (the `aura profile` CLI — dead on deletion; `aura memory` is the replacement) + fix `internal/conversations/context_profile_test.go` (swap `profile.RenderContextBlock` for a literal). Decide `Config.ProfileDir`/`AURA_PROFILE_DIR`: plan KEEPS it (no env retirement in scope). **FULL `db_integration neo4j_integration` matrix ≥85%** (`bash scripts/coverage_docker.sh`, stack up, disposable `aura_cov` DB — NEVER live), final E2E, **quality-snapshot re-attest** at phase close.

## Standing discipline / gotchas learned
- Per-commit: `gofmt -w <files>` + `go build ./...` + `go vet` + `go test` (+ `-race` for touched pkgs) BEFORE commit. Direct `git commit` (pre-commit hook ~50-60s on lint). **Do NOT push** until the operator says (phase-close rule); no push happened this session.
- **`cmd/aura` is EXCLUDED from the 85% owned-surface floor** (behaviourally covered) — but still write daemon-free tests (the `memory_onboarding_test.go` user_identifier guarantee is security-critical).
- **IDE PostToolUse diagnostics lag/race** during multi-edit bursts — trust `go build`/`go vet`, not the mid-edit red.
- Struct conversion trick: `profile.Preferences(onboarding.Preferences{...})` works (identical fields+tags) — used to avoid a throwaway converter (those sites die in commit 7 anyway).
- **`profile → onboarding` is a CYCLE** (onboarding imports `internal/agent` → profile). Keep converters/local types on the consumer side.
- idroot needed a `containedDir` white-box helper to cover the defense-in-depth escape branch (public API rejects traversal at ValidateIdentity first) → 85.7%.
- Windows-native: `go build/vet/test` fine; `-race` needs w64devkit/WSL. Run the coverage matrix + `-race` in WSL/CI (`[[aura-runs-container-ubuntu-only]]`).
- **NEVER point `db_integration`/coverage at live `aura`/neo4j** (`[[coverage-gate-nukes-live-db]]`, `[[coverage-gate-nukes-neo4j]]`) — the neo4j tier `DETACH DELETE`s the live graph. Use `scripts/coverage_docker.sh` disposable DBs. Back up / re-seed memory before/after if you touch it.
