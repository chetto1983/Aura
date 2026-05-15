package identity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidInput = errors.New("identity: invalid input")
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

type Option func(*Store)

func WithNow(now func() time.Time) Option {
	return func(s *Store) {
		if now != nil {
			s.now = func() time.Time { return now().UTC() }
		}
	}
}

func NewStore(db *sql.DB, opts ...Option) (*Store, error) {
	if db == nil {
		return nil, errors.New("identity: db required")
	}
	s := &Store{
		db:  db,
		now: func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s, nil
}

func (s *Store) CreateOrResolvePrincipal(ctx context.Context, params PrincipalParams) (Principal, bool, error) {
	params.ID = strings.TrimSpace(params.ID)
	if params.ID == "" {
		return Principal{}, false, fmt.Errorf("%w: principal id required", ErrInvalidInput)
	}
	if params.Kind == "" {
		return Principal{}, false, fmt.Errorf("%w: principal kind required", ErrInvalidInput)
	}
	if params.Status == "" {
		params.Status = PrincipalStatusActive
	}
	params.MetadataJSON = defaultJSON(params.MetadataJSON)
	now := s.now().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO principals (id, kind, display_name, status, created_at, metadata_json)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING
`, params.ID, string(params.Kind), strings.TrimSpace(params.DisplayName), string(params.Status), now, params.MetadataJSON)
	if err != nil {
		return Principal{}, false, fmt.Errorf("identity: create principal: %w", err)
	}
	created, err := rowsAffected(res)
	if err != nil {
		return Principal{}, false, err
	}
	principal, err := s.getPrincipal(ctx, params.ID)
	return principal, created, err
}

func (s *Store) CreateOrResolveChannelAccount(ctx context.Context, params ChannelAccountParams) (ChannelAccount, bool, error) {
	params.Provider = strings.TrimSpace(params.Provider)
	params.ExternalID = strings.TrimSpace(params.ExternalID)
	if params.Provider == "" || params.ExternalID == "" {
		return ChannelAccount{}, false, fmt.Errorf("%w: channel provider and external id required", ErrInvalidInput)
	}
	if strings.TrimSpace(params.PrincipalID) == "" {
		return ChannelAccount{}, false, fmt.Errorf("%w: principal id required", ErrInvalidInput)
	}
	if strings.TrimSpace(params.ID) == "" {
		params.ID = mustID("acct")
	}
	params.MetadataJSON = defaultJSON(params.MetadataJSON)
	now := s.now().Format(time.RFC3339)
	lastSeen := nullableTime(params.LastSeenAt)
	res, err := s.db.ExecContext(ctx, `
INSERT INTO channel_accounts (
  id, principal_id, provider, external_id, display_name, created_at,
  last_seen_at, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider, external_id) DO NOTHING
`, params.ID, params.PrincipalID, params.Provider, params.ExternalID, strings.TrimSpace(params.DisplayName), now, lastSeen, params.MetadataJSON)
	if err != nil {
		return ChannelAccount{}, false, fmt.Errorf("identity: create channel account: %w", err)
	}
	created, err := rowsAffected(res)
	if err != nil {
		return ChannelAccount{}, false, err
	}
	account, err := s.getChannelAccount(ctx, params.Provider, params.ExternalID)
	return account, created, err
}

func (s *Store) CreateActor(ctx context.Context, params ActorParams) (Actor, error) {
	if strings.TrimSpace(params.PrincipalID) == "" {
		return Actor{}, fmt.Errorf("%w: principal id required", ErrInvalidInput)
	}
	if params.ActorType == "" {
		return Actor{}, fmt.Errorf("%w: actor type required", ErrInvalidInput)
	}
	if strings.TrimSpace(params.ID) == "" {
		params.ID = mustID("actor")
	}
	if err := s.validateActorChannelAccount(ctx, params.PrincipalID, params.ChannelAccountID); err != nil {
		return Actor{}, err
	}
	params.MetadataJSON = defaultJSON(params.MetadataJSON)
	now := s.now().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO actors (
  id, principal_id, actor_type, parent_actor_id, channel_account_id, run_id,
  created_at, expires_at, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, params.ID, params.PrincipalID, string(params.ActorType), nullString(params.ParentActorID), nullString(params.ChannelAccountID), strings.TrimSpace(params.RunID), now, nullableTime(params.ExpiresAt), params.MetadataJSON); err != nil {
		return Actor{}, fmt.Errorf("identity: create actor: %w", err)
	}
	return s.getActor(ctx, params.ID)
}

func (s *Store) CreateGrant(ctx context.Context, params GrantParams) (Grant, error) {
	if params.SubjectType != SubjectTypePrincipal && params.SubjectType != SubjectTypeActor {
		return Grant{}, fmt.Errorf("%w: subject type required", ErrInvalidInput)
	}
	if strings.TrimSpace(params.SubjectID) == "" {
		return Grant{}, fmt.Errorf("%w: subject id required", ErrInvalidInput)
	}
	if params.Capability == "" {
		return Grant{}, fmt.Errorf("%w: capability required", ErrInvalidInput)
	}
	if err := s.validateGrantSubject(ctx, params.SubjectType, params.SubjectID); err != nil {
		return Grant{}, err
	}
	if strings.TrimSpace(params.ID) == "" {
		params.ID = mustID("grant")
	}
	params.ConstraintsJSON = defaultJSON(params.ConstraintsJSON)
	now := s.now().Format(time.RFC3339)
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO capability_grants (
  id, subject_type, subject_id, capability, resource_type, resource_id,
  constraints_json, granted_by_actor_id, created_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, params.ID, string(params.SubjectType), params.SubjectID, string(params.Capability), params.Resource.Type, params.Resource.ID, params.ConstraintsJSON, nullString(params.GrantedByActorID), now, nullableTime(params.ExpiresAt)); err != nil {
		return Grant{}, fmt.Errorf("identity: create grant: %w", err)
	}
	return s.getGrant(ctx, params.ID)
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

func actorInheritsPrincipalGrants(actor Actor) bool {
	return actor.ActorType == ActorTypeSession && strings.TrimSpace(actor.ParentActorID) == ""
}

func (s *Store) validateActorChannelAccount(ctx context.Context, principalID, channelAccountID string) error {
	principalID = strings.TrimSpace(principalID)
	channelAccountID = strings.TrimSpace(channelAccountID)
	if channelAccountID == "" {
		return nil
	}
	var accountPrincipalID string
	if err := s.db.QueryRowContext(ctx, `
SELECT principal_id
FROM channel_accounts
WHERE id = ?
`, channelAccountID).Scan(&accountPrincipalID); err != nil {
		return fmt.Errorf("identity: get actor channel account: %w", err)
	}
	if accountPrincipalID != principalID {
		return fmt.Errorf("%w: channel account belongs to different principal", ErrInvalidInput)
	}
	return nil
}

func (s *Store) validateGrantSubject(ctx context.Context, subjectType SubjectType, subjectID string) error {
	subjectID = strings.TrimSpace(subjectID)
	switch subjectType {
	case SubjectTypePrincipal:
		if _, err := s.getPrincipal(ctx, subjectID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: principal grant subject not found", ErrInvalidInput)
			}
			return err
		}
	case SubjectTypeActor:
		if _, err := s.getActor(ctx, subjectID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: actor grant subject not found", ErrInvalidInput)
			}
			return err
		}
	default:
		return fmt.Errorf("%w: subject type required", ErrInvalidInput)
	}
	return nil
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
	return decision, nil
}

func (s *Store) getPrincipal(ctx context.Context, id string) (Principal, error) {
	var p Principal
	var createdAt string
	if err := s.db.QueryRowContext(ctx, `
SELECT id, kind, display_name, status, created_at, metadata_json
FROM principals
WHERE id = ?
`, id).Scan(&p.ID, (*string)(&p.Kind), &p.DisplayName, (*string)(&p.Status), &createdAt, &p.MetadataJSON); err != nil {
		return Principal{}, fmt.Errorf("identity: get principal: %w", err)
	}
	created, err := parseTime(createdAt)
	if err != nil {
		return Principal{}, fmt.Errorf("identity: parse principal created_at: %w", err)
	}
	p.CreatedAt = created
	return p, nil
}

func (s *Store) getChannelAccount(ctx context.Context, provider, externalID string) (ChannelAccount, error) {
	var a ChannelAccount
	var createdAt string
	var lastSeen sql.NullString
	if err := s.db.QueryRowContext(ctx, `
SELECT id, principal_id, provider, external_id, display_name, created_at, last_seen_at, metadata_json
FROM channel_accounts
WHERE provider = ? AND external_id = ?
`, provider, externalID).Scan(&a.ID, &a.PrincipalID, &a.Provider, &a.ExternalID, &a.DisplayName, &createdAt, &lastSeen, &a.MetadataJSON); err != nil {
		return ChannelAccount{}, fmt.Errorf("identity: get channel account: %w", err)
	}
	created, err := parseTime(createdAt)
	if err != nil {
		return ChannelAccount{}, fmt.Errorf("identity: parse channel account created_at: %w", err)
	}
	a.CreatedAt = created
	if lastSeen.Valid && lastSeen.String != "" {
		ts, err := parseTime(lastSeen.String)
		if err != nil {
			return ChannelAccount{}, fmt.Errorf("identity: parse channel account last_seen_at: %w", err)
		}
		a.LastSeenAt = &ts
	}
	return a, nil
}

func (s *Store) getActor(ctx context.Context, id string) (Actor, error) {
	var a Actor
	var parent, account, expires sql.NullString
	var createdAt string
	if err := s.db.QueryRowContext(ctx, `
SELECT id, principal_id, actor_type, parent_actor_id, channel_account_id, run_id,
       created_at, expires_at, metadata_json
FROM actors
WHERE id = ?
`, id).Scan(&a.ID, &a.PrincipalID, (*string)(&a.ActorType), &parent, &account, &a.RunID, &createdAt, &expires, &a.MetadataJSON); err != nil {
		return Actor{}, fmt.Errorf("identity: get actor: %w", err)
	}
	created, err := parseTime(createdAt)
	if err != nil {
		return Actor{}, fmt.Errorf("identity: parse actor created_at: %w", err)
	}
	a.CreatedAt = created
	if parent.Valid {
		a.ParentActorID = parent.String
	}
	if account.Valid {
		a.ChannelAccountID = account.String
	}
	if expires.Valid && expires.String != "" {
		ts, err := parseTime(expires.String)
		if err != nil {
			return Actor{}, fmt.Errorf("identity: parse actor expires_at: %w", err)
		}
		a.ExpiresAt = &ts
	}
	return a, nil
}

func (s *Store) getGrant(ctx context.Context, id string) (Grant, error) {
	var g Grant
	var grantedBy, expires, revoked sql.NullString
	var createdAt string
	if err := s.db.QueryRowContext(ctx, `
SELECT id, subject_type, subject_id, capability, resource_type, resource_id,
       constraints_json, granted_by_actor_id, created_at, expires_at, revoked_at
FROM capability_grants
WHERE id = ?
`, id).Scan(&g.ID, (*string)(&g.SubjectType), &g.SubjectID, (*string)(&g.Capability), &g.Resource.Type, &g.Resource.ID, &g.ConstraintsJSON, &grantedBy, &createdAt, &expires, &revoked); err != nil {
		return Grant{}, fmt.Errorf("identity: get grant: %w", err)
	}
	created, err := parseTime(createdAt)
	if err != nil {
		return Grant{}, fmt.Errorf("identity: parse grant created_at: %w", err)
	}
	g.CreatedAt = created
	if grantedBy.Valid {
		g.GrantedByActorID = grantedBy.String
	}
	if expires.Valid && expires.String != "" {
		ts, err := parseTime(expires.String)
		if err != nil {
			return Grant{}, fmt.Errorf("identity: parse grant expires_at: %w", err)
		}
		g.ExpiresAt = &ts
	}
	if revoked.Valid && revoked.String != "" {
		ts, err := parseTime(revoked.String)
		if err != nil {
			return Grant{}, fmt.Errorf("identity: parse grant revoked_at: %w", err)
		}
		g.RevokedAt = &ts
	}
	return g, nil
}

func rowsAffected(res sql.Result) (bool, error) {
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("identity: rows affected: %w", err)
	}
	return n > 0, nil
}

func nullString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}

func cleanResource(resource ResourceRef) ResourceRef {
	return ResourceRef{
		Type: strings.TrimSpace(resource.Type),
		ID:   strings.TrimSpace(resource.ID),
	}
}

func defaultJSON(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "{}"
	}
	return value
}

func parseTime(value string) (time.Time, error) {
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts, nil
	}
	return time.Parse(time.RFC3339Nano, value)
}

func mustID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("identity: random id: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
