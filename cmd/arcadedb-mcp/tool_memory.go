package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// clock is injected so a test can assert on the timestamps that were written.
type clock func() time.Time

// MemoryFactSource is the single provenance shape accepted and returned by the
// memory MCP.
type MemoryFactSource struct {
	RunID     string   `json:"run_id" jsonschema:"the extraction or operator run supporting this fact"`
	MemoryIDs []string `json:"memory_ids,omitempty" jsonschema:"message or memory ids supporting this fact"`
}

// MemoryUpsertFactInput mirrors arcadedb.Fact minus the embedding, which the
// tool computes, and minus created_at, which is always now.
type MemoryUpsertFactInput struct {
	UserIdentifier string `json:"user_identifier" jsonschema:"the Aura identity whose memory this is; each identity has its own database and cannot reach another's"`
	Subject        string `json:"subject" jsonschema:"the entity the fact is about"`
	SubjectKind    string `json:"subject_kind,omitempty" jsonschema:"optional entity kind, e.g. Person or Organization"`
	Predicate      string `json:"predicate" jsonschema:"the relation, e.g. lives_in"`
	Object         string `json:"object" jsonschema:"the entity or value the subject relates to"`
	ObjectKind     string `json:"object_kind,omitempty" jsonschema:"optional entity kind, e.g. Location or Organization"`
	Statement      string `json:"statement" jsonschema:"the fact in natural language; this is what gets embedded and searched"`
	ValidFrom      string `json:"valid_from,omitempty" jsonschema:"RFC3339 instant when the fact became true; defaults to now"`
	ValidTo        string `json:"valid_to,omitempty" jsonschema:"RFC3339 instant when the fact stopped being true; omit while it still holds"`
	// Supersedes is explicit because some predicates are single-valued
	// ("lives_in") and others are not ("likes"); guessing gets one of them wrong.
	Supersedes bool `json:"supersedes,omitempty" jsonschema:"close any still-valid fact with the same subject and predicate"`
	// SupersedesFactKey, when set, closes exactly the one fact it names
	// instead of resolving the subject+predicate candidate set; it comes
	// from a fact_key a prior memory_search/memory_facts_about/memory_recall
	// result returned, and is the way to disambiguate after a refused
	// correction (D-15/D-17).
	SupersedesFactKey string           `json:"supersedes_fact_key,omitempty" jsonschema:"the fact_key of the exact fact to close, taken from a prior recall result; set this to disambiguate after a refused correction"`
	Source            MemoryFactSource `json:"source" jsonschema:"required provenance supporting this fact"`
}

// MemoryUpsertFactOutput reports what changed.
type MemoryUpsertFactOutput struct {
	Statement  string `json:"statement"`
	Superseded int    `json:"superseded" jsonschema:"how many previously-valid facts had their window closed"`
	// Refused is true when Supersedes could not identify exactly one fact to
	// close -- either supersedes_fact_key named no still-valid fact, or the
	// subject+predicate resolution matched 0 or more than 1 distinct fact.
	// The call still succeeded: nothing was written, Reason explains why,
	// and Candidates carries the fact_key-bearing previews needed to retry
	// with supersedes_fact_key (D-17). Never an mcp.ToolCallError: an
	// effect-free refusal is not a failed mutation.
	Refused    bool              `json:"refused"`
	Reason     string            `json:"reason,omitempty"`
	Candidates []MemorySearchHit `json:"candidates,omitempty"`
}

func addMemoryUpsertFactTool(server *mcp.Server, tenants *tenants, now clock) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_upsert_fact",
		Title: "Remember a fact",
		Description: "Store one fact as a bitemporal edge between two entities. A fact is " +
			"never overwritten: when it is superseded its validity window is closed, so " +
			"both what is true now and what was true then stay answerable. To correct a " +
			"fact precisely, set supersedes_fact_key to the fact_key a prior recall " +
			"returned. Without it, supersedes:true resolves the subject+predicate match " +
			"itself: exactly one candidate closes; zero or more than one candidate " +
			"REFUSES -- the call still succeeds, refused is true, and candidates carries " +
			"the previews (each with its own fact_key) to retry with supersedes_fact_key.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false},
	}, memoryUpsertFactHandler(tenants, now))
}

