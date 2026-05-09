package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/agentloop"
	"github.com/aura/aura/internal/agentruntime"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/orchestration"
	auraskills "github.com/aura/aura/internal/skills"
	auratools "github.com/aura/aura/internal/tools"

	tele "gopkg.in/telebot.v4"
)

func (b *Bot) handleConversation(c tele.Context) {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	turnStart := time.Now()

	session, loaded := b.sessionStore().Begin(userID, conversation.Config{
		MaxTokens:   b.cfg.MaxContextTokens,
		MaxMessages: b.cfg.MaxHistoryMessages,
		Summarizer:  b.llm,
		Logger:      b.logger,
	})
	defer session.Finish()
	convCtx := session.Conversation()
	_ = loaded // kept for clarity; system prompt now refreshes every turn
	userText := c.Text()

	// Build the versioned prompt and focused tool toolset fresh on every
	// turn so runtime time, overlays, skills, swarm, and sandbox state stay
	// accurate without restarting the bot.
	overlay := conversation.LoadPromptOverlay(b.cfg.PromptOverlayPath)
	var skillsBlock string
	// Slice 11q/06: read SOUL.md / AGENT.md / USER.md / TOOLS.md from the
	// configured overlay dir. Picobot pattern: lets the operator tune
	// personality, Aura runtime notes, durable user facts, and tool guidance by
	// file; the next user turn picks up the change with no recompile or
	// restart. AGENTS.md stays development-only and is not injected into Aura's prompt.
	if b.skills != nil {
		loadedSkills, err := b.skills.LoadAll()
		if err != nil {
			b.logger.Warn("failed to load local skills", "error", err)
		} else if block := auraskills.PromptBlock(loadedSkills); block != "" {
			skillsBlock = block
		}
	}
	retrievalCapsule := turnRetrievalCapsule{}
	available := orchestration.Availability{
		Swarm:          b.swarmToolsAvailable(),
		Sandbox:        b.sandboxToolsAvailable(),
		Proposals:      b.proposalToolsAvailable(),
		WorkspaceFiles: b.workspaceToolsAvailable(),
	}
	hooks := orchestration.EnsureHooks(b.orchHooks)
	hooks.BeforeToolsetSelect(orchestration.TraceEvent{Toolset: b.cfg.ToolsetMode})
	toolsetDecision := orchestration.SelectToolsetDecision(userText, b.cfg.ToolsetMode, available)
	hooks.AfterToolsetSelect(orchestration.TraceEvent{
		Toolset:             string(toolsetDecision.Toolset),
		ToolsetSelectReason: toolsetDecision.Reason,
	})
	toolset := toolsetDecision.Toolset
	runtimeToolset := runtimeToolsetForTurn(toolset, retrievalCapsule)
	toolAllowlist, err := runtimeToolset.Tools(orchestration.ToolsetContext{Toolset: toolset, Availability: available})
	if err != nil {
		b.logger.Warn("orchestration toolset failed; falling back to default", "toolset", toolset, "error", err)
		toolset = orchestration.ToolsetDefault
		toolsetDecision = orchestration.ToolsetDecision{Toolset: toolset, Reason: "toolset allowlist failed; fell back to default"}
		runtimeToolset = runtimeToolsetForTurn(toolset, retrievalCapsule)
		toolAllowlist, _ = runtimeToolset.Tools(orchestration.ToolsetContext{Toolset: toolset, Availability: available})
	}
	toolAllowlist = b.appendRegisteredMCPTools(toolAllowlist)
	hooks.BeforePromptCompose(orchestration.TraceEvent{
		Toolset:             string(toolsetDecision.Toolset),
		ToolsetSelectReason: toolsetDecision.Reason,
	})
	promptPlan := orchestration.ComposePrompt(orchestration.PromptInput{
		Version:           b.cfg.PromptVersion,
		Now:               time.Now(),
		Location:          b.loc,
		Overlay:           overlay,
		SkillsBlock:       skillsBlock,
		SwarmAvailable:    available.Swarm,
		SandboxAvailable:  available.Sandbox,
		ProposalAvailable: available.Proposals,
		Toolset:           toolset,
	})
	hooks.AfterPromptCompose(orchestration.TraceEvent{
		PromptVersion:       promptPlan.Version,
		PromptHash:          promptPlan.Hash,
		PromptModules:       promptPlan.Modules,
		Toolset:             string(toolsetDecision.Toolset),
		ToolsetSelectReason: toolsetDecision.Reason,
	})
	convCtx.SetSystemMessage(promptPlan.Content)

	b.logger.Info("conversation started",
		"user_id", userID,
		"username", c.Sender().Username,
		"message", userText,
	)

	// Capture the user text locally so we can always archive it even if
	// EnforceLimit (below) trims it out of convCtx.
	convCtx.AddUserMessage(userText)

	// Phase 08 Runtime Diet: retrieval context is routed, not automatic.
	// Generic chat/status/code turns clear this slot so stale capsule content
	// does not leak into the next answer.
	convCtx.SetSearchContext(retrievalCapsule.Text)

	// Snapshot count for archiver loop; EnforceLimit now runs after the turn
	// completes so context trimming doesn't add to perceived wait time.
	// Loop messages added by runToolCallingLoop occupy [preLoopIdx, end).
	preLoopIdx := convCtx.MessageCount()

	// No LLM configured — echo mode
	if b.llm == nil {
		echo := "Echo: " + userText
		if err := c.Send(echo); err != nil {
			b.logger.Error("failed to send echo", "user_id", userID, "error", err)
		}
		convCtx.AddAssistantMessage(echo)
		return
	}

	// Check hard budget before LLM call
	if b.budget != nil && b.budget.IsHardBudgetExceeded() {
		b.logger.Warn("hard budget exceeded, halting LLM call", "user_id", userID)
		c.Send("Budget limit reached. LLM calls are temporarily halted.")
		return
	}

	// Predict cost and check affordability
	if b.budget != nil && !b.budget.CanAfford(convCtx.EstimatedTokens(), 500) {
		b.logger.Warn("predicted cost exceeds hard budget, halting LLM call", "user_id", userID)
		c.Send("Predicted cost would exceed budget. Please adjust your budget or wait.")
		return
	}

	// Slice 16c: send an immediate placeholder so the user knows we received
	// their message. consumeStream edits this instead of creating a new one.
	placeholder, _ := c.Bot().Send(c.Recipient(), "⏳")

	response, stats := b.runToolCallingLoop(context.Background(), c, convCtx, userID, placeholder, toolAllowlist, promptPlan, toolsetDecision, strings.TrimSpace(retrievalCapsule.Text) != "")
	if response != "" {
		// Non-streamed delivery: delete the placeholder, send the real response.
		if placeholder != nil {
			if err := c.Bot().Delete(placeholder); err != nil {
				logPlaceholderDeleteFailure(b.logger, userID, placeholder, err)
			}
		}
		b.sendAssistant(c, response)
	}
	// When response == "", streaming edited the placeholder in place — nothing to do.

	// Slice 12b + 12u.7 (HR-04): archive the user message and every
	// message produced during this turn. turn_index is allocated from the
	// archive's MAX(turn_index) for this chat so it stays correct even
	// when EnforceLimit trims convCtx (which would have made an
	// in-memory MessageCount snapshot unreliable). The user message is
	// captured locally above so we always have the original even if
	// EnforceLimit dropped it from convCtx.
	if b.archiver != nil && b.archiveDB != nil {
		chatID := c.Chat().ID
		ctx := context.Background()

		nextIdx := int64(0)
		if maxIdx, err := b.archiveDB.MaxTurnIndex(ctx, chatID); err == nil {
			nextIdx = maxIdx + 1
		} else {
			b.logger.Warn("archive: max turn_index lookup failed",
				"chat_id", chatID, "error", err)
		}

		archiveConversationTurns(ctx, b.logger, b.archiveAppenderForTurn(), archiveTurnInput{
			ChatID:       chatID,
			UserID:       c.Sender().ID,
			NextIndex:    nextIdx,
			UserText:     userText,
			LoopMessages: convCtx.MessagesSince(preLoopIdx),
			Stats:        stats,
			ElapsedMS:    time.Since(turnStart).Milliseconds(),
			TokensIn:     convCtx.TotalTokensUsed(),
		})

	}

	// Slice 16d: context enforcement runs after the user has seen the
	// response so context trimming doesn't add to perceived wait time.
	go func() {
		if err := convCtx.EnforceLimit(context.Background()); err != nil {
			b.logger.Error("context enforcement failed", "user_id", userID, "error", err)
		}
	}()

	// Slice 11r: per-turn telemetry. elapsed_ms is wall-clock from
	// receive to "ready to send"; llm_calls and tool_calls expose where
	// time went so we can correlate slow turns to the responsible
	// subsystem without sprinkling timers everywhere.
	b.logger.Info("conversation complete",
		"user_id", userID,
		"tokens_used", convCtx.TotalTokensUsed(),
		"elapsed_ms", time.Since(turnStart).Milliseconds(),
		"llm_calls", stats.llmCalls,
		"tool_calls", stats.toolCalls,
		"loop_steps", stats.loopSteps,
		"prompt_version", stats.promptVersion,
		"prompt_hash", stats.promptHash,
		"prompt_modules", strings.Join(stats.promptModules, ","),
		"toolset", stats.toolset,
		"toolset_select_reason", stats.toolsetSelectReason,
		"tools_exposed", strings.Join(stats.toolsExposed, ","),
		"tools_called", strings.Join(stats.toolsCalled, ","),
		"read_skills", strings.Join(stats.readSkills, ","),
		"hidden_tool_rejected", stats.hiddenToolRejected,
		"skills_read", stats.skillsRead,
		"swarm_used", stats.swarmUsed,
		"sandbox_used", stats.sandboxUsed,
		"terminal_tool", stats.terminalTool,
		"duplicate_tool_rejected", stats.duplicateToolCall,
		"tokens_prompt", stats.tokensPrompt,
		"tokens_completion", stats.tokensCompletion,
		"tokens_total", stats.tokensTotal,
		"cost_usd", fmt.Sprintf("%.6f", stats.costUSD),
	)
	hooks.AfterTurn(orchestration.TraceEvent{
		PromptVersion:          stats.promptVersion,
		PromptHash:             stats.promptHash,
		PromptModules:          stats.promptModules,
		Toolset:                stats.toolset,
		ToolsetSelectReason:    stats.toolsetSelectReason,
		ToolsExposed:           stats.toolsExposed,
		ToolsCalled:            stats.toolsCalled,
		HiddenToolRejected:     stats.hiddenToolRejected,
		SkillReads:             len(stats.readSkills),
		SwarmUsed:              stats.swarmUsed,
		SandboxUsed:            stats.sandboxUsed,
		TokensPrompt:           stats.tokensPrompt,
		TokensCompletion:       stats.tokensCompletion,
		TokensTotal:            stats.tokensTotal,
		EstimatedContextTokens: convCtx.EstimatedTokens(),
		CostUSD:                stats.costUSD,
		LatencyMS:              time.Since(turnStart).Milliseconds(),
		LLMCalls:               stats.llmCalls,
		ToolCalls:              stats.toolCalls,
	})
}

