# Multi-identity provisioning switch (`AURA_MUSR_ISOLATION`)

`AURA_MUSR_ISOLATION` (`internal/config/config_knobs.go`, default **off**) declares a
deployment fit to host more than one identity. While it is off, the onboarding saga
refuses to provision a second one.

> **It is not a query switch.** No read path branches on it, and none ever should. An
> earlier version of this runbook described it as the selector between a scoped and an
> unscoped documents-retrieval path, and required an owner-edge backfill before the flip.
> Both are gone: the graph the documents plane lived in was deleted, ownership is a column
> now, and there is nothing to backfill. The command that version told operators to run,
> `aura documents backfill`, never existed at all — `aura docs` takes
> ingest/search/status/list.

## What is already scoped, whatever the flag says

| Plane | How |
|---|---|
| Documents | `SearchDigests` / `SetDigest` filter on the identity in SQL; `pgUUID` rejects an empty or malformed one before a statement runs, so an unresolved principal returns nothing rather than everything. |
| Conversations, turns, approvals | The `*ForIdentity` stores, with the migration-`0032` RLS policies as the kernel backstop (`db.WithIdentityTx`). |
| Shared links | Owner column plus RLS (`0041`), with a deliberate public-share carve-out for token resolution. |
| Long-term memory | One ArcadeDB database and one derived credential per identity; the server refuses cross-tenant access at the door. Created just-in-time on first use. |
| Objects / uploads | Per-identity Garage bucket and scoped key, selected per request; `IdentityStore.Resolve` fails closed on a missing row. |
| Idempotency, retention, assets | Owner column on every row. |
| MCP credentials, sessions, data | OAuth grants and runtime sessions are keyed by identity/server; the verified access-token `sub` is the tenant selector inside Calendar, WhatsApp and Memory. |
| Scheduled tasks | The model-facing `task` store derives the caller from `identityctx` and applies `identity_id` to create, list, cancel, run-now and approve. |
| Shell and filesystem | `SandboxRouter.Route` selects one box per identity on every profile and denies when the backend is unavailable. `/skills` is copied into that box; it is not a writable host mount. |

`TestTwoIdentityCrossDeny` (`cmd/aura/two_identity_e2e_test.go`) is the live proof for the
original data-plane rows. Amendment #147 records the concurrent two-subject MCP proof; the
task store and sandbox packages carry their own owner-scope tests. None branches on this flag.

## Shared administrator control planes

Some configuration is intentionally deployment-wide. Global does not mean tenant-unscoped:
these resources configure the daemon and are writable only by an administrator.

| Plane | Contract |
|---|---|
| Skills library | `skill` read/use is available to agent users. Every model-facing mutation through `skill_manage` checks the authenticated identity for `governance.write` before parsing or dispatching the action; nil/missing/error/denied checks fail closed. Changes made inside a user's copied `/skills` box tree do not write back to the host catalog. |
| `aura.settings` | Keyed deployment-wide because it configures one daemon. GET is behind `governance.read`; PUT/DELETE are behind `governance.write`; secret values are redacted on reads. |
| MCP catalog | The server catalog is deployment configuration behind `governance.read/write`. OAuth grants, credentials, client sessions and MCP data remain per identity. |
| Scheduler governance board | The operator board is a deployment-wide administrative view behind `governance.read/write`; ordinary agent task operations remain owner-scoped. |

Granting `governance.write` deliberately makes an identity an administrator of these shared
control planes. An ordinary user who only needs Aura should receive `agent.run`, not
`governance.write`.

## Why the flag still exists

The flag is the operator's explicit decision to admit additional identities, and the boot
validator accepts it only with `single_user_hardened` or `server_production`. It is not a
substitute for owner predicates and it does not select a scoped query implementation.

## Procedure

1. Select `AURA_PROFILE=single_user_hardened` or `AURA_PROFILE=server_production`.
2. Enable provisioning:

```sh
AURA_MUSR_ISOLATION=true
```

3. Restart `aura serve`, then provision the additional identity through onboarding.
4. Grant the minimum capabilities it needs. Reserve `governance.read/write` for identities
   that are intentionally trusted as deployment administrators.

## Reversibility

Flipping back to **off** re-arms the provisioning refusal. It destroys no data, does not
narrow any read, and does not remove identities already provisioned — an already
multi-principal deployment stays multi-principal, so the flag is a gate on *acquiring* a
second identity, not on serving one.

## Acceptance

- `go test -tags 'db_integration garage_integration authula_integration musr_e2e' ./cmd/aura/`
  — the two-identity cross-deny live E2E, which proves identity B is denied on every
  scoped plane while A keeps its data.
- `go test ./internal/agui/ -run TestProvisionRefused` — the refusal itself, proven to
  leave zero rows behind.
- `go test ./internal/agent/tools -run TestSkillManage` — ordinary identities cannot
  mutate the shared skill catalog; an administrator can.
- `go test ./internal/config -run TestGateMultiUserRequiresStrictProfile` — both strict
  profiles accept `AURA_MUSR_ISOLATION=true`, while development profiles reject it.
