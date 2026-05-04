# Stack Research

**Domain:** Go hardening — centralized SQLite, versioned migrations, at-rest encryption, Pyodide release packaging
**Researched:** 2026-05-04
**Confidence:** HIGH

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| `modernc.org/sqlite` | v1.50.0 | Pure-Go SQLite driver (already used) | No CGO; already the project's only SQLite driver; single `sql.Open` with WAL + busy_timeout solves the multi-pool problem without a new dependency |
| `database/sql` | stdlib | Connection pool sharing | All stores already accept `*sql.DB` via `NewStoreWithDB`; centralizing the one `sql.Open` call is an architecture cleanup, not a library swap |
| `crypto/sha256` + `crypto/rand` | stdlib | Master key derivation from TELEGRAM_TOKEN + salt | SHA-256 is already imported in auth and embed cache; a 256-bit derived key from the bootstrap secret is sufficient for symmetric encryption of settings rows |
| `crypto/aes` + `crypto/cipher` | stdlib | AES-256-GCM at-rest encryption for settings secrets | Already in Go stdlib; AES-GCM provides authenticated encryption (confidentiality + integrity); no external library needed |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| Custom `schema_versions` table | — | Lightweight versioned migration runner | A single `schema_versions` table + ordered slice of `Migration{Version, Name, UpSQL}` funcs is ~80 lines of Go; sufficient for a single-file SQLite app with infrequent schema changes |
| `goreleaser` | v2 (already configured) | Cross-platform binary releases | Already in `.goreleaser.yml`; handles GOOS/GOARCH matrix, ldflags, archives, checksums, changelog, and GitHub release creation |
| `pyodide` runtime | 0.29.3 (already bundled) | Offline Python sandbox | Already installed by `runtime/install-pyodide-bundle.mjs`; included in release archives via goreleaser `files` glob + smoke-tested in `before` hooks |
| `goversioninfo` | latest (go run) | Windows `.exe` metadata (icon, version) | Already in Makefile and goreleaser `before` hooks; generates `resource.syso` linked into the Windows binary |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| `go run ./cmd/debug_sandbox --smoke` | Pyodide release smoke test | Already in goreleaser `before` hook; verifies the bundled runtime works before publishing |
| `npm --prefix web run build` | Build embedded React dashboard | Already in goreleaser `before` hook; produces `web/dist/` embedded via `//go:embed all:dist` |

## Installation

No new Go module dependencies are introduced by this hardening work. All required capabilities already exist in stdlib or current `go.mod`:

```bash
# No `go get` commands needed.
# x/crypto is already an indirect dependency (v0.48.0) but is NOT used for encryption.
# The recommendation prefers stdlib crypto/aes over x/crypto/nacl/secretbox
# to avoid promoting an indirect dependency to direct.
```

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| `crypto/aes` (stdlib) | `golang.org/x/crypto/nacl/secretbox` | NaCl secretbox has a simpler API (no nonce management footguns) and is already an indirect dependency. Would be a fine choice if the team prefers the NaCl API over AES-GCM; the security properties are equivalent for this threat model. |
| Custom `schema_versions` table | `golang-migrate/migrate` | If Aura ever needs down-migrations, multi-DB support, or CLI tooling. Currently overkill: a single SQLite file with ~10 total tables doesn't justify bringing a framework with its own driver adapters, CLI, and migration file format. |
| Custom `schema_versions` table | `pressly/goose` | Same rationale as golang-migrate; goose is well-maintained but its feature set (embedded migrations, dialect-aware DDL) isn't needed for this scale. |
| `coreleaser` zip archives (current) | NSIS installer (e.g., `makensis`) | Only if users request a proper Windows installer with Start Menu shortcuts, uninstaller, and auto-start. Not needed for a hardening milestone. |
| Pyodide in release archive (current) | Separate Pyodide download step | Would require users to manually download 173MB of runtime files and point an env var at them. Current approach (single zip with everything) is simpler for end users. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `mattn/go-sqlite3` (CGO SQLite) | Requires CGO, a C compiler, and platform-specific linking; breaks cross-compilation. The project migrated to `modernc.org/sqlite` for this reason. | `modernc.org/sqlite` (already in use) |
| Any external encryption library (`age`, `nacl/box`, `fernet`) | Overkill for encrypting ~4 key-value pairs in a local SQLite row. Brings dependency risk for no benefit over stdlib AES-GCM. | `crypto/aes` + `crypto/cipher` (stdlib) |
| `golang-migrate` or `pressly/goose` | Heavy frameworks with CLI tooling, driver adapters, and migration file formats. A `schema_versions` table + ordered Go funcs is ~80 lines and avoids all of this. | Custom `schema_versions` table |
| NSIS/MSI installer for Windows | Unnecessary for a hardening milestone. The existing zip archive includes the binary + pyodide runtime + README; users already know how to unzip. | goreleaser zip archives (already configured) |

