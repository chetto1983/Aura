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

`TestTwoIdentityCrossDeny` (`cmd/aura/two_identity_e2e_test.go`) is the live proof for the
first five rows. Its assertions never consult this flag.

## Why the flag still exists — what is NOT scoped

A second principal would **share** these with the operator:

| Plane | Why it is shared |
|---|---|
| Skills library | `skillLoaderRoots` (`cmd/aura/serve_adapters.go`) resolves `{<export>/.agents/skills, cfg.SkillsDir}` — no identity component. The model-facing `skill` tool can create/update/delete there, so `agent.run` alone is enough to read and rewrite the operator's skills. A per-identity rooting primitive was written in Phase 36 and never called; it was **deleted** rather than left standing as a fix that looks applied. |
| `aura.settings` | Keyed by `key` alone (migration `0024`) — no identity column, no RLS. It carries `OPENROUTER_API_KEY`, `TELEGRAM_BOT_TOKEN` and `AURA_LLM_BASE_URL` for the whole daemon. Secrets are redacted on read, but `governance.write` can overwrite them. |
| MCP catalog | One shared `servers.json`, read ONCE at process boot (`config.loadMCPServers`) and mounted process-wide before any identity exists. A per-identity overlay (`managed_config_identity.go`) was written in Phase 36 and never called; it was **deleted**, and it would not have sufficed anyway — it could only toggle enable/trust over the same shared catalog, never give an identity its own connector or its own credential. |
| Governance scheduler board | `ListManageableTasks` / `ApproveTask` / `RunTaskNow` / `UpdateTask` address tasks by id and status only, with no owner predicate and no RLS. |
| ~~Host filesystem and shell~~ | **No longer shared.** `SandboxRouter.Route` (`internal/sandbox/usersandbox/router.go:80`) has no profile branch and no host arm: every profile routes into the caller's own box, and an unreachable backend denies rather than falls back (D-09/GATE-01). `shell_exec` and the `fs_*` tools no longer reach `.env` or the daemon's filesystem. |

The capability grants (`agent.run`, `governance.read`, `governance.write`) are the only
control on the middle four; the last one needs no capability beyond `agent.run`.

## Procedure

Do **not** set this flag on a deployment you share with someone you would not hand the
`.env` to. Turning it on is safe only once the table above is empty. In dependency order:

1. Root the skills tool per identity and give each box its own materialize source.
   This is a BUILD, not a wire: the Phase-36 primitive that once made it look like a
   wire has been deleted.
2. Scope `aura.settings` per identity, or make it admin-only and stop treating
   `governance.write` as a per-user capability.
3. Same for the MCP catalog.
4. Add an owner predicate to the scheduler board's list and mutate paths.
5. Make the sandbox route under every profile a second identity can log into, or fence the
   host filesystem tools when the principal is not the operator.

Only then:

```sh
AURA_MUSR_ISOLATION=true
```

Restart `aura serve`. Provisioning through the onboarding saga is available from that
point.

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
