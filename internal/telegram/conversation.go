package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/orchestration"
	"github.com/aura/aura/internal/search"
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
		Swarm:     b.swarmToolsAvailable(),
		Sandbox:   b.sandboxToolsAvailable(),
		Proposals: b.proposalToolsAvailable(),
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

	// Slice 11p: speculative wiki retrieval. The model used to discover
	// durable memory only by emitting a search_wiki tool call, which cost
	// a full extra LLM round-trip per turn ("reason → emit tool call →
	// read result → re-reason → answer"). We now run the search up-front
	// and inject the top hits into the system prompt so the very first
	// inference already has relevant context. The embedding cache (slice
	// 11h) makes repeat queries effectively free; cold queries pay one
	// embed call but save the round-trip. The explicit search_wiki tool
	// stays available for follow-up queries the model wants to refine.
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
		"terminal_swarm", stats.terminalSwarm,
		"swarm_finalization", stats.swarmFinalization,
		"post_swarm_tool_calls", stats.postSwarmToolCalls,
		"duplicate_swarm_rejected", stats.duplicateSwarm,
		"worker_count", stats.workerCount,
		"worker_failures", stats.workerFailures,
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

func (b *Bot) archiveAppenderForTurn() conversation.TurnAppender {
	if b == nil {
		return nil
	}
	if b.summRunner != nil && b.archiveDB != nil {
		return b.archiveDB
	}
	return b.archiver
}

func (b *Bot) swarmToolsAvailable() bool {
	return b.swarmMgr != nil && b.tools != nil && b.tools.Get("run_aurabot_swarm") != nil
}

func (b *Bot) proposalToolsAvailable() bool {
	return b.tools != nil && b.tools.Get("propose_wiki_change") != nil
}

func (b *Bot) sandboxToolsAvailable() bool {
	return b.tools != nil && b.tools.Get("execute_code") != nil
}

func runSpeculativeSearch(ctx context.Context, repo search.Searcher, userText string, timeoutMS int, logger *slog.Logger, userID string) string {
	if repo == nil || !repo.IsIndexed() {
		return ""
	}
	timeout := speculativeSearchTimeout(timeoutMS)
	searchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	results, err := repo.Search(searchCtx, userText, 5)
	if err == nil && len(results) > 0 {
		return search.FormatResults(results)
	}
	if err != nil && logger != nil {
		logger.Debug("speculative wiki search failed",
			"user_id", userID,
			"error", err,
			"timeout_ms", timeout.Milliseconds(),
			"elapsed_ms", time.Since(start).Milliseconds(),
		)
	}
	return ""
}

func speculativeSearchTimeout(timeoutMS int) time.Duration {
	if timeoutMS <= 0 {
		timeoutMS = config.DefaultSpeculativeSearchTimeoutMS
	}
	return time.Duration(timeoutMS) * time.Millisecond
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
	terminalSwarm          bool
	swarmFinalization      string
	postSwarmToolCalls     int
	duplicateSwarm         bool
	workerCount            int
	workerFailures         int
	tokensPrompt           int
	tokensCompletion       int
	tokensTotal            int
	costUSD                float64
	memoryCaptureTriggered bool
	memoryCaptureDecisions int
	memoryCaptureApplied   int
}

type orchestrationSnapshot struct {
	StoredAt            time.Time
	PromptVersion       string
	PromptModules       []string
	PromptHash          string
	ToolProfile         string
	ProfileSelectReason string
	ToolsExposed        []string
	ToolsCalled         []string
	ActiveCapabilities  []string
	ReadSkills          []string
	LoopSteps           int
	HiddenToolRejected  bool
	SkillPreflightFail  bool
	SkillsRead          bool
	SwarmUsed           bool
	SandboxUsed         bool
	TerminalTool        string
	TerminalSwarm       bool
	SwarmFinalization   string
	PostSwarmToolCalls  int
	DuplicateSwarm      bool
	WorkerCount         int
	WorkerFailures      int
	TokensPrompt        int
	TokensCompletion    int
	TokensTotal         int
	CostUSD             float64
}

type archiveTurnInput struct {
	ChatID       int64
	UserID       int64
	NextIndex    int64
	UserText     string
	LoopMessages []llm.Message
	Stats        turnStats
	ElapsedMS    int64
	TokensIn     int
}

func archiveConversationTurns(ctx context.Context, logger *slog.Logger, archiver conversation.TurnAppender, input archiveTurnInput) {
	if archiver == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	nextIdx := input.NextIndex
	appendTurn := func(turn conversation.Turn) {
		if err := archiver.Append(ctx, turn); err != nil {
			logger.Error("archive: append failed",
				"chat_id", turn.ChatID,
				"turn_index", turn.TurnIndex,
				"role", turn.Role,
				"error", err)
		}
	}

	appendTurn(conversation.Turn{
		ChatID:    input.ChatID,
		UserID:    input.UserID,
		TurnIndex: nextIdx,
		Role:      "user",
		Content:   input.UserText,
	})
	nextIdx++

	for i, msg := range input.LoopMessages {
		turn := conversation.Turn{
			ChatID:     input.ChatID,
			UserID:     input.UserID,
			TurnIndex:  nextIdx,
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
		}
		if len(msg.ToolCalls) > 0 {
			if raw, err := json.Marshal(msg.ToolCalls); err == nil {
				turn.ToolCalls = string(raw)
			} else {
				logger.Warn("archive: tool_calls marshal failed",
					"chat_id", input.ChatID, "turn_index", nextIdx, "error", err)
			}
		}
		if msg.Role == "assistant" && i == len(input.LoopMessages)-1 {
			turn.LLMCalls = input.Stats.llmCalls
			turn.ToolCallsCount = input.Stats.toolCalls
			turn.ElapsedMS = input.ElapsedMS
			turn.TokensIn = input.TokensIn
		}
		appendTurn(turn)
		nextIdx++
	}
}