func logPlaceholderDeleteFailure(logger *slog.Logger, userID string, placeholder *tele.Message, err error) {
	if err == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	args := []any{"user_id", userID, "error", err}
	if placeholder != nil {
		args = append(args, "message_id", placeholder.ID)
	}
	logger.Debug("telegram cleanup: placeholder delete failed", args...)
}

func (b *Bot) swarmToolsAvailable() bool {
	return b.swarmMgr != nil && b.tools != nil && b.tools.Get("run_aurabot_swarm") != nil
}

func (b *Bot) proposalToolsAvailable() bool {
	return false
}

func (b *Bot) sandboxToolsAvailable() bool {
	return b.tools != nil && b.tools.Get("execute_code") != nil
}

func (b *Bot) workspaceToolsAvailable() bool {
	return b.tools != nil && b.tools.Get("read_file") != nil && b.tools.Get("write_file") != nil
}

func runtimeToolsetForTurn(toolset orchestration.Toolset, retrievalCapsule turnRetrievalCapsule) orchestration.RuntimeToolset {
	runtimeToolset := orchestration.NewRuntimeToolset(toolset)
	if toolset != orchestration.ToolsetDocument || !retrievalCapsule.SuppressSearchMemory || strings.TrimSpace(retrievalCapsule.Text) == "" {
		return runtimeToolset
	}
	return orchestration.FilterToolset(runtimeToolset, func(_ orchestration.ToolsetContext, name string) bool {
		return name != "search_memory"
	})
}