## Stack Patterns by Variant

**If encryption key derivation needs hardware binding:**
- Consider `golang.org/x/sys/windows` DPAPI (`CryptProtectData`) for Windows — binds the master key to the current Windows user profile
- Because local SQLite encryption only needs to protect against offline DB file theft, and DPAPI ties the key to the Windows login, making DB file theft useless without the Windows login
- Currently not recommended: TELEGRAM_TOKEN derivation is simpler, cross-platform, and doesn't break when the DB is moved between machines

**If migration complexity grows past ~20 migrations:**
- Revisit `golang-migrate` or `pressly/goose` — the cost of a framework is justified when hand-rolling version tracking becomes error-prone
- Because the current codebase has ~5 effective migrations (scheduler column additions), the break-even point is well above current needs

**If Windows users request an installer:**
- Add `makensis` + an `.nsi` script to integrate with goreleaser's `nsis` pipe
- Because some users expect `Program Files` installation and Start Menu shortcuts; not a hardening concern

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `modernc.org/sqlite` v1.50.0 | Go 1.25.5 | Already tested in production; WAL mode is a SQLite runtime feature, not a driver concern |
| `crypto/aes` | Go 1.25.5 (stdlib) | AES-GCM is stable since Go 1.x; no version concerns |
| `goreleaser` v2 | `.goreleaser.yml` already configured | No upgrade needed; the existing config covers the full release pipeline |
| Pyodide 0.29.3 | `internal/sandbox/pyodide_runner.go` | Locked to a specific version via `install-pyodide-bundle.mjs`; upgrading requires a deliberate version bump and re-smoke |

## Design Notes

### Centralized DB Connection

The fix is architectural, not a library swap. Current state: 5 independent `sql.Open("sqlite", path)` calls across `auth`, `scheduler`, `settings`, `search/embed_cache`, and `swarm` stores. Several already export `NewStoreWithDB(*sql.DB)` constructors. The change:

1. Create a new `internal/db` package with a single `func Open(path string) (*sql.DB, error)` that:
   - Opens the driver once via `sql.Open("sqlite", path)`
   - Executes `PRAGMA journal_mode=WAL;` and `PRAGMA busy_timeout=5000;`
   - Executes `PRAGMA foreign_keys=ON;`
   - Runs the migration engine (all versioned migrations) before returning
   - Sets `db.SetMaxOpenConns(1)` since SQLite writes serialize on a single connection anyway; avoids multi-connection WAL confusion
2. All stores switch from `OpenStore(path)` to `NewStoreWithDB(db)`.
3. `embed_cache.OpenEmbedCache` is the exception: it receives an already-opened `*sql.DB` instead of creating its own.

### Versioned Migrations

A `schema_versions` table + ordered migration slice. Example structure:

```sql
CREATE TABLE IF NOT EXISTS schema_versions (
    version   INTEGER PRIMARY KEY,
    name      TEXT    NOT NULL,
    applied_at TEXT   NOT NULL
);
```

