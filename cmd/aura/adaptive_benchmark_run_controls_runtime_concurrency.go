package main

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/google/uuid"
)

type adaptiveBenchmarkConcurrencyContextKey struct{}

type adaptiveBenchmarkConcurrencyClientState struct {
	mu sync.Mutex

	spec           auraeval.AdaptiveBenchmarkConcurrencyClient
	subject        adaptiveBenchmarkControlSubject
	domain         adaptive.Domain
	conversationID uuid.UUID
	message        string
	requestID      uuid.UUID
	assignmentID   uuid.UUID
	startedAt      time.Time
	assignmentAt   time.Time
	releasedAt     time.Time
	deliveryAt     time.Time
	modelStartedAt time.Time
	terminalAt     time.Time
	deliveryCount  int
	turnErr        error
	releaseOnce    sync.Once

	started  chan struct{}
	assigned chan struct{}
	release  chan struct{}
	released chan struct{}
	done     chan struct{}
}

type adaptiveBenchmarkConcurrencySnapshot struct {
	evidence auraeval.AdaptiveBenchmarkConcurrencyEvidence
	facts    map[string]adaptiveBenchmarkPersistedFacts
}

func newAdaptiveBenchmarkConcurrencyClientState(
	spec auraeval.AdaptiveBenchmarkConcurrencyClient,
	subject adaptiveBenchmarkControlSubject,
	domain adaptive.Domain,
	conversationID uuid.UUID,
) *adaptiveBenchmarkConcurrencyClientState {
	return &adaptiveBenchmarkConcurrencyClientState{
		spec: spec, subject: subject, domain: domain,
		conversationID: conversationID,
		started:        make(chan struct{}),
		assigned:       make(chan struct{}),
		release:        make(chan struct{}),
		released:       make(chan struct{}),
		done:           make(chan struct{}),
	}
}

func (runtime *adaptiveBenchmarkControlRuntime) runConcurrency(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		control,
		"same_owner_concurrency",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	plan := auraeval.AdaptiveBenchmarkConcurrencySchedule()
	if control.ConcurrencyPlan == nil ||
		!reflect.DeepEqual(*control.ConcurrencyPlan, plan) {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark concurrency plan drifted",
			)
	}
	primary, err := runtime.primarySubject()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	controlOwner, err := runtime.controlSubject()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	conversations, err := adaptiveBenchmarkConcurrencyConversations(
		ctx,
		primary,
		controlOwner,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	states, err := adaptiveBenchmarkConcurrencyStates(
		plan,
		primary,
		controlOwner,
		conversations,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	cleanup := func(cleanupCtx context.Context) error {
		return errors.Join(
			adaptiveBenchmarkControlDeleteConversation(
				cleanupCtx, primary, conversations["A0"],
			),
			adaptiveBenchmarkControlDeleteConversation(
				cleanupCtx, primary, conversations["A1"],
			),
			adaptiveBenchmarkControlDeleteConversation(
				cleanupCtx, controlOwner, conversations["B0"],
			),
		)
	}
	evidence, facts, runErr := adaptiveBenchmarkRunConcurrencySchedule(
		ctx,
		plan,
		states,
	)
	cleanupErr := adaptiveBenchmarkConcurrencyCleanup(ctx, cleanup)
	if joined := errors.Join(runErr, cleanupErr); joined != nil {
		return adaptiveBenchmarkControlExecution{}, joined
	}
	if err := auraeval.ValidateAdaptiveBenchmarkConcurrencyEvidence(
		evidence,
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	runtime.concurrencySnapshot = &adaptiveBenchmarkConcurrencySnapshot{
		evidence: evidence,
		facts:    facts,
	}
	ids := make([]uuid.UUID, 0, len(evidence.Clients)*3)
	for _, client := range evidence.Clients {
		for _, value := range []string{
			client.RequestID,
			client.AssignmentID,
			client.DeliveryEventID,
		} {
			id, parseErr := parseCanonicalAdaptiveBenchmarkUUID(value)
			if parseErr != nil {
				return adaptiveBenchmarkControlExecution{}, parseErr
			}
			ids = append(ids, id)
		}
	}
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs: ids,
		Concurrency: &evidence,
	}, nil
}

func adaptiveBenchmarkConcurrencyCleanup(
	ctx context.Context,
	cleanup func(context.Context) error,
) error {
	if cleanup == nil {
		return adaptiveBenchmarkControlError(
			"adaptive benchmark concurrency cleanup is unavailable",
		)
	}
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		adaptiveBenchmarkConversationCleanupTimeout,
	)
	defer cancel()
	return cleanup(cleanupCtx)
}

