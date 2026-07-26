package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	auraeval "github.com/chetto1983/aura/internal/eval"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

func adaptiveBenchmarkSubjectJSON(
	subject auraeval.AdaptiveBenchmarkSubject,
) (string, error) {
	payload, err := json.Marshal(struct {
		ScenarioID string          `json:"scenario_id"`
		Domain     adaptive.Domain `json:"domain"`
		Prompt     string          `json:"prompt"`
	}{
		ScenarioID: subject.ScenarioID,
		Domain:     subject.Domain,
		Prompt:     subject.Prompt,
	})
	return string(payload), err
}

func adaptiveBenchmarkDriverResultFromFacts(
	facts adaptiveBenchmarkPersistedFacts,
	terminal *agent.Event,
	latency time.Duration,
	promptTokens int64,
	completionTokens int64,
	cost *float64,
	fixtureAliases adaptiveBenchmarkFixtureAliasResolver,
) (auraeval.AdaptiveBenchmarkDriverResult, error) {
	if promptTokens > math.MaxInt64-completionTokens {
		return auraeval.AdaptiveBenchmarkDriverResult{},
			errors.New("adaptive benchmark token total overflows")
	}
	costBasis := auraeval.AdaptiveBenchmarkCostUnknown
	if cost != nil {
		costBasis = auraeval.AdaptiveBenchmarkCostProviderReported
	}
	observed, err := adaptiveBenchmarkObservedFromDelivery(
		facts.Assignment.Domain,
		terminal.LLMResponse.Content,
		facts.Delivery,
		fixtureAliases,
	)
	if err != nil {
		return auraeval.AdaptiveBenchmarkDriverResult{}, err
	}
	result := adaptiveBenchmarkDriverResultFromLinkage(facts)
	result.PromptTokens = promptTokens
	result.CompletionTokens = completionTokens
	result.TotalTokens = promptTokens + completionTokens
	result.LatencyNS = latency.Nanoseconds()
	result.CostUSD = cloneAdaptiveBenchmarkCost(cost)
	result.CostBasis = costBasis
	result.Observed = observed
	return result, nil
}

func adaptiveBenchmarkDriverResultFromLinkage(
	facts adaptiveBenchmarkPersistedFacts,
) auraeval.AdaptiveBenchmarkDriverResult {
	probabilities := make(
		[]auraeval.AdaptiveBenchmarkActionProbability,
		len(facts.Assignment.ActionProbabilities),
	)
	for index, probability := range facts.Assignment.ActionProbabilities {
		probabilities[index] = auraeval.AdaptiveBenchmarkActionProbability{
			ActionID:    probability.ActionID,
			Probability: probability.Probability,
		}
	}
	return auraeval.AdaptiveBenchmarkDriverResult{
		RequestID:           facts.Assignment.RequestID.String(),
		AssignmentID:        facts.Assignment.AssignmentID.String(),
		DeliveryEventID:     facts.DeliveryEvent.ID.String(),
		ChampionActionID:    facts.Assignment.ChampionActionID,
		RecommendedActionID: facts.Assignment.RecommendedActionID,
		IntendedActionID:    facts.Assignment.IntendedActionID,
		ActualActionID:      facts.Delivery.ActualActionID,
		ActionProbabilities: probabilities,
	}
}

func adaptiveBenchmarkObservedFromDelivery(
	domain adaptive.Domain,
	answer string,
	delivery adaptive.Delivery,
	fixtureAliases adaptiveBenchmarkFixtureAliasResolver,
) (auraeval.AdaptiveBenchmarkObserved, error) {
	observed := auraeval.AdaptiveBenchmarkObserved{Answer: answer}
	for _, result := range delivery.ResultIDs {
		switch {
		case domain == adaptive.DomainToolDiscovery &&
			result.Kind() == adaptive.ResultTool &&
			observed.ToolID == "":
			observed.ToolID = result.ID()
		case domain == adaptive.DomainSkillRouting &&
			result.Kind() == adaptive.ResultSkill &&
			observed.SkillID == "":
			observed.SkillID = result.ID()
		case domain == adaptive.DomainKnowledge ||
			domain == adaptive.DomainMemoryRecall:
			id, err := parseCanonicalAdaptiveBenchmarkUUID(result.ID())
			if err != nil || fixtureAliases == nil {
				return auraeval.AdaptiveBenchmarkObserved{},
					errAdaptiveBenchmarkFixtureAliasMissing
			}
			alias, ok := fixtureAliases.ResolveFixtureAlias(domain, id)
			if !ok || strings.TrimSpace(alias) == "" {
				return auraeval.AdaptiveBenchmarkObserved{},
					errAdaptiveBenchmarkFixtureAliasMissing
			}
			observed.ResultIDs = append(
				observed.ResultIDs,
				alias,
			)
		}
	}
	return observed, nil
}

