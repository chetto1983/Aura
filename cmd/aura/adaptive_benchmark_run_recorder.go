package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/google/uuid"
)

var (
	errAdaptiveBenchmarkAssignmentFactMissing = errors.New(
		"adaptive benchmark assignment fact is missing",
	)
	errAdaptiveBenchmarkDeliveryFactMissing = errors.New(
		"adaptive benchmark delivery fact is missing",
	)
)

type adaptiveBenchmarkEventRecorder interface {
	RecordAssignment(
		context.Context,
		adaptive.Assignment,
	) (int64, error)
	RecordDelivery(
		context.Context,
		uuid.UUID,
		uuid.UUID,
		adaptive.Delivery,
	) (int64, error)
}

type adaptiveBenchmarkLedger interface {
	adaptiveBenchmarkEventRecorder
	ListAggregate(
		context.Context,
		uuid.UUID,
		string,
	) ([]adaptive.OutboxRecord, error)
}

type adaptiveBenchmarkRecordedDelivery struct {
	ownerID   uuid.UUID
	requestID uuid.UUID
	delivery  adaptive.Delivery
}

type adaptiveBenchmarkTeeRecorder struct {
	delegate adaptiveBenchmarkLedger

	mu          sync.Mutex
	assignments map[uuid.UUID]adaptive.Assignment
	deliveries  map[uuid.UUID]adaptiveBenchmarkRecordedDelivery

	probeMu sync.Mutex
	probe   *adaptiveBenchmarkRecorderProbeRegistration
}

func newAdaptiveBenchmarkTeeRecorder(
	delegate adaptiveBenchmarkLedger,
) (*adaptiveBenchmarkTeeRecorder, error) {
	if delegate == nil {
		return nil, errors.New(
			"adaptive benchmark event recorder is unavailable",
		)
	}
	return &adaptiveBenchmarkTeeRecorder{
		delegate:    delegate,
		assignments: make(map[uuid.UUID]adaptive.Assignment),
		deliveries:  make(map[uuid.UUID]adaptiveBenchmarkRecordedDelivery),
	}, nil
}

func (recorder *adaptiveBenchmarkTeeRecorder) RecordAssignment(
	ctx context.Context,
	assignment adaptive.Assignment,
) (int64, error) {
	if recorder == nil || recorder.delegate == nil {
		return 0, errors.New(
			"adaptive benchmark event recorder is unavailable",
		)
	}
	if _, err := adaptive.NewAssignmentEvent(assignment); err != nil {
		return 0, err
	}
	fact := adaptiveBenchmarkAssignmentProbeFact(assignment)
	probe := recorder.currentProbe()
	if probe != nil {
		if err := probe.probe.BeforeWrite(ctx, fact); err != nil {
			return 0, err
		}
	}
	sequence, err := recorder.delegate.RecordAssignment(ctx, assignment)
	if err != nil {
		return 0, err
	}
	recorder.mu.Lock()
	if existing, ok := recorder.assignments[assignment.AssignmentID]; ok {
		existingEvent, existingErr := adaptive.NewAssignmentEvent(existing)
		incomingEvent, incomingErr := adaptive.NewAssignmentEvent(assignment)
		if existingErr != nil ||
			incomingErr != nil ||
			existingEvent.ID != incomingEvent.ID ||
			!bytes.Equal(existingEvent.PayloadHash, incomingEvent.PayloadHash) {
			recorder.mu.Unlock()
			return 0, adaptive.ErrPayloadConflict
		}
	} else {
		recorder.assignments[assignment.AssignmentID] =
			cloneAdaptiveBenchmarkAssignment(assignment)
	}
	recorder.mu.Unlock()
	if probe != nil {
		fact.Sequence = sequence
		if err := probe.probe.AfterCommit(ctx, fact); err != nil {
			return sequence, err
		}
	}
	return sequence, nil
}

