# v1 Dashboard Security Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close v1.0 dashboard security blockers by expiring bearer tokens and redacting secret settings from API/UI reads while preserving write and test-connection flows.

**Architecture:** Keep bearer auth as the dashboard boundary, but add token expiry metadata to the shared SQLite schema and have lookup return a distinct expired-token error. Keep settings stored as-is for v1.0, but redact secret values at the API response boundary so the dashboard never receives raw API keys on read.

**Tech Stack:** Go, SQLite migrations, `database/sql`, `log/slog`, React/Vite dashboard settings UI.

---

## File Structure

- Modify: `internal/db/migrations/migrations.go`
  - Add migration v3 for `api_tokens.expires_at`.
  - Backfill legacy rows from `issued_at + 720h`.
  - Include `expires_at` in fresh schema.
- Modify: `internal/db/migrations/migrations_test.go`
  - Assert fresh/upgraded schemas include `api_tokens.expires_at`.
  - Assert v3 is recorded and legacy token rows are backfilled.
- Modify: `internal/auth/store.go`
  - Add `ErrExpired`.
  - Add a default 30-day TTL and configurable setter.
  - Store `expires_at` on `Issue`.
  - Return `ErrExpired` when `Lookup` sees an expired token.
- Modify: `internal/auth/store_test.go`
  - Prove issued tokens carry `expires_at`.
  - Prove lookup accepts before expiry and returns `ErrExpired` after expiry.
- Modify: `internal/auth/middleware.go`
  - Return a distinct 401 body `{"error":"token_expired"}` for expired tokens while preserving generic unauthorized bodies for invalid/revoked tokens.
- Modify: `internal/auth/middleware_test.go`
  - Prove expired tokens get the distinct body.
- Modify: `internal/config/config.go`
  - Add `DashboardTokenTTLHours`, default `720`.
- Modify: `internal/config/config_test.go`
  - Assert default and env override.
- Modify: `internal/telegram/setup.go`
  - Apply `DashboardTokenTTLHours` to the auth store.
- Modify: `.env.example`
  - Document `DASHBOARD_TOKEN_TTL_HOURS=720`.
- Modify later: `internal/api/settings.go`, `internal/api/settings_test.go`, `web/src/components/SettingsPanel.tsx`
  - Redact secret `Value` and `ActiveValue` on GET, while POST `/settings` and POST `/settings/test` keep accepting raw values.

## Task 1: Token Expiry Schema and Store

**Files:**
- Modify: `internal/db/migrations/migrations.go`
- Modify: `internal/db/migrations/migrations_test.go`
- Modify: `internal/auth/store.go`
- Modify: `internal/auth/store_test.go`

- [x] **Step 1: Write failing auth store tests**

Add tests that:

```go
func TestIssueSetsExpiresAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	tok, err := s.Issue(ctx, "u1")
	if err != nil { t.Fatal(err) }
	var expiresAt string
	if err := s.db.QueryRowContext(ctx, `SELECT expires_at FROM api_tokens WHERE token_hash = ?`, hashToken(tok)).Scan(&expiresAt); err != nil { t.Fatal(err) }
	if expiresAt != now.Add(30*24*time.Hour).Format(time.RFC3339) { t.Fatalf("expires_at = %q", expiresAt) }
}
```

and:

```go
func TestLookupExpiredToken(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 5, 5, 8, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	tok, err := s.Issue(ctx, "u1")
	if err != nil { t.Fatal(err) }
	s.now = func() time.Time { return now.Add(30*24*time.Hour + time.Second) }
	if _, err := s.Lookup(ctx, tok); !errors.Is(err, ErrExpired) { t.Fatalf("err = %v, want ErrExpired", err) }
}
```

- [x] **Step 2: Run red auth tests**

Run: `go test ./internal/auth -run "TestIssueSetsExpiresAt|TestLookupExpiredToken" -count=1`

Expected: FAIL because `expires_at` and `ErrExpired` do not exist.

- [x] **Step 3: Implement minimal schema/store behavior**

Add migration v3, `expires_at` reads/writes, `ErrExpired`, and a default 30-day TTL.

- [x] **Step 4: Run auth and migration tests**

Run: `go test ./internal/auth ./internal/db/migrations -count=1`

Expected: PASS.

## Task 2: Token Expiry Config and Middleware

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`
- Modify: `internal/auth/middleware.go`
- Modify: `internal/auth/middleware_test.go`
- Modify: `internal/telegram/setup.go`
- Modify: `.env.example`

- [x] **Step 1: Write failing config and middleware tests**

Add config assertions for default `720` and env override. Add middleware test expecting expired token body `{"error":"token_expired"}`.

- [x] **Step 2: Run red tests**

Run: `go test ./internal/config ./internal/auth -run "DashboardTokenTTL|ExpiredToken" -count=1`

Expected: FAIL until config and middleware are implemented.

- [x] **Step 3: Wire config into production auth store**

Set the store TTL in `internal/telegram/setup.go`:

```go
authStore.SetTokenTTL(time.Duration(cfg.DashboardTokenTTLHours) * time.Hour)
```

When the configured hours are `<= 0`, the auth store should fall back to the default 30 days.

- [x] **Step 4: Run targeted token-expiry tests**

Run: `go test ./internal/auth ./internal/config ./internal/api ./internal/db/migrations -count=1`

Expected: PASS.

## Task 3: Settings Secret Redaction

**Files:**
- Modify: `internal/api/settings.go`
- Modify: `internal/api/settings_test.go`
- Modify: `web/src/components/SettingsPanel.tsx`
- Modify: `web/src/types/api.ts` only if a new response flag is needed.

- [x] **Step 1: Write failing API test**

Change the existing settings list secret test so `LLM_API_KEY` never returns raw `Value` or raw `ActiveValue`; expect `(configured)` or an empty edit field plus `is_secret=true`.

- [x] **Step 2: Implement response-boundary redaction**

Redact every `SettingItem` where `IsSecret` is true before `writeJSON`, without changing `POST /settings` storage or `POST /settings/test` request behavior.

- [x] **Step 3: Update frontend state behavior**

Ensure the settings form does not treat redacted placeholders as dirty secrets and does not resubmit placeholders as raw values.

- [x] **Step 4: Run Go and web verification**

Run:

```powershell
go test ./internal/api ./internal/settings -count=1
powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-web.ps1
```

Expected: PASS.

## Task 4: Phase Handoff

**Files:**
- Modify: `.planning/REQUIREMENTS.md`
- Modify: `.planning/STATE.md`
- Modify: `docs/implementation-tracker.md`
- Modify: `docs/superpowers/plans/2026-05-04-v1-production-readiness-plan.md`

- [x] Mark `SEC-01` done after token expiry lands.
- [x] Mark `SEC-02` done after settings redaction lands.
- [x] Advance active state to Phase 5 after both are done.
- [x] Run full verification:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-go.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File loops\aura-implementation\scripts\verify-web.ps1
```

## Self-Review

- Spec coverage: Covers Phase 4 success criteria from `.planning/ROADMAP.md`.
- Placeholder scan: No TBD/TODO placeholders.
- Type consistency: Uses existing `auth.Store`, `config.Config`, migrations runner, and settings response types.