```go
type Migration struct {
    Version int
    Name    string
    SQL     string
}

var migrations = []Migration{
    {1, "auth tables", authSchemaSQL},
    {2, "settings table", settingsSchemaSQL},
    {3, "conversations table", conversationsSchemaSQL},
    // ... ordered by version
}
```

The runner queries `MAX(version)` from `schema_versions`, applies unapplied migrations in a single transaction, and records each in `schema_versions`. No down-migrations (the app doesn't need them; SQLite can be restored from backup). This consolidates all the per-store `migrate()` methods and the ad-hoc `addEveryMinutesColumn`/`addScheduleWeekdaysColumn` patterns into one ordered sequence.

### At-Rest Encryption for Settings

Only 4 keys hold secrets: `LLM_API_KEY`, `EMBEDDING_API_KEY`, `MISTRAL_API_KEY`, `OLLAMA_API_KEY`. `TELEGRAM_TOKEN` stays in `.env`.

Approach:
1. Derive a 32-byte AES-256 key: `SHA-256("aura:settings:v1:" + TELEGRAM_TOKEN + salt)` where salt is a 16-byte random value stored in a `_meta` row.
2. On `Set`, if the key starts with one of the known secret prefixes (or has a `secret: true` metadata flag), serialize the value as `<nonce:12><ciphertext:value_len+16>` (AES-256-GCM), Base64-encode, and store prefixed with `enc:v1:`.
3. On `Get`, if the stored value starts with `enc:v1:`, strip the prefix, Base64-decode, and AES-GCM decrypt.
4. Existing plaintext rows migrate transparently: on first `Set` after the encryption change, the value is encrypted. On `Get`, if the value doesn't start with `enc:v1:`, it's returned as-is (backward compat).

This adds zero new dependencies. `crypto/aes`, `crypto/cipher`, `crypto/rand`, `crypto/sha256`, and `encoding/base64` are all stdlib.

### Pyodide Windows Release Packaging

Already configured and working in `.goreleaser.yml`:
- `runtime/install-pyodide-bundle.mjs` downloads and extracts Pyodide runtime files into `runtime/pyodide/` during the `before` hooks
- `go run ./cmd/debug_sandbox --smoke` verifies the sandbox works before packaging
- The `files` section in `archives` includes `runtime/pyodide/**/*` in every release archive
- The binary resolves runtime path from `defaultPyodideRuntimeDir = "./runtime/pyodide"` (relative to the binary's working directory)

No changes needed. The hardening work should verify this path works end-to-end on a clean Windows machine but doesn't require new tooling.

## Sources

- `go.mod` — verified current dependencies: `modernc.org/sqlite` v1.50.0, `golang.org/x/crypto` v0.48.0 (indirect), `fyne.io/systray` v1.12.0
- `internal/settings/store.go` — confirmed plaintext secret storage in `settings` table; confirmed `NewStoreWithDB` pattern exists
- `internal/auth/store.go` — confirmed `api_tokens`, `allowed_users`, `pending_users` tables; confirmed `SHA-256` token hashing
- `internal/scheduler/store.go` — confirmed 5 per-store `migrate()` methods; ad-hoc `PRAGMA table_info` column backfills; cross-package table ownership
- `internal/swarm/store.go` — confirmed `OpenStore` path owns its own `*sql.DB`
- `internal/search/embed_cache.go` — confirmed `OpenEmbedCache` opens its own connection
- `.goreleaser.yml` — verified existing release pipeline: before hooks, CGO_ENABLED=0, GOOS/GOARCH matrix, archive files glob, smoke test
- `runtime/pyodide/` — verified 173.4MB bundle with 2,495 files across packages, wasm, and JS runtime files
- `.planning/codebase/CONCERNS.md` — confirmed all priority-ranked hardening items match the stack research scope

---

*Stack research for: Go hardening and release packaging*
*Researched: 2026-05-04*
