package telegramadapter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/agent"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	auraskills "github.com/aura/aura/internal/skills"
	tgtelegram "github.com/aura/aura/internal/telegram"

	tele "gopkg.in/telebot.v4"
)

// InvocationBuilder constructs agent.Invocation values for the Telegram channel.
// It keeps invocation construction beside the Telegram channel adapter so
// internal/telegram remains a thin channel wrapper.
type InvocationBuilder struct {
	b        *tgtelegram.Bot
	hub      *chat.Hub // set after hub creation; used for ask_user resume routing
	outbound *Outbound // canonical streaming path used by streamingChatClient
}

// NewInvocationBuilder wraps a *telegram.Bot for use by NewHub.
func NewInvocationBuilder(b *tgtelegram.Bot) *InvocationBuilder {
	return &InvocationBuilder{b: b}
}

// NewHub creates a chat.Hub wired with Telegram inbound/outbound adapters and
// this bot's InvocationBuilder while avoiding an internal/telegram ->
// internal/channels/telegram import cycle.
func NewHub(b *tgtelegram.Bot, logger *slog.Logger, lifecycle chat.LifecycleStore) (*chat.Hub, error) {
	ib := NewInvocationBuilder(b)
	adapter, err := chat.NewAgentLoopAdapter(ib.Build)
	if err != nil {
		return nil, err
	}
	hub, err := chat.New(chat.Config{Loop: adapter, LifecycleStore: lifecycle, Logger: logger})
	if err != nil {
		return nil, err
	}
	outbound := NewOutbound(logger)
	hub.RegisterInbound(New())
	hub.RegisterOutbound(outbound)
	ib.hub = hub // Build is lazy (called per-message), so wiring here is safe.
	ib.outbound = outbound
	return hub, nil
}