// turnStats aggregates per-turn counters returned from runToolCallingLoop
// so handleConversation can emit a single structured log line covering
// total latency, LLM round-trips, and tool calls.
type turnStats struct {
	llmCalls                int
	toolCalls               int
	loopSteps               int
	promptVersion           string
	promptModules           []string
	promptHash              string
	toolset                 string
	toolsetSelectReason     string
	toolsExposed            []string
	toolsCalled             []string
	readSkills              []string
	retrievalCapsulePresent bool
	hiddenToolRejected      bool
	skillsRead              bool
	swarmUsed               bool
	sandboxUsed             bool
	terminalTool            string
	duplicateToolCall       bool
	tokensPrompt            int
	tokensCompletion        int
	tokensTotal             int
	costUSD                 float64
}

func (b *Bot) runToolCallingLoop(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, placeholder *tele.Message, toolAllowlist []string, promptPlan orchestration.PromptPlan, toolsetDecision orchestration.ToolsetDecision, retrievalCapsulePresent bool) (string, turnStats) {
	loopPolicy, _ := orchestration.LoopPolicyForToolset(toolsetDecision.Toolset)
	maxIterations := b.maxToolLoopIterations(toolsetDecision.Toolset)
	baseStats := turnStats{
		promptVersion:           promptPlan.Version,
		promptModules:           append([]string(nil), promptPlan.Modules...),
		promptHash:              promptPlan.Hash,
		toolset:                 string(toolsetDecision.Toolset),
		toolsetSelectReason:     toolsetDecision.Reason,
		retrievalCapsulePresent: retrievalCapsulePresent,
	}
	toolDefs := orderToolDefinitionsForAllowlist(b.tools.DefinitionsFor(toolAllowlist), toolAllowlist)
	baseStats.toolsExposed = toolDefinitionNames(toolDefs)
	var currentStats turnStats
	afterTool := orchestration.AfterToolCallbackForToolset(toolsetDecision.Toolset)
	result, err := agentruntime.Run(ctx, agentruntime.Invocation{
		Client: telegramLoopClient{bot: b, teleCtx: c, userID: userID, placeholder: placeholder},
		Executor: agentloop.ToolExecutorFunc(func(ctx context.Context, calls []llm.ToolCall) agentloop.ExecutionSummary {
			execution := b.executeToolCalls(ctx, c, convCtx, userID, calls, baseStats.toolsExposed, toolsetDecision.Toolset, currentStats.readSkills, afterTool)
			return agentloop.ExecutionSummary{
				LastResult:     execution.lastResult,
				FatalResult:    userFacingFatalToolResult(execution.fatalResult),
				HiddenRejected: execution.hiddenRejected,
				ReadSkillNames: execution.readSkillNames,
				TerminalTool:   execution.terminalTool,
			}
		}),
		State:                   convCtx,
		PromptPlan:              promptPlan,
		ToolsetDecision:         toolsetDecision,
		Tools:                   toolDefs,
		RetrievalCapsulePresent: retrievalCapsulePresent,
		Options: agentloop.Options{
			MaxIterations:           maxIterations,
			MaxElapsed:              loopPolicy.MaxElapsed,
			TerminalToolPolicy:      b.terminalToolPolicyEnabled(),
			AllowNoToolFinalization: loopPolicy.AllowNoToolFinalization,
			BeforeTool:              orchestration.BeforeToolCallbackForToolset(toolsetDecision.Toolset),
			BeforeLLM: func() (string, bool) {
				// Context bounding happens after the response. Re-enforcing on every
				// tool iteration can trigger a compression LLM call mid-response,
				// which both burns latency and degrades fidelity.
				// MaxToolIterations already caps growth within a single user turn.
				if b.budget != nil && b.budget.IsHardBudgetExceeded() {
					b.logger.Warn("hard budget exceeded during tool loop", "user_id", userID)
					return "Budget limit reached. LLM calls are temporarily halted.", true
				}
				return "", false
			},
			RecordUsage: func(usage llm.TokenUsage) {
				if b.budget != nil {
					b.budget.RecordUsage(usage)
				}
			},
			EstimateCost: func(usage llm.TokenUsage) float64 {
				return estimateUsageCost(usage, b.cfg.CostInputPerMTokens, b.cfg.CostOutputPerMTokens)
			},
			TerminalHandler: func(ctx context.Context, terminalTool, lastToolResult string, stats *agentloop.Stats) (string, bool, bool) {
				if terminalTool == "execute_code" {
					response := formatTerminalExecuteCodeResult(lastToolResult)
					convCtx.AddAssistantMessage(response)
					return response, false, true
				}
				if isFileGenerationTool(terminalTool) {
					response := formatTerminalFileResult(terminalTool, lastToolResult)
					convCtx.AddAssistantMessage(response)
					return response, false, true
				}
				telegramStats := mergeAgentLoopStats(baseStats, *stats)
				response, delivered := b.finalizeTerminalToolWithNoToolLLM(ctx, c, convCtx, userID, placeholder, lastToolResult, &telegramStats)
				*stats = applyTelegramTerminalStats(*stats, telegramStats)
				if delivered {
					return "", true, true
				}
				return response, false, true
			},
		},
		OnEvent: func(event agentruntime.Event) {
			switch event.Type {
			case agentruntime.EventToolsExposed:
				orchestration.EnsureHooks(b.orchHooks).BeforeExposeTools(orchestration.TraceEvent{
					PromptVersion:       event.PromptVersion,
					PromptHash:          event.PromptHash,
					PromptModules:       event.PromptModules,
					Toolset:             string(event.Toolset),
					ToolsetSelectReason: event.ToolsetSelectReason,
					ToolsExposed:        event.ToolsExposed,
				})
			case agentruntime.EventStats:
				currentStats = mergeAgentLoopStats(baseStats, event.Stats)
				b.storeOrchestrationSnapshot(userID, currentStats)
			}
		},
	})
	if err != nil {
		b.logger.Error("agent loop failed", "user_id", userID, "error", err)
	}
	stats := mergeAgentLoopStats(baseStats, result.Stats)
	currentStats = stats
	if result.Stats.LLMCalls > 0 && result.Stats.TerminalTool == "" {
		b.notifySoftBudget(c, userID)
	}
	return result.Text, currentStats
}

