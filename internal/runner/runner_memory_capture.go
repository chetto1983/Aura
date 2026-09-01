package runner

import (
	"context"
	"time"

	"github.com/chetto1983/aura/internal/agent"
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

// AcceptedCapture is the runner-boundary value admitted to the durability
// queue. Sequence is assigned by MemoryCaptureQueue, never by a producer.
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
	Sequence       uint64
}

func acceptedCaptureFromEvent(_ context.Context, _ string, _ *agent.Event) (AcceptedCapture, bool) {
	return AcceptedCapture{}, false
}
