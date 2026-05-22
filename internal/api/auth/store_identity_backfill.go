package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/identity"
)

func (s *Store) BackfillAllowedUserIdentities(ctx context.Context) (identity.TelegramAllowlistBackfillSummary, error) {
	var summary identity.TelegramAllowlistBackfillSummary
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, source
		FROM allowed_users
		WHERE source <> ?
		ORDER BY created_at
	`, SourceE2EBootstrap)
	if err != nil {
		return summary, fmt.Errorf("auth identity backfill: list allowed users: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var userID, source string
		if err := rows.Scan(&userID, &source); err != nil {
			return summary, fmt.Errorf("auth identity backfill: scan allowed user: %w", err)
		}
		result, err := s.backfillAllowedUserIdentityResult(ctx, userID, source)
		if err != nil {
			return summary, err
		}
		summary.Users++
		if result.PrincipalCreated {
			summary.PrincipalsCreated++
		}
		if result.ChannelAccountCreated {
			summary.ChannelAccountsCreated++
		}
		if result.ActorCreated {
			summary.ActorsCreated++
		}
		summary.GrantsCreated += result.GrantsCreated
	}
	if err := rows.Err(); err != nil {
		return summary, fmt.Errorf("auth identity backfill: iterate allowed users: %w", err)
	}
	return summary, nil
}

// EnsureTelegramAllowlistedIdentity creates the deterministic Telegram
// principal/channel-account/session-actor/grants needed before token issuance.
// When source is blank, the source is derived from the persisted allowed_users
// row. Configured-allowlist callers pass SourceTelegramConfiguredAllowlist so
// settings remain config and do not become an allowed_users row.
func (s *Store) EnsureTelegramAllowlistedIdentity(ctx context.Context, userID, source string) (identity.TelegramAllowlistBackfillResult, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return identity.TelegramAllowlistBackfillResult{}, errors.New("auth: user id required")
	}
	source = strings.TrimSpace(source)
	if source == "" {
		var err error
		source, err = s.allowedUserSource(ctx, userID)
		if err != nil {
			return identity.TelegramAllowlistBackfillResult{}, err
		}
	}
	return s.backfillAllowedUserIdentityResult(ctx, userID, source)
}

func (s *Store) backfillAllowedUserIdentityFromRow(ctx context.Context, userID string) error {
	source, err := s.allowedUserSource(ctx, userID)
	if err != nil {
		return err
	}
	return s.backfillAllowedUserIdentity(ctx, userID, source)
}

func (s *Store) backfillAllowedUserIdentity(ctx context.Context, userID, source string) error {
	_, err := s.backfillAllowedUserIdentityResult(ctx, userID, source)
	return err
}

func (s *Store) backfillAllowedUserIdentityResult(ctx context.Context, userID, source string) (identity.TelegramAllowlistBackfillResult, error) {
	return s.backfillAllowedUserIdentityWithStoreResult(ctx, s.identity, userID, source)
}

func (s *Store) backfillAllowedUserIdentityWithStore(ctx context.Context, identityStore *identity.Store, userID, source string) error {
	_, err := s.backfillAllowedUserIdentityWithStoreResult(ctx, identityStore, userID, source)
	return err
}

func (s *Store) backfillAllowedUserIdentityWithStoreResult(ctx context.Context, identityStore *identity.Store, userID, source string) (identity.TelegramAllowlistBackfillResult, error) {
	if identityStore == nil || strings.TrimSpace(source) == SourceE2EBootstrap {
		return identity.TelegramAllowlistBackfillResult{}, nil
	}
	result, err := identityStore.BackfillTelegramAllowlistedUser(ctx, identity.TelegramAllowlistUserParams{
		UserID:        userID,
		Source:        source,
		PrincipalKind: principalKindForAllowedUserSource(source),
		Capabilities:  capabilitiesForAllowedUserSource(source),
	})
	if err != nil {
		return identity.TelegramAllowlistBackfillResult{}, fmt.Errorf("auth identity backfill: %w", err)
	}
	return result, nil
}

func (s *Store) allowedUserSource(ctx context.Context, userID string) (string, error) {
	var source string
	if err := s.db.QueryRowContext(ctx, `SELECT source FROM allowed_users WHERE user_id = ?`, userID).Scan(&source); err != nil {
		return "", fmt.Errorf("auth identity backfill: allowed user source: %w", err)
	}
	return source, nil
}

func principalKindForAllowedUserSource(source string) identity.PrincipalKind {
	switch strings.TrimSpace(source) {
	case SourceTelegramBootstrap, SourceTelegramConfiguredAllowlist, "manual":
		return identity.PrincipalKindOwner
	default:
		return identity.PrincipalKindHuman
	}
}

func capabilitiesForAllowedUserSource(source string) []identity.Capability {
	if principalKindForAllowedUserSource(source) == identity.PrincipalKindOwner {
		return identity.TelegramOwnerCapabilities()
	}
	return identity.TelegramUserCapabilities()
}