func (recorder *adaptiveBenchmarkTeeRecorder) RecordDelivery(
	ctx context.Context,
	ownerID uuid.UUID,
	assignmentID uuid.UUID,
	delivery adaptive.Delivery,
) (int64, error) {
	if recorder == nil || recorder.delegate == nil {
		return 0, errors.New(
			"adaptive benchmark event recorder is unavailable",
		)
	}
	recorder.mu.Lock()
	assignment, ok := recorder.assignments[delivery.AssignmentID]
	recorded, alreadyRecorded := recorder.deliveries[delivery.AssignmentID]
	recorder.mu.Unlock()
	if !ok ||
		assignment.OwnerID != ownerID ||
		assignment.AssignmentID != assignmentID {
		return 0, errors.New(
			"adaptive benchmark delivery has no captured assignment",
		)
	}
	incomingEvent, err := adaptive.NewDeliveryEvent(assignment, delivery)
	if err != nil {
		return 0, err
	}
	if alreadyRecorded {
		existingEvent, existingErr := adaptive.NewDeliveryEvent(
			assignment,
			recorded.delivery,
		)
		if existingErr != nil ||
			existingEvent.ID != incomingEvent.ID ||
			!bytes.Equal(existingEvent.PayloadHash, incomingEvent.PayloadHash) {
			return 0, adaptive.ErrPayloadConflict
		}
	}
	fact := adaptiveBenchmarkAssignmentProbeFact(assignment)
	fact.Kind = adaptive.EventDelivery
	probe := recorder.currentProbe()
	if probe != nil {
		if err := probe.probe.BeforeWrite(ctx, fact); err != nil {
			return 0, err
		}
	}
	sequence, err := recorder.delegate.RecordDelivery(
		ctx,
		ownerID,
		assignmentID,
		delivery,
	)
	if err != nil {
		return 0, err
	}
	recorder.mu.Lock()
	recorder.deliveries[delivery.AssignmentID] =
		adaptiveBenchmarkRecordedDelivery{
			ownerID: ownerID, requestID: assignment.RequestID,
			delivery: cloneAdaptiveBenchmarkDelivery(delivery),
		}
	recorder.mu.Unlock()
	if probe != nil {
		fact.Sequence = sequence
		if err := probe.probe.AfterCommit(ctx, fact); err != nil {
			return sequence, err
		}
	}
	return sequence, nil
}

type adaptiveBenchmarkPersistedFacts struct {
	Assignment         adaptive.Assignment
	Delivery           adaptive.Delivery
	AssignmentEvent    adaptive.Event
	DeliveryEvent      adaptive.Event
	AssignmentSequence int64
	DeliverySequence   int64
}

func (recorder *adaptiveBenchmarkTeeRecorder) PersistedFacts(
	ctx context.Context,
	ownerID uuid.UUID,
	requestID uuid.UUID,
	domain adaptive.Domain,
) (adaptiveBenchmarkPersistedFacts, error) {
	if recorder == nil ||
		recorder.delegate == nil ||
		ownerID == uuid.Nil ||
		requestID == uuid.Nil {
		return adaptiveBenchmarkPersistedFacts{},
			errors.New("adaptive benchmark fact scope is invalid")
	}
	recorder.mu.Lock()
	var assignment adaptive.Assignment
	matches := 0
	for _, candidate := range recorder.assignments {
		if candidate.OwnerID == ownerID &&
			candidate.RequestID == requestID &&
			candidate.Domain == domain {
			assignment = candidate
			matches++
		}
	}
	if matches != 1 {
		recorder.mu.Unlock()
		return adaptiveBenchmarkPersistedFacts{}, fmt.Errorf(
			"%w: "+
				"adaptive benchmark request has %d %s assignments",
			errAdaptiveBenchmarkAssignmentFactMissing,
			matches,
			domain,
		)
	}
	recorded, ok := recorder.deliveries[assignment.AssignmentID]
	if !ok ||
		recorded.ownerID != assignment.OwnerID ||
		recorded.requestID != requestID {
		recorder.mu.Unlock()
		return adaptiveBenchmarkPersistedFacts{}, fmt.Errorf(
			"%w: adaptive benchmark assignment has no delivery",
			errAdaptiveBenchmarkDeliveryFactMissing,
		)
	}
	assignment = cloneAdaptiveBenchmarkAssignment(assignment)
	delivery := cloneAdaptiveBenchmarkDelivery(recorded.delivery)
	recorder.mu.Unlock()
	decisionEvent, err := adaptive.NewAssignmentEvent(assignment)
	if err != nil {
		return adaptiveBenchmarkPersistedFacts{}, fmt.Errorf(
			"%w: %v",
			errAdaptiveBenchmarkAssignmentFactMissing,
			err,
		)
	}
	deliveryEvent, err := adaptive.NewDeliveryEvent(
		assignment,
		delivery,
	)
	if err != nil {
		return adaptiveBenchmarkPersistedFacts{}, fmt.Errorf(
			"%w: %v",
			errAdaptiveBenchmarkDeliveryFactMissing,
			err,
		)
	}
	records, err := recorder.delegate.ListAggregate(
		ctx,
		ownerID,
		requestID.String(),
	)
	if err != nil {
		return adaptiveBenchmarkPersistedFacts{}, err
	}
	assignmentSequence, err := adaptiveBenchmarkPersistedSequence(
		records,
		decisionEvent,
	)
	if err != nil {
		return adaptiveBenchmarkPersistedFacts{}, fmt.Errorf(
			"%w: %v",
			errAdaptiveBenchmarkAssignmentFactMissing,
			err,
		)
	}
	deliverySequence, err := adaptiveBenchmarkPersistedSequence(
		records,
		deliveryEvent,
	)
	if err != nil {
		return adaptiveBenchmarkPersistedFacts{}, fmt.Errorf(
			"%w: %v",
			errAdaptiveBenchmarkDeliveryFactMissing,
			err,
		)
	}
	if assignmentSequence >= deliverySequence {
		return adaptiveBenchmarkPersistedFacts{},
			errors.New(
				"adaptive benchmark delivery does not follow assignment",
			)
	}
	return adaptiveBenchmarkPersistedFacts{
		Assignment: assignment, Delivery: delivery,
		AssignmentEvent: decisionEvent, DeliveryEvent: deliveryEvent,
		AssignmentSequence: assignmentSequence,
		DeliverySequence:   deliverySequence,
	}, nil
}

