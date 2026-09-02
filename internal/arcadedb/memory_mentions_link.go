package arcadedb

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Linking is a sweep, never a write hook.
//
// The hub cap is a property of the WHOLE corpus -- an entity is excluded because
// too many facts mention it -- so it cannot be evaluated while storing one fact.
// A write-path hook would have to guess the cap and then unpick its own edges as
// the corpus grew. Rebuilding the whole edge set from the corpus is both simpler
// and the only shape that can honour a cap change, which is what makes the sweep
// idempotent: it computes the edges the current corpus and cap imply, and makes
// the graph equal to that set.

const (
	mentionEntityScanStatement = "SELECT name FROM Entity LIMIT "

	mentionFactScanStatement = "SELECT fact_key, statement, outV().name AS subject, " +
		"inV().name AS object FROM " + factEdgeType + " WHERE " + asOfCondition +
		" ORDER BY fact_key LIMIT "

	mentionEdgeScanStatement = "SELECT outV().name AS source, inV().name AS target, " +
		"fact_key FROM " + mentionsEdgeType + " LIMIT "

	// IF NOT EXISTS is belt and braces over the diff below: verified on 26.9.1, a
	// second CREATE of the same triple returns the SAME @rid and adds no row, so a
	// sweep racing another writer cannot double an edge.
	mentionCreateStatement = "CREATE EDGE " + mentionsEdgeType +
		" FROM (SELECT FROM Entity WHERE name = :source)" +
		" TO (SELECT FROM Entity WHERE name = :target)" +
		" IF NOT EXISTS SET fact_key = :fact_key"

	// DELETE FROM, not DELETE EDGE. The edge form does not parse: verified on
	// 26.9.1, `DELETE EDGE MENTIONS WHERE ...` raises CommandSQLParsingException
	// ("no viable alternative at input 'DELETE EDGE'").
	mentionDeleteStatement = "DELETE FROM " + mentionsEdgeType +
		" WHERE outV().name = :source AND inV().name = :target AND fact_key = :fact_key"
)

// MentionLinkResult is what one sweep did, in numbers rather than assertions.
type MentionLinkResult struct {
	Facts    int `json:"facts"`
	Entities int `json:"entities"`
	// Candidates is how many entities were name-shaped enough to be considered.
	Candidates int `json:"candidates"`
	// Bridges is how many survived the hub cap and actually link something.
	Bridges int `json:"bridges"`
	Cap     int `json:"cap"`
	Created int `json:"created"`
	Removed int `json:"removed"`
	// Covered is false when the corpus is larger than one scan, in which case the
	// numbers describe the scanned prefix and not the memory. Digest reports the
	// same way, for the same reason: a partial answer presented as a whole one is
	// worse than no answer.
	Covered bool `json:"covered"`
}

// mentionEdge is the edge's identity: the same triple twice is the same edge, so
// a sweep that has already run creates nothing.
type mentionEdge struct {
	Source  string
	Target  string
	FactKey string
}

// LinkMentions makes this identity's MENTIONS graph equal to what its current
// facts and the configured hub cap imply, creating what is missing and removing
// what no longer belongs.
//
// It never creates an Entity. A statement naming something the memory has never
// heard of links to nothing -- the same discipline entity_refs already applies in
// memory_reasoning.go, where an unknown reference is dropped rather than minted.
// Growing the vocabulary from prose needs a deduplication policy this memory does
// not have, and a misspelling would become a permanent second identity.
func (c *Client) LinkMentions(ctx context.Context) (MentionLinkResult, error) {
	limits := c.memoryLimits()
	scan := limits.DigestScan
	asOf := time.Now().UTC().Format(time.RFC3339)

	entityRows, err := c.Query(ctx, mentionEntityScanStatement+strconv.Itoa(scan), nil)
	if err != nil {
		return MentionLinkResult{}, fmt.Errorf("arcadedb: scan entities for mentions: %w", err)
	}
	// One row over the bound is the only honest way to tell a full corpus from a
	// truncated one: a scan that returns exactly the bound is ambiguous.
	factRows, err := c.Query(ctx, mentionFactScanStatement+strconv.Itoa(scan+1),
		map[string]any{"as_of": asOf})
	if err != nil {
		return MentionLinkResult{}, fmt.Errorf("arcadedb: scan facts for mentions: %w", err)
	}
	covered := len(factRows) <= scan
	if !covered {
		factRows = factRows[:scan]
	}

	entities := make([]string, 0, len(entityRows))
	for _, row := range entityRows {
		if name := rowString(row, "name"); name != "" {
			entities = append(entities, name)
		}
	}

	result := MentionLinkResult{
		Facts: len(factRows), Entities: len(entities), Covered: covered,
	}
	desired, stats := desiredMentionEdges(entities, factRows, limits.MentionHubShare)
	result.Candidates, result.Bridges, result.Cap = stats.candidates, stats.bridges, stats.cap

	existing, err := c.existingMentionEdges(ctx, scan)
	if err != nil {
		return MentionLinkResult{}, err
	}
	for _, edge := range sortedMentionEdges(desired) {
		if _, ok := existing[edge]; ok {
			continue
		}
		if err := c.commandMentionEdge(ctx, mentionCreateStatement, edge); err != nil {
			return result, err
		}
		result.Created++
	}
	for _, edge := range sortedMentionEdges(existing) {
		if _, ok := desired[edge]; ok {
			continue
		}
		if err := c.commandMentionEdge(ctx, mentionDeleteStatement, edge); err != nil {
			return result, err
		}
		result.Removed++
	}
	return result, nil
}

