package orchestration

import (
	"fmt"

	"github.com/aura/aura/internal/agentloop"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/tools"
)

func BeforeToolCallbackForToolset(toolset Toolset) agentloop.BeforeToolCallback {
	switch normalizeToolset(string(toolset)) {
	case ToolsetDefault:
		return agentloop.DuplicateOrMaxCallsPolicy(map[string]int{"search_memory": 1}, func(call llm.ToolCall, state agentloop.ToolCallState) string {
			if call.Name == "search_memory" {
				return tools.FormatToolError(fmt.Errorf("default route already has compact memory evidence for this turn; answer now from the previous evidence without calling tools again"))
			}
			return tools.FormatToolError(fmt.Errorf("duplicate tool call %q skipped; use the previous result already returned in this turn", call.Name))
		})
	case ToolsetDocument:
		return agentloop.DuplicateOrMaxCallsPolicy(map[string]int{"search_memory": 1}, func(call llm.ToolCall, state agentloop.ToolCallState) string {
			if call.Name == "search_memory" {
				return tools.FormatToolError(fmt.Errorf("document route already has compact retrieval for this turn; use the previous evidence and call create_docx/create_xlsx/create_pdf now"))
			}
			return tools.FormatToolError(fmt.Errorf("duplicate tool call %q skipped; use the previous result already returned in this turn", call.Name))
		})
	default:
		return agentloop.DuplicateOrMaxCallsPolicy(nil, func(call llm.ToolCall, state agentloop.ToolCallState) string {
			return tools.FormatToolError(fmt.Errorf("duplicate tool call %q skipped; use the previous result already returned in this turn", call.Name))
		})
	}
}

func AfterToolCallbackForToolset(toolset Toolset) agentloop.AfterToolCallback {
	switch normalizeToolset(string(toolset)) {
	case ToolsetDefault:
		return func(call llm.ToolCall, result string, err error) string {
			if err != nil || call.Name != "search_memory" {
				return result
			}
			return result + "\n\nNext step for this default turn: answer now from this compact evidence. Do not call search_memory again. Do not call workspace, source, web, document, or admin tools unless they are explicitly exposed and required."
		}
	case ToolsetDocument:
		return func(call llm.ToolCall, result string, err error) string {
			if err != nil || call.Name != "search_memory" {
				return result
			}
			return result + "\n\nNext step for this document turn: use this evidence now and call exactly one of create_docx, create_xlsx, or create_pdf. Do not call search_memory again. Do not call read_file, search_files, web_search, or source-reading tools unless the user explicitly asks for deeper inspection."
		}
	default:
		return nil
	}
}