func (b *Bot) runToolCallingLoop(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, placeholder *tele.Message, toolAllowlist []string, promptPlan orchestration.PromptPlan, profileDecision orchestration.ProfileDecision) (string, turnStats) {
	loopPolicy, _ := orchestration.LoopPolicyForProfile(profileDecision.Profile)
	maxIterations := b.maxToolLoopIterations(profileDecision.Profile)

	stats := turnStats{
		promptVersion:       promptPlan.Version,
		promptModules:       append([]string(nil), promptPlan.Modules...),
		promptHash:          promptPlan.Hash,
		toolProfile:         string(profileDecision.Profile),
		profileSelectReason: profileDecision.Reason,
		activeCapabilities:  capabilityNames(orchestration.CapabilitiesForProfile(profileDecision.Profile)),
	}
	var lastToolResult string
	toolDefs := orderToolDefinitionsForAllowlist(b.tools.DefinitionsFor(toolAllowlist), toolAllowlist)
	stats.toolsExposed = toolDefinitionNames(toolDefs)
	orchestration.EnsureHooks(b.orchHooks).BeforeExposeTools(orchestration.TraceEvent{
		PromptVersion:       promptPlan.Version,
		PromptHash:          promptPlan.Hash,
		PromptModules:       promptPlan.Modules,
		ToolProfile:         string(profileDecision.Profile),
		ProfileSelectReason: profileDecision.Reason,
		ToolsExposed:        stats.toolsExposed,
	})
	b.storeOrchestrationSnapshot(userID, stats)
	for iteration := 0; iteration < maxIterations; iteration++ {
		// Context bounding happens once at the start of handleConversation.
		// Re-enforcing on every tool iteration triggered a summarizer LLM
		// call mid-response, which both burned latency and degraded fidelity.
		// MaxToolIterations already caps growth within a single user turn.

		if b.budget != nil && b.budget.IsHardBudgetExceeded() {
			b.logger.Warn("hard budget exceeded during tool loop", "user_id", userID)
			return "Budget limit reached. LLM calls are temporarily halted.", stats
		}

		req := llm.Request{
			Messages: convCtx.Messages(),
			Model:    b.cfg.LLMModel,
			Tools:    toolDefs,
		}

		stats.llmCalls++
		stats.loopSteps++
		ch, err := b.llm.Stream(ctx, req)
		if err != nil {
			b.logger.Error("LLM stream failed", "user_id", userID, "error", err)
			return "Sorry, I couldn't process your message. Please try again.", stats
		}

		resp, delivered, err := b.consumeStream(c, ch, userID, placeholder)
		if err != nil {
			b.logger.Error("LLM stream read failed", "user_id", userID, "error", err)
			return "Sorry, I couldn't process your message. Please try again.", stats
		}

		convCtx.TrackTokens(resp.Usage)
		stats.tokensPrompt += resp.Usage.PromptTokens
		stats.tokensCompletion += resp.Usage.CompletionTokens
		stats.tokensTotal += resp.Usage.TotalTokens
		stats.costUSD += estimateUsageCost(resp.Usage, b.cfg.CostInputPerMTokens, b.cfg.CostOutputPerMTokens)
		if b.budget != nil {
			b.budget.RecordUsage(resp.Usage)
		}

		if !resp.HasToolCalls {
			response := strings.TrimSpace(resp.Content)
			if response == "" {
				if lastToolResult != "" {
					response = lastToolResult
				} else {
					response = "I completed the request but do not have anything else to add."
				}
			}
			convCtx.AddAssistantMessage(response)
			b.notifySoftBudget(c, userID)
			// If consumeStream already progressively edited a Telegram
			// message with the full content, suppress the caller's
			// c.Send to avoid double-delivery. Empty response signals
			// "already delivered" to handleConversation.
			if delivered {
				return "", stats
			}
			return response, stats
		}

		convCtx.AddAssistantToolCallMessage(resp.Content, resp.ToolCalls)
		stats.toolCalls += len(resp.ToolCalls)
		for _, call := range resp.ToolCalls {
			stats.toolsCalled = append(stats.toolsCalled, call.Name)
			switch call.Name {
			case "read_skill":
				stats.skillsRead = true
			case "run_aurabot_swarm":
				stats.swarmUsed = true
			case "execute_code":
				stats.sandboxUsed = true
			}
		}
		b.storeOrchestrationSnapshot(userID, stats)
		var hiddenRejected bool
		callsToExecute, duplicateSwarmCalls := capDuplicateSwarmCalls(profileDecision.Profile, resp.ToolCalls)
		stats.duplicateSwarm = stats.duplicateSwarm || len(duplicateSwarmCalls) > 0
		execution := b.executeToolCalls(ctx, c, convCtx, userID, callsToExecute, stats.toolsExposed, profileDecision.Profile, stats.readSkills)
		lastToolResult, hiddenRejected = execution.lastResult, execution.hiddenRejected
		stats.skillPreflightFail = stats.skillPreflightFail || execution.skillPreflightRejected
		stats.readSkills = appendUniqueStrings(stats.readSkills, execution.readSkillNames...)
		stats.skillsRead = stats.skillsRead || len(stats.readSkills) > 0
		if execution.terminalTool != "" {
			stats.terminalTool = execution.terminalTool
		}
		for _, duplicate := range duplicateSwarmCalls {
			convCtx.AddToolResultMessage(duplicate.ID, tools.FormatFatalToolError(fmt.Errorf("run_aurabot_swarm already completed for this turn; use the existing aggregate result")))
		}
		stats.hiddenToolRejected = stats.hiddenToolRejected || hiddenRejected
		b.storeOrchestrationSnapshot(userID, stats)
		if execution.fatalResult != "" {
			response := userFacingFatalToolResult(execution.fatalResult)
			convCtx.AddAssistantMessage(response)
			return response, stats
		}
		if b.terminalToolPolicyEnabled() && execution.terminalTool != "" && execution.terminalTool != "run_aurabot_swarm" && loopPolicy.AllowNoToolFinalization {
			if execution.terminalTool == "execute_code" {
				response := formatTerminalExecuteCodeResult(lastToolResult)
				convCtx.AddAssistantMessage(response)
				b.storeOrchestrationSnapshot(userID, stats)
				return response, stats
			}
			if isFileGenerationTool(execution.terminalTool) {
				response := formatTerminalFileResult(execution.terminalTool, lastToolResult)
				convCtx.AddAssistantMessage(response)
				b.storeOrchestrationSnapshot(userID, stats)
				return response, stats
			}
			response, delivered := b.finalizeTerminalToolWithNoToolLLM(ctx, c, convCtx, userID, placeholder, lastToolResult, &stats)
			b.storeOrchestrationSnapshot(userID, stats)
			if delivered {
				return "", stats
			}
			return response, stats
		}
	}

	fallback := "Mi sono fermato prima di completare una risposta finale affidabile."
	if lastToolResult != "" {
		fallback += "\n\nHo completato alcuni passaggi interni, ma mi sono fermato prima di generare una risposta finale pulita."
	}
	convCtx.AddAssistantMessage(fallback)
	return fallback, stats
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

func (b *Bot) runTerminalSwarm(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, placeholder *tele.Message, stats turnStats) (string, turnStats) {
	goal := latestUserMessage(convCtx.Messages())
	call := llm.ToolCall{
		ID:        "auto_run_aurabot_swarm",
		Name:      "run_aurabot_swarm",
		Arguments: map[string]any{"goal": goal, "mode": "wait"},
	}
	convCtx.AddAssistantToolCallMessage("", []llm.ToolCall{call})
	stats.toolCalls++
	stats.toolsCalled = append(stats.toolsCalled, call.Name)
	stats.swarmUsed = true
	b.storeOrchestrationSnapshot(userID, stats)

	execution := b.executeToolCalls(ctx, c, convCtx, userID, []llm.ToolCall{call}, stats.toolsExposed, orchestration.ProfileSwarmResearch, stats.readSkills)
	lastToolResult, hiddenRejected := execution.lastResult, execution.hiddenRejected
	stats.hiddenToolRejected = stats.hiddenToolRejected || hiddenRejected
	stats.skillPreflightFail = stats.skillPreflightFail || execution.skillPreflightRejected
	if execution.terminalTool != "" {
		stats.terminalTool = execution.terminalTool
	}
	if execution.fatalResult != "" {
		response := userFacingFatalToolResult(execution.fatalResult)
		convCtx.AddAssistantMessage(response)
		b.storeOrchestrationSnapshot(userID, stats)
		return response, stats
	}
	stats.terminalSwarm = true
	stats.swarmFinalization = swarmResearchFinalization()
	stats.postSwarmToolCalls = 0
	stats.applySwarmMetrics(lastToolResult, b.cfg.CostInputPerMTokens, b.cfg.CostOutputPerMTokens)
	if stats.swarmFinalization == "no_tool_llm" {
		response, delivered := b.finalizeSwarmWithNoToolLLM(ctx, c, convCtx, userID, placeholder, lastToolResult, &stats)
		b.storeOrchestrationSnapshot(userID, stats)
		if delivered {
			return "", stats
		}
		return response, stats
	}
	response := formatTerminalSwarmResult(lastToolResult)
	convCtx.AddAssistantMessage(response)
	b.storeOrchestrationSnapshot(userID, stats)
	return response, stats
}

// executeToolCalls runs an assistant turn's tool calls concurrently and
// appends results in original order. The LLM batches independent calls into
// one assistant turn (e.g. search_wiki + web_search side-by-side); running
// them sequentially serialized N round-trips of latency for no reason.
//
// Concurrency safety: Registry.Execute is RWMutex-guarded for lookup, and
// individual tools run outside the lock. Wiki/source writes serialize on
// SQLite at the storage layer. Tool activity stays in logs/orchestration
// telemetry; Telegram receives only the final user-facing synthesis so normal
// users are not exposed to tool names, JSON payloads, or raw execution traces.
//
// Returns the last result content (in original order), used by the caller
// as a fallback when the model returns an empty final response.
type toolExecutionSummary struct {
	lastResult             string
	fatalResult            string
	hiddenRejected         bool
	skillPreflightRejected bool
	readSkillNames         []string
	terminalTool           string
}

func (b *Bot) executeToolCalls(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, calls []llm.ToolCall, toolsExposed []string, profile orchestration.Profile, readSkills []string) toolExecutionSummary {
	if len(calls) == 0 {
		return toolExecutionSummary{}
	}

	type outcome struct {
		id                     string
		tool                   string
		content                string
		elapsed                time.Duration
		errorClass             string
		hiddenRejected         bool
		skillPreflightRejected bool
		fatal                  bool
		readSkillName          string
		terminalTool           string
	}
	results := make([]outcome, len(calls))

	var wg sync.WaitGroup
	var summaryMu sync.Mutex
	summary := toolExecutionSummary{}
	loopPolicy, _ := orchestration.LoopPolicyForProfile(profile)
	for i, tc := range calls {
		wg.Add(1)
		go func(i int, tc llm.ToolCall) {
			defer wg.Done()
			hooks := orchestration.EnsureHooks(b.orchHooks)
			event := orchestration.TraceEvent{
				ToolName:     tc.Name,
				ToolsExposed: toolsExposed,
				ToolProfile:  string(profile),
			}
			start := time.Now()
			if !toolAllowed(tc.Name, toolsExposed) {
				event.HiddenToolRejected = true
				event.DurationMS = time.Since(start).Milliseconds()
				event.ErrorClass = "hidden_tool"
				event.ResultSizeBytes = 0
				results[i] = outcome{
					id:             tc.ID,
					tool:           tc.Name,
					content:        tools.FormatToolError(fmt.Errorf("tool %q is not available in this runtime; choose another exposed tool or answer from current context", tc.Name)),
					elapsed:        time.Since(start),
					errorClass:     "hidden_tool",
					hiddenRejected: true,
				}
				summaryMu.Lock()
				summary.hiddenRejected = true
				summaryMu.Unlock()
				hooks.AfterToolCall(event)
				return
			}
			if err := hooks.BeforeToolCall(event); err != nil {
				errorClass := "policy_error"
				content := tools.FormatFatalToolError(err)
				hidden := errors.Is(err, orchestration.ErrHiddenTool)
				if hidden {
					errorClass = "hidden_tool"
					content = tools.FormatFatalToolError(fmt.Errorf("tool %q is not exposed in the active tool profile", tc.Name))
				}
				event.HiddenToolRejected = hidden
				event.DurationMS = time.Since(start).Milliseconds()
				event.ErrorClass = errorClass
				results[i] = outcome{
					id:             tc.ID,
					tool:           tc.Name,
					content:        content,
					elapsed:        time.Since(start),
					errorClass:     errorClass,
					hiddenRejected: hidden,
					fatal:          true,
				}
				summaryMu.Lock()
				summary.hiddenRejected = summary.hiddenRejected || hidden
				summaryMu.Unlock()
				hooks.AfterToolCall(event)
				return
			}
			toolCtx := tools.WithUserID(ctx, userID)
			result, err := b.tools.Execute(toolCtx, tc.Name, tc.Arguments)
			if err != nil {
				result = tools.FormatToolError(err)
				b.logger.Warn("tool call failed", "user_id", userID, "tool", tc.Name, "error", err)
			}
			errorClass := ""
			if err != nil {
				errorClass = "tool_error"
			}
			event.DurationMS = time.Since(start).Milliseconds()
			event.ResultSizeBytes = len(result)
			event.ErrorClass = errorClass
			usage, cost := toolResultUsage(result, b.cfg)
			event.TokensPrompt = usage.PromptTokens
			event.TokensCompletion = usage.CompletionTokens
			event.TokensTotal = usage.TotalTokens
			event.CostUSD = cost
			readSkillName := ""
			if err == nil && tc.Name == "read_skill" {
				readSkillName = skillNameFromArgs(tc.Arguments)
			}
			terminalTool := ""
			if b.terminalToolPolicyEnabled() && toolAllowed(tc.Name, loopPolicy.TerminalTools) {
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
			hooks.AfterToolCall(event)
		}(i, tc)
	}
	wg.Wait()

	for _, r := range results {
		convCtx.AddToolResultMessage(r.id, r.content)
		summary.lastResult = r.content
		if r.hiddenRejected {
			summary.hiddenRejected = true
		}
		if r.skillPreflightRejected {
			summary.skillPreflightRejected = true
		}
		if r.fatal && summary.fatalResult == "" {
			summary.fatalResult = r.content
		}
		if r.readSkillName != "" {
			summary.readSkillNames = appendUniqueStrings(summary.readSkillNames, r.readSkillName)
		}
		if summary.terminalTool == "" && r.terminalTool != "" {
			summary.terminalTool = r.terminalTool
		}
	}
	return summary
}

func (b *Bot) finalizeSwarmWithNoToolLLM(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, placeholder *tele.Message, rawSwarmResult string, stats *turnStats) (string, bool) {
	req := llm.Request{
		Messages: swarmFinalizationMessages(convCtx.Messages()),
		Model:    b.cfg.LLMModel,
		Tools:    nil,
	}
	stats.llmCalls++
	ch, err := b.llm.Stream(ctx, req)
	if err != nil {
		b.logger.Warn("swarm finalization LLM failed", "user_id", userID, "error", err)
		response := formatTerminalSwarmResult(rawSwarmResult)
		convCtx.AddAssistantMessage(response)
		return response, false
	}
	resp, delivered, err := b.consumeStream(c, ch, userID, placeholder)
	if err != nil {
		b.logger.Warn("swarm finalization stream failed", "user_id", userID, "error", err)
		response := formatTerminalSwarmResult(rawSwarmResult)
		convCtx.AddAssistantMessage(response)
		return response, false
	}
	convCtx.TrackTokens(resp.Usage)
	stats.tokensPrompt += resp.Usage.PromptTokens
	stats.tokensCompletion += resp.Usage.CompletionTokens
	stats.tokensTotal += resp.Usage.TotalTokens
	stats.costUSD += estimateUsageCost(resp.Usage, b.cfg.CostInputPerMTokens, b.cfg.CostOutputPerMTokens)
	if b.budget != nil {
		b.budget.RecordUsage(resp.Usage)
	}
	response := strings.TrimSpace(resp.Content)
	if response == "" {
		response = formatTerminalSwarmResult(rawSwarmResult)
	}
	convCtx.AddAssistantMessage(response)
	return response, delivered
}

func (b *Bot) finalizeTerminalToolWithNoToolLLM(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, placeholder *tele.Message, rawToolResult string, stats *turnStats) (string, bool) {
	req := llm.Request{
		Messages: terminalToolFinalizationMessages(convCtx.Messages(), stats.terminalTool),
		Model:    b.cfg.LLMModel,
		Tools:    nil,
	}
	stats.llmCalls++
	stats.loopSteps++
	resp, err := b.llm.Send(ctx, req)
	if err != nil {
		b.logger.Warn("terminal tool finalization LLM failed", "user_id", userID, "terminal_tool", stats.terminalTool, "error", err)
		response := terminalToolFallbackResponse(stats.terminalTool, rawToolResult)
		convCtx.AddAssistantMessage(response)
		return response, false
	}
	convCtx.TrackTokens(resp.Usage)
	stats.tokensPrompt += resp.Usage.PromptTokens
	stats.tokensCompletion += resp.Usage.CompletionTokens
	stats.tokensTotal += resp.Usage.TotalTokens
	stats.costUSD += estimateUsageCost(resp.Usage, b.cfg.CostInputPerMTokens, b.cfg.CostOutputPerMTokens)
	if b.budget != nil {
		b.budget.RecordUsage(resp.Usage)
	}
	response := strings.TrimSpace(resp.Content)
	if response == "" || resp.HasToolCalls || looksLikeToolCallMarkup(response) {
		response = terminalToolFallbackResponse(stats.terminalTool, rawToolResult)
	}
	convCtx.AddAssistantMessage(response)
	if c != nil {
		parts := renderForTelegramEntities(response)
		if len(parts) > 0 {
			if placeholder != nil {
				if _, err := b.editAssistantMessage(c.Bot(), placeholder, parts[0], response); err == nil {
					b.sendAssistantRemainder(c.Bot(), c.Recipient(), parts, 1)
					return response, true
				}
			}
			if sent, err := c.Bot().Send(c.Recipient(), parts[0].Text, parts[0].Entities); err == nil {
				b.sendAssistantRemainder(c.Bot(), c.Recipient(), parts, 1)
				_ = sent
				return response, true
			}
		}
	}
	return response, false
}

func terminalToolFinalizationMessages(messages []llm.Message, terminalTool string) []llm.Message {
	out := append([]llm.Message(nil), messages...)
	toolName := strings.TrimSpace(terminalTool)
	if toolName == "" {
		toolName = "the terminal tool"
	}
	out = append(out, llm.Message{
		Role:    "user",
		Content: fmt.Sprintf("The terminal tool %q completed. Do not call tools. Do not emit JSON, XML, DSML, or tool-call markup. Summarize the completed work for the user in their language using only the tool results already present above.", toolName),
	})
	return out
}

func looksLikeToolCallMarkup(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "tool_calls") ||
		strings.Contains(lower, "dsml") ||
		strings.Contains(lower, "invoke name=") ||
		strings.Contains(lower, "parameter name=") ||
		strings.Contains(lower, "<｜｜dsml｜｜") ||
		strings.Contains(lower, "<tool_call") ||
		strings.Contains(lower, `"tool_calls"`)
}

func looksLikeInternalToolResult(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "source_id") ||
		strings.Contains(lower, "tokens_prompt") ||
		strings.Contains(lower, "tokens_completion") ||
		strings.Contains(lower, "tokens_total") ||
		strings.Contains(lower, "llm_calls") ||
		strings.Contains(lower, "tool_calls") ||
		strings.Contains(lower, "elapsed_ms") ||
		strings.Contains(lower, "exit_code")
}

