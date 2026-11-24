# PIM tool split: verification ledger

Plan: `docs/superpowers/plans/2026-11-23-pim-tool-split.md`. Spec:
`docs/superpowers/specs/2026-11-23-pim-tool-split-design.md`. Lab VM `192.168.101.158`. Each part of
the plan appends its section here.

## Part A: the PIM pinned by sha (2026-11-24)

Release `713e5e169`, carrying `45bdb11ff`..`713e5e169`. Applied through the updater on the operator's
"Aggiorna subito" at 08:20 UTC.

| Check | Result |
|---|---|
| Aura revision after the apply | `713e5e169` |
| updater log | `env: retired AURA_PIM_MCP_IMAGE removed from .env.` |
| `AURA_PIM_MCP_IMAGE=` lines in the appliance `.env` | 0 (was 1) |
| PIM container image reference | `ghcr.io/chetto1983/aura-pim-mcp:43c49b31c11eebfc041cdd5031cdc3cd45021f3a` (was `:sidecar`) |
| PIM image digest, before and after | unchanged (`sha256:4bc5…9054`): the pin is the revision that already ran |
| PIM recreated by the updater's sidecar step | yes, 08:21–08:22 UTC, with no manual step |
| `list_accounts` through the mounted PIM after the recreate | 3 accounts, same as before |

What this does not show:
- That a push to the fork's `main` no longer reaches the VM. Task B5 records that, after the fork's
  first push.
- A fresh install. `install_env.sh` no longer writes the key; that is unit-tested
  (`TestRetiredEnvKeysStayRetired`), not run on a clean host.
