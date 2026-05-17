package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

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

func validateResolvedActor(actor Actor, params ActorParams) error {
	if actor.PrincipalID != strings.TrimSpace(params.PrincipalID) {
		return fmt.Errorf("%w: resolved actor belongs to different principal", ErrInvalidInput)
	}
	if actor.ActorType != params.ActorType {
		return fmt.Errorf("%w: resolved actor has different type", ErrInvalidInput)
	}
	if strings.TrimSpace(params.ChannelAccountID) != "" && actor.ChannelAccountID != strings.TrimSpace(params.ChannelAccountID) {
		return fmt.Errorf("%w: resolved actor has different channel account", ErrInvalidInput)
	}
	return nil
}

func TelegramPrincipalID(userID string) string {
	return "principal:telegram:" + strings.TrimSpace(userID)
}

func TelegramChannelAccountID(userID string) string {
	return "acct:telegram:" + strings.TrimSpace(userID)
}

func TelegramSessionActorID(userID string) string {
	return "actor:telegram:session:" + strings.TrimSpace(userID)
}

func TelegramPrincipalGrantID(userID string, capability Capability) string {
	return "grant:telegram:" + strings.TrimSpace(userID) + ":" + string(capability)
}

func DelegatedActorID(actorType ActorType, parentActorID, scopeID string) string {
	return "actor:" + string(actorType) + ":delegated:" + stableID(parentActorID, string(actorType), scopeID)
}

func DelegatedActorGrantID(actorID string, capability Capability, resource ResourceRef) string {
	resource = cleanResource(resource)
	return "grant:delegated:" + stableID(actorID, string(capability), resource.Type, resource.ID)
}

func TelegramUserCapabilities() []Capability {
	return []Capability{
		CapabilityAPIChat,
		CapabilityDashboardRead,
		CapabilityDashboardWrite,
		CapabilityToolExecute,
		CapabilityMemoryUserWrite,
		CapabilityWikiWrite,
	}
}

func TelegramOwnerCapabilities() []Capability {
	return []Capability{
		CapabilityAPIChat,
		CapabilityDashboardRead,
		CapabilityDashboardWrite,
		CapabilityToolExecute,
		CapabilityMemoryUserWrite,
		CapabilityWikiWrite,
		CapabilitySkillsInstall,
		CapabilitySkillsDelete,
		CapabilitySettingsWrite,
		CapabilityCronCreate,
		CapabilityCronRun,
		CapabilitySwarmSpawn,
	}
}

func uniqueCapabilities(capabilities []Capability) []Capability {
	if len(capabilities) == 0 {
		return TelegramUserCapabilities()
	}
	seen := make(map[Capability]struct{}, len(capabilities))
	out := make([]Capability, 0, len(capabilities))
	for _, capability := range capabilities {
		if capability == "" {
			continue
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		out = append(out, capability)
	}
	return out
}

func uniqueNonEmptyCapabilities(capabilities []Capability) []Capability {
	seen := make(map[Capability]struct{}, len(capabilities))
	out := make([]Capability, 0, len(capabilities))
	for _, capability := range capabilities {
		if capability == "" {
			continue
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		out = append(out, capability)
	}
	return out
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func stableID(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(strings.TrimSpace(part)))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}

func telegramAllowlistMetadata(source string) string {
	payload := map[string]string{
		"compat_layer": "allowed_users",
		"provider":     "telegram",
	}
	source = strings.TrimSpace(source)
	if source != "" {
		payload["source"] = source
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(encoded)
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
