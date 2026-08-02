# MUSR isolation runbook (`AURA_MUSR_ISOLATION`)

`AURA_MUSR_ISOLATION` (`internal/config/config_knobs.go`, default **off**) is the
deployment switch that declares a deployment fit to host more than one identity. This
runbook is the operator procedure for turning it on.

> **There is no data migration and no ordering rule.** An earlier version of this runbook
> required an owner-edge backfill before the flip, because the documents plane then lived
> in a graph where ownership was an edge that pre-isolation documents did not have. The
> documents plane is Postgres now and ownership is a column, so there is nothing to
> backfill and no window in which the operator's own library can go invisible.

## What the flag actually does today

Document retrieval is identity-scoped **unconditionally**, in SQL, whatever the flag says:
`PostgresCatalogStore.SearchDigests` takes the identity as a parameter and the query filters
on it, `SetDigest` is scoped the same way, and an empty or malformed identity is rejected by
`pgUUID` before a statement runs — so an unresolved principal returns nothing rather than
everything. Owner-scoped row-level security sits underneath as the backstop
(`db.WithIdentityTx`, migration `0032`).

What the flag gates is therefore not a query path but three deployment controls:

| Where | Behaviour when the flag is **off** |
|---|---|
| `config.gateMUSRIsolation` (`config_validate.go`) | **Fatal** config violation under `server_production` only. The lenient tiers and `single_user_hardened` are single-principal and do not require it. |
| The onboarding saga (`agui.errIsolationDisabled`) | **Refuses** to provision a second identity. |
| `aura serve` boot check | Emits a loud WARN when more than one live identity already exists. |

## Procedure

1. Set the flag in the environment / `.env`:

   ```sh
   AURA_MUSR_ISOLATION=true
   ```

2. Restart `aura serve`.
3. Confirm with `aura config validate --profile server_production` — it must report no
   `AURA_MUSR_ISOLATION` violation.

Provisioning additional identities through the onboarding saga is available from that point.

## Reversibility

Flipping back to **off** re-arms the provisioning refusal and the boot warning; it destroys
no data and does not widen any read, because scoping is not conditional on it.

## Acceptance

- `go test -tags 'db_integration garage_integration authula_integration musr_e2e' ./cmd/aura/`
  — the two-identity cross-deny live E2E (`provision → login → isolated run`), which proves
  identity B is denied on every plane while A keeps its data (D-29 / MUSR-01).
