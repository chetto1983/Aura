package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/agentloop"
	"github.com/aura/aura/internal/agentruntime"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/tools"

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

	// Build the versioned prompt and model-visible tool surface fresh on every
	// turn so runtime time, overlays, skills, and registered tools stay accurate
	// without restarting the bot.
	overlay := conversation.LoadPromptOverlay(b.cfg.PromptOverlayPath)
	var skillsBlock string
	// Slice 11q/06: read SOUL.md / USER.md / TOOLS.md from the
	// configured overlay dir. Picobot pattern: lets the operator tune
	// personality, durable user facts, and tool guidance by file; the next user
	// turn picks up the change with no recompile or restart. AGENT.md and
	// AGENTS.md stay file-readable only and are not injected into Aura's prompt.
	if b.skills != nil {
		loadedSkills, err := b.skills.LoadAll()
		if err != nil {
			b.logger.Warn("failed to load local skills", "error", err)
		} else if block := auraskills.PromptBlock(loadedSkills); block != "" {
			skillsBlock = block
		}
	}
	retrievalCapsule := turnRetrievalCapsule{}
	toolAllowlist := b.modelToolNames()
	toolManifest := tools.RenderToolManifest(b.tools.Definitions())
	promptPlan := composeAgentPrompt(b.cfg, b.loc, overlay, skillsBlock, toolManifest, time.Now())
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

	response, stats := b.runToolCallingLoop(context.Background(), c, convCtx, userID, placeholder, toolAllowlist, promptPlan, strings.TrimSpace(retrievalCapsule.Text) != "")
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

	compactedToolResults := convCtx.CompactCompletedToolResults(conversation.ToolResultCompactionPolicy{
		MaxChars:       1200,
		KeepRecentFull: 2,
	})
	if compactedToolResults > 0 {
		b.logger.Info("conversation tool results compacted",
			"user_id", userID,
			"count", compactedToolResults,
		)
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
		"tokens_lifetime", convCtx.TotalTokensUsed(),
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

// turnRetrievalCapsule carries the per-turn retrieval context text injected
// into the system message. The zero value represents an empty (no-retrieval)
// capsule, which keeps the retrieval slot clear for generic turns.
type turnRetrievalCapsule struct {
	Text string
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

type agentPromptPlan struct {
	Content string
	Version string
	Hash    string
	Modules []string
}

func composeAgentPrompt(cfg *config.Config, loc *time.Location, overlay, skillsBlock, toolManifest string, now time.Time) agentPromptPlan {
	version := "aura-agent-v1"
	if cfg != nil && strings.TrimSpace(cfg.PromptVersion) != "" {
		version = strings.TrimSpace(cfg.PromptVersion)
	}
	modules := []string{"base", "runtime", "registered-tools"}
	content := conversation.RenderSystemPrompt(now, loc)
	content += fmt.Sprintf("\n\n## Aura Runtime\n- Prompt Version: %s\n- Tool Discovery: the catalog below lists every tool you have. Call tool_search to fetch input schemas, OR invoke any tool by name and the agentloop will load its schema for this turn.\n\nChoose tools autonomously when they help. For multi-step work, prefer execute_code or execute_shell to inspect, loop, transform, and verify in one runtime pass instead of asking for many model tool-call rounds. Prefer direct answers when no tool is needed.", version)
	if strings.TrimSpace(overlay) != "" {
		content += "\n\n" + strings.TrimSpace(overlay)
		modules = append(modules, "overlay")
	}
	if strings.TrimSpace(skillsBlock) != "" {
		content += "\n\n" + strings.TrimSpace(skillsBlock)
		content += "\n\n## Skill Use\nSkills are optional operating procedures. Read a skill when the user names it or when it materially improves the work."
		modules = append(modules, "skills")
	}
	if strings.TrimSpace(toolManifest) != "" {
		content += "\n\n" + strings.TrimSpace(toolManifest)
		modules = append(modules, "tool-manifest")
	}
	sum := sha256.Sum256([]byte(content))
	return agentPromptPlan{
		Content: content,
		Version: version,
		Hash:    hex.EncodeToString(sum[:8]),
		Modules: modules,
	}
}

func (b *Bot) runToolCallingLoop(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, placeholder *tele.Message, toolAllowlist []string, promptPlan agentPromptPlan, retrievalCapsulePresent bool) (string, turnStats) {
	maxIterations := b.maxToolLoopIterations()
	var toolMu sync.Mutex
	activeToolNames := append([]string(nil), toolAllowlist...)
	currentToolNames := func() []string {
		toolMu.Lock()
		defer toolMu.Unlock()
		return append([]string(nil), activeToolNames...)
	}
	// Phase 2 D-22..D-28: per-turn ToolsProvider via the testable
	// makeToolsProvider helper. WARNING 4 of 2026-05-11 plan revision 2: the
	// helper lives in tools_provider.go (sibling file) to keep conversation.go
	// below the 600 LOC god-class ceiling. WARNING 5 of 2026-05-11 plan
	// revision 2: function-typed deps avoid extracting an interface from
	// *tools.Registry; tests pass call-counting stubs.
	toolsProvider := makeToolsProvider(
		alwaysOnCore,
		b.tools.Search,                // searchFn — TWO-arg + variadic per registry_search.go:19
		b.tools.DefinitionsFor,        // defsForFn
		b.tools.Definitions,           // defsAllFn (FULL toolset fallback)
		convCtx.LatestUserMessageText, // latestUserMsgFn (Task 1 output)
		func() int { return b.cfg.ToolSearchTopK },
		b.logger,
	)
	// MaxCallsPerTool used to cap search_memory to 2 per turn — which
	// produced the 5-stub duplicate-skip loops the user flagged on
	// 2026-05-11 ("fa troppe call memory search ... lo stiamo
	// lobotomizzando"). The cap was treating legitimate model
	// behaviour as misuse; the model knows when it needs more
	// retrieval. Only wiki_page keeps an explicit budget because its
	// retry path is structured around the 3-temperature staircase
	// (Plan 01 D-07: ContentTemperatures[0, 0.3, 0.7]).
	maxCallsPerTool := map[string]int{
		"wiki_page": 3,
	}
	duplicatePolicy := agentloop.DuplicateOrMaxCallsPolicy(maxCallsPerTool, nil)
	addActiveTools := func(names []string) {
		toolMu.Lock()
		defer toolMu.Unlock()
		activeToolNames = appendUniqueStrings(activeToolNames, names...)
	}
	baseStats := turnStats{
		promptVersion:           promptPlan.Version,
		promptModules:           append([]string(nil), promptPlan.Modules...),
		promptHash:              promptPlan.Hash,
		toolset:                 "registered",
		toolsetSelectReason:     "core tools plus Qdrant top-K=5 retrieval",
		retrievalCapsulePresent: retrievalCapsulePresent,
	}
	toolDefs := toolsProvider()
	baseStats.toolsExposed = toolDefinitionNames(toolDefs)
	var currentStats turnStats
	result, err := agentruntime.Run(ctx, agentruntime.Invocation{
		Client: telegramLoopClient{bot: b, teleCtx: c, userID: userID, placeholder: placeholder},
		Executor: agentloop.ToolExecutorFunc(func(ctx context.Context, calls []llm.ToolCall) agentloop.ExecutionSummary {
			execution := b.executeToolCalls(ctx, c, convCtx, userID, calls, currentToolNames(), currentStats.readSkills)
			if len(execution.discoveredTools) > 0 {
				addActiveTools(execution.discoveredTools)
			}
			return agentloop.ExecutionSummary{
				LastResult:     execution.lastResult,
				FatalResult:    execution.fatalResult,
				ReadSkillNames: execution.readSkillNames,
				TerminalTool:   execution.terminalTool,
				Results:        execution.results,
			}
		}),
		State:                   convCtx,
		PromptVersion:           promptPlan.Version,
		PromptHash:              promptPlan.Hash,
		PromptModules:           promptPlan.Modules,
		Toolset:                 "registered",
		ToolsetSelectReason:     "core tools plus Qdrant top-K=5 retrieval",
		Tools:                   toolsProvider(), // one-time initial population for the agentruntime.Invocation
		ToolsProvider:           toolsProvider,
		RetrievalCapsulePresent: retrievalCapsulePresent,
		Options: agentloop.Options{
			MaxIterations:           maxIterations,
			TerminalToolPolicy:      b.terminalToolPolicyEnabled(),
			AllowNoToolFinalization: true,
			MaxToolResultChars:      b.cfg.MaxToolResultChars,
			MicrocompactKeepRecent:  b.cfg.MicrocompactKeepRecent,
			MicrocompactMinChars:    b.cfg.MicrocompactMinChars,
			BeforeTool:              duplicatePolicy,
			// Deferred-tools rollout: the agent pool starts from the
			// always-on seed (just tool_search). The model invokes a
			// deferred tool by name after seeing it in the system-prompt
			// manifest; the loop resolves the schema via this callback
			// and adds it to the pool. tool_search results are absorbed
			// the same way. See internal/agentloop/pool.go for details.
			ToolResolver: b.tools.DefinitionFor,
			// Wave 2.8b: phantom-tool guard. Detects no-tool replies
			// that mention a registered tool by name without invoking it
			// (the 2026-05-12 Wave 2.7b failure mode) and re-prompts the
			// model with a correction. The state passed to agentloop.Run
			// is *conversation.Context which implements PhantomCorrector
			// via its AddUserMessage method. ToolNamesFn reads the live
			// registry so MCP and dynamic tool additions are picked up
			// without a restart.
			PhantomToolGuard: &agentloop.PhantomToolGuard{
				ToolNamesFn: b.tools.Names,
			},
			BeforeLLM: func() (string, bool) {
				// Context bounding happens after the response. Re-enforcing on every
				// tool iteration can trigger a compression LLM call mid-response,
				// which both burns latency and degrades fidelity.
				// AgentLoopMaxSteps already caps growth within a single user turn.
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
				telegramStats := mergeAgentLoopStats(baseStats, *stats)
				// Every terminal-tool branch now routes through the LLM
				// synthesizer. No canned "Done." strings, no
				// per-tool reformatters — the agent should sound like a
				// copilot, not a script. finalizeTerminalToolWithNoToolLLM
				// already retries once on synthesis failure (empty / DSML /
				// tool_calls); if both attempts fail the helper returns the
				// LLM's text (possibly empty) and Telegram simply sends
				// nothing rather than a hardcoded sentence.
				response, delivered := b.finalizeTerminalToolWithNoToolLLM(ctx, c, convCtx, userID, placeholder, lastToolResult, &telegramStats)
				*stats = applyTelegramTerminalStats(*stats, telegramStats)
				if delivered {
					return "", true, true
				}
				return response, false, true
			},
			MaxCallsPerTool: maxCallsPerTool,
		},
		OnEvent: func(event agentruntime.Event) {
			switch event.Type {
			case agentruntime.EventStats:
				currentStats = mergeAgentLoopStats(baseStats, event.Stats)
				// toolsExposed reflects what the LLM saw at the first round
				// (set on baseStats from toolsProvider()) — do NOT overwrite
				// with the broader execution allowlist or the log becomes a
				// lie about what was actually in the prompt.
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
		Messages:        messages,
		Model:           c.bot.cfg.LLMModel,
		Tools:           tools,
		ReasoningEffort: c.bot.cfg.ReasoningEffort,
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

// modelToolNames returns every registered tool name. The runtime no longer
// hides tools from the LLM behind a "core + discovery" facade — that pattern
// generated repeated "tool not available in this runtime" errors when the
// model called something it remembered from prior turns. With the registry
// as the single source of truth, the prompt grows but tool-call rejections
// disappear.
func (b *Bot) modelToolNames() []string {
	if b == nil || b.tools == nil {
		return nil
	}
	return b.tools.Names()
}

func (b *Bot) maxToolLoopIterations() int {
	maxIterations := config.DefaultAgentLoopMaxSteps
	if b != nil && b.cfg != nil && b.cfg.AgentLoopMaxSteps > 0 {
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
