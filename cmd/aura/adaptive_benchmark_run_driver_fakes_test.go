package main

import (
	"context"
	"iter"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

type adaptiveBenchmarkTurnItem struct {
	event *agent.Event
	err   error
}

type adaptiveBenchmarkFixtureAliasResolverFake map[uuid.UUID]string

func (resolver adaptiveBenchmarkFixtureAliasResolverFake) ResolveFixtureAlias(
	_ adaptive.Domain,
	id uuid.UUID,
) (string, bool) {
	alias, ok := resolver[id]
	return alias, ok
}

type adaptiveBenchmarkRunnerFake struct {
	items          []adaptiveBenchmarkTurnItem
	beforeTurn     func(context.Context) error
	createErr      error
	deleteErr      error
	deleteAffected int64

	createdID     string
	createOwnerID string
	turnOwnerID   string
	turnMessage   string
	deletedID     string
	deleteOwnerID string
}

func (runner *adaptiveBenchmarkRunnerFake) NewConversationWithID(
	ctx context.Context,
	conversationID string,
) (string, error) {
	runner.createdID = conversationID
	runner.createOwnerID = identityctx.IdentityID(ctx)
	if runner.createErr != nil {
		return "", runner.createErr
	}
	return conversationID, nil
}

func (runner *adaptiveBenchmarkRunnerFake) Turn(
	ctx context.Context,
	conversationID string,
	message *string,
) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		runner.turnOwnerID = identityctx.IdentityID(ctx)
		if message != nil {
			runner.turnMessage = *message
		}
		if runner.beforeTurn != nil {
			if err := runner.beforeTurn(ctx); err != nil {
				yield(nil, err)
				return
			}
		}
		for _, item := range runner.items {
			if !yield(item.event, item.err) {
				return
			}
		}
	}
}

func (runner *adaptiveBenchmarkRunnerFake) DeleteConversationLifecycle(
	_ context.Context,
	ownerID string,
	conversationID string,
) (int64, error) {
	runner.deletedID = conversationID
	runner.deleteOwnerID = ownerID
	return runner.deleteAffected, runner.deleteErr
}

type adaptiveBenchmarkOutcomeWriterFake struct {
	ledger  *adaptiveBenchmarkLedgerFake
	catalog adaptive.EvaluatorCatalog
	err     error

	mu           sync.Mutex
	observations []adaptive.OutcomeObservation
}

func (writer *adaptiveBenchmarkOutcomeWriterFake) RecordOutcome(
	_ context.Context,
	observation adaptive.OutcomeObservation,
) (int64, error) {
	if writer.err != nil {
		return 0, writer.err
	}
	writer.mu.Lock()
	defer writer.mu.Unlock()
	assignment, ok := writer.ledger.assignments[observation.AssignmentID]
	if !ok {
		return 0, adaptive.ErrAssignmentNotFound
	}
	event, err := adaptive.NewOutcomeEvent(
		assignment,
		observation,
		writer.catalog,
	)
	if err != nil {
		return 0, err
	}
	writer.observations = append(writer.observations, observation)
	return writer.ledger.append(event), nil
}

func adaptiveBenchmarkDriverFixture(
	t *testing.T,
	domain adaptive.Domain,
) (
	*adaptiveBenchmarkAuraDriver,
	*adaptiveBenchmarkRunnerFake,
	*adaptiveBenchmarkLedgerFake,
	*adaptiveBenchmarkOutcomeWriterFake,
	adaptive.Assignment,
	adaptive.Delivery,
) {
	t.Helper()
	ownerID := uuid.Must(uuid.NewV7())
	requestID := uuid.Must(uuid.NewV7())
	assignment := adaptiveBenchmarkTestAssignmentFor(
		t,
		ownerID,
		requestID,
		domain,
	)
	delivery := adaptiveBenchmarkTestDelivery(t, assignment)
	ledger := newAdaptiveBenchmarkLedgerFake()
	tee, err := newAdaptiveBenchmarkTeeRecorder(ledger)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := adaptive.NewEvaluatorCatalog(
		adaptiveBenchmarkDeterministicEvaluatorRegistration(),
	)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := &adaptiveBenchmarkOutcomeWriterFake{
		ledger: ledger, catalog: catalog,
	}
	terminal := adaptiveBenchmarkTerminalEvent(requestID, "414")
	runner := &adaptiveBenchmarkRunnerFake{
		items: []adaptiveBenchmarkTurnItem{{event: terminal}},
		beforeTurn: func(ctx context.Context) error {
			if _, err := tee.RecordAssignment(ctx, assignment); err != nil {
				return err
			}
			_, err := tee.RecordDelivery(
				ctx,
				assignment.OwnerID,
				assignment.AssignmentID,
				delivery,
			)
			return err
		},
		deleteAffected: 1,
	}
	runID := uuid.Must(uuid.NewV7())
	started := time.Date(2026, 7, 26, 19, 0, 0, 0, time.UTC)
	times := []time.Time{
		started,
		started.Add(25 * time.Millisecond),
		started.Add(30 * time.Millisecond),
	}
	timeIndex := 0
	driver, err := newAdaptiveBenchmarkAuraDriverWithDeps(
		adaptiveBenchmarkAuraDriverDeps{
			runner: runner, recorder: tee, ledger: ledger,
			outcomes: outcomes, catalog: catalog,
			fixtureAliases: adaptiveBenchmarkFixtureAliasResolverFake{},
			ownerID:        ownerID, runID: runID,
			providerID: "test-provider", modelID: "test-model",
			newConversationID: uuid.NewV7,
			now: func() time.Time {
				if timeIndex >= len(times) {
					return times[len(times)-1]
				}
				result := times[timeIndex]
				timeIndex++
				return result
			},
			cleanupTimeout: time.Second,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return driver, runner, ledger, outcomes, assignment, delivery
}

func adaptiveBenchmarkTerminalEvent(
	requestID uuid.UUID,
	answer string,
) *agent.Event {
	cost := 0.0125
	return &agent.Event{
		RequestID: requestID,
		LLMResponse: &agent.LLMResponse{
			Content: answer, FinishReason: "stop",
		},
		Actions: agent.Actions{StateDelta: map[string]any{
			"prompt_tokens": 11, "completion_tokens": 7,
			"cost_usd": cost,
		}},
		Timestamp: time.Now().UTC(),
	}
}

var _ adaptiveBenchmarkTurnRunner = (*adaptiveBenchmarkRunnerFake)(nil)
var _ adaptiveBenchmarkOutcomeWriter = (*adaptiveBenchmarkOutcomeWriterFake)(nil)