func adaptiveBenchmarkTerminalUsage(
	state map[string]any,
) (int64, int64, *float64, error) {
	prompt, err := adaptiveBenchmarkStateInteger(
		state,
		"prompt_tokens",
	)
	if err != nil {
		return 0, 0, nil, err
	}
	completion, err := adaptiveBenchmarkStateInteger(
		state,
		"completion_tokens",
	)
	if err != nil {
		return 0, 0, nil, err
	}
	if prompt > math.MaxInt64-completion {
		return 0, 0, nil, errors.New(
			"adaptive benchmark token total overflows",
		)
	}
	rawCost, exists := state["cost_usd"]
	if !exists {
		return prompt, completion, nil, nil
	}
	cost, ok := adaptiveBenchmarkStateNumber(rawCost)
	if !ok ||
		math.IsNaN(cost) ||
		math.IsInf(cost, 0) ||
		cost < 0 ||
		math.Signbit(cost) {
		return 0, 0, nil, errors.New(
			"adaptive benchmark provider cost is invalid",
		)
	}
	return prompt, completion, &cost, nil
}

func adaptiveBenchmarkStateInteger(
	state map[string]any,
	key string,
) (int64, error) {
	if state == nil {
		return 0, nil
	}
	raw, exists := state[key]
	if !exists {
		return 0, nil
	}
	var value int64
	valid := true
	switch number := raw.(type) {
	case int:
		if number < 0 {
			valid = false
		} else {
			value = int64(number)
		}
	case int32:
		if number < 0 {
			valid = false
		} else {
			value = int64(number)
		}
	case int64:
		value, valid = number, number >= 0
	case float64:
		if math.IsNaN(number) ||
			math.IsInf(number, 0) ||
			number < 0 ||
			number >= float64(math.MaxInt64) ||
			math.Trunc(number) != number {
			valid = false
		} else {
			value = int64(number)
		}
	case json.Number:
		parsed, err := number.Int64()
		value, valid = parsed, err == nil && parsed >= 0
	default:
		valid = false
	}
	if !valid {
		return 0, fmt.Errorf("adaptive benchmark %s is invalid", key)
	}
	return value, nil
}

func adaptiveBenchmarkStateNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case float64:
		return number, true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func validateAdaptiveBenchmarkDispatch(event *agent.Event) error {
	invocation := event.Actions.ToolInvocation
	if invocation == nil ||
		invocation.Event != agent.ToolInvocationStart {
		return nil
	}
	switch invocation.ToolName {
	case "text_response",
		"tool_search",
		"document_search",
		"read_tool_output":
		return nil
	case "skill":
		decoder := json.NewDecoder(
			bytes.NewBufferString(invocation.Arguments),
		)
		decoder.DisallowUnknownFields()
		var input struct {
			Action string `json:"action"`
			Query  string `json:"query"`
		}
		if err := decoder.Decode(&input); err != nil ||
			decoder.Decode(&struct{}{}) != io.EOF ||
			input.Action != "list" ||
			strings.TrimSpace(input.Query) == "" ||
			input.Query != strings.TrimSpace(input.Query) {
			return auraeval.ErrAdaptiveBenchmarkUnsafeDispatch
		}
		return nil
	default:
		return auraeval.ErrAdaptiveBenchmarkUnsafeDispatch
	}
}

func adaptiveBenchmarkTurnFailureReason(err error) string {
	var httpErr *openai_compat.HTTPError
	var networkErr net.Error
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.As(err, &httpErr),
		errors.As(err, &networkErr),
		errors.Is(err, openai_compat.ErrStreamIdleTimeout),
		errors.Is(err, io.EOF),
		errors.Is(err, io.ErrUnexpectedEOF):
		return auraeval.AdaptiveBenchmarkReasonModelTransportFailure
	case errors.As(err, &syntaxErr),
		errors.As(err, &typeErr):
		return auraeval.AdaptiveBenchmarkReasonModelParserFailure
	case errors.Is(err, adaptive.ErrPayloadConflict),
		errors.Is(err, adaptive.ErrAssignmentNotFound),
		errors.Is(err, adaptive.ErrPersistedAssignmentInvalid):
		return auraeval.AdaptiveBenchmarkReasonControlFailed
	default:
		return auraeval.AdaptiveBenchmarkReasonDriverFailure
	}
}

func adaptiveBenchmarkCancellation(
	ctx context.Context,
	err error,
) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return nil
}

func cloneAdaptiveBenchmarkCost(cost *float64) *float64 {
	if cost == nil {
		return nil
	}
	cloned := *cost
	return &cloned
}
