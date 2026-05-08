package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/agentloop"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/orchestration"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/tools"

	tele "gopkg.in/telebot.v4"
)

func (b *Bot) handleConversation(c tele.Context) {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	turnStart := time.Now()

	// Track active conversation
	b.active.Store(userID, true)
	defer b.active.Delete(userID)

	// Get or create conversation context
	ctxVal, loaded := b.ctxMap.LoadOrStore(userID, conversation.NewContext(conversation.Config{
		MaxTokens:   b.cfg.MaxContextTokens,
		MaxMessages: b.cfg.MaxHistoryMessages,
		Summarizer:  b.llm,
		Logger:      b.logger,
	}))
	convCtx := ctxVal.(*conversation.Context)
	_ = loaded // kept for clarity; system prompt now refreshes every turn
	userText := c.Text()

	// Build the versioned prompt and focused tool profile fresh on every
	// turn so runtime time, overlays, skills, swarm, and sandbox state stay
	// accurate without restarting the bot.
	overlay := conversation.LoadPromptOverlay(b.cfg.PromptOverlayPath)
	var skillsBlock string
	// Slice 11q: read SOUL.md / AGENTS.md / USER.md / TOOLS.md from the
	// configured overlay dir. Picobot pattern: lets the operator tune
	// personality, durable user facts, and tool guidance by editing a
	// file — the next user turn picks up the change with no recompile or
	// restart. Files are optional; missing ones are skipped silently.
	if b.skills != nil {
		loadedSkills, err := b.skills.LoadAll()
		if err != nil {
			b.logger.Warn("failed to load local skills", "error", err)
		} else if block := auraskills.PromptBlock(loadedSkills); block != "" {
			skillsBlock = block
		}
	}
	available := orchestration.Availability{
		Swarm:          b.swarmToolsAvailable(),
		Sandbox:        b.sandboxToolsAvailable(),
		Proposals:      b.proposalToolsAvailable(),
		WorkspaceFiles: b.workspaceToolsAvailable(),
	}
	hooks := orchestration.EnsureHooks(b.orchHooks)
	hooks.BeforeProfileSelect(orchestration.TraceEvent{ToolProfile: b.cfg.ToolProfileMode})
	profileDecision := orchestration.SelectProfileDecision(userText, b.cfg.ToolProfileMode, available)
	hooks.AfterProfileSelect(orchestration.TraceEvent{
		ToolProfile:         string(profileDecision.Profile),
		ProfileSelectReason: profileDecision.Reason,
	})
	toolProfile := profileDecision.Profile
	toolAllowlist, err := orchestration.ToolsForProfile(toolProfile, available)
	if err != nil {
		b.logger.Warn("orchestration profile failed; falling back to default", "profile", toolProfile, "error", err)
		toolProfile = orchestration.ProfileDefault
		profileDecision = orchestration.ProfileDecision{Profile: toolProfile, Reason: "profile allowlist failed; fell back to default"}
		toolAllowlist, _ = orchestration.ToolsForProfile(toolProfile, available)
	}
	hooks.BeforePromptCompose(orchestration.TraceEvent{
		ToolProfile:         string(profileDecision.Profile),
		ProfileSelectReason: profileDecision.Reason,
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
		Profile:           toolProfile,
	})
	hooks.AfterPromptCompose(orchestration.TraceEvent{
		PromptVersion:       promptPlan.Version,
		PromptHash:          promptPlan.Hash,
		PromptModules:       promptPlan.Modules,
		ToolProfile:         string(profileDecision.Profile),
		ProfileSelectReason: profileDecision.Reason,
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

	// Slice 11p/06: speculative memory retrieval. The model used to discover
	// durable memory only after an explicit wiki search round-trip, which cost
	// a full extra LLM round-trip per turn ("reason → emit tool call →
	// read result → re-reason → answer"). We now run the search up-front
	// and inject the top hits into the system prompt so the very first
	// inference already has relevant context. The embedding cache (slice
	// 11h) makes repeat queries effectively free; cold queries pay one
	// embed call but save the round-trip. Further exact inspection uses bounded
	// workspace file tools.
	// Picobot equivalent: internal/agent/context.go ranker injection.
	if contextText := runSpeculativeSearch(context.Background(), b.search, userText, b.cfg.SpeculativeSearchTimeoutMS, b.logger, userID); contextText != "" {
		convCtx.SetSearchContext(contextText)
	}

	// Snapshot count for archiver loop; EnforceLimit now runs after the turn
	// completes so summarizer latency doesn't add to perceived wait time.
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

	response, stats := b.runToolCallingLoop(context.Background(), c, convCtx, userID, placeholder, toolAllowlist, promptPlan, profileDecision)
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

		// Phase 5A: post-turn memory capture. When the summarizer is active,
		// archiveAppenderForTurn writes synchronously through archiveDB so this
		// extraction sees the just-finished user/assistant turn.
		if b.summRunner != nil {
			triggered, extraction, err := b.summRunner.MaybeExtract(ctx, chatID)
			if err != nil {
				b.logger.Warn("summarizer extraction failed", "chat_id", chatID, "error", err)
			} else if triggered && extraction != nil {
				stats.memoryCaptureTriggered = true
				stats.memoryCaptureDecisions = len(extraction.Decisions)
				stats.memoryCaptureApplied = extraction.Applied
				b.logger.Info("post-turn memory capture complete",
					"chat_id", chatID,
					"decisions", len(extraction.Decisions),
					"applied", extraction.Applied,
				)
			}
		}
	}

	// Slice 16d: context enforcement runs after the user has seen the
	// response so summarizer latency doesn't add to perceived wait time.
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
		"tool_profile", stats.toolProfile,
		"profile_select_reason", stats.profileSelectReason,
		"tools_exposed", strings.Join(stats.toolsExposed, ","),
		"tools_called", strings.Join(stats.toolsCalled, ","),
		"active_capabilities", strings.Join(stats.activeCapabilities, ","),
		"read_skills", strings.Join(stats.readSkills, ","),
		"hidden_tool_rejected", stats.hiddenToolRejected,
		"skill_preflight_failed", stats.skillPreflightFail,
		"skills_read", stats.skillsRead,
		"swarm_used", stats.swarmUsed,
		"sandbox_used", stats.sandboxUsed,
		"terminal_tool", stats.terminalTool,
		"duplicate_tool_rejected", stats.duplicateToolCall,
		"tokens_prompt", stats.tokensPrompt,
		"tokens_completion", stats.tokensCompletion,
		"tokens_total", stats.tokensTotal,
		"cost_usd", fmt.Sprintf("%.6f", stats.costUSD),
		"memory_capture_triggered", stats.memoryCaptureTriggered,
		"memory_capture_decisions", stats.memoryCaptureDecisions,
		"memory_capture_applied", stats.memoryCaptureApplied,
	)
	hooks.AfterTurn(orchestration.TraceEvent{
		PromptVersion:          stats.promptVersion,
		PromptHash:             stats.promptHash,
		PromptModules:          stats.promptModules,
		ToolProfile:            stats.toolProfile,
		ProfileSelectReason:    stats.profileSelectReason,
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

// turnStats aggregates per-turn counters returned from runToolCallingLoop
// so handleConversation can emit a single structured log line covering
// total latency, LLM round-trips, and tool calls.
type turnStats struct {
	llmCalls               int
	toolCalls              int
	loopSteps              int
	promptVersion          string
	promptModules          []string
	promptHash             string
	toolProfile            string
	profileSelectReason    string
	toolsExposed           []string
	toolsCalled            []string
	activeCapabilities     []string
	readSkills             []string
	hiddenToolRejected     bool
	skillPreflightFail     bool
	skillsRead             bool
	swarmUsed              bool
	sandboxUsed            bool
	terminalTool           string
	duplicateToolCall      bool
	tokensPrompt           int
	tokensCompletion       int
	tokensTotal            int
	costUSD                float64
	memoryCaptureTriggered bool
	memoryCaptureDecisions int
	memoryCaptureApplied   int
}

func (b *Bot) runToolCallingLoop(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, placeholder *tele.Message, toolAllowlist []string, promptPlan orchestration.PromptPlan, profileDecision orchestration.ProfileDecision) (string, turnStats) {
	loopPolicy, _ := orchestration.LoopPolicyForProfile(profileDecision.Profile)
	maxIterations := b.maxToolLoopIterations(profileDecision.Profile)
	baseStats := turnStats{
		promptVersion:       promptPlan.Version,
		promptModules:       append([]string(nil), promptPlan.Modules...),
		promptHash:          promptPlan.Hash,
		toolProfile:         string(profileDecision.Profile),
		profileSelectReason: profileDecision.Reason,
		activeCapabilities:  capabilityNames(orchestration.CapabilitiesForProfile(profileDecision.Profile)),
	}
	toolDefs := orderToolDefinitionsForAllowlist(b.tools.DefinitionsFor(toolAllowlist), toolAllowlist)
	baseStats.toolsExposed = toolDefinitionNames(toolDefs)
	orchestration.EnsureHooks(b.orchHooks).BeforeExposeTools(orchestration.TraceEvent{
		PromptVersion:       promptPlan.Version,
		PromptHash:          promptPlan.Hash,
		PromptModules:       promptPlan.Modules,
		ToolProfile:         string(profileDecision.Profile),
		ProfileSelectReason: profileDecision.Reason,
		ToolsExposed:        baseStats.toolsExposed,
	})
	var currentStats turnStats
	result, err := agentloop.Run(ctx,
		telegramLoopClient{bot: b, teleCtx: c, userID: userID, placeholder: placeholder},
		agentloop.ToolExecutorFunc(func(ctx context.Context, calls []llm.ToolCall) agentloop.ExecutionSummary {
			execution := b.executeToolCalls(ctx, c, convCtx, userID, calls, baseStats.toolsExposed, profileDecision.Profile, currentStats.readSkills)
			return agentloop.ExecutionSummary{
				LastResult:             execution.lastResult,
				FatalResult:            userFacingFatalToolResult(execution.fatalResult),
				HiddenRejected:         execution.hiddenRejected,
				SkillPreflightRejected: execution.skillPreflightRejected,
				ReadSkillNames:         execution.readSkillNames,
				TerminalTool:           execution.terminalTool,
			}
		}),
		convCtx,
		agentloop.Options{
			MaxIterations:           maxIterations,
			Tools:                   toolDefs,
			TerminalToolPolicy:      b.terminalToolPolicyEnabled(),
			AllowNoToolFinalization: loopPolicy.AllowNoToolFinalization,
			DuplicateToolResult: func(call llm.ToolCall) string {
				return tools.FormatToolError(fmt.Errorf("duplicate tool call %q with identical arguments skipped; use the previous result already returned in this turn", call.Name))
			},
			BeforeLLM: func() (string, bool) {
				// Context bounding happens once at the start of handleConversation.
				// Re-enforcing on every tool iteration triggered a summarizer LLM
				// call mid-response, which both burned latency and degraded fidelity.
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
			OnStats: func(stats agentloop.Stats) {
				currentStats = mergeAgentLoopStats(baseStats, stats)
				b.storeOrchestrationSnapshot(userID, currentStats)
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
	base.skillPreflightFail = stats.SkillPreflightRejected
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

func (b *Bot) maxToolLoopIterations(profile orchestration.Profile) int {
	maxIterations := 10
	if b != nil && b.cfg != nil && b.cfg.MaxToolIterations > 0 {
		maxIterations = b.cfg.MaxToolIterations
	}
	if b != nil && b.cfg != nil && b.cfg.AgentLoopMaxSteps > 0 && b.cfg.AgentLoopMaxSteps < maxIterations {
		maxIterations = b.cfg.AgentLoopMaxSteps
	}
	if loopPolicy, ok := orchestration.LoopPolicyForProfile(profile); ok && loopPolicy.MaxSteps > 0 && loopPolicy.MaxSteps < maxIterations {
		maxIterations = loopPolicy.MaxSteps
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
