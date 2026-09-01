package arcadedb

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"path"
	"slices"
	"sort"
	"strings"
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

// FactCaptureSource preserves one accepted direct-evidence envelope inside a
// fact's existing provenance record.
type FactCaptureSource struct {
	IdempotencyKey string            `json:"idempotency_key"`
	SourceKind     CaptureSourceKind `json:"source_kind"`
	SourceRefs     []string          `json:"source_refs"`
	ConversationID string            `json:"conversation_id"`
	ToolCallID     string            `json:"tool_call_id"`
	ObservedAt     time.Time         `json:"observed_at"`
	Confidence     float64           `json:"confidence"`
}

// ApplyAcceptedCapture durably applies one accepted direct-evidence envelope.
func (c *Client) ApplyAcceptedCapture(ctx context.Context, capture AcceptedCapture) error {
	return applyAcceptedCapture(
		ctx, capture, capture.ObservedAt.UTC(), c.memoryLimits(), clientMemoryBatchBackend{client: c},
	)
}

func applyAcceptedCapture(
	ctx context.Context,
	capture AcceptedCapture,
	now time.Time,
	limits MemoryLimits,
	backend memoryBatchBackend,
) error {
	capture = normalizeAcceptedCapture(capture)
	if err := validateAcceptedCaptureForGraph(capture, limits); err != nil {
		return err
	}
	if now.IsZero() {
		now = capture.ObservedAt
	}

	fact := acceptedCaptureFact(capture)
	operationType := MemoryBatchUpsertFact
	if capture.Supersedes || capture.TargetFactKey != "" {
		operationType = MemoryBatchSupersedeFact
	}
	_, err := applyMemoryBatch(
		ctx,
		MemoryBatchActor{
			IdentityID: capture.IdentityID,
			WriterRole: WriterRole(capture.ActorRole),
		},
		MemoryBatchRequest{
			IdempotencyKey: capture.IdempotencyKey,
			Operations: []MemoryBatchOperation{{
				Type: operationType,
				Fact: &fact,
			}},
		},
		now,
		limits,
		backend,
	)
	return err
}

