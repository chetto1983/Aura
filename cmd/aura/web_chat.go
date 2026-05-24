package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/agents/summarizer"
	"github.com/aura/aura/internal/agent/governance"
	"github.com/aura/aura/internal/agent/tools/attempts"
	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/api"
	webadapter "github.com/aura/aura/internal/channels/web"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/telegram"
)

func newHubBackedWebChatService(cfg *config.Config, deps *telegram.Deps, logger *slog.Logger) (api.ChatService, error) {
	depsGetter := newWebChatDepsGetter(cfg, deps)
	if depsGetter == nil {
		return nil, nil
	}
	sessionStore := agent.NewSessionStore()
	postTurnReader := webPostTurnFailureReader(deps.AttemptsRepo)
	if postTurnReader == nil && deps.Pool != nil {
		postTurnReader = attempts.NewSQLiteRepo(deps.Pool)
	}
	builder := &webInvocationBuilder{
		depsGetter:     depsGetter,
		sessionStore:   sessionStore,
		cfg:            cfg,
		logger:         logger,
		postTurnStore:  deps.MemoryStore,
		postTurnReader: postTurnReader,
		skillsLoader:   deps.Skills,
		loc:            deps.Loc,
	}
	if deps.WikiStore != nil {
		builder.wikiTOCFn = deps.WikiStore.GetCachedTOC
	}
	loop, err := chat.NewAgentLoopAdapter(builder.Build)
	if err != nil {
		return nil, err
	}
	hub, err := chat.New(chat.Config{Loop: loop, LifecycleStore: deps.RunStore, Logger: logger})
	if err != nil {
		return nil, err
	}
	router := webadapter.NewRouter()
	hub.RegisterOutbound(router)
	svc := webadapter.NewChatService(hub, router)
	if svc == nil {
		return nil, errors.New("web chat hub service unavailable")
	}
	return &apiChatServiceAdapter{svc: svc, sessionStore: sessionStore}, nil
}

type apiChatServiceAdapter struct {
	svc          *webadapter.ChatService
	sessionStore *agent.SessionStore
}

func (a *apiChatServiceAdapter) Chat(ctx context.Context, userID, threadID, message string) (api.ChatReply, error) {
	reply, err := a.svc.Chat(ctx, userID, threadID, message)
	return api.ChatReply{
		Reply:     reply.Reply,
		ElapsedMs: reply.ElapsedMs,
		LLMCalls:  reply.LLMCalls,
		ToolCalls: reply.ToolCalls,
		Tokens:    reply.Tokens,
		CacheHit:  reply.CacheHit,
		ToolsUsed: reply.ToolsUsed,
	}, err
}

type webInvocationBuilder struct {
	depsGetter     func() agent.RunTaskDeps
	sessionStore   *agent.SessionStore
	cfg            *config.Config
	logger         *slog.Logger
	postTurnStore  *memoryindex.Store
	postTurnReader agent.PostTurnFailureReader
	priorityCaches sync.Map
	skillsLoader   *auraskills.Loader
	wikiTOCFn      func() string
	loc            *time.Location
}

