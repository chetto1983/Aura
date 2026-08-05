package documents

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrPipelineCandidateRejected means the version drifted beneath the write: the
	// generation moved on, or the document entered deletion. It is never retried in
	// place, because the candidate was built against state that no longer exists.
	ErrPipelineCandidateRejected = errors.New("documents: pipeline candidate is stale or incoherent")
	// ErrPipelineStageUnavailable means nothing is claimable right now — an empty queue
	// and a queue whose entries are all leased are deliberately indistinguishable here.
	ErrPipelineStageUnavailable = errors.New("documents: no pipeline stage is claimable")
	// ErrPipelineStageLeaseLost means the fence stopped matching, so a successor may
	// already own this stage. The worker abandons rather than settling: writing a verdict
	// now would stamp it onto whoever holds the lease.
	ErrPipelineStageLeaseLost = errors.New("documents: pipeline stage lease lost")
	// ErrPipelineActivationRejected means the atomic swap found a precondition it did not
	// agree with — a count, an owner, or a generation. It is a refusal to publish, not a
	// failure to write: nothing was activated, and the prior active version still stands.
	ErrPipelineActivationRejected = errors.New(
		"documents: pipeline activation preconditions failed",
	)
	// ErrPipelineProjectionUnreconciled is raised instead of compensating when a
	// projected generation cannot be proven to belong to this attempt. The generation is
	// left in place for reconciliation rather than destroyed, because deleting an
	// authoritative one is unrecoverable.
	ErrPipelineProjectionUnreconciled = errors.New(
		"documents: projected generation left for reconciliation",
	)
	// ErrPipelineArtifactOwnershipUnproven is raised when an uploaded artifact cannot be
	// proven unreferenced. The object is left for reconciliation: derived-artifact keys
	// are content addressed and may be shared, so deleting one on an unproven hunch can
	// strip a live version.
	ErrPipelineArtifactOwnershipUnproven = errors.New(
		"documents: derived artifact ownership unproven, left for reconciliation",
	)
)

// ArtifactLedgerQuery addresses one derived-artifact key inside a single identity.
type ArtifactLedgerQuery struct {
	IdentityID string
	Bucket     string
	ObjectKey  string
}

// PipelinePersistence is the PostgreSQL half of the ingestion pipeline, split from the
// worker so stage ordering and compensation can be exercised without a database. Every
// method is owner-scoped: the identity travels in the request rather than in a WHERE
// clause a caller could omit.
type PipelinePersistence interface {
	ReserveCandidateVersion(context.Context, CandidateVersionRequest) (CandidateVersion, error)
	BindJobCandidate(context.Context, JobCandidateBinding) error
	RecordDerivedArtifact(context.Context, DerivedArtifactRequest) (string, error)
	CountArtifactLedgerOwners(context.Context, ArtifactLedgerQuery) (int, error)
	SaveCandidate(context.Context, CandidateGeneration) error
	UpsertStage(context.Context, StageUpsert) (PipelineStageRecord, error)
	ClaimStage(context.Context, StageClaim) (PipelineStageRecord, error)
	HeartbeatStage(context.Context, StageFence) error
	CompleteStage(context.Context, StageCompletion) error
	FailStage(context.Context, StageFailure) error
	ActivateCandidate(context.Context, ActivationRequest) error
}

// CandidateVersionRequest asks for the version that owns these exact bytes. The
// reservation is keyed on the raw SHA-256, so re-uploading identical content resolves to
// the version already on record instead of minting a second one.
//
// It names the object by (bucket, key) rather than by a storage-object id because the
// reservation WRITES that ledger row itself: the object, the version and the asset
// binding are one indivisible fact about an upload, and a caller that created the object
// first could attach freshly written bytes to an already-recorded version.
type CandidateVersionRequest struct {
	IdentityID         string
	DocumentID         string
	AssetID            string
	ObjectBucket       string
	ObjectKey          string
	ObjectETag         string
	RetentionClass     string
	SHA1               string
	SHA256             string
	ContentType        string
	SizeBytes          int64
	Status             string
	ChunkingConfigHash string
	PipelineConfigHash string
}

// CandidateVersion is the reserved immutable version.
//
// The two booleans are NOT interchangeable. Replayed reports only that the bytes were
// already on record — the version carrying them may still be `processing`. ReplayedActive
// additionally reports that this version is the document's published one, and it is the
// only one of the two that may be read as permission to skip processing: acting on
// Replayed alone marks a document searchable while nothing has been indexed for it.
type CandidateVersion struct {
	ID                 string
	IdentityID         string
	DocumentID         string
	SearchDocumentID   string
	AssetID            string
	StorageObjectID    string
	VersionNumber      int
	PipelineGeneration int64
	SHA1               string
	SHA256             string
	ContentType        string
	SizeBytes          int64
	Status             string
	Replayed           bool
	ReplayedActive     bool
}

// CandidateGeneration is one complete unpublished generation — passages, embeddings and
// the fingerprints of everything that produced them. It is written and activated as a
// unit so a half-written generation can never become the active one.
type CandidateGeneration struct {
	IdentityID            string
	DocumentID            string
	SearchDocumentID      string
	VersionID             string
	VersionNumber         int
	RawSHA256             string
	PipelineGeneration    int64
	Passages              []Passage
	Embeddings            [][]float64
	EmbeddingModel        string
	EmbeddingVersion      string
	EmbeddingFingerprint  string
	ProjectionFingerprint string
	ProjectionCount       int
}