// Build is the chat.InvocationBuilder for the Telegram channel.
// Translated from Bot.buildTelegramInvocation (internal/telegram/invocation_builder.go).
func (ib *InvocationBuilder) Build(ctx context.Context, run *chat.Run, msg chat.InboundMessage) (agent.Invocation, error) {
	c, ok := msg.ChannelData[ChannelDataKeyContext].(tele.Context)
	if !ok {
		return agent.Invocation{}, fmt.Errorf("buildTelegramInvocation: no tele.Context in ChannelData")
	}
	b := ib.b
	cfg := b.Config()
	userID := msg.PrincipalID
	turnStart := time.Now()

	session, _ := b.SessionStore().Begin(userID, conversation.Config{
		MaxTokens:   cfg.MaxContextTokens,
		MaxMessages: cfg.MaxHistoryMessages,
		Summarizer:  b.LLMClient(),
		Logger:      b.Logger(),
	})
	convCtx := session.Conversation()
	userText := msg.Text

	overlay := conversation.LoadPromptOverlay(cfg.PromptOverlayPath)
	var skillsBlock string
	if loader := b.SkillsLoader(); loader != nil {
		loadedSkills, err := loader.LoadAll()
		if err != nil {
			b.Logger().Warn("failed to load local skills", "error", err)
		} else if block := auraskills.PromptBlock(loadedSkills); block != "" {
			skillsBlock = block
		}
	}
	toolAllowlist := ib.modelToolNames()
	toolReg := b.ToolRegistry()
	toolManifest := tools.RenderToolManifest(toolReg.Definitions())
	promptPlan := agent.ComposeAgentPrompt(cfg, b.TimeLocation(), overlay, skillsBlock, toolManifest, time.Now())
	convCtx.SetSystemMessage(promptPlan.Content)

	b.Logger().Info("conversation started",
		"user_id", userID,
		"username", c.Sender().Username,
		"message", userText,
	)

	// Resume detection: if this thread has a pending ask_user question, route
	// the inbound text as the tool_result instead of a new user message.
	addedUserInput := false
	if ib.hub != nil {
		if status, ok := ib.hub.ThreadRunStatus(msg.ThreadID); ok && status == chat.RunStatusWaitingForUser {
			if callID, options, _, ok2 := agent.PendingAskUserCall(convCtx.Messages()); ok2 {
				content, rejected, rejectMsg := parseAskUserReply(userText, options)
				if rejected {
					if _, sendErr := c.Bot().Send(c.Recipient(), rejectMsg); sendErr != nil {
						b.Logger().Warn("ask_user: failed to send reject message", "user_id", userID, "error", sendErr)
					}
					// Signal Hub to preserve WaitingForUser status on error.
					run.Status = chat.RunStatusWaitingForUser
					session.Finish()
					return agent.Invocation{}, fmt.Errorf("ask_user: out-of-range reply, question still pending")
				}
				convCtx.AddToolResultMessage(callID, content)
				addedUserInput = true
				b.Logger().Info("ask_user: resume with tool_result",
					"user_id", userID,
					"tool_call_id", callID,
				)
			}
		}
	}
	if !addedUserInput {
		convCtx.AddUserMessage(userText)
	}
	convCtx.SetSearchContext("")
	preLoopIdx := convCtx.MessageCount()

	// Echo mode when no LLM is configured.
	if b.LLMClient() == nil {
		echo := "Echo: " + userText
		if _, err := c.Bot().Send(c.Recipient(), echo); err != nil {
			b.Logger().Error("failed to send echo", "user_id", userID, "error", err)
		}
		convCtx.AddAssistantMessage(echo)
		session.Finish()
		return agent.Invocation{}, fmt.Errorf("no LLM configured (echo mode)")
	}

	if err := ib.checkBudgetQuota(c, userID, convCtx.EstimatedTokens()); err != nil {
		session.Finish()
		return agent.Invocation{}, err
	}

	// Send the initial placeholder so the user knows the message was received.
	placeholder, _ := c.Bot().Send(c.Recipient(), "⏳")

	// Route streaming through the canonical channels/telegram.Outbound.
	if ib.outbound == nil {
		ib.outbound = NewOutbound(b.Logger())
	}
	chatClient := newStreamingChatClient(b.LLMClient(), cfg.LLMModel, cfg.ReasoningEffort, ib.outbound, c, userID, placeholder)

	maxIterations := ib.maxToolLoopIterations()
	// No hardcoded per-tool caps — DuplicatePolicy alone protects against
	// repeated identical (name, args) calls. The model decides how many
	// distinct wiki/web/etc lookups a turn needs; the loop ceiling +
	// wall-clock + budget bound runaway behavior.
	maxCallsPerTool := map[string]int{}
	duplicatePolicy := agent.DuplicateOrMaxCallsPolicy(maxCallsPerTool, nil)

	toolsProvider := agent.MakeToolsProvider(
		agent.AlwaysOnCore,
		toolReg.Search,
		toolReg.DefinitionsFor,
		toolReg.Definitions,
		convCtx.LatestUserMessageText,
		func() int { return cfg.ToolSearchTopK },
		b.Logger(),
	)
	toolDefs := toolsProvider()
	baseStats := agent.TurnStats{
		PromptVersion:       promptPlan.Version,
		PromptModules:       append([]string(nil), promptPlan.Modules...),
		PromptHash:          promptPlan.Hash,
		Toolset:             "registered",
		ToolsetSelectReason: "core tools plus Qdrant top-K=5 retrieval",
		ToolsExposed:        agent.ToolDefinitionNames(toolDefs),
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
			execution := ib.executeToolCalls(ctx, c, convCtx, userID, calls, currentToolNames(), currentStats.ReadSkills)
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
			TerminalToolPolicy:      ib.terminalToolPolicyEnabled(),
			AllowNoToolFinalization: true,
			MaxToolResultChars:      cfg.MaxToolResultChars,
			MicrocompactKeepRecent:  cfg.MicrocompactKeepRecent,
			MicrocompactMinChars:    cfg.MicrocompactMinChars,
			BeforeTool:              duplicatePolicy,
			ToolResolver:            toolReg.DefinitionFor,
			PhantomToolGuard: &agent.PhantomToolGuard{
				ToolNamesFn: toolReg.Names,
			},
			BeforeLLM: func() (string, bool) {
				if bgt := b.BudgetRuntime(); bgt != nil && bgt.IsHardBudgetExceeded() {
					b.Logger().Warn("hard budget exceeded during tool loop", "user_id", userID)
					return "Budget limit reached. LLM calls are temporarily halted.", true
				}
				return "", false
			},
			RecordUsage: func(usage llm.TokenUsage) {
				if bgt := b.BudgetRuntime(); bgt != nil {
					bgt.RecordUsage(usage)
				}
			},
			EstimateCost: func(usage llm.TokenUsage) float64 {
				return agent.EstimateUsageCost(usage, cfg.CostInputPerMTokens, cfg.CostOutputPerMTokens)
			},
			TerminalHandler: func(ctx context.Context, terminalTool, lastToolResult string, stats *agent.Stats) (string, bool, bool) {
				telegramStats := baseStats.From(*stats)
				response, delivered := b.FinalizeTerminalTool(ctx, c, convCtx, userID, placeholder, lastToolResult, &telegramStats)
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
			case agent.EventQuestionRequested:
				// ask_user fired: render the question as a Telegram message.
				question, _ := event.QuestionPayload["question"].(string)
				options := extractStringSlice(event.QuestionPayload["options"])
				kind, _ := event.QuestionPayload["kind"].(string)
				formatted := formatAskUserQuestion(question, options, kind)
				if formatted != "" {
					if _, sendErr := c.Bot().Send(c.Recipient(), formatted); sendErr != nil {
						b.Logger().Warn("ask_user: failed to send question to user",
							"user_id", userID, "error", sendErr)
					}
				}
			case agent.EventStats:
				currentStats = baseStats.From(event.Stats)
				b.StoreOrchestrationSnapshot(userID, currentStats)
			case agent.EventFinal:
				finalStats := baseStats.From(event.Stats)
				currentStats = finalStats

				// Non-streaming delivery: edit placeholder or send fresh.
				if !event.Delivered {
					finalText := strings.TrimSpace(event.Text)
					if placeholder != nil {
						if finalText != "" {
							parts := tgtelegram.RenderForEntities(finalText)
							if len(parts) > 0 {
								if _, err := b.EditAssistantMsg(c.Bot(), placeholder, parts[0], finalText); err == nil {
									b.SendAssistantMsgRemainder(c.Bot(), c.Recipient(), parts, 1)
								} else {
									if delErr := c.Bot().Delete(placeholder); delErr != nil {
										tgtelegram.LogPlaceholderDeleteFailure(b.Logger(), userID, placeholder, delErr)
									}
									b.SendAssistantText(c, finalText)
								}
							}
						} else {
							if delErr := c.Bot().Delete(placeholder); delErr != nil {
								tgtelegram.LogPlaceholderDeleteFailure(b.Logger(), userID, placeholder, delErr)
							}
						}
					} else if finalText != "" {
						b.SendAssistantText(c, finalText)
					}
				}

				if event.Stats.LLMCalls > 0 && event.Stats.TerminalTool == "" {
					b.NotifySoftBudget(c, userID)
				}

				// Archive this turn.
				archiveAppender := b.ArchiveAppender()
				archiveDB := b.ArchiveRepository()
				if archiveAppender != nil && archiveDB != nil {
					archiveCtx := context.Background()
					chatID := c.Chat().ID
					nextIdx := int64(0)
					if maxIdx, err := archiveDB.MaxTurnIndex(archiveCtx, chatID); err == nil {
						nextIdx = maxIdx + 1
					} else {
						b.Logger().Warn("archive: max turn_index lookup failed", "chat_id", chatID, "error", err)
					}
					conversation.ArchiveConversationTurns(archiveCtx, b.Logger(), archiveAppender, conversation.ArchiveTurnInput{
						ChatID:       chatID,
						UserID:       c.Sender().ID,
						NextIndex:    nextIdx,
						UserText:     userText,
						LoopMessages: convCtx.MessagesSince(preLoopIdx),
						LLMCalls:     finalStats.LLMCalls,
						ToolCalls:    finalStats.ToolCalls,
						ElapsedMS:    time.Since(turnStart).Milliseconds(),
						TokensIn:     convCtx.TotalTokensUsed(),
					})
				}

				compactedToolResults := convCtx.CompactCompletedToolResults(conversation.ToolResultCompactionPolicy{
					MaxChars:       1200,
					KeepRecentFull: 2,
				})
				if compactedToolResults > 0 {
					b.Logger().Info("conversation tool results compacted", "user_id", userID, "count", compactedToolResults)
				}
				go func() {
					if err := convCtx.EnforceLimit(context.Background()); err != nil {
						b.Logger().Error("context enforcement failed", "user_id", userID, "error", err)
					}
				}()

				b.Logger().Info("conversation complete",
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

func (ib *InvocationBuilder) executeToolCalls(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, calls []llm.ToolCall, toolsExposed []string, readSkills []string) agent.ToolExecutionSummary {
	return ib.b.ExecToolCalls(ctx, c, convCtx, userID, calls, toolsExposed, readSkills)
}

func (ib *InvocationBuilder) checkBudgetQuota(c tele.Context, userID string, estimatedTokens int) error {
	bgt := ib.b.BudgetRuntime()
	if bgt == nil {
		return nil
	}
	if bgt.IsHardBudgetExceeded() {
		ib.b.Logger().Warn("hard budget exceeded, halting LLM call", "user_id", userID)
		if _, err := c.Bot().Send(c.Recipient(), "Budget limit reached. LLM calls are temporarily halted."); err != nil {
			ib.b.Logger().Warn("budget notice send failed", "error", err)
		}
		return fmt.Errorf("hard budget exceeded")
	}
	if !bgt.CanAfford(estimatedTokens, 500) {
		ib.b.Logger().Warn("predicted cost exceeds hard budget, halting LLM call", "user_id", userID)
		if _, err := c.Bot().Send(c.Recipient(), "Predicted cost would exceed budget. Please adjust your budget or wait."); err != nil {
			ib.b.Logger().Warn("budget notice send failed", "error", err)
		}
		return fmt.Errorf("budget unaffordable")
	}
	return nil
}

func (ib *InvocationBuilder) modelToolNames() []string {
	toolReg := ib.b.ToolRegistry()
	if toolReg == nil {
		return nil
	}
	return toolReg.Names()
}

func (ib *InvocationBuilder) maxToolLoopIterations() int {
	return ib.b.MaxToolLoopIterations()
}

func (ib *InvocationBuilder) terminalToolPolicyEnabled() bool {
	return ib.b.TerminalToolPolicyEnabled()
}

// appendUniqueStrings appends additions to values, skipping blanks and duplicates.
func appendUniqueStrings(values []string, additions ...string) []string {
	for _, addition := range additions {
		addition = strings.TrimSpace(addition)
		if addition == "" {
			continue
		}
		found := false
		for _, v := range values {
			if v == addition {
				found = true
				break
			}
		}
		if !found {
			values = append(values, addition)
		}
	}
	return values
}
