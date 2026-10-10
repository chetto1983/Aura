# Aura Tempo block-list runbook

Applies to `AuraTempoBlocklistFailing`.

## Meaning

Tempo has failed to list the trace blocks of a tenant for at least fifteen minutes. While this lasts, Tempo still reports ready and keeps accepting spans, but it finds no stored trace and its retention deletes nothing. `tempodb_blocklist_length` keeps reporting the last good list, so it does not show the failure; `tempodb_blocklist_tenant_index_errors_total` does, adding 2 for every failed poll.

The cause measured on the lab VM is a block whose `meta.json` or `meta.compacted.json` was left empty by a power loss. Tempo's local backend writes the meta without fsync, and one unreadable meta aborts the poll of the whole tenant (grafana/tempo#8054). The `tempo-blocks-repair` service removes such blocks on every `docker compose up`, the one at boot included, so this alert covers a meta emptied while Tempo was running.

## Upstream fix and the next Tempo upgrade

grafana/tempo#8058 closed #8054 on 2026-10-09 (commit `51304f345` on `main`). The poller now skips a block whose meta is not valid JSON, counts it in `tempodb_blocklist_corrupt_block_meta_total{tenant}`, and fails the tenant only when every meta is corrupt. As of 2026-10-10 no release carries it: the latest is v3.1.0 and `release-v3.1` has no backport. Aura pins 3.1.0, so this runbook applies as written.

The upgrade that brings #8058 must change two things here:

1. A single empty meta no longer fails the poll, so `tempodb_blocklist_tenant_index_errors_total` stays flat and this alert stays silent. Add an alert on `tempodb_blocklist_corrupt_block_meta_total`, and keep this one for a tenant whose metas are all corrupt or for a backend error.
2. Keep `tempo-blocks-repair`. A skipped block is not in the block list, so retention never deletes it and a search never finds its traces; the repair stays the one thing that removes it. This is read in the #8058 code, not measured.

#8058 adds no fsync: the local backend still writes the meta with `os.Create` and `io.Copy`, so a power loss still leaves empty metas.

## Drilldown and correlation

Open panel 6 of `aura-data-retention`. From the Aura install directory (`/opt/aura` on the appliance), read Tempo's reason:

```sh
docker compose logs --since 1h tempo | grep 'failed to poll'
```

After a power loss `docker compose logs` can stop at the line the crash truncated and show nothing later; read the container's raw log instead (`docker inspect --format '{{.LogPath}}' aura-tempo-1`).

`failed reading unknown blocks: unexpected end of JSON input` means an unreadable meta. Any other reason, such as permission denied or no space left on the device, has a different cause and the repair below will not help.

## Immediate safe actions

1. Run the repair once: `docker compose run --rm tempo-blocks-repair`. It removes every block whose meta is empty, if that meta was written before the current boot or more than ten minutes ago, and logs each block it removes.
2. Do not restart Tempo. The next poll, five minutes later at most, picks up the change.
3. Never remove the Tempo volume to clear this alert. That deletes every stored trace, and Tempo 3.x cannot go back to a 2.x image without that step.

## Escalation

Escalate if errors keep growing after the repair reports no empty meta. A meta that holds invalid JSON is not empty, so the repair leaves it in place. Move that block directory out of `/var/tempo/blocks` instead of deleting it, keep it for analysis, and record Tempo's error text and the block ID.

## Recovery evidence

The alert resolves ten minutes after the last failed poll. Before closing, check three things:

- panel 6 is back at zero;
- Tempo has logged no new `failed to poll` line since the repair;
- a trace search over the previous day returns stored traces.

Record the removed block IDs from the repair log.
