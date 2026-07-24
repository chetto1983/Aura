package adaptive

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var decisionIDNamespace = uuid.MustParse("af95e1bb-b0fb-4a0a-a260-e0eb682709ae")

// DecisionIDForPoint derives one stable assignment identifier inside a Runner
// request. The request UUID remains the aggregate correlation key; reasoning
// and every tool call get distinct decision UUIDs for OPE/canary evidence.
func DecisionIDForPoint(requestID uuid.UUID, point string) uuid.UUID {
	return uuid.NewSHA1(decisionIDNamespace, []byte(requestID.String()+":"+strings.TrimSpace(point)))
}

// EventKind is persisted as part of the immutable adaptive-event protocol, so
// its values must remain stable across deployments.
type EventKind string

const (
	// EventDecision records the action selected at an adaptive decision point.
	EventDecision EventKind = "decision"
	// EventOutcome records the observed consequence of a prior decision.
	EventOutcome EventKind = "outcome"
	// EventCorrection records explicit feedback that supersedes an outcome.
	EventCorrection EventKind = "correction"
	// EventPromotion records a policy transition backed by eligible evidence.
	EventPromotion EventKind = "promotion"
	// EventRollback records a policy transition back to a safe predecessor.
	EventRollback EventKind = "rollback"
)

func (k EventKind) valid() bool {
	switch k {
	case EventDecision, EventOutcome, EventCorrection, EventPromotion, EventRollback:
		return true
	default:
		return false
	}
}

// EventParams contains the untrusted fields NewEvent validates and canonicalizes.
type EventParams struct {
	ID          uuid.UUID
	OwnerID     uuid.UUID
	AggregateID string
	DecisionID  uuid.UUID
	Kind        EventKind
	Payload     json.RawMessage
	CreatedAt   time.Time
}

// Event is the canonical immutable fact written to the adaptive ledger.
type Event struct {
	ID          uuid.UUID
	OwnerID     uuid.UUID
	AggregateID string
	DecisionID  uuid.UUID
	Kind        EventKind
	Payload     json.RawMessage
	PayloadHash []byte
	CreatedAt   time.Time
}

// NewEvent validates identifiers and hashes a canonical JSON payload.
func NewEvent(p EventParams) (Event, error) {
	switch {
	case p.ID == uuid.Nil:
		return Event{}, errors.New("adaptive event id is required")
	case p.OwnerID == uuid.Nil:
		return Event{}, errors.New("adaptive event owner_id is required")
	case strings.TrimSpace(p.AggregateID) == "":
		return Event{}, errors.New("adaptive event aggregate_id is required")
	case p.DecisionID == uuid.Nil:
		return Event{}, errors.New("adaptive event decision_id is required")
	case !p.Kind.valid():
		return Event{}, fmt.Errorf("adaptive event kind %q is invalid", p.Kind)
	}
	canonical, err := canonicalPayload(p.Payload)
	if err != nil {
		return Event{}, err
	}
	createdAt := p.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	hash := sha256.Sum256(canonical)
	return Event{
		ID:          p.ID,
		OwnerID:     p.OwnerID,
		AggregateID: strings.TrimSpace(p.AggregateID),
		DecisionID:  p.DecisionID,
		Kind:        p.Kind,
		Payload:     canonical,
		PayloadHash: hash[:],
		CreatedAt:   createdAt,
	}, nil
}

func canonicalPayload(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode adaptive payload: %w", err)
	}
	if decoder.More() {
		return nil, errors.New("adaptive payload contains multiple JSON values")
	}
	version, ok := value["schema_version"].(string)
	if !ok || strings.TrimSpace(version) == "" {
		return nil, errors.New("adaptive payload schema_version is required")
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("canonicalize adaptive payload: %w", err)
	}
	return canonical, nil
}
