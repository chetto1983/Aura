package openai_compat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
)

func (c *Client) buildSDKRequest(ctx context.Context, req llm.Request) (openai.ChatCompletionNewParams, []option.RequestOption, int, error) {
	native := c.projectNativeMedia(ctx, req.ContentProjection)
	messages, err := toSDKMessages(req.Messages, native)
	if err != nil {
		return openai.ChatCompletionNewParams{}, nil, 0, err
	}
	tools, err := toSDKTools(req.Tools)
	if err != nil {
		return openai.ChatCompletionNewParams{}, nil, 0, err
	}
	choice := effectiveToolChoice(req.ToolChoice)
	if choice == "none" {
		tools = nil
	}
	params := openai.ChatCompletionNewParams{
		Model:       shared.ChatModel(req.Model),
		Messages:    messages,
		Tools:       tools,
		Temperature: param.NewOpt(req.Temperature),
		ToolChoice: openai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: param.NewOpt(choice),
		},
	}
	if req.MaxTokens > 0 {
		params.MaxTokens = param.NewOpt(int64(req.MaxTokens))
	}
	if req.Sampling.TopP != nil {
		params.TopP = param.NewOpt(*req.Sampling.TopP)
	}
	if req.Sampling.PresencePenalty != nil {
		params.PresencePenalty = param.NewOpt(*req.Sampling.PresencePenalty)
	}
	var requestOpts []option.RequestOption
	if llm.ReasoningTarget(c.cfg.Provider, c.cfg.BaseURL) == llm.ReasoningTargetOpenRouter {
		requestOpts = append(requestOpts, option.WithJSONSet("provider", map[string]string{"data_collection": "deny"}))
		if req.SessionID != "" {
			requestOpts = append(requestOpts, option.WithJSONSet("session_id", req.SessionID))
		}
	}
	requestOpts = appendSamplingOptions(requestOpts, req.Sampling,
		llm.ReasoningTarget(c.cfg.Provider, c.cfg.BaseURL))
	requestOpts = appendReasoningOptions(requestOpts, c.cfg, req.Reasoning, req.MaxTokens, &params)
	if llm.ReasoningTarget(c.cfg.Provider, c.cfg.BaseURL) == llm.ReasoningTargetOpenRouter && c.cfg.OpenRouterMiddleOut {
		requestOpts = append(requestOpts, option.WithJSONSet("transforms", []string{"middle-out"}))
	}
	return params, requestOpts, len(native), nil
}

func (c *Client) projectNativeMedia(ctx context.Context, projection *llm.ContentProjection) []llm.ProjectedRequestPart {
	if projection == nil || projection.Loader == nil || len(projection.ReferenceIDs) == 0 || c.contentCaps == nil {
		return nil
	}
	caps, detected := c.contentCaps.ContentCapabilities(ctx)
	if !detected {
		return nil
	}
	out := make([]llm.ProjectedRequestPart, 0, len(projection.ReferenceIDs))
	for _, id := range projection.ReferenceIDs {
		part, err := llm.ProjectContentPart(ctx, projection.Loader, projection.Principal, id, caps)
		if err != nil || part.ReferenceOnly || len(part.Bytes) == 0 {
			continue
		}
		out = append(out, part)
	}
	return out
}

func toSDKMessages(messages []llm.Message, native []llm.ProjectedRequestPart) ([]openai.ChatCompletionMessageParamUnion, error) {
	lastUser := -1
	if len(native) > 0 {
		for i, message := range slices.Backward(messages) {
			if message.Role == llm.RoleUser {
				lastUser = i
				break
			}
		}
	}
	out := make([]openai.ChatCompletionMessageParamUnion, 0, len(messages))
	for i, message := range messages {
		switch message.Role {
		case llm.RoleSystem:
			wire := openai.SystemMessage(message.Content)
			if message.Name != "" {
				wire.OfSystem.Name = param.NewOpt(message.Name)
			}
			out = append(out, wire)
		case llm.RoleUser:
			if i != lastUser {
				wire := openai.UserMessage(message.Content)
				if message.Name != "" {
					wire.OfUser.Name = param.NewOpt(message.Name)
				}
				out = append(out, wire)
				continue
			}
			parts := []openai.ChatCompletionContentPartUnionParam{openai.TextContentPart(message.Content)}
			for _, media := range native {
				if part, ok := nativeContentPart(media); ok {
					parts = append(parts, part)
				}
			}
			wire := openai.UserMessage(parts)
			if message.Name != "" {
				wire.OfUser.Name = param.NewOpt(message.Name)
			}
			out = append(out, wire)
		case llm.RoleAssistant:
			wire := openai.ChatCompletionMessageParamUnion{OfAssistant: &openai.ChatCompletionAssistantMessageParam{}}
			if message.Content != "" {
				wire.OfAssistant.Content.OfString = param.NewOpt(message.Content)
			}
			if message.Name != "" {
				wire.OfAssistant.Name = param.NewOpt(message.Name)
			}
			for _, call := range message.ToolCalls {
				wire.OfAssistant.ToolCalls = append(wire.OfAssistant.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID: call.ID,
						Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name: call.Function.Name, Arguments: call.Function.Arguments,
						},
					},
				})
			}
			out = append(out, wire)
		case llm.RoleTool:
			out = append(out, openai.ToolMessage(message.Content, message.ToolCallID))
		default:
			return nil, fmt.Errorf("openai_compat: unsupported message role %q", message.Role)
		}
	}
	return out, nil
}

