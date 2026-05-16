package summarizer_test

import (
	"context"
	"testing"

	"github.com/aura/aura/internal/conversation/summarizer"
)

// fakeUserMemoryWriter is a stateful fake that implements idempotency by handle.
type fakeUserMemoryWriter struct {
	created map[string]bool
	rows    []summarizer.UserMemoryProposalInput
}

func (f *fakeUserMemoryWriter) CreateUserMemoryProposal(_ context.Context, p summarizer.UserMemoryProposalInput) (bool, error) {
	if f.created == nil {
		f.created = make(map[string]bool)
	}
	if f.created[p.Handle] {
		return false, nil
	}
	f.created[p.Handle] = true
	f.rows = append(f.rows, p)
	return true, nil
}

// fakeMemorySearcher returns a fixed set of hits for any query.
type fakeMemorySearcher struct {
	hits []summarizer.UserMemoryHit
}

func (f *fakeMemorySearcher) SearchUserMemory(_ context.Context, _ string, _ int) ([]summarizer.UserMemoryHit, error) {
	return f.hits, nil
}

// newRoutingApplier builds a RoutingApplier with fresh fakes.
func newTestRoutingApplier(searcher *fakeMemorySearcher) (*summarizer.RoutingApplier, *fakeWikiStore, *fakeUserMemoryWriter) {
	wiki := &fakeWikiStore{}
	writer := &fakeUserMemoryWriter{}
	ra := summarizer.NewRoutingApplier(summarizer.NewAutoApplier(wiki), writer, searcher)
	return ra, wiki, writer
}

// TestRoutingApplier_PreferenceRoutesToUserMemory verifies that a
// category='preference' candidate goes to proposed_updates, NOT the wiki.
func TestRoutingApplier_PreferenceRoutesToUserMemory(t *testing.T) {
	ra, wiki, writer := newTestRoutingApplier(&fakeMemorySearcher{})

	err := ra.Apply(context.Background(), summarizer.Decision{
		Candidate: summarizer.Candidate{
			Fact:          "User prefers dark mode",
			Category:      "preference",
			SourceTurnIDs: []int64{10},
		},
		Action: summarizer.ActionNew,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(wiki.writes) != 0 {
		t.Fatalf("wiki writes = %d, want 0", len(wiki.writes))
	}
	if len(writer.rows) != 1 {
		t.Fatalf("user_memory rows = %d, want 1", len(writer.rows))
	}
	row := writer.rows[0]
	if row.Category != "preference" {
		t.Fatalf("category = %q, want preference", row.Category)
	}
	if row.Action != "new" {
		t.Fatalf("action = %q, want new", row.Action)
	}
	if row.Handle == "" {
		t.Fatal("handle must not be empty")
	}
}

// TestRoutingApplier_ProjectRoutesToWiki verifies that a category='project'
// candidate goes to the wiki applier, NOT user_memory proposals.
func TestRoutingApplier_ProjectRoutesToWiki(t *testing.T) {
	ra, wiki, writer := newTestRoutingApplier(&fakeMemorySearcher{})

	err := ra.Apply(context.Background(), summarizer.Decision{
		Candidate: summarizer.Candidate{
			Fact:     "Aura uses Go for the backend",
			Category: "project",
		},
		Action: summarizer.ActionNew,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(wiki.writes) != 1 {
		t.Fatalf("wiki writes = %d, want 1", len(wiki.writes))
	}
	if len(writer.rows) != 0 {
		t.Fatalf("user_memory rows = %d, want 0", len(writer.rows))
	}
}

// TestRoutingApplier_IdempotentUserMemory verifies that applying the same
// preference candidate twice produces exactly one proposal row (not two).
func TestRoutingApplier_IdempotentUserMemory(t *testing.T) {
	ra, _, writer := newTestRoutingApplier(&fakeMemorySearcher{})

	d := summarizer.Decision{
		Candidate: summarizer.Candidate{
			Fact:     "User prefers dark mode",
			Category: "preference",
		},
		Action: summarizer.ActionNew,
	}
	if err := ra.Apply(context.Background(), d); err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	if err := ra.Apply(context.Background(), d); err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if len(writer.rows) != 1 {
		t.Fatalf("rows = %d after two identical applies, want 1 (idempotent)", len(writer.rows))
	}
}

// TestRoutingApplier_ExistingUserMemoryGeneratesPatch verifies that when
// MemorySearcher returns a hit, the proposal action is "patch" with the
// existing entry's handle as TargetHandle.
func TestRoutingApplier_ExistingUserMemoryGeneratesPatch(t *testing.T) {
	existingHandle := "user_memory:abc123def456abcd"
	searcher := &fakeMemorySearcher{
		hits: []summarizer.UserMemoryHit{{Handle: existingHandle}},
	}
	ra, _, writer := newTestRoutingApplier(searcher)

	err := ra.Apply(context.Background(), summarizer.Decision{
		Candidate: summarizer.Candidate{
			Fact:     "User now prefers light mode",
			Category: "preference",
		},
		Action: summarizer.ActionNew,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(writer.rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(writer.rows))
	}
	row := writer.rows[0]
	if row.Action != "patch" {
		t.Fatalf("action = %q, want patch", row.Action)
	}
	if row.TargetHandle != existingHandle {
		t.Fatalf("target_handle = %q, want %q", row.TargetHandle, existingHandle)
	}
}
