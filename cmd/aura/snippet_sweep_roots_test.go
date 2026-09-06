package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/skills"
)

const sweepIdentity = "11111111-1111-4111-8111-111111111111"

// writeFreshSnippet lays down a snippet that is NOT stale, so the sweep reports it as kept
// without archiving it. That keeps this test on the enumeration — which roots the sweep
// visits — with no database: only the archive path writes an audit row.
func writeFreshSnippet(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: d\ntype: snippet\nlanguage: python\n---\nprint('hi')\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func sweepTestWriter(t *testing.T) (*skills.Writer, string, string) {
	t.Helper()
	base := t.TempDir()
	global := filepath.Join(base, "skills")
	identities := filepath.Join(base, "identities")
	w := skills.NewWriter(skills.WriterConfig{
		ActiveDir:  global,
		ExportDir:  filepath.Join(base, "export"),
		ArchiveDir: filepath.Join(global, skills.StageArchived),
		Layout:     skills.Layout{Global: global, Identities: identities, Export: filepath.Join(base, "export")},
	})
	return w, global, identities
}

// The TTL sweep read ONE root — the deployment-global one — while everything a person saves
// through skill_manage lands in their own root (amendment #214). So no snippet an identity
// ever saved could expire, and the TTL applied only to the house library, which is the one
// place snippets are not saved. Measured live 2026-09-06 before this test existed.
func TestSnippetSweepVisitsTheIdentityRoots(t *testing.T) {
	w, global, identities := sweepTestWriter(t)
	writeFreshSnippet(t, global, "house-snippet")
	identityRoot := filepath.Join(identities, sweepIdentity)
	writeFreshSnippet(t, identityRoot, "mine")

	adapter := &snippetSweeperAdapter{w: w, identitiesDir: identities}
	_, kept, err := adapter.SweepExpiredSnippets(context.Background(), time.Hour, time.Now().UTC(), "auto")
	if err != nil {
		t.Fatalf("SweepExpiredSnippets: %v", err)
	}
	if !slices.Contains(kept, "mine") {
		t.Fatalf("kept = %v, want the identity's own snippet — its root was never visited", kept)
	}
	if !slices.Contains(kept, "house-snippet") {
		t.Fatalf("kept = %v, want the house library still swept too", kept)
	}
}

// The house library must be swept exactly once. Writer.For returns the receiver unchanged
// when a name resolves to no own root, so a loop that did not notice would re-sweep the
// global root once per directory and count every house skill N times.
func TestSnippetSweepDoesNotDoubleCountTheHouseLibrary(t *testing.T) {
	w, global, identities := sweepTestWriter(t)
	writeFreshSnippet(t, global, "house-snippet")
	for _, id := range []string{sweepIdentity, "22222222-2222-4222-8222-222222222222"} {
		writeFreshSnippet(t, filepath.Join(identities, id), "mine")
	}

	adapter := &snippetSweeperAdapter{w: w, identitiesDir: identities}
	_, kept, err := adapter.SweepExpiredSnippets(context.Background(), time.Hour, time.Now().UTC(), "auto")
	if err != nil {
		t.Fatalf("SweepExpiredSnippets: %v", err)
	}
	var house int
	for _, k := range kept {
		if k == "house-snippet" {
			house++
		}
	}
	if house != 1 {
		t.Fatalf("house-snippet swept %d times, want exactly 1 (kept = %v)", house, kept)
	}
}

// A deployment with no per-identity base behaves exactly as it did before #214: the house
// library is swept and nothing else is attempted.
func TestSnippetSweepWithoutAnIdentityBaseIsUnchanged(t *testing.T) {
	w, global, _ := sweepTestWriter(t)
	writeFreshSnippet(t, global, "house-snippet")

	adapter := &snippetSweeperAdapter{w: w} // no identitiesDir
	_, kept, err := adapter.SweepExpiredSnippets(context.Background(), time.Hour, time.Now().UTC(), "auto")
	if err != nil {
		t.Fatalf("SweepExpiredSnippets: %v", err)
	}
	if len(kept) != 1 || kept[0] != "house-snippet" {
		t.Fatalf("kept = %v, want only the house library", kept)
	}
}

// A missing base is a deployment that has no per-identity skills yet, not a fault: the sweep
// must still do the house library rather than failing the whole scheduled task.
func TestSnippetSweepToleratesAMissingIdentityBase(t *testing.T) {
	w, global, identities := sweepTestWriter(t)
	writeFreshSnippet(t, global, "house-snippet")

	adapter := &snippetSweeperAdapter{w: w, identitiesDir: filepath.Join(identities, "does-not-exist")}
	_, kept, err := adapter.SweepExpiredSnippets(context.Background(), time.Hour, time.Now().UTC(), "auto")
	if err != nil {
		t.Fatalf("SweepExpiredSnippets returned an error for an absent base: %v", err)
	}
	if !slices.Contains(kept, "house-snippet") {
		t.Fatalf("kept = %v, want the house library swept anyway", kept)
	}
}
