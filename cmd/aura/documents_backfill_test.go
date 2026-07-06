package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/documents"
)

// fakeGraph is a documents.KnowledgeClient that records the ownership-edge writes the
// backfill issues and echoes a per-query attached count via the `count` row key.
type fakeGraph struct {
	writes []string
}

func (g *fakeGraph) Read(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (g *fakeGraph) Write(_ context.Context, query string, _ map[string]any) ([]map[string]any, error) {
	g.writes = append(g.writes, query)
	return []map[string]any{{"count": 1}}, nil
}

func TestRunDocumentsCommand_UsageAndUnknown(t *testing.T) {
	factory := func(context.Context) (*backfillDeps, error) {
		t.Fatal("factory must not be called for usage/unknown dispatch")
		return nil, nil
	}
	if err := runDocumentsCommand(context.Background(), nil, nil, factory); err == nil ||
		!strings.Contains(err.Error(), "usage") {
		t.Fatalf("empty args err = %v, want usage error", err)
	}
	if err := runDocumentsCommand(context.Background(), []string{"frobnicate"}, nil, factory); err == nil ||
		!strings.Contains(err.Error(), "unknown documents command") {
		t.Fatalf("unknown subcommand err = %v, want unknown-command error", err)
	}
}

func TestDocumentsBackfill_FactoryError(t *testing.T) {
	want := errors.New("boom: no stack")
	factory := func(context.Context) (*backfillDeps, error) { return nil, want }
	if err := runDocumentsCommand(context.Background(), []string{"backfill"}, nil, factory); !errors.Is(err, want) {
		t.Fatalf("factory error = %v, want %v", err, want)
	}
}

func TestDocumentsBackfill_Success(t *testing.T) {
	graph := &fakeGraph{}
	closed := false
	factory := func(context.Context) (*backfillDeps, error) {
		return &backfillDeps{
			graph:    graph,
			operator: "op-fallback",
			closeFn:  func() { closed = true },
			owners: func(context.Context) ([]documents.DocumentOwner, error) {
				return []documents.DocumentOwner{{Identity: "idA", DocumentID: "doc-1"}}, nil
			},
		}, nil
	}
	var out strings.Builder
	if err := runDocumentsCommand(context.Background(), []string{"backfill", "--operator", "op-cli"}, &out, factory); err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if !closed {
		t.Error("closeFn was not called (dep leak)")
	}
	// Both the map pass (Op 1) and the orphan pass (Op 2) must have issued a write.
	if len(graph.writes) != 2 {
		t.Fatalf("graph writes = %d, want 2 (map + orphan pass)", len(graph.writes))
	}
	var report struct {
		OwnersSourced   int `json:"owners_sourced"`
		EdgesFromMap    int `json:"edges_from_map"`
		OrphansAttached int `json:"orphans_attached_to_operator"`
	}
	if err := json.Unmarshal([]byte(out.String()), &report); err != nil {
		t.Fatalf("decode report %q: %v", out.String(), err)
	}
	if report.OwnersSourced != 1 || report.EdgesFromMap != 1 || report.OrphansAttached != 1 {
		t.Fatalf("report = %+v, want owners=1 edges=1 orphans=1", report)
	}
}
