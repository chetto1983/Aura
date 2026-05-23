// loop.go runs one assistant turn: alternating LLM calls and tool execution
// until the model produces a final answer or the per-turn budget is exhausted.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	governance "github.com/aura/aura/internal/agent/governance"
	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/stringx"
)

func runLoop(ctx context.Context, client ChatClient, executor ToolExecutor, state State, opts Options) (loopResult, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if opts.MaxIterations < 1 {
		opts.MaxIterations = 1
	}
	if opts.MaxIterations > MaxIterationsCeiling {
		requestedMaxIterations := opts.MaxIterations
		opts.MaxIterations = MaxIterationsCeiling
		logger.Warn(
			"agent: max_iterations_capped",
			"requested_max_iterations", requestedMaxIterations,
			"effective_max_iterations", opts.MaxIterations,
			"max_iterations_ceiling", MaxIterationsCeiling,
			"reason", "runtime_ceiling",
		)
	}
	if opts.MaxElapsed <= 0 {
		opts.MaxElapsed = DefaultMaxElapsed
	}
	start := time.Now()
	var lastToolResult string
	var stats Stats
	seenToolCalls := map[string]bool{}
	seenToolCallsResult := map[string]string{}
	toolCallExecutions := map[string]int{}
	globalToolCallsExecuted := 0
	finalizing := false
	// consecutiveAllDupIter counts iterations in a row where every call the
	// model produced was a duplicate of an earlier-in-turn call. After two
	// such iterations the loop force-finalizes — the model is stuck in a
	// tight retry loop (live 2026-05-21: xlsx voice-memo test saw 22 LLM
	// rounds in 105s, all reading the same wiki page over and over).
	consecutiveAllDupIter := 0
	calledThisTurn := map[string]bool{}
	emitStats := func() {
		if opts.OnStats != nil {
			opts.OnStats(stats)
		}
	}
	emitStats()
	logger.Debug("agent: run start", "max_iterations", opts.MaxIterations, "max_elapsed_ms", opts.MaxElapsed.Milliseconds())

	var pool *toolPool

	var iterCancel context.CancelFunc
	defer func() {
		if iterCancel != nil {
			iterCancel()
		}
	}()

	for iteration := 0; iteration < opts.MaxIterations; iteration++ {
		remaining := opts.MaxElapsed - time.Since(start)
		if remaining <= 0 {
			stats.MaxElapsedHit = true
			stats.StopReason = "max_elapsed_hit"
			logger.Warn("agent: max_elapsed_hit", "iteration", iteration, "elapsed_ms", time.Since(start).Milliseconds(), "max_elapsed_ms", opts.MaxElapsed.Milliseconds())
			if iterCancel != nil {
				iterCancel()
				iterCancel = nil
			}
			return gracefulFinalize(ctx, client, state, opts, &stats, lastToolResult, emitStats)
		}
		if iterCancel != nil {
			iterCancel()
		}
		var iterCtx context.Context
		iterCtx, iterCancel = context.WithTimeout(ctx, remaining)
		if finalizing && opts.CompleteOnDeadline && opts.FinalizationTimeout > 0 {
			iterCancel()
			iterCtx, iterCancel = context.WithTimeout(ctx, opts.FinalizationTimeout)
		}
		if opts.BeforeLLM != nil {
			if message, stop := opts.BeforeLLM(); stop {
				stats.StopReason = "before_llm"
				logger.Info("agent: before_llm_stop", "iteration", iteration)
				iterCancel()
				return loopResult{Text: message, Stats: stats}, nil
			}
		}

		// Cap-hit finalize (US-LAT-01): on the final iteration, set finalizing
		// mode so toolDefs becomes nil — the model cannot call tools. Reuses
		// PhantomCorrector to inject the cap-hit nudge the same way AllDuplicate
		// and MaxToolCalls finalize do.
		if !finalizing && opts.MaxIterations > 1 && iteration == opts.MaxIterations-1 {
			finalizing = true
			if corrector, ok := state.(PhantomCorrector); ok {
				corrector.AddUserMessage(fmt.Sprintf(
					"Passo %d/%d: hai raggiunto il limite di iterazioni (AURA_AGENT_LOOP_MAX_STEPS=%d). "+
						"NON chiamare altri tool. Rispondi ora con quello che hai. "+
						"Per workflow genuinamente multi-step, imposta AURA_AGENT_LOOP_MAX_STEPS=N via env.",
					opts.MaxIterations, opts.MaxIterations, opts.MaxIterations))
			} else {
				logger.Warn("agent: max_iterations_finalize_no_corrector",
					"iteration", iteration,
					"reason", "state_lacks_AddUserMessage",
				)
			}
		}

		if pool == nil {
			seed := opts.Tools
			if opts.ToolsProvider != nil {
				seed = opts.ToolsProvider()
			}
			pool = newToolPool(seed, opts.ToolResolver)
		}
		toolDefs := pool.Defs()
		if finalizing {
			toolDefs = nil
		}

		stats.LLMCalls++
		stats.LoopSteps++
		messagesForModel := governance.Apply(state.Messages(), opts.MaxToolResultChars, opts.MicrocompactKeepRecent, opts.MicrocompactMinChars)
		if opts.Briefer != nil && len(toolDefs) > 0 {
			availableSet := make(map[string]struct{}, len(toolDefs))
			toolNames := make([]string, 0, len(toolDefs))
			for _, t := range toolDefs {
				availableSet[t.Name] = struct{}{}
				toolNames = append(toolNames, t.Name)
			}
			if capsule := opts.Briefer.Brief(iterCtx, opts.BrieferRunID, toolNames, availableSet); capsule != "" {
				messagesForModel = append([]llm.Message{{Role: "system", Content: capsule}}, messagesForModel...)
			}
		}
		// Step counter (US-LAT-01): inject per-iteration pacing hint so the
		// model sees its progress and self-terminates early on simple queries.
		if hint := conversation.RenderStepHint(iteration+1, opts.MaxIterations); hint != "" {
			messagesForModel = append([]llm.Message{{Role: "system", Content: hint}}, messagesForModel...)
		}
		if opts.OnLLMStart != nil {
			opts.OnLLMStart(iteration, len(messagesForModel), len(toolDefs))
		}
		resp, err := client.Chat(iterCtx, messagesForModel, toolDefs)
		if err != nil {
			iterCancel()
			if opts.CompleteOnDeadline && errors.Is(err, context.DeadlineExceeded) && globalToolCallsExecuted > 0 {
				content := interruptedAssistantContent(err, lastToolResult)
				state.AddAssistantMessage(content)
				emitStats()
				logger.Warn("agent: complete_on_deadline",
					"iteration", iteration,
					"finalizing", finalizing,
					"tool_calls_executed", globalToolCallsExecuted,
				)
				return loopResult{Text: content, Stats: stats}, nil
			}
			return loopResult{Text: llm.UserMessageFor(err), Stats: stats}, err
		}
		if opts.OnLLMDelta != nil && resp.Response.Content != "" {
			opts.OnLLMDelta(resp.Response.Content)
		}

		state.TrackTokens(resp.Response.Usage)
		stats.TokensPrompt += resp.Response.Usage.PromptTokens
		stats.TokensCompletion += resp.Response.Usage.CompletionTokens
		stats.TokensTotal += resp.Response.Usage.TotalTokens
		stats.CacheReadTokens += resp.Response.Usage.CacheReadTokens
		if opts.EstimateCost != nil {
			stats.CostUSD += opts.EstimateCost(resp.Response.Usage)
		}
		if opts.RecordUsage != nil {
			opts.RecordUsage(resp.Response.Usage)
		}

		if !resp.Response.HasToolCalls {
			response := strings.TrimSpace(resp.Response.Content)
			if response == "" {
				stats.StopReason = "empty_llm_response"
				iterCancel()
				return gracefulFinalize(ctx, client, state, opts, &stats, lastToolResult, emitStats)
			}
			if opts.PhantomToolGuard != nil &&
				stats.PhantomToolDetections < opts.PhantomToolGuard.RetriesAllowed() &&
				opts.PhantomToolGuard.LooksPhantom(response, false, calledThisTurn) {
				if corrector, ok := state.(PhantomCorrector); ok {
					stats.PhantomToolDetections++
					logger.Warn("agent: phantom_tool_detected",
						"iteration", iteration,
						"detections_so_far", stats.PhantomToolDetections,
						"retries_allowed", opts.PhantomToolGuard.RetriesAllowed(),
					)
					state.AddAssistantMessage(response)
					corrector.AddUserMessage(opts.PhantomToolGuard.CorrectionText())
					emitStats()
					continue
				}
				logger.Warn("agent: phantom_tool_detected_uncorrectable",
					"iteration", iteration,
					"reason", "state_lacks_AddUserMessage",
				)
			}
			state.AddAssistantMessage(response)
			emitStats()
			// Server-driven end_turn: explicit false means the provider
			// wants another sampling round even without a tool call.
			// nil (absent field) and true both use existing exit semantics.
			// MaxIterations remains the emergency brake in all cases.
			if resp.Response.EndTurn != nil && !*resp.Response.EndTurn {
				continue
			}
			iterCancel()
			if resp.Delivered {
				return loopResult{Text: response, Delivered: true, Stats: stats}, nil
			}
			return loopResult{Text: response, Stats: stats}, nil
		}

		if opts.PhantomToolGuard != nil &&
			stats.PhantomToolDetections > 0 &&
			stats.PhantomToolCorrected < stats.PhantomToolDetections {
			stats.PhantomToolCorrected++
			logger.Info("agent: phantom_tool_corrected",
				"iteration", iteration,
				"corrected_total", stats.PhantomToolCorrected,
			)
		}
		// ask_user exclusive semantics: when ask_user is in the batch, keep
		// only that call in the state so the resume sees a clean single
		// pending tool_call without orphaned unresolved stubs for other calls.
		toolCallsForState := resp.Response.ToolCalls
		if askUserIdx := findAskUserCall(resp.Response.ToolCalls); askUserIdx >= 0 {
			toolCallsForState = resp.Response.ToolCalls[askUserIdx : askUserIdx+1]
		}
		state.AddAssistantToolCallMessage(resp.Response.Content, toolCallsForState)
		stats.ToolCalls += len(resp.Response.ToolCalls)
		for _, call := range resp.Response.ToolCalls {
			stats.ToolsCalled = append(stats.ToolsCalled, call.Name)
			calledThisTurn[call.Name] = true
			switch call.Name {
			case "file":
				if action, _ := call.Arguments["action"].(string); action == "read" {
					if skill := SkillNameFromReadFileArgs(call.Arguments); skill != "" {
						stats.ReadSkills = stringx.AppendUnique(stats.ReadSkills, skill)
						stats.SkillsRead = true
					}
				}
			case "run_aurabot_swarm":
				stats.SwarmUsed = true
			case "execute_code", "execute_shell":
				stats.SandboxUsed = true
			}
		}
		emitStats()

		if pool != nil && pool.resolver != nil {
			for _, call := range resp.Response.ToolCalls {
				pool.EnsureLoaded(call.Name)
			}
		}

		var callsToExecute, duplicateToolCalls []llm.ToolCall
		if opts.DisableInBatchDedup {
			callsToExecute = resp.Response.ToolCalls
		} else {
			callsToExecute, duplicateToolCalls = DedupeToolCalls(resp.Response.ToolCalls)
		}
		inBatchDuplicate := map[string]bool{}
		for _, call := range duplicateToolCalls {
			inBatchDuplicate[duplicateToolCallKey(call)] = true
		}
		var freshCalls []llm.ToolCall
		skippedToolResults := map[string]string{}
		maxCallsHit := map[string]bool{}
		budgetCapHit := map[string]bool{}
		for _, call := range callsToExecute {
			key := duplicateToolCallKey(call)
			stateForCall := ToolCallState{
				InBatchDuplicate: inBatchDuplicate[key],
				PriorIdentical:   seenToolCalls[key],
				CallsForTool:     toolCallExecutions[call.Name],
			}
			if opts.BeforeTool != nil {
				if decision := opts.BeforeTool(call, stateForCall); decision.Skip {
					if decision.Result != "" {
						skippedToolResults[call.ID] = decision.Result
					}
					seenToolCalls[key] = true
					toolCallExecutions[call.Name]++
					duplicateToolCalls = append(duplicateToolCalls, call)
					continue
				}
			} else if seenToolCalls[key] && !IsRetryableToolResult(seenToolCallsResult[key]) {
				duplicateToolCalls = append(duplicateToolCalls, call)
				continue
			} else if maxCalls := opts.MaxCallsPerTool[call.Name]; maxCalls > 0 && toolCallExecutions[call.Name] >= maxCalls {
				duplicateToolCalls = append(duplicateToolCalls, call)
				maxCallsHit[call.ID] = true
				continue
			}
			if opts.MaxToolCalls > 0 && globalToolCallsExecuted >= opts.MaxToolCalls {
				duplicateToolCalls = append(duplicateToolCalls, call)
				budgetCapHit[call.ID] = true
				continue
			}
			// US-J05: per-(tool,class) retry budget check. Fires before each
			// fresh dispatch so repeated failures of the same class are capped.
			if opts.RetryBudgetRepo != nil && len(opts.RetryBudgets) > 0 && opts.BrieferRunID != "" {
				budgetRefused := false
				for outcome, budget := range opts.RetryBudgets {
					if allowed, reason := attempts.CheckBudget(iterCtx, opts.RetryBudgetRepo, opts.BrieferRunID, call.Name, outcome, budget); !allowed {
						logger.Debug("agent: retry_budget_exhausted",
							"iteration", iteration,
							"tool", call.Name,
							"outcome", outcome.String(),
							"reason", reason,
						)
						skippedToolResults[call.ID] = "Error: " + reason
						// Persist a refusal row so briefer and /api/tool-warnings can observe it.
						_ = opts.RetryBudgetRepo.Record(iterCtx, tools.ToolObservation{
							RunID:     opts.BrieferRunID,
							ToolName:  call.Name,
							ToolKind:  tools.ToolKindOf(call.Name),
							Outcome:   tools.OutcomeBlocked,
							Reason:    reason,
							StartedAt: time.Now(),
						})
						seenToolCalls[key] = true
						toolCallExecutions[call.Name]++
						duplicateToolCalls = append(duplicateToolCalls, call)
						budgetRefused = true
						break
					}
				}
				if budgetRefused {
					continue
				}
			}
			seenToolCalls[key] = true
			toolCallExecutions[call.Name]++
			globalToolCallsExecuted++
			freshCalls = append(freshCalls, call)
		}
		stats.DuplicateToolCall = stats.DuplicateToolCall || len(duplicateToolCalls) > 0

		// ask_user exclusive semantics: if ask_user is among the fresh calls,
		// dispatch only it and discard the rest of the batch silently (they
		// will re-emit on the next LLM turn after resume).
		if idx := findAskUserCall(freshCalls); idx >= 0 {
			freshCalls = freshCalls[idx : idx+1]
		}

		var execution ExecutionSummary
		if len(freshCalls) > 0 {
			toolNames := make([]string, 0, len(freshCalls))
			for _, call := range freshCalls {
				toolNames = append(toolNames, call.Name)
			}
			logger.Debug("agent: dispatch_tools",
				"iteration", iteration,
				"tools", toolNames,
				"duplicates", len(duplicateToolCalls),
			)
			if opts.OnToolStart != nil {
				for _, call := range freshCalls {
					opts.OnToolStart(call, argKeysFromCall(call))
				}
			}
			toolBatchStart := time.Now()
			disp := newStreamDispatcher(executor, opts.ParallelTools)
			for _, call := range freshCalls {
				disp.Submit(iterCtx, call)
			}
			execution = disp.Wait(iterCtx)
			stats.ToolCallsExecuted += len(freshCalls)
			toolBatchElapsed := time.Since(toolBatchStart)
			if opts.OnToolEnd != nil {
				for _, call := range freshCalls {
					result := execution.Results[call.ID]
					success := !strings.HasPrefix(strings.TrimSpace(result), "Error:")
					opts.OnToolEnd(call.ID, call.Name, success, toolBatchElapsed, toolResultPreview(result))
				}
			}
			lastToolResult = execution.LastResult
			stats.ReadSkills = stringx.AppendUnique(stats.ReadSkills, execution.ReadSkillNames...)
			stats.SkillsRead = stats.SkillsRead || len(stats.ReadSkills) > 0
			if execution.TerminalTool != "" {
				stats.TerminalTool = execution.TerminalTool
			}
			for _, call := range freshCalls {
				key := duplicateToolCallKey(call)
				if result, ok := execution.Results[call.ID]; ok {
					seenToolCallsResult[key] = result
				}
			}
		}
		for _, duplicate := range duplicateToolCalls {
			if result := skippedToolResults[duplicate.ID]; result != "" {
				state.AddToolResultMessage(duplicate.ID, result)
				continue
			}
			stub := duplicateToolResult(duplicate, opts)
			if maxCallsHit[duplicate.ID] {
				stub = maxCallsToolResult(duplicate)
			}
			if budgetCapHit[duplicate.ID] {
				stub = budgetCapToolResult(opts.MaxToolCalls)
			}
			state.AddToolResultMessage(duplicate.ID, WrapUntrustedToolResult(duplicate.Name, stub))
		}
		// All-duplicate-iteration guard: when the model produced zero fresh
		// calls AND at least one duplicate, it's spinning. One such iter is
		// recoverable noise (warning bounce); two in a row is a stuck loop
		// and force-finalizes with a hard nudge to the corrector. Without
		// this, the model can burn full MaxIterations re-issuing the same
		// dead call (observed 2026-05-21, xlsx test: 22 LLM rounds, 24 tool
		// calls in 105s, all duplicates of one file/read, reply empty).
		allDuplicates := len(freshCalls) == 0 && len(duplicateToolCalls) > 0
		if allDuplicates {
			consecutiveAllDupIter++
		} else {
			consecutiveAllDupIter = 0
		}
		if !finalizing && consecutiveAllDupIter >= 2 {
			finalizing = true
			if corrector, ok := state.(PhantomCorrector); ok {
				corrector.AddUserMessage("Stop. The last two LLM rounds produced only duplicate tool calls — you are in a retry loop. Do NOT call any tool again. Finalize NOW with a concise answer based on the evidence already in this turn. If the data the user asked for is genuinely not present in what you have, say so explicitly instead of trying again.")
			} else {
				logger.Warn("agent: all_dup_finalize_no_corrector",
					"iteration", iteration,
					"consecutive_all_dup", consecutiveAllDupIter,
					"reason", "state_lacks_AddUserMessage",
				)
			}
		}
		if !finalizing && opts.MaxToolCalls > 0 && globalToolCallsExecuted >= opts.MaxToolCalls {
			finalizing = true
			if corrector, ok := state.(PhantomCorrector); ok {
				corrector.AddUserMessage(toolBudgetFinalInstruction(opts.MaxToolCalls))
			} else {
				logger.Warn("agent: tool_budget_finalize_no_corrector",
					"iteration", iteration,
					"max_tool_calls", opts.MaxToolCalls,
					"reason", "state_lacks_AddUserMessage",
				)
			}
		}
		emitStats()

		// ask_user pause: stop the loop and signal the caller to wait for user.
		if execution.AwaitingUserInput != nil {
			stats.StopReason = "waiting_for_user"
			if opts.OnQuestionRequested != nil {
				opts.OnQuestionRequested(execution.AwaitingUserInput)
			}
			emitStats()
			logger.Info("agent: ask_user_pause",
				"iteration", iteration,
				"kind", execution.AwaitingUserInput.Kind,
				"options_count", len(execution.AwaitingUserInput.Options),
			)
			iterCancel()
			return loopResult{Stats: stats}, nil
		}

		if execution.FatalResult != "" {
			state.AddAssistantMessage(execution.FatalResult)
			iterCancel()
			return loopResult{Text: execution.FatalResult, Stats: stats}, nil
		}
		if opts.TerminalToolPolicy && execution.TerminalTool != "" && opts.AllowNoToolFinalization && opts.TerminalHandler != nil {
			logger.Debug("agent: terminal_handler", "iteration", iteration, "tool", execution.TerminalTool)
			response, delivered, handled := opts.TerminalHandler(iterCtx, execution.TerminalTool, lastToolResult, &stats)
			emitStats()
			if handled {
				iterCancel()
				return loopResult{Text: response, Delivered: delivered, Stats: stats}, nil
			}
		}
	}

	stats.MaxIterationsHit = true
	stats.StopReason = "max_iterations_hit"
	logger.Warn("agent: max_iterations_hit", "iterations", opts.MaxIterations, "elapsed_ms", time.Since(start).Milliseconds(), "tools_called", len(stats.ToolsCalled))
	if iterCancel != nil {
		iterCancel()
		iterCancel = nil
	}
	return gracefulFinalize(ctx, client, state, opts, &stats, lastToolResult, emitStats)
}

