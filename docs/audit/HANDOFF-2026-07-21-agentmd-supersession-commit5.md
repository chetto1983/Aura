# HANDOFF — Agent.md → memory supersession (Amendment #87), resume at COMMIT 5/7

**Written:** 2026-07-21 (end of the commit-4 session).
**Read first, in order:**
1. `docs/audit/agentmd-supersession-design-2026-07-21.md` — the ADR + 7-commit plan (truth-source).
2. `docs/audit/HANDOFF-2026-07-21-agentmd-supersession.md` — the mid-execution handoff (commits 1-3, the commit-3 contracts you reuse).
3. This file — supersedes #2 for current state; commits 1-4 are done.

Memories: `[[aura-l4-archival-memory-state]]`, `[[e2e-real-not-smoke]]`, `[[debug-by-driving-the-agent]]`, `[[coverage-gate-nukes-neo4j]]`, `[[coverage-gate-nukes-live-db]]`, `[[aura-runs-container-ubuntu-only]]`, `[[ci-coverage-gate-tags-rule]]`.

## Locked scope — "no more, no less"
Remove Agent.md; onboarding stores the operator profile into Neo4j via the agent-memory MCP. Nothing else. Executed as 7 atomic commits, "prove the new store, then tear down the old". Ratified as **PRD Amendment #87**.

## Done (local `master`, UNPUSHED — 7 commits ahead of origin, all gates green)
| SHA | Commit |
|---|---|
| `cba810e8` / `81b65f2c` | Amendment #87 ratified + corrected in prd.md |
| `d886d10b` | **1/7** `internal/idroot` relocation (path guard, 6 consumers rewired) |
| `8a964c55` | **2/7** `internal/onboarding` renderer/Preferences relocation |
| `f4684106` | **3/7** web onboarding → memory MCP (the shared `memoryProfileStore` adapter) |
| `a5e26830` | docs: mid-execution handoff (commits 1-3) |
| `ba06aa5b` | **4/7** telegram onboarding → same port + fakeMemoryStore test migration |

**Do NOT push** (phase-close rule — nothing pushed this whole effort).

