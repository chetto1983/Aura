package skills

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/skillacl"
)

// shared_test.go is the daemon-free, database-free tier of the shared-skill read (amendment
// #214 criterion 5). The two stores are faked because what needs proving here is not that
// Postgres answers — the db_integration tier does that — but the DROP rules and the
// path each surviving grant resolves to. Those are what stand between a grant and another
// person's instructions entering a context.

const (
	sharedAlice = "11111111-1111-4111-8111-111111111111"
	sharedBob   = "22222222-2222-4222-8222-222222222222"
	sharedCarol = "33333333-3333-4333-8333-333333333333"
)

// fakeGrants is a canned ACL: the ids it hands back, or an error.
type fakeGrants struct {
	ids []string
	err error
}

func (f fakeGrants) AccessibleResourceIDs(context.Context, string, skillacl.ResourceType, skillacl.Perm) ([]string, error) {
	return f.ids, f.err
}

// fakeCatalog is a canned catalog keyed by id, standing in for the RLS-scoped read.
type fakeCatalog struct {
	rows map[string]CatalogRow
	err  error
}

func (f fakeCatalog) ListByIDs(_ context.Context, _ string, ids []string) ([]CatalogRow, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]CatalogRow, 0, len(ids))
	for _, id := range ids {
		if row, ok := f.rows[id]; ok {
			out = append(out, row)
		}
	}
	return out, nil
}

// sharedFixture lays down a layout with one skill in alice's export and returns the reader.
func sharedFixture(t *testing.T, rows map[string]CatalogRow, ids ...string) (*SharedReader, Layout) {
	t.Helper()
	base := t.TempDir()
	layout := Layout{
		Global:     filepath.Join(base, "skills"),
		Identities: filepath.Join(base, "identities"),
		Export:     filepath.Join(base, "export"),
	}
	return NewSharedReader(fakeGrants{ids: ids}, fakeCatalog{rows: rows}, layout), layout
}

// writeSkillTree writes a minimal valid skill under dir/name.
func writeSkillTree(t *testing.T, dir, name, body string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(full, 0o750); err != nil {
		t.Fatalf("mkdir %q: %v", full, err)
	}
	md := "---\nname: " + name + "\ndescription: " + name + " description\ntype: instruction\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(full, "SKILL.md"), []byte(md), 0o600); err != nil {
		t.Fatalf("write %q: %v", name, err)
	}
	return full
}

// exportDirOf resolves one identity's export through the production layout, never by a join
// written here: a fixture that decides for itself where an identity exports proves nothing
// about where the code looks.
func exportDirOf(t *testing.T, layout Layout, identity string) string {
	t.Helper()
	roots, err := layout.For(identity)
	if err != nil {
		t.Fatalf("layout for %q: %v", identity, err)
	}
	return roots.Export
}

// TestSharedReaderResolvesAGrantToOneSkillDirectory is the shape criterion 5 rests on: a
// grant becomes ONE directory, the shared skill's own, under the owner's export.
func TestSharedReaderResolvesAGrantToOneSkillDirectory(t *testing.T) {
	t.Parallel()
	rows := map[string]CatalogRow{
		"id-1": {ID: "id-1", OwnerID: sharedAlice, Name: "alice-shared"},
	}
	reader, layout := sharedFixture(t, rows, "id-1")
	aliceExport := exportDirOf(t, layout, sharedAlice)
	want := writeSkillTree(t, aliceExport, "alice-shared", "alice body")
	// A second skill alice did NOT share sits right beside it. If the resolver ever answered
	// with the export TREE instead of the skill directory, this is the one that would ride
	// along (amendment #215) — so it is here to be absent, not for decoration.
	writeSkillTree(t, aliceExport, "alice-private", "private body")

	got, err := reader.For(context.Background(), sharedBob)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("For = %+v, want exactly the one shared skill", got)
	}
	if got[0].Name != "alice-shared" || got[0].OwnerID != sharedAlice || got[0].Dir != want {
		t.Fatalf("For = %+v, want {alice-shared %s %s}", got[0], sharedAlice, want)
	}
	// The dir is the SKILL's, never the owner's export: nothing beside it is reachable from it.
	if got[0].Dir == aliceExport {
		t.Fatal("the resolved source is the owner's whole export tree — every unshared skill would ride along")
	}
	if dirs := SharedDirs(got); len(dirs) != 1 || dirs[0] != want {
		t.Fatalf("SharedDirs = %v, want [%s]", dirs, want)
	}
}

