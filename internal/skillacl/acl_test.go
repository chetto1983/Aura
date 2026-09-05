package skillacl

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"go.uber.org/goleak"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// TestMain installs the package-wide goroutine-leak gate. The store owns no goroutine — it
// borrows a pool connection per transaction and returns it — so a leak here would mean a test
// left a pool open.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// The unit tier proves everything that happens BEFORE a connection is borrowed: the
// permission algebra, the argument validation, and the projections. Every case below reaches
// its verdict without a database, which is the point — a caller must not be able to reach
// Postgres with a malformed grant and find out there.
//
// A Store with a nil pool is the instrument: if validation ever stopped running first, these
// tests would panic on the nil pool instead of returning an error, so they also pin the
// ORDER, not just the answer.
func nilPoolStore() *Store { return &Store{} }

const (
	testGranter  = "11111111-1111-4111-8111-111111111111"
	testGrantee  = "22222222-2222-4222-8222-222222222222"
	testResource = "33333333-3333-4333-8333-333333333333"
)

func TestPermValid(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		perm Perm
		want bool
	}{
		{"view", PermView, true},
		{"edit", PermEdit, true},
		{"view+share", PermView | PermShare, true},
		{"every defined bit", permAll, true},
		{"empty mask grants nothing", 0, false},
		{"negative", -1, false},
		{"undefined bit nobody tests for", 16, false},
		{"defined bit beside an undefined one", PermView | 32, false},
	} {
		if got := tc.perm.Valid(); got != tc.want {
			t.Errorf("%s: Perm(%d).Valid() = %v, want %v", tc.name, tc.perm, got, tc.want)
		}
	}
}

func TestNewStoreRejectsNilPool(t *testing.T) {
	t.Parallel()
	if _, err := NewStore(nil); err == nil {
		t.Fatal("NewStore(nil) must refuse: a store with no pool answers every read with a panic")
	}
}

// TestGrantArgsRejectsMalformedInput pins the four rejections a write makes before it borrows
// a connection: an unknown resource type, an unusable permission mask, and either uuid
// malformed.
func TestGrantArgsRejectsMalformedInput(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		rt       ResourceType
		granter  string
		resource string
		perm     Perm
		want     string
	}{
		{"unknown resource type", "agent", testGranter, testResource, PermView, "unsupported resource type"},
		{"empty permission mask", ResourceSkill, testGranter, testResource, 0, "permission bits"},
		{"undefined permission bit", ResourceSkill, testGranter, testResource, 64, "permission bits"},
		{"granter is not a uuid", ResourceSkill, "operator", testResource, PermView, "granter identity"},
		{"resource is not a uuid", ResourceSkill, testGranter, "my-skill", PermView, "resource id"},
	} {
		_, err := grantArgs(tc.granter, tc.rt, tc.resource, tc.perm)
		if err == nil {
			t.Fatalf("%s: want an error, got nil", tc.name)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %q does not name %q", tc.name, err, tc.want)
		}
	}
}

func TestGrantArgsAcceptsAWellFormedGrant(t *testing.T) {
	t.Parallel()
	args, err := grantArgs(testGranter, ResourceSkill, testResource, PermView)
	if err != nil {
		t.Fatalf("grantArgs: %v", err)
	}
	if !args.granter.Valid || !args.resource.Valid {
		t.Fatal("grantArgs must return two parsed uuids")
	}
	if uuid.UUID(args.granter.Bytes).String() != testGranter {
		t.Errorf("granter = %s, want %s", uuid.UUID(args.granter.Bytes), testGranter)
	}
}

// TestWritePathsValidateBeforeTouchingThePool runs every write method on a pool-less Store
// with input that must be rejected. Reaching the database would panic, so a passing test is
// the proof that validation comes first.
func TestWritePathsValidateBeforeTouchingThePool(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := nilPoolStore()

	if err := s.GrantToIdentity(ctx, testGranter, ResourceSkill, testResource, "not-a-uuid", PermView); err == nil {
		t.Error("GrantToIdentity must reject a grantee that is not a uuid")
	}
	if err := s.GrantToIdentity(ctx, testGranter, ResourceSkill, testResource, testGrantee, 0); err == nil {
		t.Error("GrantToIdentity must reject an empty permission mask")
	}
	if err := s.GrantPublic(ctx, "operator", ResourceSkill, testResource, PermView); err == nil {
		t.Error("GrantPublic must reject a granter that is not a uuid")
	}
	if _, err := s.RevokeFromIdentity(ctx, testGranter, ResourceSkill, "nope", testGrantee); err == nil {
		t.Error("RevokeFromIdentity must reject a resource id that is not a uuid")
	}
	if _, err := s.RevokeFromIdentity(ctx, testGranter, ResourceSkill, testResource, "nope"); err == nil {
		t.Error("RevokeFromIdentity must reject a grantee that is not a uuid")
	}
	if _, err := s.RevokePublic(ctx, testGranter, ResourceSkill, "nope"); err == nil {
		t.Error("RevokePublic must reject a resource id that is not a uuid")
	}
	if _, err := s.ListGrants(ctx, testGranter, ResourceSkill, "nope"); err == nil {
		t.Error("ListGrants must reject a resource id that is not a uuid")
	}
}