func terminalToolFallbackResponse(terminalTool, rawToolResult string) string {
	raw := strings.TrimSpace(rawToolResult)
	if raw != "" && !looksLikeToolCallMarkup(raw) && !looksLikeInternalToolResult(raw) {
		return raw
	}
	toolName := strings.TrimSpace(terminalTool)
	if toolName == "" {
		toolName = "the terminal tool"
	}
	if toolName == "write_wiki" {
		return "Fatto: ho aggiornato la wiki e ho fermato il turno dopo il salvataggio."
	}
	return fmt.Sprintf("Fatto: %s ha completato il lavoro.", toolName)
}

func swarmFinalizationMessages(messages []llm.Message) []llm.Message {
	out := append([]llm.Message(nil), messages...)
	out = append(out, llm.Message{
		Role:    "user",
		Content: "Synthesize the completed swarm result for the user. Do not call tools. Use only the aggregate tool result already present above.",
	})
	return out
}

func (b *Bot) storeOrchestrationSnapshot(userID string, stats turnStats) {
	if b == nil || strings.TrimSpace(userID) == "" {
		return
	}
	now := time.Now()
	b.orchMap.Store(userID, orchestrationSnapshot{
		StoredAt:            now,
		PromptVersion:       stats.promptVersion,
		PromptModules:       append([]string(nil), stats.promptModules...),
		PromptHash:          stats.promptHash,
		ToolProfile:         stats.toolProfile,
		ProfileSelectReason: stats.profileSelectReason,
		ToolsExposed:        append([]string(nil), stats.toolsExposed...),
		ToolsCalled:         append([]string(nil), stats.toolsCalled...),
		ActiveCapabilities:  append([]string(nil), stats.activeCapabilities...),
		ReadSkills:          append([]string(nil), stats.readSkills...),
		LoopSteps:           stats.loopSteps,
		HiddenToolRejected:  stats.hiddenToolRejected,
		SkillPreflightFail:  stats.skillPreflightFail,
		SkillsRead:          stats.skillsRead,
		SwarmUsed:           stats.swarmUsed,
		SandboxUsed:         stats.sandboxUsed,
		TerminalTool:        stats.terminalTool,
		TerminalSwarm:       stats.terminalSwarm,
		SwarmFinalization:   stats.swarmFinalization,
		PostSwarmToolCalls:  stats.postSwarmToolCalls,
		DuplicateSwarm:      stats.duplicateSwarm,
		WorkerCount:         stats.workerCount,
		WorkerFailures:      stats.workerFailures,
		TokensPrompt:        stats.tokensPrompt,
		TokensCompletion:    stats.tokensCompletion,
		TokensTotal:         stats.tokensTotal,
		CostUSD:             stats.costUSD,
	})
	b.pruneOrchestrationSnapshots(now)
}

