//go:build arcadedb_integration

// These run against a live ArcadeDB. They exist because the unit tests in this
// package assert on the SQL this package emits -- which is a restatement of the
// author's assumptions, not evidence. Every defect below was green under those
// tests and only appeared when a real database answered:
//
//   - `out.name` on an edge returns NULL instead of failing, so subject and
//     object came back blank with no diagnostic
//   - vector.neighbors() on an EDGE index yields records whose endpoints no
//     longer resolve: outV() over that subquery dies with "iRecord is null"
//   - with no temporal filter a superseded fact won at rank 1
//   - supersession filtered on the object, which is the one thing that changes,
//     so it could never fire
//
// Run: go test -tags arcadedb_integration ./internal/arcadedb/
package arcadedb

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func integrationClient(t *testing.T) *Client {
	t.Helper()
	base := os.Getenv("ARCADEDB_URL")
	if base == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("ARCADEDB_URL must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("ARCADEDB_URL not set")
	}
	client, err := New(Config{
		BaseURL:  base,
		Database: envOr("ARCADEDB_DATABASE", "aura_memory_it"),
		User:     envOr("ARCADEDB_USER", "root"),
		Password: os.Getenv("ARCADEDB_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := client.EnsureMemorySchema(context.Background()); err != nil {
		t.Fatalf("EnsureMemorySchema: %v", err)
	}
	return client
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// isolate scopes a test to its own subject and run id, and removes what it wrote.
func isolate(t *testing.T, client *Client) (subject, runID string) {
	t.Helper()
	subject = fmt.Sprintf("Subject_%s", strings.ReplaceAll(t.Name(), "/", "_"))
	runID = "it-" + subject
	cleanup := func() {
		ctx := context.Background()
		_, _ = client.Command(ctx, "DELETE FROM FACT WHERE source_run_id = :run",
			map[string]any{"run": runID})
		_, _ = client.Command(ctx, "DELETE FROM Entity WHERE name LIKE :p",
			map[string]any{"p": subject + "%"})
	}
	cleanup()
	t.Cleanup(cleanup)
	return subject, runID
}

func write(t *testing.T, client *Client, fact Fact, at time.Time) FactWrite {
	t.Helper()
	written, err := client.UpsertFact(context.Background(), fact, at)
	if err != nil {
		t.Fatalf("UpsertFact(%q): %v", fact.Statement, err)
	}
	return written
}

func TestStudioGraphSerializerReturnsFactEndpoints(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	object := subject + "_ArcadeDB"
	write(t, client, Fact{
		Subject: subject, Predicate: "uses", Object: object,
		Statement: subject + " uses ArcadeDB.", SourceRunID: runID,
	}, time.Now().UTC())

	graph, err := client.QueryStudioGraph(
		context.Background(),
		"SELECT FROM FACT WHERE source_run_id = :run LIMIT 10",
		map[string]any{"run": runID},
		10,
	)
	if err != nil {
		t.Fatalf("QueryStudioGraph: %v", err)
	}
	if len(graph.Vertices) != 2 || len(graph.Edges) != 1 {
		t.Fatalf("studio graph = %d vertices / %d edges: %+v", len(graph.Vertices), len(graph.Edges), graph)
	}
	edge := graph.Edges[0]
	if edge.RID == "" || edge.Out == "" || edge.In == "" || edge.Out == edge.In {
		t.Fatalf("edge endpoints were not decoded: %+v", edge)
	}
	if edge.Properties["predicate"] != "uses" {
		t.Fatalf("edge properties = %+v", edge.Properties)
	}
}

// The endpoints must survive retrieval. `out.name` returned NULL here and no
// test noticed, because the SQL "looked right".
func TestRetrievedFactCarriesItsEndpointsAndProvenance(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	object := subject + "_Caraglio"

	write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: object,
		Statement:       subject + " lives in Caraglio.",
		SourceRunID:     runID,
		SourceMemoryIDs: []string{"msg-7"},
	}, time.Now())

	hits, err := client.SearchFacts(context.Background(), "Caraglio", 5, time.Time{})
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	hit, ok := findBySubject(hits, subject)
	if !ok {
		t.Fatalf("fact not retrieved; hits = %+v", hits)
	}
	if hit.Subject != subject {
		t.Fatalf("subject = %q, want %q", hit.Subject, subject)
	}
	if hit.Object != object {
		t.Fatalf("object = %q, want %q", hit.Object, object)
	}
	if hit.SourceRunID != runID {
		t.Fatalf("source_run_id = %q", hit.SourceRunID)
	}
	if len(hit.SourceMemoryIDs) != 1 || hit.SourceMemoryIDs[0] != "msg-7" {
		t.Fatalf("source_memory_ids = %v", hit.SourceMemoryIDs)
	}
}

// A search with no instant must answer "what is true", not "what was ever
// true". Without the default filter the expired fact ranked first.
func TestExpiredFactIsExcludedFromAPresentTenseSearch(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	now := time.Now()

	write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Torino",
		Statement:   subject + " lives in Torino.",
		SourceRunID: runID,
		ValidFrom:   now.AddDate(-6, 0, 0), ValidTo: now.AddDate(-3, 0, 0),
	}, now)

	hits, err := client.SearchFacts(context.Background(), "Torino", 5, time.Time{})
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	if _, found := findBySubject(hits, subject); found {
		t.Fatalf("an expired fact was returned by a present-tense search: %+v", hits)
	}
}

