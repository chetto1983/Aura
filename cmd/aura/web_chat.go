package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/agentdef"
	"github.com/aura/aura/internal/agent/agents/summarizer"
	"github.com/aura/aura/internal/agent/governance"
	"github.com/aura/aura/internal/agent/tools/attempts"
	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/agentcore"
	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/budget"
	webadapter "github.com/aura/aura/internal/channels/web"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	auraskills "github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/telegram"
)

func newWebInvocationBuilder(
	cfg *config.Config,
	deps *telegram.Deps,
	logger *slog.Logger,
	budgetRuntime budget.Runtime,
	archiveRepo conversation.ArchiveRepository,
	archiveAppender conversation.TurnAppender,
	streamRouter *webadapter.StreamRouter,
) (*webInvocationBuilder, *agent.SessionStore) {
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
		depsGetter:      depsGetter,
		sessionStore:    sessionStore,
		cfg:             cfg,
		logger:          logger,
		postTurnStore:   deps.MemoryStore,
		postTurnReader:  postTurnReader,
		reflectionHook:  newWebReflectionHook(deps, cfg),
		skillsLoader:    deps.Skills,
		loc:             deps.Loc,
		budgetRuntime:   budgetRuntime,
		archiveRepo:     archiveRepo,
		archiveAppender: archiveAppender,
		streamRouter:    streamRouter,
	}
	if deps.WikiStore != nil {
		builder.wikiTOCFn = deps.WikiStore.GetCachedTOC
	}
	return builder, sessionStore
}

func newWebChatService(hub *chat.Hub, router *webadapter.Router, sessionStore *agent.SessionStore) (api.ChatService, error) {
	if sessionStore == nil {
		return nil, nil
	}
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
	return webReplyToAPI(reply), err
}

func (a *apiChatServiceAdapter) AnswerChat(ctx context.Context, userID, threadID, questionID, answer string, selectedOptionIDs []string) (api.ChatReply, error) {
	reply, err := a.svc.Answer(ctx, userID, threadID, questionID, answer, selectedOptionIDs)
	return webReplyToAPI(reply), err
}

func webReplyToAPI(reply webadapter.ChatReply) api.ChatReply {
	return api.ChatReply{
		Reply:           reply.Reply,
		ElapsedMs:       reply.ElapsedMs,
		LLMCalls:        reply.LLMCalls,
		ToolCalls:       reply.ToolCalls,
		Tokens:          reply.Tokens,
		CacheHit:        reply.CacheHit,
		ToolsUsed:       reply.ToolsUsed,
		BudgetWarning:   reply.BudgetWarning,
		PendingQuestion: webPendingQuestionToAPI(reply.PendingQuestion),
	}
}

func webPendingQuestionToAPI(q *webadapter.PendingQuestion) *api.PendingQuestion {
	if q == nil {
		return nil
	}
	return &api.PendingQuestion{
		ID:       q.ID,
		Question: q.Question,
		Options:  append([]string(nil), q.Options...),
		Kind:     q.Kind,
	}
}

type webInvocationBuilder struct {
	depsGetter      func() agent.RunTaskDeps
	sessionStore    *agent.SessionStore
	cfg             *config.Config
	logger          *slog.Logger
	postTurnStore   *memoryindex.Store
	postTurnReader  agent.PostTurnFailureReader
	reflectionHook  agent.PostTurnHook
	priorityCaches  sync.Map
	skillsLoader    *auraskills.Loader
	wikiTOCFn       func() string
	loc             *time.Location
	budgetRuntime   budget.Runtime
	archiveRepo     conversation.ArchiveRepository
	archiveAppender conversation.TurnAppender
	streamRouter    *webadapter.StreamRouter
	hub             *chat.Hub
}