func (b *Bot) loadOrchestrationSnapshot(userID string) (orchestrationSnapshot, bool) {
	if b == nil {
		return orchestrationSnapshot{}, false
	}
	value, ok := b.orchMap.Load(userID)
	if !ok {
		return orchestrationSnapshot{}, false
	}
	snap, ok := value.(orchestrationSnapshot)
	return snap, ok
}

func (b *Bot) pruneOrchestrationSnapshots(now time.Time) {
	if b == nil || b.cfg == nil || b.cfg.TraceRetentionDays <= 0 {
		return
	}
	cutoff := now.Add(-time.Duration(b.cfg.TraceRetentionDays) * 24 * time.Hour)
	b.orchMap.Range(func(key, value any) bool {
		snap, ok := value.(orchestrationSnapshot)
		if !ok {
			return true
		}
		if !snap.StoredAt.IsZero() && snap.StoredAt.Before(cutoff) {
			b.orchMap.Delete(key)
		}
		return true
	})
}

func toolDefinitionNames(defs []llm.ToolDefinition) []string {
	out := make([]string, 0, len(defs))
	for _, def := range defs {
		out = append(out, def.Name)
	}
	return out
}

func orderToolDefinitionsForAllowlist(defs []llm.ToolDefinition, allowlist []string) []llm.ToolDefinition {
	if len(defs) <= 1 || len(allowlist) == 0 {
		return defs
	}
	positions := make(map[string]int, len(allowlist))
	for i, name := range allowlist {
		if _, ok := positions[name]; !ok {
			positions[name] = i
		}
	}
	out := append([]llm.ToolDefinition(nil), defs...)
	sort.SliceStable(out, func(i, j int) bool {
		left, leftOK := positions[out[i].Name]
		right, rightOK := positions[out[j].Name]
		if !leftOK && !rightOK {
			return false
		}
		if !leftOK {
			return false
		}
		if !rightOK {
			return true
		}
		return left < right
	})
	return out
}