func nativeContentPart(media llm.ProjectedRequestPart) (openai.ChatCompletionContentPartUnionParam, bool) {
	major, _, ok := strings.Cut(strings.ToLower(strings.TrimSpace(media.MIMEType)), "/")
	if !ok {
		return openai.ChatCompletionContentPartUnionParam{}, false
	}
	encoded := base64.StdEncoding.EncodeToString(media.Bytes)
	switch major {
	case "image":
		return openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
			URL: "data:" + media.MIMEType + ";base64," + encoded,
		}), true
	case "audio":
		format, ok := audioFormat(media.MIMEType)
		if !ok {
			return openai.ChatCompletionContentPartUnionParam{}, false
		}
		return openai.InputAudioContentPart(openai.ChatCompletionContentPartInputAudioInputAudioParam{
			Data: encoded, Format: format,
		}), true
	default:
		return openai.ChatCompletionContentPartUnionParam{}, false
	}
}

func audioFormat(mimeType string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(strings.SplitN(mimeType, ";", 2)[0]))
	formats := map[string]string{
		"audio/wav": "wav", "audio/x-wav": "wav", "audio/wave": "wav",
		"audio/mpeg": "mp3", "audio/mp3": "mp3",
		"audio/flac": "flac", "audio/x-flac": "flac",
		"audio/mp4": "m4a", "audio/x-m4a": "m4a", "audio/m4a": "m4a",
		"audio/ogg": "ogg",
	}
	format, ok := formats[normalized]
	return format, ok
}

func toSDKTools(tools []llm.ToolDef) ([]openai.ChatCompletionToolUnionParam, error) {
	out := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, tool := range tools {
		var parameters shared.FunctionParameters
		if len(tool.Function.Parameters) > 0 {
			if err := json.Unmarshal(tool.Function.Parameters, &parameters); err != nil {
				return nil, fmt.Errorf("openai_compat: tool %q parameters: %w", tool.Function.Name, err)
			}
		}
		definition := shared.FunctionDefinitionParam{Name: tool.Function.Name, Parameters: parameters}
		if tool.Function.Description != "" {
			definition.Description = param.NewOpt(tool.Function.Description)
		}
		out = append(out, openai.ChatCompletionFunctionTool(definition))
	}
	return out, nil
}

func appendSamplingOptions(opts []option.RequestOption, sampling llm.Sampling, target llm.ReasoningTargetKind) []option.RequestOption {
	if sampling.TopK != nil {
		opts = append(opts, option.WithJSONSet("top_k", *sampling.TopK))
	}
	if sampling.MinP != nil {
		opts = append(opts, option.WithJSONSet("min_p", *sampling.MinP))
	}
	if sampling.RepetitionPenalty != nil {
		opts = append(opts, option.WithJSONSet(repetitionPenaltyField(target), *sampling.RepetitionPenalty))
	}
	return opts
}

// repetitionPenaltyField names the same knob the way each backend spells it.
//
// The field is not standard OpenAI, so there is no single name and a server that does not
// recognise the one it is given ignores it in silence — no error, no warning, and a
// setting the operator believes is in force. Measured on 26.9.1 against a live
// llama-server by reading /slots back:
//
//	repetition_penalty: 3.0  ->  server reports repeat_penalty 1.0  (dropped)
//	repeat_penalty:     3.0  ->  server reports repeat_penalty 3.0  (applied)
//
// llama.cpp documents `repeat_penalty` (tools/server/README.md); `repetition_penalty` is
// vLLM's and HuggingFace's spelling, which OpenRouter also takes, so it stays the default
// for everything else. Ollama is deliberately left on that default rather than guessed at:
// its OpenAI bridge was not measured here, and a second wrong name would be the same bug
// with more confidence.
func repetitionPenaltyField(target llm.ReasoningTargetKind) string {
	if target == llm.ReasoningTargetLlamaCpp {
		return "repeat_penalty"
	}
	return "repetition_penalty"
}

