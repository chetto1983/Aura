package arcadedb

// The step from "what matched the wording" to "what is connected to it".
//
// Semantic recall ranks Fact edges and ConversationTurn vertices by hybrid
// similarity and returns them flat. That answers what the memory SAYS about a
// question and never what it is CONNECTED to: measured 2026-09-03 through the
// memory MCP, "che cosa usa Aura per la memoria a lungo termine" returned eight
// ranked rows on path "hybrid", among them `Aura -usa_memoria_a_lungo_termine->
// ArcadeDB`, while `memory_merge_entities -aveva_difetto-> Cypher-non-scrive-
// LIST-OF-MAP` -- one MENTIONS hop away and squarely on topic -- stayed
// invisible. The graph was reachable only by naming the entity first, which the
// asker cannot do when the whole point of asking is not knowing the name.
//
// Both halves already existed and nothing joined them: the hybrid ranking finds
// the facts, and FactsAbout walks the edges. This is the join, in the shape
// GraphRAG calls local search -- seed entities from the semantically ranked
// evidence, then expand each seed's own facts.
//
// It is ADDITIVE. The ranked evidence is untouched, keeps its full budget and its
// order; the neighbourhood arrives beside it under its own cap, so a question
// that was answered well before is answered identically now, with the graph added
// rather than traded against.

import (
	"context"
	"sort"
	"strings"
)

const (
	// How many entities a question may pull in. The seeds come from the TOP of a
	// score-ordered list, so a wider net does not find better nodes, only more
	// distant ones -- and every extra seed is another traversal.
	recallMaxEntitySeeds = 3
	// How many facts each seeded node carries. Enough to characterise the node;
	// small enough that three of them cannot outweigh the ranked evidence they
	// were derived from.
	recallFactsPerEntity = 5
	// Only the top of the ranking seeds. A fact ranked ninth is already weak
	// evidence for the question; its endpoints are weaker still.
	recallSeedFromTopHits = 4
)

// RecallEntityNode is one graph node the question reached, with its own facts.
type RecallEntityNode struct {
	Name string
	Kind string
	// Facts are this entity's own, direct edges -- not the ranked evidence it was
	// seeded from, which the caller already holds.
	Facts []FactHit
}

// expandRecallEntities seeds entity nodes from ranked evidence and reads each
// one's facts.
//
// It never fails the recall it decorates. A traversal that errors, an entity that
// has vanished between the ranking and the expansion, an embedder that produced a
// thin ranking: each yields fewer nodes, never an error, because the evidence the
// caller asked for is already computed and correct and must not be lost to a
// failure in the part that was only ever additional.
func (c *Client) expandRecallEntities(
	ctx context.Context,
	request RecallRequest,
	evidence []RecallEvidence,
) []RecallEntityNode {
	seeds := recallEntitySeeds(evidence)
	if len(seeds) == 0 {
		return nil
	}
	nodes := make([]RecallEntityNode, 0, len(seeds))
	for _, seed := range seeds {
		facts, err := c.FactsAbout(ctx, seed, "", recallFactsPerEntity, request.AsOf, FactsAboutDirect)
		if err != nil || len(facts) == 0 {
			continue
		}
		nodes = append(nodes, RecallEntityNode{
			Name: seed, Kind: recallEntityKind(seed, facts), Facts: facts,
		})
	}
	if len(nodes) == 0 {
		return nil
	}
	return nodes
}

// recallEntitySeeds picks the entity names the ranked facts lean on hardest.
//
// A name is scored by how many of the top hits name it, and ties break on the
// best rank that named it -- so an entity two facts agree on outranks one that a
// single higher-scoring fact mentions in passing, and the ranking still decides
// when the counts are level.
func recallEntitySeeds(evidence []RecallEvidence) []string {
	type seed struct {
		count    int
		bestRank int
	}
	seen := make(map[string]*seed, recallSeedFromTopHits*2)
	considered := 0
	for _, item := range evidence {
		if item.Kind != RecallEvidenceFact || item.Fact == nil {
			continue
		}
		if considered == recallSeedFromTopHits {
			break
		}
		considered++
		for _, name := range []string{item.Fact.Subject, item.Fact.Object} {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if existing, ok := seen[name]; ok {
				existing.count++
				continue
			}
			seen[name] = &seed{count: 1, bestRank: item.Rank}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := seen[names[i]], seen[names[j]]
		if a.count != b.count {
			return a.count > b.count
		}
		if a.bestRank != b.bestRank {
			return a.bestRank < b.bestRank
		}
		return names[i] < names[j]
	})
	return names[:min(len(names), recallMaxEntitySeeds)]
}

// recallEntityKind reads the seed's kind off whichever of its own facts names it,
// rather than spending a query on a vertex the traversal has already visited.
func recallEntityKind(seed string, facts []FactHit) string {
	for _, fact := range facts {
		if fact.Subject == seed && fact.SubjectKind != "" {
			return fact.SubjectKind
		}
		if fact.Object == seed && fact.ObjectKind != "" {
			return fact.ObjectKind
		}
	}
	return ""
}
