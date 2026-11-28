package anthropic

import (
	"fmt"
	"slices"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/chetto1983/aura/internal/llm"
)

// minMaxTokens is the floor of max_tokens on this route: adaptive thinking spends inside
// it, so a helper's 32-token cap or a 0 would end in `length` or a 400 (plan decision 4).
const minMaxTokens = 16_384

// efforts is output_config.effort's vocabulary, lowest first.
var efforts = []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh, llm.ReasoningEffortMax}

// ephemeral is the breakpoint every marker on this route carries: the shape the wire probe
// measured, whose TTL defaults to 5 minutes.
var ephemeral = anthropic.NewBetaCacheControlEphemeralParam()

type builtRequest struct {
	params   anthropic.BetaMessageNewParams
	effort   string
	replayed int
}

func (c *Client) model(req llm.Request) string {
	if req.Model != "" {
		return req.Model
	}
	return c.cfg.Model
}

func (c *Client) buildRequest(req llm.Request, media requestMedia) (builtRequest, error) {
	effort, err := c.effort(req.Reasoning.Effort)
	if err != nil {
		return builtRequest{}, err
	}
	if req.ToolChoice != "" && req.ToolChoice != "auto" && req.ToolChoice != "none" {
		return builtRequest{}, fmt.Errorf("anthropic: unsupported tool choice %q", req.ToolChoice)
	}
	tools, err := convertTools(req.Tools)
	if err != nil {
		return builtRequest{}, err
	}
	conv, err := convert(req, media)
	if err != nil {
		return builtRequest{}, err
	}
	conv.mark(req.LoadedHistoryMessages)
	conv.mark(req.StablePrefixMessages)
	if req.ToolsCacheControl == "ephemeral" {
		if len(tools) > 0 {
			tools[len(tools)-1].OfTool.CacheControl = ephemeral
		}
		if len(conv.system) > 0 {
			conv.system[len(conv.system)-1].CacheControl = ephemeral
		}
	}
	display := anthropic.BetaThinkingConfigAdaptiveDisplayOmitted
	if c.cfg.ShowReasoning {
		display = anthropic.BetaThinkingConfigAdaptiveDisplaySummarized
	}
	params := anthropic.BetaMessageNewParams{
		Model:     c.model(req),
		MaxTokens: int64(c.maxTokens(req.MaxTokens)),
		System:    conv.system,
		Messages:  conv.params(),
		Tools:     tools,
		Thinking: anthropic.BetaThinkingConfigParamUnion{OfAdaptive: &anthropic.BetaThinkingConfigAdaptiveParam{
			Display:      display,
			BlockBinding: anthropic.BetaThinkingBlockBindingParam{PrefixMismatchBehavior: anthropic.BetaThinkingPrefixMismatchBehaviorDropBlock},
		}},
		Betas: []anthropic.AnthropicBeta{anthropic.AnthropicBetaThinkingBindingControls2026_08_01},
	}
	if effort != "" {
		params.OutputConfig.Effort = anthropic.BetaOutputConfigEffort(effort)
	}
	if len(tools) > 0 && req.ToolChoice == "none" {
		params.ToolChoice = anthropic.BetaToolChoiceUnionParam{OfNone: &anthropic.BetaToolChoiceNoneParam{}}
	}
	return builtRequest{params: params, effort: effort, replayed: conv.replayed}, nil
}

// effort maps Aura's effort onto output_config.effort. This route never turns thinking
// off, so none and minimal send the model's lowest effort, and an effort the model does
// not list is clamped to the nearest one it does.
func (c *Client) effort(want llm.ReasoningEffort) (string, error) {
	switch want {
	case "":
		return "", nil
	case llm.ReasoningEffortNone, llm.ReasoningEffortMinimal:
		want = llm.ReasoningEffortLow
		for _, effort := range efforts {
			if slices.Contains(c.cfg.SupportedReasoningEfforts, effort) {
				want = effort
				break
			}
		}
	}
	want = c.cfg.ClampReasoningEffort(want)
	if !slices.Contains(efforts, want) {
		return "", fmt.Errorf("anthropic: unsupported reasoning effort %q", want)
	}
	return string(want), nil
}

func (c *Client) maxTokens(requested int) int {
	tokens := max(requested, c.cfg.MaxTokens, minMaxTokens)
	if c.cfg.MaxOutputTokens > 0 {
		tokens = min(tokens, c.cfg.MaxOutputTokens)
	}
	return tokens
}