func latestUserMessage(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && strings.TrimSpace(messages[i].Content) != "" {
			return strings.TrimSpace(messages[i].Content)
		}
	}
	return "Complete the requested broad read-only Aura pipeline review."
}

func toolAllowed(name string, allowlist []string) bool {
	for _, allowed := range allowlist {
		if allowed == name {
			return true
		}
	}
	return false
}

func skillNameFromArgs(args map[string]any) string {
	value, ok := args["name"]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func capabilityNames(capabilities []orchestration.Capability) []string {
	out := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		out = append(out, string(capability))
	}
	return out
}

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, addition := range additions {
		addition = strings.TrimSpace(addition)
		if addition == "" {
			continue
		}
		if !stringSliceContains(values, addition) {
			values = append(values, addition)
		}
	}
	return values
}

func stringSliceContains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func toolCallsContain(calls []llm.ToolCall, name string) bool {
	for _, call := range calls {
		if call.Name == name {
			return true
		}
	}
	return false
}

func capDuplicateSwarmCalls(profile orchestration.Profile, calls []llm.ToolCall) ([]llm.ToolCall, []llm.ToolCall) {
	if profile != orchestration.ProfileSwarmResearch {
		return calls, nil
	}
	out := make([]llm.ToolCall, 0, len(calls))
	var duplicates []llm.ToolCall
	seenSwarm := false
	for _, call := range calls {
		if call.Name != "run_aurabot_swarm" {
			out = append(out, call)
			continue
		}
		if seenSwarm {
			duplicates = append(duplicates, call)
			continue
		}
		seenSwarm = true
		out = append(out, call)
	}
	return out, duplicates
}

