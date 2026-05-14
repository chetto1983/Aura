package telegram

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	auraskills "github.com/aura/aura/internal/skills"
	tools "github.com/aura/aura/internal/agent/tools/registry"

	tele "gopkg.in/telebot.v4"
)

// channelDataKeyTeleContext is the key under which the inbound adapter stashes
// the tele.Context in InboundMessage.ChannelData. Mirror of
// internal/channels/telegram.ChannelDataKeyContext — duplicated here to avoid an
// import cycle (channels/telegram already imports internal/telegram).
const channelDataKeyTeleContext = "tele_context"

// checkBudgetQuota sends a Telegram budget notice and returns non-nil if the LLM call
// should be halted. The caller is responsible for session cleanup before returning.
func (b *Bot) checkBudgetQuota(c tele.Context, userID string, estimatedTokens int) error {
	if b.rt == nil || b.rt.budget == nil {
		return nil
	}
	if b.rt.budget.IsHardBudgetExceeded() {
		b.logger.Warn("hard budget exceeded, halting LLM call", "user_id", userID)
		if _, err := c.Bot().Send(c.Recipient(), "Budget limit reached. LLM calls are temporarily halted."); err != nil {
			b.logger.Warn("budget notice send failed", "error", err)
		}
		return fmt.Errorf("hard budget exceeded")
	}
	if !b.rt.budget.CanAfford(estimatedTokens, 500) {
		b.logger.Warn("predicted cost exceeds hard budget, halting LLM call", "user_id", userID)
		if _, err := c.Bot().Send(c.Recipient(), "Predicted cost would exceed budget. Please adjust your budget or wait."); err != nil {
			b.logger.Warn("budget notice send failed", "error", err)
		}
		return fmt.Errorf("budget unaffordable")
	}
	return nil
}

