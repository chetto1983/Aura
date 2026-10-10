# A new tenant's memory opened by several resolvers at once

Date: 2026-10-10. Trees: master `d25c4a027` and the fix (`ccr-56123334-7n4k4s-arcadedb`).
Scope: `TenantClients.For` on a tenant whose ArcadeDB memory database does not exist yet.

Not the lab VM. A cloud container with Docker: the compose `arcadedb` service (26.10.1) and
`postgres`, `aura serve --only=cli` built from each tree with `AURA_PROFILE=dev`, one `user`
identity besides the operator.

## What this run does not prove

- **Other processes.** The fix serializes the resolvers of one process. The memory sidecar
  (`cmd/arcadedb-mcp`) and the ingestion sidecar keep their own; whether either opens a new
  tenant while `aura serve` does was not measured. At boot `aura serve` reconciles its
  tenants before its listener opens, and the memory sidecar is only called through that
  listener, so the boot race above cannot involve it; that is read in the code.
- **Why ArcadeDB fails.** Its documentation says `IF NOT EXISTS` returns no error when the
  property exists and says nothing about concurrent DDL. The failures below are measured,
  not explained.
- **The appliance.** `aura serve` ran on the host, not in its container.

## Measured

| What | master | fix |
|---|---|---|
| `TestIndependentResolversOpenANewTenantTogetherLive`: 4 resolvers open a new tenant together, 5 tenants | 5 of 5 tenants failed: 500 `Cannot create type 'Entity' because already exists`, `Cannot create the property 'created_at' in type 'FACT' because it already exists`, and a `NullPointerException` in ArcadeDB's SQL parser (`Identifier.getValue()` on a null `this.name`) | 15 of 15 tenants opened, over 3 runs |
| `aura serve` boot after dropping the member's `mem_…` database | exited 6 of 8 boots: `reconcile ArcadeDB tenant …: schema for …: ensure memory schema` (or `ensure reasoning memory schema`) | up 8 of 8 boots |
| `TestIndependentResolverWaitsAndHonorsContext` (no ArcadeDB) | fails: the second resolver ran schema DDL while the first was running it | passes, 5 runs under `-race` |

The boot failure is the one first seen in
`2026-10-10-member-deployment-administration.md`: the tenant reconcile, the conversation
projection reconcile and the reasoning retention each hold their own `TenantClients`, so the
per-resolver gate never saw the others; the reconcile's error ends `aura serve`.
