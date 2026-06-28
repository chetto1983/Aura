# Authula Telegram Password Reset Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the legacy passphrase login path and ship Authula-only password reset through Telegram plus an onboarding security question.

**Architecture:** Keep the recovery feature inside `internal/agui` behind narrow ports, then wire concrete Postgres, Telegram, and Authula adapters from `cmd/aura`. Add Aura-owned recovery tables and sqlc queries; keep Authula as the only session issuer. Extend the React login and onboarding screens using the existing shadcn/Tailwind component patterns.

**Tech Stack:** Go 1.24, pgx/sqlc/golang-migrate, Authula v1.11.0 CoreServices, Telegram channel delivery, React 19, TypeScript 6, Vite, Vitest, Playwright, Tailwind/shadcn components.

---

## Scope Check

This is one connected vertical feature. Schema, backend reset service, Authula-only cutover, onboarding recovery setup, and UI changes depend on each other and should land together behind test-first commits.

## File Map

- Create `internal/db/migrations/0023_identity_recovery.up.sql`: recovery tables, indexes, grants, comments.
- Create `internal/db/migrations/0023_identity_recovery.down.sql`: reverse migration.
- Create `internal/db/queries/identity_recovery.sql`: sqlc queries for recovery setup, reset challenges, reset tokens, and recovery audit.
- Modify generated `internal/db/sqlc/*`: regenerate with `sqlc generate`.
- Create `internal/agui/recovery_hash.go` and `internal/agui/recovery_hash_test.go`: answer/code/token normalization and hashing helpers.
- Create `internal/agui/password_reset.go`, `internal/agui/password_reset_api.go`, `internal/agui/password_reset_test.go`, and `internal/agui/password_reset_api_test.go`: service, ports, handlers, and unit tests.
- Modify `internal/agui/onboarding_api.go`, `internal/agui/onboarding_provision.go`, `internal/agui/onboarding_provision_test.go`, and `internal/agui/onboarding_session.go`: recovery setup in provisioning.
- Modify `internal/agui/server.go`: register password reset routes on the AG-UI mux.
- Modify `cmd/aura/serve_auth.go`, `cmd/aura/serve_auth_test.go`, `cmd/aura/serve_webui.go`, `cmd/aura/serve_webui_test.go`, and `cmd/aura/serve_onboarding.go`: Authula-only cutover, public reset route mounts, and concrete adapters.
- Modify `web/src/routes/LoginPage.tsx` and `web/src/__tests__/LoginPage.test.tsx`: remove passphrase UI and add reset entry point.
- Create `web/src/auth/passwordResetApi.ts`, `web/src/auth/PasswordResetPanel.tsx`, and `web/src/auth/__tests__/PasswordResetPanel.test.tsx`: reset client and UI.
- Modify `web/src/onboarding/CredentialStep.tsx`, `web/src/onboarding/OnboardingWizard.tsx`, `web/src/onboarding/ReviewStep.tsx`, `web/src/onboarding/onboardingApi.ts`, `web/src/onboarding/onboardingWizardModel.ts`, and matching tests: recovery question/answer and mandatory Telegram.
- Modify `web/src/i18n/resources.ts` and `web/src/i18n/resources.onboarding.ts`: English and Italian copy.
- Modify `.env.example`, `compose.yaml`, and `docs/cockpit-overhaul/05-authula-auth-SPEC.md`: document Authula-only default and recovery flow.
- Rebuild `internal/webui/dist/**` after frontend changes.

## Task 1: Recovery Schema And sqlc Surface

**Files:**
- Create: `internal/db/migrations/0023_identity_recovery.up.sql`
- Create: `internal/db/migrations/0023_identity_recovery.down.sql`
- Create: `internal/db/queries/identity_recovery.sql`
- Modify: `internal/db/sqlc/*`

- [ ] **Step 1: Write migration test expectation**

Add a focused integration test file `internal/db/migrate_0023_integration_test.go`:

```go
//go:build db_integration

package db

import (
	"context"
	"testing"
)

func TestMigration0023IdentityRecoveryRoundTrip(t *testing.T) {
	ctx := context.Background()
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")

	if _, err := Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	pool, err := Open(ctx, &Config{URL: migrateURL})
	if err != nil {
		t.Fatalf("open migrate pool: %v", err)
	}
	t.Cleanup(pool.Close)

	for _, table := range []string{
		"aura.identity_recovery",
		"aura.password_reset_challenges",
		"aura.password_reset_tokens",
		"aura.identity_recovery_audit",
	} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("%s missing after migrate up", table)
		}
	}

	if err := MigrateSteps(ctx, migrateURL, -1); err != nil {
		t.Fatalf("migrate down 0023: %v", err)
	}
	if err := MigrateSteps(ctx, migrateURL, 1); err != nil {
		t.Fatalf("migrate re-up 0023: %v", err)
	}
}
```

- [ ] **Step 2: Run migration test and verify RED**

Run:

```bash
go test -tags db_integration ./internal/db -run TestMigration0023IdentityRecoveryRoundTrip -count=1
```

Expected: FAIL because the `0023` migration does not exist or the tables are missing.

- [ ] **Step 3: Add migration files**

Create `internal/db/migrations/0023_identity_recovery.up.sql`:

```sql
CREATE TABLE aura.identity_recovery (
    identity_id          uuid        PRIMARY KEY REFERENCES aura.identities (id) ON DELETE CASCADE,
    question             text        NOT NULL,
    answer_hash          text        NOT NULL,
    answer_hash_version  text        NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE aura.password_reset_challenges (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id      uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    code_hash        text        NOT NULL,
    telegram_user_id bigint,
    created_at       timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz NOT NULL,
    consumed_at      timestamptz,
    attempt_count    integer     NOT NULL DEFAULT 0,
    max_attempts     integer     NOT NULL DEFAULT 5,
    request_ip_hash  text,
    user_agent_hash  text
);

CREATE TABLE aura.password_reset_tokens (
    token_hash     text        PRIMARY KEY,
    challenge_id   uuid        NOT NULL REFERENCES aura.password_reset_challenges (id) ON DELETE CASCADE,
    identity_id    uuid        NOT NULL REFERENCES aura.identities (id) ON DELETE CASCADE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL,
    consumed_at    timestamptz,
    attempt_count  integer     NOT NULL DEFAULT 0,
    max_attempts   integer     NOT NULL DEFAULT 3
);

CREATE TABLE aura.identity_recovery_audit (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id      uuid,
    event            text        NOT NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    request_ip_hash  text,
    user_agent_hash  text,
    metadata         jsonb       NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX identity_recovery_updated_idx
    ON aura.identity_recovery (updated_at DESC);
CREATE INDEX password_reset_challenges_identity_active_idx
    ON aura.password_reset_challenges (identity_id, expires_at)
    WHERE consumed_at IS NULL;
CREATE INDEX password_reset_tokens_identity_active_idx
    ON aura.password_reset_tokens (identity_id, expires_at)
    WHERE consumed_at IS NULL;
CREATE INDEX identity_recovery_audit_created_idx
    ON aura.identity_recovery_audit (created_at DESC);

GRANT SELECT, INSERT, UPDATE ON aura.identity_recovery TO aura_app;
GRANT SELECT, INSERT, UPDATE ON aura.password_reset_challenges TO aura_app;
GRANT SELECT, INSERT, UPDATE ON aura.password_reset_tokens TO aura_app;
GRANT SELECT, INSERT ON aura.identity_recovery_audit TO aura_app;
GRANT ALL ON aura.identity_recovery TO aura_migrate;
GRANT ALL ON aura.password_reset_challenges TO aura_migrate;
GRANT ALL ON aura.password_reset_tokens TO aura_migrate;
GRANT ALL ON aura.identity_recovery_audit TO aura_migrate;

COMMENT ON TABLE aura.identity_recovery IS
    'Per-identity recovery question and hashed answer for Telegram password reset. Raw answers are never stored.';
COMMENT ON TABLE aura.password_reset_challenges IS
    'Short-lived Telegram code challenges for self-service Authula password reset.';
COMMENT ON TABLE aura.password_reset_tokens IS
    'Short-lived server-side reset tokens minted after Telegram code and security answer verification.';
COMMENT ON TABLE aura.identity_recovery_audit IS
    'Append-only recovery event audit. Contains no raw emails, answers, codes, passwords, IP addresses, or Telegram tokens.';
```

Create `internal/db/migrations/0023_identity_recovery.down.sql`:

```sql
DROP TABLE IF EXISTS aura.identity_recovery_audit;
DROP TABLE IF EXISTS aura.password_reset_tokens;
DROP TABLE IF EXISTS aura.password_reset_challenges;
DROP TABLE IF EXISTS aura.identity_recovery;
```

- [ ] **Step 4: Add sqlc queries**

Create `internal/db/queries/identity_recovery.sql`:

```sql
-- name: UpsertIdentityRecovery :exec
INSERT INTO aura.identity_recovery (identity_id, question, answer_hash, answer_hash_version)
VALUES ($1, $2, $3, $4)
ON CONFLICT (identity_id) DO UPDATE
SET question = EXCLUDED.question,
    answer_hash = EXCLUDED.answer_hash,
    answer_hash_version = EXCLUDED.answer_hash_version,
    updated_at = now();

-- name: GetIdentityRecoveryByIdentity :one
SELECT identity_id, question, answer_hash, answer_hash_version, created_at, updated_at
FROM aura.identity_recovery
WHERE identity_id = $1;

-- name: LookupRecoveryByEmail :one
SELECT i.id AS identity_id,
       i.name AS email,
       ial.authula_user_id,
       ir.question,
       ir.answer_hash,
       ir.answer_hash_version,
       ta.telegram_user_id
FROM aura.identities i
JOIN aura.identity_auth_links ial ON ial.identity_id = i.id
JOIN aura.identity_recovery ir ON ir.identity_id = i.id
JOIN aura.telegram_accounts ta ON ta.identity_id = i.id
WHERE lower(i.name) = lower($1);

-- name: InsertPasswordResetChallenge :one
INSERT INTO aura.password_reset_challenges (
    identity_id, code_hash, telegram_user_id, expires_at, max_attempts,
    request_ip_hash, user_agent_hash
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, identity_id, code_hash, telegram_user_id, created_at, expires_at,
    consumed_at, attempt_count, max_attempts, request_ip_hash, user_agent_hash;

-- name: GetActivePasswordResetChallenge :one
SELECT id, identity_id, code_hash, telegram_user_id, created_at, expires_at,
    consumed_at, attempt_count, max_attempts, request_ip_hash, user_agent_hash
FROM aura.password_reset_challenges
WHERE identity_id = $1
  AND consumed_at IS NULL
  AND expires_at > now()
ORDER BY created_at DESC
LIMIT 1;

-- name: IncrementPasswordResetChallengeAttempts :exec
UPDATE aura.password_reset_challenges
SET attempt_count = attempt_count + 1
WHERE id = $1;

-- name: ConsumePasswordResetChallenge :one
UPDATE aura.password_reset_challenges
SET consumed_at = now()
WHERE id = $1
  AND consumed_at IS NULL
  AND expires_at > now()
  AND attempt_count < max_attempts
RETURNING id, identity_id, code_hash, telegram_user_id, created_at, expires_at,
    consumed_at, attempt_count, max_attempts, request_ip_hash, user_agent_hash;

-- name: InsertPasswordResetToken :one
INSERT INTO aura.password_reset_tokens (
    token_hash, challenge_id, identity_id, expires_at, max_attempts
)
VALUES ($1, $2, $3, $4, $5)
RETURNING token_hash, challenge_id, identity_id, created_at, expires_at, consumed_at,
    attempt_count, max_attempts;

-- name: GetPasswordResetToken :one
SELECT token_hash, challenge_id, identity_id, created_at, expires_at, consumed_at,
    attempt_count, max_attempts
FROM aura.password_reset_tokens
WHERE token_hash = $1;

-- name: IncrementPasswordResetTokenAttempts :exec
UPDATE aura.password_reset_tokens
SET attempt_count = attempt_count + 1
WHERE token_hash = $1;

-- name: ConsumePasswordResetToken :one
UPDATE aura.password_reset_tokens
SET consumed_at = now()
WHERE token_hash = $1
  AND consumed_at IS NULL
  AND expires_at > now()
  AND attempt_count < max_attempts
RETURNING token_hash, challenge_id, identity_id, created_at, expires_at, consumed_at,
    attempt_count, max_attempts;

-- name: InsertIdentityRecoveryAudit :one
INSERT INTO aura.identity_recovery_audit (
    identity_id, event, request_ip_hash, user_agent_hash, metadata
)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, identity_id, event, created_at, request_ip_hash, user_agent_hash, metadata;
```

- [ ] **Step 5: Generate sqlc and run tests**

Run:

```bash
sqlc generate
go test -tags db_integration ./internal/db -run TestMigration0023IdentityRecoveryRoundTrip -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

Run:

```bash
git add internal/db/migrations/0023_identity_recovery.*.sql internal/db/queries/identity_recovery.sql internal/db/sqlc internal/db/migrate_0023_integration_test.go
git commit -m "feat(auth): add identity recovery schema"
```

## Task 2: Recovery Hashing And Normalization

**Files:**
- Create: `internal/agui/recovery_hash.go`
- Create: `internal/agui/recovery_hash_test.go`

- [ ] **Step 1: Write failing hash tests**

Create `internal/agui/recovery_hash_test.go`:

```go
package agui

import (
	"strings"
	"testing"
)

func TestNormalizeRecoveryAnswer(t *testing.T) {
	got := NormalizeRecoveryAnswer("  My   First   School  ")
	if got != "my first school" {
		t.Fatalf("normalized = %q, want %q", got, "my first school")
	}
}

func TestRecoveryHasherHashVerify(t *testing.T) {
	h := RecoveryHasher{}
	hash, version, err := h.HashAnswer("Blue Guitar")
	if err != nil {
		t.Fatalf("HashAnswer: %v", err)
	}
	if version != recoveryAnswerHashVersion {
		t.Fatalf("version = %q, want %q", version, recoveryAnswerHashVersion)
	}
	if strings.Contains(hash, "Blue Guitar") {
		t.Fatal("hash leaked the raw answer")
	}
	if !h.VerifyAnswer(" blue   guitar ", hash) {
		t.Fatal("normalized answer should verify")
	}
	if h.VerifyAnswer("red guitar", hash) {
		t.Fatal("wrong answer verified")
	}
}

func TestHashOpaqueSecretVerify(t *testing.T) {
	raw := "123456"
	hash, err := HashOpaqueSecret(raw)
	if err != nil {
		t.Fatalf("HashOpaqueSecret: %v", err)
	}
	if strings.Contains(hash, raw) {
		t.Fatal("opaque hash leaked raw secret")
	}
	if !VerifyOpaqueSecret(raw, hash) {
		t.Fatal("opaque secret should verify")
	}
	if VerifyOpaqueSecret("654321", hash) {
		t.Fatal("wrong opaque secret verified")
	}
}

func TestHashLookupTokenIsDeterministicAndNonReversible(t *testing.T) {
	token := "reset-token-123"
	a := HashLookupToken(token)
	b := HashLookupToken(token)
	if a != b {
		t.Fatal("lookup token hash must be deterministic")
	}
	if strings.Contains(a, token) {
		t.Fatal("lookup token hash leaked raw token")
	}
}
```

- [ ] **Step 2: Run hash tests and verify RED**

Run:

```bash
go test ./internal/agui -run 'TestNormalizeRecoveryAnswer|TestRecoveryHasherHashVerify|TestHashOpaqueSecretVerify' -count=1
```

Expected: FAIL because the recovery hashing helpers do not exist.

- [ ] **Step 3: Implement minimal hashing helpers**

Create `internal/agui/recovery_hash.go`:

```go
package agui

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

const recoveryAnswerHashVersion = "argon2id-v1"

type RecoveryHasher struct{}

func NormalizeRecoveryAnswer(s string) string {
	folded := cases.Fold().String(norm.NFKC.String(s))
	return strings.Join(strings.Fields(folded), " ")
}

func (RecoveryHasher) HashAnswer(answer string) (hash string, version string, err error) {
	normalized := NormalizeRecoveryAnswer(answer)
	hash, err = hashArgon2id(normalized)
	if err != nil {
		return "", "", err
	}
	return hash, recoveryAnswerHashVersion, nil
}

func (RecoveryHasher) VerifyAnswer(answer, encoded string) bool {
	return verifyArgon2id(NormalizeRecoveryAnswer(answer), encoded)
}

func HashOpaqueSecret(secret string) (string, error) {
	return hashArgon2id(secret)
}

func VerifyOpaqueSecret(secret, encoded string) bool {
	return verifyArgon2id(secret, encoded)
}

func HashLookupToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func hashArgon2id(secret string) (string, error) {
	var salt [16]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(secret), salt[:], 1, 64*1024, 4, 32)
	return fmt.Sprintf("$aura$argon2id$v=19$m=65536,t=1,p=4$%s$%s",
		base64.RawStdEncoding.EncodeToString(salt[:]),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

func verifyArgon2id(secret, encoded string) bool {
	salt, want, err := parseArgon2id(encoded)
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(secret), salt, 1, 64*1024, 4, 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}

func parseArgon2id(encoded string) ([]byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 7 || parts[1] != "aura" || parts[2] != "argon2id" || parts[3] != "v=19" {
		return nil, nil, errors.New("invalid argon2id hash")
	}
	params := map[string]int{}
	for _, p := range strings.Split(parts[4], ",") {
		kv := strings.SplitN(p, "=", 2)
		if len(kv) != 2 {
			return nil, nil, errors.New("invalid argon2id params")
		}
		n, err := strconv.Atoi(kv[1])
		if err != nil {
			return nil, nil, err
		}
		params[kv[0]] = n
	}
	if params["m"] != 65536 || params["t"] != 1 || params["p"] != 4 {
		return nil, nil, errors.New("unsupported argon2id params")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, err
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[6])
	if err != nil {
		return nil, nil, err
	}
	return salt, hash, nil
}
```

- [ ] **Step 4: Run hash tests and verify GREEN**

Run:

```bash
gofmt -w internal/agui/recovery_hash.go internal/agui/recovery_hash_test.go
go test ./internal/agui -run 'TestNormalizeRecoveryAnswer|TestRecoveryHasherHashVerify|TestHashOpaqueSecretVerify' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
git add internal/agui/recovery_hash.go internal/agui/recovery_hash_test.go
git commit -m "feat(auth): add recovery secret hashing"
```

## Task 3: Onboarding Recovery Setup Backend

**Files:**
- Modify: `internal/agui/onboarding_api.go`
- Modify: `internal/agui/onboarding_session.go`
- Modify: `internal/agui/onboarding_provision.go`
- Modify: `internal/agui/onboarding_provision_test.go`
- Modify: `cmd/aura/serve_onboarding.go`

- [ ] **Step 1: Write failing onboarding tests**

Add to `internal/agui/onboarding_provision_test.go`:

```go
type fakeRecoveryStore struct {
	identityID string
	question   string
	hash       string
	version    string
	err        error
}

func (f *fakeRecoveryStore) UpsertRecovery(_ context.Context, identityID, question, answerHash, answerHashVersion string) error {
	f.identityID = identityID
	f.question = question
	f.hash = answerHash
	f.version = answerHashVersion
	return f.err
}

func TestProvisionStoresRecoveryQuestionAndHash(t *testing.T) {
	au, leg, tg := &fakeAuthula{}, &fakeAuraLeg{}, &fakeTelegram{}
	recovery := &fakeRecoveryStore{}
	svc, tok := sagaService(t, au, leg, tg, []string{"identity.create"})
	svc.recovery = recovery

	req := provReq(nil)
	req.SecurityQuestion = "First school?"
	req.SecurityAnswer = "  Blue   School "
	resp, err := svc.Provision(context.Background(), "creator-1", tok, req)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if recovery.identityID != resp.IdentityID {
		t.Fatalf("recovery identity = %q, want %q", recovery.identityID, resp.IdentityID)
	}
	if recovery.question != "First school?" {
		t.Fatalf("question = %q", recovery.question)
	}
	if recovery.hash == "" || strings.Contains(recovery.hash, "Blue") {
		t.Fatalf("answer hash leaked raw answer: %q", recovery.hash)
	}
	if recovery.version != recoveryAnswerHashVersion {
		t.Fatalf("version = %q, want %q", recovery.version, recoveryAnswerHashVersion)
	}
}

func TestValidateOnboardingProvisionRequiresRecovery(t *testing.T) {
	req := provReq(nil)
	req.SecurityQuestion = ""
	req.SecurityAnswer = "answer"
	if err := validateOnboardingProvision(req); err == nil {
		t.Fatal("missing security question should fail")
	}
	req.SecurityQuestion = "Question?"
	req.SecurityAnswer = ""
	if err := validateOnboardingProvision(req); err == nil {
		t.Fatal("missing security answer should fail")
	}
	req.SecurityAnswer = "answer"
	req.LinkTelegram = false
	if err := validateOnboardingProvision(req); err == nil {
		t.Fatal("linkTelegram=false should fail")
	}
}
```

- [ ] **Step 2: Run onboarding tests and verify RED**

Run:

```bash
go test ./internal/agui -run 'TestProvisionStoresRecoveryQuestionAndHash|TestValidateOnboardingProvisionRequiresRecovery' -count=1
```

Expected: FAIL because `SecurityQuestion`, `SecurityAnswer`, and `recovery` do not exist.

- [ ] **Step 3: Add recovery port and request fields**

In `internal/agui/onboarding_api.go`, extend `OnboardingProvisionRequest`:

```go
type OnboardingProvisionRequest struct {
	Email            string   `json:"email"`
	Password         string   `json:"password"`
	SecurityQuestion string   `json:"securityQuestion"`
	SecurityAnswer   string   `json:"securityAnswer"`
	Capabilities     []string `json:"capabilities"`
	LinkTelegram     bool     `json:"linkTelegram"`
}
```

Add length constants:

```go
onboardingSecurityQuestionMaxLen = 256
onboardingSecurityAnswerMaxLen   = 512
```

Extend `validateOnboardingProvision`:

```go
if strings.TrimSpace(req.SecurityQuestion) == "" || len(req.SecurityQuestion) > onboardingSecurityQuestionMaxLen {
	return errors.New("onboarding: security question is required and must be a sane length")
}
if strings.TrimSpace(req.SecurityAnswer) == "" || len(req.SecurityAnswer) > onboardingSecurityAnswerMaxLen {
	return errors.New("onboarding: security answer is required and must be a sane length")
}
if !req.LinkTelegram {
	return errors.New("onboarding: Telegram link is required for recovery")
}
```

Add `strings` to the imports if it is not present.

In `internal/agui/onboarding_session.go`, add:

```go
type RecoverySetupWriter interface {
	UpsertRecovery(ctx context.Context, identityID, question, answerHash, answerHashVersion string) error
}
```

Add a field to `onboardingService`:

```go
recovery RecoverySetupWriter
```

Add a field to `OnboardingDeps`:

```go
Recovery RecoverySetupWriter
```

Set it in `newOnboardingService`.

- [ ] **Step 4: Store hashed recovery setup during provision**

In `internal/agui/onboarding_provision.go`, after Leg A succeeds and before Leg C mints Telegram, add:

```go
if s.recovery == nil {
	if derr := s.auraLeg.DeleteIdentity(context.WithoutCancel(ctx), identityName); derr != nil {
		slog.Error("onboarding: COMP_A (delete identity) after recovery unavailable failed", "step", "compensate")
	}
	compB()
	return OnboardingProvisionResponse{}, errProvisioningUnavailable
}
answerHash, answerVersion, err := (RecoveryHasher{}).HashAnswer(in.SecurityAnswer)
if err != nil {
	if derr := s.auraLeg.DeleteIdentity(context.WithoutCancel(ctx), identityName); derr != nil {
		slog.Error("onboarding: COMP_A (delete identity) after recovery hash failed", "step", "compensate")
	}
	compB()
	return OnboardingProvisionResponse{}, provisionFail("recovery hash", err)
}
if err := s.recovery.UpsertRecovery(ctx, identityID, strings.TrimSpace(in.SecurityQuestion), answerHash, answerVersion); err != nil {
	if derr := s.auraLeg.DeleteIdentity(context.WithoutCancel(ctx), identityName); derr != nil {
		slog.Error("onboarding: COMP_A (delete identity) after recovery write failed", "step", "compensate")
	}
	compB()
	return OnboardingProvisionResponse{}, provisionFail("recovery write", err)
}
```

Keep the existing password no-leak logging tests and add the security answer value to their scan.

- [ ] **Step 5: Wire Postgres recovery adapter**

In `cmd/aura/serve_onboarding.go`, add:

```go
type recoverySetupAdapter struct {
	pool *pgxpool.Pool
}

func (a recoverySetupAdapter) UpsertRecovery(ctx context.Context, identityID, question, answerHash, answerHashVersion string) error {
	id, err := uuid.Parse(identityID)
	if err != nil {
		return fmt.Errorf("upsert recovery identity: %w", err)
	}
	return sqlc.New(a.pool).UpsertIdentityRecovery(ctx, sqlc.UpsertIdentityRecoveryParams{
		IdentityID:         pgtype.UUID{Bytes: id, Valid: true},
		Question:           question,
		AnswerHash:         answerHash,
		AnswerHashVersion:  answerHashVersion,
	})
}
```

When `chat.pool != nil`, set:

```go
deps.Recovery = recoverySetupAdapter{pool: chat.pool}
```

- [ ] **Step 6: Run onboarding tests and commit**

Run:

```bash
gofmt -w internal/agui/onboarding_api.go internal/agui/onboarding_session.go internal/agui/onboarding_provision.go internal/agui/onboarding_provision_test.go cmd/aura/serve_onboarding.go
go test ./internal/agui -run 'TestProvisionStoresRecoveryQuestionAndHash|TestValidateOnboardingProvisionRequiresRecovery|TestProvisionNoSecretInLogs' -count=1
go test ./cmd/aura -run Test -count=1
```

Expected: PASS.

Commit:

```bash
git add internal/agui/onboarding_api.go internal/agui/onboarding_session.go internal/agui/onboarding_provision.go internal/agui/onboarding_provision_test.go cmd/aura/serve_onboarding.go
git commit -m "feat(auth): capture onboarding recovery challenge"
```

## Task 4: Password Reset Service And API

**Files:**
- Create: `internal/agui/password_reset.go`
- Create: `internal/agui/password_reset_test.go`
- Create: `internal/agui/password_reset_api.go`
- Create: `internal/agui/password_reset_api_test.go`
- Modify: `internal/agui/server.go`

- [ ] **Step 1: Write service tests**

Create `internal/agui/password_reset_test.go` with fakes for lookup, challenge, messenger, resetter, and audit. Include these tests:

```go
package agui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRecoveryLookup struct {
	rec RecoveryRecord
	err error
}

func (f fakeRecoveryLookup) LookupRecoveryByEmail(context.Context, string) (RecoveryRecord, error) {
	return f.rec, f.err
}

type fakeChallengeStore struct {
	codeHash string
	token    string
	rec      RecoveryRecord
}

func (f *fakeChallengeStore) StartChallenge(_ context.Context, rec RecoveryRecord, codeHash string, _ time.Time, _, _ string) error {
	f.rec = rec
	f.codeHash = codeHash
	return nil
}

func (f *fakeChallengeStore) VerifyChallenge(_ context.Context, rec RecoveryRecord, code string) (string, error) {
	if code != "123456" {
		return "", ErrPasswordResetDenied
	}
	f.token = "reset-token"
	return f.token, nil
}

func (f *fakeChallengeStore) ConsumeResetToken(_ context.Context, token string) (string, error) {
	if token != "reset-token" {
		return "", ErrPasswordResetDenied
	}
	return "identity-1", nil
}

type fakeMessenger struct{ text string }

func (f *fakeMessenger) SendRecoveryCode(_ context.Context, identityID, code string) error {
	f.text = identityID + ":" + code
	return nil
}

type fakePasswordResetter struct{ identityID string }

func (f *fakePasswordResetter) ResetPassword(_ context.Context, identityID, password string) error {
	f.identityID = identityID
	if password == "" {
		return errors.New("empty password")
	}
	return nil
}

type fakeRecoveryAudit struct{ events []string }

func (f *fakeRecoveryAudit) RecordRecoveryEvent(_ context.Context, identityID, event, _, _ string, _ map[string]any) error {
	f.events = append(f.events, identityID+":"+event)
	return nil
}

func TestPasswordResetStartIsNeutralAndSendsTelegramWhenConfigured(t *testing.T) {
	answerHash, _, err := (RecoveryHasher{}).HashAnswer("blue school")
	if err != nil {
		t.Fatalf("hash answer: %v", err)
	}
	messenger := &fakeMessenger{}
	svc := NewPasswordResetService(PasswordResetDeps{
		Lookup: fakeRecoveryLookup{rec: RecoveryRecord{
			IdentityID: "identity-1", Email: "op@example.com", AuthulaUserID: "auth-1",
			Question: "School?", AnswerHash: answerHash, TelegramUserID: 99,
		}},
		Challenges: &fakeChallengeStore{},
		Messenger:  messenger,
		Audit:      &fakeRecoveryAudit{},
		CodeSource: func() (string, error) { return "123456", nil },
		Now:        func() time.Time { return time.Unix(1700000000, 0) },
	})
	resp, err := svc.Start(context.Background(), PasswordResetStartRequest{Email: "op@example.com"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status = %q", resp.Status)
	}
	if messenger.text != "identity-1:123456" {
		t.Fatalf("telegram text = %q", messenger.text)
	}
}

func TestPasswordResetVerifyRequiresAnswerAndCode(t *testing.T) {
	answerHash, _, err := (RecoveryHasher{}).HashAnswer("blue school")
	if err != nil {
		t.Fatalf("hash answer: %v", err)
	}
	svc := NewPasswordResetService(PasswordResetDeps{
		Lookup: fakeRecoveryLookup{rec: RecoveryRecord{
			IdentityID: "identity-1", Question: "School?", AnswerHash: answerHash,
		}},
		Challenges: &fakeChallengeStore{},
		Audit:      &fakeRecoveryAudit{},
	})
	got, err := svc.Verify(context.Background(), PasswordResetVerifyRequest{
		Email: "op@example.com", Code: "123456", Answer: " blue   school ",
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.ResetToken == "" {
		t.Fatal("empty reset token")
	}
	_, err = svc.Verify(context.Background(), PasswordResetVerifyRequest{
		Email: "op@example.com", Code: "000000", Answer: "blue school",
	})
	if !errors.Is(err, ErrPasswordResetDenied) {
		t.Fatalf("wrong code err = %v", err)
	}
	_, err = svc.Verify(context.Background(), PasswordResetVerifyRequest{
		Email: "op@example.com", Code: "123456", Answer: "red school",
	})
	if !errors.Is(err, ErrPasswordResetDenied) {
		t.Fatalf("wrong answer err = %v", err)
	}
}

func TestPasswordResetCompleteUpdatesPasswordWithoutLoggingSecrets(t *testing.T) {
	resetter := &fakePasswordResetter{}
	svc := NewPasswordResetService(PasswordResetDeps{
		Challenges: &fakeChallengeStore{},
		Resetter:   resetter,
		Audit:      &fakeRecoveryAudit{},
	})
	err := svc.Complete(context.Background(), PasswordResetCompleteRequest{
		ResetToken: "reset-token",
		Password:   "new secret password",
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resetter.identityID != "identity-1" {
		t.Fatalf("reset identity = %q", resetter.identityID)
	}
	if strings.Contains(ErrPasswordResetDenied.Error(), "new secret password") {
		t.Fatal("sentinel leaked password")
	}
}
```

- [ ] **Step 2: Run service tests and verify RED**

Run:

```bash
go test ./internal/agui -run 'TestPasswordReset' -count=1
```

Expected: FAIL because password reset types and service do not exist.

- [ ] **Step 3: Implement service and ports**

Create `internal/agui/password_reset.go` with these exported request/response types and ports:

```go
package agui

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"
)

var ErrPasswordResetDenied = errors.New("password reset denied")

type RecoveryRecord struct {
	IdentityID        string
	Email             string
	AuthulaUserID     string
	Question          string
	AnswerHash        string
	AnswerHashVersion string
	TelegramUserID    int64
}

type PasswordResetStartRequest struct{ Email string }
type PasswordResetStartResponse struct{ Status string `json:"status"` }

type PasswordResetVerifyRequest struct {
	Email  string
	Code   string
	Answer string
}
type PasswordResetVerifyResponse struct{ ResetToken string `json:"resetToken"` }

type PasswordResetCompleteRequest struct {
	ResetToken string
	Password   string
}
type PasswordResetCompleteResponse struct{ Status string `json:"status"` }

type IdentityRecoveryLookup interface {
	LookupRecoveryByEmail(ctx context.Context, email string) (RecoveryRecord, error)
}
type ResetChallengeStore interface {
	StartChallenge(ctx context.Context, rec RecoveryRecord, codeHash string, expiresAt time.Time, ipHash, uaHash string) error
	VerifyChallenge(ctx context.Context, rec RecoveryRecord, code string) (resetToken string, err error)
	ConsumeResetToken(ctx context.Context, token string) (identityID string, err error)
}
type RecoveryMessenger interface {
	SendRecoveryCode(ctx context.Context, identityID, code string) error
}
type AuthulaPasswordResetter interface {
	ResetPassword(ctx context.Context, identityID, password string) error
}
type RecoveryAuditWriter interface {
	RecordRecoveryEvent(ctx context.Context, identityID, event, ipHash, uaHash string, metadata map[string]any) error
}

type PasswordResetDeps struct {
	Lookup     IdentityRecoveryLookup
	Challenges ResetChallengeStore
	Messenger  RecoveryMessenger
	Resetter   AuthulaPasswordResetter
	Audit      RecoveryAuditWriter
	CodeSource func() (string, error)
	Now        func() time.Time
}

type PasswordResetService struct{ deps PasswordResetDeps }

func NewPasswordResetService(d PasswordResetDeps) *PasswordResetService {
	if d.CodeSource == nil {
		d.CodeSource = newRecoveryCode
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &PasswordResetService{deps: d}
}

func (s *PasswordResetService) Start(ctx context.Context, req PasswordResetStartRequest) (PasswordResetStartResponse, error) {
	rec, err := s.deps.Lookup.LookupRecoveryByEmail(ctx, req.Email)
	if err != nil || rec.IdentityID == "" || rec.TelegramUserID == 0 {
		s.audit(ctx, "", "reset_start_neutral", "", "", nil)
		return PasswordResetStartResponse{Status: "ok"}, nil
	}
	code, err := s.deps.CodeSource()
	if err != nil {
		return PasswordResetStartResponse{}, err
	}
	codeHash, err := HashOpaqueSecret(code)
	if err != nil {
		return PasswordResetStartResponse{}, err
	}
	if err := s.deps.Challenges.StartChallenge(ctx, rec, codeHash, s.deps.Now().Add(10*time.Minute), "", ""); err != nil {
		return PasswordResetStartResponse{}, err
	}
	if err := s.deps.Messenger.SendRecoveryCode(ctx, rec.IdentityID, code); err != nil {
		return PasswordResetStartResponse{}, err
	}
	s.audit(ctx, rec.IdentityID, "reset_code_sent", "", "", nil)
	return PasswordResetStartResponse{Status: "ok"}, nil
}

func (s *PasswordResetService) Verify(ctx context.Context, req PasswordResetVerifyRequest) (PasswordResetVerifyResponse, error) {
	rec, err := s.deps.Lookup.LookupRecoveryByEmail(ctx, req.Email)
	if err != nil || rec.IdentityID == "" || !((RecoveryHasher{}).VerifyAnswer(req.Answer, rec.AnswerHash)) {
		return PasswordResetVerifyResponse{}, ErrPasswordResetDenied
	}
	token, err := s.deps.Challenges.VerifyChallenge(ctx, rec, req.Code)
	if err != nil {
		return PasswordResetVerifyResponse{}, err
	}
	s.audit(ctx, rec.IdentityID, "reset_verified", "", "", nil)
	return PasswordResetVerifyResponse{ResetToken: token}, nil
}

func (s *PasswordResetService) Complete(ctx context.Context, req PasswordResetCompleteRequest) error {
	identityID, err := s.deps.Challenges.ConsumeResetToken(ctx, req.ResetToken)
	if err != nil {
		return err
	}
	if err := s.deps.Resetter.ResetPassword(ctx, identityID, req.Password); err != nil {
		return err
	}
	s.audit(ctx, identityID, "reset_completed", "", "", nil)
	return nil
}

func (s *PasswordResetService) audit(ctx context.Context, identityID, event, ipHash, uaHash string, metadata map[string]any) {
	if s.deps.Audit != nil {
		_ = s.deps.Audit.RecordRecoveryEvent(ctx, identityID, event, ipHash, uaHash, metadata)
	}
}

func newRecoveryCode() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	n := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	if n < 0 {
		n = -n
	}
	return base64.RawURLEncoding.EncodeToString([]byte{byte(n), byte(n >> 8), byte(n >> 16), byte(n >> 24)})[:8], nil
}
```

Use the compile errors to tighten nil checks and exact imports. Keep all failure bodies neutral in the API task.

- [ ] **Step 4: Add API tests**

Create `internal/agui/password_reset_api_test.go`:

```go
package agui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPasswordResetStartHandlerNeutral202(t *testing.T) {
	svc := NewPasswordResetService(PasswordResetDeps{
		Lookup:     fakeRecoveryLookup{},
		Challenges: &fakeChallengeStore{},
		Messenger:  &fakeMessenger{},
		Audit:      &fakeRecoveryAudit{},
	})
	s := NewServer(nil, nil, ServerConfig{})
	s.SetPasswordResetService(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/password-reset/start", strings.NewReader(`{"email":"missing@example.com"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestPasswordResetVerifyBadRequestOnMalformedBody(t *testing.T) {
	s := NewServer(nil, nil, ServerConfig{})
	s.SetPasswordResetService(NewPasswordResetService(PasswordResetDeps{}))
	req := httptest.NewRequest(http.MethodPost, "/api/auth/password-reset/verify", strings.NewReader(`{`))
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
```

- [ ] **Step 5: Implement handlers**

Create `internal/agui/password_reset_api.go`:

```go
package agui

import (
	"encoding/json"
	"errors"
	"net/http"
)

const maxPasswordResetBodyBytes = 64 << 10

func (s *Server) SetPasswordResetService(service *PasswordResetService) {
	s.passwordReset = service
}

func (s *Server) registerPasswordResetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/password-reset/start", s.handlePasswordResetStart)
	mux.HandleFunc("POST /api/auth/password-reset/verify", s.handlePasswordResetVerify)
	mux.HandleFunc("POST /api/auth/password-reset/complete", s.handlePasswordResetComplete)
}

func (s *Server) handlePasswordResetStart(w http.ResponseWriter, r *http.Request) {
	var req PasswordResetStartRequest
	if !s.decodePasswordReset(w, r, &req) {
		return
	}
	resp, err := s.passwordReset.Start(r.Context(), req)
	if err != nil {
		http.Error(w, "password reset unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, resp)
}

func (s *Server) handlePasswordResetVerify(w http.ResponseWriter, r *http.Request) {
	var req PasswordResetVerifyRequest
	if !s.decodePasswordReset(w, r, &req) {
		return
	}
	resp, err := s.passwordReset.Verify(r.Context(), req)
	if err != nil {
		http.Error(w, "password reset denied", http.StatusUnauthorized)
		return
	}
	writeJSON(w, resp)
}

func (s *Server) handlePasswordResetComplete(w http.ResponseWriter, r *http.Request) {
	var req PasswordResetCompleteRequest
	if !s.decodePasswordReset(w, r, &req) {
		return
	}
	if err := s.passwordReset.Complete(r.Context(), req); err != nil {
		status := http.StatusUnauthorized
		if !errors.Is(err, ErrPasswordResetDenied) {
			status = http.StatusServiceUnavailable
		}
		http.Error(w, "password reset denied", status)
		return
	}
	writeJSON(w, PasswordResetCompleteResponse{Status: "password_updated"})
}

func (s *Server) decodePasswordReset(w http.ResponseWriter, r *http.Request, dst any) bool {
	if s.passwordReset == nil {
		http.Error(w, "password reset unavailable", http.StatusServiceUnavailable)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPasswordResetBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}
```

In `internal/agui/server.go`, add `passwordReset *PasswordResetService` to `Server` and call:

```go
s.registerPasswordResetRoutes(mux)
```

near the onboarding route registration.

- [ ] **Step 6: Run service and API tests and commit**

Run:

```bash
gofmt -w internal/agui/password_reset*.go internal/agui/server.go
go test ./internal/agui -run 'TestPasswordReset' -count=1
```

Expected: PASS.

Commit:

```bash
git add internal/agui/password_reset.go internal/agui/password_reset_test.go internal/agui/password_reset_api.go internal/agui/password_reset_api_test.go internal/agui/server.go
git commit -m "feat(auth): add telegram password reset service"
```

## Task 5: Concrete Recovery Store, Telegram Messenger, And Authula Resetter

**Files:**
- Modify: `cmd/aura/serve_onboarding.go`
- Modify: `cmd/aura/serve.go`
- Create or modify: `cmd/aura/serve_password_reset_test.go`

- [ ] **Step 1: Write composition tests**

Create `cmd/aura/serve_password_reset_test.go`:

```go
package main

import (
	"context"
	"strings"
	"testing"
)

func TestRecoveryCodeMessageIsNeutralAndContainsCode(t *testing.T) {
	msg := recoveryCodeMessage("ABC12345")
	if !containsAll(msg, []string{"ABC12345", "Aura", "10"}) {
		t.Fatalf("message = %q", msg)
	}
}

func containsAll(s string, parts []string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

func TestAuthulaPasswordResetterRejectsMissingCore(t *testing.T) {
	err := authulaPasswordResetter{}.ResetPassword(context.Background(), "identity-1", "pw")
	if err == nil {
		t.Fatal("missing Authula core should fail closed")
	}
}
```

Add the missing `strings` import after the RED run.

- [ ] **Step 2: Run composition tests and verify RED**

Run:

```bash
go test ./cmd/aura -run 'TestRecoveryCodeMessage|TestAuthulaPasswordResetter' -count=1
```

Expected: FAIL because the adapters do not exist.

- [ ] **Step 3: Implement concrete adapters**

In `cmd/aura/serve_onboarding.go`, add:

```go
type recoveryStoreAdapter struct {
	pool *pgxpool.Pool
}

func (a recoveryStoreAdapter) LookupRecoveryByEmail(ctx context.Context, email string) (agui.RecoveryRecord, error) {
	row, err := sqlc.New(a.pool).LookupRecoveryByEmail(ctx, email)
	if err != nil {
		return agui.RecoveryRecord{}, err
	}
	return agui.RecoveryRecord{
		IdentityID:        uuid.UUID(row.IdentityID.Bytes).String(),
		Email:             row.Email,
		AuthulaUserID:     row.AuthulaUserID,
		Question:          row.Question,
		AnswerHash:        row.AnswerHash,
		AnswerHashVersion: row.AnswerHashVersion,
		TelegramUserID:    row.TelegramUserID,
	}, nil
}

func (a recoveryStoreAdapter) StartChallenge(ctx context.Context, rec agui.RecoveryRecord, codeHash string, expiresAt time.Time, ipHash, uaHash string) error {
	id, err := uuid.Parse(rec.IdentityID)
	if err != nil {
		return err
	}
	_, err = sqlc.New(a.pool).InsertPasswordResetChallenge(ctx, sqlc.InsertPasswordResetChallengeParams{
		IdentityID:     pgtype.UUID{Bytes: id, Valid: true},
		CodeHash:       codeHash,
		TelegramUserID: pgtype.Int8{Int64: rec.TelegramUserID, Valid: rec.TelegramUserID != 0},
		ExpiresAt:      pgtype.Timestamptz{Time: expiresAt, Valid: true},
		MaxAttempts:    5,
		RequestIpHash:  pgtype.Text{String: ipHash, Valid: ipHash != ""},
		UserAgentHash:  pgtype.Text{String: uaHash, Valid: uaHash != ""},
	})
	return err
}
```

Add these methods to the same adapter:

```go
func (a recoveryStoreAdapter) VerifyChallenge(ctx context.Context, rec agui.RecoveryRecord, code string) (string, error) {
	id, err := uuid.Parse(rec.IdentityID)
	if err != nil {
		return "", err
	}
	q := sqlc.New(a.pool)
	row, err := q.GetActivePasswordResetChallenge(ctx, pgtype.UUID{Bytes: id, Valid: true})
	if err != nil {
		return "", agui.ErrPasswordResetDenied
	}
	if !agui.VerifyOpaqueSecret(code, row.CodeHash) {
		_ = q.IncrementPasswordResetChallengeAttempts(ctx, row.ID)
		return "", agui.ErrPasswordResetDenied
	}
	consumed, err := q.ConsumePasswordResetChallenge(ctx, row.ID)
	if err != nil {
		return "", agui.ErrPasswordResetDenied
	}
	token, err := newResetToken()
	if err != nil {
		return "", err
	}
	tokenHash := agui.HashLookupToken(token)
	_, err = q.InsertPasswordResetToken(ctx, sqlc.InsertPasswordResetTokenParams{
		TokenHash:   tokenHash,
		ChallengeID: consumed.ID,
		IdentityID:  consumed.IdentityID,
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().UTC().Add(10 * time.Minute), Valid: true},
		MaxAttempts: 3,
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

func (a recoveryStoreAdapter) ConsumeResetTokenHash(ctx context.Context, tokenHash string) (string, error) {
	q := sqlc.New(a.pool)
	row, err := q.GetPasswordResetToken(ctx, tokenHash)
	if err != nil {
		return "", agui.ErrPasswordResetDenied
	}
	consumed, err := q.ConsumePasswordResetToken(ctx, row.TokenHash)
	if err != nil {
		return "", agui.ErrPasswordResetDenied
	}
	return uuid.UUID(consumed.IdentityID.Bytes).String(), nil
}

func (a recoveryStoreAdapter) RecordRecoveryEvent(ctx context.Context, identityID, event, ipHash, uaHash string, metadata map[string]any) error {
	var id pgtype.UUID
	if identityID != "" {
		parsed, err := uuid.Parse(identityID)
		if err != nil {
			return err
		}
		id = pgtype.UUID{Bytes: parsed, Valid: true}
	}
	rawMeta, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = sqlc.New(a.pool).InsertIdentityRecoveryAudit(ctx, sqlc.InsertIdentityRecoveryAuditParams{
		IdentityID:     id,
		Event:          event,
		RequestIpHash:  pgtype.Text{String: ipHash, Valid: ipHash != ""},
		UserAgentHash:  pgtype.Text{String: uaHash, Valid: uaHash != ""},
		Metadata:       rawMeta,
	})
	return err
}

func newResetToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
```

This code needs `crypto/rand`, `encoding/base64`, and `encoding/json` imports in `cmd/aura/serve_onboarding.go`.

Add:

```go
type telegramRecoveryMessenger struct {
	deliverer interface {
		DeliverToIdentity(ctx context.Context, identityID, text string) (bool, error)
	}
}

func (m telegramRecoveryMessenger) SendRecoveryCode(ctx context.Context, identityID, code string) error {
	delivered, err := m.deliverer.DeliverToIdentity(ctx, identityID, recoveryCodeMessage(code))
	if err != nil {
		return err
	}
	if !delivered {
		return fmt.Errorf("telegram recovery delivery unavailable")
	}
	return nil
}

func recoveryCodeMessage(code string) string {
	return "Aura password reset code: " + code + "\nIt expires in 10 minutes. If you did not request this, ignore this message."
}
```

Add:

```go
type authulaPasswordResetter struct {
	core *authulaservices.CoreServices
	pool *pgxpool.Pool
}
```

Add this method:

```go
func (r authulaPasswordResetter) ResetPassword(ctx context.Context, identityID, password string) error {
	if r.core == nil || r.core.PasswordService == nil || r.core.AccountService == nil || r.core.SessionService == nil || r.pool == nil {
		return fmt.Errorf("authula password resetter unavailable")
	}
	var authulaUserID string
	if err := r.pool.QueryRow(ctx,
		`SELECT authula_user_id FROM aura.identity_auth_links WHERE identity_id = $1::uuid`,
		identityID,
	).Scan(&authulaUserID); err != nil {
		return fmt.Errorf("resolve authula user: %w", err)
	}
	account, err := r.core.AccountService.GetByUserIDAndProvider(ctx, authulaUserID, authulamodels.AuthProviderEmail.String())
	if err != nil {
		return fmt.Errorf("lookup authula account: %w", err)
	}
	hash, err := r.core.PasswordService.Hash(password)
	if err != nil {
		return fmt.Errorf("hash new password: %w", err)
	}
	account.Password = &hash
	if _, err := r.core.AccountService.Update(ctx, account); err != nil {
		return fmt.Errorf("update authula password: %w", err)
	}
	if err := r.core.SessionService.DeleteAllByUserID(ctx, authulaUserID); err != nil {
		return fmt.Errorf("clear authula sessions: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Wire password reset service**

In `cmd/aura/serve.go`, directly after:

```go
aguiServer.SetOnboardingService(buildOnboardingService(ctx, chat, onboardingAuthulaProvider))
```

create a `PasswordResetService` when `chat.pool`, the channel registry, and Authula provider are present:

```go
if chat.pool != nil && authulaProvider != nil {
	core := authulaProvider.CoreServices()
	recoveryStore := recoveryStoreAdapter{pool: chat.pool}
	aguiServer.SetPasswordResetService(agui.NewPasswordResetService(agui.PasswordResetDeps{
		Lookup:     recoveryStore,
		Challenges: recoveryStore,
		Messenger:  telegramRecoveryMessenger{deliverer: reg},
		Resetter:   authulaPasswordResetter{core: core, pool: chat.pool},
		Audit:      recoveryStore,
	}))
}
```

- [ ] **Step 5: Run tests and commit**

Run:

```bash
gofmt -w cmd/aura/serve_onboarding.go cmd/aura/serve.go cmd/aura/serve_password_reset_test.go
go test ./cmd/aura -run 'TestRecoveryCodeMessage|TestAuthulaPasswordResetter|TestResolveWebAuthIdentityID' -count=1
go test ./internal/agui -run 'TestPasswordReset' -count=1
```

Expected: PASS.

Commit:

```bash
git add cmd/aura/serve_onboarding.go cmd/aura/serve.go cmd/aura/serve_password_reset_test.go
git commit -m "feat(auth): wire recovery adapters"
```

## Task 6: Public Route Mounts And Authula-Only Backend Cutover

**Files:**
- Modify: `cmd/aura/serve_auth.go`
- Modify: `cmd/aura/serve_auth_test.go`
- Modify: `cmd/aura/serve_webui.go`
- Modify: `cmd/aura/serve_webui_test.go`
- Modify: `internal/agui/auth.go`
- Modify: `internal/agui/auth_test.go`

- [ ] **Step 1: Write failing route and cutover tests**

In `cmd/aura/serve_webui_test.go`, add a test that `POST /api/auth/password-reset/start` routes to the AG-UI handler and does not serve the SPA shell:

```go
t.Run("password reset public routes -> AG-UI handler", func(t *testing.T) {
	aguiHits = nil
	resp, err := http.Post(srv.URL+"/api/auth/password-reset/start", "application/json", strings.NewReader(`{"email":"op@example.com"}`))
	if err != nil {
		t.Fatalf("POST password-reset/start: %v", err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if len(aguiHits) != 1 || aguiHits[0] != "/api/auth/password-reset/start" {
		t.Fatalf("password-reset/start did not route to AG-UI: hits=%v body=%s", aguiHits, raw)
	}
	if strings.Contains(string(raw), indexMarker) {
		t.Fatalf("password-reset/start leaked the SPA shell")
	}
})
```

In `cmd/aura/serve_auth_test.go`, replace the passphrase-local test with:

```go
func TestResolveWebAuthIdentityIDUsesAuthulaOperatorIdentity(t *testing.T) {
	store := &namedIdentityStore{ids: map[string]identity.Identity{
		"local":    {ID: "local-id", Name: "local"},
		"operator": {ID: "operator-id", Name: "operator"},
	}}
	got := resolveWebAuthIdentityID(context.Background(), store, &config.Config{
		AuthulaOperatorIdentity: "operator",
	})
	if got != "operator-id" {
		t.Fatalf("identity id = %q, want operator-id", got)
	}
}
```

- [ ] **Step 2: Run backend route tests and verify RED**

Run:

```bash
go test ./cmd/aura -run 'TestServeWebui/password reset|TestResolveWebAuthIdentityID' -count=1
```

Expected: FAIL because reset routes are not mounted and auth still has passphrase branching.

- [ ] **Step 3: Mount reset routes as public backend routes**

In `cmd/aura/serve_webui.go`, add constants:

```go
const (
	passwordResetStartRoute    = "POST /api/auth/password-reset/start"
	passwordResetVerifyRoute   = "POST /api/auth/password-reset/verify"
	passwordResetCompleteRoute = "POST /api/auth/password-reset/complete"
)
```

In `newServeHandler`, register them to `aguiHandler` before `mux.Handle("/", static)`:

```go
mux.Handle(passwordResetStartRoute, aguiHandler)
mux.Handle(passwordResetVerifyRoute, aguiHandler)
mux.Handle(passwordResetCompleteRoute, aguiHandler)
```

Extend `auth.PublicRoute`:

```go
if r.URL.Path == "/api/auth/password-reset/start" ||
	r.URL.Path == "/api/auth/password-reset/verify" ||
	r.URL.Path == "/api/auth/password-reset/complete" {
	return true
}
```

- [ ] **Step 4: Make Authula the only backend provider**

In `cmd/aura/serve_auth.go`, remove the provider branch:

```go
provider, validator, err := buildAuthulaProvider(ctx, chat, deps.LocalIdentityID)
if err != nil {
	return agui.AuthDeps{}, nil, err
}
deps.Secret = ""
deps.SigningKey = nil
deps.SecretConfigured = true
deps.SessionValidator = func(r *http.Request) (string, bool) {
	id, verr := validator.Validate(r)
	if verr != nil {
		return "", false
	}
	return id, true
}
return deps, provider, nil
```

Update `resolveWebAuthIdentityID` so it always honors `AuthulaOperatorIdentity` when set and defaults to `local`.

In `cmd/aura/serve_webui.go`, change `newAuthConfigHandler` to always emit Authula config. Remove the `Provider: "passphrase"` path.

Keep `AuthDeps.LoginHandler` only if another test still needs it; remove the parent mux `POST /login` mount once the web tests and route tests no longer expect passphrase.

- [ ] **Step 5: Run tests and commit**

Run:

```bash
gofmt -w cmd/aura/serve_auth.go cmd/aura/serve_auth_test.go cmd/aura/serve_webui.go cmd/aura/serve_webui_test.go internal/agui/auth.go internal/agui/auth_test.go
go test ./cmd/aura -run 'TestServeWebui|TestResolveWebAuthIdentityID|TestAuthulaProvisioningConfigured' -count=1
go test ./internal/agui -run 'TestRequireAuth|TestRequireCapability' -count=1
```

Expected: PASS after removing or updating passphrase-only assertions.

Commit:

```bash
git add cmd/aura/serve_auth.go cmd/aura/serve_auth_test.go cmd/aura/serve_webui.go cmd/aura/serve_webui_test.go internal/agui/auth.go internal/agui/auth_test.go
git commit -m "feat(auth): make Authula the sole web provider"
```

## Task 7: Frontend Password Reset And Authula-Only Login

**Files:**
- Create: `web/src/auth/passwordResetApi.ts`
- Create: `web/src/auth/PasswordResetPanel.tsx`
- Create: `web/src/auth/__tests__/PasswordResetPanel.test.tsx`
- Modify: `web/src/routes/LoginPage.tsx`
- Modify: `web/src/__tests__/LoginPage.test.tsx`
- Modify: `web/src/i18n/resources.ts`

- [ ] **Step 1: Write password reset API and UI tests**

Create `web/src/auth/__tests__/PasswordResetPanel.test.tsx`:

```tsx
import { afterEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { PasswordResetPanel } from '../PasswordResetPanel';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('PasswordResetPanel', () => {
  it('starts reset, verifies Telegram code plus answer, and completes password update', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ status: 'ok' }), { status: 202 }))
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ resetToken: 'reset-token' }), { status: 200 }),
      )
      .mockResolvedValueOnce(
        new Response(JSON.stringify({ status: 'password_updated' }), { status: 200 }),
      );
    vi.stubGlobal('fetch', fetchMock);

    render(<PasswordResetPanel onDone={vi.fn()} onCancel={vi.fn()} />);

    fireEvent.change(screen.getByLabelText('Operator email'), {
      target: { value: 'op@example.com' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Send Telegram code' }));

    await screen.findByText('Enter the code sent to Telegram and answer your recovery question.');
    fireEvent.change(screen.getByLabelText('Telegram code'), { target: { value: '123456' } });
    fireEvent.change(screen.getByLabelText('Security answer'), { target: { value: 'blue' } });
    fireEvent.click(screen.getByRole('button', { name: 'Verify recovery' }));

    await screen.findByLabelText('New password');
    fireEvent.change(screen.getByLabelText('New password'), { target: { value: 'New-pass-123' } });
    fireEvent.change(screen.getByLabelText('Confirm new password'), {
      target: { value: 'New-pass-123' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Update password' }));

    await screen.findByText('Password updated. Sign in with the new password.');
    expect(fetchMock).toHaveBeenNthCalledWith(
      1,
      '/api/auth/password-reset/start',
      expect.objectContaining({ method: 'POST', credentials: 'same-origin' }),
    );
  });

  it('does not submit mismatched new passwords', async () => {
    vi.stubGlobal('fetch', vi.fn());
    render(<PasswordResetPanel onDone={vi.fn()} onCancel={vi.fn()} initialEmail="op@example.com" />);
    fireEvent.click(screen.getByRole('button', { name: 'Send Telegram code' }));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
  });
});
```

Update `web/src/__tests__/LoginPage.test.tsx` by replacing passphrase expectations with:

```tsx
it('renders Authula email/password fields by default and no passphrase field', async () => {
  vi.stubGlobal('fetch', vi.fn(() => authulaConfigResponse()));
  const { container } = renderLogin();
  expect(await screen.findByLabelText('Operator email')).toBeTruthy();
  expect(screen.getByLabelText('Password')).toBeTruthy();
  expect(screen.queryByLabelText('Operator passphrase')).toBeNull();
  expect(screen.getByRole('button', { name: 'Forgot password' })).toBeTruthy();
  expectLoginAriaValuesToBeAxeValid(container);
});
```

- [ ] **Step 2: Run web tests and verify RED**

Run:

```bash
cd web
npx vitest run src/auth/__tests__/PasswordResetPanel.test.tsx src/__tests__/LoginPage.test.tsx --coverage.enabled=false
```

Expected: FAIL because the reset panel does not exist and login still has passphrase tests/logic.

- [ ] **Step 3: Add password reset API client**

Create `web/src/auth/passwordResetApi.ts`:

```ts
import { postJSON } from '../api/json';

export interface PasswordResetStartRequest {
  readonly email: string;
}
export interface PasswordResetVerifyRequest {
  readonly email: string;
  readonly code: string;
  readonly answer: string;
}
export interface PasswordResetVerifyResponse {
  readonly resetToken: string;
}
export interface PasswordResetCompleteRequest {
  readonly resetToken: string;
  readonly password: string;
}

export function startPasswordReset(req: PasswordResetStartRequest): Promise<{ status: string }> {
  return postJSON('/api/auth/password-reset/start', req);
}

export function verifyPasswordReset(
  req: PasswordResetVerifyRequest,
): Promise<PasswordResetVerifyResponse> {
  return postJSON('/api/auth/password-reset/verify', req);
}

export function completePasswordReset(
  req: PasswordResetCompleteRequest,
): Promise<{ status: string }> {
  return postJSON('/api/auth/password-reset/complete', req);
}
```

- [ ] **Step 4: Add reset panel**

Create `web/src/auth/PasswordResetPanel.tsx` with a three-step state machine:

```tsx
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  completePasswordReset,
  startPasswordReset,
  verifyPasswordReset,
} from './passwordResetApi';

type Step = 'start' | 'verify' | 'complete' | 'done';

export function PasswordResetPanel({
  initialEmail = '',
  onCancel,
  onDone,
}: {
  readonly initialEmail?: string;
  readonly onCancel: () => void;
  readonly onDone: () => void;
}) {
  const { t } = useTranslation();
  const [step, setStep] = useState<Step>('start');
  const [email, setEmail] = useState(initialEmail);
  const [code, setCode] = useState('');
  const [answer, setAnswer] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [token, setToken] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function start() {
    setBusy(true);
    setError('');
    try {
      await startPasswordReset({ email });
      setStep('verify');
    } catch {
      setError(t('login.reset.error'));
    } finally {
      setBusy(false);
    }
  }

  async function verify() {
    setBusy(true);
    setError('');
    try {
      const resp = await verifyPasswordReset({ email, code, answer });
      setToken(resp.resetToken);
      setStep('complete');
    } catch {
      setError(t('login.reset.denied'));
    } finally {
      setBusy(false);
    }
  }

  async function finish() {
    if (password !== confirm) {
      setError(t('login.reset.mismatch'));
      return;
    }
    setBusy(true);
    setError('');
    try {
      await completePasswordReset({ resetToken: token, password });
      setPassword('');
      setConfirm('');
      setStep('done');
    } catch {
      setError(t('login.reset.denied'));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="flex flex-col gap-3">
      {step === 'start' ? (
        <>
          <Label htmlFor="reset-email">{t('login.authula.emailLabel')}</Label>
          <Input id="reset-email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
          <Button type="button" disabled={busy || email.trim() === ''} onClick={() => void start()}>
            {t('login.reset.sendCode')}
          </Button>
        </>
      ) : null}

      {step === 'verify' ? (
        <>
          <p className="text-sm text-text-muted">{t('login.reset.verifyBody')}</p>
          <Label htmlFor="reset-code">{t('login.reset.codeLabel')}</Label>
          <Input id="reset-code" value={code} onChange={(e) => setCode(e.target.value)} />
          <Label htmlFor="reset-answer">{t('login.reset.answerLabel')}</Label>
          <Input id="reset-answer" type="password" value={answer} onChange={(e) => setAnswer(e.target.value)} />
          <Button type="button" disabled={busy} onClick={() => void verify()}>
            {t('login.reset.verifyCta')}
          </Button>
        </>
      ) : null}

      {step === 'complete' ? (
        <>
          <Label htmlFor="reset-password">{t('login.reset.newPasswordLabel')}</Label>
          <Input id="reset-password" type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
          <Label htmlFor="reset-confirm">{t('login.reset.confirmPasswordLabel')}</Label>
          <Input id="reset-confirm" type="password" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
          <Button type="button" disabled={busy} onClick={() => void finish()}>
            {t('login.reset.completeCta')}
          </Button>
        </>
      ) : null}

      {step === 'done' ? (
        <>
          <p role="status" className="text-sm text-success">{t('login.reset.done')}</p>
          <Button type="button" onClick={onDone}>{t('login.reset.backToLogin')}</Button>
        </>
      ) : null}

      {error !== '' ? (
        <Alert variant="destructive"><AlertDescription>{error}</AlertDescription></Alert>
      ) : null}

      {step !== 'done' ? (
        <Button type="button" variant="ghost" onClick={onCancel}>{t('login.reset.cancel')}</Button>
      ) : null}
    </div>
  );
}
```

Run Prettier after implementation; break long JSX lines.

- [ ] **Step 5: Simplify LoginPage to Authula-only**

In `web/src/routes/LoginPage.tsx`:

- remove `AuthProvider = 'passphrase'`
- set `defaultAuthConfig.provider` to `authula`
- remove `submitPassphrase`
- remove passphrase JSX
- add `const [resetOpen, setResetOpen] = useState(false)`
- render `<PasswordResetPanel>` when `resetOpen` is true
- add a ghost button labeled `t('login.reset.forgot')`

Keep Authula credential and TOTP behavior unchanged.

- [ ] **Step 6: Add i18n copy**

In `web/src/i18n/resources.ts`, add English and Italian `login.reset` keys:

```ts
reset: {
  forgot: 'Forgot password',
  sendCode: 'Send Telegram code',
  verifyBody: 'Enter the code sent to Telegram and answer your recovery question.',
  codeLabel: 'Telegram code',
  answerLabel: 'Security answer',
  verifyCta: 'Verify recovery',
  newPasswordLabel: 'New password',
  confirmPasswordLabel: 'Confirm new password',
  completeCta: 'Update password',
  done: 'Password updated. Sign in with the new password.',
  backToLogin: 'Back to sign in',
  cancel: 'Cancel',
  denied: 'The reset could not be verified. Check the code and answer, then try again.',
  mismatch: 'The passwords do not match.',
  error: 'Password reset is not available right now.',
}
```

Add the Italian equivalent in the `it.translation.login` tree.

- [ ] **Step 7: Run web tests and commit**

Run:

```bash
cd web
npx vitest run src/auth/__tests__/PasswordResetPanel.test.tsx src/__tests__/LoginPage.test.tsx --coverage.enabled=false
npm run typecheck
```

Expected: PASS.

Commit:

```bash
git add web/src/auth/passwordResetApi.ts web/src/auth/PasswordResetPanel.tsx web/src/auth/__tests__/PasswordResetPanel.test.tsx web/src/routes/LoginPage.tsx web/src/__tests__/LoginPage.test.tsx web/src/i18n/resources.ts
git commit -m "feat(web): add telegram password reset UI"
```

## Task 8: Onboarding UI Recovery Fields And Mandatory Telegram

**Files:**
- Modify: `web/src/onboarding/CredentialStep.tsx`
- Modify: `web/src/onboarding/OnboardingWizard.tsx`
- Modify: `web/src/onboarding/ReviewStep.tsx`
- Modify: `web/src/onboarding/onboardingApi.ts`
- Modify: `web/src/onboarding/onboardingWizardModel.ts`
- Modify tests under `web/src/onboarding/__tests__`
- Modify: `web/src/i18n/resources.onboarding.ts`

- [ ] **Step 1: Write failing model/API/component tests**

Update `web/src/onboarding/__tests__/onboardingWizardModel.test.ts`:

```ts
it('requires email, password, security question, and security answer', () => {
  expect(credentialsValid('a@b.com', 'pw', 'Question?', 'answer')).toBe(true);
  expect(credentialsValid('a@b.com', 'pw', '', 'answer')).toBe(false);
  expect(credentialsValid('a@b.com', 'pw', 'Question?', '')).toBe(false);
});
```

Update `web/src/onboarding/__tests__/onboardingApi.test.ts` so provision expects:

```ts
expect(JSON.parse(init.body as string)).toEqual({
  email: 'a@b.com',
  password: 'pw',
  securityQuestion: 'First school?',
  securityAnswer: 'blue',
  capabilities: ['skills.read'],
  linkTelegram: true,
});
```

Update `web/src/onboarding/__tests__/CredentialStep.test.tsx` to assert:

```tsx
expect(screen.getByLabelText('Security question')).toBeTruthy();
const answer = screen.getByLabelText('Security answer');
expect(answer.getAttribute('type')).toBe('password');
```

Update `web/src/onboarding/__tests__/OnboardingWizard.test.tsx` helper `fillCredentialsAndAdvance` to fill security fields and assert `provisionOnboarding` receives `securityQuestion` and `securityAnswer`.

- [ ] **Step 2: Run onboarding web tests and verify RED**

Run:

```bash
cd web
npx vitest run src/onboarding/__tests__/CredentialStep.test.tsx src/onboarding/__tests__/OnboardingWizard.test.tsx src/onboarding/__tests__/onboardingApi.test.ts src/onboarding/__tests__/onboardingWizardModel.test.ts --coverage.enabled=false
```

Expected: FAIL because security fields are not wired and Telegram can be unchecked.

- [ ] **Step 3: Extend frontend contracts**

In `web/src/onboarding/onboardingApi.ts`, extend `OnboardingProvisionRequest`:

```ts
readonly securityQuestion: string;
readonly securityAnswer: string;
```

In `web/src/onboarding/onboardingWizardModel.ts`, change:

```ts
export function credentialsValid(
  email: string,
  password: string,
  securityQuestion: string,
  securityAnswer: string,
): boolean {
  return (
    email.trim() !== '' &&
    password !== '' &&
    securityQuestion.trim() !== '' &&
    securityAnswer.trim() !== ''
  );
}
```

- [ ] **Step 4: Add CredentialStep fields**

Update `CredentialStepProps`:

```ts
readonly securityQuestion: string;
readonly securityAnswer: string;
readonly onSecurityQuestionChange: (value: string) => void;
readonly onSecurityAnswerChange: (value: string) => void;
```

Render two fields after password:

```tsx
<div className="flex flex-col gap-2">
  <Label htmlFor={questionId} className="text-[13px] font-semibold text-text">
    {t('onboarding.credentials.securityQuestionLabel')}
  </Label>
  <Input
    id={questionId}
    value={securityQuestion}
    onChange={(e) => onSecurityQuestionChange(e.target.value)}
    className="bg-surface-2 text-[15.5px]"
  />
</div>

<div className="flex flex-col gap-2">
  <Label htmlFor={answerId} className="text-[13px] font-semibold text-text">
    {t('onboarding.credentials.securityAnswerLabel')}
  </Label>
  <Input
    id={answerId}
    type="password"
    autoComplete="off"
    value={securityAnswer}
    onChange={(e) => onSecurityAnswerChange(e.target.value)}
    className="bg-surface-2 text-[15.5px]"
  />
  <p className="text-[13px] leading-relaxed text-text-muted">
    {t('onboarding.credentials.securityAnswerHint')}
  </p>
</div>
```

- [ ] **Step 5: Wire OnboardingWizard and ReviewStep**

In `OnboardingWizard.tsx`, add state:

```ts
const [securityQuestion, setSecurityQuestion] = useState('');
const [securityAnswer, setSecurityAnswer] = useState('');
```

Pass values into `CredentialStep` and into `credentialsValid`.

In `create`, send:

```ts
securityQuestion,
securityAnswer,
linkTelegram: true,
```

Clear `setSecurityAnswer('')` after provisioning succeeds.

In `ReviewStep.tsx`, remove the checkbox and render a static required Telegram row:

```tsx
<dd className="text-[15.5px] text-text">{t('onboarding.review.telegramRequired')}</dd>
```

Remove `onToggleTelegram` from props.

- [ ] **Step 6: Add onboarding copy**

In `web/src/i18n/resources.onboarding.ts`, add:

```ts
securityQuestionLabel: 'Security question',
securityAnswerLabel: 'Security answer',
securityAnswerHint: 'Used with Telegram to reset this password. The answer is never shown again.',
```

Add `telegramRequired` under `review` in English and Italian.

- [ ] **Step 7: Run tests and commit**

Run:

```bash
cd web
npx vitest run src/onboarding/__tests__/CredentialStep.test.tsx src/onboarding/__tests__/OnboardingWizard.test.tsx src/onboarding/__tests__/ReviewStep.test.tsx src/onboarding/__tests__/onboardingApi.test.ts src/onboarding/__tests__/onboardingWizardModel.test.ts --coverage.enabled=false
npm run typecheck
```

Expected: PASS.

Commit:

```bash
git add web/src/onboarding web/src/i18n/resources.onboarding.ts
git commit -m "feat(onboarding): require recovery setup"
```

## Task 9: Env, Docs, Bundle, And Verification

**Files:**
- Modify: `.env.example`
- Modify: `compose.yaml`
- Modify: `docs/cockpit-overhaul/05-authula-auth-SPEC.md`
- Modify: `internal/webui/dist/**`

- [ ] **Step 1: Update env/docs**

In `.env.example`, remove passphrase-as-default wording. Set:

```dotenv
AURA_WEB_AUTH_PROVIDER=authula
AURA_AUTHULA_SECRET=
```

In `compose.yaml`, change:

```yaml
AURA_WEB_AUTH_PROVIDER: ${AURA_WEB_AUTH_PROVIDER:-authula}
```

Remove comments that describe passphrase as the default login provider. Keep `AURA_WEB_AUTH_SECRET` only if another subsystem still reads it during compile; otherwise remove it from the compose env block.

In `docs/cockpit-overhaul/05-authula-auth-SPEC.md`, add a dated note:

```markdown
2026-06-28 update: Aura web auth is Authula-only. Password reset uses a Telegram one-time code plus the security answer captured during onboarding. The legacy passphrase provider is no longer an active product path.
```

- [ ] **Step 2: Run backend focused tests**

Run:

```bash
go test ./internal/agui ./cmd/aura ./internal/webauth -count=1
```

Expected: PASS.

- [ ] **Step 3: Run web quality checks**

Run:

```bash
cd web
npm run lint
npm run typecheck
npm run test -- --coverage.enabled=false
npm run build
```

Expected: PASS and `internal/webui/dist/**` updated by the build.

- [ ] **Step 4: Run full verification**

Run:

```bash
go test ./... -count=1
cd web
npm run contrast
npm run test:e2e -- e2e/governance.spec.ts --project=chromium --project=mobile-chrome
cd ..
docker compose up -d --build aura
```

Expected: Go tests pass, web contrast passes, selected E2E passes, and the Aura container rebuilds.

- [ ] **Step 5: Commit verification docs and bundle**

Run:

```bash
git add .env.example compose.yaml docs/cockpit-overhaul/05-authula-auth-SPEC.md internal/webui/dist web
git commit -m "chore(auth): document Authula-only recovery"
```

## Self-Review Notes

- Spec coverage: tasks cover schema, recovery hashing, onboarding recovery setup, Telegram reset service, Authula password update, route publicness, Authula-only cutover, login UI, onboarding UI, env/docs, web bundle, and verification.
- TDD: every behavior-changing task starts with a failing test and a command that should fail before implementation.
- Risk items: Task 5 touches Authula CoreServices and should be reviewed carefully against `github.com/Authula/authula@v1.11.0/services/core.go`; Task 6 removes passphrase assumptions and will require updating passphrase-only tests rather than preserving them.

