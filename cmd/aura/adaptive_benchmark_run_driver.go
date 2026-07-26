package main

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

const adaptiveBenchmarkConversationCleanupTimeout = 30 * time.Second

var (
	errAdaptiveBenchmarkScenarioFailure = errors.New(
		"adaptive benchmark scenario failed",
	)
	errAdaptiveBenchmarkFixtureAliasMissing = errors.New(
		"adaptive benchmark fixture alias is missing",
	)
)

type adaptiveBenchmarkTurnRunner interface {
	NewConversationWithID(context.Context, string) (string, error)
	Turn(
		context.Context,
		string,
		*string,
	) iter.Seq2[*agent.Event, error]
	DeleteConversationLifecycle(
		context.Context,
		string,
		string,
	) (int64, error)
}

type adaptiveBenchmarkFixtureAliasResolver interface {
	ResolveFixtureAlias(
		adaptive.Domain,
		uuid.UUID,
	) (string, bool)
}

type adaptiveBenchmarkOutcomeWriter interface {
	RecordOutcome(
		context.Context,
		adaptive.OutcomeObservation,
	) (int64, error)
}

type adaptiveBenchmarkAuraDriverDeps struct {
	runner   adaptiveBenchmarkTurnRunner
	recorder *adaptiveBenchmarkTeeRecorder
	ledger   adaptiveBenchmarkLedger
	outcomes adaptiveBenchmarkOutcomeWriter
	catalog  adaptive.EvaluatorCatalog

	fixtureAliases adaptiveBenchmarkFixtureAliasResolver
	ownerID        uuid.UUID
	runID          uuid.UUID
	providerID     string
	modelID        string

	newConversationID func() (uuid.UUID, error)
	now               func() time.Time
	cleanupTimeout    time.Duration
}

type adaptiveBenchmarkAuraDriver struct {
	adaptiveBenchmarkAuraDriverDeps

	mu      sync.Mutex
	pending map[string]adaptiveBenchmarkPendingEvaluation
}

type adaptiveBenchmarkPendingEvaluation struct {
	assignment        adaptive.Assignment
	deliveryEvent     adaptive.Event
	deliverySequence  int64
	outcomeObservedAt time.Time
}

func newAdaptiveBenchmarkAuraDriverWithDeps(
	deps adaptiveBenchmarkAuraDriverDeps,
) (*adaptiveBenchmarkAuraDriver, error) {
	if deps.runner == nil ||
		deps.recorder == nil ||
		deps.ledger == nil ||
		deps.outcomes == nil ||
		deps.fixtureAliases == nil ||
		deps.ownerID == uuid.Nil ||
		deps.runID == uuid.Nil ||
		strings.TrimSpace(deps.providerID) == "" ||
		strings.TrimSpace(deps.modelID) == "" ||
		deps.newConversationID == nil ||
		deps.now == nil ||
		deps.cleanupTimeout <= 0 {
		return nil, errors.New(
			"adaptive benchmark Aura driver dependencies are invalid",
		)
	}
	return &adaptiveBenchmarkAuraDriver{
		adaptiveBenchmarkAuraDriverDeps: deps,
		pending: make(
			map[string]adaptiveBenchmarkPendingEvaluation,
		),
	}, nil
}

func (driver *adaptiveBenchmarkAuraDriver) RunScenario(
	ctx context.Context,
	subject auraeval.AdaptiveBenchmarkSubject,
) (auraeval.AdaptiveBenchmarkDriverResult, error) {
	if err := driver.validateSubject(subject); err != nil {
		return driver.failure(
			auraeval.AdaptiveBenchmarkDriverResult{},
			auraeval.AdaptiveBenchmarkReasonDriverFailure,
		)
	}
	conversationID, err := driver.newConversationID()
	if err != nil ||
		conversationID == uuid.Nil ||
		conversationID.Version() != 7 {
		return driver.failure(
			auraeval.AdaptiveBenchmarkDriverResult{},
			auraeval.AdaptiveBenchmarkReasonDriverFailure,
		)
	}
	ownerCtx := identityctx.WithIdentityID(
		ctx,
		driver.ownerID.String(),
	)
	if _, err := driver.runner.NewConversationWithID(
		ownerCtx,
		conversationID.String(),
	); err != nil {
		result, failureErr := driver.failure(
			auraeval.AdaptiveBenchmarkDriverResult{},
			auraeval.AdaptiveBenchmarkReasonDriverFailure,
		)
		cleanupErr := driver.cleanupConversation(
			ownerCtx,
			conversationID.String(),
		)
		return result, errors.Join(failureErr, cleanupErr)
	}

	result, pending, runErr := driver.runTurn(
		ownerCtx,
		conversationID.String(),
		subject,
	)
	cleanupErr := driver.cleanupConversation(
		ownerCtx,
		conversationID.String(),
	)
	if runErr != nil {
		return result, errors.Join(runErr, cleanupErr)
	}
	if cleanupErr != nil {
		failed, failureErr := driver.failure(
			result,
			auraeval.AdaptiveBenchmarkReasonDriverFailure,
		)
		return failed, errors.Join(failureErr, cleanupErr)
	}
	driver.mu.Lock()
	if _, exists := driver.pending[subject.ScenarioID]; exists {
		driver.mu.Unlock()
		return driver.failure(
			result,
			auraeval.AdaptiveBenchmarkReasonFactLinkageInvalid,
		)
	}
	driver.pending[subject.ScenarioID] = pending
	driver.mu.Unlock()
	return result, nil
}

