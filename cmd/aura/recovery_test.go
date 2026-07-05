package main

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/agui"
)

// TestBreakGlassTokenTTLIsShortLived pins the security property (D-16): the
// host-minted bearer token has a short window and reuses the online mint's exact
// 10-minute lifetime.
func TestBreakGlassTokenTTLIsShortLived(t *testing.T) {
	if breakGlassTokenTTL <= 0 || breakGlassTokenTTL > time.Hour {
		t.Fatalf("breakGlassTokenTTL = %s, want a short-lived (0, 1h] window", breakGlassTokenTTL)
	}
	if breakGlassTokenTTL != 10*time.Minute {
		t.Fatalf("breakGlassTokenTTL = %s, want 10m to match the online reset-token mint", breakGlassTokenTTL)
	}
}

// TestBreakGlassChallengeParams asserts the parent challenge reuses the shipped
// challenge cap, omits any Telegram binding (host-minted, not delivered), and
// threads the code hash + short expiry through.
func TestBreakGlassChallengeParams(t *testing.T) {
	identityID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	codeHash := agui.HashLookupToken("throwaway-challenge-secret")

	got := breakGlassChallengeParams(identityID, codeHash, now)

	if got.MaxAttempts != passwordResetChallengeMaxAttempts {
		t.Fatalf("MaxAttempts = %d, want %d (shipped challenge cap)", got.MaxAttempts, passwordResetChallengeMaxAttempts)
	}
	if got.TelegramUserID.Valid {
		t.Fatalf("TelegramUserID = %+v, want absent for a host-minted challenge", got.TelegramUserID)
	}
	if got.CodeHash != codeHash {
		t.Fatalf("CodeHash = %q, want the threaded hash %q", got.CodeHash, codeHash)
	}
	if !got.ExpiresAt.Valid || !got.ExpiresAt.Time.Equal(now.Add(breakGlassTokenTTL)) {
		t.Fatalf("ExpiresAt = %+v, want %s", got.ExpiresAt, now.Add(breakGlassTokenTTL))
	}
	if got.IdentityID != identityID {
		t.Fatalf("IdentityID = %+v, want %+v", got.IdentityID, identityID)
	}
}

// TestBreakGlassTokenParams asserts the token insert reuses the online mint's
// exact max-attempts + expiry shape and stores the hashed token, not plaintext.
func TestBreakGlassTokenParams(t *testing.T) {
	challengeID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	identityID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	tokenHash := agui.HashLookupToken("raw-break-glass-token")

	got := breakGlassTokenParams(tokenHash, challengeID, identityID, now)

	if got.MaxAttempts != passwordResetTokenMaxAttempts {
		t.Fatalf("MaxAttempts = %d, want %d (online token cap)", got.MaxAttempts, passwordResetTokenMaxAttempts)
	}
	if got.TokenHash != tokenHash {
		t.Fatalf("TokenHash = %q, want the sha256 hash %q (never plaintext)", got.TokenHash, tokenHash)
	}
	if got.TokenHash == "raw-break-glass-token" {
		t.Fatal("TokenHash stored the plaintext token")
	}
	if got.ChallengeID != challengeID {
		t.Fatalf("ChallengeID = %+v, want %+v", got.ChallengeID, challengeID)
	}
	if got.IdentityID != identityID {
		t.Fatalf("IdentityID = %+v, want %+v", got.IdentityID, identityID)
	}
	if !got.ExpiresAt.Valid || !got.ExpiresAt.Time.Equal(now.Add(breakGlassTokenTTL)) {
		t.Fatalf("ExpiresAt = %+v, want %s", got.ExpiresAt, now.Add(breakGlassTokenTTL))
	}
}
