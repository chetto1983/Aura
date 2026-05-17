package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidInput    = errors.New("identity: invalid input")
	ErrUnauthorized    = errors.New("identity: unauthorized")
	ErrPermissionDenied = errors.New("identity: permission denied")
)

type Store struct {
	db  sqlRunner
	now func() time.Time
}

type sqlRunner interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
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
	return newStore(db, opts...), nil
}

func NewStoreWithTx(tx *sql.Tx, opts ...Option) (*Store, error) {
	if tx == nil {
		return nil, errors.New("identity: tx required")
	}
	return newStore(tx, opts...), nil
}

func newStore(db sqlRunner, opts ...Option) *Store {
	s := &Store{
		db:  db,
		now: func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
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
	actor, _, err := s.createOrResolveActor(ctx, params, false)
	return actor, err
}

func (s *Store) CreateOrResolveActor(ctx context.Context, params ActorParams) (Actor, bool, error) {
	return s.createOrResolveActor(ctx, params, true)
}

func (s *Store) createOrResolveActor(ctx context.Context, params ActorParams, resolve bool) (Actor, bool, error) {
	if strings.TrimSpace(params.PrincipalID) == "" {
		return Actor{}, false, fmt.Errorf("%w: principal id required", ErrInvalidInput)
	}
	if params.ActorType == "" {
		return Actor{}, false, fmt.Errorf("%w: actor type required", ErrInvalidInput)
	}
	if strings.TrimSpace(params.ID) == "" {
		params.ID = mustID("actor")
	}
	if err := s.validateActorChannelAccount(ctx, params.PrincipalID, params.ChannelAccountID); err != nil {
		return Actor{}, false, err
	}
	params.MetadataJSON = defaultJSON(params.MetadataJSON)
	now := s.now().Format(time.RFC3339)
	stmt := `
INSERT INTO actors (
  id, principal_id, actor_type, parent_actor_id, channel_account_id, run_id,
  created_at, expires_at, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	if resolve {
		stmt += `ON CONFLICT(id) DO NOTHING
`
	}
	res, err := s.db.ExecContext(ctx, stmt, params.ID, params.PrincipalID, string(params.ActorType), nullString(params.ParentActorID), nullString(params.ChannelAccountID), strings.TrimSpace(params.RunID), now, nullableTime(params.ExpiresAt), params.MetadataJSON)
	if err != nil {
		return Actor{}, false, fmt.Errorf("identity: create actor: %w", err)
	}
	created, err := rowsAffected(res)
	if err != nil {
		return Actor{}, false, err
	}
	actor, err := s.getActor(ctx, params.ID)
	if err != nil {
		return Actor{}, false, err
	}
	if !created && resolve {
		if err := validateResolvedActor(actor, params); err != nil {
			return Actor{}, false, err
		}
	}
	return actor, created, nil
}

func (s *Store) CreateGrant(ctx context.Context, params GrantParams) (Grant, error) {
	grant, _, err := s.createOrResolveGrant(ctx, params, false)
	return grant, err
}

func (s *Store) CreateOrResolveGrant(ctx context.Context, params GrantParams) (Grant, bool, error) {
	return s.createOrResolveGrant(ctx, params, true)
}

func (s *Store) createOrResolveGrant(ctx context.Context, params GrantParams, resolve bool) (Grant, bool, error) {
	if params.SubjectType != SubjectTypePrincipal && params.SubjectType != SubjectTypeActor {
		return Grant{}, false, fmt.Errorf("%w: subject type required", ErrInvalidInput)
	}
	if strings.TrimSpace(params.SubjectID) == "" {
		return Grant{}, false, fmt.Errorf("%w: subject id required", ErrInvalidInput)
	}
	if params.Capability == "" {
		return Grant{}, false, fmt.Errorf("%w: capability required", ErrInvalidInput)
	}
	if err := s.validateGrantSubject(ctx, params.SubjectType, params.SubjectID); err != nil {
		return Grant{}, false, err
	}
	if strings.TrimSpace(params.ID) == "" {
		params.ID = mustID("grant")
	}
	params.ConstraintsJSON = defaultJSON(params.ConstraintsJSON)
	now := s.now().Format(time.RFC3339)
	stmt := `
INSERT INTO capability_grants (
  id, subject_type, subject_id, capability, resource_type, resource_id,
  constraints_json, granted_by_actor_id, created_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
	if resolve {
		stmt += `ON CONFLICT(id) DO NOTHING
`
	}
	res, err := s.db.ExecContext(ctx, stmt, params.ID, string(params.SubjectType), params.SubjectID, string(params.Capability), params.Resource.Type, params.Resource.ID, params.ConstraintsJSON, nullString(params.GrantedByActorID), now, nullableTime(params.ExpiresAt))
	if err != nil {
		return Grant{}, false, fmt.Errorf("identity: create grant: %w", err)
	}
	created, err := rowsAffected(res)
	if err != nil {
		return Grant{}, false, err
	}
	grant, err := s.getGrant(ctx, params.ID)
	return grant, created, err
}

// Authorization-shaped methods (DelegateActor, BackfillTelegramAllowlistedUser,
// RevokeGrant, Authorize, recordDecision, hasActiveGrant,
// hasActiveGrantForSubject) live in store_auth.go.
