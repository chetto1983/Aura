package agent

import (
	"context"
	"errors"
	"reflect"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// ReasoningAction is one frozen, already-supported reasoning control.
type ReasoningAction string

const (
	// ReasoningActionHigh selects the existing high reasoning tier.
	ReasoningActionHigh ReasoningAction = "reasoning_high"
	// ReasoningActionLow selects the existing low reasoning tier.
	ReasoningActionLow ReasoningAction = "reasoning_low"
	// ReasoningActionNone selects the existing no-reasoning tier.
	ReasoningActionNone ReasoningAction = "reasoning_none"
	// ReasoningActionStatic preserves Aura's existing static tier decision.
	ReasoningActionStatic ReasoningAction = "static"
)

// ReasoningControlInput is the privacy-safe identity of one primary-model round.
type ReasoningControlInput struct {
	OwnerID       uuid.UUID
	RequestID     uuid.UUID
	PointOrdinal  uint32
	MessageCount  int
	ProviderID    string
	BaseURL       string
	ModelID       string
	StaticTier    prompt.ReasoningTier
	StaticTierSet bool
	Override      llm.ReasoningEffort
}

// PreparedReasoningRequest holds a request until its delivery fact is durable.
type PreparedReasoningRequest struct {
	Request    llm.Request
	HookResult *ModelHookResult
}

// ReasoningRequestBuilder constructs and validates one selected request.
type ReasoningRequestBuilder func(
	context.Context,
	ReasoningAction,
) (PreparedReasoningRequest, error)

// StaticReasoningRequestBuilder reconstructs Aura's static request on fallback.
type StaticReasoningRequestBuilder func(
	context.Context,
) (PreparedReasoningRequest, error)

// ReasoningControl coordinates typed assignment, request construction, and delivery.
type ReasoningControl interface {
	DecideAndDeliverReasoning(
		context.Context,
		ReasoningControlInput,
		ReasoningRequestBuilder,
		StaticReasoningRequestBuilder,
	) (PreparedReasoningRequest, error)
}

func (a *LlmAgent) prepareReasoningRequest(
	ctx context.Context,
	budget prompt.Budget,
	round modelRound,
	staticTier prompt.ReasoningTier,
	staticTierSet bool,
) (PreparedReasoningRequest, error) {
	static := func(context.Context) (PreparedReasoningRequest, error) {
		return a.prepareStaticRequest(ctx, budget, staticTier, staticTierSet, round.requestID)
	}
	if a.reasoningControl == nil {
		return static(ctx)
	}
	ownerID, err := reasoningOwnerID(ctx)
	if err != nil {
		return static(ctx)
	}
	input := ReasoningControlInput{
		OwnerID: ownerID, RequestID: round.requestID, PointOrdinal: round.ordinal,
		MessageCount: len(a.history),
		ProviderID:   a.cfg.Provider, BaseURL: a.cfg.BaseURL, ModelID: a.cfg.Model,
		StaticTier: staticTier, StaticTierSet: staticTierSet,
		Override: a.reasoningOverride,
	}
	return a.reasoningControl.DecideAndDeliverReasoning(
		ctx,
		input,
		func(
			ctx context.Context,
			action ReasoningAction,
		) (PreparedReasoningRequest, error) {
			return a.prepareAssignedRequest(
				ctx, budget, staticTier, staticTierSet, input, action,
			)
		},
		static,
	)
}

func (a *LlmAgent) prepareAssignedRequest(
	ctx context.Context,
	budget prompt.Budget,
	staticTier prompt.ReasoningTier,
	staticTierSet bool,
	input ReasoningControlInput,
	action ReasoningAction,
) (PreparedReasoningRequest, error) {
	request, ok := a.buildReasoningActionRequest(
		budget, staticTier, staticTierSet, action,
	)
	if !ok {
		return PreparedReasoningRequest{}, errors.New("reasoning action is unsupported")
	}
	request.SessionID = a.sessionID
	request = a.attachDynamicTail(request)
	expected := request
	hookResult, err := a.transformModelRequest(ctx, &request, input.RequestID)
	if err != nil {
		return PreparedReasoningRequest{}, err
	}
	prepared := PreparedReasoningRequest{
		Request: request, HookResult: hookResult,
	}
	if hookResult != nil {
		return prepared, nil
	}
	if err := validateAssignedReasoningRequest(
		request, expected, input, a.cfg.MaxTokens, a.sessionID,
	); err != nil {
		return PreparedReasoningRequest{}, err
	}
	return prepared, nil
}

func (a *LlmAgent) prepareStaticRequest(
	ctx context.Context,
	budget prompt.Budget,
	staticTier prompt.ReasoningTier,
	staticTierSet bool,
	requestID uuid.UUID,
) (PreparedReasoningRequest, error) {
	request := a.buildRequest(budget, staticTier, staticTierSet)
	request.SessionID = a.sessionID
	request = a.attachDynamicTail(request)
	hookResult, err := a.transformModelRequest(ctx, &request, requestID)
	if err != nil {
		return PreparedReasoningRequest{}, err
	}
	return PreparedReasoningRequest{
		Request: request, HookResult: hookResult,
	}, nil
}

func (a *LlmAgent) transformModelRequest(
	ctx context.Context,
	request *llm.Request,
	requestID uuid.UUID,
) (*ModelHookResult, error) {
	prefixBefore := prefixSnapshot(request.Messages)
	result, err := a.hooks.BeforeModel(ctx, request)
	if err != nil {
		return nil, err
	}
	a.checkPrefixDrift(prefixBefore, request.Messages, requestID.String())
	return result, nil
}

func (a *LlmAgent) buildReasoningActionRequest(
	budget prompt.Budget,
	staticTier prompt.ReasoningTier,
	staticTierSet bool,
	action ReasoningAction,
) (llm.Request, bool) {
	if action == ReasoningActionStatic {
		return a.buildRequest(budget, staticTier, staticTierSet), true
	}
	tier, ok := reasoningTierForAction(action)
	if !ok {
		return llm.Request{}, false
	}
	if llm.ReasoningTarget(a.cfg.Provider, a.cfg.BaseURL) ==
		llm.ReasoningTargetLlamaCpp {
		return a.builder.BuildWithReasoningOverride(
			a.history, a.registry, a.cfg.Provider, a.cfg, budget,
			llm.ReasoningEffort(tier), a.activated,
		), true
	}
	return a.builder.BuildWithReasoningTier(
		a.history, a.registry, a.cfg.Provider, a.cfg, budget, tier, a.activated,
	), true
}

func reasoningTierForAction(
	action ReasoningAction,
) (prompt.ReasoningTier, bool) {
	switch action {
	case ReasoningActionHigh:
		return prompt.ReasoningTierHigh, true
	case ReasoningActionLow:
		return prompt.ReasoningTierLow, true
	case ReasoningActionNone:
		return prompt.ReasoningTierNone, true
	default:
		return "", false
	}
}

func validateAssignedReasoningRequest(
	request llm.Request,
	expected llm.Request,
	input ReasoningControlInput,
	maxTokens int,
	sessionID string,
) error {
	switch {
	case request.Model != input.ModelID:
		return errors.New("reasoning request model changed after assignment")
	case request.SessionID != sessionID:
		return errors.New("reasoning request session changed after assignment")
	case request.MaxTokens <= 0 || request.MaxTokens > maxTokens:
		return errors.New("reasoning request max_tokens exceeds the configured cap")
	case !equalReasoningConfig(request.Reasoning, expected.Reasoning):
		return errors.New("reasoning request effort changed after assignment")
	}
	return nil
}

func equalReasoningConfig(left, right llm.ReasoningConfig) bool {
	return left.Effort == right.Effort &&
		left.MaxTokens == right.MaxTokens &&
		reflect.DeepEqual(left.Exclude, right.Exclude) &&
		reflect.DeepEqual(left.Enabled, right.Enabled)
}

func reasoningOwnerID(ctx context.Context) (uuid.UUID, error) {
	ownerID, err := uuid.Parse(identityctx.IdentityID(ctx))
	if err != nil || ownerID == uuid.Nil {
		return uuid.Nil, errors.New("reasoning assignment requires an owner UUID")
	}
	return ownerID, nil
}
