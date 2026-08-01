package main

import (
	"context"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// clock is injected so a test can assert on the timestamps that were written.
type clock func() time.Time

// MemoryUpsertFactInput mirrors arcadedb.Fact minus the embedding, which the
// tool computes, and minus created_at, which is always now.
type MemoryUpsertFactInput struct {
	Subject   string `json:"subject" jsonschema:"the entity the fact is about"`
	Predicate string `json:"predicate" jsonschema:"the relation, e.g. lives_in"`
	Object    string `json:"object" jsonschema:"the entity or value the subject relates to"`
	Statement string `json:"statement" jsonschema:"the fact in natural language; this is what gets embedded and searched"`
	ValidFrom string `json:"valid_from,omitempty" jsonschema:"RFC3339 instant when the fact became true; defaults to now"`
	ValidTo   string `json:"valid_to,omitempty" jsonschema:"RFC3339 instant when the fact stopped being true; omit while it still holds"`
	// Supersedes is explicit because some predicates are single-valued
	// ("lives_in") and others are not ("likes"); guessing gets one of them wrong.
	Supersedes      bool     `json:"supersedes,omitempty" jsonschema:"close any still-valid fact with the same subject and predicate"`
	SourceRunID     string   `json:"source_run_id" jsonschema:"which run wrote this; required so everything a run produced can be found and removed"`
	SourceMemoryIDs []string `json:"source_memory_ids,omitempty" jsonschema:"ids of the messages this was derived from"`
}

// MemoryUpsertFactOutput reports what changed.
type MemoryUpsertFactOutput struct {
	Statement  string `json:"statement"`
	Superseded int    `json:"superseded" jsonschema:"how many previously-valid facts had their window closed"`
}

func addMemoryUpsertFactTool(server *mcp.Server, client *arcadedb.Client, now clock) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_upsert_fact",
		Title: "Remember a fact",
		Description: "Store one fact as a bitemporal edge between two entities. A fact is " +
			"never overwritten: when it is superseded its validity window is closed, so " +
			"both what is true now and what was true then stay answerable.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false},
	}, memoryUpsertFactHandler(client, now))
}

func memoryUpsertFactHandler(
	client *arcadedb.Client,
	now clock,
) mcp.ToolHandlerFor[MemoryUpsertFactInput, MemoryUpsertFactOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		in MemoryUpsertFactInput,
	) (*mcp.CallToolResult, MemoryUpsertFactOutput, error) {
		validFrom, err := parseOptionalTime(in.ValidFrom, "valid_from")
		if err != nil {
			return nil, MemoryUpsertFactOutput{}, err
		}
		validTo, err := parseOptionalTime(in.ValidTo, "valid_to")
		if err != nil {
			return nil, MemoryUpsertFactOutput{}, err
		}
		fact := arcadedb.Fact{
			Subject:         in.Subject,
			Predicate:       in.Predicate,
			Object:          in.Object,
			Statement:       in.Statement,
			ValidFrom:       validFrom,
			ValidTo:         validTo,
			Supersedes:      in.Supersedes,
			SourceRunID:     in.SourceRunID,
			SourceMemoryIDs: in.SourceMemoryIDs,
		}
		written, err := client.UpsertFact(ctx, fact, now())
		if err != nil {
			return nil, MemoryUpsertFactOutput{}, fmt.Errorf("memory_upsert_fact: %w", err)
		}
		return nil, MemoryUpsertFactOutput{
			Statement:  written.Statement,
			Superseded: written.Superseded,
		}, nil
	}
}

// MemorySearchInput asks a question of the fact graph.
type MemorySearchInput struct {
	Query string `json:"query" jsonschema:"what to look for, in natural language"`
	Limit int    `json:"limit,omitempty" jsonschema:"how many facts to return; defaults to 5"`
	AsOf  string `json:"as_of,omitempty" jsonschema:"RFC3339 instant; return the facts that were valid then rather than the ones valid now"`
}