// JobCandidateBinding ties a claimed job to the version it is allowed to write. The
// lease generation travels with it so a stalled worker that wakes up late binds nothing.
type JobCandidateBinding struct {
	IdentityID         string
	JobID              string
	AssetID            string
	DocumentID         string
	VersionID          string
	WorkerID           string
	LeaseGeneration    int64
	PipelineGeneration int64
	Stage              PipelineStage
}

// DerivedArtifactRequest registers a stage output in the storage ledger. The (bucket,
// object key) pair is unique schema-wide and the key is content addressed, so the same
// key can legitimately be reached by more than one version.
type DerivedArtifactRequest struct {
	IdentityID         string
	DocumentID         string
	VersionID          string
	PipelineGeneration int64
	Bucket             string
	ObjectKey          string
	Kind               string
	SHA256             string
	SizeBytes          int64
	ContentType        string
	RetentionClass     string
}

// CandidateKey names one generation exactly. Reads through it deliberately ignore the
// document's active version: it exists to verify a candidate BEFORE activation, when
// none of its rows is active yet.
type CandidateKey struct {
	IdentityID         string
	VersionID          string
	PipelineGeneration int64
}

// CandidateEmbedding is a ledger row, never a vector: it records model, dimension and
// projection state plus a pointer into the vector index, so the vectors themselves stay
// reachable only through the projection store.
type CandidateEmbedding struct {
	ID                    string
	IdentityID            string
	DocumentID            string
	VersionID             string
	PassageID             string
	PipelineGeneration    int64
	EmbeddingModel        string
	EmbeddingVersion      string
	EmbeddingDimension    int
	EmbeddingFingerprint  string
	ProjectionFingerprint string
	ProjectionStatus      string
	ProjectedAt           time.Time
	Active                bool
}

// PipelineStageRecord is the durable state of one stage. InputFingerprint is what makes
// a stage cacheable: an identical fingerprint means the recorded artifact can be reused
// instead of re-running conversion, embedding or projection.
type PipelineStageRecord struct {
	ID                      string
	IdentityID              string
	DocumentID              string
	VersionID               string
	Stage                   PipelineStage
	InputFingerprint        string
	ProducerVersion         string
	PipelineGeneration      int64
	ArtifactStorageObjectID string
	ArtifactObjectKey       string
	ArtifactSHA256          string
	ArtifactSizeBytes       int64
	Status                  string
	AttemptCount            int
	MaxAttempts             int
	LeaseGeneration         int64
	LockedBy                string
	LockedUntil             time.Time
	NextAttemptAt           time.Time
	DiagnosticState         map[string]any
	CompletedAt             time.Time
}

// StageUpsert creates or re-reads a stage row for a fingerprint. It is idempotent so a
// resumed run converges on the existing row rather than forking a second attempt.
type StageUpsert struct {
	IdentityID         string
	DocumentID         string
	VersionID          string
	Stage              PipelineStage
	InputFingerprint   string
	ProducerVersion    string
	PipelineGeneration int64
	MaxAttempts        int
	NextAttemptAt      time.Time
	DiagnosticState    map[string]any
}

// StageClaim leases a stage to one worker, incrementing its fence generation so any
// earlier holder is locked out of every subsequent write.
type StageClaim struct {
	IdentityID    string
	StageID       string
	Stage         PipelineStage
	WorkerID      string
	LeaseDuration time.Duration
}

// StageFence is the credential every mutating stage call must present. The SQL matches
// on all of it plus an unexpired lease, so a worker resuming after a stall writes
// nothing instead of overwriting its successor's progress.
type StageFence struct {
	IdentityID      string
	StageID         string
	WorkerID        string
	AttemptCount    int
	LeaseGeneration int64
	LeaseDuration   time.Duration
}

// StageCompletion records the artifact a stage produced. The SHA-256 and size are stored
// so a later cache hit can verify the bytes it loads rather than trusting the pointer.
type StageCompletion struct {
	StageFence
	ArtifactStorageObjectID string
	ArtifactObjectKey       string
	ArtifactSHA256          string
	ArtifactSizeBytes       int64
	DiagnosticState         map[string]any
}

// StageFailure demotes a stage. Retryable separates a transient fault from a permanent
// one, and the message is pre-redacted because it is persisted: raw backend error text
// must never reach a row.
type StageFailure struct {
	StageFence
	Retryable       bool
	ErrorClass      string
	ErrorCode       string
	RedactedMessage string
	NextAttemptAt   time.Time
}

// ActivationRequest publishes a generation. The expected counts are handed to the SQL
// rather than pre-checked in Go, where any count would be stale by the time the swap
// runs; a count the statement disagrees with rejects the activation outright.
type ActivationRequest struct {
	IdentityID              string
	DocumentID              string
	VersionID               string
	AssetID                 string
	JobID                   string
	LockedBy                string
	LeaseGeneration         int64
	PipelineGeneration      int64
	ExpectedPassageCount    int
	ExpectedEmbeddingCount  int
	ExpectedProjectionCount int
	EventMessage            string
}