// buildTelegramInvocation is the chat.InvocationBuilder for the Telegram channel.
// It extracts the tele.Context from msg.ChannelData, creates a full session,
// assembles the system prompt and tool pool, sends the initial "⏳" placeholder,
// and returns the wired agent.Invocation.
func (b *Bot) buildTelegramInvocation(ctx context.Context, run *chat.Run, msg chat.InboundMessage) (agent.Invocation, error) {
	c, ok := msg.ChannelData[channelDataKeyTeleContext].(tele.Context)
	if !ok {
		return agent.Invocation{}, fmt.Errorf("buildTelegramInvocation: no tele.Context in ChannelData")
	}
	userID := msg.PrincipalID
	turnStart := time.Now()

	var summarizer llm.Client
	if b.rt != nil {
		summarizer = b.rt.llm
	}
	session, _ := b.sessionStore().Begin(userID, conversation.Config{
		MaxTokens:   b.cfg.MaxContextTokens,
		MaxMessages: b.cfg.MaxHistoryMessages,
		Summarizer:  summarizer,
		Logger:      b.logger,
	})
	convCtx := session.Conversation()
	userText := msg.Text

	overlay := conversation.LoadPromptOverlay(b.cfg.PromptOverlayPath)
	var skillsBlock string
	if b.rt != nil && b.rt.skills != nil {
		loadedSkills, err := b.rt.skills.LoadAll()
		if err != nil {
			b.logger.Warn("failed to load local skills", "error", err)
		} else if block := auraskills.PromptBlock(loadedSkills); block != "" {
			skillsBlock = block
		}
	}
	toolAllowlist := b.modelToolNames()
	toolManifest := tools.RenderToolManifest(b.rt.tools.Definitions())
	promptPlan := agent.ComposeAgentPrompt(b.cfg, b.loc, overlay, skillsBlock, toolManifest, time.Now())
	convCtx.SetSystemMessage(promptPlan.Content)

	b.logger.Info("conversation started",
		"user_id", userID,
		"username", c.Sender().Username,
		"message", userText,
	)

	convCtx.AddUserMessage(userText)
	convCtx.SetSearchContext("")
	preLoopIdx := convCtx.MessageCount()

	// Echo mode when no LLM is configured.
	if b.rt == nil || b.rt.llm == nil {
		echo := "Echo: " + userText
		if _, err := c.Bot().Send(c.Recipient(), echo); err != nil {
			b.logger.Error("failed to send echo", "user_id", userID, "error", err)
		}
		convCtx.AddAssistantMessage(echo)
		session.Finish()
		return agent.Invocation{}, fmt.Errorf("no LLM configured (echo mode)")
	}

	if err := b.checkBudgetQuota(c, userID, convCtx.EstimatedTokens()); err != nil {
		session.Finish()
		return agent.Invocation{}, err
	}

	// Send the initial placeholder so the user knows the message was received.
	placeholder, _ := c.Bot().Send(c.Recipient(), "⏳")

	chatClient := &telegramHubChatClient{
		b:           b,
		teleCtx:     c,
		userID:      userID,
		placeholder: placeholder,
	}

	maxIterations := b.maxToolLoopIterations()
	maxCallsPerTool := map[string]int{"wiki_page": 3}
	duplicatePolicy := agent.DuplicateOrMaxCallsPolicy(maxCallsPerTool, nil)

	toolsProvider := makeToolsProvider(
		alwaysOnCore,
		b.rt.tools.Search,
		b.rt.tools.DefinitionsFor,
		b.rt.tools.Definitions,
		convCtx.LatestUserMessageText,
		func() int { return b.cfg.ToolSearchTopK },
		b.logger,
	)
	toolDefs := toolsProvider()
	baseStats := agent.TurnStats{
		PromptVersion:       promptPlan.Version,
		PromptModules:       append([]string(nil), promptPlan.Modules...),
		PromptHash:          promptPlan.Hash,
		Toolset:             "registered",
		ToolsetSelectReason: "core tools plus Qdrant top-K=5 retrieval",
		ToolsExposed:        toolDefinitionNames(toolDefs),
	}

	var currentStats agent.TurnStats
	var toolMu sync.Mutex
	activeToolNames := append([]string(nil), toolAllowlist...)
	currentToolNames := func() []string {
		toolMu.Lock()
		defer toolMu.Unlock()
		return append([]string(nil), activeToolNames...)
	}
	addActiveTools := func(names []string) {
		toolMu.Lock()
		defer toolMu.Unlock()
		activeToolNames = appendUniqueStrings(activeToolNames, names...)
	}

	inv := agent.Invocation{
		Client: chatClient,
		Executor: agent.ToolExecutorFunc(func(ctx context.Context, calls []llm.ToolCall) agent.ExecutionSummary {
			execution := b.executeToolCalls(ctx, c, convCtx, userID, calls, currentToolNames(), currentStats.ReadSkills)
			if len(execution.DiscoveredTools) > 0 {
				addActiveTools(execution.DiscoveredTools)
			}
			return agent.ExecutionSummary{
				LastResult:     execution.LastResult,
				FatalResult:    execution.FatalResult,
				ReadSkillNames: execution.ReadSkillNames,
				TerminalTool:   execution.TerminalTool,
				Results:        execution.Results,
			}
		}),
		State:                   convCtx,
		PromptVersion:           promptPlan.Version,
		PromptHash:              promptPlan.Hash,
		PromptModules:           promptPlan.Modules,
		Toolset:                 "registered",
		ToolsetSelectReason:     "core tools plus Qdrant top-K=5 retrieval",
		Tools:                   toolDefs,
		ToolsProvider:           toolsProvider,
		RetrievalCapsulePresent: false,
		Options: agent.Options{
			MaxIterations:           maxIterations,
			TerminalToolPolicy:      b.terminalToolPolicyEnabled(),
			AllowNoToolFinalization: true,
			MaxToolResultChars:      b.cfg.MaxToolResultChars,
			MicrocompactKeepRecent:  b.cfg.MicrocompactKeepRecent,
			MicrocompactMinChars:    b.cfg.MicrocompactMinChars,
			BeforeTool:              duplicatePolicy,
			ToolResolver:            b.rt.tools.DefinitionFor,
			PhantomToolGuard: &agent.PhantomToolGuard{
				ToolNamesFn: b.rt.tools.Names,
			},
			BeforeLLM: func() (string, bool) {
				if b.rt != nil && b.rt.budget != nil && b.rt.budget.IsHardBudgetExceeded() {
					b.logger.Warn("hard budget exceeded during tool loop", "user_id", userID)
					return "Budget limit reached. LLM calls are temporarily halted.", true
				}
				return "", false
			},
			RecordUsage: func(usage llm.TokenUsage) {
				if b.rt != nil && b.rt.budget != nil {
					b.rt.budget.RecordUsage(usage)
				}
			},
			EstimateCost: func(usage llm.TokenUsage) float64 {
				return estimateUsageCost(usage, b.cfg.CostInputPerMTokens, b.cfg.CostOutputPerMTokens)
			},
			TerminalHandler: func(ctx context.Context, terminalTool, lastToolResult string, stats *agent.Stats) (string, bool, bool) {
				telegramStats := baseStats.From(*stats)
				response, delivered := b.finalizeTerminalToolWithNoToolLLM(ctx, c, convCtx, userID, placeholder, lastToolResult, &telegramStats)
				telegramStats.ApplyTo(stats)
				if delivered {
					return "", true, true
				}
				return response, false, true
			},
			MaxCallsPerTool: maxCallsPerTool,
		},
		OnEvent: func(event agent.Event) {
			switch event.Type {
			case agent.EventStats:
				currentStats = baseStats.From(event.Stats)
				b.storeOrchestrationSnapshot(userID, currentStats)
			case agent.EventFinal:
				finalStats := baseStats.From(event.Stats)
				currentStats = finalStats

				// Non-streaming delivery: edit placeholder or send fresh.
				if !event.Delivered {
					finalText := strings.TrimSpace(event.Text)
					if placeholder != nil {
						if finalText != "" {
							parts := renderForTelegramEntities(finalText)
							if len(parts) > 0 {
								if _, err := b.editAssistantMessage(c.Bot(), placeholder, parts[0], finalText); err == nil {
									b.sendAssistantRemainder(c.Bot(), c.Recipient(), parts, 1)
								} else {
									if delErr := c.Bot().Delete(placeholder); delErr != nil {
										logPlaceholderDeleteFailure(b.logger, userID, placeholder, delErr)
									}
									b.sendAssistant(c, finalText)
								}
							}
						} else {
							if delErr := c.Bot().Delete(placeholder); delErr != nil {
								logPlaceholderDeleteFailure(b.logger, userID, placeholder, delErr)
							}
						}
					} else if finalText != "" {
						b.sendAssistant(c, finalText)
					}
				}

				if event.Stats.LLMCalls > 0 && event.Stats.TerminalTool == "" {
					b.notifySoftBudget(c, userID)
				}

				// Archive this turn.
				if b.rt != nil && b.rt.archiver != nil && b.rt.archiveDB != nil {
					archiveCtx := context.Background()
					chatID := c.Chat().ID
					nextIdx := int64(0)
					if maxIdx, err := b.rt.archiveDB.MaxTurnIndex(archiveCtx, chatID); err == nil {
						nextIdx = maxIdx + 1
					} else {
						b.logger.Warn("archive: max turn_index lookup failed", "chat_id", chatID, "error", err)
					}
					archiveConversationTurns(archiveCtx, b.logger, b.archiveAppenderForTurn(), archiveTurnInput{
						ChatID:       chatID,
						UserID:       c.Sender().ID,
						NextIndex:    nextIdx,
						UserText:     userText,
						LoopMessages: convCtx.MessagesSince(preLoopIdx),
						Stats:        finalStats,
						ElapsedMS:    time.Since(turnStart).Milliseconds(),
						TokensIn:     convCtx.TotalTokensUsed(),
					})
				}

				compactedToolResults := convCtx.CompactCompletedToolResults(conversation.ToolResultCompactionPolicy{
					MaxChars:       1200,
					KeepRecentFull: 2,
				})
				if compactedToolResults > 0 {
					b.logger.Info("conversation tool results compacted", "user_id", userID, "count", compactedToolResults)
				}
				go func() {
					if err := convCtx.EnforceLimit(context.Background()); err != nil {
						b.logger.Error("context enforcement failed", "user_id", userID, "error", err)
					}
				}()

				b.logger.Info("conversation complete",
					"user_id", userID,
					"tokens_lifetime", convCtx.TotalTokensUsed(),
					"elapsed_ms", time.Since(turnStart).Milliseconds(),
					"llm_calls", finalStats.LLMCalls,
					"tool_calls", finalStats.ToolCalls,
					"loop_steps", finalStats.LoopSteps,
					"prompt_version", finalStats.PromptVersion,
					"prompt_hash", finalStats.PromptHash,
					"prompt_modules", strings.Join(finalStats.PromptModules, ","),
					"toolset", finalStats.Toolset,
					"toolset_select_reason", finalStats.ToolsetSelectReason,
					"tools_exposed", strings.Join(finalStats.ToolsExposed, ","),
					"tools_called", strings.Join(finalStats.ToolsCalled, ","),
					"read_skills", strings.Join(finalStats.ReadSkills, ","),
					"skills_read", finalStats.SkillsRead,
					"swarm_used", finalStats.SwarmUsed,
					"sandbox_used", finalStats.SandboxUsed,
					"terminal_tool", finalStats.TerminalTool,
					"duplicate_tool_rejected", finalStats.DuplicateToolCall,
					"tokens_prompt", finalStats.TokensPrompt,
					"tokens_completion", finalStats.TokensCompletion,
					"tokens_total", finalStats.TokensTotal,
					"cost_usd", fmt.Sprintf("%.6f", finalStats.CostUSD),
				)

				session.Finish()
			}
		},
	}
	return inv, nil
}

