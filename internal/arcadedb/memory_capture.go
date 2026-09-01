package arcadedb

import (
	"context"
	"errors"
	"time"
)

// CaptureSourceKind is the closed set of direct evidence sources eligible for
// automatic durable capture.
type CaptureSourceKind string

const (
	// CaptureSourceExplicitFact identifies canonical memory_upsert_fact evidence.
	CaptureSourceExplicitFact CaptureSourceKind = "explicit_fact"
	// CaptureSourceDurableArtifact identifies an allowlisted persisted file.
	CaptureSourceDurableArtifact CaptureSourceKind = "durable_artifact"
)

// AcceptedCapture is the host-authenticated direct-evidence envelope admitted
// by the runner's ordered durability queue.
type AcceptedCapture struct {
	IdempotencyKey string
	IdentityID     string
	ActorRunID     string
	ActorRole      string
	SourceKind     CaptureSourceKind
	ConversationID string
	ToolCallID     string
	SourceRefs     []string
	Subject        string
	Predicate      string
	Object         string
	Statement      string
	ArtifactRef    string
	Confidence     float64
	ObservedAt     time.Time
	ValidFrom      time.Time
	ValidTo        time.Time
	Supersedes     bool
	TargetFactKey  string
	Sequence       uint64
}

var errAcceptedCaptureNotImplemented = errors.New("arcadedb: accepted capture sink not implemented")

// ApplyAcceptedCapture durably applies one accepted direct-evidence envelope.
func (c *Client) ApplyAcceptedCapture(ctx context.Context, capture AcceptedCapture) error {
	return applyAcceptedCapture(ctx, capture, time.Now().UTC(), c.memoryLimits(), clientMemoryBatchBackend{client: c})
}

func applyAcceptedCapture(
	context.Context,
	AcceptedCapture,
	time.Time,
	MemoryLimits,
	memoryBatchBackend,
) error {
	return errAcceptedCaptureNotImplemented
}
