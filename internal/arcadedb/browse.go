package arcadedb

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Browsing exists because search only answers a question you already thought to
// ask. Nothing here ranks or matches: these list what is present, so an agent
// that does not yet know a name can find one.
//
// The digest is the load-bearing one. Measured on Claude Code's own 158-note
// memory: full-text search put the right note at rank 1 in 6 cases out of 10,
// while a 137-line index held in context did it in 10 out of 10. A memory that
// must be queried is an archive; the index is what makes it a memory. Note the
// honest limit -- that 10/10 was a hand-written index, and this one is
// generated, so it is only as good as the statements that went in.

const (
	defaultEntityLimit = 50
	defaultDigestFacts = 3
	digestHookRunes    = 120
)

// EntitySummary is one name the memory knows, with how much hangs off it.
type EntitySummary struct {
	Name  string `json:"name"`
	Kind  string `json:"kind,omitempty"`
	Facts int    `json:"facts"`
}

// EntityListing is the entities the caller asked for, plus how many exist in
// total — because the listing is capped and a caller cannot otherwise tell a
// complete answer from a truncated one.
//
// Measured 2026-09-02: asking for 400 entities on a memory holding 201 returned
// exactly 100 (the Results cap) with nothing to say so, so the honest reading of
// the reply was "this memory has 100 entities" — a false conclusion about the
// caller's own memory, drawn from a partial result presented as a whole. Digest
// already refuses that shape with its Covered flag; this is the same signal on
// the same surface.
type EntityListing struct {
	Entities []EntitySummary `json:"entities"`
	// Total is every entity in this identity's memory, not just the returned
	// page. Total > len(Entities) means the listing was truncated.
	Total int `json:"total"`
}

// ListEntities returns the entities holding the most facts first, because an
// entity with one stray fact is noise and an entity with twenty is a subject.
//
// The count is edges of type FACT only, and the label is load-bearing. A bare
// bothE() also counts MENTIONS, which the sweep writes by the hundred, so an
// entity named in many statements reported a fact count it did not have.
// Measured 2026-09-03 on a 68-fact memory: Neo4j read as 22 facts against 6
// real edges. The reply is what a writer consults to decide whether a name is
// already load-bearing, so inflating it teaches exactly the wrong lesson.
//
// The count is a second statement rather than a LIMIT+1 probe: +1 would say only
// "there are more", and the useful answer to "am I seeing my whole memory" is the
// number it is being compared against. Unlike Digest this is a browsing call, not
// a per-turn one, so the extra indexed count is affordable.
func (c *Client) ListEntities(ctx context.Context, limit int) (EntityListing, error) {
	limit = boundedLimit(limit, defaultEntityLimit, c.memoryLimits().Results)
	rows, err := c.Query(ctx,
		"SELECT name, kind, bothE('"+factEdgeType+"').size() AS facts FROM Entity"+
			" ORDER BY facts DESC LIMIT "+strconv.Itoa(limit), nil)
	if err != nil {
		return EntityListing{}, fmt.Errorf("arcadedb: list entities: %w", err)
	}
	out := make([]EntitySummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, EntitySummary{
			Name:  rowString(row, "name"),
			Kind:  rowString(row, "kind"),
			Facts: int(rowInt(row, "facts")),
		})
	}
	totals, err := c.Query(ctx, "SELECT count(*) AS total FROM Entity", nil)
	if err != nil {
		return EntityListing{}, fmt.Errorf("arcadedb: count entities: %w", err)
	}
	// Fall back to what was actually returned rather than to zero: a count row
	// without the column would otherwise report a memory of zero entities while
	// handing back a non-empty list, which is a worse lie than no total at all.
	listing := EntityListing{Entities: out, Total: len(out)}
	if len(totals) > 0 {
		if _, ok := totals[0]["total"]; ok {
			listing.Total = int(rowInt(totals[0], "total"))
		}
	}
	return listing, nil
}

// DigestResult is the memory's index: a block of text meant to be held in
// context, plus the counts that say how much of the memory it covers.
type DigestResult struct {
	Text     string `json:"text"`
	Entities int    `json:"entities"`
	Facts    int    `json:"facts"`
	Covered  bool   `json:"covered" jsonschema:"false when the memory is larger than the digest and search is still needed"`
}

