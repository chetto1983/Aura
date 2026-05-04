# Pitfalls Research

**Domain:** Go codebase hardening
**Researched:** 2026-05-04
**Confidence:** HIGH

## Critical Pitfalls

### Pitfall 1: Breaking existing stores with centralized DB

**What goes wrong:**
Stores that currently own their `*sql.DB` lifecycle (`sql.Open` → `Ping` → `migrate` → close on error) break when the connection is injected from outside. If `db.Open()` closes the shared connection during shutdown while a store's deferred `Close()` also fires, it double-closes.

**Why it happens:**
Each store's constructor does `sql.Open("sqlite", path)` and stores it as a private field. Switching to `NewStoreWithDB` means the store no longer owns the connection — but existing `Close()` methods may still call `db.Close()`.

**How to avoid:**
All 5 stores (`scheduler`, `auth`, `settings`, `swarm`, `embed_cache`) must switch to a `owned bool` flag or remove their `Close()` methods entirely, delegating lifetime to `internal/db`. The `db.Open()` function becomes the sole Close() point during shutdown.

**Warning signs:**
- "database is closed" errors after startup
- Double-close panics from sql.DB
- Stores that still call `sql.Open` internally

**Phase to address:**
Phase 1 (DB centralization)

---

### Pitfall 2: Partially-migrated databases on upgrade

**What goes wrong:**
A user upgrades from a pre-migration Aura to a new version. The new `migrations.Run()` starts at version 0, tries `CREATE TABLE IF NOT EXISTS` for tables that already exist (idempotent, fine), but then tries `ALTER TABLE ADD COLUMN` for columns that may or may not exist. If the old `scheduler.migrate()` added `schedule_every_minutes` but not `schedule_weekdays`, the migration runner might skip the first ALTER (column exists) but not the second — or apply an ALTER on a table that doesn't yet have the prior column.

**Why it happens:**
The current per-store `PRAGMA table_info` check pattern is ad-hoc: each store independently decides what columns to add, with no version number to track state.

**How to avoid:**
Versioned migration approach: `schema_versions` table with ordered up-migrations. Each migration is `IF NOT EXISTS` idempotent for tables and `PRAGMA table_info`-guarded for columns. The runner records the applied version in a transaction. On upgrade, it starts from the last recorded version and applies only new migrations.

**Warning signs:**
- "duplicate column name" SQLite errors on startup
- New columns missing on old databases
- Schema drift between fresh install and upgraded install

**Phase to address:**
Phase 2 (Versioned migrations)

---

### Pitfall 3: Losing the encryption key

**What goes wrong:**
Settings secrets are encrypted with AES-256-GCM using a key derived from `TELEGRAM_TOKEN`. If the bot token changes (user regenerates with BotFather), ALL encrypted settings become permanently unreadable — the user loses their LLM/Embedding/Mistral/Ollama API keys.

**Why it happens:**
Deriving the encryption key from a mutable external secret creates a hidden coupling. TELEGRAM_TOKEN is the most likely config value to change (token leak, BotFather rotation).

**How to avoid:**
Store a separate random `encryption_key` (32 bytes, hex-encoded) in `.env` on first migration. Derive the AES key from `SHA-256(encryption_key + fixed_salt)` instead of `TELEGRAM_TOKEN`. Add a `SETTINGS_ENCRYPTION_KEY` env var. Document clearly: "If you lose this key, re-enter your API keys in the dashboard."

**Warning signs:**
- All secrets decrypting to empty/error after token rotation
- No `SETTINGS_ENCRYPTION_KEY` in `.env.example`
- Silent decryption failures on startup

**Phase to address:**
Phase 4 (Secrets encryption)

---

### Pitfall 4: Expiring tokens for active dashboard sessions

**What goes wrong:**
Adding `expires_at` and enforcing it in `auth.Lookup()` immediately kills active dashboard sessions. A user who issued a token 25 days ago suddenly gets 401 on their next dashboard refresh with no warning.

**Why it happens:**
Existing tokens have `issued_at` but no `expires_at`. Adding `NOT NULL` or enforcing expiry on lookup rejects tokens that were valid seconds before the upgrade.

**How to avoid:**
Migration adds `expires_at` as nullable. Existing tokens get `expires_at = issued_at + (30 * 24 * time.Hour)` — 30 days from their issue date. Tokens already older than 30 days get 7-day grace period from migration time. Lookup enforces expiry but returns a specific `ErrExpired` (distinct from `ErrInvalid`) so the dashboard can show "Session expired" instead of "Invalid token". Frontend handles 401+expired → redirect to /login with clear message.

