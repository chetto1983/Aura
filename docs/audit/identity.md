# Audit: internal/identity

**Verdict:** needs-work — capability enforcement is wired to the DB but never called from the runtime; the Store's access-control surface exists only in tests.

**Counts:** critical 0 / high 1 / medium 1 / low 1

---

## Findings

### [HIGH][NOT-WIRED] `HasCapability` defined, granted via CLI, but never enforced at runtime

**Location:** `internal/identity/store.go:111-121`
**Confidence:** high

`Store.HasCapability` is the sole enforcement mechanism for the capability-grant system described in Slice 1.7 (T-04-05/T-04-06 mitigations). The method is fully implemented and covered by integration tests, but it is **never called anywhere outside the identity package's own tests** (`internal/identity/store_test.go`).

Verified by grep across the entire repo (`D:/Aura`, all `.go` files):

- `internal/runner/interfaces.go:82-84` defines `IdentityStore` — this interface contains only `GetIdentityByName`, not `HasCapability`.
- No production caller (`cmd/`, `internal/runner/`, `internal/agent/`, `internal/channels/`) calls `.HasCapability` or any equivalent on the store or its interface.

Effect: `aura identity grant <name> <cap>` correctly writes a row to `aura.capability_grants`, but that row is never consulted before a tool is dispatched. The capability system is administratively functional (CLI read/write) but provides zero runtime enforcement — an identity with no grants can invoke every tool.

**Suggested fix:** Add `HasCapability(ctx context.Context, identityID, capability string) (bool, error)` to `runner.IdentityStore` and call it in the Runner's tool-dispatch path (before executing any tool call) with the owning identity's ID. The gate should enforce for non-wildcard identities only, consistent with the existing wildcard semantics.

---

### [MEDIUM][NOT-WIRED] `ErrWildcardManaged` and `ErrInvalidCapability` sentinels exported but no caller outside the identity package inspects them

**Location:** `internal/identity/store.go:39-43`
**Confidence:** high

Both sentinels are exported (`ErrWildcardManaged`, `ErrInvalidCapability`). Their only production callers are in `cmd/aura/identity.go`, which prints the returned error to stderr and calls `os.Exit(1)` — no `errors.Is` inspection is done. In `cmd/aura/cachefakes.go` and `internal/runner/fakes_test.go`, only `ErrIdentityNotFound` is used.

This is not a bug (the CLI behavior is correct: print + exit on any error), but both sentinels carry no additional semantic weight for callers today. If future callers are expected to distinguish these error types, document that contract explicitly. If not, they could be unexported.

This is low-severity as-is — the exported names are stable public API with a clear semantic (wildcard-managed, invalid name), so keeping them exported is defensible. Flagged because the audit scope requires noting symbols exported without verified external consumers that perform type-aware error inspection.

**Suggested fix:** Either (a) document in a godoc `// Callers should use errors.Is to distinguish…` comment, or (b) demote to unexported if the CLI-only usage pattern is intentional and capability enforcement is long-term centralised in the Runner (which already translates all identity errors through its own wrapping).

---

### [LOW][NOT-WIRED] `Wildcard` constant exported but has no caller outside the package

**Location:** `internal/identity/store.go:29`
**Confidence:** high

`const Wildcard = "*"` is exported. Grep across `D:/Aura` for `identity.Wildcard` finds zero matches. The constant is used internally at line 161 (`if capability == Wildcard`) but no external code references it by its exported name. Test files reference `"*"` directly rather than this constant.

This is low severity — the export is cheap and the name is self-documenting — but it is dead as an exported symbol today.

**Suggested fix:** Either document that callers should compare against `identity.Wildcard` instead of the raw literal, or demote to unexported `wildcard` and update the one internal use.

---

## What was checked and why it is otherwise clean

- **Nil pointer risk in `fromRow`:** `AuraIdentities.ID` is `pgtype.UUID` (a value type, not a pointer). `uuid.UUID(r.ID.Bytes)` is a direct array cast — safe regardless of `Valid` because `ID` was scanned from a NOT NULL column. No nil deref possible.
- **Error wrapping:** Every error path uses `%w`; sentinel classification follows `errors.As`/`errors.Is` throughout. `isUniqueViolation` correctly uses `errors.As` (not string matching).
- **Resource leaks:** `ListIdentities` delegates to sqlc which iterates rows and closes them internally. No raw `pgx.Rows` is held in identity code.
- **Context propagation:** Every public method receives and forwards `ctx`; no `context.Background()` substitution exists in production paths.
- **Race conditions:** No shared mutable state. `capNameRe` is a package-level compiled regex (read-only after init). `Store` fields `pool` and `q` are assigned once at construction and never mutated.
- **Integer/UUID conversion:** `uuid.UUID(r.ID.Bytes)` is a safe `[16]byte` cast — no overflow, no truncation.
- **Loop var capture:** go.mod declares `go 1.26.4`; the loopvar fix applies automatically. No captures to audit.
- **Dead unexported code:** `fromRow`, `parseUUID`, `isUniqueViolation`, `validateGrantInput`, `capNameRe` are all called from exported methods. None are dead.