func adaptiveBenchmarkPersistedSequence(
	records []adaptive.OutboxRecord,
	expected adaptive.Event,
) (int64, error) {
	matches := 0
	var sequence int64
	for _, record := range records {
		if record.ID != expected.ID {
			continue
		}
		if record.OwnerID != expected.OwnerID ||
			record.AggregateID != expected.AggregateID ||
			record.DecisionID != expected.DecisionID ||
			record.Kind != expected.Kind ||
			!bytes.Equal(record.Payload, expected.Payload) ||
			!bytes.Equal(record.PayloadHash, expected.PayloadHash) {
			return 0, errors.New(
				"adaptive benchmark persisted fact drifted",
			)
		}
		matches++
		sequence = record.Sequence
	}
	if matches != 1 || sequence <= 0 {
		return 0, errors.New(
			"adaptive benchmark persisted fact is missing or duplicated",
		)
	}
	return sequence, nil
}

func cloneAdaptiveBenchmarkAssignment(
	assignment adaptive.Assignment,
) adaptive.Assignment {
	cloned := assignment
	cloned.EligibleActions = append(
		[]string(nil),
		assignment.EligibleActions...,
	)
	cloned.ActionProbabilities = append(
		[]adaptive.ActionProbability(nil),
		assignment.ActionProbabilities...,
	)
	cloned.Features = make(
		map[adaptive.FeatureKey]float64,
		len(assignment.Features),
	)
	for key, value := range assignment.Features {
		cloned.Features[key] = value
	}
	if assignment.CohortID != nil {
		value := *assignment.CohortID
		cloned.CohortID = &value
	}
	if assignment.ArmProbability != nil {
		value := *assignment.ArmProbability
		cloned.ArmProbability = &value
	}
	return cloned
}

func cloneAdaptiveBenchmarkDelivery(
	delivery adaptive.Delivery,
) adaptive.Delivery {
	cloned := delivery
	cloned.ResultIDs = append(
		[]adaptive.ResultID(nil),
		delivery.ResultIDs...,
	)
	cloned.EffectiveLimits = make(
		map[string]int,
		len(delivery.EffectiveLimits),
	)
	for key, value := range delivery.EffectiveLimits {
		cloned.EffectiveLimits[key] = value
	}
	if delivery.ExposureProbability != nil {
		value := *delivery.ExposureProbability
		cloned.ExposureProbability = &value
	}
	return cloned
}
