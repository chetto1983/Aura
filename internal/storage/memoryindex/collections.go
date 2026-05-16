package memoryindex

// Collection is the typed discriminator for memory collection layers.
type Collection string

const (
	// CollectionWiki covers the wiki knowledge-base — Markdown pages with YAML
	// frontmatter stored in the filesystem and indexed in wiki_documents / Qdrant.
	CollectionWiki Collection = "wiki"

	// CollectionSource covers ingested source documents (PDFs, web pages) chunked
	// by page and stored in compact_memory_documents with Kind == KindSource.
	CollectionSource Collection = "source"

	// CollectionArchive covers conversation turns archived for retrieval, stored
	// in compact_memory_documents with Kind == KindArchive.
	CollectionArchive Collection = "archive"

	// CollectionProposal covers pending wiki update proposals produced by the
	// summariser, stored in compact_memory_documents with Kind == KindProposal.
	CollectionProposal Collection = "proposal"

	// CollectionUserMemory is wired Phase-O via approved proposed_updates.
	// User facts promoted through proposed_updates(kind='user_memory') are
	// written to compact_memory_documents by learning.WriteApprovedUserFact
	// and surfaced via the recall_user_memory tool.
	CollectionUserMemory Collection = "user_memory"

	// CollectionOperational stores validated operational lessons (repeated tool
	// failures promoted via the Phase-N lesson-promotion pipeline). Rows are
	// written by learning.WriteApprovedLesson when an operator approves an
	// operational_memory proposal and surfaced via the recall_operational tool.
	CollectionOperational Collection = "operational"
)

// RetrievalMode describes the search strategy used for a collection.
type RetrievalMode string

const (
	RetrievalExact  RetrievalMode = "exact"
	RetrievalFTS    RetrievalMode = "fts"
	RetrievalVector RetrievalMode = "vector"
	RetrievalHybrid RetrievalMode = "hybrid"
)

// CollectionDescriptor carries stable metadata for a memory collection.
// StorageBackend is a stable token naming the underlying SQL table or index so
// that future projection-freshness work has a reliable join key.
type CollectionDescriptor struct {
	Kind             Collection
	Label            string
	Description      string
	DefaultMode      RetrievalMode
	ParentCollection Collection // empty for root collections
	StorageBackend   string
}

// Registry is the authoritative map of Collection → CollectionDescriptor.
var Registry = map[Collection]CollectionDescriptor{
	CollectionWiki: {
		Kind:           CollectionWiki,
		Label:          "Wiki",
		Description:    "LLM-maintained knowledge base: Markdown pages with YAML frontmatter, full-text and vector search via wiki_documents / Qdrant.",
		DefaultMode:    RetrievalHybrid,
		StorageBackend: "wiki_documents",
	},
	CollectionSource: {
		Kind:           CollectionSource,
		Label:          "Source",
		Description:    "Ingested source documents (PDFs, web pages) chunked by page and indexed in compact_memory_documents.",
		DefaultMode:    RetrievalHybrid,
		StorageBackend: "compact_memory_documents",
	},
	CollectionArchive: {
		Kind:           CollectionArchive,
		Label:          "Archive",
		Description:    "Conversation turns archived for retrieval, stored in compact_memory_documents.",
		DefaultMode:    RetrievalFTS,
		StorageBackend: "compact_memory_documents",
	},
	CollectionProposal: {
		Kind:           CollectionProposal,
		Label:          "Proposal",
		Description:    "Pending wiki update proposals produced by the summariser, indexed in compact_memory_documents.",
		DefaultMode:    RetrievalFTS,
		StorageBackend: "proposed_updates",
	},
	CollectionUserMemory: {
		Kind:           CollectionUserMemory,
		Label:          "User Memory",
		Description:    "wired Phase-O via approved proposed_updates. Validated user facts and preferences stored in compact_memory_documents with Kind=user_memory.",
		DefaultMode:    RetrievalHybrid,
		StorageBackend: "compact_memory_documents",
	},
	CollectionOperational: {
		Kind:           CollectionOperational,
		Label:          "Operational",
		Description:    "Validated operational lessons (repeated tool failures) promoted via the Phase-N pipeline. Indexed in compact_memory_documents with Kind=operational.",
		DefaultMode:    RetrievalHybrid,
		StorageBackend: "compact_memory_documents",
	},
}

// Lookup returns the CollectionDescriptor for k and true, or the zero value
// and false if k is not registered.
func Lookup(k Collection) (CollectionDescriptor, bool) {
	d, ok := Registry[k]
	return d, ok
}
