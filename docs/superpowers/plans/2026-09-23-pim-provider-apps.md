# PIM Provider Apps Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An admin sets the Google and Microsoft OAuth client once per provider. The cockpit
stops asking members for it. Aura injects it into every account-create it forwards to the PIM
sidecar.

**Architecture:**
- A new Postgres table `aura.pim_provider_app` holds one row per managed provider. The Google
  secret is sealed with `internal/secret.Sealer`.
- A new package `internal/pimprovider` owns the provider rules and the store.
- `internal/agui` gets two routes: `GET` (members see only `configured`) and `PUT` (gated on
  `identity.create`). It also rewrites the body of the existing `POST /api/connect/pim/accounts`
  before forwarding.
- The cockpit hides the credential fields for managed providers and shows admins a
  per-provider form.
- The sidecar does not change.

**Tech Stack:** Go 1.26, pgx/v5, sqlc 1.31, golang-migrate, React 19 + TanStack Query + i18next,
vitest.

**Spec:** `docs/superpowers/specs/2026-09-23-pim-provider-apps-design.md`. Read it first; every
decision below argues from it.

## Global Constraints

- **Managed providers:** exactly `google`, `microsoft365`, `outlook.com`.
- **Canonical providers:** exactly `google`, `microsoft365`, `outlook.com`, `imap`, `ics`,
  `json`. Compare byte-for-byte, never case-folded.
- **Owned providerConfig keys:** `clientId`, `clientSecret`, `tenantId`. Drop them
  case-insensitively.
- **HKDF info label:** `aura-pim-provider-app-key-v1`.
- **Redirect URI constant:** `https://chetto1983.github.io/aura-connect/google/callback/`.
- **Admin gate:** `identity.create` (`identity.CapIdentityCreate`). Member routes stay on
  `governance.write`.
- **409 error token:** `provider_not_configured`.
- **Migration number:** the next free slot from `ls internal/db/migrations/ | tail -1`. It was
  0131 on 2026-09-23; re-check at landing.
- **Code rules:**
  - no file over 600 LOC;
  - comments only for non-obvious reasons;
  - all cockpit copy in English and Italian under `governance.mcp.calendar.*`.
- **Gates run in WSL:**
  `export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"`, and `CGO_ENABLED=1` for `-race`.
- **Commits and pushes from WSL:**
  `LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks …`. Never
  `--no-verify`.
- **Commit trailer:** `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.

## Review Focus

1. **A member sends `"provider":"Google"`** (or `GOOGLE`, or ` google`) to create an account on
   their own client. Expected: 400, and nothing reaches the sidecar. Pinned in Task 5.
2. **A body carries `ClientId` or `CLIENTSECRET`** next to the injected keys. Expected: they are
   dropped, and the sidecar never sees two case-variant keys, which would make it answer 500.
   Pinned in Task 5.
3. **The admin changes the Google client ID and leaves the secret blank.** Expected: 400 naming
   the secret, never a saved app whose consent fails later. Pinned in Tasks 2 and 4.
4. **A member reads `GET /api/connect/pim/providers`.** Expected: only `provider` and
   `configured`, never the client ID or the secret. Pinned in Task 4.
5. **Save for Google with an empty secret on an existing row.** Expected: the stored secret is
   kept. It must not fail the row CHECK (measured: `INSERT … ON CONFLICT` checks the proposed
   row first). Pinned in Task 3.

---

### Task 1: Migration and sqlc queries

**Files:**
- Create: `internal/db/migrations/0131_pim_provider_app.up.sql`
- Create: `internal/db/migrations/0131_pim_provider_app.down.sql`
- Create: `internal/db/queries/pim_provider_app.sql`
- Generated: `internal/db/sqlc/pim_provider_app.sql.go`, `internal/db/sqlc/models.go`,
  `internal/db/sqlc/querier.go` (via `make sqlc`)
- Test: `internal/db/migrate_0131_integration_test.go`

**Interfaces:**
- Produces:
  - `sqlc.AuraPimProviderApp{Provider, ClientID, TenantID string; ClientSecretCiphertext []byte; UpdatedAt pgtype.Timestamptz; UpdatedBy string}`
  - `(*sqlc.Queries).ListPIMProviderApps(ctx) ([]sqlc.ListPIMProviderAppsRow, error)`, whose
    row has `Provider, ClientID, TenantID string; SecretSet bool; UpdatedAt pgtype.Timestamptz; UpdatedBy string`
  - `GetPIMProviderApp(ctx, provider string) (sqlc.AuraPimProviderApp, error)`
  - `UpsertPIMProviderApp(ctx, sqlc.UpsertPIMProviderAppParams) error`
  - `UpdatePIMProviderAppKeepSecret(ctx, sqlc.UpdatePIMProviderAppKeepSecretParams) (int64, error)`

- [ ] **Step 1: Confirm the slot**

Run: `ls internal/db/migrations/ | tail -1`
Expected: `0130_cloudflare_remote_access.up.sql`. If a higher number exists, use the next one and
rename every `0131` in this task.

- [ ] **Step 2: Write the failing migration test**

`internal/db/migrate_0131_integration_test.go`:

```go
//go:build db_integration

package db

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrate0131PIMProviderAppFreshUpDownUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	admin, migrateURL, _ := fresh0093Database(t, ctx, "aura_migrate0131_pim_apps")

	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate fresh database to head: %v", err)
	}
	assert0131Schema(t, ctx, admin)

	steps, err := MigrationStepsAbove(130)
	if err != nil {
		t.Fatalf("MigrationStepsAbove(130): %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, -steps); err != nil {
		t.Fatalf("MigrateSteps(%d) down to pre-0131: %v", -steps, err)
	}
	if regclass(t, ctx, admin, "pim_provider_app") != nil {
		t.Fatal("aura.pim_provider_app survived the down migration")
	}
	if err := MigrateSteps(ctx, migrateURL, steps); err != nil {
		t.Fatalf("migrate 0131 back up: %v", err)
	}
	assert0131Schema(t, ctx, admin)
}

func assert0131Schema(t *testing.T, ctx context.Context, admin *pgxpool.Pool) {
	t.Helper()
	if regclass(t, ctx, admin, "pim_provider_app") == nil {
		t.Fatal("aura.pim_provider_app missing after migrate")
	}
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"google without a secret", `INSERT INTO aura.pim_provider_app (provider, client_id) VALUES ('google', 'cid')`},
		{"microsoft with a secret", `INSERT INTO aura.pim_provider_app (provider, client_id, tenant_id, client_secret_ciphertext) VALUES ('outlook.com', 'cid', 'consumers', '\x01')`},
		{"microsoft without a tenant", `INSERT INTO aura.pim_provider_app (provider, client_id) VALUES ('microsoft365', 'cid')`},
		{"google with a tenant", `INSERT INTO aura.pim_provider_app (provider, client_id, tenant_id, client_secret_ciphertext) VALUES ('google', 'cid', 't', '\x01')`},
		{"unmanaged provider", `INSERT INTO aura.pim_provider_app (provider, client_id, tenant_id) VALUES ('imap', 'cid', 't')`},
		{"empty client id", `INSERT INTO aura.pim_provider_app (provider, client_id, tenant_id) VALUES ('outlook.com', '', 'consumers')`},
	} {
		_, err := admin.Exec(ctx, tc.sql)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s: err = %v, want check_violation 23514", tc.name, err)
		}
	}
	if _, err := admin.Exec(ctx, `INSERT INTO aura.pim_provider_app (provider, client_id, tenant_id) VALUES ('outlook.com', 'cid', 'consumers')`); err != nil {
		t.Fatalf("a well-formed Microsoft row was rejected: %v", err)
	}
	if _, err := admin.Exec(ctx, `DELETE FROM aura.pim_provider_app`); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `go test -tags db_integration -run TestMigrate0131 -count=1 ./internal/db/`. It needs the stack
up with `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` exported.
Expected: FAIL. `pim_provider_app` is missing.

- [ ] **Step 4: Write the migration**

`0131_pim_provider_app.up.sql`:

```sql
-- One OAuth client per managed PIM provider, set by an admin and injected by Aura into every
-- account-create it forwards to the aura-pim-mcp sidecar
-- (docs/superpowers/specs/2026-09-23-pim-provider-apps-design.md). Install-wide like 0130:
-- no RLS, and 0001's default privileges give aura_app its DML.
CREATE TABLE aura.pim_provider_app (
  provider text PRIMARY KEY CHECK (provider IN ('google', 'microsoft365', 'outlook.com')),
  client_id text NOT NULL CHECK (client_id <> ''),
  tenant_id text NOT NULL DEFAULT '',
  client_secret_ciphertext bytea,
  updated_at timestamptz NOT NULL DEFAULT now(),
  updated_by text NOT NULL DEFAULT '',
  CHECK ((provider = 'google') = (client_secret_ciphertext IS NOT NULL)),
  CHECK ((provider = 'google') = (tenant_id = ''))
);
COMMENT ON COLUMN aura.pim_provider_app.client_secret_ciphertext IS
  'AES-256-GCM with the nonce prepended; key HKDF(AURA_AUTHULA_SECRET, "aura-pim-provider-app-key-v1"). Google only.';
```

`0131_pim_provider_app.down.sql`:

```sql
DROP TABLE aura.pim_provider_app;
```

- [ ] **Step 5: Write the queries**

`internal/db/queries/pim_provider_app.sql`:

```sql
-- name: ListPIMProviderApps :many
SELECT provider, client_id, tenant_id,
       (client_secret_ciphertext IS NOT NULL)::boolean AS secret_set,
       updated_at, updated_by
FROM aura.pim_provider_app
ORDER BY provider;

-- name: GetPIMProviderApp :one
SELECT * FROM aura.pim_provider_app WHERE provider = $1;

-- name: UpsertPIMProviderApp :exec
INSERT INTO aura.pim_provider_app (provider, client_id, tenant_id, client_secret_ciphertext, updated_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (provider) DO UPDATE
SET client_id = EXCLUDED.client_id,
    tenant_id = EXCLUDED.tenant_id,
    client_secret_ciphertext = EXCLUDED.client_secret_ciphertext,
    updated_at = now(),
    updated_by = EXCLUDED.updated_by;

-- Keeping the stored secret is its own UPDATE: an INSERT … ON CONFLICT checks the row CHECK
-- against the proposed ('google', NULL) row before conflict handling (measured 2026-09-23).
-- name: UpdatePIMProviderAppKeepSecret :execrows
UPDATE aura.pim_provider_app
SET client_id = $2, tenant_id = $3, updated_at = now(), updated_by = $4
WHERE provider = $1;
```

- [ ] **Step 6: Generate and check the names**

Run: `make sqlc && git status --short internal/db/sqlc/`
Expected: `pim_provider_app.sql.go` is created and `models.go`/`querier.go` are modified. Open
`pim_provider_app.sql.go` and confirm the names in **Interfaces** above. If sqlc picked another
Go type for `secret_set` or the params, use its names in Tasks 3 and 4.

- [ ] **Step 7: Run the migration test**

Run: `go test -tags db_integration -run TestMigrate0131 -count=1 ./internal/db/`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/db/migrations/0131_pim_provider_app.*.sql internal/db/queries/pim_provider_app.sql internal/db/sqlc/ internal/db/migrate_0131_integration_test.go
git commit -m "feat(db): add aura.pim_provider_app for admin-set PIM OAuth clients"
```

---

### Task 2: Provider rules (`internal/pimprovider`, pure)

**Files:**
- Create: `internal/pimprovider/rules.go`
- Test: `internal/pimprovider/rules_test.go`

**Interfaces:**
- Produces (package `pimprovider`):
  - `const Google, Microsoft365, OutlookCom = "google", "microsoft365", "outlook.com"`
  - `var ErrNotConfigured`, `var ErrInvalid`
  - `type App struct { Provider, ClientID, TenantID, ClientSecret string; SecretSet bool; UpdatedAt time.Time; UpdatedBy string }`
  - `func Canonical(provider string) bool`
  - `func Managed(provider string) bool`
  - `func ManagedProviders() []string`, returning google, microsoft365, outlook.com in that order
  - `func IsOwnedKey(key string) bool`
  - `func Validate(next App, stored *App) error`: the error wraps `ErrInvalid`
  - `func (a App) ProviderConfig() map[string]string`

- [ ] **Step 1: Write the failing tests**

`internal/pimprovider/rules_test.go`:

```go
package pimprovider