func (driver *adaptiveBenchmarkAuraDriver) runTurn(
	ctx context.Context,
	conversationID string,
	subject auraeval.AdaptiveBenchmarkSubject,
) (
	auraeval.AdaptiveBenchmarkDriverResult,
	adaptiveBenchmarkPendingEvaluation,
	error,
) {
	message, err := adaptiveBenchmarkSubjectJSON(subject)
	if err != nil {
		result, failureErr := driver.failure(
			auraeval.AdaptiveBenchmarkDriverResult{},
			auraeval.AdaptiveBenchmarkReasonDriverFailure,
		)
		return result, adaptiveBenchmarkPendingEvaluation{}, failureErr
	}
	assignmentBaseline := driver.capturedAssignmentBaseline(subject.Domain)
	started := driver.now()
	var terminal *agent.Event
	var completed time.Time
	var requestID uuid.UUID
	recoverParserFailure := func(
		elapsed time.Duration,
	) (
		auraeval.AdaptiveBenchmarkDriverResult,
		adaptiveBenchmarkPendingEvaluation,
		error,
	) {
		return driver.recoverTurnFailure(
			ctx,
			requestID,
			subject.Domain,
			auraeval.AdaptiveBenchmarkReasonModelParserFailure,
			elapsed,
			assignmentBaseline,
		)
	}
	for event, turnErr := range driver.runner.Turn(
		ctx,
		conversationID,
		&message,
	) {
		if turnErr != nil {
			if errors.Is(
				turnErr,
				auraeval.ErrAdaptiveBenchmarkUnsafeDispatch,
			) {
				return auraeval.AdaptiveBenchmarkDriverResult{},
					adaptiveBenchmarkPendingEvaluation{},
					turnErr
			}
			if cancellation := adaptiveBenchmarkCancellation(
				ctx,
				turnErr,
			); cancellation != nil {
				return auraeval.AdaptiveBenchmarkDriverResult{},
					adaptiveBenchmarkPendingEvaluation{},
					cancellation
			}
			reason := adaptiveBenchmarkTurnFailureReason(turnErr)
			elapsed := time.Duration(0)
			switch reason {
			case auraeval.AdaptiveBenchmarkReasonModelTransportFailure,
				auraeval.AdaptiveBenchmarkReasonModelParserFailure:
				elapsed = driver.now().Sub(started)
			}
			return driver.recoverTurnFailure(
				ctx,
				requestID,
				subject.Domain,
				reason,
				elapsed,
				assignmentBaseline,
			)
		}
		if event == nil ||
			event.RequestID == uuid.Nil ||
			event.RequestID.Version() != 7 ||
			(requestID != uuid.Nil && requestID != event.RequestID) {
			result, failureErr := driver.failure(
				auraeval.AdaptiveBenchmarkDriverResult{},
				auraeval.AdaptiveBenchmarkReasonRequestMissing,
			)
			return result, adaptiveBenchmarkPendingEvaluation{},
				failureErr
		}
		requestID = event.RequestID
		if err := validateAdaptiveBenchmarkDispatch(event); err != nil {
			return auraeval.AdaptiveBenchmarkDriverResult{},
				adaptiveBenchmarkPendingEvaluation{},
				err
		}
		if event.LLMResponse != nil &&
			event.LLMResponse.FinishReason != "" {
			if terminal != nil {
				return recoverParserFailure(driver.now().Sub(started))
			}
			terminal = event
			completed = driver.now()
		}
	}
	if cancellation := adaptiveBenchmarkCancellation(ctx, nil); cancellation != nil {
		return auraeval.AdaptiveBenchmarkDriverResult{},
			adaptiveBenchmarkPendingEvaluation{},
			cancellation
	}
	if terminal == nil || requestID == uuid.Nil {
		return recoverParserFailure(driver.now().Sub(started))
	}
	latency := completed.Sub(started)
	promptTokens, completionTokens, cost, err :=
		adaptiveBenchmarkTerminalUsage(terminal.Actions.StateDelta)
	if err != nil {
		return recoverParserFailure(latency)
	}
	if latency <= 0 {
		result, failureErr := driver.failure(
			auraeval.AdaptiveBenchmarkDriverResult{},
			auraeval.AdaptiveBenchmarkReasonProvenanceMissing,
		)
		return result, adaptiveBenchmarkPendingEvaluation{}, failureErr
	}
	facts, err := driver.recorder.PersistedFacts(
		ctx,
		driver.ownerID,
		requestID,
		subject.Domain,
	)
	if err != nil {
		result := auraeval.AdaptiveBenchmarkDriverResult{
			RequestID: requestID.String(),
			LatencyNS: latency.Nanoseconds(),
		}
		reason := auraeval.AdaptiveBenchmarkReasonFactLinkageInvalid
		switch {
		case errors.Is(
			err,
			errAdaptiveBenchmarkAssignmentFactMissing,
		):
			reason = auraeval.AdaptiveBenchmarkReasonAssignmentMissing
		case errors.Is(
			err,
			errAdaptiveBenchmarkDeliveryFactMissing,
		):
			reason = auraeval.AdaptiveBenchmarkReasonDeliveryMissing
		}
		failed, failureErr := driver.failure(
			result,
			reason,
		)
		return failed, adaptiveBenchmarkPendingEvaluation{}, failureErr
	}
	if facts.Assignment.ProviderID != driver.providerID ||
		facts.Assignment.ModelID != driver.modelID {
		result, failureErr := driver.failure(
			auraeval.AdaptiveBenchmarkDriverResult{
				RequestID: requestID.String(),
			},
			auraeval.AdaptiveBenchmarkReasonProvenanceMissing,
		)
		return result, adaptiveBenchmarkPendingEvaluation{}, failureErr
	}
	result, err := adaptiveBenchmarkDriverResultFromFacts(
		facts,
		terminal,
		latency,
		promptTokens,
		completionTokens,
		cost,
		driver.fixtureAliases,
	)
	if err != nil {
		reason := auraeval.AdaptiveBenchmarkReasonControlFailed
		if errors.Is(err, errAdaptiveBenchmarkFixtureAliasMissing) {
			reason = auraeval.AdaptiveBenchmarkReasonFactLinkageInvalid
		}
		failed, failureErr := driver.failure(
			result,
			reason,
		)
		return failed, adaptiveBenchmarkPendingEvaluation{}, failureErr
	}
	return result, adaptiveBenchmarkPendingEvaluation{
		assignment:       facts.Assignment,
		deliveryEvent:    facts.DeliveryEvent,
		deliverySequence: facts.DeliverySequence,
	}, nil
}