func adaptiveBenchmarkConcurrencyConversations(
	ctx context.Context,
	primary adaptiveBenchmarkControlSubject,
	control adaptiveBenchmarkControlSubject,
) (map[string]uuid.UUID, error) {
	conversations := make(map[string]uuid.UUID, 3)
	created := make([]struct {
		subject adaptiveBenchmarkControlSubject
		id      uuid.UUID
	}, 0, 3)
	for _, entry := range []struct {
		alias   string
		subject adaptiveBenchmarkControlSubject
	}{
		{alias: "A0", subject: primary},
		{alias: "A1", subject: primary},
		{alias: "B0", subject: control},
	} {
		id, err := adaptiveBenchmarkControlCreateConversation(
			ctx,
			entry.subject,
		)
		if err != nil {
			cleanupErrors := make([]error, 0, len(created)+1)
			cleanupErrors = append(cleanupErrors, err)
			for _, conversation := range created {
				cleanupErrors = append(
					cleanupErrors,
					adaptiveBenchmarkControlDeleteConversation(
						ctx,
						conversation.subject,
						conversation.id,
					),
				)
			}
			return nil, errors.Join(cleanupErrors...)
		}
		conversations[entry.alias] = id
		created = append(created, struct {
			subject adaptiveBenchmarkControlSubject
			id      uuid.UUID
		}{subject: entry.subject, id: id})
	}
	return conversations, nil
}

func adaptiveBenchmarkConcurrencyStates(
	plan auraeval.AdaptiveBenchmarkConcurrencyPlan,
	primary adaptiveBenchmarkControlSubject,
	control adaptiveBenchmarkControlSubject,
	conversations map[string]uuid.UUID,
) (map[string]*adaptiveBenchmarkConcurrencyClientState, error) {
	states := make(
		map[string]*adaptiveBenchmarkConcurrencyClientState,
		len(plan.Clients),
	)
	for _, spec := range plan.Clients {
		scenario, err := adaptiveBenchmarkControlScenario(spec.ScenarioID)
		if err != nil {
			return nil, err
		}
		message, err := adaptiveBenchmarkSubjectJSON(
			auraeval.AdaptiveBenchmarkSubject{
				ScenarioID: scenario.ScenarioID,
				Domain:     scenario.Domain,
				Prompt:     scenario.Prompt,
			},
		)
		if err != nil {
			return nil, err
		}
		subject := primary
		if spec.OwnerAlias == "B" {
			subject = control
		}
		conversationID := conversations[spec.ConversationAlias]
		if conversationID == uuid.Nil {
			return nil, adaptiveBenchmarkControlError(
				"adaptive benchmark concurrency conversation is unavailable",
			)
		}
		state := newAdaptiveBenchmarkConcurrencyClientState(
			spec,
			subject,
			scenario.Domain,
			conversationID,
		)
		state.message = message
		states[spec.ClientID] = state
	}
	return states, nil
}