// gracefulFinalize is the unified budget-exit handler used by all three
// budget paths (MaxElapsed, empty-LLM-response, MaxIterations). When
// AllowNoToolFinalization is true it attempts one extra LLM round via
// finalizeAnswerAfterBudget; on failure or when the flag is false it falls
// back to finalAnswerOnBudgetWithContext. It always adds the assistant message
// and fires emitStats before returning.
func gracefulFinalize(ctx context.Context, client ChatClient, state State, opts Options, stats *Stats, lastToolResult string, emitStats func()) (loopResult, error) {
	lastToolName := ""
	if len(stats.ToolsCalled) > 0 {
		lastToolName = stats.ToolsCalled[len(stats.ToolsCalled)-1]
	}
	var answer string
	if opts.AllowNoToolFinalization {
		if text, ok := finalizeAnswerAfterBudget(ctx, client, state, opts, stats); ok {
			answer = text
		} else {
			answer = finalAnswerOnBudgetWithContext(lastToolResult, lastToolName, stats.StopReason, opts)
		}
	} else {
		answer = finalAnswerOnBudgetWithContext(lastToolResult, lastToolName, stats.StopReason, opts)
	}
	state.AddAssistantMessage(answer)
	emitStats()
	return loopResult{Text: answer, Stats: *stats}, nil
}

