package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// store_auth.go holds the authorization-shaped methods on *Store:
// delegation, allowlist backfill, grant revocation, and the Authorize
// decision path with its private helpers. Pure CRUD (Create*/Get*/List*)
// stays in store.go.

func (s *Store) DelegateActor(ctx context.Context, params DelegateActorParams) (DelegateActorResult, error) {
	params.ParentActorID = strings.TrimSpace(params.ParentActorID)
	if params.ParentActorID == "" {
		return DelegateActorResult{}, fmt.Errorf("%w: parent actor id required", ErrInvalidInput)
	}
	if params.ActorType == "" {
		return DelegateActorResult{}, fmt.Errorf("%w: delegated actor type required", ErrInvalidInput)
	}
	capabilities := uniqueNonEmptyCapabilities(params.Capabilities)
	if len(capabilities) == 0 {
		return DelegateActorResult{}, fmt.Errorf("%w: delegated capabilities required", ErrInvalidInput)
	}
	parent, err := s.getActor(ctx, params.ParentActorID)
	if err != nil {
		return DelegateActorResult{}, fmt.Errorf("identity: get parent actor: %w", err)
	}

	result := DelegateActorResult{Decisions: make([]AuthorizationDecision, 0, len(capabilities))}
	for _, capability := range capabilities {
		decision, err := s.Authorize(ctx, AuthorizeParams{
			ActorID:    parent.ID,
			Capability: capability,
			Resource:   cleanResource(params.Resource),
			RunID:      strings.TrimSpace(params.RunID),
		})
		result.Decisions = append(result.Decisions, decision)
		if err != nil {
			return result, err
		}
		if decision.Decision != DecisionAllow {
			return result, fmt.Errorf("%w: parent actor %q lacks capability %q", ErrUnauthorized, parent.ID, capability)
		}
	}

	if strings.TrimSpace(params.ID) == "" {
		params.ID = DelegatedActorID(params.ActorType, parent.ID, params.RunID)
	}
	actor, actorCreated, err := s.CreateOrResolveActor(ctx, ActorParams{
		ID:            params.ID,
		PrincipalID:   parent.PrincipalID,
		ActorType:     params.ActorType,
		ParentActorID: parent.ID,
		RunID:         strings.TrimSpace(params.RunID),
		ExpiresAt:     params.ExpiresAt,
		MetadataJSON:  params.MetadataJSON,
	})
	if err != nil {
		return result, err
	}
	result.Actor = actor
	result.ActorCreated = actorCreated

	for _, capability := range capabilities {
		_, created, err := s.CreateOrResolveGrant(ctx, GrantParams{
			ID:               DelegatedActorGrantID(actor.ID, capability, params.Resource),
			SubjectType:      SubjectTypeActor,
			SubjectID:        actor.ID,
			Capability:       capability,
			Resource:         cleanResource(params.Resource),
			ConstraintsJSON:  params.ConstraintsJSON,
			GrantedByActorID: parent.ID,
			ExpiresAt:        params.ExpiresAt,
		})
		if err != nil {
			return result, err
		}
		if created {
			result.GrantsCreated++
		}
	}
	return result, nil
}

func (s *Store) BackfillTelegramAllowlistedUser(ctx context.Context, params TelegramAllowlistUserParams) (TelegramAllowlistBackfillResult, error) {
	userID := strings.TrimSpace(params.UserID)
	if userID == "" {
		return TelegramAllowlistBackfillResult{}, fmt.Errorf("%w: telegram user id required", ErrInvalidInput)
	}
	if params.PrincipalKind == "" {
		params.PrincipalKind = PrincipalKindHuman
	}
	displayName := strings.TrimSpace(params.DisplayName)
	if displayName == "" {
		displayName = "Telegram " + userID
	}
	metadata := telegramAllowlistMetadata(params.Source)
	principal, principalCreated, err := s.CreateOrResolvePrincipal(ctx, PrincipalParams{
		ID:           TelegramPrincipalID(userID),
		Kind:         params.PrincipalKind,
		DisplayName:  displayName,
		Status:       PrincipalStatusActive,
		MetadataJSON: metadata,
	})
	if err != nil {
		return TelegramAllowlistBackfillResult{}, err
	}
	account, accountCreated, err := s.CreateOrResolveChannelAccount(ctx, ChannelAccountParams{
		ID:           TelegramChannelAccountID(userID),
		PrincipalID:  principal.ID,
		Provider:     "telegram",
		ExternalID:   userID,
		DisplayName:  displayName,
		MetadataJSON: metadata,
	})
	if err != nil {
		return TelegramAllowlistBackfillResult{}, err
	}
	_, actorCreated, err := s.CreateOrResolveActor(ctx, ActorParams{
		ID:               TelegramSessionActorID(userID),
		PrincipalID:      principal.ID,
		ActorType:        ActorTypeSession,
		ChannelAccountID: account.ID,
		MetadataJSON:     metadata,
	})
	if err != nil {
		return TelegramAllowlistBackfillResult{}, err
	}
	result := TelegramAllowlistBackfillResult{
		PrincipalCreated:      principalCreated,
		ChannelAccountCreated: accountCreated,
		ActorCreated:          actorCreated,
	}
	for _, capability := range uniqueCapabilities(params.Capabilities) {
		_, created, err := s.CreateOrResolveGrant(ctx, GrantParams{
			ID:              TelegramPrincipalGrantID(userID, capability),
			SubjectType:     SubjectTypePrincipal,
			SubjectID:       principal.ID,
			Capability:      capability,
			ConstraintsJSON: metadata,
		})
		if err != nil {
			return TelegramAllowlistBackfillResult{}, err
		}
		if created {
			result.GrantsCreated++
		}
	}
	return result, nil
}

