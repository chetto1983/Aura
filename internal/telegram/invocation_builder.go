package telegram

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// turnStats aggregates per-turn counters for structured logging and snapshot storage.
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

// toolExecutionSummary holds the outcome of executing a batch of tool calls.
type toolExecutionSummary struct {
	lastResult      string
	fatalResult     string
	readSkillNames  []string
	terminalTool    string
	discoveredTools []string
	results         map[string]string
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

// From populates agent-loop–tracked fields from stats, preserving telegram-metadata
// fields (promptVersion, toolset, toolsExposed, etc.) already set on s.
func (s turnStats) From(stats agent.Stats) turnStats {
	s.llmCalls = stats.LLMCalls
	s.toolCalls = stats.ToolCalls
	s.loopSteps = stats.LoopSteps
	s.toolsCalled = append([]string(nil), stats.ToolsCalled...)
	s.readSkills = append([]string(nil), stats.ReadSkills...)
	s.skillsRead = stats.SkillsRead
	s.swarmUsed = stats.SwarmUsed
	s.sandboxUsed = stats.SandboxUsed
	s.terminalTool = stats.TerminalTool
	s.duplicateToolCall = stats.DuplicateToolCall
	s.tokensPrompt = stats.TokensPrompt
	s.tokensCompletion = stats.TokensCompletion
	s.tokensTotal = stats.TokensTotal
	s.costUSD = stats.CostUSD
	return s
}

// applyTo writes accumulated turn counters back into agent.Stats after
// finalizeTerminalToolWithNoToolLLM mutates the turn counters.
func (s turnStats) applyTo(stats *agent.Stats) {
	stats.LLMCalls = s.llmCalls
	stats.LoopSteps = s.loopSteps
	stats.TokensPrompt = s.tokensPrompt
	stats.TokensCompletion = s.tokensCompletion
	stats.TokensTotal = s.tokensTotal
	stats.CostUSD = s.costUSD
}

// checkBudgetQuota sends a Telegram budget notice and returns non-nil if the LLM call
// should be halted. The caller is responsible for session cleanup before returning.
func (b *Bot) checkBudgetQuota(c tele.Context, userID string, estimatedTokens int) error {
	if b.budget == nil {
		return nil
	}
	if b.budget.IsHardBudgetExceeded() {
		b.logger.Warn("hard budget exceeded, halting LLM call", "user_id", userID)
		if _, err := c.Bot().Send(c.Recipient(), "Budget limit reached. LLM calls are temporarily halted."); err != nil {
			b.logger.Warn("budget notice send failed", "error", err)
		}
		return fmt.Errorf("hard budget exceeded")
	}
	if !b.budget.CanAfford(estimatedTokens, 500) {
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

	session, _ := b.sessionStore().Begin(userID, conversation.Config{
		MaxTokens:   b.cfg.MaxContextTokens,
		MaxMessages: b.cfg.MaxHistoryMessages,
		Summarizer:  b.llm,
		Logger:      b.logger,
	})
	convCtx := session.Conversation()
	userText := msg.Text

	overlay := conversation.LoadPromptOverlay(b.cfg.PromptOverlayPath)
	var skillsBlock string
	if b.skills != nil {
		loadedSkills, err := b.skills.LoadAll()
		if err != nil {
			b.logger.Warn("failed to load local skills", "error", err)
		} else if block := auraskills.PromptBlock(loadedSkills); block != "" {
			skillsBlock = block
		}
	}
	toolAllowlist := b.modelToolNames()
	toolManifest := tools.RenderToolManifest(b.tools.Definitions())
	promptPlan := composeAgentPrompt(b.cfg, b.loc, overlay, skillsBlock, toolManifest, time.Now())
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
	if b.llm == nil {
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
		b.tools.Search,
		b.tools.DefinitionsFor,
		b.tools.Definitions,
		convCtx.LatestUserMessageText,
		func() int { return b.cfg.ToolSearchTopK },
		b.logger,
	)
	toolDefs := toolsProvider()
	baseStats := turnStats{
		promptVersion:       promptPlan.Version,
		promptModules:       append([]string(nil), promptPlan.Modules...),
		promptHash:          promptPlan.Hash,
		toolset:             "registered",
		toolsetSelectReason: "core tools plus Qdrant top-K=5 retrieval",
		toolsExposed:        toolDefinitionNames(toolDefs),
	}

	var currentStats turnStats
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
			execution := b.executeToolCalls(ctx, c, convCtx, userID, calls, currentToolNames(), currentStats.readSkills)
			if len(execution.discoveredTools) > 0 {
				addActiveTools(execution.discoveredTools)
			}
			return agent.ExecutionSummary{
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
			ToolResolver:            b.tools.DefinitionFor,
			PhantomToolGuard: &agent.PhantomToolGuard{
				ToolNamesFn: b.tools.Names,
			},
			BeforeLLM: func() (string, bool) {
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
			TerminalHandler: func(ctx context.Context, terminalTool, lastToolResult string, stats *agent.Stats) (string, bool, bool) {
				telegramStats := baseStats.From(*stats)
				response, delivered := b.finalizeTerminalToolWithNoToolLLM(ctx, c, convCtx, userID, placeholder, lastToolResult, &telegramStats)
				telegramStats.applyTo(stats)
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
				if b.archiver != nil && b.archiveDB != nil {
					archiveCtx := context.Background()
					chatID := c.Chat().ID
					nextIdx := int64(0)
					if maxIdx, err := b.archiveDB.MaxTurnIndex(archiveCtx, chatID); err == nil {
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
					"llm_calls", finalStats.llmCalls,
					"tool_calls", finalStats.toolCalls,
					"loop_steps", finalStats.loopSteps,
					"prompt_version", finalStats.promptVersion,
					"prompt_hash", finalStats.promptHash,
					"prompt_modules", strings.Join(finalStats.promptModules, ","),
					"toolset", finalStats.toolset,
					"toolset_select_reason", finalStats.toolsetSelectReason,
					"tools_exposed", strings.Join(finalStats.toolsExposed, ","),
					"tools_called", strings.Join(finalStats.toolsCalled, ","),
					"read_skills", strings.Join(finalStats.readSkills, ","),
					"skills_read", finalStats.skillsRead,
					"swarm_used", finalStats.swarmUsed,
					"sandbox_used", finalStats.sandboxUsed,
					"terminal_tool", finalStats.terminalTool,
					"duplicate_tool_rejected", finalStats.duplicateToolCall,
					"tokens_prompt", finalStats.tokensPrompt,
					"tokens_completion", finalStats.tokensCompletion,
					"tokens_total", finalStats.tokensTotal,
					"cost_usd", fmt.Sprintf("%.6f", finalStats.costUSD),
				)

				session.Finish()
			}
		},
	}
	return inv, nil
}

// telegramHubChatClient implements agent.ChatClient for the Hub path.
// It calls llm.Stream and drives progressive Telegram edits in real time.
type telegramHubChatClient struct {
	b           *Bot
	teleCtx     tele.Context
	userID      string
	placeholder *tele.Message
}

func (c *telegramHubChatClient) Chat(ctx context.Context, messages []llm.Message, toolDefs []llm.ToolDefinition) (agent.ChatResponse, error) {
	req := llm.Request{
		Messages:        messages,
		Model:           c.b.cfg.LLMModel,
		Tools:           toolDefs,
		ReasoningEffort: c.b.cfg.ReasoningEffort,
	}
	ch, err := c.b.llm.Stream(ctx, req)
	if err != nil {
		c.b.logger.Error("LLM stream failed", "user_id", c.userID, "error", err)
		return agent.ChatResponse{Response: llm.Response{Content: "Sorry, I couldn't process your message. Please try again."}}, err
	}
	resp, delivered, err := c.streamTokens(ch)
	if err != nil {
		c.b.logger.Error("LLM stream read failed", "user_id", c.userID, "error", err)
		return agent.ChatResponse{Response: llm.Response{Content: "Sorry, I couldn't process your message. Please try again."}}, err
	}
	return agent.ChatResponse{Response: resp, Delivered: delivered}, nil
}

// streamTokens reads tokens from ch and progressively edits a Telegram message.
// Mirrors the legacy bot streaming path with identical flush thresholds.
func (c *telegramHubChatClient) streamTokens(ch <-chan llm.Token) (llm.Response, bool, error) {
	var sb strings.Builder
	var cotBuf strings.Builder
	var msg *tele.Message
	var lastEdit time.Time
	var resp llm.Response

	readyToFlush := func() bool {
		if sb.Len() >= streamingMinThreshold {
			return true
		}
		if sb.Len() == 0 && cotBuf.Len() >= streamingReasoningMinThreshold {
			return true
		}
		return false
	}

	flush := func() {
		if !readyToFlush() {
			return
		}
		raw := composeStreamingMessage(cotBuf.String(), sb.String())
		parts := renderForTelegramEntities(raw)
		if len(parts) == 0 {
			return
		}
		if msg == nil {
			if c.placeholder != nil {
				if edited, err := c.b.editAssistantMessage(c.teleCtx.Bot(), c.placeholder, parts[0], raw); err != nil {
					c.b.logger.Debug("placeholder edit failed, falling back to new message", "user_id", c.userID, "error", err)
					sent, sendErr := c.teleCtx.Bot().Send(c.teleCtx.Recipient(), parts[0].Text, parts[0].Entities)
					if sendErr != nil {
						return
					}
					msg = sent
				} else {
					msg = edited
				}
			} else {
				sent, err := c.teleCtx.Bot().Send(c.teleCtx.Recipient(), parts[0].Text, parts[0].Entities)
				if err != nil {
					c.b.logger.Warn("streaming initial send failed", "user_id", c.userID, "error", err)
					return
				}
				msg = sent
			}
			lastEdit = time.Now()
			return
		}
		if time.Since(lastEdit) < streamingEditThrottle {
			return
		}
		if _, err := c.b.editAssistantMessage(c.teleCtx.Bot(), msg, parts[0], raw); err != nil {
			c.b.logger.Debug("streaming edit failed", "user_id", c.userID, "error", err)
			return
		}
		lastEdit = time.Now()
	}

	for tok := range ch {
		if tok.Err != nil {
			return llm.Response{}, msg != nil, tok.Err
		}
		if tok.Reasoning != "" {
			cotBuf.WriteString(tok.Reasoning)
			flush()
		}
		if tok.Content != "" {
			sb.WriteString(tok.Content)
			flush()
		}
		if tok.Done {
			resp = llm.Response{
				Content:      sb.String(),
				HasToolCalls: len(tok.ToolCalls) > 0,
				ToolCalls:    tok.ToolCalls,
				Usage:        tok.Usage,
				Reasoning:    strings.TrimSpace(cotBuf.String()),
			}
			if msg != nil && !resp.HasToolCalls {
				raw := strings.TrimSpace(sb.String())
				if raw == "" {
					break
				}
				parts := renderForTelegramEntities(raw)
				if len(parts) == 0 {
					break
				}
				if _, err := c.b.editAssistantMessage(c.teleCtx.Bot(), msg, parts[0], raw); err != nil {
					c.b.logger.Warn("streaming final edit failed", "user_id", c.userID, "error", err)
				}
				c.b.sendAssistantRemainder(c.teleCtx.Bot(), c.teleCtx.Recipient(), parts, 1)
			}
			break
		}
	}
	return resp, msg != nil && !resp.HasToolCalls, nil
}

// executeToolCalls runs the LLM's tool calls concurrently and appends results
// in original order.
func (b *Bot) executeToolCalls(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, calls []llm.ToolCall, toolsExposed []string, readSkills []string) toolExecutionSummary {
	if len(calls) == 0 {
		return toolExecutionSummary{}
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
			toolCtx := tools.WithAllowedToolNames(tools.WithUserID(ctx, userID), b.tools.Names())
			args := toolArgumentsForTool(tc.Name, tc.Arguments, chatIDFromTeleContext(c))
			result, err := b.tools.Execute(toolCtx, tc.Name, args)
			if err != nil {
				result = tools.FormatToolError(err)
				b.logger.Warn("tool call failed", "user_id", userID, "tool", tc.Name, "error", err)
			}
			readSkillName := ""
			if err == nil && tc.Name == "read_file" {
				readSkillName = agent.SkillNameFromReadFileArgs(tc.Arguments)
			}
			terminalTool := ""
			if err == nil && b.terminalToolPolicyEnabled() && isTerminalTool(tc.Name) {
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

	summary := toolExecutionSummary{results: make(map[string]string, len(results))}
	for _, r := range results {
		wrapped := agent.WrapUntrustedToolResult(r.tool, r.content)
		convCtx.AddToolResultMessage(r.id, wrapped)
		summary.lastResult = r.content
		summary.results[r.id] = r.content
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