func finalizeAnswerAfterBudget(ctx context.Context, client ChatClient, state State, opts Options, stats *Stats) (string, bool) {
	if client == nil || state == nil || stats == nil {
		return "", false
	}
	messages := governance.Apply(state.Messages(), opts.MaxToolResultChars, opts.MicrocompactKeepRecent, opts.MicrocompactMinChars)
	messages = append(messages, llm.Message{
		Role:    "user",
		Content: "You reached the per-turn tool budget. Do not call any more tools. Answer the user naturally using the evidence above. Do not paste raw JSON, tool names, scores, or internal IDs. If the evidence is insufficient, say so plainly in one sentence.",
	})
	stats.LLMCalls++
	stats.LoopSteps++
	resp, err := client.Chat(ctx, messages, nil)
	if err != nil {
		return "", false
	}
	state.TrackTokens(resp.Response.Usage)
	stats.TokensPrompt += resp.Response.Usage.PromptTokens
	stats.TokensCompletion += resp.Response.Usage.CompletionTokens
	stats.TokensTotal += resp.Response.Usage.TotalTokens
	stats.CacheReadTokens += resp.Response.Usage.CacheReadTokens
	if opts.EstimateCost != nil {
		stats.CostUSD += opts.EstimateCost(resp.Response.Usage)
	}
	if opts.RecordUsage != nil {
		opts.RecordUsage(resp.Response.Usage)
	}
	text := strings.TrimSpace(resp.Response.Content)
	if text == "" || resp.Response.HasToolCalls {
		return "", false
	}
	return text, true
}

func SkillNameFromReadFileArgs(args map[string]any) string {
	value, ok := args["path"]
	if !ok {
		return ""
	}
	path := strings.TrimSpace(fmt.Sprint(value))
	if path == "" {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 2 || parts[len(parts)-1] != "SKILL.md" {
		return ""
	}
	name := strings.TrimSpace(parts[len(parts)-2])
	if name == "" || strings.EqualFold(name, "skills") {
		return ""
	}
	return name
}
