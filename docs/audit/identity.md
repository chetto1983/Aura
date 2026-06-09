# Audit: internal/identity

**Verdict:** needs-work — two methods defined and tested but never reached in production; one exported symbol with no external consumer.
**Counts:** critical 0 / high 0 / medium 1 / low 2

## Findings

### [MEDIUM][NOT-WIRED] `HasCapability` defined but never called in production

**Location:** `internal/identity/store.go:111-121`
**Confidence:** high

`(*Store).HasCapability` is the central capability-gate predicate — it encodes the wildcard-or-exact logic that enforces `capability_grants` at runtime. However, no production code path calls it:

- The narrow `IdentityStore` interface that the Runner consumes (`internal/runner/interfaces.go:82-84`) requires only `GetIdentityByName`; `HasCapability` is absent.
- No tool handler, agent, CLI command, or channel handler calls `.HasCapability(...)` (grep across `D:/Aura/**/*.go` excluding `*_test.go` returns zero matches).
- Every caller in the repo is inside `internal/identity/store_test.go` (integration tier, `db_integration` build tag).

The consequence is that Slice 1.7's capability-grant mechanism exists in the DB schema, the Store, and the tests — but the runtime never enforces it. An agent or tool could execute regardless of what `capability_grants` contains.

**Suggested fix:** Either add `HasCapability` to the `IdentityStore` interface and have the Runner (or tool dispatcher) call it before executing a capability-gated tool, or — if enforcement is intentionally deferred to a future slice — add a `// TODO(Slice 1.7b): wire into tool dispatcher` comment and a known-gap entry in the PRD so the method's orphaned status is intentional and tracked.

---

### [LOW][NOT-WIRED] `DeleteIdentity` defined but has no CLI or API caller

**Location:** `internal/identity/store.go:101-106`
**Confidence:** high

`(*Store).DeleteIdentity` is called only from integration tests (`internal/identity/store_test.go:293, 356`). The `aura identity` CLI dispatcher (`cmd/aura/identity.go:48-60`) handles `list`, `get`, `grant`, and `revoke`, but has no `delete` branch. No other package calls the method.

Deleting the seeded `local` identity would cascade-delete all its `capability_grants`, which is a footgun. That risk may be the reason the CLI omits it. If so, the method should carry a comment explaining the deliberate omission; if a `delete` subcommand is planned, a TODO and PRD entry should track it.

**Suggested fix:** Add a `// No CLI surface by design — deleting the seeded local identity is destructive; a future operator command would need a hard confirmation gate.` comment above the method, or wire a `delete` subcommand with appropriate safeguards.

---

### [LOW][NOT-WIRED] `Wildcard` exported constant has no external consumer

**Location:** `internal/identity/store.go:29`
**Confidence:** high

`const Wildcard = "*"` is exported but referenced only inside the same file (`store.go:161`, `validateGrantInput`). No package outside `internal/identity` uses `identity.Wildcard` — callers that need to compare against the wildcard string (e.g., integration test literals, raw SQL in `capability_grants.sql.go`) hardcode `"*"` directly.

This is a minor issue: the constant is exported on the assumption a future consumer will import it, but currently it leaks an implementation detail into the public API surface for no benefit. `go vet` and `deadcode` won't flag exported symbols, but the exported surface is unnecessary today.

**Suggested fix:** Unexport to `wildcard` (or just use the inline literal in `validateGrantInput`) until an actual external consumer exists. If the symbol is intentionally reserved for future callers (e.g., a scheduler or agent that must enumerate the wildcard), a comment documenting that intent would silence the concern.

---

## What was checked (no findings)

**Bugs:** All error paths use `%w` wrapping correctly. `GetIdentityByName` maps `pgx.ErrNoRows` to `ErrIdentityNotFound` before returning. `GrantCapability` correctly swallows `23505` unique-violation via `errors.As + pgErr.Code` (never message-matching). `parseUUID` validates before constructing `pgtype.UUID{Valid: true}`. No unchecked error returns, no unclosed resources (the Store holds a `*pgxpool.Pool` — pool lifetime is managed by the caller; no rows/Body/file handles are opened in this package). Context is propagated to every DB call.

**Races:** The Store is a plain struct with two read-only fields (`pool`, `q`) set at construction and never mutated. No goroutines are spawned. `capNameRe` is a package-level `var` initialized once at package init via `regexp.MustCompile` (safe for concurrent use; `regexp.Regexp` is documented as goroutine-safe). No shared mutable state.

**Dead code (unexported):** All unexported helpers (`fromRow`, `parseUUID`, `isUniqueViolation`, `validateGrantInput`, `capNameRe`) are used by the exported methods in the same file. None are dead.

**Logic / regex boundary:** The capability name regex `^[a-z][a-z0-9._-]{0,63}$` correctly enforces a 1-to-64 character limit (1 mandatory leading letter + 0–63 tail characters). The unit test's "too long rejected" case (`"a" + repeat("b", 64)` = 65 chars) correctly fails the regex. Single-char names (`"a"`) are valid. The wildcard check (`capability == Wildcard`) runs before the regex check, so `"*"` is never passed to `capNameRe.MatchString`.
