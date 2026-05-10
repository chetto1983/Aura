package telegram

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/tools"

	tele "gopkg.in/telebot.v4"
)

// executeToolCalls runs an assistant turn's tool calls concurrently and
// appends results in original order. The LLM batches independent calls into
// one assistant turn (e.g. search_files + web_search side-by-side); running
// them sequentially serialized N round-trips of latency for no reason.
//
// Concurrency safety: Registry.Execute is RWMutex-guarded for lookup, and
// individual tools run outside the lock. Wiki/source writes serialize on
// SQLite at the storage layer. Tool activity stays in logs; Telegram receives
// only the final user-facing synthesis so normal users are not exposed to
// tool names, JSON payloads, or raw execution traces.
//
// Tool routing is permissive: any tool present in the registry executes. The
// runtime no longer rejects tools as "not available in this turn" — that
// pattern produced 60-90% of observed loop iterations because the LLM would
// re-attempt with slightly different arguments. The model decides what to
// call; the registry decides whether the call is real.
//
// Returns the last result content (in original order), used by the caller
// as a fallback when the model returns an empty final response.
type toolExecutionSummary struct {
	lastResult      string
	fatalResult     string
	readSkillNames  []string
	terminalTool    string
	discoveredTools []string
}

func (b *Bot) executeToolCalls(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, calls []llm.ToolCall, toolsExposed []string, readSkills []string) toolExecutionSummary {
	if len(calls) == 0 {
		return toolExecutionSummary{}
	}

	type outcome struct {
		id            string
		tool          string
		content       string
		elapsed       time.Duration
		errorClass    string
		fatal         bool
		readSkillName string
		terminalTool  string
	}
	results := make([]outcome, len(calls))

	var wg sync.WaitGroup
	summary := toolExecutionSummary{}
	for i, tc := range calls {
		wg.Add(1)
		go func(i int, tc llm.ToolCall) {
			defer wg.Done()
			start := time.Now()
			toolCtx := tools.WithAllowedToolNames(tools.WithUserID(ctx, userID), b.tools.Names())
			args := toolArgumentsForTool(tc.Name, tc.Arguments, chatIDFromTeleContext(c))
			result, err := b.tools.Execute(toolCtx, tc.Name, args)
			if err != nil {
				result = tools.FormatToolError(err)
				b.logger.Warn("tool call failed", "user_id", userID, "tool", tc.Name, "error", err)
			}
			errorClass := ""
			if err != nil {
				errorClass = "tool_error"
			}
			readSkillName := ""
			if err == nil {
				switch tc.Name {
				case "read_file":
					readSkillName = skillNameFromReadFileArgs(tc.Arguments)
				}
			}
			terminalTool := ""
			if err == nil && b.terminalToolPolicyEnabled() && isTerminalTool(tc.Name) {
				terminalTool = tc.Name
			}
			results[i] = outcome{
				id:            tc.ID,
				tool:          tc.Name,
				content:       result,
				elapsed:       time.Since(start),
				errorClass:    errorClass,
				readSkillName: readSkillName,
				terminalTool:  terminalTool,
			}
		}(i, tc)
	}
	wg.Wait()

	for _, r := range results {
		convCtx.AddToolResultMessage(r.id, r.content)
		summary.lastResult = r.content
		if r.fatal && summary.fatalResult == "" {
			summary.fatalResult = r.content
		}
		if r.readSkillName != "" {
			summary.readSkillNames = appendUniqueStrings(summary.readSkillNames, r.readSkillName)
		}
		if summary.terminalTool == "" && r.terminalTool != "" {
			summary.terminalTool = r.terminalTool
		}
		if r.tool == "tool_search" {
			summary.discoveredTools = appendUniqueStrings(summary.discoveredTools, toolNamesFromToolSearchResult(r.content)...)
		}
	}
	return summary
}

func toolNamesFromToolSearchResult(content string) []string {
	var payload struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil
	}
	names := make([]string, 0, len(payload.Tools))
	for _, tool := range payload.Tools {
		if tool.Name != "" {
			names = append(names, tool.Name)
		}
	}
	return names
}

func toolArgumentsForTool(name string, args map[string]any, chatID int64) map[string]any {
	if name != "search_memory" {
		return args
	}
	out := make(map[string]any, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	if chatID > 0 {
		out["chat_id"] = float64(chatID)
	}
	return out
}

func isTerminalTool(name string) bool {
	return name == "execute_code" || name == "execute_shell" || isFileGenerationTool(name)
}

func chatIDFromTeleContext(c tele.Context) int64 {
	if c == nil || c.Chat() == nil {
		return 0
	}
	return c.Chat().ID
}