func adaptiveBenchmarkRunConcurrencySchedule(
	ctx context.Context,
	plan auraeval.AdaptiveBenchmarkConcurrencyPlan,
	states map[string]*adaptiveBenchmarkConcurrencyClientState,
) (
	auraeval.AdaptiveBenchmarkConcurrencyEvidence,
	map[string]adaptiveBenchmarkPersistedFacts,
	error,
) {
	clear, err := adaptiveBenchmarkInstallConcurrencyProbes(states)
	if err != nil {
		return auraeval.AdaptiveBenchmarkConcurrencyEvidence{}, nil, err
	}
	clearStreams, err :=
		adaptiveBenchmarkInstallConcurrencyStreamInterceptors(states)
	if err != nil {
		clear()
		return auraeval.AdaptiveBenchmarkConcurrencyEvidence{}, nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	started := make([]*adaptiveBenchmarkConcurrencyClientState, 0, len(states))
	defer func() {
		adaptiveBenchmarkShutdownConcurrencyClients(cancel, started)
		clearStreams()
		clear()
	}()
	startClient := func(
		ctx context.Context,
		state *adaptiveBenchmarkConcurrencyClientState,
	) error {
		if err := adaptiveBenchmarkStartConcurrencyClient(ctx, state); err != nil {
			return err
		}
		started = append(started, state)
		return nil
	}
	waitDeadline := adaptiveBenchmarkConcurrencyDeadline(runCtx)
	if err := adaptiveBenchmarkStartConcurrencyClients(
		runCtx,
		states,
		startClient,
		func(
			ctx context.Context,
			state *adaptiveBenchmarkConcurrencyClientState,
		) error {
			return adaptiveBenchmarkProveConcurrencyClientBlocked(
				ctx,
				state,
				adaptiveBenchmarkConcurrencyBlockedObservation,
			)
		},
	); err != nil {
		return auraeval.AdaptiveBenchmarkConcurrencyEvidence{}, nil, err
	}
	for _, clientID := range []string{"C2", "C3"} {
		if err := adaptiveBenchmarkWaitForConcurrencyAssignment(
			runCtx,
			states[clientID],
		); err != nil {
			return auraeval.AdaptiveBenchmarkConcurrencyEvidence{}, nil, err
		}
	}
	for _, clientID := range []string{"C3", "C2", "C0"} {
		if err := adaptiveBenchmarkReleaseConcurrencyClient(
			runCtx,
			states[clientID],
		); err != nil {
			return auraeval.AdaptiveBenchmarkConcurrencyEvidence{}, nil, err
		}
	}
	if err := adaptiveBenchmarkWaitSignal(
		runCtx, states["C0"].done, "C0 terminal",
	); err != nil {
		return auraeval.AdaptiveBenchmarkConcurrencyEvidence{}, nil, err
	}
	if err := adaptiveBenchmarkWaitForConcurrencyAssignment(
		runCtx,
		states["C1"],
	); err != nil {
		return auraeval.AdaptiveBenchmarkConcurrencyEvidence{}, nil, err
	}
	if err := adaptiveBenchmarkReleaseConcurrencyClient(
		runCtx,
		states["C1"],
	); err != nil {
		return auraeval.AdaptiveBenchmarkConcurrencyEvidence{}, nil, err
	}
	for _, clientID := range []string{"C1", "C2", "C3"} {
		if err := adaptiveBenchmarkWaitSignal(
			runCtx, states[clientID].done, clientID+" terminal",
		); err != nil {
			return auraeval.AdaptiveBenchmarkConcurrencyEvidence{}, nil, err
		}
	}
	return adaptiveBenchmarkConcurrencyEvidence(
		runCtx,
		plan,
		states,
		waitDeadline,
	)
}

func adaptiveBenchmarkInstallConcurrencyProbes(
	states map[string]*adaptiveBenchmarkConcurrencyClientState,
) (func(), error) {
	byOwner := map[uuid.UUID]*adaptiveBenchmarkTeeRecorder{}
	for _, state := range states {
		byOwner[state.subject.ownerID] = state.subject.recorder
	}
	handles := make([]*adaptiveBenchmarkRecorderProbeHandle, 0, len(byOwner))
	for ownerID, recorder := range byOwner {
		probeOwner := ownerID
		handle, err := recorder.InstallProbe(
			adaptiveBenchmarkRecorderProbe{
				BeforeWrite: func(
					context.Context,
					adaptiveBenchmarkRecorderProbeFact,
				) error {
					return nil
				},
				AfterCommit: func(
					ctx context.Context,
					fact adaptiveBenchmarkRecorderProbeFact,
				) error {
					clientID, _ := ctx.Value(
						adaptiveBenchmarkConcurrencyContextKey{},
					).(string)
					state := states[clientID]
					if state == nil ||
						state.subject.ownerID != probeOwner ||
						state.domain != fact.Domain {
						return nil
					}
					return state.recordFact(ctx, fact)
				},
			},
		)
		if err != nil {
			for _, installed := range handles {
				_ = installed.Clear()
			}
			return nil, err
		}
		handles = append(handles, handle)
	}
	clearObservers := make([]func(), 0, len(byOwner))
	for _, state := range states {
		observer := state.subject.observed
		already := false
		for _, ownerState := range states {
			if ownerState != state &&
				ownerState.subject.observed == observer &&
				ownerState.spec.ClientID < state.spec.ClientID {
				already = true
			}
		}
		if already {
			continue
		}
		clearObservers = append(
			clearObservers,
			observer.SetModelStartObserver(
				func(ctx context.Context, requestID string) {
					clientID, _ := ctx.Value(
						adaptiveBenchmarkConcurrencyContextKey{},
					).(string)
					if client := states[clientID]; client != nil {
						client.recordModelStart(requestID)
					}
				},
			),
		)
	}
	return func() {
		for _, observer := range clearObservers {
			observer()
		}
		for _, handle := range handles {
			_ = handle.Clear()
		}
	}, nil
}

func (state *adaptiveBenchmarkConcurrencyClientState) recordFact(
	ctx context.Context,
	fact adaptiveBenchmarkRecorderProbeFact,
) error {
	state.mu.Lock()
	switch fact.Kind {
	case adaptive.EventDecision:
		if state.assignmentID != uuid.Nil {
			state.mu.Unlock()
			return adaptiveBenchmarkControlError(
				"adaptive benchmark concurrency assignment duplicated",
			)
		}
		state.requestID = fact.RequestID
		state.assignmentID = fact.AssignmentID
		state.assignmentAt = time.Now().Round(0).UTC()
		close(state.assigned)
		state.mu.Unlock()
		select {
		case <-state.release:
			state.mu.Lock()
			state.releasedAt = time.Now().Round(0).UTC()
			state.mu.Unlock()
			close(state.released)
			return nil
		case <-ctx.Done():
			return context.Cause(ctx)
		}
	case adaptive.EventDelivery:
		state.deliveryCount++
		state.deliveryAt = time.Now().Round(0).UTC()
		state.mu.Unlock()
		return nil
	default:
		state.mu.Unlock()
		return nil
	}
}

func (state *adaptiveBenchmarkConcurrencyClientState) recordModelStart(
	requestID string,
) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.deliveryAt.IsZero() ||
		!state.modelStartedAt.IsZero() ||
		state.requestID.String() != requestID {
		return
	}
	state.modelStartedAt = time.Now().Round(0).UTC()
}

