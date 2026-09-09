package documents

import (
	"context"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// attachNeighbours fills every returned passage's surrounding context, in one lookup.
//
// It runs AFTER ranking on purpose: only the passages that survived the limit are worth
// pulling context for, and asking before would fetch neighbours for candidates the caller
// never sees. A failure is silenced the way passageLegNames is -- the answer is correct
// without the context, and losing the whole response over an extra convenience would be a
// worse answer rather than a safer one.
func (r *HostRetriever) attachNeighbours(
	ctx context.Context,
	identityID string,
	documents []RetrievalDocument,
	neighbours int,
) {
	if neighbours <= 0 || r.PassageIndex == nil {
		return
	}
	refs := neighbourRefs(documents, neighbours)
	if len(refs) == 0 {
		return
	}
	found, err := r.PassageIndex.PassagesAt(ctx, identityID, refs)
	if err != nil || len(found) == 0 {
		return
	}
	byRef := make(map[arcadedb.PassageRef]arcadedb.PassageCandidate, len(found))
	for _, passage := range found {
		byRef[arcadedb.PassageRef{
			SearchDocumentID: passage.SearchDocumentID, Ordinal: passage.Ordinal,
		}] = passage
	}
	for documentIndex := range documents {
		documentID := documents[documentIndex].DocumentID
		for passageIndex := range documents[documentIndex].Passages {
			passage := &documents[documentIndex].Passages[passageIndex]
			passage.ContextBefore = contextRun(byRef, documentID, passage.Ordinal, neighbours, -1)
			passage.ContextAfter = contextRun(byRef, documentID, passage.Ordinal, neighbours, +1)
		}
	}
}

// neighbourRefs is every ordinal adjacent to a returned passage, deduplicated by the index
// itself. Ordinals are numbered within a document, and a document that carries passages took
// its id from the first of them -- rankDocuments walks the passage leg before the card leg
// precisely so the best-evidenced copy names the file -- so the document's id is the one its
// own ordinals are counted against.
func neighbourRefs(documents []RetrievalDocument, neighbours int) []arcadedb.PassageRef {
	refs := make([]arcadedb.PassageRef, 0, len(documents)*neighbours*2)
	for _, document := range documents {
		if document.DocumentID == "" {
			continue
		}
		for _, passage := range document.Passages {
			documentID := document.DocumentID
			for offset := 1; offset <= neighbours; offset++ {
				if passage.Ordinal-int64(offset) >= 0 {
					refs = append(refs, arcadedb.PassageRef{
						SearchDocumentID: documentID, Ordinal: passage.Ordinal - int64(offset),
					})
				}
				refs = append(refs, arcadedb.PassageRef{
					SearchDocumentID: documentID, Ordinal: passage.Ordinal + int64(offset),
				})
			}
		}
	}
	return refs
}

// contextRun walks outwards from a passage and STOPS at the first gap, so the context is
// always a contiguous run of text. Returning ordinal+2 when ordinal+1 is missing would
// present two disjoint fragments as though they ran on from each other.
func contextRun(
	byRef map[arcadedb.PassageRef]arcadedb.PassageCandidate,
	documentID string,
	from int64,
	neighbours, step int,
) []PassageContext {
	var run []PassageContext
	for offset := 1; offset <= neighbours; offset++ {
		ordinal := from + int64(step*offset)
		if ordinal < 0 {
			break
		}
		found, ok := byRef[arcadedb.PassageRef{SearchDocumentID: documentID, Ordinal: ordinal}]
		if !ok {
			break
		}
		locator := citationLocator(found)
		run = append(run, PassageContext{
			PassageID: found.PassageID, Ordinal: found.Ordinal, Text: found.Text,
			CitationToken: "document:" + found.SearchDocumentID + "@" +
				shortHash(found.RawSHA256) + "#" + locator,
			Locator: passageLocator(found),
		})
	}
	return run
}
