package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/llm"
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

func TestAdaptiveBenchmarkWaitForConcurrencyAssignmentReportsEarlyTerminal(
	t *testing.T,
) {
	state := newAdaptiveBenchmarkConcurrencyClientState(
		auraeval.AdaptiveBenchmarkConcurrencyClient{ClientID: "C2"},
		adaptiveBenchmarkControlSubject{},
		adaptive.DomainMemoryRecall,
		uuid.Must(uuid.NewV7()),
	)
	terminalErr := errors.New("turn terminated")
	state.mu.Lock()
	state.turnErr = terminalErr
	state.mu.Unlock()
	close(state.done)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	err := adaptiveBenchmarkWaitForConcurrencyAssignment(ctx, state)

	if !errors.Is(err, terminalErr) ||
		!strings.Contains(
			err.Error(),
			"C2 terminated before memory_recall assignment",
		) ||
		errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("early terminal error = %v", err)
	}
}

func TestAdaptiveBenchmarkConcurrencyCleanupIgnoresRunCancellation(
	t *testing.T,
) {
	runCtx, cancelRun := context.WithCancel(t.Context())
	cancelRun()
	cleanupErr := errors.New("cleanup marker")
	called := false

	err := adaptiveBenchmarkConcurrencyCleanup(
		runCtx,
		func(cleanupCtx context.Context) error {
			called = true
			if cleanupCtx.Err() != nil {
				t.Fatalf("cleanup context is canceled: %v", cleanupCtx.Err())
			}
			deadline, ok := cleanupCtx.Deadline()
			if !ok {
				t.Fatal("cleanup context has no deadline")
			}
			remaining := time.Until(deadline)
			if remaining <= 0 ||
				remaining > adaptiveBenchmarkConversationCleanupTimeout {
				t.Fatalf("cleanup deadline remaining = %s", remaining)
			}
			return cleanupErr
		},
	)

	if !called || !errors.Is(err, cleanupErr) {
		t.Fatalf("cleanup called/error = %v/%v", called, err)
	}
}

func TestAdaptiveBenchmarkShutdownConcurrencyClientsCancelsAndJoinsStartedClients(
	t *testing.T,
) {
	runCtx, cancel := context.WithCancel(t.Context())
	states := make([]*adaptiveBenchmarkConcurrencyClientState, 0, 2)
	for _, clientID := range []string{"C0", "C1"} {
		state := newAdaptiveBenchmarkConcurrencyClientState(
			auraeval.AdaptiveBenchmarkConcurrencyClient{
				ClientID: clientID,
			},
			adaptiveBenchmarkControlSubject{},
			adaptive.DomainReasoning,
			uuid.Must(uuid.NewV7()),
		)
		states = append(states, state)
		go func() {
			defer close(state.done)
			close(state.started)
			select {
			case <-state.release:
			case <-runCtx.Done():
			}
		}()
	}

	adaptiveBenchmarkShutdownConcurrencyClients(cancel, states)

	for _, state := range states {
		select {
		case <-state.done:
		default:
			t.Fatalf("%s was not joined", state.spec.ClientID)
		}
		select {
		case <-state.release:
		default:
			t.Fatalf("%s was not released", state.spec.ClientID)
		}
	}
}

func TestAdaptiveBenchmarkConcurrencyC3StreamUsesDocumentSearchThenTerminates(
	t *testing.T,
) {
	const query = "frozen k01 query"
	interceptor := adaptiveBenchmarkConcurrencyStreamInterceptor(query)
	ctx := context.WithValue(
		t.Context(),
		adaptiveBenchmarkConcurrencyContextKey{},
		"C3",
	)
	var documentSearch llm.ToolDef
	documentSearch.Function.Name = "document_search"

	first, err := interceptor(
		ctx,
		llm.Request{Tools: []llm.ToolDef{documentSearch}},
	)
	if err != nil {
		t.Fatal(err)
	}
	firstChunks := adaptiveBenchmarkTestChunks(first)
	if len(firstChunks) != 2 ||
		firstChunks[0].ToolCall == nil ||
		firstChunks[0].ToolCall.Function.Name != "document_search" ||
		firstChunks[1].FinishReason != "tool_calls" {
		t.Fatalf("C3 first stream = %#v", firstChunks)
	}
	var arguments struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(
		[]byte(firstChunks[0].ToolCall.Function.Arguments),
		&arguments,
	); err != nil {
		t.Fatal(err)
	}
	if arguments.Query != query || arguments.Limit != 8 {
		t.Fatalf("document_search arguments = %#v", arguments)
	}

	second, err := interceptor(
		ctx,
		llm.Request{Messages: []llm.Message{{
			Role: llm.RoleTool,
		}}},
	)
	if err != nil {
		t.Fatal(err)
	}
	secondChunks := adaptiveBenchmarkTestChunks(second)
	if len(secondChunks) != 2 ||
		secondChunks[0].Text == "" ||
		secondChunks[0].ToolCall != nil ||
		secondChunks[1].FinishReason != "stop" {
		t.Fatalf("C3 second stream = %#v", secondChunks)
	}

	if _, err := interceptor(ctx, llm.Request{}); err == nil {
		t.Fatal("C3 stream accepted a request without document_search")
	}
}

func TestAdaptiveBenchmarkConcurrencyStreamInterceptorsAreScheduleScoped(
	t *testing.T,
) {
	primaryDelegate := &adaptiveBenchmarkLLMClientFake{
		stream: make(chan llm.Chunk),
	}
	primary, err := newAdaptiveBenchmarkObservedClient(primaryDelegate)
	if err != nil {
		t.Fatal(err)
	}
	controlDelegate := &adaptiveBenchmarkLLMClientFake{
		stream: make(chan llm.Chunk),
	}
	control, err := newAdaptiveBenchmarkObservedClient(controlDelegate)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]*adaptiveBenchmarkConcurrencyClientState{}
	for _, clientID := range []string{"C0", "C1", "C2", "C3"} {
		observed := primary
		if clientID == "C3" {
			observed = control
		}
		states[clientID] = newAdaptiveBenchmarkConcurrencyClientState(
			auraeval.AdaptiveBenchmarkConcurrencyClient{
				ClientID: clientID,
			},
			adaptiveBenchmarkControlSubject{observed: observed},
			adaptive.DomainReasoning,
			uuid.Must(uuid.NewV7()),
		)
	}

	clear, err := adaptiveBenchmarkInstallConcurrencyStreamInterceptors(
		states,
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(
		t.Context(),
		adaptiveBenchmarkConcurrencyContextKey{},
		"C2",
	)
	if _, err := primary.Stream(ctx, llm.Request{}); err != nil {
		t.Fatal(err)
	}
	if primaryDelegate.calls != 0 {
		t.Fatalf(
			"primary delegate calls inside schedule = %d",
			primaryDelegate.calls,
		)
	}

	clear()
	if _, err := primary.Stream(ctx, llm.Request{}); err != nil {
		t.Fatal(err)
	}
	if primaryDelegate.calls != 1 {
		t.Fatalf(
			"primary delegate calls after schedule = %d",
			primaryDelegate.calls,
		)
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

func adaptiveBenchmarkTestChunks(stream <-chan llm.Chunk) []llm.Chunk {
	var chunks []llm.Chunk
	for chunk := range stream {
		chunks = append(chunks, chunk)
	}
	return chunks
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
