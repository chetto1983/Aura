# Aura Tempo block-list runbook

Applies to `AuraTempoBlocklistFailing`.

## Meaning

Tempo has failed to list the trace blocks of a tenant for at least fifteen minutes. While this lasts, Tempo still reports ready and keeps accepting spans, but it finds no stored trace and its retention deletes nothing. `tempodb_blocklist_length` keeps reporting the last good list, so it does not show the failure; `tempodb_blocklist_tenant_index_errors_total` does, adding 2 for every failed poll.

The cause measured on the lab VM is a block whose `meta.json` or `meta.compacted.json` was left empty by a power loss. Tempo's local backend writes the meta without fsync, and one unreadable meta aborts the poll of the whole tenant (grafana/tempo#8054). The `tempo-blocks-repair` service removes such blocks on every `docker compose up`, the one at boot included, so this alert covers a meta emptied while Tempo was running.

## Drilldown and correlation

Open panel 6 of `aura-data-retention`. From the Aura install directory (`/opt/aura` on the appliance), read Tempo's reason:

```sh
docker compose logs --since 1h tempo | grep 'failed to poll'
```

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
