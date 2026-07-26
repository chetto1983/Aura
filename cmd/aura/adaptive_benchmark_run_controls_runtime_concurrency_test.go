package main

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

func TestAdaptiveBenchmarkStartConcurrencyClientsProvesC1BlockedFirst(
	t *testing.T,
) {
	states := make(map[string]*adaptiveBenchmarkConcurrencyClientState, 4)
	for _, clientID := range []string{"C0", "C1", "C2", "C3"} {
		states[clientID] = newAdaptiveBenchmarkConcurrencyClientState(
			auraeval.AdaptiveBenchmarkConcurrencyClient{
				ClientID: clientID,
			},
			adaptiveBenchmarkControlSubject{},
			adaptive.DomainReasoning,
			uuid.Must(uuid.NewV7()),
		)
	}
	var starts []string
	start := func(
		_ context.Context,
		state *adaptiveBenchmarkConcurrencyClientState,
	) error {
		starts = append(starts, state.spec.ClientID)
		close(state.started)
		if state.spec.ClientID == "C0" {
			close(state.assigned)
		}
		return nil
	}
	proofs := 0
	proveBlocked := func(
		_ context.Context,
		state *adaptiveBenchmarkConcurrencyClientState,
	) error {
		proofs++
		if state != states["C1"] ||
			!slices.Equal(starts, []string{"C0", "C1"}) {
			return errors.New("C1 was not proved before independent starts")
		}
		select {
		case <-state.assigned:
			return errors.New("C1 reached assignment during blocked proof")
		default:
			return nil
		}
	}

	if err := adaptiveBenchmarkStartConcurrencyClients(
		t.Context(),
		states,
		start,
		proveBlocked,
	); err != nil {
		t.Fatal(err)
	}
	if proofs != 1 ||
		!slices.Equal(starts, []string{"C0", "C1", "C2", "C3"}) {
		t.Fatalf("proofs/starts = %d/%v", proofs, starts)
	}
}

func TestAdaptiveBenchmarkStartConcurrencyClientsStopsOnPreparationFailure(
	t *testing.T,
) {
	states := make(map[string]*adaptiveBenchmarkConcurrencyClientState, 4)
	for _, clientID := range []string{"C0", "C1", "C2", "C3"} {
		states[clientID] = newAdaptiveBenchmarkConcurrencyClientState(
			auraeval.AdaptiveBenchmarkConcurrencyClient{
				ClientID: clientID,
			},
			adaptiveBenchmarkControlSubject{},
			adaptive.DomainReasoning,
			uuid.Must(uuid.NewV7()),
		)
	}
	preparationErr := errors.New("scenario preparation failed")
	var starts []string
	start := func(
		_ context.Context,
		state *adaptiveBenchmarkConcurrencyClientState,
	) error {
		starts = append(starts, state.spec.ClientID)
		if state.spec.ClientID == "C0" {
			close(state.assigned)
			return nil
		}
		return preparationErr
	}
	proofs := 0
	err := adaptiveBenchmarkStartConcurrencyClients(
		t.Context(),
		states,
		start,
		func(
			context.Context,
			*adaptiveBenchmarkConcurrencyClientState,
		) error {
			proofs++
			return nil
		},
	)
	if !errors.Is(err, preparationErr) ||
		proofs != 0 ||
		!slices.Equal(starts, []string{"C0", "C1"}) {
		t.Fatalf("error/proofs/starts = %v/%d/%v", err, proofs, starts)
	}
}

func TestAdaptiveBenchmarkProveConcurrencyClientBlockedRejectsAssignment(
	t *testing.T,
) {
	state := newAdaptiveBenchmarkConcurrencyClientState(
		auraeval.AdaptiveBenchmarkConcurrencyClient{ClientID: "C1"},
		adaptiveBenchmarkControlSubject{},
		adaptive.DomainReasoning,
		uuid.Must(uuid.NewV7()),
	)
	close(state.assigned)
	if err := adaptiveBenchmarkProveConcurrencyClientBlocked(
		t.Context(),
		state,
		time.Millisecond,
	); err == nil {
		t.Fatal("blocked proof accepted a committed C1 assignment")
	}
}

