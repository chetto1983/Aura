package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/google/uuid"
)

func TestAdaptiveBenchmarkTeeRecorderPersistsAndRereadsTypedFacts(t *testing.T) {
	ledger := newAdaptiveBenchmarkLedgerFake()
	recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
	if err != nil {
		t.Fatalf("newAdaptiveBenchmarkTeeRecorder: %v", err)
	}
	assignment := adaptiveBenchmarkTestAssignment(t, adaptive.DomainReasoning)
	delivery := adaptiveBenchmarkTestDelivery(t, assignment)

	if _, err := recorder.RecordAssignment(t.Context(), assignment); err != nil {
		t.Fatalf("RecordAssignment: %v", err)
	}
	if _, err := recorder.RecordDelivery(
		t.Context(),
		assignment.OwnerID,
		assignment.AssignmentID,
		delivery,
	); err != nil {
		t.Fatalf("RecordDelivery: %v", err)
	}
	facts, err := recorder.PersistedFacts(
		t.Context(),
		assignment.OwnerID,
		assignment.RequestID,
		assignment.Domain,
	)
	if err != nil {
		t.Fatalf("PersistedFacts: %v", err)
	}
	if facts.Assignment.AssignmentID != assignment.AssignmentID ||
		facts.Delivery.AssignmentID != assignment.AssignmentID ||
		facts.AssignmentEvent.DecisionID != assignment.AssignmentID ||
		facts.DeliveryEvent.DecisionID != assignment.AssignmentID ||
		facts.AssignmentSequence >= facts.DeliverySequence {
		t.Fatalf("persisted facts are not linked and ordered: %#v", facts)
	}
	if ledger.deliveryAssignmentID != assignment.AssignmentID {
		t.Fatalf(
			"delegate assignment ID = %s, want %s",
			ledger.deliveryAssignmentID,
			assignment.AssignmentID,
		)
	}
}

func TestAdaptiveBenchmarkTeeRecorderRejectsPersistedFactDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*adaptive.OutboxRecord)
	}{
		{
			name: "payload hash",
			mutate: func(record *adaptive.OutboxRecord) {
				record.PayloadHash = []byte("tampered")
			},
		},
		{
			name: "decision identity",
			mutate: func(record *adaptive.OutboxRecord) {
				record.DecisionID = uuid.Must(uuid.NewV7())
			},
		},
		{
			name: "kind",
			mutate: func(record *adaptive.OutboxRecord) {
				record.Kind = adaptive.EventOutcome
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ledger := newAdaptiveBenchmarkLedgerFake()
			recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
			if err != nil {
				t.Fatal(err)
			}
			assignment := adaptiveBenchmarkTestAssignment(
				t,
				adaptive.DomainReasoning,
			)
			delivery := adaptiveBenchmarkTestDelivery(t, assignment)
			if _, err := recorder.RecordAssignment(
				t.Context(),
				assignment,
			); err != nil {
				t.Fatal(err)
			}
			if _, err := recorder.RecordDelivery(
				t.Context(),
				assignment.OwnerID,
				assignment.AssignmentID,
				delivery,
			); err != nil {
				t.Fatal(err)
			}
			test.mutate(&ledger.records[1])

			if _, err := recorder.PersistedFacts(
				t.Context(),
				assignment.OwnerID,
				assignment.RequestID,
				assignment.Domain,
			); err == nil {
				t.Fatal("PersistedFacts accepted drifted ledger fact")
			}
		})
	}
}