type mentionStats struct{ candidates, bridges, cap int }

// desiredMentionEdges is the whole rule in one pure function, so the graph a
// sweep will produce can be tested without a server.
func desiredMentionEdges(
	entities []string,
	factRows []map[string]any,
	share float64,
) (map[mentionEdge]struct{}, mentionStats) {
	scanner := newMentionScanner(entities)
	stats := mentionStats{
		candidates: len(scanner.names),
		cap:        hubCap(len(factRows), share),
	}

	// Two passes: the cap counts how many facts mention each name across the whole
	// corpus, so no edge can be decided until every fact has been read.
	mentioned := make([][]string, len(factRows))
	counts := map[string]int{}
	for index, row := range factRows {
		subject, object := rowString(row, "subject"), rowString(row, "object")
		names := scanner.namesIn(rowString(row, "statement"), subject, object)
		mentioned[index] = names
		for _, name := range names {
			counts[name]++
		}
	}

	edges := map[mentionEdge]struct{}{}
	bridges := map[string]struct{}{}
	for index, row := range factRows {
		factKey := rowString(row, "fact_key")
		if factKey == "" {
			continue
		}
		for _, name := range mentioned[index] {
			if counts[name] > stats.cap {
				continue
			}
			for _, endpoint := range []string{rowString(row, "subject"), rowString(row, "object")} {
				if endpoint == "" {
					continue
				}
				edges[mentionEdge{Source: endpoint, Target: name, FactKey: factKey}] = struct{}{}
				bridges[name] = struct{}{}
			}
		}
	}
	stats.bridges = len(bridges)
	return edges, stats
}

func (c *Client) existingMentionEdges(
	ctx context.Context,
	scan int,
) (map[mentionEdge]struct{}, error) {
	rows, err := c.Query(ctx, mentionEdgeScanStatement+strconv.Itoa(scan), nil)
	if err != nil {
		if isMissingTypeError(err) {
			return map[mentionEdge]struct{}{}, nil
		}
		return nil, fmt.Errorf("arcadedb: scan mention edges: %w", err)
	}
	edges := make(map[mentionEdge]struct{}, len(rows))
	for _, row := range rows {
		edges[mentionEdge{
			Source:  rowString(row, "source"),
			Target:  rowString(row, "target"),
			FactKey: rowString(row, "fact_key"),
		}] = struct{}{}
	}
	return edges, nil
}

func (c *Client) commandMentionEdge(ctx context.Context, statement string, edge mentionEdge) error {
	_, err := c.Command(ctx, statement, map[string]any{
		"source": edge.Source, "target": edge.Target, "fact_key": edge.FactKey,
	})
	if err != nil {
		return fmt.Errorf("arcadedb: mention edge %s -> %s: %w", edge.Source, edge.Target, err)
	}
	return nil
}

// sortedMentionEdges makes a sweep emit its statements in the same order every
// time, so two runs over the same corpus are comparable and a failure part-way
// through resumes at the same place.
func sortedMentionEdges(edges map[mentionEdge]struct{}) []mentionEdge {
	out := make([]mentionEdge, 0, len(edges))
	for edge := range edges {
		out = append(out, edge)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		if out[i].Target != out[j].Target {
			return out[i].Target < out[j].Target
		}
		return out[i].FactKey < out[j].FactKey
	})
	return out
}

// isMissingTypeError lets the first sweep on a memory whose schema predates
// MENTIONS read an empty edge set instead of failing. EnsureMemorySchema creates
// the type, but a client that has not run it yet must still be able to report
// zero rather than an error.
func isMissingTypeError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "was not found")
}