func (s *Store) RevokeGrant(ctx context.Context, grantID string) error {
	grantID = strings.TrimSpace(grantID)
	if grantID == "" {
		return fmt.Errorf("%w: grant id required", ErrInvalidInput)
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE capability_grants
SET revoked_at = ?
WHERE id = ? AND (revoked_at IS NULL OR revoked_at = '')
`, s.now().Format(time.RFC3339), grantID)
	if err != nil {
		return fmt.Errorf("identity: revoke grant: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("identity: revoke grant rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) Authorize(ctx context.Context, params AuthorizeParams) (AuthorizationDecision, error) {
	params.ActorID = strings.TrimSpace(params.ActorID)
	params.RunID = firstNonEmptyString(params.RunID, RunIDFromContext(ctx))
	params.EventID = firstNonEmptyString(params.EventID, EventIDFromContext(ctx))
	if params.ActorID == "" {
		return AuthorizationDecision{
			Decision:   DecisionDeny,
			Capability: params.Capability,
			Resource:   cleanResource(params.Resource),
			Reason:     "missing_actor",
			CreatedAt:  s.now(),
		}, nil
	}
	if params.Capability == "" {
		return AuthorizationDecision{}, fmt.Errorf("%w: capability required", ErrInvalidInput)
	}

	actor, err := s.getActor(ctx, params.ActorID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AuthorizationDecision{
				ActorID:    params.ActorID,
				Capability: params.Capability,
				Resource:   cleanResource(params.Resource),
				Decision:   DecisionDeny,
				Reason:     "unknown_actor",
				CreatedAt:  s.now(),
			}, nil
		}
		return AuthorizationDecision{}, err
	}

	principal, err := s.getPrincipal(ctx, actor.PrincipalID)
	if err != nil {
		return AuthorizationDecision{}, err
	}

	reason := "missing_grant"
	decision := DecisionDeny
	if principal.Status != PrincipalStatusActive {
		reason = "principal_disabled"
	} else if actor.ExpiresAt != nil && !s.now().Before(*actor.ExpiresAt) {
		reason = "actor_expired"
	} else {
		allowed, err := s.hasActiveGrant(ctx, actor, params.Capability, cleanResource(params.Resource))
		if err != nil {
			return AuthorizationDecision{}, err
		}
		if allowed {
			decision = DecisionAllow
			reason = "active_grant"
		}
	}
	return s.recordDecision(ctx, AuthorizationDecision{
		ID:         mustID("authz"),
		ActorID:    actor.ID,
		Capability: params.Capability,
		Resource:   cleanResource(params.Resource),
		Decision:   decision,
		Reason:     reason,
		RunID:      strings.TrimSpace(params.RunID),
		EventID:    strings.TrimSpace(params.EventID),
		CreatedAt:  s.now(),
	})
}

func (s *Store) recordDecision(ctx context.Context, decision AuthorizationDecision) (AuthorizationDecision, error) {
	if decision.ID == "" {
		decision.ID = mustID("authz")
	}
	if decision.CreatedAt.IsZero() {
		decision.CreatedAt = s.now()
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO authz_decisions (
  id, actor_id, capability, resource_type, resource_id, decision, reason,
  run_id, event_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, decision.ID, decision.ActorID, string(decision.Capability), decision.Resource.Type, decision.Resource.ID, string(decision.Decision), decision.Reason, decision.RunID, decision.EventID, decision.CreatedAt.UTC().Format(time.RFC3339)); err != nil {
		return AuthorizationDecision{}, fmt.Errorf("identity: record authz decision: %w", err)
	}
	if decision.Decision == DecisionDeny {
		if recorder, ok := AuthorizationDenialRecorderFromContext(ctx); ok {
			if err := recorder.RecordAuthorizationDenial(ctx, decision); err != nil {
				return decision, fmt.Errorf("identity: record authorization denial event: %w", err)
			}
		}
	}
	return decision, nil
}

func (s *Store) hasActiveGrant(ctx context.Context, actor Actor, capability Capability, resource ResourceRef) (bool, error) {
	allowed, err := s.hasActiveGrantForSubject(ctx, SubjectTypeActor, actor.ID, capability, resource)
	if err != nil || allowed {
		return allowed, err
	}
	if !actorInheritsPrincipalGrants(actor) {
		return false, nil
	}
	return s.hasActiveGrantForSubject(ctx, SubjectTypePrincipal, actor.PrincipalID, capability, resource)
}

func (s *Store) hasActiveGrantForSubject(ctx context.Context, subjectType SubjectType, subjectID string, capability Capability, resource ResourceRef) (bool, error) {
	now := s.now().Format(time.RFC3339)
	var id string
	err := s.db.QueryRowContext(ctx, `
SELECT id
FROM capability_grants
WHERE capability = ?
  AND (revoked_at IS NULL OR revoked_at = '')
  AND (expires_at IS NULL OR expires_at = '' OR expires_at > ?)
  AND subject_type = ?
  AND subject_id = ?
  AND (
    (resource_type = '' AND resource_id = '')
    OR (resource_type = ? AND resource_id = '')
    OR (resource_type = ? AND resource_id = ?)
  )
LIMIT 1
`, string(capability), now, string(subjectType), subjectID, resource.Type, resource.Type, resource.ID).Scan(&id)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, fmt.Errorf("identity: authorize grant lookup: %w", err)
}