func (driver *adaptiveBenchmarkAuraDriver) validateSubject(
	subject auraeval.AdaptiveBenchmarkSubject,
) error {
	if driver == nil ||
		subject.ScenarioID == "" ||
		strings.TrimSpace(subject.Prompt) == "" {
		return errors.New("adaptive benchmark subject is invalid")
	}
	switch subject.Domain {
	case adaptive.DomainReasoning,
		adaptive.DomainToolDiscovery,
		adaptive.DomainSkillRouting,
		adaptive.DomainKnowledge,
		adaptive.DomainMemoryRecall:
		return nil
	default:
		return errors.New("adaptive benchmark subject domain is invalid")
	}
}

func (driver *adaptiveBenchmarkAuraDriver) cleanupConversation(
	ctx context.Context,
	conversationID string,
) error {
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		driver.cleanupTimeout,
	)
	defer cancel()
	affected, err := driver.runner.DeleteConversationLifecycle(
		cleanupCtx,
		driver.ownerID.String(),
		conversationID,
	)
	if err != nil {
		return fmt.Errorf("adaptive benchmark cleanup: %w", err)
	}
	if affected != 1 {
		return errors.New(
			"adaptive benchmark cleanup did not delete its conversation",
		)
	}
	return nil
}

func (driver *adaptiveBenchmarkAuraDriver) failure(
	result auraeval.AdaptiveBenchmarkDriverResult,
	reason string,
) (auraeval.AdaptiveBenchmarkDriverResult, error) {
	result.FailureReasonCodes = []string{reason}
	return result, fmt.Errorf(
		"%w: %s",
		errAdaptiveBenchmarkScenarioFailure,
		reason,
	)
}

var _ auraeval.AdaptiveBenchmarkDriver = (*adaptiveBenchmarkAuraDriver)(nil)
