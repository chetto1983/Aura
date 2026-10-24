# Appliance updates: the night instead of the idle spell

**The operator asked** on 2026-10-24. Andrea complains that updates arrive without notice. Andrea
was working when one arrived, so the cockpit should have warned them. **The operator approved**
the same day:
- (a) a message on the operator's channel before and after an update, with what changed;
- (b) a daily update window;
- (d) docs that say what the updater does. This one landed in `da6781b`.

**The operator then decided the shape.** In order:
- "lascerei sempre il PopUp con aggiornamento automatico alle 3:00 locali";
- "si lascia il modal: se l'utente vuole aggiornare aggiorna, se no si applica di notte";
- "basta che togli solo l'inattività".

So the first draft's U1–U5 (`cc89c1a`) are withdrawn: the countdown, the presence beat, the
window, the members' notice and the measurement rule. A second draft also removed the
deferral and gave members the modal; it went further than asked and was dropped before push.

## What existed, read in the tree on 2026-10-24 at `da6781b`

- **The cadence.**
  - `deploy/aura-image-update.timer` ticks 5 minutes after the last tick ended.
  - Each tick pulls everything new. `publish-aura-edge.yml` publishes on every push to master
    that changes the image: 7 successful runs on 2026-10-24.
- **When a tick restarts Aura** (`deploy/aura-update-consent.sh` `decide_update`):
  - an admin's request;
  - the host restarted and Aura already runs the new image;
  - `aura_is_idle`: no use for `AURA_UPDATE_IDLE_SECONDS` (900), and no deferral in force.

  The deadline (`MAX_DEFER`, 24 h from the first pending build) only ends a deferral
  (`scripts/aura_update_consent_test.sh`, "the deadline passed while idle").
- **What counts as use.** `cmd/aura/serve_host_update.go` writes `update/activity` every minute
  from two queries (`internal/db/queries/appliance_activity.sql`):
  - the newest `tool_invocations.ts` and the running jobs' heartbeats;
  - each identity's newest `conversations.last_active_at`.

  Nothing else counts. A person who reads documents, edits in a Craft app, browses files or
  changes settings without a chat turn is idle.
- **When the measurement fails, Aura counts as down.** If `Activity` returns an error,
  `ActivityWriter.publish` writes nothing (`internal/hostupdate/activity.go`). After
  `ACTIVITY_STALE_SECONDS` (180) the updater reads the stale file as "Aura is down", and down
  counts as idle. One identity whose transaction fails is enough: every build then applies on
  arrival.
- **No time between decision and restart.** An unattended apply goes from `pending` to
  `applying` in the same tick (`apply_update`), then `compose up` replaces aura.
- **What the cockpit shows.**
  - The SPA polls `GET /api/system/update` every 60 s, and every 3 s in `requested` or
    `applying`.
  - `SystemUpdateLayer` shows everyone a banner only in `requested`. That state exists from an
    admin's POST until the updater's next pass, a few seconds since `aura-update-request.path`
    fires at once.
  - The overlay shows in `applying`, or while the daemon is unreachable after a request.
  - A member whose 60 s poll misses `applying` sees failed requests, then a reload when the new
    revision answers (`useSystemUpdate`).
  - The admin dialog opens once per build per browser session, showing the revision and the
    build time, not what changed.
- **Reaching a person outside the cockpit.**
  - `channels.Registry.DeliverToIdentity(ctx, identityID, text)` (`internal/channels/registry.go`)
    pushes to the identity's Telegram DM. Telegram is the only implementation; it returns false
    when the identity has no linked account.
  - The scheduler's `cron.Notifier` also routes to WhatsApp and to email through the PIM.
  - There is no configured operator or admin contact, no Web Push, and no server-fed cockpit
    inbox.
- **No record of cockpit use.** There is no presence, last-seen or heartbeat endpoint.
  - Authula renews a session at most once an hour (`internal/webauth/session_renew.go`).
  - Counting authenticated requests would count the shell's own polling. `/healthz`,
    `/readyz` and `/api/approvals` run every 5 s in any visible tab, whether anyone is looking
    or not.

## What is not known

- **Why Andrea's restarts happened.** `journalctl -u aura-image-update` on their host names each
  apply's reason ("nobody is using Aura", "an admin asked for it", "the host restarted"). Aura's
  log shows whether the measurement failed: "host update: activity not measured".
- **Whether the reload loses work.** Unsaved edits in a Craft editor or a draft in the composer
  may be lost. Nobody has checked.
- **Andrea's role.** An admin sees the dialog, a member does not.

## Decisions

**N1. The idle rule becomes the night, and nothing else changes.**
- The unattended reason "nobody is using Aura for 15 minutes" (`AURA_UPDATE_IDLE_SECONDS`) is
  gone.
- In its place: the first `AURA_UPDATE_AT` (default `03:00`, host local time) after the first
  pending build. It is recorded as `apply_at` in `status`, and a newer build keeps it.
- At that time a tool call or job in flight still holds the restart, and the next tick tries
  again. A stale activity report still means Aura is down.
- **The night lasts three hours** (`NIGHT_SECONDS`). A night that a running job or a
  postponement takes up whole moves `apply_at` to the next one. Without that, the first quiet
  tick after it would restart Aura in the morning (CodeRabbit on #180).
- **Unchanged:**
  - an admin's **Update now**;
  - the reboot case;
  - the admin's modal, the postponement and its 24-hour deadline. A postponement holds the night
    back until the deadline.
- **A failed install moves `apply_at` a night later.** Past the night, every tick would otherwise
  retry it through the day: the same complaint by another road. The admin's dialog names that
  night, even past the deadline.

**N2. The modal tells the admin when.** `GET /api/system/update` returns `apply_at`, and the
headline reads "It will install itself tomorrow at 03:00". The old headline said "at the first
quiet moment, by <deadline>", which N1 makes untrue.
- A postponement that runs past the night moves the time shown to its end.
- Once the moment has passed, the updater waits only for the work in flight, and the headline
  says so.

**N3. The messages to the operator (a)** stay approved for a later step:
- to every active `identity.create` identity, through `DeliverToIdentity`;
- the commit subjects carried by the image.

## Order of landing

1. **The updater (N1):** `7b1a23e`, with `scripts/aura_update_consent_test.sh`.
2. **The daemon and the modal (N2):** `e6df958`.
3. **The docs:** `docs/INSTALL.md`, `CHANGELOG.md`.
4. **The lab VM E2E**, recorded in `docs/superpowers/verification/`:
   - a build published at noon is pending with tonight's `apply_at`, and nothing restarts in the
     day;
   - the restart happens at the first tick after 03:00;
   - the same with a job running at 03:00: it waits for the job.

   Then the PRD paragraph on appliance updates is amended from that run.
5. **N3.**

## What this does not cover

- **Andrea's journal.** It still tells which rule restarted their host. N1 removes the day-time
  rule whatever the answer.
- **The members' view.** A member still sees the banner only after an admin's **Update now**.
  Nothing restarts in the day without one anymore.
- **A stable channel for appliances that do not need edge** (option c, not chosen).
- **The first update.** The one that brings this is decided by the old rules, because its updater
  predates them.