func memoryUpsertFactHandler(
	tenants *tenants,
	now clock,
) mcp.ToolHandlerFor[MemoryUpsertFactInput, MemoryUpsertFactOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		in MemoryUpsertFactInput,
	) (*mcp.CallToolResult, MemoryUpsertFactOutput, error) {
		client, err := tenants.For(ctx, in.UserIdentifier)
		if err != nil {
			return nil, MemoryUpsertFactOutput{}, err
		}
		validFrom, err := parseOptionalTime(in.ValidFrom, "valid_from")
		if err != nil {
			return nil, MemoryUpsertFactOutput{}, err
		}
		validTo, err := parseOptionalTime(in.ValidTo, "valid_to")
		if err != nil {
			return nil, MemoryUpsertFactOutput{}, err
		}
		targetFactKey := strings.TrimSpace(in.SupersedesFactKey)
		fact := arcadedb.Fact{
			Subject:     in.Subject,
			SubjectKind: in.SubjectKind,
			Predicate:   in.Predicate,
			Object:      in.Object,
			ObjectKind:  in.ObjectKind,
			Statement:   in.Statement,
			ValidFrom:   validFrom,
			ValidTo:     validTo,
			// A supplied supersedes_fact_key always means "close this one
			// fact" (D-15), whether or not the model also set supersedes --
			// naming an exact key and forgetting the boolean must not
			// silently no-op the correction.
			Supersedes:    in.Supersedes || targetFactKey != "",
			TargetFactKey: targetFactKey,
			Source: arcadedb.FactSource{
				RunID: in.Source.RunID, MemoryIDs: in.Source.MemoryIDs,
			},
		}
		written, err := client.UpsertFact(ctx, fact, now())
		if err != nil {
			return nil, MemoryUpsertFactOutput{}, fmt.Errorf("memory_upsert_fact: %w", err)
		}
		return nil, MemoryUpsertFactOutput{
			Statement:  written.Statement,
			Superseded: written.Superseded,
			Refused:    written.Refused,
			Reason:     written.Reason,
			Candidates: toHits(written.Candidates),
		}, nil
	}
}

// MemorySearchInput asks a question of the fact graph.
type MemorySearchInput struct {
	UserIdentifier string `json:"user_identifier" jsonschema:"the Aura identity whose memory this is; each identity has its own database and cannot reach another's"`
	Query          string `json:"query" jsonschema:"what to look for, in natural language"`
	Limit          int    `json:"limit,omitempty" jsonschema:"how many facts to return; defaults to 5"`
	AsOf           string `json:"as_of,omitempty" jsonschema:"RFC3339 instant; return the facts that were valid then rather than the ones valid now"`
}

// MemorySearchHit is one fact, with the provenance needed to check it.
type MemorySearchHit struct {
	Statement   string             `json:"statement"`
	Predicate   string             `json:"predicate"`
	Subject     string             `json:"subject"`
	SubjectKind string             `json:"subject_kind,omitempty"`
	Object      string             `json:"object"`
	ObjectKind  string             `json:"object_kind,omitempty"`
	ValidFrom   string             `json:"valid_from,omitempty"`
	ValidTo     string             `json:"valid_to,omitempty" jsonschema:"absent while the fact still holds"`
	Sources     []MemoryFactSource `json:"sources"`
	// FactKey identifies this fact for a later correction: pass it back as
	// supersedes_fact_key to close exactly this edge. Empty when the fact is
	// already closed (D-15).
	FactKey string `json:"fact_key,omitempty" jsonschema:"identifies this fact for a later correction; pass it back as supersedes_fact_key"`
}

// MemorySearchOutput carries the hits.
type MemorySearchOutput struct {
	Facts     []MemorySearchHit       `json:"facts"`
	Retrieval MemoryRetrievalMetadata `json:"retrieval"`
}

// MemoryRetrievalMetadata makes fallback and abstention visible to the caller.
type MemoryRetrievalMetadata struct {
	Path      string `json:"path" jsonschema:"effective retrieval path: hybrid, lexical, or graph"`
	Abstained bool   `json:"abstained" jsonschema:"true when no fact met the retrieval contract"`
	Reason    string `json:"reason,omitempty" jsonschema:"named fallback or abstention reason"`
}

// MemoryRecallInput is the model-facing read contract. Entity selects the exact
// graph path; otherwise query selects hybrid retrieval. The path-specific tools
// remain host operations for the CLI and automatic context.
type MemoryRecallInput struct {
	UserIdentifier string `json:"user_identifier" jsonschema:"the Aura identity whose memory this is; each identity has its own database and cannot reach another's"`
	Query          string `json:"query,omitempty" jsonschema:"what to recall in natural language; required when entity is empty"`
	Entity         string `json:"entity,omitempty" jsonschema:"exact entity name when known; selects graph traversal instead of semantic search"`
	Predicate      string `json:"predicate,omitempty" jsonschema:"optional relation filter used with entity"`
	Limit          int    `json:"limit,omitempty" jsonschema:"how many facts to return"`
	AsOf           string `json:"as_of,omitempty" jsonschema:"RFC3339 instant; return facts valid then rather than now"`
}

func addMemoryRecallTool(server *mcp.Server, tenants *tenants) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_recall",
		Title: "Recall memory",
		Description: "The single deep-read operation for Aura memory. Current profile facts " +
			"are already supplied in conversation context; call this only for a fact outside " +
			"that bounded index or for a historical as_of question. Give entity when its exact " +
			"name is known; otherwise give query. Returns explicit abstention instead of an " +
			"approximate unrelated fact.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, memoryRecallHandler(tenants))
}