// TestSharedReaderDrops covers the per-row drop rules, each with the failure it prevents.
func TestSharedReaderDrops(t *testing.T) {
	t.Parallel()

	t.Run("a row the reader owns is not a share", func(t *testing.T) {
		t.Parallel()
		rows := map[string]CatalogRow{"id-1": {ID: "id-1", OwnerID: sharedBob, Name: "mine"}}
		reader, layout := sharedFixture(t, rows, "id-1")
		writeSkillTree(t, exportDirOf(t, layout, sharedBob), "mine", "my body")
		got, err := reader.For(context.Background(), sharedBob)
		if err != nil || len(got) != 0 {
			t.Fatalf("For = (%+v, %v), want the reader's own row dropped", got, err)
		}
	})

	t.Run("a name the reader's own library already uses is dropped", func(t *testing.T) {
		t.Parallel()
		rows := map[string]CatalogRow{"id-1": {ID: "id-1", OwnerID: sharedAlice, Name: "deploy"}}
		reader, layout := sharedFixture(t, rows, "id-1")
		writeSkillTree(t, exportDirOf(t, layout, sharedAlice), "deploy", "alice deploy")
		// Bob's own `deploy`, in his own root — the loader root, not the export.
		bobRoots, err := layout.For(sharedBob)
		if err != nil {
			t.Fatalf("layout for bob: %v", err)
		}
		writeSkillTree(t, bobRoots.Identity, "deploy", "bob deploy")

		got, gerr := reader.For(context.Background(), sharedBob)
		if gerr != nil || len(got) != 0 {
			t.Fatalf("For = (%+v, %v), want the colliding share dropped — a share must not land INSIDE bob's own skill", got, gerr)
		}
	})

	t.Run("a name the deployment library uses is dropped", func(t *testing.T) {
		t.Parallel()
		rows := map[string]CatalogRow{"id-1": {ID: "id-1", OwnerID: sharedAlice, Name: "deploy"}}
		reader, layout := sharedFixture(t, rows, "id-1")
		writeSkillTree(t, exportDirOf(t, layout, sharedAlice), "deploy", "alice deploy")
		writeSkillTree(t, layout.Global, "deploy", "house deploy")

		got, err := reader.For(context.Background(), sharedBob)
		if err != nil || len(got) != 0 {
			t.Fatalf("For = (%+v, %v), want house policy untouched by a share of the same name", got, err)
		}
	})

	t.Run("a row whose body is not on disk is not a skill", func(t *testing.T) {
		t.Parallel()
		rows := map[string]CatalogRow{"id-1": {ID: "id-1", OwnerID: sharedAlice, Name: "archived-one"}}
		reader, _ := sharedFixture(t, rows, "id-1")
		got, err := reader.For(context.Background(), sharedBob)
		if err != nil || len(got) != 0 {
			t.Fatalf("For = (%+v, %v), want an archived/deleted body to stop being readable", got, err)
		}
	})

	t.Run("a name that cannot name a directory is dropped", func(t *testing.T) {
		t.Parallel()
		// The catalog CHECK makes this unstorable; the guard is here because the name is
		// joined into a path HERE, and a validation that lives only where the value was
		// written is a validation the next writer can bypass.
		rows := map[string]CatalogRow{"id-1": {ID: "id-1", OwnerID: sharedAlice, Name: "../escape"}}
		reader, _ := sharedFixture(t, rows, "id-1")
		got, err := reader.For(context.Background(), sharedBob)
		if err != nil || len(got) != 0 {
			t.Fatalf("For = (%+v, %v), want a traversal-shaped name refused", got, err)
		}
	})
}

// TestSharedReaderDropsANameTwoOwnersShare is the collision the reader's OWN library cannot
// cause and nameTaken therefore cannot see: the catalog is unique on (owner, name), so alice
// and carol may each own a `deploy` and each share it with bob. Both consumers would then be
// handed two entries for one name — the loader keeps whichever came last, and the box tars
// BOTH trees into the single /skills/deploy, leaving one person's files beside the other
// person's SKILL.md. Neither is readable instead.
func TestSharedReaderDropsANameTwoOwnersShare(t *testing.T) {
	t.Parallel()
	rows := map[string]CatalogRow{
		"id-1": {ID: "id-1", OwnerID: sharedAlice, Name: "deploy"},
		"id-2": {ID: "id-2", OwnerID: sharedCarol, Name: "deploy"},
		"id-3": {ID: "id-3", OwnerID: sharedCarol, Name: "uncontested"},
	}
	reader, layout := sharedFixture(t, rows, "id-1", "id-2", "id-3")
	writeSkillTree(t, exportDirOf(t, layout, sharedAlice), "deploy", "alice deploy")
	carolExport := exportDirOf(t, layout, sharedCarol)
	writeSkillTree(t, carolExport, "deploy", "carol deploy")
	writeSkillTree(t, carolExport, "uncontested", "carol only")

	got, err := reader.For(context.Background(), sharedBob)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	// The contested name is gone from BOTH owners; the share nobody contests is untouched, so
	// the rule drops a NAME and not an owner.
	if len(got) != 1 || got[0].Name != "uncontested" || got[0].OwnerID != sharedCarol {
		t.Fatalf("For = %+v, want only carol's uncontested share — two owners' trees must not both claim /skills/deploy", got)
	}
	for _, dir := range SharedDirs(got) {
		if filepath.Base(dir) == "deploy" {
			t.Fatalf("a contested source survived: %q", dir)
		}
	}
}