import (
	"errors"
	"maps"
	"strings"
	"testing"
)

func TestCanonicalIsByteExact(t *testing.T) {
	for _, p := range []string{"google", "microsoft365", "outlook.com", "imap", "ics", "json"} {
		if !Canonical(p) {
			t.Errorf("Canonical(%q) = false", p)
		}
	}
	for _, p := range []string{"Google", "GOOGLE", " google", "google ", "Outlook.com", "", "m365"} {
		if Canonical(p) {
			t.Errorf("Canonical(%q) = true; the sidecar folds case, so only the exact id may pass", p)
		}
	}
}

func TestManagedIsTheThreeOAuthProviders(t *testing.T) {
	if got := strings.Join(ManagedProviders(), ","); got != "google,microsoft365,outlook.com" {
		t.Fatalf("ManagedProviders() = %s", got)
	}
	for _, p := range []string{"imap", "ics", "json", "Google"} {
		if Managed(p) {
			t.Errorf("Managed(%q) = true", p)
		}
	}
}

func TestIsOwnedKeyIgnoresCase(t *testing.T) {
	for _, k := range []string{"clientId", "ClientId", "CLIENTSECRET", "tenantid"} {
		if !IsOwnedKey(k) {
			t.Errorf("IsOwnedKey(%q) = false", k)
		}
	}
	for _, k := range []string{"icsUrl", "clientIdx", ""} {
		if IsOwnedKey(k) {
			t.Errorf("IsOwnedKey(%q) = true", k)
		}
	}
}

func TestProviderConfigCarriesOnlyTheProvidersKeys(t *testing.T) {
	g := App{Provider: Google, ClientID: "cid", ClientSecret: "sec", TenantID: "ignored"}
	if want := map[string]string{"clientId": "cid", "clientSecret": "sec"}; !maps.Equal(g.ProviderConfig(), want) {
		t.Fatalf("google config = %v, want %v", g.ProviderConfig(), want)
	}
	m := App{Provider: OutlookCom, ClientID: "cid", TenantID: "consumers", ClientSecret: "never"}
	if want := map[string]string{"clientId": "cid", "tenantId": "consumers"}; !maps.Equal(m.ProviderConfig(), want) {
		t.Fatalf("outlook config = %v, want %v", m.ProviderConfig(), want)
	}
}

func TestValidate(t *testing.T) {
	storedGoogle := &App{Provider: Google, ClientID: "cid", ClientSecret: "sec", SecretSet: true}
	for _, tc := range []struct {
		name   string
		next   App
		stored *App
		ok     bool
	}{
		{"google first save with secret", App{Provider: Google, ClientID: "cid", ClientSecret: "sec"}, nil, true},
		{"google first save without secret", App{Provider: Google, ClientID: "cid"}, nil, false},
		{"google same client keeps secret", App{Provider: Google, ClientID: "cid"}, storedGoogle, true},
		{"google new client without secret", App{Provider: Google, ClientID: "other"}, storedGoogle, false},
		{"google new client with secret", App{Provider: Google, ClientID: "other", ClientSecret: "s2"}, storedGoogle, true},
		{"google with tenant", App{Provider: Google, ClientID: "cid", ClientSecret: "sec", TenantID: "t"}, nil, false},
		{"google empty client", App{Provider: Google, ClientSecret: "sec"}, nil, false},
		{"outlook ok", App{Provider: OutlookCom, ClientID: "cid", TenantID: "consumers"}, nil, true},
		{"m365 ok", App{Provider: Microsoft365, ClientID: "cid", TenantID: "t"}, nil, true},
		{"microsoft without tenant", App{Provider: Microsoft365, ClientID: "cid"}, nil, false},
		{"microsoft with secret", App{Provider: OutlookCom, ClientID: "cid", TenantID: "consumers", ClientSecret: "x"}, nil, false},
		{"unmanaged", App{Provider: "imap", ClientID: "cid"}, nil, false},
	} {
		err := Validate(tc.next, tc.stored)
		if tc.ok && err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
		if !tc.ok && !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", tc.name, err)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/pimprovider/`
Expected: FAIL (package does not compile: undefined names).

- [ ] **Step 3: Implement**

`internal/pimprovider/rules.go`:

```go
// Package pimprovider holds the OAuth client an admin sets once per PIM provider, and the rules
// that decide which providers are managed that way. Aura injects the client into every
// account-create it forwards to the aura-pim-mcp sidecar, so members never type one
// (docs/superpowers/specs/2026-09-23-pim-provider-apps-design.md).
package pimprovider

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	Google       = "google"
	Microsoft365 = "microsoft365"
	OutlookCom   = "outlook.com"
)

var (
	ErrNotConfigured = errors.New("pimprovider: provider app not configured")
	ErrInvalid       = errors.New("pimprovider: invalid provider app")
)

// canonical is every id the cockpit sends. The sidecar matches provider names without regard to
// case, so anything but these exact bytes could name a managed provider while dodging injection.
var canonical = []string{Google, Microsoft365, OutlookCom, "imap", "ics", "json"}

var managed = []string{Google, Microsoft365, OutlookCom}

var ownedKeys = []string{"clientId", "clientSecret", "tenantId"}

// App is one provider's OAuth client. ClientSecret is plaintext and lives only in memory; on a
// save, an empty ClientSecret for Google means "keep the stored one".
type App struct {
	Provider     string
	ClientID     string
	TenantID     string
	ClientSecret string
	SecretSet    bool
	UpdatedAt    time.Time
	UpdatedBy    string
}

func Canonical(provider string) bool { return slices.Contains(canonical, provider) }

func Managed(provider string) bool { return slices.Contains(managed, provider) }

func ManagedProviders() []string { return slices.Clone(managed) }

// IsOwnedKey ignores case because the sidecar folds providerConfig keys; a leftover "ClientId"
// beside the injected "clientId" makes its dictionary copy throw.
func IsOwnedKey(key string) bool {
	return slices.ContainsFunc(ownedKeys, func(k string) bool { return strings.EqualFold(k, key) })
}

// ProviderConfig is exactly the providerConfig the sidecar reads for this provider.
func (a App) ProviderConfig() map[string]string {
	if a.Provider == Google {
		return map[string]string{"clientId": a.ClientID, "clientSecret": a.ClientSecret}
	}
	return map[string]string{"clientId": a.ClientID, "tenantId": a.TenantID}
}

// Validate checks a save against the stored app (nil when there is none). A Google save without
// a secret is accepted only for the same client ID: a new client without its own secret would
// save fine and then fail every consent at the code exchange, where the admin never looks.
func Validate(next App, stored *App) error {
	if !Managed(next.Provider) {
		return fmt.Errorf("%w: %q is not a managed provider", ErrInvalid, next.Provider)
	}
	if next.ClientID == "" {
		return fmt.Errorf("%w: clientId is required", ErrInvalid)
	}
	if next.Provider != Google {
		if next.TenantID == "" {
			return fmt.Errorf("%w: tenantId is required for %s", ErrInvalid, next.Provider)
		}
		if next.ClientSecret != "" {
			return fmt.Errorf("%w: %s uses a public client and takes no clientSecret", ErrInvalid, next.Provider)
		}
		return nil
	}
	if next.TenantID != "" {
		return fmt.Errorf("%w: google takes no tenantId", ErrInvalid)
	}
	if next.ClientSecret == "" && (stored == nil || !stored.SecretSet || stored.ClientID != next.ClientID) {
		return fmt.Errorf("%w: clientSecret is required for a new Google client", ErrInvalid)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests**

Run: `go test -race ./internal/pimprovider/ && go vet ./internal/pimprovider/`
Expected: PASS, and vet is clean.

- [ ] **Step 5: Commit**

```bash
git add internal/pimprovider/rules.go internal/pimprovider/rules_test.go
git commit -m "feat(pimprovider): add the managed-provider rules for admin-set OAuth clients"
```

---

### Task 3: Sealed store and coverage policy

**Files:**
- Create: `internal/pimprovider/store.go`
- Create: `internal/pimprovider/store_test.go` (unit: `NewStore` guards)
- Test: `internal/pimprovider/store_integration_test.go` (`db_integration`)
- Modify: `scripts/coverage_package_policy.json`. Add the package in sorted position.

**Interfaces:**
- Consumes:
  - Task 1's sqlc names;
  - Task 2's `App`, `Google`, `ErrNotConfigured`;
  - `secret.NewSealer(secretHex, info string) (*secret.Sealer, error)`, `SealOptional`,
    `OpenOptional`.
- Produces:
  - `func NewStore(pool *pgxpool.Pool, authulaSecretHex string) (*Store, error)`
  - `func (s *Store) List(ctx) ([]App, error)`: `SecretSet` is populated and `ClientSecret` is
    never decrypted.
  - `func (s *Store) Get(ctx, provider string) (App, error)`: decrypts `ClientSecret`; returns
    `ErrNotConfigured` when there is no row.
  - `func (s *Store) Upsert(ctx, app App, updatedBy string) error`

- [ ] **Step 1: Write the failing tests**

`internal/pimprovider/store_test.go`:

```go
package pimprovider

import (
	"strings"
	"testing"
)

const testSecretHex = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestNewStoreRequiresAPool(t *testing.T) {
	if _, err := NewStore(nil, testSecretHex); err == nil || !strings.Contains(err.Error(), "pool") {
		t.Fatalf("NewStore(nil pool) err = %v, want a pool error", err)
	}
}
```

`internal/pimprovider/store_integration_test.go`:

```go
//go:build db_integration

package pimprovider

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/secret"
)

func appsEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("pimprovider integration requires %s under CI", key)
		}
		t.Skipf("pimprovider integration requires %s", key)
	}
	return value
}

func liveStore(t *testing.T) (*Store, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, &db.Config{URL: appsEnvOrSkip(t, "AURA_DB_URL")})
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(pool.Close)
	clean := func() { _, _ = pool.Exec(context.Background(), `DELETE FROM aura.pim_provider_app`) }
	clean()
	t.Cleanup(clean)
	store, err := NewStore(pool, appsEnvOrSkip(t, "AURA_AUTHULA_SECRET"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store, pool
}

func TestStoreSealsTheGoogleSecretAndKeepsItOnAResave(t *testing.T) {
	store, pool := liveStore(t)
	ctx := context.Background()

	if _, err := store.Get(ctx, Google); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Get before save err = %v, want ErrNotConfigured", err)
	}
	if err := store.Upsert(ctx, App{Provider: Google, ClientID: "cid-1", ClientSecret: "plain-secret"}, "admin-1"); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	var ciphertext []byte
	if err := pool.QueryRow(ctx, `SELECT client_secret_ciphertext FROM aura.pim_provider_app WHERE provider = 'google'`).Scan(&ciphertext); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	if len(ciphertext) == 0 || bytes.Contains(ciphertext, []byte("plain-secret")) {
		t.Fatal("client_secret_ciphertext is empty or holds the plaintext")
	}
	other, err := secret.NewSealer(appsEnvOrSkip(t, "AURA_AUTHULA_SECRET"), "aura-mcp-registry-key-v1")
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	if _, err := other.Open(ciphertext); err == nil {
		t.Fatal("another store's key opened the PIM provider secret")
	}

	// Same client, empty secret: the keep-secret UPDATE, not the upsert the row CHECK rejects.
	if err := store.Upsert(ctx, App{Provider: Google, ClientID: "cid-1"}, "admin-2"); err != nil {
		t.Fatalf("resave without secret: %v", err)
	}
	got, err := store.Get(ctx, Google)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ClientSecret != "plain-secret" || !got.SecretSet || got.UpdatedBy != "admin-2" {
		t.Fatalf("after resave = %+v, want the stored secret kept and updated_by admin-2", got)
	}
}

