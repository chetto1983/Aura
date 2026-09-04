package main

import (
	"strings"
)

// preloadEdgesPerEntity bounds one node's outline. A seeded entity can carry many
// facts and three of them would outweigh the ranked evidence they were seeded from;
// the point of the outline is to show the model that the node is worth opening, not
// to be the read.
const preloadEdgesPerEntity = 4

// preloadConversationLines bounds how many recalled turns ride along, and
// preloadTurnRunes bounds each one.
//
// A conversation window is several turns of full prose -- the whole reason it was
// excluded from the preload in the first place. What goes in is the ANCHOR turn
// alone, cut short: enough for the model to recognise that the exchange happened
// and open it with memory_recall, not enough to be the read.
//
// Three, because that is where the signal ended in the one ranking measured
// (2026-09-04, "cosa ti arriva dal contesto"): scores fell 0.73, 0.52, 0.46, 0.40
// and the tail was filler. One ranking is not a distribution -- if a later
// measurement shows the fourth hit carrying its weight, this is the number to move.
const (
	preloadConversationLines = 3
	preloadTurnRunes         = 240
)

// preloadFact is the one field of a fact hit the block injects: what it says.
type preloadFact struct {
	Statement string `json:"statement"`
}

// preloadTurn is one projected conversation turn.
type preloadTurn struct {
	Seq        int    `json:"seq"`
	Role       string `json:"role"`
	Content    string `json:"content"`
	OccurredAt string `json:"occurred_at"`
}

// preloadConversation is a recalled window with the turn it was anchored on.
type preloadConversation struct {
	AnchorSeq int           `json:"anchor_seq"`
	Turns     []preloadTurn `json:"turns"`
}

// preloadTriple is one edge of a seeded entity.
type preloadTriple struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    string `json:"object"`
}

// preloadEntity is a graph node the question reached, with its own edges.
type preloadEntity struct {
	Name  string          `json:"name"`
	Kind  string          `json:"kind,omitempty"`
	Facts []preloadTriple `json:"facts"`
}

// preloadEvidence is one ranked record of the union memory_recall returns.
type preloadEvidence struct {
	Fact         *preloadFact         `json:"fact,omitempty"`
	Conversation *preloadConversation `json:"conversation,omitempty"`
}

// memoryPreloadResult is the slice of memory_recall's payload the preload injects.
// Deliberately not the whole DTO: provenance, scores and validity windows are all
// real and all cost tokens in EVERY turn, while what the model needs at this point
// is what is true, what was said, and what it is connected to. The tools return the
// rest when a turn actually opens memory.
type memoryPreloadResult struct {
	Evidence  []preloadEvidence `json:"evidence"`
	Entities  []preloadEntity   `json:"entities,omitempty"`
	Retrieval struct {
		Abstained bool `json:"abstained"`
	} `json:"retrieval"`
}

// renderMemoryPreload writes the block: the ranked evidence in rank order, then a
// one-line outline per entity the question reached.
//
// Conversations are rendered BESIDE facts rather than dropped. They were dropped
// until 2026-09-04, on the reasoning that a conversation window costs tokens every
// turn -- true, and it made the preload silently empty on the questions that reach
// memory at all. Measured that day on the live graph: "cosa ti arriva dal contesto"
// recalled five records, every one of them a conversation, fact_count 0 and
// entity_count 0, so the renderer had nothing to write and the turn arrived with no
// <memory_recall> block while the retrieval had in fact succeeded. The effective
// path is `conversations` far more often than the fact-only renderer assumed.
func renderMemoryPreload(out memoryPreloadResult) string {
	var b strings.Builder
	conversations := 0
	for _, item := range out.Evidence {
		switch {
		case item.Fact != nil:
			if statement := strings.TrimSpace(item.Fact.Statement); statement != "" {
				writePreloadLine(&b, "- ", statement)
			}
		case item.Conversation != nil && conversations < preloadConversationLines:
			if line := renderPreloadTurn(*item.Conversation); line != "" {
				writePreloadLine(&b, "", line)
				conversations++
			}
		}
	}
	for _, node := range out.Entities {
		if outline := renderPreloadEntity(node); outline != "" {
			writePreloadLine(&b, "", outline)
		}
	}
	return strings.TrimSpace(b.String())
}

// writePreloadLine appends one bullet without building it first: the block is
// rebuilt on every turn, and a Builder exists precisely so the pieces are not
// concatenated into a throwaway string on the way in.
func writePreloadLine(b *strings.Builder, prefix, line string) {
	b.WriteString(prefix)
	b.WriteString(line)
	b.WriteByte('\n')
}

// renderPreloadTurn writes the anchor turn of a recalled window as one line, marked
// as something said rather than something known. The distinction has to survive into
// the prompt: a fact is the memory asserting, a turn is only the record that the
// words were used, and an agent that reads the two the same way will quote a
// question back as though it were an answer.
func renderPreloadTurn(window preloadConversation) string {
	turn, ok := preloadAnchorTurn(window)
	if !ok {
		return ""
	}
	content := strings.Join(strings.Fields(turn.Content), " ")
	if content == "" {
		return ""
	}
	if runes := []rune(content); len(runes) > preloadTurnRunes {
		content = strings.TrimSpace(string(runes[:preloadTurnRunes])) + "…"
	}
	label := strings.TrimSpace(turn.Role)
	if label == "" {
		label = "turn"
	}
	if day := preloadDay(turn.OccurredAt); day != "" {
		label = day + " " + label
	}
	return "- said [" + label + "] " + content
}

// preloadAnchorTurn picks the turn the window was ranked on. The window carries its
// neighbours for context the tools will return in full; injecting them all is the
// cost that kept conversations out of the block to begin with.
func preloadAnchorTurn(window preloadConversation) (preloadTurn, bool) {
	for _, turn := range window.Turns {
		if turn.Seq == window.AnchorSeq {
			return turn, true
		}
	}
	if len(window.Turns) == 0 {
		return preloadTurn{}, false
	}
	return window.Turns[len(window.Turns)-1], true
}

// preloadDay keeps the date and drops the clock: which day something was said places
// it against the rest of the memory, the second it was said never does.
func preloadDay(occurredAt string) string {
	fields := strings.Fields(occurredAt)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// renderPreloadEntity writes one node as triples, not prose. A statement repeats the
// sentence a fact was written as, which for a node's fourth edge is mostly words the
// model already has; `predicate -> object` says the same connection in a fraction of
// the budget and reads as the graph it is.
func renderPreloadEntity(node preloadEntity) string {
	name := strings.TrimSpace(node.Name)
	if name == "" {
		return ""
	}
	edges := make([]string, 0, preloadEdgesPerEntity)
	for _, fact := range node.Facts {
		if len(edges) == preloadEdgesPerEntity {
			break
		}
		// Read the edge from this node outwards: a fact that names the node as its
		// OBJECT points the other way, and printing it unreversed would claim the
		// node holds a relation it is on the receiving end of.
		predicate, other := fact.Predicate, fact.Object
		if strings.TrimSpace(fact.Object) == name {
			predicate, other = predicate+" (of)", fact.Subject
		}
		if predicate = strings.TrimSpace(predicate); predicate == "" {
			continue
		}
		if other = strings.TrimSpace(other); other == "" {
			continue
		}
		edges = append(edges, predicate+" -> "+other)
	}
	if len(edges) == 0 {
		return ""
	}
	if kind := strings.TrimSpace(node.Kind); kind != "" {
		name += " (" + kind + ")"
	}
	return name + ": " + strings.Join(edges, "; ")
}
