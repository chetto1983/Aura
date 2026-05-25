// loop.go runs one assistant turn: alternating LLM calls and tool execution
// until the model produces a final answer or the per-turn budget is exhausted.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	applyBudgetDefaults(&opts)
	start := time.Now()
	var lastToolResult string
	var stats Stats
	// lengthRecoveries counts finish_reason='length' text recoveries this turn.
	// Capped at maxLengthRecoveries (US-OUT-05).
	lengthRecoveries := 0
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
		// OR-of-four guard (US-OUT-08): iter at loop boundary; token/wall-clock/cost below.
		if checkTokenBudget(&stats, opts, logger, iteration) {
			if iterCancel != nil {
				iterCancel()
				iterCancel = nil
			}
			return gracefulFinalize(ctx, client, state, opts, &stats, lastToolResult, emitStats)
		}
		// Signal 3: wall-clock.
		remaining := opts.MaxElapsed - time.Since(start)
		if remaining <= 0 {
			stats.MaxElapsedHit = true
			stats.StopReason = governance.StopReasonWallClock
			logger.Warn("agent: wall_clock_exceeded", "iteration", iteration, "elapsed_ms", time.Since(start).Milliseconds(), "max_elapsed_ms", opts.MaxElapsed.Milliseconds())
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
		if checkCostBudget(&stats, opts, logger, iteration) {
			if iterCancel != nil {
				iterCancel()
				iterCancel = nil
			}
			return gracefulFinalize(ctx, client, state, opts, &stats, lastToolResult, emitStats)
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
		// UserMessageInjector to add the cap-hit nudge the same way AllDuplicate
		// and MaxToolCalls finalize do.
		if !finalizing && opts.MaxIterations > 1 && iteration == opts.MaxIterations-1 {
			finalizing = true
			if injector, ok := state.(UserMessageInjector); ok {
				injector.AddUserMessage(fmt.Sprintf(
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
		// US-OUT-06: scrub orphan/missing tool results before each LLM call to
		// prevent provider 400 errors on malformed history (SQLite WAL recovery,
		// ask_user interruption, etc.). governance.Apply chains the remaining
		// transforms (microcompact, truncate) on the already-clean slice.
		// US-CTX-01: apply ContextEngine compression before governance pass so
		// the LLM sees a trimmed history without mutating the State (state.Messages()
		// always returns the full slice; compression is per-iteration and transient).
		currentMsgs := state.Messages()
		if opts.ContextEngine != nil && opts.ContextEngine.ShouldCompress(len(currentMsgs)) {
			currentMsgs = opts.ContextEngine.Compress(currentMsgs, len(currentMsgs), lastUserMessageText(currentMsgs))
		}
		rawHistory := governance.ScrubOrphanToolCalls(currentMsgs)
		messagesForModel := governance.Apply(rawHistory, opts.MaxToolResultChars, opts.MicrocompactKeepRecent, opts.MicrocompactMinChars)
		if opts.Briefer != nil && len(toolDefs) > 0 {
			availableSet := make(map[string]struct{}, len(toolDefs))
			toolNames := make([]string, 0, len(toolDefs))
			for _, t := range toolDefs {
				availableSet[t.Name] = struct{}{}
				toolNames = append(toolNames, t.Name)
			}
			if capsule := opts.Briefer.Brief(iterCtx, opts.BrieferRunID, toolNames, availableSet); capsule != "" {
				messagesForModel = conversation.InjectSystemExtras(messagesForModel, capsule)
			}
		}
		// US-OUT-04: inject "## Already done this turn" block when at least one
		// tool call has been made this turn. Placed after the briefer capsule and
		// before the step hint so the model has an at-a-glance ledger of its own
		// actions and does not repeat queries it already tried.
		if block := conversation.RenderAlreadyDoneBlock(stats.TurnActions); block != "" {
			messagesForModel = conversation.InjectSystemExtras(messagesForModel, block)
		}
		// Step counter (US-LAT-01): inject per-iteration pacing hint into the
		// single system message so there is exactly ONE role=system at [0].
		if hint := conversation.RenderStepHint(iteration+1, opts.MaxIterations); hint != "" {
			messagesForModel = conversation.InjectSystemExtras(messagesForModel, hint)
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
		// US-CTX-01: record token usage in the engine for threshold calculations.
		if opts.ContextEngine != nil {
			opts.ContextEngine.UpdateFromResponse(resp.Response.Usage)
		}
		if opts.EstimateCost != nil {
			stats.CostUSD += opts.EstimateCost(resp.Response.Usage)
		}
		if opts.RecordUsage != nil {
			opts.RecordUsage(resp.Response.Usage)
		}

		// US-OUT-05: finish_reason='length' safety rails. Must run before the
		// HasToolCalls branch so truncated tool JSON is never dispatched.
		if resp.Response.FinishReason == "length" {
			if resp.Response.HasToolCalls {
				// Truncated mid-JSON — tool-call arguments are likely malformed.
				// Drop the calls; keep any partial text in history so the model
				// has context for the next iteration.
				logger.Warn("agent: finish_reason_length_tool_calls_dropped",
					"iteration", iteration,
					"tool_calls_dropped", len(resp.Response.ToolCalls),
					"reason", "truncated_json_unsafe_to_execute",
				)
				if partial := strings.TrimSpace(resp.Response.Content); partial != "" {
					state.AddAssistantMessage(partial)
				}
				emitStats()
				continue
			}
			// Text-only truncation. Inject recovery prompt if under the cap.
			if partial := strings.TrimSpace(resp.Response.Content); partial != "" {
				state.AddAssistantMessage(partial)
				emitStats()
				if lengthRecoveries < maxLengthRecoveries {
					lengthRecoveries++
					if injector, ok := state.(UserMessageInjector); ok {
						injector.AddUserMessage(lengthRecoveryPrompt)
					} else {
						logger.Warn("agent: finish_reason_length_no_corrector",
							"iteration", iteration,
							"reason", "state_lacks_AddUserMessage",
						)
					}
					continue
				}
				// Cap exceeded — return the partial text rather than an error.
				iterCancel()
				return loopResult{Text: partial, Stats: stats}, nil
			}
		}

		if !resp.Response.HasToolCalls {
			response := strings.TrimSpace(resp.Response.Content)
			if response == "" {
				// lastToolResult fallback (US-CACHE-04): when the LLM emits empty
				// text and no tool calls after a tool has already been executed,
				// it is treating the tool result as the final answer. Surface it
				// directly rather than falling back to a generic budget message.
				if lastToolResult != "" {
					state.AddAssistantMessage(lastToolResult)
					emitStats()
					iterCancel()
					return loopResult{Text: lastToolResult, Stats: stats}, nil
				}
				stats.StopReason = governance.StopReasonEmptyResponse
				iterCancel()
				return gracefulFinalize(ctx, client, state, opts, &stats, lastToolResult, emitStats)
			}
			if text, ok := extractTextResponsePseudoCall(response); ok {
				state.AddAssistantMessage(text)
				emitStats()
				iterCancel()
				if resp.Delivered {
					return loopResult{Text: text, Delivered: true, Stats: stats}, nil
				}
				return loopResult{Text: text, Stats: stats}, nil
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
			case "execute_code":
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
			// Append fresh call outcomes to TurnActions ledger (US-OUT-04).
			for _, call := range freshCalls {
				if result, ok := execution.Results[call.ID]; ok {
					stats.TurnActions = append(stats.TurnActions, conversation.TaskEntry{
						ToolName:     call.Name,
						ArgsSummary:  argsSummaryFor(call.Name, call.Arguments),
						Status:       toolCallStatus(result),
						BriefOutcome: toolCallBriefOutcome(result),
					})
				}
			}
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
			if injector, ok := state.(UserMessageInjector); ok {
				injector.AddUserMessage("Stop. The last two LLM rounds produced only duplicate tool calls — you are in a retry loop. Do NOT call any tool again. Finalize NOW with a concise answer based on the evidence already in this turn. If the data the user asked for is genuinely not present in what you have, say so explicitly instead of trying again.")
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
			if injector, ok := state.(UserMessageInjector); ok {
				injector.AddUserMessage(toolBudgetFinalInstruction(opts.MaxToolCalls))
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
	stats.StopReason = governance.StopReasonMaxIterations
	logger.Warn("agent: max_iterations_hit", "iterations", opts.MaxIterations, "elapsed_ms", time.Since(start).Milliseconds(), "tools_called", len(stats.ToolsCalled))
	if iterCancel != nil {
		iterCancel()
		iterCancel = nil
	}
	return gracefulFinalize(ctx, client, state, opts, &stats, lastToolResult, emitStats)
}
