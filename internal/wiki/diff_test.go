package wiki

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
)

func TestDiffStoreFromGitRef_RoundTripSyntheticHistory(t *testing.T) {
	store := newGraphDiffTestStore(t)
	writeGraphDiffPage(t, store, "Alpha", "Alpha links [[beta]].")
	writeGraphDiffPage(t, store, "Beta", "Beta page.")

	baseline := currentGitHead(t, store.Dir())

	writeGraphDiffPage(t, store, "Alpha", "Alpha links [[gamma]].")
	writeGraphDiffPage(t, store, "Gamma", "Gamma links [[alpha]].")

	diff, resolved, err := DiffStoreFromGitRef(store, baseline)
	if err != nil {
		t.Fatalf("DiffStoreFromGitRef: %v", err)
	}
	if resolved != baseline {
		t.Fatalf("resolved = %q, want baseline %q", resolved, baseline)
	}
	if got, want := diff.NewNodes, []string{"gamma"}; !sameStrings(got, want) {
		t.Fatalf("new_nodes = %v, want %v", got, want)
	}
	if len(diff.RemovedNodes) != 0 {
		t.Fatalf("removed_nodes = %v, want none", diff.RemovedNodes)
	}
	if !hasGraphEdge(diff.NewEdges, GraphEdge{Source: "alpha", Target: "gamma", Type: "mentions"}) {
		t.Fatalf("new_edges missing alpha->gamma: %+v", diff.NewEdges)
	}
	if !hasGraphEdge(diff.RemovedEdges, GraphEdge{Source: "alpha", Target: "beta", Type: "mentions"}) {
		t.Fatalf("removed_edges missing alpha->beta: %+v", diff.RemovedEdges)
	}
}

func TestDiffStoreFromGitRef_EmptyDiff(t *testing.T) {
	store := newGraphDiffTestStore(t)
	writeGraphDiffPage(t, store, "Alpha", "Alpha links [[beta]].")
	writeGraphDiffPage(t, store, "Beta", "Beta page.")

	head := currentGitHead(t, store.Dir())
	diff, _, err := DiffStoreFromGitRef(store, head)
	if err != nil {
		t.Fatalf("DiffStoreFromGitRef: %v", err)
	}
	if diff.Summary != "no changes" {
		t.Fatalf("summary = %q, want no changes", diff.Summary)
	}
	if len(diff.NewNodes)+len(diff.RemovedNodes)+len(diff.NewEdges)+len(diff.RemovedEdges) != 0 {
		t.Fatalf("empty diff has changes: %+v", diff)
	}
}

func TestDiffStoreFromGitRef_InvalidSinceReturnsError(t *testing.T) {
	store := newGraphDiffTestStore(t)
	writeGraphDiffPage(t, store, "Alpha", "Alpha page.")

	_, _, err := DiffStoreFromGitRef(store, "not-a-real-ref")
	if err == nil {
		t.Fatal("DiffStoreFromGitRef returned nil error for invalid since")
	}
	if !strings.Contains(err.Error(), "resolve") {
		t.Fatalf("error = %v, want resolve failure", err)
	}
}

func newGraphDiffTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(t.TempDir(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func writeGraphDiffPage(t *testing.T, store *Store, title, body string) {
	t.Helper()
	page := &Page{
		Title:         title,
		Body:          body,
		SchemaVersion: CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     "2026-05-25T00:00:00Z",
		UpdatedAt:     "2026-05-25T00:00:00Z",
	}
	if err := store.WritePage(context.Background(), page); err != nil {
		t.Fatalf("WritePage(%s): %v", title, err)
	}
}

func currentGitHead(t *testing.T, dir string) string {
	t.Helper()
	repo, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatalf("PlainOpen: %v", err)
	}
	ref, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	return ref.Hash().String()
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func hasGraphEdge(edges []GraphEdge, want GraphEdge) bool {
	for _, edge := range edges {
		if edge == want {
			return true
		}
	}
	return false
}