func TestAdaptiveBenchmarkProveConcurrencyClientBlockedIsBounded(
	t *testing.T,
) {
	state := newAdaptiveBenchmarkConcurrencyClientState(
		auraeval.AdaptiveBenchmarkConcurrencyClient{ClientID: "C1"},
		adaptiveBenchmarkControlSubject{},
		adaptive.DomainReasoning,
		uuid.Must(uuid.NewV7()),
	)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := adaptiveBenchmarkProveConcurrencyClientBlocked(
		ctx,
		state,
		time.Millisecond,
	); err != nil {
		t.Fatal(err)
	}
	state.mu.Lock()
	assignmentID := state.assignmentID
	state.mu.Unlock()
	if assignmentID != uuid.Nil {
		t.Fatalf("C1 assignment = %s during blocked proof", assignmentID)
	}
}

func TestAdaptiveBenchmarkConcurrencyClientStateRecordsBoundaries(
	t *testing.T,
) {
	state := newAdaptiveBenchmarkConcurrencyClientState(
		auraeval.AdaptiveBenchmarkConcurrencyClient{ClientID: "C0"},
		adaptiveBenchmarkControlSubject{},
		adaptive.DomainReasoning,
		uuid.MustParse("018f0000-0000-7000-8000-000000000101"),
	)
	requestID := uuid.MustParse(
		"018f0000-0000-7000-8000-000000000102",
	)
	assignmentID := uuid.MustParse(
		"018f0000-0000-7000-8000-000000000103",
	)
	ctx := context.WithValue(
		t.Context(),
		adaptiveBenchmarkConcurrencyContextKey{},
		"C0",
	)
	recorded := make(chan error, 1)
	go func() {
		recorded <- state.recordFact(
			ctx,
			adaptiveBenchmarkRecorderProbeFact{
				Kind: adaptive.EventDecision, Domain: adaptive.DomainReasoning,
				RequestID: requestID, AssignmentID: assignmentID,
			},
		)
	}()
	if err := adaptiveBenchmarkWaitSignal(
		t.Context(),
		state.assigned,
		"assignment",
	); err != nil {
		t.Fatal(err)
	}
	close(state.release)
	if err := adaptiveBenchmarkWaitSignal(
		t.Context(),
		state.released,
		"release",
	); err != nil {
		t.Fatal(err)
	}
	if err := <-recorded; err != nil {
		t.Fatal(err)
	}
	if err := state.recordFact(
		ctx,
		adaptiveBenchmarkRecorderProbeFact{
			Kind: adaptive.EventDelivery, Domain: adaptive.DomainReasoning,
			RequestID: requestID, AssignmentID: assignmentID,
		},
	); err != nil {
		t.Fatal(err)
	}
	state.recordModelStart(requestID.String())
	state.recordTerminalAt(time.Now().Round(0).UTC())
	state.recordTerminal(
		adaptiveBenchmarkControlTurn{RequestID: requestID},
		nil,
	)
	snapshot := state.snapshot()
	if snapshot.requestID != requestID ||
		snapshot.assignmentID != assignmentID ||
		snapshot.deliveryCount != 1 ||
		snapshot.assignmentAt.IsZero() ||
		snapshot.releasedAt.IsZero() ||
		snapshot.deliveryAt.IsZero() ||
		snapshot.modelStartedAt.IsZero() ||
		snapshot.terminalAt.IsZero() {
		t.Fatalf("concurrency snapshot = %#v", snapshot)
	}
}

func TestAdaptiveBenchmarkStaticProbabilityRequiresStaticCandidate(
	t *testing.T,
) {
	assignment := adaptive.Assignment{
		ActionProbabilities: []adaptive.ActionProbability{{
			ActionID:    adaptive.StaticActionID,
			Probability: 1,
		}},
	}
	if got := adaptiveBenchmarkStaticProbability(assignment); got != 1 {
		t.Fatalf("static probability = %v", got)
	}
	if got := adaptiveBenchmarkStaticProbability(
		adaptive.Assignment{},
	); got != -1 {
		t.Fatalf("missing static probability = %v", got)
	}
}