func (b *webInvocationBuilder) AttachHub(hub *chat.Hub) {
	if b != nil {
		b.hub = hub
	}
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
			if block := memoryindex.OperationalLessonsBlock(docs, 1600); block != "" {
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
	allowlist := agent.CleanToolList(agent.AlwaysOnCore)
	var delegateTools []toolregistry.Tool
	var toolManifest string
	if deps.Tools != nil {
		delegateTools = agentdef.WithArchetypeDelegates(
			agentdef.DefaultChatArchetype,
			deps.AgentDefs,
			deps.DelegateRunner,
			[]string{agentdef.DefaultChatArchetype},
			allowlist,
			logger,
		)
		toolManifest = toolregistry.RenderSplitManifest(agentdef.AppendUniqueFullDefinitions(
			deps.Tools.FullDefinitions(),
			agentdef.DelegateFullDefinitions(delegateTools),
		))
	}
	pinned := b.renderPinnedOperational(ctx, msg.ThreadID, turnIdx)
	loc := b.loc
	if loc == nil {
		loc = time.Local
	}
	promptPlan := agent.ComposeAgentPrompt(b.cfg, loc, overlay, pinned, skillsBlock, toolManifest, wikiTOC, time.Now())
	system := promptPlan.Content
	convCtx.SetSystemMessage(system)
	if grounding := agent.BuildTurnGroundingCapsule(ctx, b.postTurnStore, webUserInputText(msg)); grounding != "" {
		convCtx.SetSearchContext(grounding)
	}
	addedUserInput, resumeErr := b.addWebUserInput(ctx, run, msg, convCtx, session, logger)
	if resumeErr != nil {
		return agent.Invocation{}, resumeErr
	}
	if !addedUserInput {
		convCtx.AddUserMessage(webUserInputText(msg))
	}
	preLoopIdx := convCtx.MessageCount()
	turnStart := time.Now()
	runID := ""
	if run != nil {
		runID = run.ID
	}
	var toolDefs []llm.ToolDefinition
	if deps.Tools != nil {
		toolDefs = agentdef.AppendUniqueLLMDefinitions(
			deps.Tools.DefinitionsFor(allowlist),
			agentdef.DelegateLLMDefinitions(delegateTools),
		)
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

	var toolsProvider func() []llm.ToolDefinition
	var toolResolver func(string) (llm.ToolDefinition, bool)
	if deps.Tools != nil {
		delegateDefs := agentdef.DelegateLLMDefinitions(delegateTools)
		toolsProvider = func() []llm.ToolDefinition {
			return agentdef.AppendUniqueLLMDefinitions(deps.Tools.DefinitionsFor(allowlist), delegateDefs)
		}
		toolResolver = func(name string) (llm.ToolDefinition, bool) {
			if def, ok := agentdef.ResolveDelegateDefinition(delegateTools, name); ok {
				return def, true
			}
			return deps.Tools.DefinitionFor(name)
		}
	}
	chatClient := agent.NewNoStreamClient(deps.LLM, deps.Model, nil, deps.ReasoningEffort, msg.ThreadID)
	if msg.Mode == chat.DeliveryModeStreaming {
		chatClient = webadapter.NewStreamingChatClient(deps.LLM, deps.Model, deps.ReasoningEffort, msg.ThreadID, b.streamRouter)
	}
	inv, err := (agentcore.Builder{}).Build(agentcore.InvocationInput{
		Client: chatClient,
		Executor: agent.ToolExecutorFunc(func(execCtx context.Context, calls []llm.ToolCall) agent.ExecutionSummary {
			b.announceDelegates(execCtx, msg, runID, calls, delegateTools)
			summary := agent.ExecuteToolCalls(
				execCtx,
				agentdef.NewToolOverlay(deps.Tools, delegateTools),
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
		State:         convCtx,
		Tools:         toolDefs,
		ToolsProvider: toolsProvider,
		AgentDefs:     deps.AgentDefs,
		PostTurn:      b.postTurnConfig(runID, msg, logger, deps),
		Options: agent.Options{
			ContextEngine:           ctxEngine,
			MaxIterations:           deps.MaxIterations,
			MaxToolResultChars:      b.maxToolResultChars(),
			MicrocompactKeepRecent:  b.microcompactKeepRecent(),
			MicrocompactMinChars:    b.microcompactMinChars(),
			DisableInBatchDedup:     true,
			AllowNoToolFinalization: true,
			BeforeLLM: func() (string, bool) {
				if b.budgetRuntime != nil && b.budgetRuntime.IsHardBudgetExceeded() {
					logger.Warn("web chat hard budget exceeded", "session_key", sessionKey)
					return "Budget limit reached. LLM calls are temporarily halted.", true
				}
				return "", false
			},
			RecordUsage: func(usage llm.TokenUsage) {
				if b.budgetRuntime != nil {
					b.budgetRuntime.RecordUsage(usage)
				}
			},
			EstimateCost: func(usage llm.TokenUsage) float64 {
				return agent.EstimateUsageCost(usage, b.cfg.CostInputPerMTokens, b.cfg.CostOutputPerMTokens)
			},
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
			ToolResolver: toolResolver,
			Logger:       logger,
		},
		Logger: logger,
		OnEvent: func(event agent.Event) {
			switch event.Type {
			case agent.EventStats:
				if warning := b.consumeSoftBudgetWarning(); warning != "" && run != nil {
					if run.Metadata == nil {
						run.Metadata = map[string]any{}
					}
					run.Metadata["budget_warning"] = warning
				}
				return
			case agent.EventFinal:
			default:
				return
			}
			b.archiveWebTurn(context.Background(), logger, msg, userID, convCtx, preLoopIdx, event.Stats, time.Since(turnStart).Milliseconds())
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
	})
	if err != nil {
		return agent.Invocation{}, err
	}
	return inv, nil
}