**Warning signs:**
- Dashboard users suddenly logged out after upgrade
- `expires_at` migration applies NULL → all tokens rejected
- No grace period for existing tokens

**Phase to address:**
Phase 3 (Token expiry)

---

### Pitfall 5: Tests depending on real infrastructure

**What goes wrong:**
Adding integration tests for `internal/telegram/conversation.go` that connect to real Telegram API, real LLM endpoints, or real SQLite files in the working directory. Tests become slow, flaky, and network-dependent.

**Why it happens:**
The conversation handler depends on telebot.Context (real Telegram types), llm.Client (real API), and multiple stores (real SQLite). Mocking all of them is tedious, so the temptation is to just use real ones.

**How to avoid:**
Use `cmd/debug_memory_quality` as the established test harness pattern: hermetic temp SQLite files (`t.TempDir()`), stub `llm.Client` that returns canned responses or routes to recorded fixtures, and a fake telebot context wrapper. The pattern from `internal/telegram/setup_test.go` already exists — extend it. For coverage that absolutely needs real LLM calls, use a separate `-live` flag (like `debug_memory_quality -live-llm`).

**Warning signs:**
- Tests that pass locally but fail in CI
- Tests with `time.Sleep` for rate limiting
- Real API keys in test files

**Phase to address:**
Phase 5 (Telegram test coverage)

---

### Pitfall 6: Pyodide release bundling breaking non-Windows builds

**What goes wrong:**
The Pyodide bundle gets baked into the Windows `.exe` via goreleaser extras/extra_files, but the same goreleaser config breaks Linux/macOS cross-compilation or CI validation builds.

**Why it happens:**
Pyodide's `aura-pyodide-runner.cmd` is Windows-specific. If `goreleaser` tries to include it in a Linux build artifact, we get an unusable file. If the runner binary path is hardcoded for Windows in the sandbox package, non-Windows builds fail.

**How to avoid:**
goreleaser `extra_files` config should use `builder: windows` filter. The sandbox package already handles runtime_kind=unavailable gracefully on non-Windows (fail-closed). Release CI runs `go run ./cmd/debug_sandbox --smoke` only after the Windows binary is built. Non-Windows builds ship without the runtime (same as no-bundle behavior today).

**Warning signs:**
- goreleaser build fails on Linux CI runners
- Cross-compiled binary panics on sandbox init
- Pyodide files with wrong line endings or execute permissions

**Phase to address:**
Phase 7 (Release packaging)

---

## Technical Debt Patterns

Shortcuts that seem reasonable but create long-term problems.

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Skip `internal/telegram` integration tests; just fix the panic | Fast, low effort | 22.1% coverage stays forever; more bugs ship | Never — this is the whole point of the milestone |
| Skip versioned migrations; just add PRAGMA checks for new columns | Zero new code | Schema drift continues; next dev adds more ad-hoc ALTERs | Never — schema_versions table is ~30 lines |
| Don't encrypt secrets; document SQLite file permissions instead | Zero complexity | Settings store is a plaintext key dump anyone with aura.db can read | Already documented in CONCERNS.md — that's insufficient |
| Hardcode 30-day token TTL in code | Simpler than env var | Operator can't adjust to their security policy | Never — settings page already handles int fields |

## Integration Gotchas

Common mistakes when connecting to external services or modifying existing integration points.

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| `telebot v4` in tests | Trying to create real telebot.Bot instances | Use the existing `fakeBot` / `stub` patterns from setup_test.go |
| SQLite WAL mode | Applying `PRAGMA journal_mode=WAL` on every connection open (idempotent but redundant) | Apply once in `db.Open()`; subsequent connections on same file inherit WAL |
| goreleaser extra_files | Using `src: "../runtime/pyodide/*"` — paths break with different workdirs | Use `src: "runtime/pyodide/**/*"` with `strip_parent: true` and goreleaser's project-root resolution |
| Settings store encryption | Encrypting ALL settings instead of just secrets | Only encrypt `LLM_API_KEY`, `EMBEDDING_API_KEY`, `MISTRAL_API_KEY`, `OLLAMA_API_KEY` — non-secret values stay queryable |

## Performance Traps

