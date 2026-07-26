package adaptive

import "fmt"

// RetrievalRevisionValues carries the registered retrieval stack without
// query, result content, scores, embeddings, or content fingerprints.
type RetrievalRevisionValues struct {
	CorpusEpoch uint64
	Parser      string
	Chunker     string
	Embedding   string
	Index       string
	Reranker    string
	Retriever   string
}

// NewRetrievalDeliveryMetadata validates exact ordered chunk IDs and every
// immutable retrieval revision against catalogs frozen before execution.
func NewRetrievalDeliveryMetadata(
	registeredChunkIDs []string,
	deliveredChunkIDs []string,
	values RetrievalRevisionValues,
) ([]ResultID, RevisionSet, error) {
	catalog, err := newResultCatalog(map[ResultKind][]string{
		ResultChunk: registeredChunkIDs,
	})
	if err != nil {
		return nil, RevisionSet{}, err
	}
	results := make([]ResultID, len(deliveredChunkIDs))
	for index, id := range deliveredChunkIDs {
		results[index], err = NewChunkResultID(id, catalog)
		if err != nil {
			return nil, RevisionSet{}, err
		}
	}
	if err := validateResultIDs(results); err != nil {
		return nil, RevisionSet{}, err
	}
	fixed := map[RevisionKind]string{
		RevisionParser: values.Parser, RevisionChunker: values.Chunker,
		RevisionEmbedding: values.Embedding, RevisionIndex: values.Index,
		RevisionReranker: values.Reranker, RevisionRetriever: values.Retriever,
	}
	entries := make(map[RevisionKind][]string, len(fixed))
	for kind, value := range fixed {
		entries[kind] = []string{value}
	}
	revisionCatalog, err := newRevisionCatalog(entries)
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
		RevisionParser, RevisionChunker, RevisionEmbedding,
		RevisionIndex, RevisionReranker, RevisionRetriever,
	} {
		revision, err := NewRegisteredRevision(
			kind, fixed[kind], revisionCatalog,
		)
		if err != nil {
			return nil, RevisionSet{}, fmt.Errorf(
				"build retrieval %s revision: %w", kind, err,
			)
		}
		revisions = append(revisions, revision)
	}
	set, err := NewRevisionSet(revisions...)
	return results, set, err
}
