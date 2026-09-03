package arcadedb

// The memory's vocabulary, and what a write is told about it.
//
// Measured on a real 107-fact memory on 2026-09-03: it held 211 entities, of which only
// THIRTY were one- or two-word names. The other 181 were descriptive phrases — "la
// lentezza percepita di Aura sui turni banali", "attribuzione di una run a pagamento" —
// and 157 of those contained no existing name at all. Nothing can be merged with them,
// because there is nothing they are a variant OF. They are coinages, one per fact, and a
// corpus of coinages cannot connect: 76 of the 107 facts shared no entity with any other.
//
// Telling the writer to reuse names does not fix it. That was tried, in writing, on a
// model that then reported having done so; it produced eight facts, thirteen entities and
// two links. A rule in prose competes with the instinct to be descriptive, and loses.
//
// So the write ANSWERS with the vocabulary instead of asking to be given it: when a fact
// mints a name the memory has never held, the reply names the closest things it already
// knows. Nothing is refused and nothing is rewritten — the writer keeps the last word,
// and gets it at the only moment the choice is still cheap.

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// vocabularyNearLimit caps how many neighbours a coinage is offered. Enough to recognise
// the right one, few enough that the reply stays readable.
const vocabularyNearLimit = 5

// CoinedEntity is an endpoint this write introduced as a NEW entity, together with the
// existing names closest to it. An empty Near is itself the answer: the memory holds
// nothing like this, so the coinage is genuinely new rather than a variant.
type CoinedEntity struct {
	Name string
	Near []string
}

// vocabularyScan reads the names the memory already holds. It runs BEFORE the write's
// entity upserts, or the new name would be in its own suggestion list.
func (c *Client) vocabularyScan(ctx context.Context) ([]string, error) {
	rows, err := c.Query(ctx, mentionEntityScanStatement+strconv.Itoa(c.memoryLimits().DigestScan), nil)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: scan vocabulary: %w", err)
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if name := rowString(row, "name"); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// coinedEntities reports which of this fact's endpoints the vocabulary does not already
// contain, each with its nearest existing names.
func coinedEntities(vocabulary []string, endpoints ...string) []CoinedEntity {
	held := make(map[string]struct{}, len(vocabulary))
	for _, name := range vocabulary {
		held[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}
	var coined []CoinedEntity
	seen := map[string]struct{}{}
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		folded := strings.ToLower(endpoint)
		if endpoint == "" {
			continue
		}
		if _, already := held[folded]; already {
			continue
		}
		if _, twice := seen[folded]; twice {
			continue
		}
		seen[folded] = struct{}{}
		coined = append(coined, CoinedEntity{Name: endpoint, Near: nearestNames(vocabulary, endpoint)})
	}
	return coined
}

// nearestNames ranks the vocabulary by how much of it the coinage already contains.
//
// The ranking is deliberately lexical and not semantic. A vector would need the coinage
// embedded on the write path, which puts a model call between the caller and its own
// write; and the failure being repaired is not subtlety of meaning but a writer who spelt
// the same thing differently, which shared words already catch. `il protocollo SSE di
// Aura` reaches `Aura` on the word, without asking anything what it means.
func nearestNames(vocabulary []string, coinage string) []string {
	want := significantTokens(coinage)
	if len(want) == 0 {
		return nil
	}
	type scored struct {
		name  string
		score float64
	}
	var ranked []scored
	for _, name := range vocabulary {
		have := significantTokens(name)
		if len(have) == 0 {
			continue
		}
		shared := 0
		for token := range have {
			if _, ok := want[token]; ok {
				shared++
			}
		}
		if shared == 0 {
			continue
		}
		// Divided by the CANDIDATE's length, not the union: a short name wholly contained
		// in a long coinage is exactly the case worth surfacing, and a Jaccard score would
		// bury it under the coinage's own extra words.
		ranked = append(ranked, scored{name: name, score: float64(shared) / float64(len(have))})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		if len(ranked[i].name) != len(ranked[j].name) {
			return len(ranked[i].name) < len(ranked[j].name)
		}
		return ranked[i].name < ranked[j].name
	})
	near := make([]string, 0, vocabularyNearLimit)
	for _, candidate := range ranked {
		if len(near) == vocabularyNearLimit {
			break
		}
		near = append(near, candidate.name)
	}
	return near
}

// significantTokens folds a name to the words worth matching on. Words of one or two
// runes are dropped because in both languages this memory is written in they are
// articles and prepositions — `di`, `il`, `la`, `of`, `a` — which every phrase shares and
// which would therefore make every phrase look related to every other.
func significantTokens(name string) map[string]struct{} {
	tokens := map[string]struct{}{}
	for _, field := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(field)) > 2 {
			tokens[field] = struct{}{}
		}
	}
	return tokens
}
