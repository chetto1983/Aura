# Lab VM reboot: readiness at boot, and broker-analysis's first start

Date: 2026-10-30. The lab VM (192.168.101.158) was updated through the cockpit's updater to
`aabc5f5dc`, then the operator rebooted it. This run checks two fixes from 2026-10-29 on that
real boot:

- `7d400f958`: a tenant's memory open is no longer cancelled by the readiness probe that started it;
- `51f500b4e`: the box keeps uv's downloaded Pythons on a volume, `uv-data`.

**Verdict:**
- aura was healthy and caddy running 79 s after the reboot, with no memory probe failure.
- broker-analysis's board probe passed on every attempt after the reboot.
- One reboot is one sample. The box has not yet been recreated with Python already in the volume.

## Boot readiness (`7d400f958`)

The failure being fixed: after the reboot of 2026-10-28 09:34 UTC, `/readyz` failed for 6.5
minutes on `"probe":"memory"` with `context deadline exceeded`. compose gave up at about 2m15s
and left `aura-caddy` Created, so the cockpit was unreachable.

The cause was measured on 2026-10-29:
- The first `memory_search` for a tenant first sends 125 schema requests. They took 2.55 s on a
  freshly restarted ArcadeDB and 0.93 s on a warm one.
- The probe's 2 s budget cancelled that work half-way, and every probe started it again from the
  first statement.

This boot, in UTC:

| Event | Time |
|---|---|
| VM up | 07:05:32 |
| `aura-arcadedb-mcp` started | 07:06:27 |
| `aura-caddy` started | 07:06:51 |
| first sample: aura `healthy`, caddy `running`, `/readyz` 200 in 0.42 s | 07:06:57 |

`docker logs aura` since the boot holds no `"probe":"memory"` line.

## broker-analysis's first start (`51f500b4e`)

The failure being fixed: the MCP board sometimes showed broker-analysis as "Errore — dial
failed" while its tools worked.

The cause was measured on 2026-10-29:
- The server needs Python 3.14 and the box has 3.11, so `uvx` downloads one into
  `/root/.local/share/uv`, which was not on a volume.
- After a box recreation the first start took 25 s against 7 s warm. The board's probe has 15 s.
- Reproduced through the board: with the Python removed, the first probe failed at 15.0 s with
  `context deadline exceeded`, and the next ones passed in 6.4 s.

This run:
- The update recreated the box at 07:03:03, and it now mounts
  `aura-box-<id>-uv-data -> /root/.local/share/uv`.
- `/root/.local/share/uv/python` holds `cpython-3.14.6-linux-x86_64-gnu`.
- After the reboot, three board probes of `/api/governance/mcp/broker-analysis/probe` passed:
  `ok (14 tools)` in 5.0 s and 3.5 s, and the third also passed.

## What this run does not prove

- **That boot readiness always passes.** One boot is one sample, and this one had no ArcadeDB
  crash recovery to do. The 2026-10-28 boot did, so it was not the same load.
- **That the Python survives a recreation.** The box was recreated before the Python was in the
  volume, then only restarted. The next image update will recreate it with the Python already
  there; that start should not download it again.
- **That a start always fits the board's 15 s.** `uvx` still re-resolves the `v0.3.0` git tag on
  GitHub at every start, about 2.3 s; a commit pin takes 0.4 s. The operator chose to leave that
  as it is.
