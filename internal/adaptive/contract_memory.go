package adaptive

import "fmt"

// MemoryRecallResult is one ordered long-term memory reference.
type MemoryRecallResult struct {
	Kind ResultKind
	ID   string
}

// MemoryRecallRevisionValues freezes the coherent memory retrieval stack.
type MemoryRecallRevisionValues struct {
	CorpusEpoch uint64
	Retriever   string
	Reranker    string
	Embedding   string
	Index       string
}

// NewMemoryRecallDeliveryMetadata validates owner-private long-term result IDs
// and the exact retrieval revisions without accepting recalled content.
func NewMemoryRecallDeliveryMetadata(
	delivered []MemoryRecallResult,
	values MemoryRecallRevisionValues,
) ([]ResultID, RevisionSet, error) {
	results := make([]ResultID, len(delivered))
	var err error
	for index, result := range delivered {
		switch result.Kind {
		case ResultMemoryPreference:
			results[index], err = NewMemoryPreferenceResultID(result.ID)
		case ResultMemoryEntity:
			results[index], err = NewMemoryEntityResultID(result.ID)
		default:
			err = fmt.Errorf(
				"adaptive memory recall result kind %q is not long-term",
				result.Kind,
			)
		}
		if err != nil {
			return nil, RevisionSet{}, err
		}
	}
	if err := validateResultIDs(results); err != nil {
		return nil, RevisionSet{}, err
	}

	fixed := map[RevisionKind]string{
		RevisionRetriever: values.Retriever,
		RevisionReranker:  values.Reranker,
		RevisionEmbedding: values.Embedding,
		RevisionIndex:     values.Index,
	}
	entries := make(map[RevisionKind][]string, len(fixed))
	for kind, value := range fixed {
		entries[kind] = []string{value}
	}
	catalog, err := newRevisionCatalog(entries)
	if err != nil {
		return nil, RevisionSet{}, err
	}
	revisions := make([]Revision, 0, len(fixed)+1)
	corpus, err := NewCorpusRevision(values.CorpusEpoch)
	if err != nil {
		return nil, RevisionSet{}, err
	}
	revisions = append(revisions, corpus)
	for _, kind := range []RevisionKind{
		RevisionEmbedding,
		RevisionIndex,
		RevisionReranker,
		RevisionRetriever,
	} {
		revision, err := NewRegisteredRevision(kind, fixed[kind], catalog)
		if err != nil {
			return nil, RevisionSet{}, fmt.Errorf(
				"build memory recall %s revision: %w",
				kind,
				err,
			)
		}
		revisions = append(revisions, revision)
	}
	set, err := NewRevisionSet(revisions...)
	return results, set, err
}