func (b *webInvocationBuilder) Build(ctx context.Context, run *chat.Run, msg chat.InboundMessage) (agent.Invocation, error) {
	if b == nil || b.depsGetter == nil {
		return agent.Invocation{}, errors.New("web chat: deps unavailable")
	}
	deps := b.depsGetter()
	if deps.LLM == nil {
		return agent.Invocation{}, errors.New("web chat: LLM unavailable")
	}
	userID := strings.TrimSpace(msg.PrincipalID)
	logger := deps.Logger
	if logger == nil {
		logger = b.logger
	}
	if logger == nil {
		logger = slog.Default()
	}
	sessionKey := webSessionKey(userID, msg.ThreadID)
	session, _ := b.sessionStore.Begin(sessionKey, conversation.Config{
		MaxTokens:   b.cfg.MaxContextTokens,
		MaxMessages: b.cfg.MaxHistoryMessages,
		Summarizer:  deps.LLM,
		Logger:      logger,
	})
	convCtx := session.Conversation()
	turnIdx := convCtx.MessageCount()

	overlay := conversation.LoadPromptOverlay(b.cfg.PromptOverlayPath)
	if b.postTurnStore != nil {
		if docs, fetchErr := b.postTurnStore.FetchRecentOperational(ctx, 10); fetchErr == nil {
			if block := memoryindex.OperationalLessonsBlock(docs, 5120); block != "" {
				overlay += "\n\n" + block
			}
		}
	}
	var skillsBlock string
	if b.skillsLoader != nil {
		if loadedSkills, loadErr := b.skillsLoader.LoadAll(); loadErr == nil {
			if block := auraskills.PromptBlock(loadedSkills); block != "" {
				skillsBlock = block
			}
		}
	}
	var wikiTOC string
	if b.wikiTOCFn != nil {
		wikiTOC = b.wikiTOCFn()
	}
	var toolManifest string
	if deps.Tools != nil {
		toolManifest = toolregistry.RenderSplitManifest(deps.Tools.FullDefinitions())
	}
	pinned := b.renderPinnedOperational(ctx, msg.ThreadID, turnIdx)
	loc := b.loc
	if loc == nil {
		loc = time.Local
	}
	promptPlan := agent.ComposeAgentPrompt(b.cfg, loc, overlay, pinned, skillsBlock, toolManifest, wikiTOC, time.Now())
	system := promptPlan.Content
	convCtx.SetSystemMessage(system)
	convCtx.AddUserMessage(msg.Text)
	runID := ""
	if run != nil {
		runID = run.ID
	}
	allowlist := agent.CleanToolList(nil)
	var toolDefs []llm.ToolDefinition
	if deps.Tools != nil {
		allowlist = agent.CleanToolList(deps.Tools.Names())
		toolDefs = deps.Tools.DefinitionsFor(allowlist)
	}
	toolTimeout := deps.ToolTimeout
	if toolTimeout <= 0 {
		toolTimeout = deps.Timeout
	}
	// US-CTX-04/05: wire AutoCompactEngine when enabled and MaxConversationTokens is set.
	var ctxEngine conversation.ContextEngine
	if b.cfg.CTXEngine != "default" && b.cfg.MaxConversationTokens > 0 && deps.LLM != nil {
		compressor := conversation.NewContextCompressor(deps.LLM, b.cfg.ModelContextWindow)
		engine := conversation.NewAutoCompactEngine(compressor, b.cfg.MaxConversationTokens, b.cfg.CTXCompactScope)
		engine.MinTurnsBetweenCompactions = b.cfg.CTXMinTurnsBetweenCompactions
		ctxEngine = engine
	}

	// US-CTX-05: wire payload summarizer (Layer-2 LLM-based compaction).
	var payloadSummarizer governance.PayloadSummarizer
	if b.cfg.PayloadSummarizerEnabled && deps.LLM != nil {
		payloadSummarizer = governance.NewSubagentPayloadSummarizer(
			deps.LLM, deps.Model, summarizer.Prompt,
			b.cfg.PayloadThresholdTokens, b.cfg.PayloadMaxTokens,
		)
	}

	inv := agent.Invocation{
		Client: agent.NewNoStreamClient(deps.LLM, deps.Model, nil, deps.ReasoningEffort, msg.ThreadID),
		Executor: agent.ToolExecutorFunc(func(execCtx context.Context, calls []llm.ToolCall) agent.ExecutionSummary {
			summary := agent.ExecuteToolCalls(
				execCtx,
				deps.Tools,
				convCtx,
				userID,
				0,
				calls,
				b.terminalToolPolicyEnabled(),
				logger,
				agent.WithToolAttemptRecording(runID, deps.AttemptsRepo),
				agent.WithTokenJuice(deps.TokenJuiceEnabled),
				agent.WithPayloadSummarizer(payloadSummarizer),
				agent.WithConversationID(msg.ThreadID),
				agent.WithToolTimeout(toolTimeout),
			)
			if agent.PinnedOperationalWriteInCalls(calls) {
				b.invalidatePinnedOperational(msg.ThreadID)
			}
			return webExecutionSummary(summary)
		}),
		State:    convCtx,
		Tools:    toolDefs,
		PostTurn: b.postTurnConfig(runID, msg, logger, deps),
		Options: agent.Options{
			ContextEngine:           ctxEngine,
			MaxIterations:           deps.MaxIterations,
			MaxToolResultChars:      b.maxToolResultChars(),
			MicrocompactKeepRecent:  b.microcompactKeepRecent(),
			MicrocompactMinChars:    b.microcompactMinChars(),
			PhantomToolGuard:        deps.PhantomGuard,
			DisableInBatchDedup:     true,
			AllowNoToolFinalization: true,
			// text_response is the canonical terminal tool: Execute()
			// already returned the verbatim reply, so we must exit the
			// loop on it instead of feeding the wrapped tool_result back
			// to the LLM (which would re-call text_response, looping).
			// Telegram had this wired since day one; web was missing it
			// (discovered 2026-05-24 in single-tool-mode validation:
			// "Salutami usando text_response" → 7 tool_calls / 35s).
			TerminalToolPolicy: b.terminalToolPolicyEnabled(),
			TerminalHandler: func(_ context.Context, terminalTool, _ string, _ *agent.Stats) (string, bool, bool) {
				if terminalTool != "text_response" {
					return "", false, false
				}
				text := extractLastTextResponseArg(convCtx.Messages())
				if text == "" {
					return "", false, false
				}
				convCtx.AddAssistantMessage(text)
				return text, false, true
			},
			Logger: logger,
		},
		Logger: logger,
		OnEvent: func(event agent.Event) {
			if event.Type != agent.EventFinal {
				return
			}
			compacted := convCtx.CompactCompletedToolResults(conversation.ToolResultCompactionPolicy{
				MaxChars:       1200,
				KeepRecentFull: 2,
			})
			if compacted > 0 {
				logger.Info("web chat tool results compacted", "session_key", sessionKey, "count", compacted)
			}
			if err := convCtx.EnforceLimit(context.Background()); err != nil {
				logger.Error("web chat context enforcement failed", "session_key", sessionKey, "error", err)
			}
			session.Finish()
		},
	}
	if deps.Tools != nil {
		inv.ToolsProvider = func() []llm.ToolDefinition {
			return deps.Tools.DefinitionsFor(allowlist)
		}
		inv.Options.ToolResolver = deps.Tools.DefinitionFor
	}
	return inv, nil
}