func TestStoreKeepSecretOnAMissingRowIsNotConfigured(t *testing.T) {
	store, _ := liveStore(t)
	err := store.Upsert(context.Background(), App{Provider: Google, ClientID: "cid"}, "admin")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("keep-secret on no row err = %v, want ErrNotConfigured", err)
	}
}

func TestStoreListsWithoutDecrypting(t *testing.T) {
	store, _ := liveStore(t)
	ctx := context.Background()
	if err := store.Upsert(ctx, App{Provider: OutlookCom, ClientID: "ms-cid", TenantID: "consumers"}, "admin"); err != nil {
		t.Fatalf("Upsert outlook: %v", err)
	}
	if err := store.Upsert(ctx, App{Provider: Google, ClientID: "g-cid", ClientSecret: "s"}, "admin"); err != nil {
		t.Fatalf("Upsert google: %v", err)
	}
	apps, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(apps) != 2 || apps[0].Provider != Google || apps[1].Provider != OutlookCom {
		t.Fatalf("List = %+v, want google then outlook.com", apps)
	}
	if !apps[0].SecretSet || apps[0].ClientSecret != "" || apps[1].SecretSet || apps[1].TenantID != "consumers" {
		t.Fatalf("List rows = %+v", apps)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/pimprovider/` and
`go test -tags db_integration -run TestStore -count=1 ./internal/pimprovider/`
Expected: FAIL (`NewStore` undefined).

- [ ] **Step 3: Implement**

`internal/pimprovider/store.go`:

```go
package pimprovider

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/secret"
)

// keyDerivationInfo must differ from every other store's label: two stores sharing one would
// share a key, so one leak would open both.
const keyDerivationInfo = "aura-pim-provider-app-key-v1"

// Store is the Postgres table aura.pim_provider_app.
type Store struct {
	q      *sqlc.Queries
	sealer *secret.Sealer
}

func NewStore(pool *pgxpool.Pool, authulaSecretHex string) (*Store, error) {
	if pool == nil {
		return nil, errors.New("pimprovider: a database pool is required")
	}
	sealer, err := secret.NewSealer(authulaSecretHex, keyDerivationInfo)
	if err != nil {
		return nil, err
	}
	return &Store{q: sqlc.New(pool), sealer: sealer}, nil
}

func (s *Store) List(ctx context.Context) ([]App, error) {
	rows, err := s.q.ListPIMProviderApps(ctx)
	if err != nil {
		return nil, fmt.Errorf("pimprovider: list: %w", err)
	}
	apps := make([]App, 0, len(rows))
	for _, row := range rows {
		apps = append(apps, App{
			Provider:  row.Provider,
			ClientID:  row.ClientID,
			TenantID:  row.TenantID,
			SecretSet: row.SecretSet,
			UpdatedAt: row.UpdatedAt.Time,
			UpdatedBy: row.UpdatedBy,
		})
	}
	return apps, nil
}

func (s *Store) Get(ctx context.Context, provider string) (App, error) {
	row, err := s.q.GetPIMProviderApp(ctx, provider)
	if errors.Is(err, pgx.ErrNoRows) {
		return App{}, ErrNotConfigured
	}
	if err != nil {
		return App{}, fmt.Errorf("pimprovider: get %s: %w", provider, err)
	}
	plaintext, err := s.sealer.OpenOptional(row.ClientSecretCiphertext)
	if err != nil {
		return App{}, fmt.Errorf("pimprovider: open the %s secret: %w", provider, err)
	}
	return App{
		Provider:     row.Provider,
		ClientID:     row.ClientID,
		TenantID:     row.TenantID,
		ClientSecret: string(plaintext),
		SecretSet:    len(row.ClientSecretCiphertext) > 0,
		UpdatedAt:    row.UpdatedAt.Time,
		UpdatedBy:    row.UpdatedBy,
	}, nil
}

// Upsert saves app. A Google app without a secret keeps the stored one through a plain UPDATE:
// Postgres checks the row CHECK against the proposed row of an INSERT … ON CONFLICT before it
// handles the conflict, so a ('google', NULL) upsert fails even when a secret is stored.
func (s *Store) Upsert(ctx context.Context, app App, updatedBy string) error {
	if app.Provider == Google && app.ClientSecret == "" {
		n, err := s.q.UpdatePIMProviderAppKeepSecret(ctx, sqlc.UpdatePIMProviderAppKeepSecretParams{
			Provider: app.Provider, ClientID: app.ClientID, TenantID: app.TenantID, UpdatedBy: updatedBy,
		})
		if err != nil {
			return fmt.Errorf("pimprovider: update %s: %w", app.Provider, err)
		}
		if n == 0 {
			return ErrNotConfigured
		}
		return nil
	}
	ciphertext, err := s.sealer.SealOptional([]byte(app.ClientSecret))
	if err != nil {
		return fmt.Errorf("pimprovider: seal the %s secret: %w", app.Provider, err)
	}
	if err := s.q.UpsertPIMProviderApp(ctx, sqlc.UpsertPIMProviderAppParams{
		Provider: app.Provider, ClientID: app.ClientID, TenantID: app.TenantID,
		ClientSecretCiphertext: ciphertext, UpdatedBy: updatedBy,
	}); err != nil {
		return fmt.Errorf("pimprovider: upsert %s: %w", app.Provider, err)
	}
	return nil
}
```

Add to `scripts/coverage_package_policy.json`, alphabetically after `internal/packs` or wherever
it sorts:

```json
    "github.com/chetto1983/aura/internal/pimprovider": {"mode": "target"},
```

- [ ] **Step 4: Run the tests**

Run: `go test -race ./internal/pimprovider/ && go test -tags db_integration -race -count=1 ./internal/pimprovider/`
Expected: PASS. Then run `go test -tags db_integration -coverprofile=/tmp/pp.out ./internal/pimprovider/ && go tool cover -func=/tmp/pp.out | tail -1`:
the total must be ≥85%. If it falls short, add rules cases, not trivial tests.

- [ ] **Step 5: Commit**

```bash
git add internal/pimprovider/store.go internal/pimprovider/store_test.go internal/pimprovider/store_integration_test.go scripts/coverage_package_policy.json
git commit -m "feat(pimprovider): seal the admin-set PIM OAuth clients in Postgres"
```

---

### Task 4: Provider routes in `internal/agui`

**Files:**
- Create: `internal/agui/connect_pim_providers_api.go`
- Modify: `internal/agui/server.go`. Add the field `pimApps pimProviderApps` next to
  `calendarMCPAuth`.
- Modify: `internal/agui/connect_pim_api.go`. Register the two routes in
  `registerConnectPIMRoutes`.
- Modify: `internal/agui/idempotency_http.go`. Add the inventory entry after the PIM entries.
- Test: `internal/agui/connect_pim_providers_api_test.go`

**Interfaces:**
- Consumes: Task 2 (`pimprovider.*`), `principalIdentityID(r) (string, bool)`,
  `readCappedBody(w, r) ([]byte, bool)`, `writeJSON(w, v)`, `writeJSONStatus(w, code, v)`,
  `s.idAdmin.HasCapability`.
- Produces:
  - `const PIMGoogleRelayRedirectURI`
  - `type pimProviderApps interface{ List; Get; Upsert }`, with the same signatures as
    `*pimprovider.Store`
  - `func (s *Server) SetPIMProviderApps(apps pimProviderApps)`
  - the test fake `fakePIMApps` and the helper
    `connectPIMServerWithApps(baseURL string, apps pimProviderApps, admin identityAdmin) *httptest.Server`,
    both reused by Task 5

- [ ] **Step 1: Write the failing tests**

`internal/agui/connect_pim_providers_api_test.go`:

```go
package agui

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/pimprovider"
)

type fakePIMApps struct {
	apps     map[string]pimprovider.App
	getErr   error
	upserted []pimprovider.App
	by       []string
}

func (f *fakePIMApps) List(context.Context) ([]pimprovider.App, error) {
	out := make([]pimprovider.App, 0, len(f.apps))
	for _, key := range slices.Sorted(maps.Keys(f.apps)) {
		app := f.apps[key]
		app.ClientSecret = ""
		out = append(out, app)
	}
	return out, nil
}

func (f *fakePIMApps) Get(_ context.Context, provider string) (pimprovider.App, error) {
	if f.getErr != nil {
		return pimprovider.App{}, f.getErr
	}
	app, ok := f.apps[provider]
	if !ok {
		return pimprovider.App{}, pimprovider.ErrNotConfigured
	}
	return app, nil
}

func (f *fakePIMApps) Upsert(_ context.Context, app pimprovider.App, updatedBy string) error {
	f.upserted = append(f.upserted, app)
	f.by = append(f.by, updatedBy)
	if f.apps == nil {
		f.apps = map[string]pimprovider.App{}
	}
	if app.ClientSecret == "" {
		app.ClientSecret = f.apps[app.Provider].ClientSecret
	}
	app.SecretSet = app.ClientSecret != ""
	f.apps[app.Provider] = app
	return nil
}

func connectPIMServerWithApps(baseURL string, apps pimProviderApps, admin identityAdmin) *httptest.Server {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	if baseURL != "" {
		s.SetCalendarMCP(baseURL, "", staticMCPAccessTokenProvider{token: fakePIMToken})
	}
	if apps != nil {
		s.SetPIMProviderApps(apps)
	}
	if admin != nil {
		s.SetIdentityAdmin(admin)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Mux().ServeHTTP(w, withPrincipal(r, fakePIMIdentity))
	}))
}

func adminOf(id string) identityAdmin {
	return &fakeIdentityAdmin{caps: map[string][]string{id: {identity.CapIdentityCreate}}}
}

func googleApp() map[string]pimprovider.App {
	return map[string]pimprovider.App{pimprovider.Google: {
		Provider: pimprovider.Google, ClientID: "g-cid", ClientSecret: "g-secret", SecretSet: true,
	}}
}

