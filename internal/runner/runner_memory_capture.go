package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/secret"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
)

var errMemoryCaptureNotImplemented = errors.New("memory capture queue not implemented")

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

// MemoryCaptureSink durably applies one accepted capture. Calls are serialized
// by MemoryCaptureQueue.
type MemoryCaptureSink interface {
	ApplyAcceptedCapture(ctx context.Context, capture AcceptedCapture) error
}

// MemoryCaptureQueueConfig bounds queue capacity, write attempts, and each sink
// attempt. Zero values select safe defaults.
type MemoryCaptureQueueConfig struct {
	Capacity     int
	MaxAttempts  int
	WriteTimeout time.Duration
	RetryDelay   time.Duration
}

// MemoryCaptureQueue is the single ordered durability queue owned by a Runner.
type MemoryCaptureQueue struct{}

// NewMemoryCaptureQueue constructs one bounded ordered queue.
func NewMemoryCaptureQueue(MemoryCaptureSink, MemoryCaptureQueueConfig) *MemoryCaptureQueue {
	return &MemoryCaptureQueue{}
}

// Accept assigns a monotonic sequence and admits one capture with backpressure.
func (*MemoryCaptureQueue) Accept(context.Context, AcceptedCapture) (uint64, error) {
	return 0, errMemoryCaptureNotImplemented
}

// AcceptedSequence returns the highest sequence admitted so far.
func (*MemoryCaptureQueue) AcceptedSequence() uint64 { return 0 }

// FlushThrough waits until every capture through sequence is durable.
func (*MemoryCaptureQueue) FlushThrough(context.Context, uint64) error {
	return errMemoryCaptureNotImplemented
}

// Close stops admission, drains accepted work, and joins the queue worker.
func (*MemoryCaptureQueue) Close(context.Context) error { return errMemoryCaptureNotImplemented }

const memoryUpsertFactModelName = "memory__memory_upsert_fact"

func acceptedCaptureFromEvent(ctx context.Context, conversationID string, ev *agent.Event) (AcceptedCapture, bool) {
	if ev == nil || ev.Actions.DiscardStreamed || ev.RequestID == uuid.Nil || ev.Timestamp.IsZero() {
		return AcceptedCapture{}, false
	}
	ti := ev.Actions.ToolInvocation
	identityID := strings.TrimSpace(identityctx.IdentityID(ctx))
	if ti == nil || ti.Event != agent.ToolInvocationEnd || ti.Status != "ok" || ti.Error != "" ||
		identityID == "" || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(ti.ToolCallID) == "" {
		return AcceptedCapture{}, false
	}
	base := AcceptedCapture{
		IdentityID: identityID, ConversationID: conversationID, ToolCallID: ti.ToolCallID,
		ObservedAt: ev.Timestamp.UTC(), Confidence: 1,
		SourceRefs: []string{"conversation:" + conversationID, "tool_call:" + ti.ToolCallID},
	}
	switch ti.ToolName {
	case memoryUpsertFactModelName:
		evidence, ok := ti.Meta[tools.MetaAcceptedFact].(tools.AcceptedFactEvidence)
		if !ok || !validCaptureActor(ev, evidence.ActorRunID, evidence.ActorRole) ||
			captureTextUnsafe(evidence.Subject, evidence.Predicate, evidence.Object, evidence.Statement) {
			return AcceptedCapture{}, false
		}
		for _, ref := range evidence.SourceMemoryIDs {
			if captureTextUnsafe(ref) {
				return AcceptedCapture{}, false
			}
			base.SourceRefs = append(base.SourceRefs, "memory:"+ref)
		}
		base.SourceKind = CaptureSourceExplicitFact
		base.ActorRunID, base.ActorRole = evidence.ActorRunID, evidence.ActorRole
		base.Subject, base.Predicate, base.Object, base.Statement =
			evidence.Subject, evidence.Predicate, evidence.Object, evidence.Statement
	case "write_file", "patch":
		evidence, ok := ti.Meta[tools.MetaDurableArtifact].(tools.DurableArtifactEvidence)
		expectedOperation := "write"
		if ti.ToolName == "patch" {
			expectedOperation = "patch"
		}
		if !ok || evidence.Operation != expectedOperation ||
			!strings.HasPrefix(evidence.Path, "/workspace/") ||
			!validCaptureActor(ev, evidence.ActorRunID, evidence.ActorRole) || captureTextUnsafe(evidence.Path) {
			return AcceptedCapture{}, false
		}
		base.SourceKind = CaptureSourceDurableArtifact
		base.ActorRunID, base.ActorRole = evidence.ActorRunID, evidence.ActorRole
		base.ArtifactRef = evidence.Path
		base.Subject, base.Predicate, base.Object = evidence.Path, "durable_artifact", evidence.Operation
		base.SourceRefs = append(base.SourceRefs, "artifact:"+evidence.Path)
	default:
		return AcceptedCapture{}, false
	}
	base.IdempotencyKey = captureIdempotencyKey(base)
	return base, true
}

func validCaptureActor(ev *agent.Event, runID, role string) bool {
	return runID != "" && runID == ev.RequestID.String() && (role == "parent" || role == "worker")
}

func captureTextUnsafe(values ...string) bool {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || secret.RedactConfigured(trimmed) != trimmed ||
			toolinvocations.RedactForLedger(trimmed, 0) != trimmed {
			return true
		}
	}
	return false
}

func captureIdempotencyKey(capture AcceptedCapture) string {
	parts := []string{
		capture.IdentityID, string(capture.SourceKind), capture.ActorRunID, capture.ActorRole,
		capture.ConversationID, capture.ToolCallID, capture.Subject, capture.Predicate,
		capture.Object, capture.Statement, capture.ArtifactRef,
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}