func formatTerminalSwarmResult(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "Swarm research completed, but no synthesis was returned."
	}
	var resp struct {
		OK      bool   `json:"ok"`
		Status  string `json:"status"`
		Summary string `json:"summary"`
		Tasks   []struct {
			Role          string `json:"role"`
			Status        string `json:"status"`
			ResultPreview string `json:"result_preview"`
			LastError     string `json:"last_error"`
		} `json:"tasks"`
		Metrics struct {
			TotalTasks       int   `json:"total_tasks"`
			CompletedTasks   int   `json:"completed_tasks"`
			FailedTasks      int   `json:"failed_tasks"`
			LLMCalls         int   `json:"llm_calls"`
			ToolCalls        int   `json:"tool_calls"`
			TokensTotal      int   `json:"tokens_total"`
			WallMS           int64 `json:"wall_ms"`
			TaskElapsedMS    int64 `json:"task_elapsed_ms"`
			TokensPrompt     int   `json:"tokens_prompt"`
			TokensCompletion int   `json:"tokens_completion"`
		} `json:"metrics"`
		LastError string `json:"last_error"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return "Ho completato la verifica parallela, ma non sono riuscito a trasformare il risultato interno in un riassunto pulito."
	}
	var sb strings.Builder
	taskSummary := terminalSwarmTaskSummary(resp.Tasks)
	if taskSummary != "" && isTechnicalSwarmSummary(resp.Summary) {
		sb.WriteString(taskSummary)
	} else if strings.TrimSpace(resp.Summary) != "" {
		sb.WriteString(strings.TrimSpace(resp.Summary))
		if taskSummary != "" {
			sb.WriteString("\n\n")
			sb.WriteString(taskSummary)
		}
	} else if resp.LastError != "" {
		sb.WriteString("Swarm research finished with an error: ")
		sb.WriteString(resp.LastError)
	} else if taskSummary != "" {
		sb.WriteString(taskSummary)
	} else {
		sb.WriteString("Swarm research completed.")
	}
	if resp.Metrics.TotalTasks > 0 {
		fmt.Fprintf(&sb, "\n\nHo completato la verifica parallela: %d/%d controlli riusciti",
			resp.Metrics.CompletedTasks,
			resp.Metrics.TotalTasks,
		)
		if resp.Metrics.FailedTasks > 0 {
			fmt.Fprintf(&sb, ", %d da rivedere", resp.Metrics.FailedTasks)
		}
		sb.WriteString(".")
	}
	return sb.String()
}

func terminalSwarmTaskSummary(tasks []struct {
	Role          string `json:"role"`
	Status        string `json:"status"`
	ResultPreview string `json:"result_preview"`
	LastError     string `json:"last_error"`
}) string {
	var lines []string
	for _, task := range tasks {
		body := strings.TrimSpace(task.ResultPreview)
		if body == "" {
			body = strings.TrimSpace(task.LastError)
		}
		if body == "" {
			continue
		}
		role := strings.TrimSpace(task.Role)
		if role != "" && len(tasks) > 1 {
			body = role + ": " + body
		}
		lines = append(lines, body)
	}
	return strings.Join(lines, "\n\n")
}

func isTechnicalSwarmSummary(summary string) bool {
	summary = strings.TrimSpace(summary)
	return strings.HasPrefix(summary, "Run ") && strings.Contains(summary, " Metrics:")
}

func formatTerminalExecuteCodeResult(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "Sandbox execution completed, but no result was returned."
	}
	body := raw
	if idx := strings.Index(body, "\n\n"); idx >= 0 {
		body = strings.TrimSpace(body[idx+2:])
	}
	artifacts := ""
	if idx := strings.Index(body, "\n\nartifacts:"); idx >= 0 {
		artifacts = strings.TrimSpace(body[idx+len("\n\nartifacts:"):])
		body = strings.TrimSpace(body[:idx])
	}
	if body == "" {
		body = "Sandbox execution completed."
	}
	if artifacts == "" {
		return body
	}
	names := artifactNamesFromSandboxResult(artifacts)
	if len(names) == 0 {
		return body + "\n\nHo generato gli allegati richiesti."
	}
	return body + "\n\nFile generati: " + strings.Join(names, ", ") + "."
}

func isFileGenerationTool(name string) bool {
	switch name {
	case "create_docx", "create_xlsx", "create_pdf":
		return true
	default:
		return false
	}
}

func formatTerminalFileResult(toolName, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "File created, but no metadata was returned."
	}
	var resp struct {
		SourceID  string `json:"source_id"`
		Filename  string `json:"filename"`
		SizeBytes int64  `json:"size_bytes"`
		Delivered bool   `json:"delivered"`
		Duplicate bool   `json:"duplicate"`
	}
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		return "File creato e salvato."
	}
	kind := strings.TrimPrefix(toolName, "create_")
	if kind == "" {
		kind = "file"
	}
	var sb strings.Builder
	if strings.TrimSpace(resp.Filename) != "" {
		fmt.Fprintf(&sb, "Ho creato il file %s `%s`", strings.ToUpper(kind), resp.Filename)
	} else {
		fmt.Fprintf(&sb, "Ho creato il file %s", strings.ToUpper(kind))
	}
	if resp.SizeBytes > 0 {
		fmt.Fprintf(&sb, " (%d bytes)", resp.SizeBytes)
	}
	if resp.Delivered {
		sb.WriteString(" e l'ho inviato qui")
	}
	if resp.Duplicate {
		sb.WriteString(" (era gia' presente)")
	}
	sb.WriteString(".")
	return sb.String()
}

func swarmResearchFinalization() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("SWARM_RESEARCH_FINALIZATION")))
	if mode == "no_tool_llm" {
		return mode
	}
	return "aggregate"
}

func (s *turnStats) applySwarmMetrics(raw string, inputPerM, outputPerM float64) {
	if s == nil {
		return
	}
	var resp struct {
		Metrics struct {
			LLMCalls         int `json:"llm_calls"`
			ToolCalls        int `json:"tool_calls"`
			TotalTasks       int `json:"total_tasks"`
			FailedTasks      int `json:"failed_tasks"`
			RunningTasks     int `json:"running_tasks"`
			PendingTasks     int `json:"pending_tasks"`
			TokensPrompt     int `json:"tokens_prompt"`
			TokensCompletion int `json:"tokens_completion"`
			TokensTotal      int `json:"tokens_total"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &resp); err != nil {
		return
	}
	s.llmCalls += resp.Metrics.LLMCalls
	s.toolCalls += resp.Metrics.ToolCalls
	s.workerCount = resp.Metrics.TotalTasks
	s.workerFailures = resp.Metrics.FailedTasks + resp.Metrics.RunningTasks + resp.Metrics.PendingTasks
	s.tokensPrompt += resp.Metrics.TokensPrompt
	s.tokensCompletion += resp.Metrics.TokensCompletion
	s.tokensTotal += resp.Metrics.TokensTotal
	s.costUSD += estimateUsageCost(llm.TokenUsage{
		PromptTokens:     resp.Metrics.TokensPrompt,
		CompletionTokens: resp.Metrics.TokensCompletion,
		TotalTokens:      resp.Metrics.TokensTotal,
	}, inputPerM, outputPerM)
}