// The whole point of the bitemporal model: the past stays answerable.
func TestAsOfReturnsWhatWasTrueThen(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	now := time.Now()
	past := now.AddDate(-5, 0, 0)

	write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Torino",
		Statement:   subject + " lives in Torino.",
		SourceRunID: runID,
		ValidFrom:   now.AddDate(-6, 0, 0), ValidTo: now.AddDate(-3, 0, 0),
	}, now)

	hits, err := client.SearchFacts(context.Background(), "Torino", 5, past)
	if err != nil {
		t.Fatalf("SearchFacts: %v", err)
	}
	hit, ok := findBySubject(hits, subject)
	if !ok {
		t.Fatalf("as_of inside the window returned nothing; hits = %+v", hits)
	}
	if !strings.Contains(hit.Statement, "Torino") {
		t.Fatalf("statement = %q", hit.Statement)
	}
}

// Supersession must close the previous window rather than delete it, and must
// match on subject+predicate: filtering on the object meant it never fired.
func TestSupersessionClosesTheWindowAndKeepsThePastQueryable(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	now := time.Now()
	moved := now.AddDate(-1, 0, 0)

	write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Torino",
		Statement:   subject + " lives in Torino.",
		SourceRunID: runID,
		ValidFrom:   now.AddDate(-6, 0, 0),
	}, now)

	written := write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Caraglio",
		Statement:   subject + " lives in Caraglio.",
		SourceRunID: runID,
		ValidFrom:   moved, Supersedes: true,
	}, now)
	if written.Superseded != 1 {
		t.Fatalf("superseded = %d, want 1", written.Superseded)
	}

	present, err := client.SearchFacts(context.Background(), "Caraglio", 5, time.Time{})
	if err != nil {
		t.Fatalf("SearchFacts(now): %v", err)
	}
	if hit, ok := findBySubject(present, subject); !ok || !strings.Contains(hit.Statement, "Caraglio") {
		t.Fatalf("present-tense answer = %+v, want Caraglio", present)
	}

	before, err := client.SearchFacts(context.Background(), "Torino", 5, now.AddDate(-3, 0, 0))
	if err != nil {
		t.Fatalf("SearchFacts(past): %v", err)
	}
	hit, ok := findBySubject(before, subject)
	if !ok || !strings.Contains(hit.Statement, "Torino") {
		t.Fatalf("the superseded fact stopped being queryable: %+v", before)
	}
	// Closed, not deleted.
	if hit.ValidTo == "" {
		t.Fatal("superseded fact has no valid_to: the window was not closed")
	}
}

// The schema must be replayable: it runs on every boot.
func TestEnsureMemorySchemaIsIdempotentAgainstALiveDatabase(t *testing.T) {
	client := integrationClient(t)
	for range 2 {
		if err := client.EnsureMemorySchema(context.Background()); err != nil {
			t.Fatalf("EnsureMemorySchema: %v", err)
		}
	}
}

func findBySubject(hits []FactHit, subject string) (FactHit, bool) {
	for _, hit := range hits {
		if hit.Subject == subject {
			return hit, true
		}
	}
	return FactHit{}, false
}

// The traversal path is the one that must be exact: when the question names the
// entity there is nothing to rank, and the answer is not allowed to be a guess.
func TestFactsAboutAnswersExactlyAndRespectsTime(t *testing.T) {
	client := integrationClient(t)
	subject, runID := isolate(t, client)
	now := time.Now()

	write(t, client, Fact{
		Subject: subject, Predicate: "works_for", Object: subject + "_PmSync",
		Statement:   subject + " works for PmSync.",
		SourceRunID: runID, ValidFrom: now.AddDate(-4, 0, 0),
	}, now)
	write(t, client, Fact{
		Subject: subject, Predicate: "lives_in", Object: subject + "_Torino",
		Statement:   subject + " lives in Torino.",
		SourceRunID: runID,
		ValidFrom:   now.AddDate(-6, 0, 0), ValidTo: now.AddDate(-3, 0, 0),
	}, now)

	all, err := client.FactsAbout(context.Background(), subject, "", 10, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout: %v", err)
	}
	if len(all) != 1 || all[0].Predicate != "works_for" {
		t.Fatalf("present-tense traversal = %+v, want only the open fact", all)
	}

	narrowed, err := client.FactsAbout(context.Background(), subject, "works_for", 10, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout(predicate): %v", err)
	}
	if len(narrowed) != 1 || narrowed[0].Object != subject+"_PmSync" {
		t.Fatalf("narrowed = %+v", narrowed)
	}

	past, err := client.FactsAbout(context.Background(), subject, "lives_in", 10, now.AddDate(-5, 0, 0))
	if err != nil {
		t.Fatalf("FactsAbout(as_of): %v", err)
	}
	if len(past) != 1 || !strings.Contains(past[0].Statement, "Torino") {
		t.Fatalf("as_of traversal = %+v", past)
	}
}