// executeToolCalls runs the LLM's tool calls concurrently and appends results
// in original order.
func (b *Bot) executeToolCalls(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, calls []llm.ToolCall, toolsExposed []string, readSkills []string) agent.ToolExecutionSummary {
	if len(calls) == 0 {
		return agent.ToolExecutionSummary{}
	}

	type outcome struct {
		id            string
		tool          string
		content       string
		fatal         bool
		readSkillName string
		terminalTool  string
	}
	results := make([]outcome, len(calls))

	var wg sync.WaitGroup
	for i, tc := range calls {
		wg.Add(1)
		go func(i int, tc llm.ToolCall) {
			defer wg.Done()
			toolCtx := tools.WithAllowedToolNames(tools.WithUserID(ctx, userID), b.rt.tools.Names())
			args := agent.ToolArgumentsForTool(tc.Name, tc.Arguments, chatIDFromTeleContext(c))
			result, err := b.rt.tools.Execute(toolCtx, tc.Name, args)
			if err != nil {
				result = tools.FormatToolError(err)
				b.logger.Warn("tool call failed", "user_id", userID, "tool", tc.Name, "error", err)
			}
			readSkillName := ""
			if err == nil && tc.Name == "read_file" {
				readSkillName = agent.SkillNameFromReadFileArgs(tc.Arguments)
			}
			terminalTool := ""
			if err == nil && b.terminalToolPolicyEnabled() && agent.IsTerminalTool(tc.Name) {
				terminalTool = tc.Name
			}
			results[i] = outcome{
				id:            tc.ID,
				tool:          tc.Name,
				content:       result,
				readSkillName: readSkillName,
				terminalTool:  terminalTool,
			}
		}(i, tc)
	}
	wg.Wait()

	summary := agent.ToolExecutionSummary{Results: make(map[string]string, len(results))}
	for _, r := range results {
		wrapped := agent.WrapUntrustedToolResult(r.tool, r.content)
		convCtx.AddToolResultMessage(r.id, wrapped)
		summary.LastResult = r.content
		summary.Results[r.id] = r.content
		if r.fatal && summary.FatalResult == "" {
			summary.FatalResult = r.content
		}
		if r.readSkillName != "" {
			summary.ReadSkillNames = appendUniqueStrings(summary.ReadSkillNames, r.readSkillName)
		}
		if summary.TerminalTool == "" && r.terminalTool != "" {
			summary.TerminalTool = r.terminalTool
		}
	}
	return summary
}

func (b *Bot) modelToolNames() []string {
	if b == nil || b.rt == nil || b.rt.tools == nil {
		return nil
	}
	return b.rt.tools.Names()
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
