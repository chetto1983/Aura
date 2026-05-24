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

const (
	webChatMaxMessages = 30
	webChatIdleTTL     = 30 * time.Minute
)

func newHubBackedWebChatService(cfg *config.Config, deps *telegram.Deps, logger *slog.Logger) (api.ChatService, error) {
	depsGetter := newWebChatDepsGetter(cfg, deps)
	if depsGetter == nil {
		return nil, nil
	}
	sessions := newWebChatSessions()
	postTurnReader := webPostTurnFailureReader(deps.AttemptsRepo)
	if postTurnReader == nil && deps.Pool != nil {
		postTurnReader = attempts.NewSQLiteRepo(deps.Pool)
	}
	builder := &webInvocationBuilder{
		depsGetter:     depsGetter,
		sessions:       sessions,
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
	return &apiChatServiceAdapter{svc: svc, sessions: sessions}, nil
}

type apiChatServiceAdapter struct {
	svc      *webadapter.ChatService
	sessions *webChatSessions
}

func (a *apiChatServiceAdapter) Chat(ctx context.Context, userID, threadID, message string) (api.ChatReply, error) {
	reply, err := a.svc.Chat(ctx, userID, threadID, message)
	if err != nil {
		a.sessions.rollback(reply.RunID)
	} else {
		a.sessions.commit(reply.RunID, userID, threadID)
	}
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
	sessions       *webChatSessions
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
	turnIdx := b.sessions.messageCount(userID, msg.ThreadID)

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
	runID := ""
	if run != nil {
		runID = run.ID
	}
	state := b.sessions.begin(runID, userID, msg.ThreadID, system, msg.Text)
	allowlist := cleanWebToolList(nil)
	var toolDefs []llm.ToolDefinition
	if deps.Tools != nil {
		allowlist = cleanWebToolList(deps.Tools.Names())
		toolDefs = deps.Tools.DefinitionsFor(allowlist)
	}
	toolTimeout := deps.ToolTimeout
	if toolTimeout <= 0 {
		toolTimeout = deps.Timeout
	}
	logger := deps.Logger
	if logger == nil {
		logger = b.logger
	}
	if logger == nil {
		logger = slog.Default()
	}
	// US-CTX-04/05: wire AutoCompactEngine when enabled and MaxConversationTokens is set.
	var ctxEngine conversation.ContextEngine
	if b.cfg.CTXEngine != "default" && b.cfg.MaxConversationTokens > 0 && deps.LLM != nil {
		compressor := conversation.NewContextCompressor(deps.LLM, b.cfg.ModelContextWindow)
		ctxEngine = conversation.NewAutoCompactEngine(compressor, b.cfg.MaxConversationTokens, b.cfg.CTXCompactScope)
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
		Executor: &webToolExecutor{
			tools:                 deps.Tools,
			state:                 state,
			logger:                logger,
			allowlist:             allowlist,
			userID:                userID,
			conversationID:        msg.ThreadID,
			runID:                 runID,
			attemptsRepo:          deps.AttemptsRepo,
			maxChars:              b.maxToolResultChars(),
			toolTimeout:           toolTimeout,
			terminalPolicyEnabled: b.terminalToolPolicyEnabled(),
			tokenJuiceEnabled:     deps.TokenJuiceEnabled,
			payloadSummarizer:     payloadSummarizer,
			invalidatePinned: func() {
				b.invalidatePinnedOperational(msg.ThreadID)
			},
		},
		State:    state,
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
				text := extractLastTextResponseArg(state.Messages())
				if text == "" {
					return "", false, false
				}
				state.AddAssistantMessage(text)
				return text, false, true
			},
			Logger: logger,
		},
		Logger: logger,
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
		b.cfg.OP07HeuristicEnabled,
		b.postTurnStore,
		b.postTurnReader,
		b.cfg.OP07NFailThreshold,
		b.cfg.OP07RecentTurns,
		logger,
		record,
	)
	if hook := agent.NewMemoryJudgeHook(b.cfg.MemoryJudgeEnabled, deps.LLM, deps.Model, deps.ReasoningEffort, logger); hook != nil && b.postTurnStore != nil {
		cfg.Store = b.postTurnStore
		cfg.Record = record
		cfg.Hooks = append(cfg.Hooks, hook)
	}
	return cfg
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