type telegramLoopClient struct {
	bot         *Bot
	teleCtx     tele.Context
	userID      string
	placeholder *tele.Message
}

func (c telegramLoopClient) Chat(ctx context.Context, messages []llm.Message, tools []llm.ToolDefinition) (agentloop.ChatResponse, error) {
	req := llm.Request{
		Messages: messages,
		Model:    c.bot.cfg.LLMModel,
		Tools:    tools,
	}
	ch, err := c.bot.llm.Stream(ctx, req)
	if err != nil {
		c.bot.logger.Error("LLM stream failed", "user_id", c.userID, "error", err)
		return agentloop.ChatResponse{Response: llm.Response{Content: "Sorry, I couldn't process your message. Please try again."}}, err
	}
	resp, delivered, err := c.bot.consumeStream(c.teleCtx, ch, c.userID, c.placeholder)
	if err != nil {
		c.bot.logger.Error("LLM stream read failed", "user_id", c.userID, "error", err)
		return agentloop.ChatResponse{Response: llm.Response{Content: "Sorry, I couldn't process your message. Please try again."}}, err
	}
	return agentloop.ChatResponse{Response: resp, Delivered: delivered}, nil
}

func mergeAgentLoopStats(base turnStats, stats agentloop.Stats) turnStats {
	base.llmCalls = stats.LLMCalls
	base.toolCalls = stats.ToolCalls
	base.loopSteps = stats.LoopSteps
	base.toolsCalled = append([]string(nil), stats.ToolsCalled...)
	base.readSkills = append([]string(nil), stats.ReadSkills...)
	base.hiddenToolRejected = stats.HiddenToolRejected
	base.skillsRead = stats.SkillsRead
	base.swarmUsed = stats.SwarmUsed
	base.sandboxUsed = stats.SandboxUsed
	base.terminalTool = stats.TerminalTool
	base.duplicateToolCall = stats.DuplicateToolCall
	base.tokensPrompt = stats.TokensPrompt
	base.tokensCompletion = stats.TokensCompletion
	base.tokensTotal = stats.TokensTotal
	base.costUSD = stats.CostUSD
	return base
}

