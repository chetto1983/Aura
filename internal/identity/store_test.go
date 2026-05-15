package identity

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
)

func openIdentityStore(t *testing.T, now time.Time) (*Store, *sql.DB) {
	t.Helper()
	db, err := auradb.Open(filepath.Join(t.TempDir(), "identity.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store, err := NewStore(db, WithNow(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store, db
}

func seedActor(t *testing.T, store *Store) Actor {
	t.Helper()
	ctx := context.Background()
	if _, _, err := store.CreateOrResolvePrincipal(ctx, PrincipalParams{
		ID:          "principal-1",
		Kind:        PrincipalKindHuman,
		DisplayName: "Owner",
	}); err != nil {
		t.Fatalf("CreateOrResolvePrincipal: %v", err)
	}
	account, _, err := store.CreateOrResolveChannelAccount(ctx, ChannelAccountParams{
		ID:          "acct-1",
		PrincipalID: "principal-1",
		Provider:    "telegram",
		ExternalID:  "123",
		DisplayName: "Owner TG",
	})
	if err != nil {
		t.Fatalf("CreateOrResolveChannelAccount: %v", err)
	}
	actor, err := store.CreateActor(ctx, ActorParams{
		ID:               "actor-1",
		PrincipalID:      "principal-1",
		ActorType:        ActorTypeSession,
		ChannelAccountID: account.ID,
		RunID:            "run-1",
	})
	if err != nil {
		t.Fatalf("CreateActor: %v", err)
	}
	return actor
}

func TestBackfillTelegramAllowlistedUserCreatesIdentityRecordsAndGrants(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, db := openIdentityStore(t, now)
	ctx := context.Background()

	result, err := store.BackfillTelegramAllowlistedUser(ctx, TelegramAllowlistUserParams{
		UserID:        "12345",
		Source:        "telegram_bootstrap",
		PrincipalKind: PrincipalKindOwner,
		Capabilities:  TelegramOwnerCapabilities(),
	})
	if err != nil {
		t.Fatalf("BackfillTelegramAllowlistedUser: %v", err)
	}
	if !result.PrincipalCreated || !result.ChannelAccountCreated || !result.ActorCreated {
		t.Fatalf("created result = %+v, want principal/account/actor created", result)
	}
	if result.GrantsCreated != len(TelegramOwnerCapabilities()) {
		t.Fatalf("GrantsCreated = %d, want %d", result.GrantsCreated, len(TelegramOwnerCapabilities()))
	}

	assertIdentityScalar(t, db, `SELECT kind FROM principals WHERE id = ?`, "owner", TelegramPrincipalID("12345"))
	assertIdentityScalar(t, db, `SELECT principal_id FROM channel_accounts WHERE id = ?`, TelegramPrincipalID("12345"), TelegramChannelAccountID("12345"))
	assertIdentityScalar(t, db, `SELECT actor_type FROM actors WHERE id = ?`, "session", TelegramSessionActorID("12345"))

	decision, err := store.Authorize(ctx, AuthorizeParams{
		ActorID:    TelegramSessionActorID("12345"),
		Capability: CapabilitySkillsInstall,
	})
	if err != nil {
		t.Fatalf("Authorize owner grant: %v", err)
	}
	if decision.Decision != DecisionAllow {
		t.Fatalf("owner skills.install decision = %s, want allow", decision.Decision)
	}

	again, err := store.BackfillTelegramAllowlistedUser(ctx, TelegramAllowlistUserParams{
		UserID:        "12345",
		Source:        "telegram_bootstrap",
		PrincipalKind: PrincipalKindOwner,
		Capabilities:  TelegramOwnerCapabilities(),
	})
	if err != nil {
		t.Fatalf("BackfillTelegramAllowlistedUser again: %v", err)
	}
	if again.PrincipalCreated || again.ChannelAccountCreated || again.ActorCreated || again.GrantsCreated != 0 {
		t.Fatalf("second backfill result = %+v, want no new rows", again)
	}
}

func TestBackfillTelegramAllowlistedUserSeedsUserCapabilitiesOnly(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, _ := openIdentityStore(t, now)
	ctx := context.Background()

	if _, err := store.BackfillTelegramAllowlistedUser(ctx, TelegramAllowlistUserParams{
		UserID:        "67890",
		Source:        "dashboard_approve",
		PrincipalKind: PrincipalKindHuman,
		Capabilities:  TelegramUserCapabilities(),
	}); err != nil {
		t.Fatalf("BackfillTelegramAllowlistedUser: %v", err)
	}

	allowed, err := store.Authorize(ctx, AuthorizeParams{
		ActorID:    TelegramSessionActorID("67890"),
		Capability: CapabilityAPIChat,
	})
	if err != nil {
		t.Fatalf("Authorize api.chat: %v", err)
	}
	if allowed.Decision != DecisionAllow {
		t.Fatalf("api.chat decision = %s, want allow", allowed.Decision)
	}

	denied, err := store.Authorize(ctx, AuthorizeParams{
		ActorID:    TelegramSessionActorID("67890"),
		Capability: CapabilitySkillsInstall,
	})
	if err != nil {
		t.Fatalf("Authorize skills.install: %v", err)
	}
	if denied.Decision != DecisionDeny {
		t.Fatalf("skills.install decision = %s, want deny", denied.Decision)
	}
}

func TestAuthorize_DefaultDenyRecordsDecision(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, db := openIdentityStore(t, now)
	actor := seedActor(t, store)

	decision, err := store.Authorize(context.Background(), AuthorizeParams{
		ActorID:    actor.ID,
		Capability: CapabilityAPIChat,
		Resource:   ResourceRef{Type: "thread", ID: "thread-1"},
		RunID:      "run-1",
		EventID:    "event-1",
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if decision.Decision != DecisionDeny {
		t.Fatalf("Decision = %s, want deny", decision.Decision)
	}
	if decision.Reason != "missing_grant" {
		t.Fatalf("Reason = %s, want missing_grant", decision.Reason)
	}

	var got string
	if err := db.QueryRow(`SELECT decision FROM authz_decisions WHERE actor_id = ?`, actor.ID).Scan(&got); err != nil {
		t.Fatalf("query authz_decisions: %v", err)
	}
	if got != string(DecisionDeny) {
		t.Fatalf("stored decision = %s, want deny", got)
	}
}

func TestAuthorize_AllowsActivePrincipalGrant(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, _ := openIdentityStore(t, now)
	actor := seedActor(t, store)

	if _, err := store.CreateGrant(context.Background(), GrantParams{
		ID:          "grant-1",
		SubjectType: SubjectTypePrincipal,
		SubjectID:   actor.PrincipalID,
		Capability:  CapabilityAPIChat,
		Resource:    ResourceRef{Type: "thread", ID: "thread-1"},
	}); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	decision, err := store.Authorize(context.Background(), AuthorizeParams{
		ActorID:    actor.ID,
		Capability: CapabilityAPIChat,
		Resource:   ResourceRef{Type: "thread", ID: "thread-1"},
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if decision.Decision != DecisionAllow {
		t.Fatalf("Decision = %s, want allow", decision.Decision)
	}
	if decision.Reason != "active_grant" {
		t.Fatalf("Reason = %s, want active_grant", decision.Reason)
	}
}

func TestAuthorize_AllowsActiveActorGrant(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, _ := openIdentityStore(t, now)
	actor := seedActor(t, store)

	if _, err := store.CreateGrant(context.Background(), GrantParams{
		ID:          "grant-actor",
		SubjectType: SubjectTypeActor,
		SubjectID:   actor.ID,
		Capability:  CapabilityToolExecute,
		Resource:    ResourceRef{Type: "tool", ID: "shell"},
	}); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	decision, err := store.Authorize(context.Background(), AuthorizeParams{
		ActorID:    actor.ID,
		Capability: CapabilityToolExecute,
		Resource:   ResourceRef{Type: "tool", ID: "shell"},
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if decision.Decision != DecisionAllow {
		t.Fatalf("Decision = %s, want allow", decision.Decision)
	}
}

func TestAuthorize_DeniesRevokedAndExpiredGrants(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, _ := openIdentityStore(t, now)
	actor := seedActor(t, store)

	if _, err := store.CreateGrant(context.Background(), GrantParams{
		ID:          "grant-revoked",
		SubjectType: SubjectTypeActor,
		SubjectID:   actor.ID,
		Capability:  CapabilityAPIChat,
	}); err != nil {
		t.Fatalf("CreateGrant revoked: %v", err)
	}
	if err := store.RevokeGrant(context.Background(), "grant-revoked"); err != nil {
		t.Fatalf("RevokeGrant: %v", err)
	}

	decision, err := store.Authorize(context.Background(), AuthorizeParams{
		ActorID:    actor.ID,
		Capability: CapabilityAPIChat,
	})
	if err != nil {
		t.Fatalf("Authorize revoked: %v", err)
	}
	if decision.Decision != DecisionDeny {
		t.Fatalf("revoked decision = %s, want deny", decision.Decision)
	}

	expiredAt := now.Add(-time.Minute)
	if _, err := store.CreateGrant(context.Background(), GrantParams{
		ID:          "grant-expired",
		SubjectType: SubjectTypeActor,
		SubjectID:   actor.ID,
		Capability:  CapabilityDashboardRead,
		ExpiresAt:   &expiredAt,
	}); err != nil {
		t.Fatalf("CreateGrant expired: %v", err)
	}
	decision, err = store.Authorize(context.Background(), AuthorizeParams{
		ActorID:    actor.ID,
		Capability: CapabilityDashboardRead,
	})
	if err != nil {
		t.Fatalf("Authorize expired: %v", err)
	}
	if decision.Decision != DecisionDeny {
		t.Fatalf("expired decision = %s, want deny", decision.Decision)
	}
}

func TestAuthorize_DeniesExpiredActor(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, _ := openIdentityStore(t, now)
	ctx := context.Background()
	if _, _, err := store.CreateOrResolvePrincipal(ctx, PrincipalParams{
		ID:   "principal-expired",
		Kind: PrincipalKindHuman,
	}); err != nil {
		t.Fatalf("CreateOrResolvePrincipal: %v", err)
	}
	expiredAt := now.Add(-time.Second)
	actor, err := store.CreateActor(ctx, ActorParams{
		ID:          "actor-expired",
		PrincipalID: "principal-expired",
		ActorType:   ActorTypeSession,
		ExpiresAt:   &expiredAt,
	})
	if err != nil {
		t.Fatalf("CreateActor: %v", err)
	}
	if _, err := store.CreateGrant(ctx, GrantParams{
		ID:          "grant-expired-actor",
		SubjectType: SubjectTypeActor,
		SubjectID:   actor.ID,
		Capability:  CapabilityDashboardRead,
	}); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	decision, err := store.Authorize(ctx, AuthorizeParams{
		ActorID:    actor.ID,
		Capability: CapabilityDashboardRead,
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if decision.Decision != DecisionDeny {
		t.Fatalf("Decision = %s, want deny", decision.Decision)
	}
	if decision.Reason != "actor_expired" {
		t.Fatalf("Reason = %s, want actor_expired", decision.Reason)
	}
}

func TestAuthorize_DeniesDisabledPrincipal(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, _ := openIdentityStore(t, now)
	ctx := context.Background()
	if _, _, err := store.CreateOrResolvePrincipal(ctx, PrincipalParams{
		ID:     "principal-disabled",
		Kind:   PrincipalKindHuman,
		Status: PrincipalStatusDisabled,
	}); err != nil {
		t.Fatalf("CreateOrResolvePrincipal: %v", err)
	}
	actor, err := store.CreateActor(ctx, ActorParams{
		ID:          "actor-disabled",
		PrincipalID: "principal-disabled",
		ActorType:   ActorTypeSession,
	})
	if err != nil {
		t.Fatalf("CreateActor: %v", err)
	}
	if _, err := store.CreateGrant(ctx, GrantParams{
		ID:          "grant-disabled-principal",
		SubjectType: SubjectTypePrincipal,
		SubjectID:   actor.PrincipalID,
		Capability:  CapabilityDashboardRead,
	}); err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}

	decision, err := store.Authorize(ctx, AuthorizeParams{
		ActorID:    actor.ID,
		Capability: CapabilityDashboardRead,
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if decision.Decision != DecisionDeny {
		t.Fatalf("Decision = %s, want deny", decision.Decision)
	}
	if decision.Reason != "principal_disabled" {
		t.Fatalf("Reason = %s, want principal_disabled", decision.Reason)
	}
}

func TestAuthorize_DelegatedActorRequiresDirectGrant(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, _ := openIdentityStore(t, now)
	session := seedActor(t, store)

	delegated, err := store.CreateActor(context.Background(), ActorParams{
		ID:            "actor-cron",
		PrincipalID:   session.PrincipalID,
		ActorType:     ActorTypeCron,
		ParentActorID: session.ID,
	})
	if err != nil {
		t.Fatalf("CreateActor delegated: %v", err)
	}
	if _, err := store.CreateGrant(context.Background(), GrantParams{
		ID:          "grant-principal-cron",
		SubjectType: SubjectTypePrincipal,
		SubjectID:   session.PrincipalID,
		Capability:  CapabilityCronRun,
	}); err != nil {
		t.Fatalf("CreateGrant principal: %v", err)
	}

	decision, err := store.Authorize(context.Background(), AuthorizeParams{
		ActorID:    delegated.ID,
		Capability: CapabilityCronRun,
	})
	if err != nil {
		t.Fatalf("Authorize principal grant: %v", err)
	}
	if decision.Decision != DecisionDeny {
		t.Fatalf("delegated principal-grant decision = %s, want deny", decision.Decision)
	}

	if _, err := store.CreateGrant(context.Background(), GrantParams{
		ID:          "grant-actor-cron",
		SubjectType: SubjectTypeActor,
		SubjectID:   delegated.ID,
		Capability:  CapabilityCronRun,
	}); err != nil {
		t.Fatalf("CreateGrant actor: %v", err)
	}
	decision, err = store.Authorize(context.Background(), AuthorizeParams{
		ActorID:    delegated.ID,
		Capability: CapabilityCronRun,
	})
	if err != nil {
		t.Fatalf("Authorize actor grant: %v", err)
	}
	if decision.Decision != DecisionAllow {
		t.Fatalf("delegated actor-grant decision = %s, want allow", decision.Decision)
	}
}

func TestAuthorize_ParentedSessionRequiresDirectGrant(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, _ := openIdentityStore(t, now)
	session := seedActor(t, store)

	child, err := store.CreateActor(context.Background(), ActorParams{
		ID:            "actor-session-child",
		PrincipalID:   session.PrincipalID,
		ActorType:     ActorTypeSession,
		ParentActorID: session.ID,
	})
	if err != nil {
		t.Fatalf("CreateActor child session: %v", err)
	}
	if _, err := store.CreateGrant(context.Background(), GrantParams{
		ID:          "grant-principal-session-child",
		SubjectType: SubjectTypePrincipal,
		SubjectID:   session.PrincipalID,
		Capability:  CapabilityAPIChat,
	}); err != nil {
		t.Fatalf("CreateGrant principal: %v", err)
	}

	decision, err := store.Authorize(context.Background(), AuthorizeParams{
		ActorID:    child.ID,
		Capability: CapabilityAPIChat,
	})
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if decision.Decision != DecisionDeny {
		t.Fatalf("parented session decision = %s, want deny", decision.Decision)
	}
}

func TestAuthorize_ResourceScopes(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, _ := openIdentityStore(t, now)
	actor := seedActor(t, store)
	ctx := context.Background()

	if _, err := store.CreateGrant(ctx, GrantParams{
		ID:          "grant-global",
		SubjectType: SubjectTypeActor,
		SubjectID:   actor.ID,
		Capability:  CapabilityDashboardRead,
	}); err != nil {
		t.Fatalf("CreateGrant global: %v", err)
	}
	decision, err := store.Authorize(ctx, AuthorizeParams{
		ActorID:    actor.ID,
		Capability: CapabilityDashboardRead,
		Resource:   ResourceRef{Type: "dashboard", ID: "main"},
	})
	if err != nil {
		t.Fatalf("Authorize global: %v", err)
	}
	if decision.Decision != DecisionAllow {
		t.Fatalf("global scope decision = %s, want allow", decision.Decision)
	}

	if _, err := store.CreateGrant(ctx, GrantParams{
		ID:          "grant-type",
		SubjectType: SubjectTypeActor,
		SubjectID:   actor.ID,
		Capability:  CapabilityWikiWrite,
		Resource:    ResourceRef{Type: "wiki"},
	}); err != nil {
		t.Fatalf("CreateGrant type: %v", err)
	}
	decision, err = store.Authorize(ctx, AuthorizeParams{
		ActorID:    actor.ID,
		Capability: CapabilityWikiWrite,
		Resource:   ResourceRef{Type: "wiki", ID: "page-1"},
	})
	if err != nil {
		t.Fatalf("Authorize type: %v", err)
	}
	if decision.Decision != DecisionAllow {
		t.Fatalf("type scope decision = %s, want allow", decision.Decision)
	}
	decision, err = store.Authorize(ctx, AuthorizeParams{
		ActorID:    actor.ID,
		Capability: CapabilityWikiWrite,
		Resource:   ResourceRef{Type: "memory", ID: "profile"},
	})
	if err != nil {
		t.Fatalf("Authorize wrong type: %v", err)
	}
	if decision.Decision != DecisionDeny {
		t.Fatalf("wrong type decision = %s, want deny", decision.Decision)
	}
}

func TestCreateOrResolveChannelAccountRequiresExistingPrincipal(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, _ := openIdentityStore(t, now)

	_, _, err := store.CreateOrResolveChannelAccount(context.Background(), ChannelAccountParams{
		ID:          "acct-bad",
		PrincipalID: "missing",
		Provider:    "telegram",
		ExternalID:  "999",
	})
	if err == nil {
		t.Fatal("CreateOrResolveChannelAccount error = nil, want foreign-key error")
	}
	if errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateOrResolveChannelAccount returned validation error, want DB integrity error: %v", err)
	}
}

func TestCreateActorRejectsChannelAccountPrincipalMismatch(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, _ := openIdentityStore(t, now)
	ctx := context.Background()
	if _, _, err := store.CreateOrResolvePrincipal(ctx, PrincipalParams{
		ID:   "principal-a",
		Kind: PrincipalKindHuman,
	}); err != nil {
		t.Fatalf("CreateOrResolvePrincipal principal-a: %v", err)
	}
	if _, _, err := store.CreateOrResolvePrincipal(ctx, PrincipalParams{
		ID:   "principal-b",
		Kind: PrincipalKindHuman,
	}); err != nil {
		t.Fatalf("CreateOrResolvePrincipal principal-b: %v", err)
	}
	account, _, err := store.CreateOrResolveChannelAccount(ctx, ChannelAccountParams{
		ID:          "acct-a",
		PrincipalID: "principal-a",
		Provider:    "telegram",
		ExternalID:  "123",
	})
	if err != nil {
		t.Fatalf("CreateOrResolveChannelAccount: %v", err)
	}

	_, err = store.CreateActor(ctx, ActorParams{
		ID:               "actor-mismatch",
		PrincipalID:      "principal-b",
		ActorType:        ActorTypeSession,
		ChannelAccountID: account.ID,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateActor mismatch error = %v, want ErrInvalidInput", err)
	}
}

func TestCreateGrantRequiresExistingSubject(t *testing.T) {
	now := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)
	store, _ := openIdentityStore(t, now)
	ctx := context.Background()
	actor := seedActor(t, store)

	_, err := store.CreateGrant(ctx, GrantParams{
		ID:          "grant-missing-principal",
		SubjectType: SubjectTypePrincipal,
		SubjectID:   "missing-principal",
		Capability:  CapabilityAPIChat,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateGrant missing principal error = %v, want ErrInvalidInput", err)
	}

	_, err = store.CreateGrant(ctx, GrantParams{
		ID:          "grant-missing-actor",
		SubjectType: SubjectTypeActor,
		SubjectID:   "missing-actor",
		Capability:  CapabilityAPIChat,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("CreateGrant missing actor error = %v, want ErrInvalidInput", err)
	}

	if _, err := store.CreateGrant(ctx, GrantParams{
		ID:          "grant-existing-actor",
		SubjectType: SubjectTypeActor,
		SubjectID:   actor.ID,
		Capability:  CapabilityAPIChat,
	}); err != nil {
		t.Fatalf("CreateGrant existing actor: %v", err)
	}
}

func assertIdentityScalar(t *testing.T, db *sql.DB, query, want string, args ...any) {
	t.Helper()
	var got string
	if err := db.QueryRow(query, args...).Scan(&got); err != nil {
		t.Fatalf("query scalar %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("scalar %q = %q, want %q", query, got, want)
	}
}
