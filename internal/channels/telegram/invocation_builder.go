package telegramadapter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/agents/summarizer"
	"github.com/aura/aura/internal/agent/governance"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/agentnote"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/stringx"
	tgtelegram "github.com/aura/aura/internal/telegram"

	tele "gopkg.in/telebot.v4"
)

// InvocationBuilder constructs agent.Invocation values for the Telegram channel.
// It keeps invocation construction beside the Telegram channel adapter so
// internal/telegram remains a thin channel wrapper.
type InvocationBuilder struct {
	b                 *tgtelegram.Bot
	hub               *chat.Hub          // set after hub creation; used for ask_user resume routing
	outbound          *Outbound          // canonical streaming path used by streamingChatClient
	agentNoteStore    *agentnote.Store   // nil when not configured; injected by NewHub
	memoryStore       *memoryindex.Store // nil when compact memory unavailable; injected by NewHub
	priorityCaches    sync.Map           // thread_id -> *memoryindex.PrioritySectionCache
	payloadSummarizer governance.PayloadSummarizer // nil = disabled; wired by NewHub when config enables it
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
	ib.agentNoteStore = b.AgentNoteStore()
	ib.memoryStore = b.MemoryStore()
	if cfg := b.Config(); cfg != nil && cfg.PayloadSummarizerEnabled && b.LLMClient() != nil {
		ib.payloadSummarizer = governance.NewSubagentPayloadSummarizer(
			b.LLMClient(), cfg.LLMModel, summarizer.Prompt,
			cfg.PayloadThresholdTokens, cfg.PayloadMaxTokens,
		)
	}
	adapter, err := chat.NewAgentLoopAdapter(ib.Build)
	if err != nil {
		return nil, err
	}
	hub, err := chat.New(chat.Config{Loop: adapter, LifecycleStore: lifecycle, Logger: logger})
	if err != nil {
		return nil, err
	}
	outbound := NewOutbound(logger)
	outbound.SetTTS(b.TTSClient())
	outbound.SetVoicePolicy(b.VoicePolicy())
	outbound.SetDispatchDB(b.Pool())
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
	runID := ""
	if run != nil {
		runID = run.ID
	}

	session, _ := b.SessionStore().Begin(userID, conversation.Config{
		MaxTokens:   cfg.MaxContextTokens,
		MaxMessages: cfg.MaxHistoryMessages,
		Summarizer:  b.LLMClient(),
		Logger:      b.Logger(),
	})
	convCtx := session.Conversation()
	userText := msg.Text

	overlay := conversation.LoadPromptOverlay(cfg.PromptOverlayPath)
	if ib.memoryStore != nil {
		if docs, fetchErr := ib.memoryStore.FetchRecentOperational(ctx, 10); fetchErr == nil {
			if block := memoryindex.OperationalLessonsBlock(docs, 5120); block != "" {
				overlay += "\n\n" + block
			}
		} else {
			b.Logger().Warn("ops overlay: failed to fetch operational lessons", "error", fetchErr)
		}
	}
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
	toolManifest := tools.RenderSplitManifest(toolReg.FullDefinitions())
	pinnedOperational := ib.renderPinnedOperational(ctx, msg.ThreadID, convCtx.MessageCount())
	wikiTOC := b.WikiTOC()
	promptPlan := agent.ComposeAgentPrompt(cfg, b.TimeLocation(), overlay, pinnedOperational, skillsBlock, toolManifest, wikiTOC, time.Now())
	convCtx.SetSystemMessage(promptPlan.Content)

	// Inject agent working-memory note from the previous turn (if any).
	// The conversationID for agent_note is the Telegram chat ID as a string.
	if ib.agentNoteStore != nil && c.Chat() != nil {
		convID := fmt.Sprintf("%d", c.Chat().ID)
		if noteContent, exists, err := ib.agentNoteStore.Get(ctx, convID); err == nil && exists && noteContent != "" {
			convCtx.SetAgentNote(noteContent)
		}
	}

	b.Logger().Info("conversation started",
		"user_id", userID,
		"username", c.Sender().Username,
		"message", userText,
	)

	// Resume detection: if this thread has a pending ask_user question, route
	// the inbound text as the tool_result instead of a new user message.
	addedUserInput := false
	if ib.hub != nil {
		pending, hasDurablePending, pendingErr := ib.hub.PendingQuestion(ctx, msg.ThreadID, msg.Channel)
		if pendingErr != nil {
			b.Logger().Warn("ask_user: pending question lookup failed", "user_id", userID, "error", pendingErr)
		}
		status, hasThreadStatus := ib.hub.ThreadRunStatus(msg.ThreadID)
		if hasDurablePending || (hasThreadStatus && status == chat.RunStatusWaitingForUser) {
			resume, ok := prepareAskUserResumeInput(userText, convCtx.Messages(), pending.Options, pending.Kind, hasDurablePending)
			if ok {
				if resume.Rejected {
					if _, sendErr := c.Bot().Send(c.Recipient(), resume.RejectMessage); sendErr != nil {
						b.Logger().Warn("ask_user: failed to send reject message", "user_id", userID, "error", sendErr)
					}
					// Signal Hub to preserve WaitingForUser status on error.
					run.Status = chat.RunStatusWaitingForUser
					session.Finish()
					return agent.Invocation{}, fmt.Errorf("ask_user: out-of-range reply, question still pending")
				}
				if hasDurablePending {
					if recordErr := ib.hub.RecordQuestionAnswer(ctx, run, msg, chat.QuestionAnswer{
						QuestionID:        pending.ID,
						SelectedOptionIDs: resume.SelectedOptionIDs,
						FreeText:          resume.Content,
						AnsweredMessageID: msg.ID,
					}); recordErr != nil {
						b.Logger().Warn("ask_user: failed to record question answer",
							"user_id", userID,
							"tool_call_id", resume.CallID,
							"question_id", pending.ID,
							"error", recordErr)
					}
				}
				if resume.HasPendingCall {
					convCtx.AddToolResultMessage(resume.CallID, resume.Content)
				} else {
					convCtx.AddUserMessage("Answer to pending " + resume.Kind + " question: " + resume.Content)
				}
				addedUserInput = true
				b.Logger().Info("ask_user: resume with answer",
					"user_id", userID,
					"tool_call_id", resume.CallID,
					"question_id", pending.ID,
					"durable_pending", hasDurablePending,
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

	// Native "typing…" indicator. Telegram client expires sendChatAction
	// after ~5s, so we refresh every 4s for the full duration of the turn.
	// Goroutine stops when EventFinal fires OR the run context is cancelled
	// (budget exceeded, hard timeout, etc.). The 15-min backstop is a
	// defensive ceiling — typing should never outlive a turn by that much.
	typingCtx, cancelTyping := context.WithCancel(ctx)
	go func() {
		defer cancelTyping()
		_ = c.Notify(tele.Typing) // fire once immediately
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		backstop := time.NewTimer(15 * time.Minute)
		defer backstop.Stop()
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-backstop.C:
				return
			case <-ticker.C:
				_ = c.Notify(tele.Typing)
			}
		}
	}()

	// statusPane renders in-flight tool calls into the same placeholder
	// during the tool-loop phase. Once Outbound starts streaming content
	// it calls pane.EnterContentMode() and the pane stops issuing its own
	// edits — see internal/channels/telegram/outbound.go::flush().
	pane := newStatusPane(time.Now, func(text string, ents tele.Entities) error {
		if placeholder == nil {
			return nil
		}
		_, err := c.Bot().Edit(placeholder, text, ents)
		if err != nil {
			b.Logger().Debug("status pane edit failed",
				"user_id", userID, "error", err)
		}
		return err
	})

	// Route streaming through the canonical channels/telegram.Outbound.
	if ib.outbound == nil {
		ib.outbound = NewOutbound(b.Logger())
		ib.outbound.SetTTS(b.TTSClient())
		ib.outbound.SetVoicePolicy(b.VoicePolicy())
	}
	chatClient := newStreamingChatClient(b.LLMClient(), cfg.LLMModel, cfg.ReasoningEffort, msg.ThreadID, ib.outbound, c, userID, placeholder, pane)

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
		nil,
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
	addActiveTools := func(names []string) {
		toolMu.Lock()
		defer toolMu.Unlock()
		activeToolNames = stringx.AppendUnique(activeToolNames, names...)
	}
	var failureReader agent.PostTurnFailureReader
	if repo := b.ToolAttemptsRepo(); repo != nil {
		if reader, ok := repo.(agent.PostTurnFailureReader); ok {
			failureReader = reader
		} else {
			b.Logger().Warn("posthook: attempts repo does not support thread failure reads")
		}
	}
	postTurnRecord := agent.TurnRecord{
		RunID:       runID,
		ThreadID:    msg.ThreadID,
		UserMessage: userText,
	}
	postTurn := agent.NewHeuristicPostTurnConfig(ib.memoryStore, failureReader, cfg.OP07NFailThreshold, cfg.OP07RecentTurns, b.Logger(), postTurnRecord)
	if hook := agent.NewMemoryJudgeHook(cfg.MemoryJudgeEnabled, b.LLMClient(), cfg.LLMModel, cfg.ReasoningEffort, b.Logger()); hook != nil && ib.memoryStore != nil {
		postTurn.Store = ib.memoryStore
		postTurn.Record = postTurnRecord
		postTurn.Hooks = append(postTurn.Hooks, hook)
	}

	// US-CTX-04/05: wire AutoCompactEngine when enabled and MaxConversationTokens is set.
	// agentloop.go falls back to DefaultContextEngine when ContextEngine is nil.
	var ctxEngine conversation.ContextEngine
	if cfg.CTXEngine != "default" && cfg.MaxConversationTokens > 0 && b.LLMClient() != nil {
		compressor := conversation.NewContextCompressor(b.LLMClient(), cfg.ModelContextWindow)
		ctxEngine = conversation.NewAutoCompactEngine(compressor, cfg.MaxConversationTokens, cfg.CTXCompactScope)
	}

	inv := agent.Invocation{
		Client: chatClient,
		Executor: agent.ToolExecutorFunc(func(ctx context.Context, calls []llm.ToolCall) agent.ExecutionSummary {
			execution := ib.executeToolCalls(ctx, c, convCtx, userID, calls)
			if len(execution.DiscoveredTools) > 0 {
				addActiveTools(execution.DiscoveredTools)
			}
			pinnedWrite := pinnedOperationalWriteInCalls(calls)
			if pinnedWrite {
				ib.invalidatePinnedOperational(msg.ThreadID)
			}
			// Hot-reload system prompt when the file tool writes to an overlay
			// file so the NEXT LLM iteration in this conversation sees the
			// updated rules without waiting for a new conversation.
			if overlayWriteInCalls(calls) || pinnedWrite {
				newOverlay := conversation.LoadPromptOverlay(cfg.PromptOverlayPath)
				if ib.memoryStore != nil {
					if docs, fetchErr := ib.memoryStore.FetchRecentOperational(ctx, 10); fetchErr == nil {
						if block := memoryindex.OperationalLessonsBlock(docs, 5120); block != "" {
							newOverlay += "\n\n" + block
						}
					}
				}
				newPinnedOperational := ib.renderPinnedOperational(ctx, msg.ThreadID, convCtx.MessageCount())
				newPromptPlan := agent.ComposeAgentPrompt(cfg, b.TimeLocation(), newOverlay, newPinnedOperational, skillsBlock, toolManifest, wikiTOC, time.Now())
				convCtx.SetSystemMessage(newPromptPlan.Content)
				b.Logger().Debug("overlay: hot-reloaded after file tool write to overlay file")
			}
			return agent.ExecutionSummary{
				LastResult:        execution.LastResult,
				FatalResult:       execution.FatalResult,
				ReadSkillNames:    execution.ReadSkillNames,
				TerminalTool:      execution.TerminalTool,
				Results:           execution.Results,
				AwaitingUserInput: execution.AwaitingUserInput,
			}
		}),
		State:               convCtx,
		PromptVersion:       promptPlan.Version,
		PromptHash:          promptPlan.Hash,
		PromptModules:       promptPlan.Modules,
		Toolset:             "registered",
		ToolsetSelectReason: "core tools plus Qdrant top-K=5 retrieval",
		Tools:               toolDefs,
		ToolsProvider:       toolsProvider,
		PostTurn:            postTurn,
		Options: agent.Options{
			ContextEngine:           ctxEngine,
			MaxIterations:           maxIterations,
			ParallelTools:           cfg.AgentParallelTools,
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
			case agent.EventToolStart:
				pane.OnToolStart(event.ToolCallID, event.ToolName, event.ToolArgKeys)
			case agent.EventToolEnd:
				pane.OnToolEnd(
					event.ToolCallID,
					event.ToolSuccess,
					time.Duration(event.ToolElapsedMs)*time.Millisecond,
					event.ToolResultPreview,
				)
			case agent.EventQuestionRequested:
				// ask_user fired: render the question as a Telegram message.
				question, _ := event.QuestionPayload["question"].(string)
				options := extractStringSlice(event.QuestionPayload["options"])
				kind, _ := event.QuestionPayload["kind"].(string)
				formatted := formatAskUserQuestion(question, options, kind)
				if formatted != "" {
					sendOpts := []any{}
					if markup := askUserQuestionMarkup(options, kind); markup != nil {
						sendOpts = append(sendOpts, markup)
					}
					if _, sendErr := c.Bot().Send(c.Recipient(), formatted, sendOpts...); sendErr != nil {
						b.Logger().Warn("ask_user: failed to send question to user",
							"user_id", userID, "error", sendErr)
					}
				}
			case agent.EventStats:
				currentStats = baseStats.From(event.Stats)
				b.StoreOrchestrationSnapshot(userID, currentStats)
			case agent.EventFinal:
				// Stop the native typing indicator and the status pane
				// before we touch the placeholder so neither races the
				// final edit/delete below.
				cancelTyping()
				pane.Finalize()

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

func (ib *InvocationBuilder) executeToolCalls(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, calls []llm.ToolCall) agent.ToolExecutionSummary {
	return agent.ExecuteToolCalls(ctx, ib.b.ToolRegistry(), convCtx, userID, tgtelegram.ChatIDFromTeleContext(c), calls, ib.b.TerminalToolPolicyEnabled(), ib.b.Logger(),
		agent.WithToolAttemptRecording(identity.RunIDFromContext(ctx), ib.b.ToolAttemptsRepo()),
		agent.WithTokenJuice(ib.b.TokenJuiceEnabled()),
		agent.WithPayloadSummarizer(ib.payloadSummarizer))
}

func (ib *InvocationBuilder) renderPinnedOperational(ctx context.Context, threadID string, turnIdx int) string {
	if ib == nil {
		return ""
	}
	return memoryindex.RenderPinnedSectionWithCache(ctx, ib.memoryStore, &ib.priorityCaches, threadID, "telegram", turnIdx)
}

func (ib *InvocationBuilder) invalidatePinnedOperational(threadID string) {
	if ib == nil {
		return
	}
	memoryindex.InvalidatePinnedSectionInCache(&ib.priorityCaches, threadID, "telegram")
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
