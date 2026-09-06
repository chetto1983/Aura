//go:build db_integration

// Live proof of amendment #214 acceptance criterion 5's read: a grant standing in
// aura.resource_acl turns one identity's skill into a directory another identity may load,
// and revoking it takes the directory away.
//
// It is the sentence the unit tier cannot say, because the unit tier fakes the two stores
// that RLS actually governs: here the grant, the catalog row and the visibility rule are the
// database's, and what is asserted is that the join reaches a BODY and stops there.
//
// Requires a Postgres with the migrations applied through 0118 and AURA_DB_URL set (the
// aura_app DSN). No-skip-as-green: catalogEnvOrSkip t.Fatals under $CI when the DSN is unset.
//
//	go test -tags db_integration -race -run TestSharedReader ./internal/skills -count=1
package skills

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/skillacl"
)

// sharedLiveFixture builds a layout, a writer scoped to the owner, and the SharedReader over
// the live stores — all from the SAME layout, so the directory the reader resolves is the one
// the writer really wrote.
func sharedLiveFixture(t *testing.T, pool *pgxpool.Pool, owner string) (*SharedReader, *Writer, Layout) {
	t.Helper()
	root := t.TempDir()
	layout := Layout{
		Global:     filepath.Join(root, "active"),
		Identities: filepath.Join(root, "identities"),
		Export:     filepath.Join(root, "export"),
	}
	w := NewWriter(WriterConfig{
		Pool:         pool,
		ActiveDir:    layout.Global,
		ExportDir:    layout.Export,
		ArchiveDir:   filepath.Join(layout.Global, StageArchived),
		Layout:       layout,
		BodyCapBytes: 32768,
	})
	scoped, err := w.For(owner)
	if err != nil {
		t.Fatalf("writer for %q: %v", owner, err)
	}
	acl, err := skillacl.NewStore(pool)
	if err != nil {
		t.Fatalf("skillacl.NewStore: %v", err)
	}
	return NewSharedReader(acl, NewCatalogStore(pool), layout), scoped, layout
}

// TestSharedReaderSeesAGrantAndLosesItOnRevoke walks the whole of criterion 5's read path
// against the live database: nothing before the grant, the body after it, nothing again after
// the revoke — and never the skill the owner did not share.
func TestSharedReaderSeesAGrantAndLosesItOnRevoke(t *testing.T) {
	pool := catalogPool(t)
	ctx := context.Background()
	alice := catalogIdentity(t, pool)
	bob := catalogIdentity(t, pool)

	reader, scoped, layout := sharedLiveFixture(t, pool, alice)
	actor := AuditActor{ActorID: "cli", IdentityID: alice}
	if _, err := scoped.WriteMutationByName(ctx, "create", "shared-one", "shared", "SHARED BODY", false, actor); err != nil {
		t.Fatalf("write shared skill: %v", err)
	}
	if _, err := scoped.WriteMutationByName(ctx, "create", "private-one", "private", "PRIVATE BODY", false, actor); err != nil {
		t.Fatalf("write private skill: %v", err)
	}

	// Before the grant there is nothing to see, which is the baseline the rest only means
	// something against.
	if got, err := reader.For(ctx, bob); err != nil || len(got) != 0 {
		t.Fatalf("before any grant: (%+v, %v), want nothing shared with bob", got, err)
	}

	catalog := NewCatalogStore(pool)
	id, err := catalog.ResolveID(ctx, alice, "shared-one")
	if err != nil {
		t.Fatalf("ResolveID: %v", err)
	}
	acl, err := skillacl.NewStore(pool)
	if err != nil {
		t.Fatalf("skillacl.NewStore: %v", err)
	}
	if err := acl.GrantToIdentity(ctx, alice, skillacl.ResourceSkill, id, bob, skillacl.PermView); err != nil {
		t.Fatalf("grant: %v", err)
	}

	got, err := reader.For(ctx, bob)
	if err != nil {
		t.Fatalf("after the grant: %v", err)
	}
	if len(got) != 1 || got[0].Name != "shared-one" || got[0].OwnerID != alice {
		t.Fatalf("after the grant = %+v, want alice's shared-one and nothing else", got)
	}
	aliceExport, err := layout.For(alice)
	if err != nil {
		t.Fatalf("layout for alice: %v", err)
	}
	if want := filepath.Join(aliceExport.Export, "shared-one"); got[0].Dir != want {
		t.Fatalf("resolved dir = %q, want the skill's own directory %q — not the export tree", got[0].Dir, want)
	}
	if !isSkillDir(got[0].Dir) {
		t.Fatalf("the resolved dir %q holds no SKILL.md — the join resolved a row, not a skill", got[0].Dir)
	}

	// A public grant reaches an identity nobody named, and still only for the one skill.
	carol := catalogIdentity(t, pool)
	if err := acl.GrantPublic(ctx, alice, skillacl.ResourceSkill, id, skillacl.PermView); err != nil {
		t.Fatalf("grant public: %v", err)
	}
	public, err := reader.For(ctx, carol)
	if err != nil || len(public) != 1 || public[0].Name != "shared-one" {
		t.Fatalf("public grant for carol = (%+v, %v), want exactly shared-one", public, err)
	}

	// Revoked: both grants gone, and the answer is empty again. This is the half that keeps
	// the box honest — the resolver stops naming the source, and the mirror does the rest.
	if _, err := acl.RevokePublic(ctx, alice, skillacl.ResourceSkill, id); err != nil {
		t.Fatalf("revoke public: %v", err)
	}
	if _, err := acl.RevokeFromIdentity(ctx, alice, skillacl.ResourceSkill, id, bob); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if got, err := reader.For(ctx, bob); err != nil || len(got) != 0 {
		t.Fatalf("after the revoke = (%+v, %v), want nothing shared with bob", got, err)
	}
	if got, err := reader.For(ctx, carol); err != nil || len(got) != 0 {
		t.Fatalf("after the revoke = (%+v, %v), want nothing shared with carol", got, err)
	}
}

// TestSharedReaderWillNotResolveAnUngrantedID is the forged-share probe: bob asking with an
// id he was never granted resolves to nothing, because the catalog read runs under HIS RLS
// and not under the id he happens to know.
func TestSharedReaderWillNotResolveAnUngrantedID(t *testing.T) {
	pool := catalogPool(t)
	ctx := context.Background()
	alice := catalogIdentity(t, pool)
	bob := catalogIdentity(t, pool)

	_, scoped, layout := sharedLiveFixture(t, pool, alice)
	if _, err := scoped.WriteMutationByName(ctx, "create", "not-yours", "private", "PRIVATE BODY", false,
		AuditActor{ActorID: "cli", IdentityID: alice}); err != nil {
		t.Fatalf("write: %v", err)
	}
	catalog := NewCatalogStore(pool)
	id, err := catalog.ResolveID(ctx, alice, "not-yours")
	if err != nil {
		t.Fatalf("ResolveID: %v", err)
	}

	// The ACL is bypassed on purpose: this is what a bug that trusted a caller-supplied id
	// would do, and the database must still refuse.
	forged := NewSharedReader(fixedGrants{ids: []string{id}}, catalog, layout)
	if got, err := forged.For(ctx, bob); err != nil || len(got) != 0 {
		t.Fatalf("an ungranted id resolved to %+v (err %v) — RLS is not the floor here", got, err)
	}
}

// fixedGrants answers with ids nobody granted — the ACL step, forged.
type fixedGrants struct{ ids []string }

func (f fixedGrants) AccessibleResourceIDs(context.Context, string, skillacl.ResourceType, skillacl.Perm) ([]string, error) {
	return f.ids, nil
}
