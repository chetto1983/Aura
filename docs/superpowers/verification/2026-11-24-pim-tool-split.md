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

## Part B: the split fork published, the VM unmoved (2026-11-24)

Fork `main` pushed `43c49b3..8fbd799`, carrying the 29 per-operation tools.

| Check | Result |
|---|---|
| Fork CI Build on `8fbd799` | green |
| `aura-publish-image` on `8fbd799` | green, finished 10:40 UTC; `:8fbd79960eee2fbb2970a5a725d5759fb44ccfdb` published |
| `TestCalendarServerLive` against the published image | fails as expected: the sidecar advertises 29 tools and the test still wants the single `calendar` tool (Task C8 updates it) |
| VM updater ticks after the publish | yes, 10:42 UTC |
| VM PIM after that tick | still `ghcr.io/chetto1983/aura-pim-mcp:43c49b31c11eebfc041cdd5031cdc3cd45021f3a`, digest `sha256:4bc5…9054`, started 08:22 UTC |

A push to the fork's `main` no longer reaches the VM: the pin from Part A holds it.

What this does not show:
- A `tools/call` on each of the 29 tools against the published image. Task C8 runs the live tier
  against it.
- That Aura reads the new schemas (nullable optional types, per-tool required lists). That is Part C.