// MemorySearchHit is one fact, with the provenance needed to check it.
type MemorySearchHit struct {
	Statement       string   `json:"statement"`
	Predicate       string   `json:"predicate"`
	Subject         string   `json:"subject"`
	Object          string   `json:"object"`
	ValidFrom       string   `json:"valid_from,omitempty"`
	ValidTo         string   `json:"valid_to,omitempty" jsonschema:"absent while the fact still holds"`
	SourceRunID     string   `json:"source_run_id,omitempty"`
	SourceMemoryIDs []string `json:"source_memory_ids,omitempty"`
}

// MemorySearchOutput carries the hits.
type MemorySearchOutput struct {
	Facts []MemorySearchHit `json:"facts"`
}

func addMemorySearchTool(server *mcp.Server, client *arcadedb.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_search",
		Title: "Search facts",
		Description: "Find facts by their words, when you do not know which entity to ask " +
			"about. If you do know the entity, call memory_facts_about instead: it is exact. " +
			"Pass as_of to ask what was true at a past instant instead of what is true now.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, memorySearchHandler(client))
}

func memorySearchHandler(
	client *arcadedb.Client,
) mcp.ToolHandlerFor[MemorySearchInput, MemorySearchOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		in MemorySearchInput,
	) (*mcp.CallToolResult, MemorySearchOutput, error) {
		asOf, err := parseOptionalTime(in.AsOf, "as_of")
		if err != nil {
			return nil, MemorySearchOutput{}, err
		}
		hits, err := client.SearchFactsHybrid(ctx, in.Query, in.Limit, asOf)
		if err != nil {
			return nil, MemorySearchOutput{}, fmt.Errorf("memory_search: %w", err)
		}
		return nil, MemorySearchOutput{Facts: toHits(hits)}, nil
	}
}

func parseOptionalTime(value, field string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be an RFC3339 instant, got %q", field, value)
	}
	return parsed, nil
}

// MemoryFactsAboutInput asks the graph directly.
type MemoryFactsAboutInput struct {
	Entity    string `json:"entity" jsonschema:"the entity to read the facts of, by exact name"`
	Predicate string `json:"predicate,omitempty" jsonschema:"narrow to one relation, e.g. works_for"`
	Limit     int    `json:"limit,omitempty" jsonschema:"how many facts to return; defaults to 20"`
	AsOf      string `json:"as_of,omitempty" jsonschema:"RFC3339 instant; the facts valid then rather than now"`
}

func addMemoryFactsAboutTool(server *mcp.Server, client *arcadedb.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_facts_about",
		Title: "Facts about an entity",
		Description: "Read an entity's facts by walking its edges. Exact and cheap: prefer " +
			"this whenever the question names an entity, and fall back to memory_search only " +
			"when it does not.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, memoryFactsAboutHandler(client))
}

func memoryFactsAboutHandler(
	client *arcadedb.Client,
) mcp.ToolHandlerFor[MemoryFactsAboutInput, MemorySearchOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		in MemoryFactsAboutInput,
	) (*mcp.CallToolResult, MemorySearchOutput, error) {
		asOf, err := parseOptionalTime(in.AsOf, "as_of")
		if err != nil {
			return nil, MemorySearchOutput{}, err
		}
		hits, err := client.FactsAbout(ctx, in.Entity, in.Predicate, in.Limit, asOf)
		if err != nil {
			return nil, MemorySearchOutput{}, fmt.Errorf("memory_facts_about: %w", err)
		}
		return nil, MemorySearchOutput{Facts: toHits(hits)}, nil
	}
}

func toHits(hits []arcadedb.FactHit) []MemorySearchHit {
	out := make([]MemorySearchHit, 0, len(hits))
	for _, hit := range hits {
		out = append(out, MemorySearchHit{
			Statement:       hit.Statement,
			Predicate:       hit.Predicate,
			Subject:         hit.Subject,
			Object:          hit.Object,
			ValidFrom:       hit.ValidFrom,
			ValidTo:         hit.ValidTo,
			SourceRunID:     hit.SourceRunID,
			SourceMemoryIDs: hit.SourceMemoryIDs,
		})
	}
	return out
}
