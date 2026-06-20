# Phase 30: Telegram Onboarding on Frontend with Link and QR Code — Specification

> # ⚰️ TOMBSTONE — ABSORBED INTO PHASE 28 (D-09, 2026-06-20)
>
> **Phase 30 is absorbed into Phase 28.** The Telegram link/QR cockpit surface is delivered as requirement **ONBD-01b** inside Phase 28's onboarding wizard (deep-link + server-rendered scannable QR, reusing the existing `internal/setup` mint + `ConsumeOnboarding` single-use token flow). **Do NOT plan or execute Phase 30 separately** — it has no plans of its own.
>
> **Authoritative spec → [`28-SPEC.md`](../28-governance-boards-web-onboarding/28-SPEC.md) §ONBD-01b.** The original Phase-30 requirements (7 locked, below) are retained verbatim **for traceability only**; ONBD-01b is the live contract. Recorded by the Phase-28 D-07 PRD-amendment commit (prd.md amendment #64).

**Created:** 2026-06-19
**Status:** ⚰️ Absorbed into Phase 28 (ONBD-01b) — 2026-06-20 (D-09)
**Ambiguity score:** 0.14 (gate: ≤ 0.20)
**Requirements:** 7 locked (retained for traceability; delivered as 28-SPEC §ONBD-01b)

## Goal

An authenticated operator can link a Telegram account from the web cockpit — set the Telegram bot token, mint a re-mintable `t.me` deep-link rendered as a scannable server-generated SVG QR, and watch a live confirmation naming the linked user — surfaced both as a skippable step in the onboarding wizard and as a "Connect Telegram" settings panel, reusing the existing Telegram channel + setup-wizard backend.

## Background

A complete Telegram setup-wizard backend already exists from Phase 13 (`internal/setup`), but it runs as an **isolated loopback `:9081` HTTP server** gated by a one-time in-memory setup token, with **no cockpit or frontend surface**:

- `internal/setup/handlers.go` — `POST /setup/token` (getMe-validates a bot token → returns username), `POST /setup/onboard-link` (mints a single-use UUID, INSERTs a `telegram_setup_pending` row with a 1h TTL, returns `{deep_link, qr_svg}`), `GET /setup/status`, `GET /setup/events` (SSE `onboarding_completed`).
- The deep-link is already built: `https://t.me/<bot>?start=<onboardingToken>` ([handlers.go:183](internal/setup/handlers.go#L183)); the channel's `/start <token>` consumes it single-use (`consumed_at IS NULL AND expires_at > now()`).
- **The QR is the gap**: `internal/setup/qr.go` is a deliberate deferred stub — `qrSVG()` returns `""`. Today only a *terminal ASCII* QR (`qrterminal`) is printed to the operator's stdout. The code note already names the intended generator (boombuler/barcode) for when the frontend lands.
- The setup token is one-time: `handleEvents` calls `InvalidateToken()` on completion ([handlers.go:149](internal/setup/handlers.go#L149)) — a second navigation 401s. This is correct for the loopback bootstrap but must NOT govern the cockpit surface.

The cockpit (`:9080`) mounts AG-UI + `/api/*` routes on one loopback server behind `RequireAuth` (Authula/passphrase), with mutating routes interposed by `RequireCapability` ([serve_webui.go:180](cmd/aura/serve_webui.go#L180)). The React frontend has shell modes `chat`/`tree`/`graph`/`displays`/`settings` (only `chat` live, [web/src/shell/modes.ts](web/src/shell/modes.ts)) and **no** onboarding/connect/telegram surface.

Two infrastructure gaps make the "set the bot token" path non-trivial and shape the scope:
- **No runtime secret store** — `TELEGRAM_BOT_TOKEN` is read from `os.Getenv` once at boot ([internal/channels/telegram/config.go:43](internal/channels/telegram/config.go#L43)); persisting a cockpit-set token requires a net-new secret-persistence mechanism.
- **No per-channel restart seam** — `channels.Registry` only does `StartAll`/`StopAll` once each ([internal/channels/registry.go](internal/channels/registry.go)); hot-reload was deliberately deferred this phase (token "applies on restart").
- The `telegram_setup_pending` schema records `consumed_at` but **not** which `telegram_user_id` consumed the token — naming the linked user needs a migration (latest migration is `0020`, so this is **`0021`**).

## Requirements

1. **Server-side SVG QR**: The onboard-link response returns a real, scannable SVG QR of the deep-link.
   - Current: `qrSVG()` is a deferred stub returning `""` ([internal/setup/qr.go](internal/setup/qr.go)); only a terminal ASCII QR is produced.
   - Target: `qrSVG(deepLink)` generates a valid SVG QR (boombuler/barcode, MIT — a new Go dep) encoding the exact `https://t.me/<bot>?start=<token>` URL, returned in the `qr_svg` field; an empty/blank deep-link yields no QR (never a QR of an empty/garbage URL).
   - Acceptance: a unit test decodes the generated SVG's QR payload and asserts it equals the input deep-link; `qrSVG("")` returns `""`.

2. **Cockpit onboard-link API**: New cockpit REST endpoints mint a re-mintable deep-link + SVG QR behind cockpit auth.
   - Current: minting is only reachable on the loopback `:9081` server gated by the one-time setup token; no cockpit route exists.
   - Target: cockpit `/api/telegram/*` route(s) (siblings under the `/api/` exclusion carve-out, never bare `/api/`) mint `{deep_link, qr_svg}`, reusing the `internal/setup` mint/consume logic, gated by `RequireAuth` + `RequireCapability` (parity with `POST /agent/run`), independent of the `:9081` one-time setup token.
   - Acceptance: an authenticated capability-bearing `POST` returns a distinct valid deep-link + non-empty `qr_svg`; an unauthenticated request 401s and a no-capability request 403s; two sequential mints return two distinct, independently-consumable tokens.

3. **Cockpit bot-token configuration (validate + persist, apply on restart)**: The operator sets the Telegram bot token from the cockpit.
   - Current: the bot token is env-only (`TELEGRAM_BOT_TOKEN`, read once at boot); the `:9081` `/setup/token` only validates in memory and never persists.
   - Target: a cockpit endpoint getMe-validates the submitted token and persists it to a secret-classified store; the running channel adopts it on the **next daemon restart** (no hot-reload this phase); status reports `bot_configured` + a `needs_restart` indicator when a newly-persisted token differs from the running one. The token is never logged, echoed, or returned.
   - Acceptance: submitting a valid token returns `{ok:true, bot_username}` and persists it; restarting the daemon boots the channel on the persisted token; submitting an invalid token returns a 400 with a non-leaky message; no response/log line contains the token bytes.

4. **Live confirmation naming the linked user**: When an onboarding token is consumed, the cockpit shows a live confirmation naming the linked Telegram user.
   - Current: the `SetupEvent` `telegram_user_id`/`username` fields exist but stay unset — the schema has no token→consumer link, so the SSE only signals "an account linked".
   - Target: migration `0021` records the consuming `telegram_user_id` + `username` on the channel `/start` consume; the consume path writes them; the events stream populates the `onboarding_completed` event with them; the cockpit renders "linked @username" live, tolerating an **absent** username (Telegram usernames are optional) by falling back to the user id / a generic "account linked".
   - Acceptance: after a simulated consume, the SSE event carries the recorded `telegram_user_id` (and `username` when present); the cockpit confirmation shows the username when present and a non-broken fallback (never "@" / "@undefined") when absent.

5. **Onboarding wizard step (skippable)**: The link+QR is a skippable step in the Phase-28 onboarding wizard.
   - Current: no onboarding wizard step for Telegram (no telegram/onboarding surface in `web/src`).
   - Target: a reusable React component rendered as an explicit step in the Phase-28 wizard with a Skip action; skipping completes onboarding with no linked account and no error.
   - Acceptance: the wizard advances past the step both on a successful link and on Skip; on Skip onboarding completes and no `telegram_setup_pending`/account row is required.

6. **Settings "Connect Telegram" panel**: The same component is reachable post-onboarding under the settings shell mode.
   - Current: the `settings` shell mode is a placeholder (not in `LIVE_MODES`); no Connect-Telegram panel.
   - Target: the same link+QR component is mounted as a "Connect Telegram" panel under the `settings` surface for post-onboarding linking/re-minting; when no bot token is configured the panel shows a "configure the bot first" state rather than an opaque mint error.
   - Acceptance: the settings panel renders the link+QR for an operator who has already onboarded; with no bot token configured it shows the configure-first state (no failed mint surfaced as a raw error).

7. **Re-mintable link with expiry**: The cockpit reflects the 1h single-use TTL and lets the operator mint fresh.
   - Current: links carry a 1h TTL + single-use consume guard, but nothing surfaces expiry or re-mint to a user.
   - Target: the surface shows time-remaining and an expired state, and offers a "generate new link" action that mints a fresh deep-link + QR; an expired or already-consumed token's `/start` stays rejected by the existing channel consume guard.
   - Acceptance: the UI shows an expired state once the TTL elapses and a re-mint produces a new working link; a `/start` with an expired or already-consumed token is rejected (no second consume).

## Boundaries

**In scope:**
- Server-side SVG QR generation (implement the deferred `qrSVG`, new boombuler/barcode dep).
- New cockpit `/api/telegram/*` endpoints (mint link+QR, set bot token, status, SSE confirmation) behind `RequireAuth` + capability.
- Bot-token validate + persist to a secret-classified store; applies on next daemon restart; `needs_restart` status.
- Migration `0021` recording the consuming `telegram_user_id` + `username`; channel consume path + SSE event populate them.
- A reusable React link+QR component used in BOTH the Phase-28 onboarding wizard (skippable step) AND a settings "Connect Telegram" panel.
- Re-mintable link with time-remaining / expired state.
- Live SSE confirmation naming the linked user (with absent-username fallback).
- en + it i18n bundles for all new copy; WCAG AA on the new surface.

**Out of scope:**
- Removing/replacing the `:9081` loopback setup wizard — left untouched as the headless bootstrap path (operator decision R3); Phase 30 only ADDS the cockpit surface.
- Multi-user / per-identity linking — linking targets only the seeded `local` identity (multi-user is an explicit v2 deferral in REQUIREMENTS).
- Unlink / remove an already-linked Telegram account from the cockpit — link-only this phase (no delete endpoint/UI).
- Hot-reload of the bot token into the running channel without restart — deliberately deferred (no per-channel restart seam built); token applies on restart.
- The Phase-28 onboarding LoopAgent / `Agent.md` identity flow itself (ONBD-01/02) — Phase 30 only adds the Telegram step into that existing wizard.

## Constraints

- The new cockpit endpoints mount in `cmd/aura/serve_webui.go` as specific `/api/telegram/*` siblings under the `/api/` exclusion carve-out — never a bare `/api/` (would shadow `/api/integrations/`, T-24-07). Mutating routes use `RequireCapability` (parity with `POST /agent/run`); reads inherit `RequireAuth`.
- The cockpit surface MUST NOT depend on or trigger the `:9081` one-time setup-token invalidation — re-mintability requires each mint to be independent.
- The persisted bot token is a secret: classified by `IsSecretEnvKey`-equivalent, never logged/echoed/returned, never readable by a `shell_exec` child or written world-readable (HARDEN-04 secret boundary is canon; this store inherits the obligation).
- boombuler/barcode (MIT) — license is permissive (compatible with the DGX-Spark commercial bundle, unlike the open NVL question for Phase 27).
- Owned-surface coverage floor ≥85% (CLAUDE.md); frontend quality gates: vitest coverage ≥85% + Stryker mutation ≥70% (operator directive). No-skip-as-green: integration tiers must execute, not skip, under `$CI`.
- All new user-facing copy added to BOTH `web/src/i18n/resources.ts` en + it bundles; `aria-invalid` omit-when-valid pattern on any token-entry input; WCAG AA contrast on the blue theme.

## Acceptance Criteria

- [ ] `qrSVG(deepLink)` returns an SVG whose decoded QR payload equals the deep-link; `qrSVG("")` returns `""`.
- [ ] An authenticated, capability-bearing cockpit mint request returns a distinct deep-link + non-empty `qr_svg`; unauth → 401, no-capability → 403.
- [ ] Two sequential cockpit mints return two distinct, independently single-use tokens (concurrent mints race-clean).
- [ ] Submitting a valid bot token persists it and returns `{ok:true, bot_username}`; after a daemon restart the channel runs on the persisted token.
- [ ] Submitting an invalid bot token returns 400 with a non-leaky message; no response body or log line contains the token.
- [ ] Re-submitting a bot token is idempotent (upsert / last-write-wins, re-validated each call; no torn write under concurrency).
- [ ] After a consume, the `onboarding_completed` SSE event carries the consuming `telegram_user_id` (+ `username` when present); the cockpit shows "linked @username" or a non-broken fallback when the username is absent.
- [ ] The onboarding step advances on both a successful link and Skip; Skip completes onboarding with no linked account and no error.
- [ ] The settings "Connect Telegram" panel renders the link+QR post-onboarding; with no bot configured it shows the configure-first state, not a raw mint error.
- [ ] The surface shows an expired state after the 1h TTL and a re-mint produces a fresh working link; a `/start` with an expired/consumed token is rejected.
- [ ] The `:9081` loopback wizard still works unchanged (no regression in its routes/one-time-token behavior).
- [ ] New copy present in both en + it i18n bundles; new surface passes the WCAG AA contrast gate.

## Edge Coverage

**Coverage:** 8/9 applicable edges resolved · 0 unresolved

| Category | Requirement | Status | Resolution / Reason |
|----------|-------------|--------|---------------------|
| empty | R1 | ✅ covered | `qrSVG("")` returns `""`; onboard-link 409s when no bot configured → no QR of an empty/garbage URL (AC1) |
| encoding | R1 | ⛔ dismissed | Deep-link payload is ASCII-only (`t.me/<[A-Za-z0-9_] username>?start=<UUID>`); no unicode/normalization/grapheme surface in the QR payload |
| concurrency | R2 | ✅ covered | Concurrent mints INSERT distinct UUID rows → distinct, independently single-use tokens; race-clean (AC3) |
| idempotency | R3 | ✅ covered | Set-bot-token is upsert / last-write-wins, getMe re-validated each call; no duplicate side effect (AC6) |
| concurrency | R3 | ✅ covered | Secret-store write is atomic; concurrent set-token = last-write-wins, no torn persisted value (AC6; race test in plan) |
| unclassified | R4 | ✅ covered | Absent Telegram username (usernames are optional) → confirmation falls back to user id / "account linked", never "@undefined" (AC7) |
| unclassified | R5 | ✅ covered | Skip completes onboarding with no linked account/row and no error; re-link later via settings (AC8) |
| unclassified | R6 | ✅ covered | No-bot-configured → settings/onboarding surface shows configure-first state, not an opaque mint error (AC9) |
| unclassified | R7 | ✅ covered | Expired/consumed `/start` rejected by the existing consume guard; cockpit shows expired state + re-mint (AC10) |

## Prohibitions (must-NOT)

**Coverage:** 5/5 applicable prohibitions resolved · 0 unresolved

| Prohibition (must-NOT statement) | Requirement | Status | Verification / Reason |
|----------------------------------|-------------|--------|------------------------|
| MUST NOT log, echo, or return the submitted/persisted Telegram bot token in any response, error message, or log line | R3 | resolved | verification: test (handler error path + log-scan assert no token bytes; `T-13-07-BotTokenLeak` parity) |
| MUST NOT expose the cockpit mint-link or set-bot-token endpoints without authentication + the mutating-write capability | R2, R3 | resolved | verification: test (unauth → 401; no-capability → 403; parity with `POST /agent/run`) |
| MUST NOT persist the bot token where a `shell_exec` child / non-secret env / world-readable artifact can read it | R3 | resolved | verification: judgment (secret-at-rest; HARDEN-04 / `IsSecretEnvKey` is canon — this store inherits the obligation) |
| MUST NOT couple the cockpit mint/completion lifecycle to the `:9081` one-time setup-token invalidation (would break re-mintability + the loopback bootstrap) | R2, R7 | resolved | verification: judgment (cockpit mint independent of `setup.Token.Invalidate`) |
| MUST NOT record or display Telegram PII beyond `telegram_user_id` + `@username` (no phone, no full name/contact) | R4 | resolved | verification: judgment (migration `0021` + display surface only id + username) |

Canon-referral (not minted here): deep-link injection / SSRF / open-redirect on the `t.me` URL — owned by `/gsd-secure-phase` (the bot username is from a trusted getMe, the token is a server-minted UUID; no attacker-controlled interpolation).

## Ambiguity Report

| Dimension          | Score | Min  | Status | Notes                                                        |
|--------------------|-------|------|--------|--------------------------------------------------------------|
| Goal Clarity       | 0.90  | 0.75 | ✓      | Deliverable + placement + QR approach + token effect locked  |
| Boundary Clarity   | 0.88  | 0.70 | ✓      | In/out explicit; multi-user, unlink, :9081-removal out       |
| Constraint Clarity | 0.82  | 0.65 | ✓      | New secret store + migration 0021 + boombuler dep + auth model|
| Acceptance Criteria| 0.80  | 0.70 | ✓      | 12 pass/fail criteria                                        |
| **Ambiguity**      | 0.14  | ≤0.20| ✓      |                                                              |

Status: ✓ = met minimum, ⚠ = below minimum (planner treats as assumption)

## Interview Log

| Round | Perspective     | Question summary                                  | Decision locked                                                        |
|-------|-----------------|---------------------------------------------------|-----------------------------------------------------------------------|
| 1     | Researcher      | Where does the surface live?                      | Telegram-link step in the Phase-28 onboarding wizard                   |
| 1     | Researcher      | QR generation approach?                            | Server-side SVG (implement deferred `qr_svg`, boombuler/barcode)       |
| 1     | Simplifier      | Cockpit sets the bot token, or env-only?           | Cockpit can set the bot token                                          |
| 2     | Boundary Keeper | What does setting the token DO?                    | Validate + persist + (initially hot-restart) — see round 3            |
| 2     | Boundary Keeper | Completion confirmation mechanism?                 | Live SSE confirmation                                                  |
| 2     | Failure Analyst | Link expiry / regeneration behavior?              | Re-mintable link, shows expiry                                         |
| 3     | Failure Analyst | Token effect given no secret store / no restart seam? | Persist + apply on restart (minimal-industrial; one new mechanism) |
| 3     | Boundary Keeper | Explicit out-of-scope?                             | :9081 removal OUT (kept)                                               |
| 4     | Seed Closer     | Pull WHO-linked / multi-user / unlink into scope?  | WHO-linked IN (needs migration 0021); multi-user + unlink OUT         |
| 5     | Seed Closer     | Onboarding step optional or required?              | Optional / skippable                                                   |
| 5     | Seed Closer     | Post-onboarding re-link entry point?              | Add a settings "Connect Telegram" panel (reusable component, "both")  |

---

*Phase: 30-telegram-onboarding-on-frontend-with-link-and-qr-code*
*Spec created: 2026-06-19*
*Next step: /gsd-discuss-phase 30 — implementation decisions (secret-store medium, channel-restart wiring, endpoint shapes, component layout)*