func normalizeAcceptedCapture(capture AcceptedCapture) AcceptedCapture {
	capture.IdempotencyKey = strings.TrimSpace(capture.IdempotencyKey)
	capture.IdentityID = strings.TrimSpace(capture.IdentityID)
	capture.ActorRunID = strings.TrimSpace(capture.ActorRunID)
	capture.ActorRole = strings.TrimSpace(capture.ActorRole)
	capture.ConversationID = strings.TrimSpace(capture.ConversationID)
	capture.ToolCallID = strings.TrimSpace(capture.ToolCallID)
	capture.Subject = strings.TrimSpace(capture.Subject)
	capture.Predicate = strings.TrimSpace(capture.Predicate)
	capture.Object = strings.TrimSpace(capture.Object)
	capture.Statement = strings.TrimSpace(capture.Statement)
	capture.ArtifactRef = strings.TrimSpace(capture.ArtifactRef)
	capture.TargetFactKey = strings.TrimSpace(capture.TargetFactKey)
	capture.ObservedAt = capture.ObservedAt.UTC()
	capture.ValidFrom = capture.ValidFrom.UTC()
	capture.ValidTo = capture.ValidTo.UTC()
	refs := make([]string, 0, len(capture.SourceRefs))
	for _, ref := range capture.SourceRefs {
		if ref = strings.TrimSpace(ref); ref != "" && !slices.Contains(refs, ref) {
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)
	capture.SourceRefs = refs
	return capture
}

func validateAcceptedCaptureForGraph(capture AcceptedCapture, limits MemoryLimits) error {
	limits = limits.normalized()
	if len(capture.IdempotencyKey) != sha256HexLength ||
		strings.ToLower(capture.IdempotencyKey) != capture.IdempotencyKey {
		return fmt.Errorf("arcadedb: capture idempotency key must be lowercase SHA-256")
	}
	if _, err := hex.DecodeString(capture.IdempotencyKey); err != nil {
		return fmt.Errorf("arcadedb: capture idempotency key must be lowercase SHA-256: %w", err)
	}
	if capture.IdentityID == "" || capture.ActorRunID == "" || capture.ConversationID == "" ||
		capture.ToolCallID == "" || capture.ObservedAt.IsZero() {
		return fmt.Errorf("arcadedb: capture is missing required direct provenance")
	}
	if capture.ActorRole != string(WriterParent) && capture.ActorRole != string(WriterWorker) {
		return fmt.Errorf("arcadedb: capture actor role must be %q or %q", WriterParent, WriterWorker)
	}
	if math.IsNaN(capture.Confidence) || math.IsInf(capture.Confidence, 0) ||
		capture.Confidence <= 0 || capture.Confidence > 1 {
		return fmt.Errorf("arcadedb: capture confidence must be in (0,1]")
	}
	if !capture.ValidFrom.IsZero() && !capture.ValidTo.IsZero() &&
		!capture.ValidTo.After(capture.ValidFrom) {
		return fmt.Errorf("arcadedb: capture valid_to must be after valid_from")
	}
	if capture.TargetFactKey != "" && len(capture.TargetFactKey) != sha256HexLength {
		return fmt.Errorf("arcadedb: capture target fact key must be SHA-256")
	}
	if len(capture.SourceRefs)+1 > limits.SourceMemoryIDs {
		return fmt.Errorf("arcadedb: capture source refs exceeds %d items", limits.SourceMemoryIDs-1)
	}
	for _, field := range []struct {
		name  string
		value string
		limit int
	}{
		{"identity", capture.IdentityID, limits.SourceRunIDRunes},
		{"actor run id", capture.ActorRunID, limits.SourceRunIDRunes},
		{"conversation id", capture.ConversationID, limits.SourceMemoryIDRunes},
		{"tool call id", capture.ToolCallID, limits.SourceMemoryIDRunes},
	} {
		if err := validateRuneLimit(field.name, field.value, field.limit); err != nil {
			return err
		}
	}
	if err := validateCaptureSourceRefs(capture, limits); err != nil {
		return err
	}

	switch capture.SourceKind {
	case CaptureSourceExplicitFact:
		if capture.ArtifactRef != "" {
			return fmt.Errorf("arcadedb: explicit fact capture cannot carry an artifact ref")
		}
		fact := acceptedCaptureFact(capture)
		if err := fact.validate(limits); err != nil {
			return fmt.Errorf("arcadedb: accepted explicit fact: %w", err)
		}
	case CaptureSourceDurableArtifact:
		if capture.Supersedes || capture.TargetFactKey != "" {
			return fmt.Errorf("arcadedb: durable artifact capture cannot supersede facts")
		}
		if capture.ArtifactRef == "" || path.Clean(capture.ArtifactRef) != capture.ArtifactRef ||
			!strings.HasPrefix(capture.ArtifactRef, "/workspace/") ||
			capture.Subject != capture.ArtifactRef || capture.Predicate != "durable_artifact" ||
			(capture.Object != "write" && capture.Object != "patch") {
			return fmt.Errorf("arcadedb: durable artifact capture has invalid structured evidence")
		}
		fact := acceptedCaptureFact(capture)
		if err := fact.validate(limits); err != nil {
			return fmt.Errorf("arcadedb: accepted durable artifact: %w", err)
		}
	default:
		return fmt.Errorf("arcadedb: capture source kind %q is not eligible", capture.SourceKind)
	}
	return nil
}

const sha256HexLength = 64

func validateCaptureSourceRefs(capture AcceptedCapture, limits MemoryLimits) error {
	required := map[string]bool{
		"conversation:" + capture.ConversationID: false,
		"tool_call:" + capture.ToolCallID:        false,
	}
	if capture.SourceKind == CaptureSourceDurableArtifact {
		required["artifact:"+capture.ArtifactRef] = false
	}
	for _, ref := range capture.SourceRefs {
		if err := validateRuneLimit("capture source ref", ref, limits.SourceMemoryIDRunes); err != nil {
			return err
		}
		if _, exists := required[ref]; exists {
			required[ref] = true
			continue
		}
		if capture.SourceKind == CaptureSourceExplicitFact && strings.HasPrefix(ref, "memory:") &&
			len(strings.TrimPrefix(ref, "memory:")) > 0 {
			continue
		}
		return fmt.Errorf("arcadedb: capture source ref %q is not allowed for %q", ref, capture.SourceKind)
	}
	for ref, found := range required {
		if !found {
			return fmt.Errorf("arcadedb: capture is missing direct source ref %q", ref)
		}
	}
	return nil
}

func acceptedCaptureFact(capture AcceptedCapture) Fact {
	statement := capture.Statement
	if capture.SourceKind == CaptureSourceDurableArtifact {
		statement = fmt.Sprintf(
			"Durable artifact %s persisted by %s.", capture.ArtifactRef, capture.Object,
		)
	}
	refs := append(slices.Clone(capture.SourceRefs), "capture:"+capture.IdempotencyKey)
	sort.Strings(refs)
	return Fact{
		Subject: capture.Subject, Predicate: capture.Predicate, Object: capture.Object,
		Statement: statement, ValidFrom: capture.ValidFrom, ValidTo: capture.ValidTo,
		Supersedes:    capture.Supersedes || capture.TargetFactKey != "",
		TargetFactKey: capture.TargetFactKey,
		Source: FactSource{
			RunID: capture.ActorRunID, MemoryIDs: refs, WriterRole: WriterRole(capture.ActorRole),
		},
	}
}