// Digest builds the whole index in ONE round trip and groups in memory. The
// obvious shape -- list entities, then read each one's facts -- is N+1 queries
// for a thing meant to be cheap enough to build every turn.
func (c *Client) Digest(ctx context.Context, entityLimit, factsPerEntity int) (DigestResult, error) {
	limits := c.memoryLimits()
	entityLimit = boundedLimit(entityLimit, defaultEntityLimit, limits.Results)
	factsPerEntity = boundedLimit(factsPerEntity, defaultDigestFacts, limits.DigestFactsPerEntity)
	rows, err := c.Query(ctx,
		"SELECT outV().name AS subject, inV().name AS object, predicate, statement FROM "+
			factEdgeType+" WHERE "+asOfCondition+" LIMIT "+strconv.Itoa(limits.DigestScan),
		map[string]any{"as_of": time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		return DigestResult{}, fmt.Errorf("arcadedb: build digest: %w", err)
	}

	// Every fact is filed under BOTH of its endpoints. Grouping by subject alone
	// hid the graph's hubs completely: on 1002 facts about 334 clients, the
	// busiest entities were branches and towns -- always the OBJECT of a fact,
	// never the subject -- so a subject-keyed digest listed 50 arbitrary clients
	// in alphabetical order and not one of the things they had in common.
	grouped := map[string][]string{}
	order := []string{}
	add := func(entity, hook string) {
		if entity == "" {
			return
		}
		if _, seen := grouped[entity]; !seen {
			order = append(order, entity)
		}
		grouped[entity] = append(grouped[entity], hook)
	}
	for _, row := range rows {
		add(rowString(row, "subject"), digestHook(row))
		add(rowString(row, "object"), digestHookInbound(row))
	}
	// Most-connected first, and alphabetical within a tie so the block is stable
	// between turns -- an index that reshuffles itself defeats prompt caching.
	sort.SliceStable(order, func(i, j int) bool {
		if len(grouped[order[i]]) != len(grouped[order[j]]) {
			return len(grouped[order[i]]) > len(grouped[order[j]])
		}
		return order[i] < order[j]
	})

	shown := min(len(order), entityLimit)
	lines := make([]string, 0, shown)
	for _, subject := range order[:shown] {
		hooks := grouped[subject]
		if len(hooks) > factsPerEntity {
			hooks = hooks[:factsPerEntity]
		}
		lines = append(lines, "- "+subject+" — "+strings.Join(hooks, "; "))
	}
	return DigestResult{
		Text:     strings.Join(lines, "\n"),
		Entities: shown,
		Facts:    len(rows),
		Covered:  shown == len(order) && len(rows) < limits.DigestScan,
	}, nil
}

// digestHook renders one fact the way a hand-written index entry reads:
// `predicate object`, not the whole sentence. A digest of truncated prose is
// mostly connective tissue -- "was rewritten from the saved contract and
// verified against the liv…" spends 60 characters to say less than
// `verified_on production corpus`. The full sentence is one
// memory_facts_about away, and the index exists to tell you it is worth asking.
func digestHook(row map[string]any) string {
	predicate := strings.TrimSpace(rowString(row, "predicate"))
	object := strings.TrimSpace(rowString(row, "object"))
	if predicate != "" && object != "" {
		return truncateRunes(predicate+" "+object, digestHookRunes)
	}
	return truncateRunes(rowString(row, "statement"), digestHookRunes)
}

// digestHookInbound renders the same fact from the object's side. The arrow is
// there because "Venaria — assigned_to 2A S.P.A." reads as if the branch were
// assigned to the company; "← assigned_to 2A S.P.A." says which way it goes.
func digestHookInbound(row map[string]any) string {
	predicate := strings.TrimSpace(rowString(row, "predicate"))
	subject := strings.TrimSpace(rowString(row, "subject"))
	if predicate != "" && subject != "" {
		return truncateRunes("← "+predicate+" "+subject, digestHookRunes)
	}
	return truncateRunes(rowString(row, "statement"), digestHookRunes)
}

func truncateRunes(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return strings.TrimRight(string(runes[:limit]), " ,;") + "…"
}
