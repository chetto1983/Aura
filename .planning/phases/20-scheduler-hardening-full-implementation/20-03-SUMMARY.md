---
phase: 20-scheduler-hardening-full-implementation
plan: 03
subsystem: infra
tags: [scheduler, cron, channels, telegram, routing, dependency-injection]

requires:
  - phase: 20-01
    provides: channels.Registry.DeliverToIdentity fan-out + Telegram.Deliver
  - phase: 20-02
    provides: scheduler_tasks origin_conversation_id + identity_id schedule-time snapshot
provides:
  - cron-local ChannelDeliverer seam + DispatchDeps.ChannelDeliverer/.PreferOriginChannel
  - (*Dispatch).deliverToOrigin + originGate precedence helper (single-sourced)
  - config.SchedulerPreferOriginChannel via envBoolDefault(AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL, true)
  - serve boot reorder (bootChannelsAndSetup before buildDispatch) wiring Registry as ChannelDeliverer
  - LIVE Step-1 sign-off: a Telegram reminder routes back to the same Telegram chat
affects: [20-04, scheduler, channels, telegram]

tech-stack:
  added: []
  patterns:
    - "Consumer-declared cron-local interface (ChannelDeliverer) + composition-root adapter (*channels.Registry)"
    - "Default-on ops kill-switch resolved once at the root via envBoolDefault, injected (cron imports no config/channels)"

key-files:
  created:
    - internal/cron/deliver.go
    - internal/cron/deliver_test.go
  modified:
    - internal/cron/dispatch.go
    - internal/config/config.go
    - cmd/aura/serve.go

key-decisions:
  - "deliverToOrigin lives in new internal/cron/deliver.go (keeps dispatch.go under 600 LOC)"
  - "Kill-switch AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL defaults TRUE (unset/malformed → on)"
  - "AMENDED R7 (live Step-1 gate): notify=\"stdout\" is treated like an unset route (defers to origin), not an explicit channel — the agent auto-populates stdout, so the old gate re-created the Phase-19 headline bug. Only whatsapp/email pre-empt origin."

patterns-established:
  - "originGate single-sources the precedence semantics for both the live-task and swept-row (20-04) paths"

requirements-completed: [R4, R5, R7]

duration: ~50min (incl. live gate + amendment)
completed: 2026-06-11
---

# Phase 20 Plan 03: Dispatch route precedence + LIVE Step-1 Summary

**Origin-channel routing for the scheduler: a Telegram-scheduled reminder now lands back in the same Telegram DM via a default-on kill-switch, the late-bound Registry wired as `cron.ChannelDeliverer` — proven live end-to-end ("Drink water!" delivered to the origin chat).**

## Accomplishments
- `cron.ChannelDeliverer` consumer-declared seam + `DispatchDeps.ChannelDeliverer`/`.PreferOriginChannel`; `cron` imports neither `internal/channels` nor `internal/config`.
- `originGate` + `deliverToOrigin` precedence helper (new `internal/cron/deliver.go`): kill-switch + explicit-route-wins + un-owned-identity fallback + owns-but-failed → failed pending row (no double-delivery).
- `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` kill-switch (default true) resolved at the root via `envBoolDefault`, injected into `DispatchDeps`.
- serve boot reorder: `bootChannelsAndSetup` before `buildDispatch`; `buildDispatch(chat, store, reg)`; `var _ cron.ChannelDeliverer = (*channels.Registry)(nil)`.
- **LIVE Step-1 hard gate (D-04) signed off.**

## Task Commits
1. **Task 1: ChannelDeliverer seam + deliverToOrigin + kill-switch** — `bab9cefe` (feat)
2. **Task 2: kill-switch config + serve boot reorder + Registry wiring** — `ca121106` (feat)
3. **Live-gate amendment: stdout defers to origin (R7)** — `fcdd8ac8` (fix)

## Live Step-1 sign-off evidence
- **Setup:** onboarded `telegram_user_id 1148481707 → local identity`; `aura serve` (Telegram + scheduler) booted clean (boot reorder, no panic); CDP-driven web.telegram.org.
- **Trigger:** "remind me in 1 minute to drink water" in the Telegram DM → agent scheduled `kind=reminder` (with `notify="stdout"`, its default).
- **DB ground truth:** `scheduler_tasks` row had `origin_conversation_id=03b9c7c2-…` AND `identity_id=00000000-…-001` (the snapshot). ✅
- **DESTINATION:** "Drink water!" rendered in the SAME Telegram chat (CDP-observed), NOT stdout/whatsapp. ✅ (No assertion on the agent's reply text.)

## Deviations from Plan
**1. [Live-gate finding] R7 amended — `stdout` defers to origin.** The live gate exposed that the scheduling agent auto-populates `notify="stdout"` (the task tool's documented default route). The as-planned `originGate` (`notifyRoute != ""` → skip origin) treated that default as an explicit channel and routed to the server console — the exact Phase-19 headline bug. Fix (user-approved): `originGate` treats `stdout`/empty alike (defer to origin); only `whatsapp`/`email` pre-empt. Single-sourced, so 20-04's swept-row path inherits it. SPEC R7 amended; unit case `stdout route defers to origin` added (7 total). Re-verified live: the agent's default-`stdout` reminder now lands in Telegram.

## Issues Encountered
- Concurrent Phase-14 session churned the shared `cmd/aura` tree (stale IDE diagnostics, one `index.lock`); resolved by re-reading by symbol, explicit `git add`, polling the lock. Binary built from a clean worktree at HEAD to dodge Phase-14's transient `telegram` compile break.

## Next Phase Readiness
- `deliverToOrigin`/`originGate` ready for 20-04's swept-row reuse (already consumed + live-proven in Step 2).

---
*Phase: 20-scheduler-hardening-full-implementation*
*Completed: 2026-06-11*