### Commit 4 is DONE + LIVE-VERIFIED
- Code: `internal/channels/telegram` is **off `internal/profile`** entirely (grep-verified). `Deps.Profile` is `onboarding.ProfileMemoryStore`; `serve_channels.go` wires `newMemoryProfileStore()`. `maybeStart` gate = `Status(ctx,id)`; `writeCompleted/writeSkipped` take ctx → `StoreConfirmed/StoreSkipped`. Three test files migrated to an in-memory `fakeMemoryStore` (the handoff listed 2; `bot_dispatch_test.go`'s shared `dispatchChannel` helper was the 3rd). `go build/vet/test` + `-race` (WSL) green for `internal/channels/telegram` + `cmd/aura`.
- **Live E2E (deployed `aura:local`, rebuilt+redeployed with commits 1-4):** drove the real web profile-onboarding through the cockpit; full 5-step interview → correct draft → `/profile complete` = `completed:true` → **`StoreConfirmed` ran live**. Neo4j ground truth: `:User.identifier` = the operator UUID scopes the mapped entities (Davide/PmSync/Caraglio/Aura…), facts (`role`/`located_in`/`stack`/`knows`…), preferences, the raw-draft `:Message`, and the sentinel `{subject:<operator UUID>, predicate:onboarding_completed, object:2026-07-21T08:26:24Z}`. `/api/onboarding/status` = `completed:true` via the real `memory_get_facts` round-trip. Idempotency proven (re-run updated the sentinel timestamp via dedup, no dup). The literal Required→Completed flip was not shown fresh (operator was already onboarded-in-memory from a prior session); the surgical sentinel-delete to demo it was blocked by the safety classifier and left alone — both halves are otherwise proven (write in-graph + Status read live; the Required branch is unit-tested).

## NEXT: COMMIT 5/7 — remove the messages[1] injection **leg1** (profile)
Goal: nothing injects the Agent.md profile at messages[1] anymore. **Leg2** (archival-recall, `AURA_CONTEXT_MEMORY_RECALL`, default-OFF) and **leg3** (always-on skills) STAY. **READ BEFORE EDIT — verify every anchor below; line numbers are as of `ba06aa5b` and drift.**

1. **`internal/runner/runner_context.go`** — `renderContextBlock` (`:52`). Guard at `:57` is `if r.contextBlock != nil || r.archivalRecaller != nil` → narrow to `if r.archivalRecaller != nil`. Delete the leg1 block (`:63-67`, `if r.contextBlock != nil { … }`). Keep leg2 (`:68+`, archivalRecaller). Update the `:47` doc-comment ("up to three legs" → the remaining set).
2. **`internal/runner/runner.go`** — remove `Deps.ContextBlock` (`:93`), the `contextBlock` field (`:176`), and its New() assignment (`:251`).
3. **`internal/runner/interfaces.go`** — remove the `ContextBlockProvider` type (`:61-63`).
4. **`cmd/aura/serve_adapters.go`** — delete `profileContextProvider` (`:516`, the whole func).
5. **`cmd/aura/chat_boot.go`** — remove the `ContextBlock: profileContextProvider(cfg),` wiring (`:346`).
6. **`cmd/aura/cache_audit.go`** — ⚠️ **CAUTION, re-verify, do NOT blind-delete.** The prior handoff/ADR said "retire the Agent.md-cache-churn scenario (moot once nothing is injected)." But the messages[1] block is **profile (leg1) + skills (leg3)**, and **leg3 skills still populate messages[1]**. So the scenario is NOT fully moot: it must DROP the Agent.md/profile leg but likely KEEP the skills-block stability check. Anchors: imports `internal/profile` (`:31`), `ContextBlock: profileContextProvider(auditCfg)` (`:205`), messages[1] "profile/skills" framing (`:99-138`, `:161-174`, `:251` `hashMessages1`). Decide: trim to skills-only vs delete — READ the file, confirm skills still inject at messages[1], keep the diff minimal and honest.
7. **Tests:** delete `internal/runner/profile_context_test.go` (the leg1 unit test). **Re-drive `internal/runner/runner_wiring_test.go` GetError/IdentityError branches via `ArchivalRecaller` (leg2)** — otherwise `internal/runner` drops below the 85% owned-surface floor (those error branches were only reached through leg1).
8. **Verify:** `gofmt` + `go build ./...` + `go vet ./...` + `go test ./internal/runner/ ./cmd/aura/` (+ `-race` in WSL, `CGO_ENABLED=1`, `wsl -e bash -lc '… cd "$(wslpath D:/Repo/Aura)"; …'`). Trust `go build`, not IDE PostToolUse red (it lags during multi-edit bursts).
9. Direct `git commit` (pre-commit hook ~10-60s: gofmt/file-size/vet/lint). **Do NOT push.**

## THEN commits 6-7
- **6/7** — Trim `<profile_context>` in `internal/agent/prompt.go` (`:53-60`): remove the messages[1]/Agent.md framing, keep the `<memory>` D-03 block byte-identical. Update the two `prompt_test.go` needles (`'messages[1]'`, `'operator-pinned context'`). One-time `messages[0]` hash change — **no golden hex pin exists** (verified). Re-run `internal/agent` tests.
- **7/7 (last)** — Delete `internal/profile/*` + `cmd/aura/profile.go` (the dead `aura profile` CLI — `aura memory` replaces it). Fix `internal/conversations/context_profile_test.go` (swap `profile.RenderContextBlock` for a literal). **KEEP** `Config.ProfileDir`/`AURA_PROFILE_DIR` (no env retirement in scope). **FULL `db_integration neo4j_integration` matrix ≥85%** via `bash scripts/coverage_docker.sh` (stack up, **disposable `aura_cov` DB — NEVER live `aura`/neo4j**, `[[coverage-gate-nukes-live-db]]`/`[[coverage-gate-nukes-neo4j]]`). Final E2E + **quality-snapshot re-attest** at phase close (`docs/aura-quality-snapshot.md`; verify `scripts/quality_snapshot_gate.sh` prints `ok:`). Then the phase-close `git push` + CI-green check (per CLAUDE.md GIT PUSH DISCIPLINE — confirm with operator; nothing has been pushed).

## Standing discipline / gotchas
- Per-commit: gofmt + build + vet + test (+ `-race` WSL for touched pkgs). Direct `git commit`, **no push**.
- `cmd/aura` is EXCLUDED from the 85% owned-surface floor, but still write daemon-free tests.
- `internal/runner` IS in the floor → the `runner_wiring_test.go` leg2 re-drive is mandatory or coverage fails.
- Windows-native: build/vet/test fine; `-race` + the coverage matrix need WSL/CI.

## Live E2E recipe (reuse for the commit-7 final E2E)
- Stack is up (docker; cockpit `http://localhost:9080`). Rebuild+redeploy the deployed binary before an E2E: `docker compose build aura && docker compose up -d aura` (wait `docker inspect -f '{{.State.Health.Status}}' aura` = healthy). The E2E driver used this session lives in the **session scratchpad (ephemeral — recreate it)**; the recipe:
- **Operator identity UUID:** `b130c94d-a213-463a-a797-ec124104363a` (`dvdmarchetto@gmail.com`).
- **Env:** creds in `.env` as `AURA_E2E_AUTHULA_EMAIL`/`AURA_E2E_AUTHULA_PASSWORD`. ⚠️ The email value has **trailing spaces** — TRIM it (`awk … sub(/[[:space:]]+$/,"")`). TOTP not required. `TELEGRAM_BOT_TOKEN` present (needed: `CompleteProfile`→`mintTelegramLink` gates on a resolvable bot username).
- **Login (Authula double-submit):** `GET /api/auth/config` → `csrf_token`; `POST /auth/email-password/sign-in` with `Content-Type: application/json`, header `X-AUTHULA-CSRF-TOKEN: <tok>`, `Cookie: __Host-authula_csrf_token=<tok>` (the `__Host-` cookie is Secure so curl won't persist it over http — send it explicitly), `-c jar` to capture `__Host-authula_session`. **NO `Origin` header** (server rejects a mismatched Origin vs its `127.0.0.1:9080` bind → 400). Token is short-lived: fetch it immediately before sign-in. Write the driver with **LF line endings** (a stray `\r` from a CRLF script corrupts the cookie name → 400).
- **Drive:** `POST /api/onboarding/profile/start` → `sessionToken`; `POST /api/onboarding/{token}/step` `{"intent":"answer","text":"…"}` ×5 (identity→work→projects→social→style; the 5th returns `step=draft,status=draft` + the `draft`); `{"intent":"confirm"}` → `status=completed`; `POST /api/onboarding/{token}/profile` → `completed:true`. `GET /api/onboarding/status` reads the sentinel.
- **Neo4j ground truth (read-only):** `docker exec aura aura neo4j cypher read "…"`. ⚠️ **Never `toString()` a node's `embedding` prop** (DoubleArray → TypeError) — exclude the `embedding` key. Shape: `:User{id,identifier}`, `:Fact{subject,predicate,object}`, `:Preference`, `:Entity:*`, `:Message`. Scope = `User.identifier` = operator UUID. `docker exec aura aura neo4j cypher write "…"` exists but destructive writes on live neo4j are (rightly) blocked by the safety classifier.