func TestAdaptiveBenchmarkTeeRecorderRejectsConflictingDelivery(t *testing.T) {
	ledger := newAdaptiveBenchmarkLedgerFake()
	recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
	if err != nil {
		t.Fatal(err)
	}
	assignment := adaptiveBenchmarkTestAssignment(t, adaptive.DomainReasoning)
	delivery := adaptiveBenchmarkTestDelivery(t, assignment)
	if _, err := recorder.RecordAssignment(t.Context(), assignment); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.RecordDelivery(
		t.Context(),
		assignment.OwnerID,
		assignment.AssignmentID,
		delivery,
	); err != nil {
		t.Fatal(err)
	}
	conflict := delivery
	conflict.ResultCount = 1
	if _, err := recorder.RecordDelivery(
		t.Context(),
		assignment.OwnerID,
		assignment.AssignmentID,
		conflict,
	); !errors.Is(err, adaptive.ErrPayloadConflict) {
		t.Fatalf("conflicting delivery error = %v", err)
	}
}

func TestAdaptiveBenchmarkTeeRecorderFailsClosedOnInvalidOperations(
	t *testing.T,
) {
	if _, err := newAdaptiveBenchmarkTeeRecorder(nil); err == nil {
		t.Fatal("constructed recorder without delegate")
	}
	var nilRecorder *adaptiveBenchmarkTeeRecorder
	assignment := adaptiveBenchmarkTestAssignment(
		t,
		adaptive.DomainReasoning,
	)
	delivery := adaptiveBenchmarkTestDelivery(t, assignment)
	if _, err := nilRecorder.RecordAssignment(
		t.Context(),
		assignment,
	); err == nil {
		t.Fatal("nil recorder accepted assignment")
	}
	if _, err := nilRecorder.RecordDelivery(
		t.Context(),
		assignment.OwnerID,
		assignment.AssignmentID,
		delivery,
	); err == nil {
		t.Fatal("nil recorder accepted delivery")
	}
	if _, err := nilRecorder.PersistedFacts(
		t.Context(),
		assignment.OwnerID,
		assignment.RequestID,
		assignment.Domain,
	); err == nil {
		t.Fatal("nil recorder returned facts")
	}

	ledger := newAdaptiveBenchmarkLedgerFake()
	recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.RecordAssignment(
		t.Context(),
		adaptive.Assignment{},
	); err == nil {
		t.Fatal("recorder accepted invalid assignment")
	}
	if _, err := recorder.RecordDelivery(
		t.Context(),
		assignment.OwnerID,
		assignment.AssignmentID,
		delivery,
	); err == nil {
		t.Fatal("recorder accepted delivery without assignment")
	}
	if _, err := recorder.RecordAssignment(
		t.Context(),
		assignment,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.RecordAssignment(
		t.Context(),
		assignment,
	); err != nil {
		t.Fatalf("exact assignment retry: %v", err)
	}
	conflict := assignment
	conflict.ModelID = "conflicting-model"
	if _, err := recorder.RecordAssignment(
		t.Context(),
		conflict,
	); !errors.Is(err, adaptive.ErrPayloadConflict) {
		t.Fatalf("conflicting assignment error = %v", err)
	}
}

func TestAdaptiveBenchmarkTeeRecorderClassifiesMissingFactsAndStoreFailure(
	t *testing.T,
) {
	ledger := newAdaptiveBenchmarkLedgerFake()
	recorder, err := newAdaptiveBenchmarkTeeRecorder(ledger)
	if err != nil {
		t.Fatal(err)
	}
	assignment := adaptiveBenchmarkTestAssignment(
		t,
		adaptive.DomainReasoning,
	)
	if _, err := recorder.PersistedFacts(
		t.Context(),
		assignment.OwnerID,
		assignment.RequestID,
		assignment.Domain,
	); !errors.Is(err, errAdaptiveBenchmarkAssignmentFactMissing) {
		t.Fatalf("missing assignment error = %v", err)
	}
	if _, err := recorder.RecordAssignment(
		t.Context(),
		assignment,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.PersistedFacts(
		t.Context(),
		assignment.OwnerID,
		assignment.RequestID,
		assignment.Domain,
	); !errors.Is(err, errAdaptiveBenchmarkDeliveryFactMissing) {
		t.Fatalf("missing delivery error = %v", err)
	}
	delivery := adaptiveBenchmarkTestDelivery(t, assignment)
	if _, err := recorder.RecordDelivery(
		t.Context(),
		assignment.OwnerID,
		assignment.AssignmentID,
		delivery,
	); err != nil {
		t.Fatal(err)
	}
	storeErr := errors.New("store unavailable")
	ledger.listErr = storeErr
	if _, err := recorder.PersistedFacts(
		t.Context(),
		assignment.OwnerID,
		assignment.RequestID,
		assignment.Domain,
	); !errors.Is(err, storeErr) {
		t.Fatalf("store error = %v", err)
	}
	if _, err := recorder.PersistedFacts(
		t.Context(),
		uuid.Nil,
		assignment.RequestID,
		assignment.Domain,
	); err == nil {
		t.Fatal("invalid fact scope was accepted")
	}
}

type adaptiveBenchmarkLedgerFake struct {
	assignments          map[uuid.UUID]adaptive.Assignment
	records              []adaptive.OutboxRecord
	deliveryAssignmentID uuid.UUID
	listErr              error
}

func newAdaptiveBenchmarkLedgerFake() *adaptiveBenchmarkLedgerFake {
	return &adaptiveBenchmarkLedgerFake{
		assignments: make(map[uuid.UUID]adaptive.Assignment),
	}
}

func (ledger *adaptiveBenchmarkLedgerFake) RecordAssignment(
	_ context.Context,
	assignment adaptive.Assignment,
) (int64, error) {
	event, err := adaptive.NewAssignmentEvent(assignment)
	if err != nil {
		return 0, err
	}
	ledger.assignments[assignment.AssignmentID] = assignment
	return ledger.append(event), nil
}

func (ledger *adaptiveBenchmarkLedgerFake) RecordDelivery(
	_ context.Context,
	ownerID uuid.UUID,
	assignmentID uuid.UUID,
	delivery adaptive.Delivery,
) (int64, error) {
	ledger.deliveryAssignmentID = assignmentID
	assignment, ok := ledger.assignments[assignmentID]
	if !ok || assignment.OwnerID != ownerID {
		return 0, adaptive.ErrAssignmentNotFound
	}
	event, err := adaptive.NewDeliveryEvent(assignment, delivery)
	if err != nil {
		return 0, err
	}
	return ledger.append(event), nil
}

func (ledger *adaptiveBenchmarkLedgerFake) ListAggregate(
	_ context.Context,
	ownerID uuid.UUID,
	aggregateID string,
) ([]adaptive.OutboxRecord, error) {
	if ledger.listErr != nil {
		return nil, ledger.listErr
	}
	var result []adaptive.OutboxRecord
	for _, record := range ledger.records {
		if record.OwnerID == ownerID && record.AggregateID == aggregateID {
			cloned := record
			cloned.Payload = append([]byte(nil), record.Payload...)
			cloned.PayloadHash = append([]byte(nil), record.PayloadHash...)
			result = append(result, cloned)
		}
	}
	return result, nil
}

func (ledger *adaptiveBenchmarkLedgerFake) append(
	event adaptive.Event,
) int64 {
	for _, record := range ledger.records {
		if record.ID == event.ID {
			if bytes.Equal(record.PayloadHash, event.PayloadHash) {
				return record.Sequence
			}
			return 0
		}
	}
	sequence := int64(len(ledger.records) + 1)
	ledger.records = append(ledger.records, adaptive.OutboxRecord{
		ID: event.ID, OwnerID: event.OwnerID,
		AggregateID: event.AggregateID, Sequence: sequence,
		DecisionID: event.DecisionID, Kind: event.Kind,
		Payload:     append([]byte(nil), event.Payload...),
		PayloadHash: append([]byte(nil), event.PayloadHash...),
		CreatedAt:   event.CreatedAt,
	})
	return sequence
}

func adaptiveBenchmarkTestAssignment(
	t *testing.T,
	domain adaptive.Domain,
) adaptive.Assignment {
	t.Helper()
	return adaptiveBenchmarkTestAssignmentFor(
		t,
		uuid.Must(uuid.NewV7()),
		uuid.Must(uuid.NewV7()),
		domain,
	)
}

func adaptiveBenchmarkTestAssignmentFor(
	t *testing.T,
	ownerID uuid.UUID,
	requestID uuid.UUID,
	domain adaptive.Domain,
) adaptive.Assignment {
	t.Helper()
	point := adaptiveBenchmarkTestPoint(domain)
	assignmentID, err := adaptive.AssignmentIDForPoint(
		ownerID,
		requestID,
		point,
		1,
	)
	if err != nil {
		t.Fatal(err)
	}
	eligibleActions := []string{"candidate", adaptive.StaticActionID}
	recommendedAction := "candidate"
	probabilities := []adaptive.ActionProbability{
		{ActionID: "candidate", Probability: 0},
		{ActionID: adaptive.StaticActionID, Probability: 1},
	}
	if domain == adaptive.DomainMemoryRecall {
		eligibleActions = []string{
			adaptive.StaticActionID,
			"long_term_top_4",
		}
		recommendedAction = "long_term_top_4"
		probabilities = []adaptive.ActionProbability{
			{ActionID: adaptive.StaticActionID, Probability: 1},
			{ActionID: "long_term_top_4", Probability: 0},
		}
	}
	return adaptive.Assignment{
		SchemaVersion: adaptive.SchemaVersion2,
		AssignmentID:  assignmentID, OwnerID: ownerID,
		RequestID: requestID, Domain: domain, Point: point,
		PointOrdinal: 1, PolicyEpoch: 1,
		PolicyVersion:  "benchmark-test-v1",
		PolicyMode:     adaptive.PolicyShadow,
		SnapshotID:     uuid.Must(uuid.NewV7()),
		SnapshotSHA256: strings.Repeat("a", 64),
		Environment:    adaptive.EvaluationOffline,
		ProviderID:     "test-provider", ModelID: "test-model",
		EligibleActions:     eligibleActions,
		EligibilitySHA256:   strings.Repeat("b", 64),
		CatalogSHA256:       strings.Repeat("c", 64),
		ChampionActionID:    adaptive.StaticActionID,
		RecommendedActionID: recommendedAction,
		IntendedActionID:    adaptive.StaticActionID,
		ActionProbabilities: probabilities,
		SelectionReason:     adaptive.SelectionShadowStatic,
		Features:            map[adaptive.FeatureKey]float64{},
	}
}

func adaptiveBenchmarkTestDelivery(
	t *testing.T,
	assignment adaptive.Assignment,
) adaptive.Delivery {
	t.Helper()
	probability := float64(1)
	revisions, err := adaptive.NewRevisionSet()
	if err != nil {
		t.Fatal(err)
	}
	return adaptive.Delivery{
		SchemaVersion:    adaptive.SchemaVersion2,
		AssignmentID:     assignment.AssignmentID,
		IntendedActionID: adaptive.StaticActionID,
		ActualActionID:   adaptive.StaticActionID,
		Status:           adaptive.DeliverySuccess,
		ExposureKnown:    true, ExposureProbability: &probability,
		ResultIDs: []adaptive.ResultID{},
		Revisions: revisions, EffectiveLimits: map[string]int{},
	}
}

func adaptiveBenchmarkTestPoint(
	domain adaptive.Domain,
) adaptive.DecisionPoint {
	switch domain {
	case adaptive.DomainReasoning:
		return adaptive.PointReasoning
	case adaptive.DomainToolDiscovery:
		return adaptive.PointToolDiscovery
	case adaptive.DomainSkillRouting:
		return adaptive.PointSkillRouting
	case adaptive.DomainKnowledge:
		return adaptive.PointKnowledge
	case adaptive.DomainMemoryRecall:
		return adaptive.PointMemoryRecall
	default:
		return ""
	}
}