type webChatSessions struct {
	mu       sync.Mutex
	sessions map[string][]llm.Message
	active   map[string]*webAgentState
	updated  map[string]time.Time
}

func newWebChatSessions() *webChatSessions {
	return &webChatSessions{
		sessions: make(map[string][]llm.Message),
		active:   make(map[string]*webAgentState),
		updated:  make(map[string]time.Time),
	}
}

func (s *webChatSessions) begin(runID, userID, threadID, system, message string) *webAgentState {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	key := webChatSessionKey(userID, threadID)
	messages := llm.CloneMessages(s.sessions[key])
	messages = setSystemMessage(messages, system)
	messages = append(messages, llm.Message{Role: "user", Content: message})
	state := &webAgentState{messages: messages}
	if runID != "" {
		s.active[runID] = state
	}
	return state
}

func (s *webChatSessions) messageCount(userID, threadID string) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions[webChatSessionKey(userID, threadID)])
}

func (s *webChatSessions) commit(runID, userID, threadID string) {
	if runID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.active[runID]
	if !ok {
		return
	}
	delete(s.active, runID)
	key := webChatSessionKey(userID, threadID)
	s.sessions[key] = trimWebMessages(state.Messages())
	s.updated[key] = time.Now()
}

func (s *webChatSessions) rollback(runID string) {
	if runID == "" {
		return
	}
	s.mu.Lock()
	delete(s.active, runID)
	s.mu.Unlock()
}

func (s *webChatSessions) gcLocked() {
	cutoff := time.Now().Add(-webChatIdleTTL)
	for userID, updated := range s.updated {
		if updated.Before(cutoff) {
			delete(s.sessions, userID)
			delete(s.updated, userID)
		}
	}
}

func webChatSessionKey(userID, threadID string) string {
	userID = strings.TrimSpace(userID)
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		threadID = "default"
	}
	return userID + "\x00" + threadID
}

type webAgentState struct {
	messages []llm.Message
}

var _ agent.State = (*webAgentState)(nil)
var _ agent.PhantomCorrector = (*webAgentState)(nil)

func (s *webAgentState) Messages() []llm.Message {
	return llm.CloneMessages(s.messages)
}

func (s *webAgentState) TrackTokens(llm.TokenUsage) {}

func (s *webAgentState) AddUserMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "user", Content: content})
}

func (s *webAgentState) AddAssistantMessage(content string) {
	s.messages = append(s.messages, llm.Message{Role: "assistant", Content: content})
}

func (s *webAgentState) AddAssistantToolCallMessage(content string, calls []llm.ToolCall) {
	s.messages = append(s.messages, llm.Message{Role: "assistant", Content: content, ToolCalls: calls})
}

func (s *webAgentState) AddToolResultMessage(id, content string) {
	s.messages = append(s.messages, llm.Message{Role: "tool", Content: content, ToolCallID: id})
}

// webToolExecutor and its helpers live in web_chat_executor.go — they were
// hoisted out 2026-05-24 to keep this file under the 600-LOC cap when the
// TerminalHandler wiring landed for text_response. See
// extractLastTextResponseArg below for the helper that bridges to it.

func setSystemMessage(messages []llm.Message, system string) []llm.Message {
	system = strings.TrimSpace(system)
	if system == "" {
		return messages
	}
	if len(messages) > 0 && messages[0].Role == "system" {
		messages[0].Content = system
		return messages
	}
	return append([]llm.Message{{Role: "system", Content: system}}, messages...)
}

// extractLastTextResponseArg scans messages newest-to-oldest looking for an
// assistant tool_call to text_response and returns the trimmed `text`
// argument verbatim. Used by web's TerminalHandler to close the loop with
// the exact reply the model passed in, bypassing the untrusted-output
// wrapper that webToolExecutor applies to every result.
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

func trimWebMessages(messages []llm.Message) []llm.Message {
	if len(messages) <= webChatMaxMessages {
		return llm.CloneMessages(messages)
	}
	drop := len(messages) - webChatMaxMessages
	return llm.CloneMessages(messages[drop:])
}

// cloneToolArgs + limitToolContent live in web_chat_executor.go.
