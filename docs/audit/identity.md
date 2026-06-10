# Audit: internal/identity

**Verdict:** needs-work — two not-wired methods (no production caller); no bugs, no races, no dead unexported code.

**Counts:** critical 0 / high 0 / medium 1 / low 1

## Findings

---

### [MEDIUM][NOT-WIRED] `Store.DeleteIdentity` has no production caller

**Location:** `internal/identity/store.go:101-106`

**Confidence:** high

**Detail:**
`(*Store).DeleteIdentity` is defined and exported but is called exclusively from integration tests (`internal/identity/store_test.go:293`, `356`). No production code path — not the CLI (`cmd/aura/identity.go` has `list|get|grant|revoke` but no `delete` subcommand), not the runner, not any other package — invokes it. The PRD spec for Slice 1.7 (§853) lists the CLI surface as `{list|get|grant|revoke}`, explicitly omitting `delete`. The method is fully exercised by the FK-cascade integration test, which is the correct verification for the migration's `ON DELETE CASCADE` constraint, but the method itself has no reachable production call site.

**Suggested fix:**
Either (a) add an `aura identity delete <name>` CLI subcommand in `cmd/aura/identity.go` if deletion is a future user-facing action (requires a PRD amendment to add it to the CLI surface), or (b) make the method package-internal (lowercase `deleteIdentity`) to reflect that it is only needed as a test helper, until a real consumer is wired. Option (b) is lower-risk and consistent with the "accept interfaces, return structs" pattern — if no external package needs it, don't export it.

---

### [LOW][NOT-WIRED] `Store.HasCapability` has no production caller

**Location:** `internal/identity/store.go:111-120`

**Confidence:** high

**Detail:**
`(*Store).HasCapability` is defined and exported but is called only from `internal/identity/store_test.go` (lines 127, 160, 172, 175, 359, 371). It is absent from the `IdentityStore` interface in `internal/runner/interfaces.go` and from every non-test call site across the repo. The PRD explicitly includes `HasCapability` in the Slice 1.7 acceptance criteria (§852) as scaffolding for future capability-gating (Slice 7, local-LLM guard, etc.), so its presence is intentional. The severity is low rather than medium because this is deliberate pre-built scaffolding, not accidentally unreachable code. However, it is currently a tested-but-not-wired method with zero production reach.

**Suggested fix:**
No immediate code change required — this is planned scaffolding. Document in a TODO comment that the first consumer (Slice 7 capability gate / Slice 1.7b runtime enforcement) should add `HasCapability` to a narrow `CapabilityStore` interface and wire it in the enforcement path. This avoids the interface being silently forgotten.

---

## What was checked

- All non-test Go files in `internal/identity/`: `store.go` (186 LOC, sole production file).
- Tests read for behavioral intent: `store_test.go` (db_integration), `store_unit_test.go` (unit), `main_test.go`.
- Generated sqlc files verified: `internal/db/sqlc/identity.sql.go`, `capability_grants.sql.go`, `models.go`, `querier.go`.
- Migration `0004_identity.up.sql` verified for schema constraints.
- Full repo grep for every exported symbol: `New`, `Store`, `Identity`, `ErrWildcardManaged`, `ErrInvalidCapability`, `ErrIdentityNotFound`, `Wildcard`, `ListIdentities`, `GetIdentityByName`, `DeleteIdentity`, `HasCapability`, `GrantCapability`, `RevokeCapability`.
- Full repo grep for import of `internal/identity` to enumerate all callers.

**Clean areas:**
- No goroutines, tickers, or resource handles — no leak risk.
- Error wrapping: all `%w` chains are correct; `errors.Is` traversal works for all sentinels.
- `fromRow` UUID conversion: safe because `id` is `PRIMARY KEY` (NOT NULL) by schema.
- `capNameRe` grammar: correct length bounds (1–64 chars) confirmed by unit tests.
- `isUniqueViolation`: correctly uses `errors.As` + `pgErr.Code`, not message substring.
- `validateGrantInput` wildcard precedence: `"*"` is caught before the regex (returning `ErrWildcardManaged`, not `ErrInvalidCapability`) — intentional and correct.
- No shared mutable state; no concurrency constructs — no race surface.
- No dead unexported symbols: `capNameRe`, `fromRow`, `parseUUID`, `isUniqueViolation`, `validateGrantInput` are all called from exported methods or tests within the package.
- Go 1.26 in `go.mod` — loop-variable capture is not a concern.