func toolResultUsage(raw string, cfg *config.Config) (llm.TokenUsage, float64) {
	var resp struct {
		Metrics struct {
			TokensPrompt     int `json:"tokens_prompt"`
			TokensCompletion int `json:"tokens_completion"`
			TokensTotal      int `json:"tokens_total"`
		} `json:"metrics"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &resp); err != nil {
		return llm.TokenUsage{}, 0
	}
	usage := llm.TokenUsage{
		PromptTokens:     resp.Metrics.TokensPrompt,
		CompletionTokens: resp.Metrics.TokensCompletion,
		TotalTokens:      resp.Metrics.TokensTotal,
	}
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 {
		return llm.TokenUsage{}, 0
	}
	var inputPerM, outputPerM float64
	if cfg != nil {
		inputPerM = cfg.CostInputPerMTokens
		outputPerM = cfg.CostOutputPerMTokens
	}
	return usage, estimateUsageCost(usage, inputPerM, outputPerM)
}

func userFacingFatalToolResult(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.Contains(trimmed, "not exposed in the active tool profile") {
		if strings.Contains(trimmed, "write_wiki") {
			return "Non posso scrivere direttamente nella memoria con il profilo attivo. La memoria del turno resta gestita dalla cattura automatica post-turn e, se non e' low-risk, dalla coda review."
		}
		return "Una capacita' interna richiesta non e' disponibile nel profilo attivo. Ho fermato l'azione invece di usare un permesso non esposto."
	}
	return raw
}

func estimateUsageCost(usage llm.TokenUsage, inputPerM, outputPerM float64) float64 {
	prompt := usage.PromptTokens
	completion := usage.CompletionTokens
	if prompt == 0 && completion == 0 {
		prompt = usage.TotalTokens
	}
	return (float64(prompt)*inputPerM + float64(completion)*outputPerM) / 1_000_000
}

func toolActivityMessage(name string) string {
	return "Sto lavorando alla richiesta..."
}

func artifactNamesFromSandboxResult(artifacts string) []string {
	var names []string
	for _, line := range strings.Split(artifacts, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if line == "" {
			continue
		}
		if idx := strings.Index(line, " ("); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line != "" {
			names = append(names, line)
		}
	}
	return names
}
