package documents

// DocumentStatus is the logical document lifecycle state.
//
// The set is closed by aura.documents_status_check (migration 0093). A value absent
// from that CHECK cannot be stored, so this enum and the constraint move together;
// TestDocumentVocabulariesMatchTheDatabase is what enforces it.
type DocumentStatus string

const (
	// DocumentStatusAccepted means the logical document exists before its bytes are stored.
	DocumentStatusAccepted DocumentStatus = "accepted"
	// DocumentStatusStored means the raw bytes are in object storage and hashed.
	DocumentStatusStored DocumentStatus = "stored"
	// DocumentStatusQueued means pipeline work has been queued.
	DocumentStatusQueued DocumentStatus = "queued"
	// DocumentStatusConverting means the extractor is turning the raw bytes into a document.
	DocumentStatusConverting DocumentStatus = "converting"
	// DocumentStatusChunking means the converted document is being split into passages.
	DocumentStatusChunking DocumentStatus = "chunking"
	// DocumentStatusEmbedding means passages are being embedded.
	DocumentStatusEmbedding DocumentStatus = "embedding"
	// DocumentStatusProjecting means embeddings are being written to the search index.
	DocumentStatusProjecting DocumentStatus = "projecting"
	// DocumentStatusReady means the active document version is searchable.
	DocumentStatusReady DocumentStatus = "ready"
	// DocumentStatusFailed means the latest pipeline attempt failed and may be retried.
	DocumentStatusFailed DocumentStatus = "failed"
	// DocumentStatusDeadLetter means the pipeline exhausted its retries.
	DocumentStatusDeadLetter DocumentStatus = "dead_letter"
	// DocumentStatusDeleting means deletion has started but cleanup is still in progress.
	DocumentStatusDeleting DocumentStatus = "deleting"
	// DocumentStatusDeleted means the logical document has been soft-deleted.
	DocumentStatusDeleted DocumentStatus = "deleted"
)

// DocumentVersionStatus is one immutable version's pipeline state.
//
// The set is closed by aura.document_versions_status_check (migration 0025) and is a
// DIFFERENT vocabulary from DocumentStatus: a version names parse and embed stages the
// logical document does not, and it still admits "archived", which documents no longer
// do. The two must not be conflated. This was a bare string until 2026-08-05, which is
// how the recorder came to write "processing" — a value the constraint never admitted.
type DocumentVersionStatus string

// The sixteen values aura.document_versions_status_check admits, in pipeline order.
const (
	DocumentVersionStatusUploaded       DocumentVersionStatus = "uploaded"
	DocumentVersionStatusHashCalculated DocumentVersionStatus = "hash_calculated"
	DocumentVersionStatusStored         DocumentVersionStatus = "stored"
	DocumentVersionStatusQueued         DocumentVersionStatus = "queued"
	DocumentVersionStatusParsing        DocumentVersionStatus = "parsing"
	DocumentVersionStatusParsed         DocumentVersionStatus = "parsed"
	DocumentVersionStatusChunking       DocumentVersionStatus = "chunking"
	DocumentVersionStatusChunked        DocumentVersionStatus = "chunked"
	DocumentVersionStatusEmbedding      DocumentVersionStatus = "embedding"
	DocumentVersionStatusEmbedded       DocumentVersionStatus = "embedded"
	DocumentVersionStatusIndexed        DocumentVersionStatus = "indexed"
	DocumentVersionStatusReady          DocumentVersionStatus = "ready"
	DocumentVersionStatusFailed         DocumentVersionStatus = "failed"
	DocumentVersionStatusDeleting       DocumentVersionStatus = "deleting"
	DocumentVersionStatusDeleted        DocumentVersionStatus = "deleted"
	DocumentVersionStatusArchived       DocumentVersionStatus = "archived"
)

// AllDocumentStatuses and AllDocumentVersionStatuses enumerate every declared value.
// Go has no enum reflection, so the conformance test against the database CHECK
// constraints needs these lists; a constant declared above and missing here is
// invisible to it.
var (
	AllDocumentStatuses = []DocumentStatus{
		DocumentStatusAccepted, DocumentStatusStored, DocumentStatusQueued,
		DocumentStatusConverting, DocumentStatusChunking, DocumentStatusEmbedding,
		DocumentStatusProjecting, DocumentStatusReady, DocumentStatusFailed,
		DocumentStatusDeadLetter, DocumentStatusDeleting, DocumentStatusDeleted,
	}

	AllDocumentVersionStatuses = []DocumentVersionStatus{
		DocumentVersionStatusUploaded, DocumentVersionStatusHashCalculated,
		DocumentVersionStatusStored, DocumentVersionStatusQueued,
		DocumentVersionStatusParsing, DocumentVersionStatusParsed,
		DocumentVersionStatusChunking, DocumentVersionStatusChunked,
		DocumentVersionStatusEmbedding, DocumentVersionStatusEmbedded,
		DocumentVersionStatusIndexed, DocumentVersionStatusReady,
		DocumentVersionStatusFailed, DocumentVersionStatusDeleting,
		DocumentVersionStatusDeleted, DocumentVersionStatusArchived,
	}
)