// TestAccessibleResourceIDsAnswersEmptyForNobody proves the two answers that need no
// database: an unusable mask is an error, and a caller with no identity is told the truth —
// nothing is shared with nobody — rather than being asked a question the RLS floor would
// answer with the same emptiness one layer down.
func TestAccessibleResourceIDsAnswersEmptyForNobody(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := nilPoolStore()

	if _, err := s.AccessibleResourceIDs(ctx, testGrantee, ResourceSkill, 0); !errors.Is(err, ErrInvalidPerm) {
		t.Errorf("empty mask = %v, want ErrInvalidPerm", err)
	}
	ids, err := s.AccessibleResourceIDs(ctx, "", ResourceSkill, PermView)
	if err != nil {
		t.Fatalf("unscoped caller: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("unscoped caller sees %v, want nothing", ids)
	}
	if _, err := s.AccessibleResourceIDs(ctx, "operator", ResourceSkill, PermView); err == nil {
		t.Error("AccessibleResourceIDs must reject an identity that is not a uuid")
	}
}

// TestUUIDProjectionDropsNulls pins the projection's refusal to invent a principal: a NULL
// uuid renders as the empty string, never as the zero uuid, which would read as a real
// identity in a listing and in an ACL comparison.
func TestUUIDProjectionDropsNulls(t *testing.T) {
	t.Parallel()
	parsed := uuid.MustParse(testGrantee)
	valid := pgtype.UUID{Bytes: parsed, Valid: true}

	if got := uuidString(pgtype.UUID{}); got != "" {
		t.Errorf("uuidString(NULL) = %q, want empty", got)
	}
	if got := uuidString(valid); got != testGrantee {
		t.Errorf("uuidString = %q, want %s", got, testGrantee)
	}
	got := uuidStrings([]pgtype.UUID{valid, {}, valid})
	if len(got) != 2 || got[0] != testGrantee || got[1] != testGrantee {
		t.Fatalf("uuidStrings = %v, want the two valid ids only", got)
	}
}

// TestGrantsProjectionRendersBothPrincipalShapes covers the operator's listing: an identity
// grant carries its principal, a public grant carries NULL, and NULL must render as the empty
// string rather than the zero uuid — which in a "who can read this?" listing would be
// indistinguishable from a real principal nobody granted anything to.
func TestGrantsProjectionRendersBothPrincipalShapes(t *testing.T) {
	t.Parallel()
	resource := pgtype.UUID{Bytes: uuid.MustParse(testResource), Valid: true}
	granter := pgtype.UUID{Bytes: uuid.MustParse(testGranter), Valid: true}
	grantee := pgtype.UUID{Bytes: uuid.MustParse(testGrantee), Valid: true}

	got := grantsFrom([]sqlc.AuraResourceAcl{
		{ResourceType: "skill", ResourceID: resource, PrincipalType: "identity", PrincipalID: grantee, PermBits: int32(PermView | PermEdit), GrantedBy: granter},
		{ResourceType: "skill", ResourceID: resource, PrincipalType: "public", PermBits: int32(PermView), GrantedBy: granter},
	})
	if len(got) != 2 {
		t.Fatalf("grantsFrom returned %d grants, want 2", len(got))
	}
	if got[0].PrincipalType != "identity" || got[0].PrincipalID != testGrantee || got[0].Perm != PermView|PermEdit {
		t.Errorf("identity grant = %+v", got[0])
	}
	if got[0].ResourceType != ResourceSkill || got[0].ResourceID != testResource || got[0].GrantedBy != testGranter {
		t.Errorf("identity grant lost a field: %+v", got[0])
	}
	if got[1].PrincipalID != "" {
		t.Errorf("public grant principal = %q, want empty — a zero uuid reads as a real identity", got[1].PrincipalID)
	}
	if empty := grantsFrom(nil); empty == nil || len(empty) != 0 {
		t.Errorf("grantsFrom(nil) = %v, want an empty non-nil slice", empty)
	}
}