func adaptiveBenchmarkStartConcurrencyClient(
	ctx context.Context,
	state *adaptiveBenchmarkConcurrencyClientState,
) error {
	if state == nil {
		return adaptiveBenchmarkControlError(
			"adaptive benchmark concurrency client is unavailable",
		)
	}
	if state.message == "" {
		return adaptiveBenchmarkControlError(
			"adaptive benchmark concurrency client message is unavailable",
		)
	}
	clientCtx := context.WithValue(
		ctx,
		adaptiveBenchmarkConcurrencyContextKey{},
		state.spec.ClientID,
	)
	go func() {
		defer close(state.done)
		state.mu.Lock()
		state.startedAt = time.Now().Round(0).UTC()
		state.mu.Unlock()
		close(state.started)
		turn, err := adaptiveBenchmarkControlRunTurnObserved(
			clientCtx, state.subject, state.conversationID, state.message,
			state.recordTerminalAt,
		)
		state.recordTerminal(turn, err)
	}()
	return nil
}

func (state *adaptiveBenchmarkConcurrencyClientState) recordTerminalAt(
	terminalAt time.Time,
) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.terminalAt.IsZero() {
		state.terminalAt = terminalAt
	}
}

func (state *adaptiveBenchmarkConcurrencyClientState) recordTerminal(
	turn adaptiveBenchmarkControlTurn,
	err error,
) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.requestID == uuid.Nil {
		state.requestID = turn.RequestID
	}
	if state.terminalAt.IsZero() {
		state.terminalAt = time.Now().Round(0).UTC()
	}
	state.turnErr = err
}

func adaptiveBenchmarkWaitSignal(
	ctx context.Context,
	signal <-chan struct{},
	name string,
) error {
	select {
	case <-signal:
		return nil
	case <-ctx.Done():
		return errors.Join(
			adaptiveBenchmarkControlError(
				"adaptive benchmark concurrency wait failed: "+name,
			),
			context.Cause(ctx),
		)
	}
}

func adaptiveBenchmarkConcurrencyDeadline(ctx context.Context) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline.Round(0).UTC()
	}
	return time.Now().Add(adaptiveBenchmarkControlTimeout).Round(0).UTC()
}
