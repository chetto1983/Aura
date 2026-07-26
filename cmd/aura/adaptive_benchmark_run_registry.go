package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
)

var adaptiveBenchmarkRestrictedSkillParameters = json.RawMessage(`{
  "type": "object",
  "properties": {
    "action": {
      "type": "string",
      "enum": ["list"],
      "description": "The only permitted benchmark skill action."
    },
    "query": {
      "type": "string",
      "minLength": 1,
      "description": "A required nonblank query used to rank installed skills."
    }
  },
  "required": ["action", "query"],
  "additionalProperties": false
}`)

type adaptiveBenchmarkRestrictedSkill struct {
	delegate *tools.SkillTool
}

func (skill adaptiveBenchmarkRestrictedSkill) Spec() tools.Spec {
	return tools.Spec{
		Name:        "skill",
		Summary:     "List installed skills relevant to a nonblank query.",
		Description: "Benchmark-local read-only skill catalog search. Only action=list is accepted.",
		Parameters:  adaptiveBenchmarkRestrictedSkillParameters,
		Deferred:    false,
	}
}

func (skill adaptiveBenchmarkRestrictedSkill) Execute(
	ctx context.Context,
	raw json.RawMessage,
) (tools.ToolResult, error) {
	if skill.delegate == nil {
		return tools.ToolResult{}, errors.New(
			"benchmark skill catalog is unavailable",
		)
	}
	var args struct {
		Action string `json:"action"`
		Query  string `json:"query"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return tools.ToolResult{}, fmt.Errorf(
			"benchmark skill args: %w",
			err,
		)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return tools.ToolResult{}, errors.New(
			"benchmark skill args contain trailing content",
		)
	}
	if args.Action != "list" ||
		args.Query == "" ||
		strings.TrimSpace(args.Query) != args.Query {
		return tools.ToolResult{}, errors.New(
			"benchmark skill accepts only action=list with a nonblank canonical query",
		)
	}
	return skill.delegate.Execute(ctx, raw)
}

func restrictAdaptiveBenchmarkRegistry(
	full *tools.Registry,
) (*tools.Registry, error) {
	if full == nil {
		return nil, errors.New("benchmark source registry is unavailable")
	}
	textResponse, ok := full.Get("text_response")
	if !ok {
		return nil, errors.New("benchmark registry lacks text_response")
	}
	if _, ok := textResponse.(tools.TextResponse); !ok {
		return nil, errors.New("benchmark text_response is not production")
	}
	documentSearch, ok := full.Get("document_search")
	if !ok {
		return nil, errors.New("benchmark registry lacks document_search")
	}
	if _, ok := documentSearch.(*tools.DocumentSearch); !ok {
		return nil, errors.New("benchmark document_search is not production")
	}
	skillTool, ok := full.Get("skill")
	if !ok {
		return nil, errors.New("benchmark registry lacks skill")
	}
	productionSkill, ok := skillTool.(*tools.SkillTool)
	if !ok {
		return nil, errors.New("benchmark skill is not production")
	}
	searchTool, ok := full.Get("tool_search")
	if !ok {
		return nil, errors.New("benchmark registry lacks tool_search")
	}
	productionSearch, ok := searchTool.(*tools.ToolSearch)
	if !ok {
		return nil, errors.New("benchmark tool_search is not production")
	}

	restricted := tools.NewRegistry()
	restricted.Register(textResponse)
	restricted.Register(documentSearch)
	if readOutput, exists := full.Get("read_tool_output"); exists {
		if _, ok := readOutput.(*tools.ReadToolOutput); !ok {
			return nil, errors.New(
				"benchmark read_tool_output is not production",
			)
		}
		restricted.Register(readOutput)
	}
	restricted.Register(adaptiveBenchmarkRestrictedSkill{
		delegate: productionSkill,
	})
	restricted.Register(&tools.ToolSearch{
		Registry: restricted,
		Embed:    productionSearch.Embed,
		Adaptive: productionSearch.Adaptive,
	})
	if err := restricted.Validate(); err != nil {
		return nil, fmt.Errorf("validate benchmark registry: %w", err)
	}
	return restricted, nil
}