// TestSharedReaderIsOrderedAndDeterministic pins the name ordering both consumers depend on:
// the materialize sources and the loader dirs must be the same list in the same order every
// resume, or an identical grant set would look like a change and rebuild the world.
func TestSharedReaderIsOrderedAndDeterministic(t *testing.T) {
	t.Parallel()
	rows := map[string]CatalogRow{
		"id-1": {ID: "id-1", OwnerID: sharedAlice, Name: "zebra"},
		"id-2": {ID: "id-2", OwnerID: sharedAlice, Name: "alpha"},
	}
	reader, layout := sharedFixture(t, rows, "id-1", "id-2")
	export := exportDirOf(t, layout, sharedAlice)
	writeSkillTree(t, export, "zebra", "z")
	writeSkillTree(t, export, "alpha", "a")

	got, err := reader.For(context.Background(), sharedBob)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "zebra" {
		t.Fatalf("For = %+v, want name order [alpha zebra]", got)
	}
}

// TestSharedReaderFailsLoudly proves a lookup failure is an ERROR and not an empty list. The
// two are the same silence to a caller that does not check, and only one of them is true;
// the composition root degrades on it deliberately, which it can only do if it is told.
func TestSharedReaderFailsLoudly(t *testing.T) {
	t.Parallel()
	boom := errors.New("database is unreachable")
	layout := Layout{Global: t.TempDir(), Identities: t.TempDir(), Export: t.TempDir()}

	grantsFail := NewSharedReader(fakeGrants{err: boom}, fakeCatalog{}, layout)
	if _, err := grantsFail.For(context.Background(), sharedBob); !errors.Is(err, boom) {
		t.Fatalf("a failed grant lookup returned %v, want the cause wrapped", err)
	}
	catalogFail := NewSharedReader(fakeGrants{ids: []string{"id-1"}}, fakeCatalog{err: boom}, layout)
	if _, err := catalogFail.For(context.Background(), sharedBob); !errors.Is(err, boom) {
		t.Fatalf("a failed catalog lookup returned %v, want the cause wrapped", err)
	}
	if _, err := grantsFail.For(context.Background(), "../escape"); err == nil {
		t.Fatal("a reader whose id cannot name a root must be an error, not an empty answer")
	}
}

// TestSharedReaderDegenerateWirings covers the answers a composition without a pool, without
// grants, or without an identity must give: nothing, and no panic.
func TestSharedReaderDegenerateWirings(t *testing.T) {
	t.Parallel()
	layout := Layout{Global: t.TempDir(), Identities: t.TempDir(), Export: t.TempDir()}

	if r := NewSharedReader(nil, fakeCatalog{}, layout); r != nil {
		t.Fatal("a reader with no ACL must be nil, not a store that answers from half a join")
	}
	if r := NewSharedReader(fakeGrants{}, nil, layout); r != nil {
		t.Fatal("a reader with no catalog must be nil")
	}
	var absent *SharedReader
	if got, err := absent.For(context.Background(), sharedBob); got != nil || err != nil {
		t.Fatalf("nil reader = (%+v, %v), want (nil, nil)", got, err)
	}
	unscoped := NewSharedReader(fakeGrants{ids: []string{"id-1"}}, fakeCatalog{}, layout)
	if got, err := unscoped.For(context.Background(), ""); got != nil || err != nil {
		t.Fatalf("unscoped reader = (%+v, %v), want (nil, nil) — nothing is shared with nobody", got, err)
	}
	empty := NewSharedReader(fakeGrants{}, fakeCatalog{}, layout)
	if got, err := empty.For(context.Background(), sharedBob); len(got) != 0 || err != nil {
		t.Fatalf("no grants = (%+v, %v), want no shares and no error", got, err)
	}
}
