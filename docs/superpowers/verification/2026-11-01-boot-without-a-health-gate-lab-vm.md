# Lab VM reboot: a boot that no longer waits on aura's health

Date: 2026-11-01. The lab VM (192.168.101.158) ran `1344ba204`, which carries `06060f6b5` (the
memory readiness probe reaches no embedding route) and `4ea413677` (no service gates on aura's
health, and aura's `start_period` is 300 s). This run reboots it to check those two fixes on a real
boot.

**Verdict:**
- aura.service finished, and aura was healthy with caddy, prometheus and tempo all running, 96 s
  after the boot.
- `/readyz` failed once, one second after the listener opened, and answered 200 from then on.
- One graceful reboot is one sample, and it was not the morning's worst case: see "What this does
  not show".

## The failure being fixed

On the boot of 13:34 UTC the cockpit never came up. `aura.service` failed at 13:39:41 with
`dependency failed to start: container aura is unhealthy`, and `aura-caddy` stayed Created.

A capture on the memory sidecar placed the time, the same on every failed probe:
- ArcadeDB answered the three gate counts in 28 ms.
- The embedding request left for OpenRouter, and nothing came back in the remaining 1.97 s of the
  2 s budget.

`/readyz` failed from 13:39 to 13:44, passed for about a minute, and failed again from 13:46
until the fix arrived. The listener had opened 60 s after the container started, past the 45 s
`start_period`, which left two real attempts before compose gave up. `prd.md` §19 records the
measure and the decision.

## After the update, before the reboot

Read at 15:08 UTC on `3656f6fe1`, which the operator applied from the cockpit at 14:57:
- aura `healthy`, 0 readiness failures in that container, 11 minutes after it started;
- `/readyz` 200 in 0.07 s, three times out of three;
- `/opt/aura/compose.yaml` holds `service_started` on all six dependencies on aura.

## This boot

`systemctl reboot` was sent at 15:38:07 UTC. Times in UTC, seconds from the kernel's boot:

| Event | Time | Offset |
|---|---|---|
| VM up | 15:38:41 | 0 s |
| aura container started | 15:39:13 | 32 s |
| `aura.service` starting | 15:39:25 | 44 s |
| first sample: caddy, prometheus and tempo running, aura `starting` | 15:39:54 | 73 s |
| `aura.service` finished | 15:39:54 | 73 s |
| aura's listener opened | 15:40:07 | 86 s |
| the one readiness failure, `"probe":"memory"` | 15:40:08 | 87 s |
| aura `healthy` | by 15:40:17 | 96 s |

- `aura.service` finished 13 s before aura was listening. On the morning's boot it waited for aura
  and failed.
- The listener opened 54 s after the container started, past the old 45 s window.
- `/readyz` then answered 200 three times: 1.71 s, 0.05 s, 0.04 s.
- `https://127.0.0.1/` answered 401 in 0.02 s, the cockpit's login gate behind caddy.
- The stack ended up on `1344ba204`, with the updater at `state=current`.

Between 73 s and 86 s caddy was up with no listener behind it. The sample did not read what a
browser sees in that gap; the operator looked during it and reported that the VM was not up yet.

## What this does not show

- **A silent OpenRouter.** Its state during this boot was not controlled or read. What holds is
  structural: the probe no longer reaches the route, which `TestFactsAboutReachesNoEmbeddingRoute`
  pins with a control.
- **A dirty shutdown.** This was a graceful reboot, so ArcadeDB's crash recovery did not run as it
  did that morning.
- **A reboot with an update pending.** Nothing was held, so compose recreated nothing. That
  morning it replaced the whole stack on the boot, applying `bd994a7ef` without its admin. That
  defect is still open (`prd.md` §19).
- **The cockpit's badge.** The chip's panel is covered by vitest and the Playwright run in CI, not
  by this run.
- **A sandbox failure at 15:27:46.** `docker logs aura` holds one `"probe":"sandbox"` line from
  before this boot. It was not investigated.
