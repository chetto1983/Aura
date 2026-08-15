package documents

// DocumentStatus is the logical document lifecycle state.
//
// Go writes two of the values aura.documents_status_check (migration 0093) admits, and
// only these two are declared. The other ten named the in-process stages — queued,
// converting, chunking, embedding, projecting, ready, failed, dead_letter, deleting,
// deleted — and Go stopped writing any of them when that pipeline was deleted on
// 2026-08-08: extraction, chunking, embedding and indexing are CocoIndex's, and deletion
// sets the column from SQL. A constant nothing writes is not a vocabulary, it is a
// promise about a lifecycle this process no longer has.
//
// The constraint still admits all twelve, and that asymmetry is deliberate: rows written
// before the convergence carry those values, and reads decode whatever the column holds.
// What must not drift is the other direction — a constant here the constraint does not
// admit is a 23514 in production, which is what TestDocumentVocabulariesMatchTheDatabase
// enforces.
type DocumentStatus string

const (
	// DocumentStatusAccepted means the logical document exists before its bytes are stored.
	DocumentStatusAccepted DocumentStatus = "accepted"
	// DocumentStatusStored means the raw bytes are in object storage and hashed.
	DocumentStatusStored DocumentStatus = "stored"
)

// DocumentVersionStatus is one immutable version's pipeline state.
//
// It is closed by aura.document_versions_status_check (migration 0025) and is a DIFFERENT
// vocabulary from DocumentStatus — the constraint admits sixteen values, including parse
// and embed stages the logical document never named. The two must not be conflated. This
// was a bare string until 2026-08-05, which is how the recorder came to write
// "processing", a value the constraint never admitted.
//
// Only the one value Go still writes is declared, for the reason DocumentStatus gives.
type DocumentVersionStatus string

// DocumentVersionStatusStored means the version's bytes are in object storage and hashed.
const DocumentVersionStatusStored DocumentVersionStatus = "stored"

// AllDocumentStatuses and AllDocumentVersionStatuses enumerate every declared value.
// Go has no enum reflection, so the conformance test against the database CHECK
// constraints needs these lists; a constant declared above and missing here is
// invisible to it.
var (
	AllDocumentStatuses = []DocumentStatus{
		DocumentStatusAccepted, DocumentStatusStored,
	}

	AllDocumentVersionStatuses = []DocumentVersionStatus{
		DocumentVersionStatusStored,
	}
)