Patterns that work at small scale but fail as usage grows.

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Multiple `*sql.DB` pools | `database is locked` under concurrent writes | Single pool + WAL + busy_timeout | Already breaking (scheduler + conversation archiving compete) |
| Migrations outside transaction | Partial schema after crash | Transaction-wrapped migration runner | Any crash during migration application |
| Encryption on every settings read | Dashboard /settings page lag | Decrypt once at load, cache plaintext in memory, re-encrypt on save | 4 secrets × AES-GCM is <1ms — not a real concern at this scale |

## Security Mistakes

Domain-specific security issues beyond general web security.

| Mistake | Risk | Prevention |
|---------|------|------------|
| Key derivation from mutable secret | Permanent data loss if TELEGRAM_TOKEN rotates | Separate SETTINGS_ENCRYPTION_KEY in .env |
| No key rotation story | Compromised key = all historical settings exposed | Document manual rotation: re-enter keys in dashboard after changing encryption key |
| Plaintext API keys in test fixtures | CI leaks | Use env-based config in tests, skip live-LLM in CI |
| Encryption without authentication | Bit-flip attacks on ciphertext | AES-256-GCM (authenticated mode, built into Go stdlib) |

## "Looks Done But Isn't" Checklist

Things that appear complete but are missing critical pieces.

- [ ] **DB centralization:** Often missing — old `db.Close()` calls in store Shutdown. Verify: `git grep 'sql.Open\|\.Close()' internal/` finds only `internal/db/` matches
- [ ] **Versioned migrations:** Often missing — old `scheduler.migrate()` is still called. Verify: `scheduler.NewStoreWithDB` does NOT call migrate internally
- [ ] **Token expiry:** Often missing — Lookup doesn't actually check `expires_at`. Verify: test with expired token returns ErrExpired, not Ok
- [ ] **Secrets encryption:** Often missing — plaintext settings are still readable. Verify: `SELECT value FROM settings WHERE key='LLM_API_KEY'` returns ciphertext, not plaintext
- [ ] **Telegram coverage:** Often missing — tests pass but don't actually exercise the conversation loop. Verify: `go test -coverprofile=coverage.out ./internal/telegram` shows ≥55%
- [ ] **Pyodide release:** Often missing — bundle is in the artifact but debug_sandbox wasn't run. Verify: `unzip aura_Windows_x86_64.zip && ./runtime/pyodide/...` exists with correct hashes

## Recovery Strategies

When pitfalls occur despite prevention, how to recover.

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Broken DB connection | LOW | Roll back to previous binary; DB is backward-compatible |
| Partial migrations | MEDIUM | Backup aura.db → apply missing migrations manually → verify with debug_sandbox |
| Lost encryption key | LOW | Re-enter API keys in dashboard settings; they re-encrypt with new key |
| Expired tokens lockout | LOW | Issue new token via Telegram with `request_dashboard_token` |
| Flaky telegram tests | MEDIUM | Run with `-count=1 -race`; add retry for known-flaky timing-dependent assertions |

## Pitfall-to-Phase Mapping

How roadmap phases should address these pitfalls.

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| Breaking stores with shared DB | Phase 1 | `go test ./internal/scheduler ./internal/auth ./internal/settings ./internal/swarm ./internal/search -count=1` passes with single db.Open |
| Partially-migrated DB | Phase 2 | Fresh install + upgrade from pre-migration DB both produce identical schema |
| Losing encryption key | Phase 4 | Rotate SETTINGS_ENCRYPTION_KEY, verify secrets still readable |
| Expiring active sessions | Phase 3 | Token issued 60 days ago still works after upgrade (grace period) |
| Tests with real infra | Phase 5 | `go test ./internal/telegram -short` completes with no network calls |
| Pyodide on non-Windows | Phase 7 | `goreleaser build` passes on all platforms; Linux binary starts with runtime_kind=unavailable |

## Sources

- D:\Aura\.planning\codebase\CONCERNS.md — baseline issues inventory
- D:\Aura\.planning\codebase\ARCHITECTURE.md — integration points and startup sequence
- D:\Aura\internal\scheduler\store.go — current migration pattern (PRAGMA table_info)
- D:\Aura\internal\auth\store.go — token schema and issue/lookup flow
- D:\Aura\internal\settings\store.go — plaintext storage pattern
- Go stdlib `crypto/aes`, `crypto/cipher` — AES-256-GCM authenticated encryption

---

*Pitfalls research for: Aura hardening*
*Researched: 2026-05-04*