func appendReasoningOptions(opts []option.RequestOption, cfg llm.Config, reasoning llm.ReasoningConfig, maxTokens int, params *openai.ChatCompletionNewParams) []option.RequestOption {
	target := llm.ReasoningTarget(cfg.Provider, cfg.BaseURL)
	if target == llm.ReasoningTargetOllama {
		params.StreamOptions.IncludeUsage = param.NewOpt(true)
		if effort, ok := ollamaReasoningEffort(reasoning); ok {
			return append(opts, option.WithJSONSet("reasoning_effort", effort))
		}
		return opts
	}
	if reasoning.Empty() {
		if target == llm.ReasoningTargetLlamaCpp {
			params.StreamOptions.IncludeUsage = param.NewOpt(true)
		}
		return opts
	}
	if target == llm.ReasoningTargetLlamaCpp {
		params.StreamOptions.IncludeUsage = param.NewOpt(true)
		budget, thinking := llamaCppReasoning(reasoning)
		if budget != nil && *budget > 0 {
			fittedMax, fittedBudget := fitThinkingBudget(maxTokens, *budget, llm.OutputReserve(cfg.ContextWindow, cfg.MaxOutputTokens))
			if fittedMax > 0 {
				params.MaxTokens = param.NewOpt(int64(fittedMax))
			}
			if fittedBudget == 0 {
				return append(opts, option.WithJSONSet("chat_template_kwargs", map[string]bool{"enable_thinking": false}))
			}
			budget = &fittedBudget
		}
		if budget != nil {
			return append(opts, option.WithJSONSet("thinking_budget_tokens", *budget))
		}
		if thinking != nil {
			return append(opts, option.WithJSONSet("chat_template_kwargs", map[string]bool{"enable_thinking": *thinking}))
		}
		return opts
	}
	wire := map[string]any{}
	if reasoning.Effort != "" {
		wire["effort"] = string(reasoning.Effort)
	}
	if reasoning.MaxTokens != 0 {
		wire["max_tokens"] = reasoning.MaxTokens
	}
	if reasoning.Exclude != nil {
		wire["exclude"] = *reasoning.Exclude
	}
	if reasoning.Enabled != nil {
		wire["enabled"] = *reasoning.Enabled
	}
	return append(opts, option.WithJSONSet("reasoning", wire))
}

func ollamaReasoningEffort(reasoning llm.ReasoningConfig) (string, bool) {
	if reasoning.Enabled != nil && !*reasoning.Enabled {
		return "none", true
	}
	switch reasoning.Effort {
	case llm.ReasoningEffortNone, llm.ReasoningEffortLow,
		llm.ReasoningEffortMedium, llm.ReasoningEffortHigh:
		return string(reasoning.Effort), true
	case llm.ReasoningEffortMinimal:
		return "low", true
	case llm.ReasoningEffortXHigh, llm.ReasoningEffortMax:
		return "high", true
	default:
		return "", false
	}
}

// answerFloorTokens is the smallest visible-answer allowance kept after a numeric
// thinking budget when max_tokens cannot grow any further (pi-agent's
// adjustMaxTokensForThinking, read 2026-08-30).
const answerFloorTokens = 1024

// fitThinkingBudget sizes max_tokens and the thinking budget together (amendment
// #189). llama.cpp generates the reasoning inside max_tokens, so a budget at or above
// the cap ends the turn at finish_reason=length with reasoning and no answer
// (measured 2026-08-30: 8,192 budget under an 8,092 cap, 23,863-token completions
// with empty content). The cap grows by the budget up to ceiling — the context
// budget's output reserve — and never below the operator's cap; when that still
// leaves less than the answer floor, the budget shrinks instead, down to 0, which
// the caller sends as thinking off.
func fitThinkingBudget(maxTokens, budget, ceiling int) (fittedMax, fittedBudget int) {
	if maxTokens <= 0 || budget <= 0 {
		return maxTokens, budget
	}
	fittedMax = maxTokens + budget
	if ceiling > 0 {
		fittedMax = min(fittedMax, ceiling)
	}
	fittedMax = max(fittedMax, maxTokens)
	fittedBudget = budget
	if fittedMax-fittedBudget < answerFloorTokens {
		fittedBudget = max(fittedMax-answerFloorTokens, 0)
	}
	return fittedMax, fittedBudget
}

const (
	llamaCppBudgetLow   = 512
	llamaCppBudgetMid   = 2048
	llamaCppBudgetHigh  = 8192
	llamaCppBudgetExtra = 16384
	llamaCppBudgetMax   = -1
)

func llamaCppReasoning(reasoning llm.ReasoningConfig) (budget *int, thinking *bool) {
	switch reasoning.Effort {
	case llm.ReasoningEffortNone:
		value := false
		return nil, &value
	case llm.ReasoningEffortLow:
		return new(llamaCppBudgetLow), nil
	case llm.ReasoningEffortMedium:
		return new(llamaCppBudgetMid), nil
	case llm.ReasoningEffortHigh:
		return new(llamaCppBudgetHigh), nil
	case llm.ReasoningEffortXHigh:
		return new(llamaCppBudgetExtra), nil
	case llm.ReasoningEffortMax:
		return new(llamaCppBudgetMax), nil
	default:
		return nil, nil
	}
}