func (b *webInvocationBuilder) renderPinnedOperational(ctx context.Context, threadID string, turnIdx int) string {
	if b == nil {
		return ""
	}
	return memoryindex.RenderPinnedSectionWithCache(ctx, b.postTurnStore, &b.priorityCaches, threadID, "web", turnIdx)
}

func (b *webInvocationBuilder) invalidatePinnedOperational(threadID string) {
	if b == nil {
		return
	}
	memoryindex.InvalidatePinnedSectionInCache(&b.priorityCaches, threadID, "web")
}

func webPostTurnFailureReader(repo attempts.Repo) agent.PostTurnFailureReader {
	if repo == nil {
		return nil
	}
	reader, _ := repo.(agent.PostTurnFailureReader)
	return reader
}

func (b *webInvocationBuilder) postTurnConfig(runID string, msg chat.InboundMessage, logger *slog.Logger, deps agent.RunTaskDeps) agent.PostTurnConfig {
	if b == nil || b.cfg == nil {
		return agent.PostTurnConfig{}
	}
	record := agent.TurnRecord{
		RunID:       runID,
		ThreadID:    msg.ThreadID,
		UserMessage: msg.Text,
	}
	cfg := agent.NewHeuristicPostTurnConfig(
		b.postTurnStore,
		b.postTurnReader,
		b.cfg.OP07NFailThreshold,
		b.cfg.OP07RecentTurns,
		logger,
		record,
	)
	if hook := agent.NewMemoryJudgeHook(deps.LLM, deps.Model, deps.ReasoningEffort, logger); hook != nil && b.postTurnStore != nil {
		cfg.Store = b.postTurnStore
		cfg.Record = record
		cfg.Hooks = append(cfg.Hooks, hook)
	}
	return cfg
}

func webSessionKey(userID, threadID string) string {
	threadID = strings.TrimSpace(threadID)
	if strings.HasPrefix(threadID, "web:") {
		return threadID
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = "anonymous"
	}
	if threadID == "" {
		threadID = "default"
	}
	return "web:" + userID + ":" + threadID
}

func webExecutionSummary(summary agent.ToolExecutionSummary) agent.ExecutionSummary {
	return agent.ExecutionSummary{
		LastResult:        summary.LastResult,
		FatalResult:       summary.FatalResult,
		ReadSkillNames:    summary.ReadSkillNames,
		TerminalTool:      summary.TerminalTool,
		Results:           summary.Results,
		AwaitingUserInput: summary.AwaitingUserInput,
	}
}

func (b *webInvocationBuilder) maxToolResultChars() int {
	if b == nil || b.cfg == nil {
		return 0
	}
	return b.cfg.MaxToolResultChars
}

func (b *webInvocationBuilder) microcompactKeepRecent() int {
	if b == nil || b.cfg == nil {
		return 0
	}
	return b.cfg.MicrocompactKeepRecent
}

func (b *webInvocationBuilder) microcompactMinChars() int {
	if b == nil || b.cfg == nil {
		return 0
	}
	return b.cfg.MicrocompactMinChars
}

func (b *webInvocationBuilder) terminalToolPolicyEnabled() bool {
	if b == nil || b.cfg == nil {
		return true
	}
	return config.NormalizeTerminalToolPolicy(b.cfg.TerminalToolPolicy) != "off"
}

// extractLastTextResponseArg scans messages newest-to-oldest looking for an
// assistant tool_call to text_response and returns the trimmed `text`
// argument verbatim. Used by web's TerminalHandler to close the loop with
// the exact reply the model passed in, bypassing the untrusted-output
// wrapper that ExecuteToolCalls applies to every result.
func extractLastTextResponseArg(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		for j := len(m.ToolCalls) - 1; j >= 0; j-- {
			call := m.ToolCalls[j]
			if call.Name != "text_response" {
				continue
			}
			text, _ := call.Arguments["text"].(string)
			text = strings.TrimSpace(text)
			if text != "" {
				return text
			}
		}
	}
	return ""
}
