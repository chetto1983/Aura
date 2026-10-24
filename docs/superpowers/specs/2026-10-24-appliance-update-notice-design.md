# Appliance updates: a notice before every restart, a daily window, cockpit use counted as use

**The operator asked** on 2026-10-24. Andrea complains that updates arrive without notice. Andrea
was working when one arrived, so the cockpit should have warned them. **The operator approved**
the same day:
- (a) a message on the operator's channel before and after an update, with what changed;
- (b) a daily update window;
- (d) docs that say what the updater does. This one landed in `da6781b`.

**Still open:** the decisions below, and the reason for Andrea's restarts. That reason is in the
host's journal, which has not been read yet (see "What is not known").

## What exists, read in the tree on 2026-10-24 at `da6781b`

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

**U1. Cockpit use counts as use.**
- The SPA sends `POST /api/system/presence` on a pointer, key or wheel event in a visible tab, at
  most once a minute. Background polls never send it.
- The daemon keeps the newest time in memory. `hostUpdateActivity` reports the later of that
  and the database's.
- A restart forgets it, which is harmless: the next input sends it again within a minute.
- This is a new endpoint, not a reuse. The inventory above found nothing that tells a person
  from a tab, so the operator confirms it before a line is written.

**U2. A notice before every unattended restart.**
- When a tick would apply because Aura is idle, it records a new state, `scheduled`, with
  `apply_at = now + AURA_UPDATE_NOTICE_SECONDS` (default 600, two ticks), and restarts nothing.
- A later tick applies once `apply_at` has passed, Aura is still idle, and the window (U4) is
  open. Any use in between returns it to `pending`, so touching the cockpit postpones the
  restart.
- The cockpit shows everyone a banner in `scheduled`: the restart time and what changed (U3).
  An admin can still update now or defer.
- An admin's request and a host restart apply as today, with no notice.
- **Order of landing.** `hostupdate.ReadStatus` rejects a state it does not know, and the
  updater writes `scheduled` while the old Aura still runs. So Aura must read `scheduled` one
  edge build before the updater writes it.

**U3. The operator hears about it, with what changed.**
- **Recipients:** every active identity holding `identity.create`, through `DeliverToIdentity`.
  An admin with no linked Telegram account gets nothing outside the cockpit; email or WhatsApp
  through `cron.Notifier` can be added if the operator wants them.
- **Messages:**
  - one when the state becomes `scheduled`, or when an admin's update starts;
  - one when the new build serves, sent by the new process;
  - one on `failed`.

  The daemon tracks what it last announced in `update/notified` (written only by Aura), so a
  restart sends nothing twice.
- **What changed.**
  - The aura image carries its commit subjects in `/usr/share/aura/changes.txt`: the edge
    checkout gets `fetch-depth: 200`, and the file holds `<sha> <subject>` lines.
  - When it schedules, the updater copies the lines between `running_rev` and `available_rev`
    into `update/changes`.
  - The message and the banner leave out `build(deps)`, `docs`, `test` and `ci` subjects, and
    show at most 15 lines plus a count of the rest.
  - A `running_rev` outside the 200 commits shows "more than 200 changes".

**U4. A daily window.**
- `AURA_UPDATE_WINDOW=HH:MM-HH:MM` in `/etc/default/aura`, in the host's local time. It uses
  `AURA_SCHEDULER_QUIET_HOURS`' format and may wrap midnight.
- Unattended applies, notice included, happen only inside it. An admin's update and a host
  restart ignore it.
- **Default:** `03:00-05:00`, set in the consent script, so it reaches existing hosts as well
  as new installs. `AURA_UPDATE_WINDOW=always` restores today's behaviour.
- With the window, a busy edge day costs one restart at night, not one per quiet spell.

**U5. A failed measurement stops counting as "Aura is down".**
- The writer still writes a fresh report, marked `measured=0`. The updater reads it as busy.
- A stale report keeps meaning that Aura is down, as today, so an update can still bring back
  a dead Aura.
- **The cost:** while the measurement fails, unattended updates wait. The admin dialog shows why.
- This reverses a choice the code states on purpose ("writes nothing rather than a guess"), so
  it needs the operator's yes.

## Order of landing

Every step ships behind the one before, on edge, each with its tests and the lab VM run recorded
in `docs/superpowers/verification/`.

1. **Andrea's journal**, to confirm which of the three reasons fired, or whether U5's case did.
2. **U5 and U1.** Aura only; the updater's consent test covers `measured=0`.
3. **Aura reads `scheduled`** (U2's first half): the API, the banner for everyone, the change
   list read from `update/changes`.
4. **The updater writes `scheduled` and honours the window** (U2, U4), with the consent test
   extended for each transition.
5. **The messages** (U3), and `changes.txt` in the image.
6. **Lab VM E2E.**
   - A member works in the cockpit without chatting: no restart.
   - Then the member stops: the banner appears 10 minutes ahead, the restart happens inside the
     window, and the admin's Telegram gets both messages.
   - The same with the activity query forced to fail: no restart.

## What this does not cover

- **A stable channel for appliances that do not need edge** (the operator's option c, not
  chosen).
- **Saving unsaved work before a reload.** First measure whether any is lost.
- **Updates that move only a sidecar or the sandbox boxes.** They go through the same decision,
  and `compose up` leaves an unchanged aura running. Whether recreating a box under a running
  tool needs its own notice is not measured.
