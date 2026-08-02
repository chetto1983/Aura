package agent

import (
	"context"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// PreparedReasoningRequest carries one built model request together with any
// BeforeModel hook interception, so the run loop can short-circuit on a hook
// result without re-deriving the request.
type PreparedReasoningRequest struct {
	Request    llm.Request
	HookResult *ModelHookResult
}

// prepareReasoningRequest builds the model request for one round at the tier the
// adaptive reasoning classifier picked (llm_agent_reasoning.go), then runs the
// BeforeModel hooks over it.
func (a *LlmAgent) prepareReasoningRequest(
	ctx context.Context,
	budget prompt.Budget,
	round modelRound,
	tier prompt.ReasoningTier,
	tierSet bool,
) (PreparedReasoningRequest, error) {
	request := a.buildRequest(budget, tier, tierSet)
	request.SessionID = a.sessionID
	hookResult, err := a.transformModelRequest(ctx, &request, round.requestID)
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