func getProviders(t *testing.T, srv *httptest.Server) (int, string, []map[string]any) {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/connect/pim/providers")
	if err != nil {
		t.Fatalf("GET providers: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var body struct {
		Providers []map[string]any `json:"providers"`
	}
	_ = json.Unmarshal(raw, &body)
	return resp.StatusCode, string(raw), body.Providers
}

func putProvider(t *testing.T, srv *httptest.Server, provider, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/connect/pim/providers/"+provider, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", provider, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestPIMProvidersMemberSeesOnlyConfigured(t *testing.T) {
	srv := connectPIMServerWithApps("", &fakePIMApps{apps: googleApp()}, &fakeIdentityAdmin{})
	defer srv.Close()
	code, raw, providers := getProviders(t, srv)
	if code != http.StatusOK || len(providers) != 3 {
		t.Fatalf("GET = %d %s", code, raw)
	}
	for i, want := range []string{"google", "microsoft365", "outlook.com"} {
		keys := slices.Sorted(maps.Keys(providers[i]))
		if providers[i]["provider"] != want || !slices.Equal(keys, []string{"configured", "provider"}) {
			t.Fatalf("member row %d = %v, want only provider+configured for %s", i, providers[i], want)
		}
	}
	if providers[0]["configured"] != true || providers[1]["configured"] != false {
		t.Fatalf("configured flags = %v", providers)
	}
	if strings.Contains(raw, "g-cid") || strings.Contains(raw, "g-secret") {
		t.Fatalf("member view leaks the client: %s", raw)
	}
}

func TestPIMProvidersAdminSeesClientButNeverSecret(t *testing.T) {
	srv := connectPIMServerWithApps("", &fakePIMApps{apps: googleApp()}, adminOf(fakePIMIdentity))
	defer srv.Close()
	_, raw, providers := getProviders(t, srv)
	g := providers[0]
	if g["clientId"] != "g-cid" || g["secretSet"] != true || g["redirectUri"] != PIMGoogleRelayRedirectURI {
		t.Fatalf("admin google row = %v", g)
	}
	if _, ok := providers[1]["redirectUri"]; ok {
		t.Fatalf("microsoft row carries a redirect URI: %v", providers[1])
	}
	if strings.Contains(raw, "g-secret") {
		t.Fatalf("admin view leaks the secret: %s", raw)
	}
}

func TestPIMProviderPutSavesAndStampsThePrincipal(t *testing.T) {
	apps := &fakePIMApps{}
	srv := connectPIMServerWithApps("", apps, adminOf(fakePIMIdentity))
	defer srv.Close()
	code, raw := putProvider(t, srv, "google", `{"clientId":" g-cid ","clientSecret":"g-secret"}`)
	if code != http.StatusOK || strings.Contains(raw, "g-secret") || !strings.Contains(raw, `"secretSet":true`) {
		t.Fatalf("PUT = %d %s", code, raw)
	}
	if len(apps.upserted) != 1 || apps.upserted[0].ClientID != "g-cid" || apps.by[0] != fakePIMIdentity {
		t.Fatalf("upserted %+v by %v", apps.upserted, apps.by)
	}
}

func TestPIMProviderPutSecretRules(t *testing.T) {
	apps := &fakePIMApps{apps: googleApp()}
	srv := connectPIMServerWithApps("", apps, adminOf(fakePIMIdentity))
	defer srv.Close()
	if code, raw := putProvider(t, srv, "google", `{"clientId":"new-cid"}`); code != http.StatusBadRequest || !strings.Contains(raw, "clientSecret") {
		t.Fatalf("new client without secret = %d %s, want 400 naming clientSecret", code, raw)
	}
	if code, _ := putProvider(t, srv, "google", `{"clientId":"g-cid"}`); code != http.StatusOK {
		t.Fatalf("same client without secret = %d, want 200", code)
	}
	if apps.apps["google"].ClientSecret != "g-secret" {
		t.Fatal("the stored secret was not kept")
	}
	if code, _ := putProvider(t, srv, "outlook.com", `{"clientId":"m","tenantId":"consumers","clientSecret":"x"}`); code != http.StatusBadRequest {
		t.Fatalf("microsoft with secret = %d, want 400", code)
	}
	if code, _ := putProvider(t, srv, "microsoft365", `{"clientId":"m"}`); code != http.StatusBadRequest {
		t.Fatalf("microsoft without tenant = %d, want 400", code)
	}
	if code, _ := putProvider(t, srv, "google", `{`); code != http.StatusBadRequest {
		t.Fatalf("malformed JSON = %d, want 400", code)
	}
}

func TestPIMProviderPutUnknownProvider404(t *testing.T) {
	srv := connectPIMServerWithApps("", &fakePIMApps{}, adminOf(fakePIMIdentity))
	defer srv.Close()
	for _, p := range []string{"imap", "Google", "nope"} {
		if code, _ := putProvider(t, srv, p, `{"clientId":"c"}`); code != http.StatusNotFound {
			t.Errorf("PUT %s = %d, want 404", p, code)
		}
	}
}

func TestPIMProvidersWithoutStore503(t *testing.T) {
	srv := connectPIMServerWithApps("", nil, adminOf(fakePIMIdentity))
	defer srv.Close()
	if code, _, _ := getProviders(t, srv); code != http.StatusServiceUnavailable {
		t.Fatalf("GET without store = %d, want 503", code)
	}
	if code, _ := putProvider(t, srv, "google", `{"clientId":"c","clientSecret":"s"}`); code != http.StatusServiceUnavailable {
		t.Fatalf("PUT without store = %d, want 503", code)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -run 'TestPIMProvider' ./internal/agui/`
Expected: FAIL (undefined: `pimProviderApps`, `SetPIMProviderApps`, `PIMGoogleRelayRedirectURI`).

- [ ] **Step 3: Implement**

`internal/agui/connect_pim_providers_api.go`:

```go
package agui

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/pimprovider"
)

// PIMGoogleRelayRedirectURI is the one redirect URI every install registers in its Google Web
// client. It must equal the sidecar's GoogleOAuthRelayUrl default (chetto1983/aura-pim-mcp,
// src/CalendarMcp.Core/Configuration/CalendarMcpConfiguration.cs); the relay's address is fixed
// by design, which is why a constant is enough for the admin panel.
const PIMGoogleRelayRedirectURI = "https://chetto1983.github.io/aura-connect/google/callback/"

type pimProviderApps interface {
	List(ctx context.Context) ([]pimprovider.App, error)
	Get(ctx context.Context, provider string) (pimprovider.App, error)
	Upsert(ctx context.Context, app pimprovider.App, updatedBy string) error
}

// SetPIMProviderApps wires the admin-set OAuth clients. Until it is called the provider routes
// and every managed-provider account create answer 503.
func (s *Server) SetPIMProviderApps(apps pimProviderApps) { s.pimApps = apps }

type pimProviderStatus struct {
	Provider   string `json:"provider"`
	Configured bool   `json:"configured"`
}

type pimProviderAdminStatus struct {
	pimProviderStatus
	ClientID    string `json:"clientId"`
	TenantID    string `json:"tenantId"`
	SecretSet   bool   `json:"secretSet"`
	RedirectURI string `json:"redirectUri,omitempty"`
}

var errPIMAppsUnavailable = map[string]string{"error": "provider apps unavailable"}

// handlePIMProvidersList serves GET /api/connect/pim/providers. A member learns only whether each
// managed provider is ready to connect; the client ID and tenant are for the admin who edits them.
func (s *Server) handlePIMProvidersList(w http.ResponseWriter, r *http.Request) {
	if s.pimApps == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, errPIMAppsUnavailable)
		return
	}
	apps, err := s.pimApps.List(r.Context())
	if err != nil {
		slog.Error("pim providers: list failed", "err", err)
		writeJSONStatus(w, http.StatusServiceUnavailable, errPIMAppsUnavailable)
		return
	}
	byProvider := make(map[string]pimprovider.App, len(apps))
	for _, app := range apps {
		byProvider[app.Provider] = app
	}
	admin := s.callerIsAdmin(r)
	views := make([]any, 0, len(pimprovider.ManagedProviders()))
	for _, provider := range pimprovider.ManagedProviders() {
		app, configured := byProvider[provider]
		views = append(views, pimProviderView(provider, app, configured, admin))
	}
	writeJSON(w, map[string]any{"providers": views})
}

// handlePIMProviderPut serves PUT /api/connect/pim/providers/{provider}. The identity.create gate
// is on the mount in cmd/aura/serve_webui.go.
func (s *Server) handlePIMProviderPut(w http.ResponseWriter, r *http.Request) {
	provider := r.PathValue("provider")
	if !pimprovider.Managed(provider) {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "unknown provider"})
		return
	}
	if s.pimApps == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, errPIMAppsUnavailable)
		return
	}
	body, ok := readCappedBody(w, r)
	if !ok {
		return
	}
	var req struct {
		ClientID     string `json:"clientId"`
		TenantID     string `json:"tenantId"`
		ClientSecret string `json:"clientSecret"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	next := pimprovider.App{
		Provider:     provider,
		ClientID:     strings.TrimSpace(req.ClientID),
		TenantID:     strings.TrimSpace(req.TenantID),
		ClientSecret: strings.TrimSpace(req.ClientSecret),
	}
	var stored *pimprovider.App
	current, err := s.pimApps.Get(r.Context(), provider)
	switch {
	case err == nil:
		stored = &current
	case !errors.Is(err, pimprovider.ErrNotConfigured):
		slog.Error("pim providers: read before save failed", "provider", provider, "err", err)
		writeJSONStatus(w, http.StatusServiceUnavailable, errPIMAppsUnavailable)
		return
	}
	if err := pimprovider.Validate(next, stored); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	updatedBy, _ := principalIdentityID(r)
	if err := s.pimApps.Upsert(r.Context(), next, updatedBy); err != nil {
		slog.Error("pim providers: save failed", "provider", provider, "err", err)
		writeJSONStatus(w, http.StatusServiceUnavailable, errPIMAppsUnavailable)
		return
	}
	saved := next
	saved.SecretSet = provider == pimprovider.Google
	writeJSON(w, pimProviderView(provider, saved, true, true))
}

func pimProviderView(provider string, app pimprovider.App, configured, admin bool) any {
	status := pimProviderStatus{Provider: provider, Configured: configured}
	if !admin {
		return status
	}
	view := pimProviderAdminStatus{
		pimProviderStatus: status,
		ClientID:          app.ClientID,
		TenantID:          app.TenantID,
		SecretSet:         app.SecretSet,
	}
	if provider == pimprovider.Google {
		view.RedirectURI = PIMGoogleRelayRedirectURI
	}
	return view
}

// callerIsAdmin fails closed: no identity seam, no principal or a failed read all mean "member".
func (s *Server) callerIsAdmin(r *http.Request) bool {
	id, ok := principalIdentityID(r)
	if !ok || s.idAdmin == nil {
		return false
	}
	admin, err := s.idAdmin.HasCapability(r.Context(), id, identity.CapIdentityCreate)
	if err != nil {
		slog.Warn("pim providers: admin check failed", "err", err)
		return false
	}
	return admin
}
```

In `internal/agui/server.go`, next to `calendarMCPAuth MCPAccessTokenProvider`:

```go
	pimApps           pimProviderApps
```

In `registerConnectPIMRoutes` (`connect_pim_api.go`), before the callback line:

```go
	mux.HandleFunc("GET /api/connect/pim/providers", s.handlePIMProvidersList)
	mux.HandleFunc("PUT /api/connect/pim/providers/{provider}", s.handlePIMProviderPut)
```

Also update that function's doc comment: the providers `GET` rides `governance.write`, and the
`PUT` rides `identity.create`.

In `idempotency_http.go` `httpMutationRoutes`, after `"POST /api/connect/pim/accounts/{id}/logout"`:

```go
	"PUT /api/connect/pim/providers/{provider}":                httpMutationMeta("pim_provider_app_put"),
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./internal/agui/ && CGO_ENABLED=1 go test -race -run 'TestPIMProvider|TestEveryRegisteredUnsafeHTTPRouteIsClassified' ./internal/agui/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agui/connect_pim_providers_api.go internal/agui/connect_pim_providers_api_test.go internal/agui/server.go internal/agui/connect_pim_api.go internal/agui/idempotency_http.go
git commit -m "feat(agui): serve the admin-set PIM provider apps"
```

---

### Task 5: Inject the app into account creates

**Files:**
- Create: `internal/agui/connect_pim_inject.go`
- Modify: `internal/agui/connect_pim_api.go`, `handlePIMCreateAccount` (lines 102-111). Update
  its doc comment and the file header, which say the operator enters their own credentials.
- Modify: `internal/agui/connect_pim_api_test.go`, `TestPIMCreateAccountForwardsBody`. Switch it
  to `ics`: Google is now managed, and this test is about plain forwarding. Say so in the commit
  body.
- Test: `internal/agui/connect_pim_inject_test.go`

**Interfaces:**
- Consumes: Task 2 (`Canonical`, `Managed`, `IsOwnedKey`, `App.ProviderConfig`,
  `ErrNotConfigured`), and Task 4 (`fakePIMApps`, `connectPIMServerWithApps`, `googleApp`).
- Produces: `func (s *Server) injectPIMProviderApp(ctx context.Context, body []byte) ([]byte, int, string)`.
  A status of 0 means forward the returned body.

- [ ] **Step 1: Write the failing tests**

`internal/agui/connect_pim_inject_test.go`:

```go
package agui

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/pimprovider"
)

func postAccount(t *testing.T, srv *httptest.Server, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/connect/pim/accounts", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST accounts: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func forwardedConfig(t *testing.T, p *fakePIM) (map[string]json.RawMessage, map[string]string) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(p.gotBody), &fields); err != nil {
		t.Fatalf("forwarded body %q: %v", p.gotBody, err)
	}
	var config map[string]string
	if err := json.Unmarshal(fields["providerConfig"], &config); err != nil {
		t.Fatalf("forwarded providerConfig: %v", err)
	}
	return fields, config
}

func TestPIMCreateInjectsTheGoogleAppAndDropsBrowserKeysInAnyCase(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServerWithApps(sidecar.URL, &fakePIMApps{apps: googleApp()}, nil)
	defer srv.Close()

	in := `{"id":"work","displayName":"Work","provider":"google","priority":3,
	  "providerConfig":{"clientId":"mine","ClientSecret":"mine","CLIENTSECRET":"x","TenantId":"t"}}`
	if code, raw := postAccount(t, srv, in); code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, raw)
	}
	fields, config := forwardedConfig(t, p)
	if want := map[string]string{"clientId": "g-cid", "clientSecret": "g-secret"}; !maps.Equal(config, want) {
		t.Fatalf("forwarded providerConfig = %v, want exactly %v", config, want)
	}
	if string(fields["priority"]) != "3" || string(fields["id"]) != `"work"` {
		t.Fatalf("other fields not kept: %s", p.gotBody)
	}
}

func TestPIMCreateInjectsTheMicrosoftAppWithoutASecretKey(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	apps := &fakePIMApps{apps: map[string]pimprovider.App{pimprovider.OutlookCom: {
		Provider: pimprovider.OutlookCom, ClientID: "ms-cid", TenantID: "consumers",
	}}}
	srv := connectPIMServerWithApps(sidecar.URL, apps, nil)
	defer srv.Close()

	if code, raw := postAccount(t, srv, `{"id":"home","displayName":"Home","provider":"outlook.com","providerConfig":{"clientSecret":"x"}}`); code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, raw)
	}
	_, config := forwardedConfig(t, p)
	if want := map[string]string{"clientId": "ms-cid", "tenantId": "consumers"}; !maps.Equal(config, want) {
		t.Fatalf("forwarded providerConfig = %v, want exactly %v", config, want)
	}
}

func TestPIMCreateRejectsANonCanonicalProvider(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServerWithApps(sidecar.URL, &fakePIMApps{apps: googleApp()}, nil)
	defer srv.Close()
	for _, provider := range []string{`"Google"`, `"GOOGLE"`, `" google"`, `""`, `null`, `7`} {
		body := `{"id":"x","displayName":"X","provider":` + provider + `,"providerConfig":{"clientId":"mine","clientSecret":"mine"}}`
		if code, _ := postAccount(t, srv, body); code != http.StatusBadRequest {
			t.Errorf("provider %s = %d, want 400", provider, code)
		}
	}
	if code, _ := postAccount(t, srv, `{"id":"x"}`); code != http.StatusBadRequest {
		t.Errorf("missing provider = %d, want 400", code)
	}
	if code, _ := postAccount(t, srv, `not json`); code != http.StatusBadRequest {
		t.Errorf("malformed body = %d, want 400", code)
	}
	if p.gotPath != "" {
		t.Fatalf("a rejected create reached the sidecar: %s", p.gotPath)
	}
}

func TestPIMCreateUnconfiguredManagedProviderIs409WithoutTheSidecar(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServerWithApps(sidecar.URL, &fakePIMApps{}, nil)
	defer srv.Close()
	code, raw := postAccount(t, srv, `{"id":"w","displayName":"W","provider":"microsoft365","providerConfig":{}}`)
	if code != http.StatusConflict || !strings.Contains(raw, `"provider_not_configured"`) {
		t.Fatalf("unconfigured = %d %s, want 409 provider_not_configured", code, raw)
	}
	if p.gotPath != "" {
		t.Fatal("an unconfigured create reached the sidecar")
	}
}

func TestPIMCreateManagedProviderWithoutStoreIs503(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServerWithApps(sidecar.URL, nil, nil)
	defer srv.Close()
	if code, _ := postAccount(t, srv, `{"id":"w","displayName":"W","provider":"google","providerConfig":{}}`); code != http.StatusServiceUnavailable {
		t.Fatalf("no store = %d, want 503", code)
	}
}

func TestPIMCreateUnmanagedProviderPassesThroughUnchanged(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServerWithApps(sidecar.URL, nil, nil)
	defer srv.Close()
	in := `{"id":"cal","displayName":"Cal","provider":"ics","providerConfig":{"icsUrl":"https://example.com/a.ics"}}`
	if code, raw := postAccount(t, srv, in); code != http.StatusCreated {
		t.Fatalf("ics create = %d %s", code, raw)
	}
	if p.gotBody != in {
		t.Fatalf("unmanaged body changed: %q", p.gotBody)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test -run 'TestPIMCreate' ./internal/agui/`
Expected: FAIL. `TestPIMCreateInjects…` sees the browser's `mine`, the canonical test gets 201,
and the 409 test gets 201.

- [ ] **Step 3: Implement**

`internal/agui/connect_pim_inject.go`:

```go
package agui

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net/http"

	"github.com/chetto1983/aura/internal/pimprovider"
)

// injectPIMProviderApp rewrites an account-create body before it reaches the sidecar. For a
// managed provider it replaces the browser's credentials with the admin-set app, so a member
// cannot choose the OAuth client. A non-zero status is the answer to send instead of forwarding.
//
// Aura mounts no account-update proxy. Any future one must run this same rewrite, because the
// sidecar's PUT replaces providerConfig wholesale.
func (s *Server) injectPIMProviderApp(ctx context.Context, body []byte) ([]byte, int, string) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, http.StatusBadRequest, "invalid JSON body"
	}
	var provider string
	if err := json.Unmarshal(fields["provider"], &provider); err != nil || !pimprovider.Canonical(provider) {
		return nil, http.StatusBadRequest, "unknown provider"
	}
	if !pimprovider.Managed(provider) {
		return body, 0, ""
	}
	if s.pimApps == nil {
		return nil, http.StatusServiceUnavailable, "provider apps unavailable"
	}
	app, err := s.pimApps.Get(ctx, provider)
	if errors.Is(err, pimprovider.ErrNotConfigured) {
		return nil, http.StatusConflict, "provider_not_configured"
	}
	if err != nil {
		slog.Error("pim create: provider app read failed", "provider", provider, "err", err)
		return nil, http.StatusServiceUnavailable, "provider apps unavailable"
	}
	config := map[string]string{}
	if raw, ok := fields["providerConfig"]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, http.StatusBadRequest, "invalid providerConfig"
		}
	}
	maps.DeleteFunc(config, func(key, _ string) bool { return pimprovider.IsOwnedKey(key) })
	maps.Copy(config, app.ProviderConfig())
	rewritten, err := json.Marshal(config)
	if err != nil {
		return nil, http.StatusInternalServerError, "providerConfig encoding failed"
	}
	fields["providerConfig"] = rewritten
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, http.StatusInternalServerError, "body encoding failed"
	}
	return out, 0, ""
}
```

Replace `handlePIMCreateAccount` in `connect_pim_api.go`:

```go
// handlePIMCreateAccount serves POST /api/connect/pim/accounts: forward the sidecar
// POST /admin/accounts after injectPIMProviderApp has put the admin-set OAuth client into a
// managed provider's providerConfig.
func (s *Server) handlePIMCreateAccount(w http.ResponseWriter, r *http.Request) {
	body, ok := readCappedBody(w, r)
	if !ok {
		return
	}
	body, status, reason := s.injectPIMProviderApp(r.Context(), body)
	if status != 0 {
		writeJSONStatus(w, status, map[string]string{"error": reason})
		return
	}
	s.forwardPIMJSON(w, r, http.MethodPost, "/admin/accounts", body)
}
```

In `TestPIMCreateAccountForwardsBody`, replace the body and the assertion:

```go
	in := `{"id":"work","displayName":"Work","provider":"ics","providerConfig":{"icsUrl":"https://example.com/w.ics"}}`
	…
	if !strings.Contains(p.gotBody, `"icsUrl":"https://example.com/w.ics"`) {
		t.Fatalf("create body not forwarded: %q", p.gotBody)
	}
```

- [ ] **Step 4: Run the package**

Run: `go vet ./internal/agui/ && CGO_ENABLED=1 go test -race -count=1 ./internal/agui/`
Expected: PASS for the whole package. Other tests may post a Google create; convert those to
`ics` the same way and note each one in the commit body.

- [ ] **Step 5: Commit**

```bash
git add internal/agui/connect_pim_inject.go internal/agui/connect_pim_inject_test.go internal/agui/connect_pim_api.go internal/agui/connect_pim_api_test.go
git commit -m "feat(agui): inject the admin-set OAuth client into PIM account creates"
```

Commit body: name the case-folding bypass that the canonical check closes, and why the
forwarding test moved to `ics`.

---

### Task 6: Daemon wiring and route gates

**Files:**
- Create: `cmd/aura/serve_pim_providers.go`
- Modify: `cmd/aura/serve_agui.go`. Call `wirePIMProviderApps(aguiServer, chat)` right after
  `wireSettingsProviders(aguiServer, chat)` at line 222.
- Modify: `cmd/aura/serve_webui_routes.go`. Add two constants to the `connectPIM*` block at
  lines 262-279.
- Modify: `cmd/aura/serve_webui.go`. Add two mounts after `connectPIMAuthCancelRoute`.
- Test: `cmd/aura/pim_provider_routes_test.go`

**Interfaces:**
- Consumes: `pimprovider.NewStore`, `agui.(*Server).SetPIMProviderApps`, `chat.pool`,
  `chat.cfg.AuthulaSecret`, and the test helpers `authulaTestDeps`, `newServeHandler`,
  `fakeAuthulaProvider`, `addAuthulaSession`, `errWiringNotFound` (in
  `serve_webui_auth_test.go`).

- [ ] **Step 1: Write the failing test**

`cmd/aura/pim_provider_routes_test.go`:

```go
package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/identity"
)

// memberIdentities holds exactly the capabilities every identity gets at provisioning, so it
// passes governance.write and fails identity.create — the line between member and admin.
type memberIdentities struct{ id string }

func (m memberIdentities) GetIdentityByID(_ context.Context, id string) (agui.Identity, error) {
	if id != m.id {
		return agui.Identity{}, errWiringNotFound
	}
	return agui.Identity{ID: id, Name: "member", Kind: "user"}, nil
}

func (m memberIdentities) HasCapability(_ context.Context, id, capability string) (bool, error) {
	return id == m.id && slices.Contains(identity.UserSet(), capability), nil
}

func TestPIMProviderRoutesSplitMemberAndAdmin(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	auth := authulaTestDeps(localID, memberIdentities{id: localID})
	var hits []string
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.Method+" "+r.URL.Path)
		_, _ = io.WriteString(w, `{}`)
	})
	handler, err := newServeHandler(aguiHandler, auth, &fakeAuthulaProvider{})
	if err != nil {
		t.Fatalf("newServeHandler: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/connect/pim/providers", nil)
	addAuthulaSession(req)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || len(hits) != 1 {
		t.Fatalf("member GET providers = %d (hits %v), want it to reach the handler", rec.Code, hits)
	}

	hits = nil
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/connect/pim/providers/google", strings.NewReader(`{"clientId":"c","clientSecret":"s"}`))
	addAuthulaSession(req)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || len(hits) != 0 {
		t.Fatalf("member PUT provider = %d (hits %v), want 403 before the handler", rec.Code, hits)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run TestPIMProviderRoutesSplitMemberAndAdmin ./cmd/aura/`
Expected: FAIL. The GET gets 404, because it is not mounted.

- [ ] **Step 3: Implement**

`cmd/aura/serve_webui_routes.go`, in the `connectPIM*` const block:

```go
	connectPIMProvidersListRoute  = "GET /api/connect/pim/providers"
	connectPIMProviderPutRoute    = "PUT /api/connect/pim/providers/{provider}"
```

`cmd/aura/serve_webui.go`, after the `connectPIMAuthCancelRoute` mount:

```go
	mux.Handle(connectPIMProvidersListRoute, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	// Members connect accounts; only an admin sets the OAuth client they connect with.
	mux.Handle(connectPIMProviderPutRoute, agui.RequireCapability(aguiHandler, auth, identityCreateCapability))
```

`cmd/aura/serve_pim_providers.go`:

```go
package main

import (
	"log/slog"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/pimprovider"
)

func wirePIMProviderApps(server *agui.Server, chat *chatEnv) {
	store, err := pimprovider.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Error("PIM provider-app store unavailable; provider routes and managed account creates answer 503", "err", err)
		return
	}
	server.SetPIMProviderApps(store)
}
```

`cmd/aura/serve_agui.go`, after `wireSettingsProviders(aguiServer, chat)`:

```go
	wirePIMProviderApps(aguiServer, chat)
```

- [ ] **Step 4: Run the tests**

Run: `go vet ./cmd/aura/ && go build ./... && CGO_ENABLED=1 go test -race -count=1 ./cmd/aura/`
Expected: PASS. `container_artifacts_test.go` and the route tests stay green.

- [ ] **Step 5: Commit**

```bash
git add cmd/aura/serve_pim_providers.go cmd/aura/serve_agui.go cmd/aura/serve_webui_routes.go cmd/aura/serve_webui.go cmd/aura/pim_provider_routes_test.go
git commit -m "feat(aura): wire the PIM provider-app store and gate its PUT on identity.create"
```

---

### Task 7: Cockpit data layer

**Files:**
- Modify: `web/src/governance/governanceApi.ts`. Add `putJSON` after `patchJSON` at line 159.
- Modify: `web/src/governance/pimApi.ts`:
  - add the provider-app functions;
  - update the header comment (lines 1-10): managed providers now use the admin-set app;
  - update the `createPimAccount` doc (line 106): it can throw 409 `provider_not_configured`.
- Modify: `web/src/governance/pimProviders.ts`:
  - add `appFields` to `PimProviderDef`;
  - move the Google and Microsoft fields into `appFields`;
  - add `pimIsManaged`;
  - fix the header claim that the sidecar's reads are case-sensitive;
  - add per-provider tenant hints.
- Test: `web/src/governance/__tests__/pimApi.providers.test.ts`, and update
  `web/src/governance/__tests__/pimProviders.test.ts`.

**Interfaces:**
- Produces:
  - `putJSON<T>(url, body?)`
  - `GOV_PIM_PROVIDERS_PATH`, `PIM_PROVIDERS_KEY`, `PIM_PROVIDER_NOT_CONFIGURED`
  - `type PimManagedProviderId`, `interface PimProviderApp`, `interface PimProviderAppInput`
  - `listPimProviderApps(): Promise<readonly PimProviderApp[]>`
  - `savePimProviderApp(provider, input): Promise<PimProviderApp>`
  - `PimProviderDef.appFields`, `pimIsManaged(def): boolean`

- [ ] **Step 1: Write the failing tests**

`web/src/governance/__tests__/pimApi.providers.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest';
import { listPimProviderApps, savePimProviderApp } from '../pimApi';

function respond(body: unknown, status = 200) {
  return vi.fn((_url: string, _init?: RequestInit) =>
    Promise.resolve(new Response(JSON.stringify(body), { status })),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('listPimProviderApps', () => {
  it('reads the member shape with empty admin fields', async () => {
    vi.stubGlobal(
      'fetch',
      respond({
        providers: [
          { provider: 'google', configured: true },
          { provider: 'outlook.com', configured: false },
        ],
      }),
    );
    await expect(listPimProviderApps()).resolves.toEqual([
      { provider: 'google', configured: true, clientId: '', tenantId: '', secretSet: false, redirectUri: '' },
      { provider: 'outlook.com', configured: false, clientId: '', tenantId: '', secretSet: false, redirectUri: '' },
    ]);
  });

  it('drops rows for providers it does not manage', async () => {
    vi.stubGlobal('fetch', respond({ providers: [{ provider: 'imap', configured: true }, null] }));
    await expect(listPimProviderApps()).resolves.toEqual([]);
  });
});

describe('savePimProviderApp', () => {
  it('PUTs the input to the encoded provider path', async () => {
    const fetchMock = respond({ provider: 'outlook.com', configured: true, clientId: 'm', tenantId: 'consumers', secretSet: false });
    vi.stubGlobal('fetch', fetchMock);
    const saved = await savePimProviderApp('outlook.com', { clientId: 'm', tenantId: 'consumers' });
    expect(saved.tenantId).toBe('consumers');
    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(url).toBe('/api/connect/pim/providers/outlook.com');
    expect(init?.method).toBe('PUT');
    expect(JSON.parse(String(init?.body))).toEqual({ clientId: 'm', tenantId: 'consumers' });
  });

  it('surfaces the server reason on a 400', async () => {
    vi.stubGlobal('fetch', respond({ error: 'clientSecret is required for a new Google client' }, 400));
    await expect(savePimProviderApp('google', { clientId: 'x' })).rejects.toThrow(/clientSecret is required/);
  });
});
```

In `pimProviders.test.ts`, add:

```ts
import { PIM_PROVIDERS, pimIsManaged, pimProviderById } from '../pimProviders';

describe('managed providers', () => {
  it('moves the OAuth client to app fields for exactly the three OAuth providers', () => {
    expect(PIM_PROVIDERS.filter(pimIsManaged).map((p) => p.id)).toEqual(['google', 'microsoft365', 'outlook.com']);
    expect(pimProviderById('google').fields).toEqual([]);
    expect(pimProviderById('google').appFields.map((f) => f.key)).toEqual(['clientId', 'clientSecret']);
    expect(pimProviderById('outlook.com').appFields.map((f) => f.key)).toEqual(['tenantId', 'clientId']);
    expect(pimProviderById('imap').appFields).toEqual([]);
  });
});
```

Also update any existing `pimProviders.test.ts` expectation that reads Google or Microsoft keys
from `fields`, so it reads them from `appFields`.

- [ ] **Step 2: Run them to verify they fail**

Run (WSL): `cd web && npx vitest run src/governance/__tests__/pimApi.providers.test.ts src/governance/__tests__/pimProviders.test.ts`
Expected: FAIL (the new functions and fields are not exported).

- [ ] **Step 3: Implement**

`governanceApi.ts`, after `patchJSON`:

```ts
export function putJSON<T>(url: string, body?: unknown): Promise<T> {
  return sendJSON<T>('PUT', url, body);
}
```

`pimApi.ts`: change the import to `import { deleteJSON, postJSON, putJSON, stringValue } from './governanceApi';`
and add:

```ts
export const GOV_PIM_PROVIDERS_PATH = '/api/connect/pim/providers';
export const PIM_PROVIDERS_KEY = ['connect', 'pim', 'providers'] as const;
/** The `error` token Aura's 409 carries when a managed provider has no admin-set app yet. The
 * sidecar's own 409 means a duplicate account id, so the token is what tells the two apart. */
export const PIM_PROVIDER_NOT_CONFIGURED = 'provider_not_configured';

export type PimManagedProviderId = 'google' | 'microsoft365' | 'outlook.com';

export interface PimProviderApp {
  readonly provider: PimManagedProviderId;
  readonly configured: boolean;
  readonly clientId: string;
  readonly tenantId: string;
  readonly secretSet: boolean;
  readonly redirectUri: string;
}

export interface PimProviderAppInput {
  readonly clientId: string;
  readonly tenantId?: string;
  readonly clientSecret?: string;
}

const MANAGED_PROVIDERS: readonly PimManagedProviderId[] = ['google', 'microsoft365', 'outlook.com'];

function pimProviderApp(value: unknown): PimProviderApp | null {
  if (value === null || typeof value !== 'object') return null;
  const raw = value as Record<string, unknown>;
  const provider = MANAGED_PROVIDERS.find((p) => p === raw.provider);
  if (provider === undefined) return null;
  return {
    provider,
    configured: isTrue(raw.configured),
    clientId: stringValue(raw.clientId),
    tenantId: stringValue(raw.tenantId),
    secretSet: isTrue(raw.secretSet),
    redirectUri: stringValue(raw.redirectUri),
  };
}

/** GET /api/connect/pim/providers — which managed providers have an admin-set OAuth client. A
 * member's rows carry only `configured`; the admin fields then read as empty. */
export async function listPimProviderApps(): Promise<readonly PimProviderApp[]> {
  const raw = await getJSON<{ providers?: readonly unknown[] }>(GOV_PIM_PROVIDERS_PATH);
  return (raw.providers ?? []).flatMap((entry): PimProviderApp[] => {
    const app = pimProviderApp(entry);
    return app === null ? [] : [app];
  });
}

/** PUT /api/connect/pim/providers/{provider} — admin only. An empty clientSecret keeps the stored
 * one while the client ID is unchanged; otherwise the server answers 400 with the reason. */
export async function savePimProviderApp(
  provider: PimManagedProviderId,
  input: PimProviderAppInput,
): Promise<PimProviderApp> {
  const raw = await putJSON<unknown>(`${GOV_PIM_PROVIDERS_PATH}/${encodeURIComponent(provider)}`, input);
  const app = pimProviderApp(raw);
  if (app === null) throw new Error('malformed provider app response');
  return app;
}
```

`pimProviders.ts`:

```ts
export interface PimProviderDef {
  readonly id: PimProviderId;
  readonly labelKey: string;
  readonly authFlow: PimAuthFlow;
  /** Per-account fields a member fills in. */
  readonly fields: readonly PimFieldDef[];
  /** The OAuth client an admin sets once for the provider; Aura injects it on create. */
  readonly appFields: readonly PimFieldDef[];
}
```

- google: `fields: []` and `appFields` = the current `clientId` and `clientSecret` entries.
- microsoft365: `fields: []`, and `appFields` = the current tenant + client entries with
  `hintKey: \`${F}.tenantIdHintM365\``.
- outlook.com: `fields: []`, and `appFields` = the current entries, keeping
  `hintKey: \`${F}.tenantIdHint\``.
- imap, ics, json: `appFields: []`.

Then add:

```ts
/** A managed provider's OAuth client is set by an admin, never typed into the account wizard. */
export function pimIsManaged(def: PimProviderDef): boolean {
  return def.appFields.length > 0;
}
```

In the header comment, replace "the provider READS are case-sensitive, so the casing here is
load-bearing — a mismatch validates yet silently fails" with: "the sidecar folds providerConfig
keys case-insensitively, and Aura drops any case variant of an owned key before injecting".

- [ ] **Step 4: Run the tests and the static gates**

Run (WSL): `cd web && npx vitest run src/governance && npx tsc --noEmit && npx oxlint --type-aware --max-warnings=0 . | grep Found && npx prettier --check src/governance`
Expected: PASS and `Found 0 warnings and 0 errors`. Some `CalendarConnect.test.tsx` cases will
fail now, because Google has no fields left; Task 8 rewrites them. Run only the two files above
to be green at this commit.

- [ ] **Step 5: Commit**

```bash
git add web/src/governance/governanceApi.ts web/src/governance/pimApi.ts web/src/governance/pimProviders.ts web/src/governance/__tests__/pimApi.providers.test.ts web/src/governance/__tests__/pimProviders.test.ts
git commit -m "feat(web): read and save the admin-set PIM provider apps"
```

---

### Task 8: Member wizard, member panel, copy

**Files:**
- Modify: `web/src/governance/CalendarConnect.tsx` (the `AddAccountForm`, lines 214-340).
- Modify: `web/src/governance/PimGoogleConnectPanel.tsx`. Drop the redirect block at lines 43-47
  and update the header comment.
- Modify: `web/src/i18n/resources.governance.ts`. Add keys in English (block at line 63) and
  Italian (block at line 293).
- Test: rewrite the Google and Microsoft cases in
  `web/src/governance/__tests__/CalendarConnect.test.tsx`.

**Interfaces:**
- Consumes: Task 7 (`listPimProviderApps`, `PIM_PROVIDERS_KEY`, `PIM_PROVIDER_NOT_CONFIGURED`,
  `pimIsManaged`).

- [ ] **Step 1: Write the failing tests**

In `CalendarConnect.test.tsx`:

- add `const listPimProviderApps = vi.fn();` next to the other mocks, and to the `vi.mock('../pimApi')`
  return object:
  - `listPimProviderApps: (...a: unknown[]) => listPimProviderApps(...a) as Promise<unknown>`;
  - `PIM_PROVIDERS_KEY: actual.PIM_PROVIDERS_KEY`;
  - `PIM_PROVIDER_NOT_CONFIGURED: actual.PIM_PROVIDER_NOT_CONFIGURED`.
- add a module mock for the admin hook before the dynamic import:

```ts
let caps = { isAdmin: false };
vi.mock('../../admin/useAdmin', () => ({ useCapabilities: () => caps }));
```

- in `beforeEach`, set `caps = { isAdmin: false };` and
  `listPimProviderApps.mockResolvedValue([{ provider: 'google', configured: true, clientId: '', tenantId: '', secretSet: false, redirectUri: '' }, { provider: 'microsoft365', configured: true, clientId: '', tenantId: '', secretSet: false, redirectUri: '' }, { provider: 'outlook.com', configured: false, clientId: '', tenantId: '', secretSet: false, redirectUri: '' }]);`.
- replace every step that typed Client ID, Client secret or Tenant ID for Google or Microsoft:
  those fields no longer exist. The create body now carries `providerConfig: {}`.
- add:

```tsx
it('shows no credential fields for a managed provider', async () => {
  renderConnect();
  await screen.findByText(/add account/i);
  expect(screen.queryByLabelText(/client id/i)).toBeNull();
  expect(screen.queryByLabelText(/client secret/i)).toBeNull();
});

it('blocks an unconfigured managed provider with a note', async () => {
  renderConnect();
  fireEvent.change(await screen.findByLabelText('Provider'), { target: { value: 'outlook.com' } });
  expect(await screen.findByText(/administrator has to configure outlook\.com/i)).toBeTruthy();
  expect(screen.getByRole('button', { name: /create account/i })).toHaveProperty('disabled', true);
});

it('translates the provider_not_configured 409', async () => {
  createPimAccount.mockRejectedValueOnce(new HttpError(409, 'provider_not_configured'));
  renderConnect();
  fireEvent.change(await screen.findByLabelText(/account id/i), { target: { value: 'work' } });
  fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'Work' } });
  fireEvent.click(screen.getByRole('button', { name: /create account/i }));
  expect(await screen.findByRole('alert')).toHaveTextContent(/administrator has to configure google/i);
});

it('does not show members a redirect URI to register', async () => {
  createPimAccount.mockResolvedValueOnce({ id: 'work', displayName: 'Work', provider: 'google', enabled: true });
  pimGoogleStart.mockResolvedValueOnce({ authUrl: 'https://accounts.google.com/o/oauth2/auth?x', redirectUri: 'https://chetto1983.github.io/aura-connect/google/callback/' });
  renderConnect();
  fireEvent.change(await screen.findByLabelText(/account id/i), { target: { value: 'work' } });
  fireEvent.change(screen.getByLabelText(/display name/i), { target: { value: 'Work' } });
  fireEvent.click(screen.getByRole('button', { name: /create account/i }));
  expect(await screen.findByRole('link', { name: /connect google/i })).toBeTruthy();
  expect(screen.queryByText(/aura-connect/)).toBeNull();
});
```

The note names the provider by its i18n label (`Google`, `Outlook.com`, from
`resources.governance.ts:73-80`), which the case-insensitive regexes above match. The label
queries are the ones this file already uses: `'Provider'`, `/Account ID/i`, `/Display name/i`.

- [ ] **Step 2: Run to verify they fail**

Run (WSL): `cd web && npx vitest run src/governance/__tests__/CalendarConnect.test.tsx`
Expected: FAIL (no note, the submit is not disabled, the 409 shows the raw token, the redirect
URI is still rendered).

- [ ] **Step 3: Implement**

`CalendarConnect.tsx`:

- import `listPimProviderApps`, `PIM_PROVIDERS_KEY`, `PIM_PROVIDER_NOT_CONFIGURED` from
  `./pimApi`, and `pimIsManaged` from `./pimProviders`;
- in `AddAccountForm`, after `const def = …`:

```tsx
  const apps = useQuery({ queryKey: PIM_PROVIDERS_KEY, queryFn: listPimProviderApps, retry: false });
  const managed = pimIsManaged(def);
  const appConfigured =
    !managed || apps.data?.some((a) => a.provider === providerId && a.configured) === true;
  const providerLabel = t(def.labelKey);
```

- extend `invalidForm` with `|| !appConfigured`;
- render the note just above `<AdvancedSection`:

```tsx
      {managed && apps.isSuccess && !appConfigured ? (
        <p role="note" className="text-[13px] text-warning">
          {t('governance.mcp.calendar.providerNotConfigured', { provider: providerLabel })}
        </p>
      ) : null}
```

- make the submit `disabled={create.isPending || !appConfigured}`;
- replace the create error text:

```tsx
          {createErrorText(create.error) ?? t('governance.error')}
```

with this helper inside the component, replacing the `serverReason(create.error)` use:

```tsx
  function createErrorText(error: unknown): string | null {
    if (error instanceof HttpError && error.reason === PIM_PROVIDER_NOT_CONFIGURED) {
      return t('governance.mcp.calendar.providerNotConfigured', { provider: providerLabel });
    }
    return serverReason(error);
  }
```

`PimGoogleConnectPanel.tsx`: delete the `redirectHeading`, `redirectUri` and `redirectHint`
paragraphs. Rewrite the header comment's first sentence to: "renders the Google consent step
after the wizard creates a Google account: the consent link, then a confirmation once the
sidecar stores the token. Registering the redirect URI is the admin's job
(PimProviderAppsPanel)."

`resources.governance.ts`, under `calendar:`:
- English: `providerNotConfigured: 'An administrator has to configure {{provider}} first.',`
- Italian: `providerNotConfigured: 'Un amministratore deve prima configurare {{provider}}.',`

- [ ] **Step 4: Run the tests and gates**

Run (WSL): `cd web && npx vitest run src/governance && npx tsc --noEmit && npx oxlint --type-aware --max-warnings=0 . | grep Found && npx prettier --check .`
Expected: PASS, and `Found 0 warnings and 0 errors`.

- [ ] **Step 5: Commit**

```bash
git add web/src/governance/CalendarConnect.tsx web/src/governance/PimGoogleConnectPanel.tsx web/src/i18n/resources.governance.ts web/src/governance/__tests__/CalendarConnect.test.tsx
git commit -m "feat(web): members connect PIM accounts without typing an OAuth client"
```

---

### Task 9: Admin panel and operator docs

**Files:**
- Create: `web/src/governance/PimProviderAppsPanel.tsx`
- Modify: `web/src/governance/CalendarConnect.tsx`. Render the panel for admins above
  `<AddAccountForm`.
- Modify: `web/src/i18n/resources.governance.ts`. Add the `apps.*` keys and `tenantIdHintM365`
  in English and Italian, and reword `tenantIdHint`.
- Modify: `docs/mcp-manager.md`, the "Google" section (lines 219-242) and the Microsoft bullets
  (lines 214-217).
- Test: `web/src/governance/__tests__/PimProviderAppsPanel.test.tsx`

**Interfaces:**
- Consumes: Task 7 (`listPimProviderApps`, `savePimProviderApp`, `PIM_PROVIDERS_KEY`,
  `pimIsManaged`, `PIM_PROVIDERS`), and `Field` from `./CalendarConnectFields`.

- [ ] **Step 1: Write the failing tests**

`web/src/governance/__tests__/PimProviderAppsPanel.test.tsx`:

```tsx
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '../../i18n/i18n';
import { HttpError } from '../../api/json';

const listPimProviderApps = vi.fn();
const savePimProviderApp = vi.fn();

vi.mock('../pimApi', async () => {
  const actual = await vi.importActual<typeof import('../pimApi')>('../pimApi');
  return {
    PIM_PROVIDERS_KEY: actual.PIM_PROVIDERS_KEY,
    listPimProviderApps: (...a: unknown[]) => listPimProviderApps(...a) as Promise<unknown>,
    savePimProviderApp: (...a: unknown[]) => savePimProviderApp(...a) as Promise<unknown>,
  };
});

const { PimProviderAppsPanel } = await import('../PimProviderAppsPanel');

const RELAY = 'https://chetto1983.github.io/aura-connect/google/callback/';

function renderPanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  render(
    <QueryClientProvider client={qc}>
      <PimProviderAppsPanel />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  listPimProviderApps.mockReset();
  savePimProviderApp.mockReset();
  listPimProviderApps.mockResolvedValue([
    { provider: 'google', configured: true, clientId: 'g-cid', tenantId: '', secretSet: true, redirectUri: RELAY },
    { provider: 'microsoft365', configured: false, clientId: '', tenantId: '', secretSet: false, redirectUri: '' },
    { provider: 'outlook.com', configured: false, clientId: '', tenantId: '', secretSet: false, redirectUri: '' },
  ]);
});

describe('PimProviderAppsPanel', () => {
  it('pre-fills the client ID, never the secret, and shows the relay URI under Google', async () => {
    renderPanel();
    const clientIds = await screen.findAllByLabelText(/client id/i);
    expect((clientIds[0] as HTMLInputElement).value).toBe('g-cid');
    expect((screen.getByLabelText(/client secret/i) as HTMLInputElement).value).toBe('');
    expect(screen.getByText(RELAY)).toBeTruthy();
    expect(screen.getByText(/secret is stored/i)).toBeTruthy();
  });

  it('saves Google without a secret when the client ID is unchanged', async () => {
    savePimProviderApp.mockResolvedValueOnce({ provider: 'google', configured: true, clientId: 'g-cid', tenantId: '', secretSet: true, redirectUri: RELAY });
    renderPanel();
    await screen.findAllByLabelText(/client id/i);
    fireEvent.click(screen.getAllByRole('button', { name: /save/i })[0] as HTMLElement);
    await waitFor(() => {
      expect(savePimProviderApp).toHaveBeenCalledWith('google', { clientId: 'g-cid' });
    });
  });

  it('sends the tenant for Outlook.com and shows the server reason on a 400', async () => {
    savePimProviderApp.mockRejectedValueOnce(new HttpError(400, 'tenantId is required for outlook.com'));
    renderPanel();
    const clientIds = await screen.findAllByLabelText(/client id/i);
    fireEvent.change(clientIds[2] as HTMLElement, { target: { value: 'ms-cid' } });
    fireEvent.click(screen.getAllByRole('button', { name: /save/i })[2] as HTMLElement);
    await waitFor(() => {
      expect(savePimProviderApp).toHaveBeenCalledWith('outlook.com', { clientId: 'ms-cid', tenantId: '' });
    });
    expect(await screen.findByRole('alert')).toHaveTextContent(/tenantId is required/);
  });
});
```

In `CalendarConnect.test.tsx`, add:

```tsx
it('shows the provider-app panel only to admins', async () => {
  renderConnect();
  await screen.findByText(/add account/i);
  expect(screen.queryByText(/provider oauth apps/i)).toBeNull();
});

it('shows the provider-app panel to an admin', async () => {
  caps = { isAdmin: true };
  renderConnect();
  expect(await screen.findByText(/provider oauth apps/i)).toBeTruthy();
});
```

- [ ] **Step 2: Run to verify they fail**

Run (WSL): `cd web && npx vitest run src/governance/__tests__/PimProviderAppsPanel.test.tsx src/governance/__tests__/CalendarConnect.test.tsx`
Expected: FAIL (the module is missing, and no panel is rendered for admins).

- [ ] **Step 3: Implement**

`web/src/governance/PimProviderAppsPanel.tsx`:

```tsx
import { useId, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { HttpError } from '../api/json';
import { Spinner } from '../components/Spinner';
import { Field } from './CalendarConnectFields';
import {
  listPimProviderApps,
  PIM_PROVIDERS_KEY,
  savePimProviderApp,
  type PimManagedProviderId,
  type PimProviderApp,
} from './pimApi';
import { PIM_PROVIDERS, pimIsManaged, type PimProviderDef } from './pimProviders';
import { Button } from '@/components/ui/button';

// PimProviderAppsPanel is the admin's half of calendar connect: one OAuth client per managed
// provider, set once, which Aura injects into every member's account create. The secret field is
// never pre-filled — the server never returns it — and an empty one keeps the stored secret.

export function PimProviderAppsPanel() {
  const { t } = useTranslation();
  const apps = useQuery({ queryKey: PIM_PROVIDERS_KEY, queryFn: listPimProviderApps, retry: false });
  if (!apps.isSuccess) return null;
  return (
    <section className="flex flex-col gap-3 rounded-md border border-border bg-surface px-3 py-3">
      <p className="text-[13px] font-semibold text-text-muted">
        {t('governance.mcp.calendar.apps.heading')}
      </p>
      <p className="text-[13px] text-text-muted">{t('governance.mcp.calendar.apps.intro')}</p>
      {PIM_PROVIDERS.filter(pimIsManaged).map((def) => (
        <ProviderAppForm
          key={def.id}
          def={def}
          app={apps.data.find((a) => a.provider === def.id)}
        />
      ))}
    </section>
  );
}

function ProviderAppForm({
  def,
  app,
}: {
  readonly def: PimProviderDef;
  readonly app: PimProviderApp | undefined;
}) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const idPrefix = useId();
  const [values, setValues] = useState<Record<string, string>>(() => ({
    clientId: app?.clientId ?? '',
    tenantId: app?.tenantId ?? '',
    clientSecret: '',
  }));

  const save = useMutation({
    mutationFn: () => {
      const input: { clientId: string; tenantId?: string; clientSecret?: string } = {
        clientId: (values.clientId ?? '').trim(),
      };
      if (def.appFields.some((f) => f.key === 'tenantId')) input.tenantId = (values.tenantId ?? '').trim();
      const secret = (values.clientSecret ?? '').trim();
      if (secret !== '') input.clientSecret = secret;
      return savePimProviderApp(def.id as PimManagedProviderId, input);
    },
    onSuccess: () => {
      setValues((prev) => ({ ...prev, clientSecret: '' }));
      void queryClient.invalidateQueries({ queryKey: PIM_PROVIDERS_KEY });
    },
  });

  return (
    <form
      className="flex flex-col gap-2 rounded-md border border-border-strong bg-surface-2 px-3 py-3"
      onSubmit={(event) => {
        event.preventDefault();
        save.mutate();
      }}
    >
      <p className="text-[13px] font-semibold text-text">
        {t(def.labelKey)} ·{' '}
        {app?.configured
          ? t('governance.mcp.calendar.apps.configured')
          : t('governance.mcp.calendar.apps.notConfigured')}
      </p>
      {def.appFields.map((field) => (
        <Field
          key={field.key}
          id={`${idPrefix}-${field.key}`}
          label={t(field.labelKey)}
          type={field.type === 'password' ? 'password' : 'text'}
          value={values[field.key] ?? ''}
          onChange={(v) => {
            setValues((prev) => ({ ...prev, [field.key]: v }));
          }}
          invalid={false}
          hint={
            field.key === 'clientSecret' && app?.secretSet
              ? t('governance.mcp.calendar.apps.secretStored')
              : field.hintKey !== undefined
                ? t(field.hintKey)
                : undefined
          }
        />
      ))}
      {app?.redirectUri ? (
        <>
          <p className="text-[13px] font-semibold text-text">
            {t('governance.mcp.calendar.redirectHeading')}
          </p>
          <p className="break-all font-mono text-[13px] text-text">{app.redirectUri}</p>
          <p className="text-[13px] text-text-muted">{t('governance.mcp.calendar.redirectHint')}</p>
        </>
      ) : null}
      <Button
        type="submit"
        disabled={save.isPending}
        aria-busy={save.isPending}
        className="self-start text-[13px]"
      >
        {save.isPending ? <Spinner /> : null}
        {t('governance.mcp.calendar.apps.save')}
      </Button>
      {save.isError ? (
        <p role="alert" className="text-[13px] text-danger">
          {save.error instanceof HttpError && save.error.reason !== ''
            ? save.error.reason
            : t('governance.error')}
        </p>
      ) : null}
      {save.isSuccess ? (
        <p role="status" className="text-[13px] text-success">
          {t('governance.mcp.calendar.apps.saved')}
        </p>
      ) : null}
    </form>
  );
}
```

`CalendarConnect.tsx`:
- `import { useCapabilities } from '../admin/useAdmin';`
- `import { PimProviderAppsPanel } from './PimProviderAppsPanel';`
- in `CalendarConnect()`: `const { isAdmin } = useCapabilities();`
- before `<AddAccountForm onCreated={refresh} />`: `{isAdmin ? <PimProviderAppsPanel /> : null}`.
- run oxlint and let it place the imports.

`resources.governance.ts`, under `calendar:`, in English:

```ts
        apps: {
          heading: 'Provider OAuth apps',
          intro: 'Set once per provider. Members then connect their accounts without typing any credential.',
          configured: 'configured',
          notConfigured: 'not configured',
          secretStored: 'A secret is stored. Leave empty to keep it; a new client ID needs its own secret.',
          save: 'Save',
          saved: 'Saved.',
        },
```

and in Italian:

```ts
        apps: {
          heading: 'App OAuth dei provider',
          intro: 'Si impostano una volta per provider. Gli utenti poi collegano i propri account senza inserire credenziali.',
          configured: 'configurato',
          notConfigured: 'non configurato',
          secretStored: 'Un secret è salvato. Lascia vuoto per mantenerlo; un nuovo client ID richiede il suo secret.',
          save: 'Salva',
          saved: 'Salvato.',
        },
```

Under `fields:`:
- English:
  - `tenantIdHint: 'Personal accounts: "consumers" when the app accepts only personal accounts, "common" when it also accepts work accounts.',`
  - `tenantIdHintM365: 'The directory (tenant) ID of your organization, or "organizations" for any work account.',`
- Italian:
  - `tenantIdHint: 'Account personali: "consumers" se l\'app accetta solo account personali, "common" se accetta anche quelli di lavoro.',`
  - `tenantIdHintM365: 'Il Directory (tenant) ID della tua organizzazione, oppure "organizations" per qualsiasi account di lavoro.',`

`docs/mcp-manager.md`:
- **Microsoft section:** add that an admin enters the tenant and client ID once, in the cockpit's
  "Provider OAuth apps" panel. The Entra app needs the delegated Graph permissions `Mail.Read`,
  `Mail.ReadWrite`, `Mail.Send`, `Calendars.ReadWrite` and `Contacts.ReadWrite`, plus "Allow
  public client flows: Yes".
- **Google step 1:** the admin enters the Web client's ID and secret once in "Provider OAuth
  apps", where the relay URI to register is shown.
- **New step between 1 and 2:** a member adds a Google account with only an account ID and a
  display name, and Aura injects the admin-set client.

- [ ] **Step 4: Run the tests and all web gates**

Run (WSL): `cd web && npx vitest run src/governance && npx tsc --noEmit && npx oxlint --type-aware --max-warnings=0 . | grep Found && npx prettier --check . && node scripts/lint-contract.mjs`
Expected: PASS, `Found 0 warnings and 0 errors`, and the formatting and contract checks pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/governance/PimProviderAppsPanel.tsx web/src/governance/__tests__/PimProviderAppsPanel.test.tsx web/src/governance/CalendarConnect.tsx web/src/governance/__tests__/CalendarConnect.test.tsx web/src/i18n/resources.governance.ts docs/mcp-manager.md
git commit -m "feat(web): let an admin set each PIM provider's OAuth client once"
```

---

### Task 10: Ship and prove it on the VM

**Files:**
- Modify: `prd.md` §13, after the paragraph that ends "…Cloudflare tunnel." (added 2026-09-23).
- Modify: `C:\Users\Davide\.claude\projects\d--Aura\memory\project_calendar_mcp_fork_aura_pim_mcp.md`,
  one line on the admin-set provider apps.

- [ ] **Step 1: Full local gates (WSL, stack up)**

Run: `go vet ./... && go build ./... && CGO_ENABLED=1 go test -race -count=1 ./internal/agui/ ./internal/pimprovider/ ./cmd/aura/ ./internal/db/`
Then run `bash scripts/coverage_docker.sh`: the aggregate must be ≥85% and
`internal/pimprovider` ≥85%.
Expected: all green. If `coverage_docker.sh` wipes the local identity, re-seed it per memory
`reference_local_identity_seed_reseed_integration.md`.

- [ ] **Step 2: Push**

Run (WSL):
`LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks push origin master`
Expected: the lefthook banner, and all pre-push commands ✔️. If the web hooks die with "Cannot
find native binding", add the Linux twins as in memory
`reference_wsl_push_silently_skips_lefthook.md`. Never skip a hook.

- [ ] **Step 3: CI**

Run: `gh run list --limit 8` and `gh run watch <id> --exit-status` for CI, Skills and "Publish
Aura edge image" on the pushed SHA.
Expected: all green. A cancelled run means a newer push superseded it; watch the newer one.

- [ ] **Step 4: VM E2E (192.168.101.158)**

Wait for `aura-image-update` to pull the new edge image (up to about 6 minutes after publish).
Confirm the revision with `docker inspect aura --format '{{index .Config.Labels "org.opencontainers.image.revision"}}'`.
Then, with the operator in the cockpit:
1. As the admin, "Provider OAuth apps" → Google: enter the Web client ID and secret and save.
   The relay URI is shown. Do the same for Outlook.com with the tenant and client ID.
2. As a member identity (create one through onboarding if none exists), Add account → Google
   shows no credential fields. Enter an account ID and a display name, then Create. Complete the
   consent: the panel turns to "linked".
3. Ask the agent in chat "Quali calendari ho?". It lists the new account's calendars.
4. As the admin, change the Google client ID without a secret: the reason is shown (400). Save
   it back.
5. Check that `personale` and `lavoro`, linked before the change, still list calendars. They
   kept their own copy.

- [ ] **Step 5: PRD and memory**

Add to prd.md §13 what Step 4 measured:
- the table;
- the `identity.create` gate;
- the injection on create, with the canonical-provider rule;
- the keep-secret UPDATE;
- that already-linked accounts kept working.

Name what stayed unmeasured: rotating a secret of the same Google client. Commit as
`docs(prd): record the measured admin-set PIM provider apps` and push as in Step 2.