func applyTelegramTerminalStats(stats agentloop.Stats, telegramStats turnStats) agentloop.Stats {
	stats.LLMCalls = telegramStats.llmCalls
	stats.LoopSteps = telegramStats.loopSteps
	stats.TokensPrompt = telegramStats.tokensPrompt
	stats.TokensCompletion = telegramStats.tokensCompletion
	stats.TokensTotal = telegramStats.tokensTotal
	stats.CostUSD = telegramStats.costUSD
	return stats
}

func (b *Bot) appendRegisteredMCPTools(allowlist []string) []string {
	if b == nil || b.tools == nil {
		return allowlist
	}
	for _, name := range b.tools.NamesByCategory(auratools.CategoryMCP) {
		allowlist = appendUniqueStrings(allowlist, name)
	}
	return allowlist
}

func (b *Bot) maxToolLoopIterations(toolset orchestration.Toolset) int {
	maxIterations := 10
	if b != nil && b.cfg != nil && b.cfg.MaxToolIterations > 0 {
		maxIterations = b.cfg.MaxToolIterations
	}
	if b != nil && b.cfg != nil && b.cfg.AgentLoopMaxSteps > 0 && b.cfg.AgentLoopMaxSteps < maxIterations {
		maxIterations = b.cfg.AgentLoopMaxSteps
	}
	if maxIterations < 1 {
		return 1
	}
	return maxIterations
}

func (b *Bot) terminalToolPolicyEnabled() bool {
	if b == nil || b.cfg == nil {
		return true
	}
	return config.NormalizeTerminalToolPolicy(b.cfg.TerminalToolPolicy) != "off"
}