func memoryRecallHandler(
	tenants *tenants,
) mcp.ToolHandlerFor[MemoryRecallInput, MemorySearchOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		in MemoryRecallInput,
	) (*mcp.CallToolResult, MemorySearchOutput, error) {
		if strings.TrimSpace(in.Entity) != "" {
			return memoryFactsAboutHandler(tenants)(ctx, nil, MemoryFactsAboutInput{
				UserIdentifier: in.UserIdentifier,
				Entity:         in.Entity,
				Predicate:      in.Predicate,
				Limit:          in.Limit,
				AsOf:           in.AsOf,
			})
		}
		if strings.TrimSpace(in.Query) == "" {
			return nil, MemorySearchOutput{}, fmt.Errorf("memory_recall needs query or entity")
		}
		result, output, err := memorySearchHandler(tenants)(ctx, nil, MemorySearchInput{
			UserIdentifier: in.UserIdentifier,
			Query:          in.Query,
			Limit:          in.Limit,
			AsOf:           in.AsOf,
		})
		if err != nil {
			return nil, MemorySearchOutput{}, fmt.Errorf("memory_recall: %w", err)
		}
		return result, output, nil
	}
}

func addMemorySearchTool(server *mcp.Server, tenants *tenants) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_search",
		Title: "Search facts",
		Description: "Find facts by their words, when you do not know which entity to ask " +
			"about. If you do know the entity, call memory_facts_about instead: it is exact. " +
			"Pass as_of to ask what was true at a past instant instead of what is true now. " +
			"Returns no facts and marks retrieval.abstained when no candidate is relevant enough.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, memorySearchHandler(tenants))
}

func memorySearchHandler(
	tenants *tenants,
) mcp.ToolHandlerFor[MemorySearchInput, MemorySearchOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		in MemorySearchInput,
	) (*mcp.CallToolResult, MemorySearchOutput, error) {
		client, err := tenants.For(ctx, in.UserIdentifier)
		if err != nil {
			return nil, MemorySearchOutput{}, err
		}
		asOf, err := parseOptionalTime(in.AsOf, "as_of")
		if err != nil {
			return nil, MemorySearchOutput{}, err
		}
		result, err := client.SearchFactsHybrid(ctx, in.Query, in.Limit, asOf)
		if err != nil {
			return nil, MemorySearchOutput{}, fmt.Errorf("memory_search: %w", err)
		}
		return nil, MemorySearchOutput{
			Facts: toHits(result.Facts),
			Retrieval: MemoryRetrievalMetadata{
				Path:      result.RetrievalPath,
				Abstained: result.Abstained,
				Reason:    result.Reason,
			},
		}, nil
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
	UserIdentifier string `json:"user_identifier" jsonschema:"the Aura identity whose memory this is; each identity has its own database and cannot reach another's"`
	Entity         string `json:"entity" jsonschema:"the entity to read the facts of, by exact name"`
	Predicate      string `json:"predicate,omitempty" jsonschema:"narrow to one relation, e.g. works_for"`
	Limit          int    `json:"limit,omitempty" jsonschema:"how many facts to return; defaults to 20"`
	AsOf           string `json:"as_of,omitempty" jsonschema:"RFC3339 instant; the facts valid then rather than now"`
}

func addMemoryFactsAboutTool(server *mcp.Server, tenants *tenants) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "memory_facts_about",
		Title: "Facts about an entity",
		Description: "Read an entity's facts by walking its edges. Exact and cheap: prefer " +
			"this whenever the question names an entity, and fall back to memory_search only " +
			"when it does not.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, memoryFactsAboutHandler(tenants))
}

func memoryFactsAboutHandler(
	tenants *tenants,
) mcp.ToolHandlerFor[MemoryFactsAboutInput, MemorySearchOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		in MemoryFactsAboutInput,
	) (*mcp.CallToolResult, MemorySearchOutput, error) {
		client, err := tenants.For(ctx, in.UserIdentifier)
		if err != nil {
			return nil, MemorySearchOutput{}, err
		}
		asOf, err := parseOptionalTime(in.AsOf, "as_of")
		if err != nil {
			return nil, MemorySearchOutput{}, err
		}
		hits, err := client.FactsAbout(ctx, in.Entity, in.Predicate, in.Limit, asOf)
		if err != nil {
			return nil, MemorySearchOutput{}, fmt.Errorf("memory_facts_about: %w", err)
		}
		retrieval := MemoryRetrievalMetadata{Path: "graph"}
		if len(hits) == 0 {
			retrieval.Abstained = true
			retrieval.Reason = "no_facts"
		}
		return nil, MemorySearchOutput{Facts: toHits(hits), Retrieval: retrieval}, nil
	}
}

func toHits(hits []arcadedb.FactHit) []MemorySearchHit {
	out := make([]MemorySearchHit, 0, len(hits))
	for _, hit := range hits {
		sources := make([]MemoryFactSource, 0, len(hit.Sources))
		for _, source := range hit.Sources {
			sources = append(sources, MemoryFactSource{RunID: source.RunID, MemoryIDs: source.MemoryIDs})
		}
		out = append(out, MemorySearchHit{
			Statement:   hit.Statement,
			Predicate:   hit.Predicate,
			Subject:     hit.Subject,
			SubjectKind: hit.SubjectKind,
			Object:      hit.Object,
			ObjectKind:  hit.ObjectKind,
			ValidFrom:   hit.ValidFrom,
			ValidTo:     hit.ValidTo,
			Sources:     sources,
			FactKey:     hit.FactKey,
		})
	}
	return out
}
