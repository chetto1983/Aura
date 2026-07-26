package main

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/conversations"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

type adaptiveBenchmarkControlProbeTrace struct {
	mu          sync.Mutex
	assignments []adaptiveBenchmarkRecorderProbeFact
	deliveries  []adaptiveBenchmarkRecorderProbeFact
	assignedAt  map[uuid.UUID]time.Time
	deliveredAt map[uuid.UUID]time.Time
}

func newAdaptiveBenchmarkControlProbeTrace() *adaptiveBenchmarkControlProbeTrace {
	return &adaptiveBenchmarkControlProbeTrace{
		assignedAt:  make(map[uuid.UUID]time.Time),
		deliveredAt: make(map[uuid.UUID]time.Time),
	}
}

func (trace *adaptiveBenchmarkControlProbeTrace) afterCommit(
	fact adaptiveBenchmarkRecorderProbeFact,
) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	switch fact.Kind {
	case adaptive.EventDecision:
		trace.assignments = append(trace.assignments, fact)
		trace.assignedAt[fact.AssignmentID] = time.Now().UTC()
	case adaptive.EventDelivery:
		trace.deliveries = append(trace.deliveries, fact)
		trace.deliveredAt[fact.AssignmentID] = time.Now().UTC()
	}
}

func (runtime *adaptiveBenchmarkControlRuntime) runNegativeControl(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		control,
		"negative_control",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	subject, err := runtime.primarySubject()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	var factCalls atomic.Int64
	handle, err := subject.recorder.InstallProbe(
		adaptiveBenchmarkRecorderProbe{
			BeforeWrite: func(
				context.Context,
				adaptiveBenchmarkRecorderProbeFact,
			) error {
				factCalls.Add(1)
				return nil
			},
			AfterCommit: func(
				context.Context,
				adaptiveBenchmarkRecorderProbeFact,
			) error {
				return nil
			},
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	defer func() { _ = handle.Clear() }()
	var modelCalls atomic.Int64
	clearObserver := subject.observed.SetModelStartObserver(
		func(context.Context, string) {
			modelCalls.Add(1)
		},
	)
	defer clearObserver()
	conversationID, err := adaptiveBenchmarkControlCreateConversation(
		ctx,
		subject,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	turn, runErr := adaptiveBenchmarkControlRunTurn(
		ctx,
		subject,
		conversationID,
		"ciao",
	)
	deleteErr := adaptiveBenchmarkControlDeleteConversation(
		ctx,
		subject,
		conversationID,
	)
	if runErr != nil || deleteErr != nil {
		return adaptiveBenchmarkControlExecution{},
			errors.Join(runErr, deleteErr)
	}
	if factCalls.Load() != 0 || modelCalls.Load() != 0 {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark negative control reached adaptive execution",
			)
	}
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs: []uuid.UUID{
			turn.RequestID,
			conversationID,
		},
	}, nil
}

func (runtime *adaptiveBenchmarkControlRuntime) runStaticEquivalence(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		control,
		"static_equivalence",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	subject, err := runtime.primarySubject()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	scenario, err := adaptiveBenchmarkControlScenario("r01")
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	message, err := adaptiveBenchmarkSubjectJSON(
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: scenario.ScenarioID,
			Domain:     scenario.Domain,
			Prompt:     scenario.Prompt,
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	trace := newAdaptiveBenchmarkControlProbeTrace()
	handle, err := subject.recorder.InstallProbe(
		adaptiveBenchmarkRecorderProbe{
			BeforeWrite: func(
				context.Context,
				adaptiveBenchmarkRecorderProbeFact,
			) error {
				return nil
			},
			AfterCommit: func(
				_ context.Context,
				fact adaptiveBenchmarkRecorderProbeFact,
			) error {
				trace.afterCommit(fact)
				return nil
			},
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	defer func() { _ = handle.Clear() }()
	run := func(
		turnSubject adaptiveBenchmarkControlSubject,
		title string,
	) (adaptiveBenchmarkControlTurn, error) {
		conversationID, createErr :=
			adaptiveBenchmarkControlCreateConversation(ctx, turnSubject)
		if createErr != nil {
			return adaptiveBenchmarkControlTurn{}, createErr
		}
		ownerCtx := identityctx.WithIdentityID(
			ctx,
			turnSubject.ownerID.String(),
		)
		if renameErr := runtime.environment.env.conv.Rename(
			ownerCtx,
			conversationID.String(),
			title,
		); renameErr != nil {
			deleteErr := adaptiveBenchmarkControlDeleteConversation(
				ctx, turnSubject, conversationID,
			)
			return adaptiveBenchmarkControlTurn{},
				errors.Join(renameErr, deleteErr)
		}
		turn, turnErr := adaptiveBenchmarkControlRunTurn(
			ctx, turnSubject, conversationID, message,
		)
		deleteErr := adaptiveBenchmarkControlDeleteConversation(
			ctx, turnSubject, conversationID,
		)
		return turn, errors.Join(turnErr, deleteErr)
	}
	staticRunner, err := adaptiveBenchmarkStaticRunner(
		runtime.environment.env,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	staticSubject := subject
	staticSubject.runner = staticRunner
	baseline, err := run(
		staticSubject,
		"adaptive benchmark static baseline",
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	trace.mu.Lock()
	baselineRecordedFacts :=
		len(trace.assignments) != 0 || len(trace.deliveries) != 0
	trace.mu.Unlock()
	if baselineRecordedFacts {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark static baseline reached adaptive controls",
			)
	}
	var modelAt time.Time
	var modelRequest string
	var modelMu sync.Mutex
	clearObserver := subject.observed.SetModelStartObserver(
		func(_ context.Context, requestID string) {
			modelMu.Lock()
			modelAt = time.Now().UTC()
			modelRequest = requestID
			modelMu.Unlock()
		},
	)
	defer clearObserver()
	shadow, err := run(
		subject,
		"adaptive benchmark shadow equivalence",
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	facts, err := subject.recorder.PersistedFacts(
		ctx,
		subject.ownerID,
		shadow.RequestID,
		adaptive.DomainReasoning,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	result := adaptiveBenchmarkDriverResultFromLinkage(facts)
	if err := adaptiveBenchmarkValidateStaticEquivalence(
		[]byte(baseline.Terminal.LLMResponse.Content),
		[]byte(shadow.Terminal.LLMResponse.Content),
		result,
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	assignmentID := facts.Assignment.AssignmentID
	modelMu.Lock()
	observedModelAt := modelAt
	observedRequest := modelRequest
	modelMu.Unlock()
	trace.mu.Lock()
	assignedAt := trace.assignedAt[assignmentID]
	deliveredAt := trace.deliveredAt[assignmentID]
	trace.mu.Unlock()
	if assignedAt.IsZero() ||
		deliveredAt.IsZero() ||
		!assignedAt.Before(deliveredAt) ||
		observedModelAt.IsZero() ||
		observedModelAt.Before(deliveredAt) ||
		observedRequest != shadow.RequestID.String() {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark static delivery timing drifted",
			)
	}
	evidence, err := adaptiveBenchmarkControlEvidenceFromResult(result)
	evidence = append(
		evidence,
		baseline.RequestID,
		baseline.ConversationID,
		shadow.ConversationID,
	)
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs: evidence,
	}, err
}

func (runtime *adaptiveBenchmarkControlRuntime) runRestartRecovery(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		control,
		"restart_recovery",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	before, err := runtime.primarySubject()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	_, first, _, err := adaptiveBenchmarkControlRunScenario(
		ctx,
		before,
		"r01",
		nil,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	if err := adaptiveBenchmarkValidateStaticResult(first); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	if err := runtime.environment.Restart(ctx); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	after, err := runtime.primarySubject()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	_, second, _, err := adaptiveBenchmarkControlRunScenario(
		ctx,
		after,
		"r02",
		nil,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	if err := adaptiveBenchmarkValidateStaticResult(second); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	firstIDs, err := adaptiveBenchmarkControlEvidenceFromResult(first)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	secondIDs, err := adaptiveBenchmarkControlEvidenceFromResult(second)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	seen := make(map[uuid.UUID]struct{}, len(firstIDs))
	for _, id := range firstIDs {
		seen[id] = struct{}{}
	}
	for _, id := range secondIDs {
		if _, duplicate := seen[id]; duplicate {
			return adaptiveBenchmarkControlExecution{},
				adaptiveBenchmarkControlError(
					"adaptive benchmark restart reused typed fact identity",
				)
		}
	}
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs: append(firstIDs, secondIDs...),
	}, nil
}

func adaptiveBenchmarkValidateStaticResult(
	result auraeval.AdaptiveBenchmarkDriverResult,
) error {
	if result.ChampionActionID != adaptive.StaticActionID ||
		result.IntendedActionID != adaptive.StaticActionID ||
		result.ActualActionID != adaptive.StaticActionID ||
		len(result.ActionProbabilities) == 0 {
		return adaptiveBenchmarkControlError(
			"adaptive benchmark did not serve the static champion",
		)
	}
	staticProbability := float64(-1)
	for _, candidate := range result.ActionProbabilities {
		if candidate.ActionID == adaptive.StaticActionID {
			staticProbability = candidate.Probability
			break
		}
	}
	if staticProbability != 1 {
		return adaptiveBenchmarkControlError(
			"adaptive benchmark static action probability drifted",
		)
	}
	return nil
}

func (runtime *adaptiveBenchmarkControlRuntime) runPrivacyRedaction(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		control,
		"privacy_redaction",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	subject, err := runtime.primarySubject()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	const sentinel = "AURA-PRIVATE-CONTROL-SENTINEL-93-13"
	driver, err := subject.newDriver(nil)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	result, err := driver.RunScenario(
		ctx,
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: "privacy-control",
			Domain:     adaptive.DomainReasoning,
			Prompt:     sentinel,
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	recorded, err := driver.RecordEvaluation(
		ctx,
		auraeval.AdaptiveBenchmarkEvaluationRecord{
			ScenarioID:      "privacy-control",
			RequestID:       result.RequestID,
			AssignmentID:    result.AssignmentID,
			DeliveryEventID: result.DeliveryEventID,
			Evaluation: auraeval.AdaptiveBenchmarkEvaluation{
				Passed: false,
				ReasonCodes: []string{
					auraeval.AdaptiveBenchmarkReasonAnswerMismatch,
				},
			},
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	requestID, _ := uuid.Parse(result.RequestID)
	records, err := subject.ledger.ListAggregate(
		ctx,
		subject.ownerID,
		requestID.String(),
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	for _, record := range records {
		if bytes.Contains(record.Payload, []byte(sentinel)) ||
			bytes.Contains(record.Payload, []byte("expected")) ||
			bytes.Contains(record.Payload, []byte("tool_args")) ||
			bytes.Contains(record.Payload, []byte("content")) {
			return adaptiveBenchmarkControlExecution{},
				adaptiveBenchmarkControlError(
					"adaptive benchmark typed evidence contains private content",
				)
		}
	}
	evidence, err := adaptiveBenchmarkControlEvidenceFromResult(result)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	outcomeID, err := parseCanonicalAdaptiveBenchmarkUUID(
		recorded.QualityOutcomeID,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	evidence = append(evidence, outcomeID)
	return adaptiveBenchmarkControlExecution{EvidenceIDs: evidence}, nil
}

func (runtime *adaptiveBenchmarkControlRuntime) runRetentionCleanup(
	ctx context.Context,
	control auraeval.AdaptiveBenchmarkControlCase,
) (adaptiveBenchmarkControlExecution, error) {
	if err := adaptiveBenchmarkUnexpectedControlCase(
		control,
		"retention_cleanup",
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	subject, err := runtime.primarySubject()
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	scenario, err := adaptiveBenchmarkControlScenario("r01")
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	message, err := adaptiveBenchmarkSubjectJSON(
		auraeval.AdaptiveBenchmarkSubject{
			ScenarioID: scenario.ScenarioID,
			Domain:     scenario.Domain,
			Prompt:     scenario.Prompt,
		},
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	conversationID, err := adaptiveBenchmarkControlCreateConversation(
		ctx,
		subject,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	turn, runErr := adaptiveBenchmarkControlRunTurn(
		ctx,
		subject,
		conversationID,
		message,
	)
	if runErr != nil {
		_ = adaptiveBenchmarkControlDeleteConversation(
			ctx,
			subject,
			conversationID,
		)
		return adaptiveBenchmarkControlExecution{}, runErr
	}
	facts, err := subject.recorder.PersistedFacts(
		ctx,
		subject.ownerID,
		turn.RequestID,
		adaptive.DomainReasoning,
	)
	if err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	if err := adaptiveBenchmarkControlDeleteConversation(
		ctx,
		subject,
		conversationID,
	); err != nil {
		return adaptiveBenchmarkControlExecution{}, err
	}
	ownerCtx := identityctx.WithIdentityID(
		ctx,
		subject.ownerID.String(),
	)
	if _, err := subject.conversations.GetForIdentity(
		ownerCtx,
		conversationID.String(),
		subject.ownerID.String(),
	); !errors.Is(err, conversations.ErrConversationNotFound) {
		return adaptiveBenchmarkControlExecution{},
			adaptiveBenchmarkControlError(
				"adaptive benchmark retained a deleted control conversation",
			)
	}
	return adaptiveBenchmarkControlExecution{
		EvidenceIDs: []uuid.UUID{
			conversationID,
			turn.RequestID,
			facts.Assignment.AssignmentID,
			facts.DeliveryEvent.ID,
		},
	}, nil
}
