package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/tools/attempts"
	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/api"
	webadapter "github.com/aura/aura/internal/channels/web"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
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
		a.sessions.commit(reply.RunID, userID)
	}
	return api.ChatReply{
		Reply:     reply.Reply,
		ElapsedMs: reply.ElapsedMs,
		LLMCalls:  reply.LLMCalls,
		ToolCalls: reply.ToolCalls,
		Tokens:    reply.Tokens,
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
	turnIdx := b.sessions.messageCount(userID)
	system := conversation.RenderSystemPrompt(time.Now(), time.Local)
	if pinned := b.renderPinnedOperational(ctx, msg.ThreadID, turnIdx); pinned != "" {
		system += "\n\n" + pinned
	}
	runID := ""
	if run != nil {
		runID = run.ID
	}
	state := b.sessions.begin(runID, userID, system, msg.Text)
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
	inv := agent.Invocation{
		Client: agent.NewNoStreamClient(deps.LLM, deps.Model, nil, deps.ReasoningEffort),
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
			invalidatePinned: func() {
				b.invalidatePinnedOperational(msg.ThreadID)
			},
		},
		State:    state,
		Tools:    toolDefs,
		PostTurn: b.postTurnConfig(runID, msg, logger, deps),
		Options: agent.Options{
			MaxIterations:           deps.MaxIterations,
			MaxToolResultChars:      b.maxToolResultChars(),
			MicrocompactKeepRecent:  b.microcompactKeepRecent(),
			MicrocompactMinChars:    b.microcompactMinChars(),
			PhantomToolGuard:        deps.PhantomGuard,
			DisableInBatchDedup:     true,
			AllowNoToolFinalization: true,
			Logger:                  logger,
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
	if b == nil || b.postTurnStore == nil {
		return ""
	}
	cacheKey := strings.TrimSpace(threadID)
	if cacheKey == "" {
		cacheKey = "web"
	}
	cacheAny, _ := b.priorityCaches.LoadOrStore(cacheKey, &memoryindex.PrioritySectionCache{})
	cache, ok := cacheAny.(*memoryindex.PrioritySectionCache)
	if !ok || cache == nil {
		return memoryindex.RenderPrioritySection(ctx, b.postTurnStore)
	}
	return cache.Render(ctx, b.postTurnStore, turnIdx)
}

func (b *webInvocationBuilder) invalidatePinnedOperational(threadID string) {
	if b == nil {
		return
	}
	cacheKey := strings.TrimSpace(threadID)
	if cacheKey == "" {
		cacheKey = "web"
	}
	if cacheAny, ok := b.priorityCaches.Load(cacheKey); ok {
		if cache, ok := cacheAny.(*memoryindex.PrioritySectionCache); ok {
			cache.InvalidatePinSection()
		}
	}
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

func (s *webChatSessions) begin(runID, userID, system, message string) *webAgentState {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	messages := llm.CloneMessages(s.sessions[userID])
	messages = setSystemMessage(messages, system)
	messages = append(messages, llm.Message{Role: "user", Content: message})
	state := &webAgentState{messages: messages}
	if runID != "" {
		s.active[runID] = state
	}
	return state
}

func (s *webChatSessions) messageCount(userID string) int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.sessions[userID])
}

func (s *webChatSessions) commit(runID, userID string) {
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
	s.sessions[userID] = trimWebMessages(state.Messages())
	s.updated[userID] = time.Now()
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

type webToolExecutor struct {
	tools                 *toolregistry.Registry
	state                 agent.State
	logger                *slog.Logger
	allowlist             []string
	userID                string
	conversationID        string
	runID                 string
	attemptsRepo          attempts.Repo
	maxChars              int
	toolTimeout           time.Duration
	terminalPolicyEnabled bool
	tokenJuiceEnabled     bool
	invalidatePinned      func()
}

func (e *webToolExecutor) ExecuteToolCalls(ctx context.Context, calls []llm.ToolCall) agent.ExecutionSummary {
	type outcome struct {
		id        string
		tool      string
		content   string
		awaiting  *toolregistry.ErrAwaitingUserInput
		readSkill string
		terminal  string
	}
	outcomes := make([]outcome, len(calls))
	pinnedWrite := webPinnedOperationalWriteInCalls(calls)
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		callCopy := call
		callCopy.Arguments = cloneToolArgs(call.Arguments)
		go func(i int, call llm.ToolCall) {
			defer wg.Done()
			startedAt := time.Now()
			raw, executedArgs, class, err := e.executeOne(ctx, call)
			var awaitErr *toolregistry.ErrAwaitingUserInput
			if errors.As(err, &awaitErr) {
				awaitErr.ToolCallID = call.ID
				agent.RecordToolAttempt(ctx, e.logger, e.attemptsRepo, agent.ToolAttemptRecordInput{
					RunID:     e.runID,
					ToolName:  call.Name,
					Arguments: executedArgs,
					StartedAt: startedAt,
					Elapsed:   time.Since(startedAt),
					Class:     "cancelled",
				})
				outcomes[i] = outcome{id: call.ID, awaiting: awaitErr}
				return
			}
			content := raw
			if err != nil {
				if class == "" {
					class = toolregistry.ClassifyToolError(err)
				}
				content = toolregistry.FormatToolError(err)
				if e.logger != nil {
					e.logger.Warn("web chat tool call failed", "tool", call.Name, "error", err)
				}
			}
			agent.RecordToolAttempt(ctx, e.logger, e.attemptsRepo, agent.ToolAttemptRecordInput{
				RunID:     e.runID,
				ToolName:  call.Name,
				Arguments: executedArgs,
				StartedAt: startedAt,
				Elapsed:   time.Since(startedAt),
				Class:     class,
				Err:       err,
			})
			if err == nil && e.tokenJuiceEnabled {
				content = agent.CompactToolOutput(e.logger, call.Name, executedArgs, content)
			}
			wrapped := agent.WrapUntrustedToolResult(call.Name, content)
			outcomes[i] = outcome{
				id:        call.ID,
				tool:      call.Name,
				content:   limitToolContent(wrapped, e.maxChars),
				readSkill: agent.SkillNameFromReadFileArgs(call.Arguments),
				terminal:  terminalToolName(call.Name, class == "" && err == nil && e.terminalPolicyEnabled),
			}
		}(i, callCopy)
	}
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		for i, call := range calls {
			if outcomes[i].id == "" {
				outcomes[i] = outcome{id: call.ID, content: toolregistry.FormatToolError(ctx.Err())}
			}
		}
	}
	if pinnedWrite && e.invalidatePinned != nil {
		e.invalidatePinned()
	}
	summary := agent.ExecutionSummary{Results: make(map[string]string, len(outcomes))}
	for _, item := range outcomes {
		if item.awaiting != nil {
			summary.AwaitingUserInput = item.awaiting
			continue
		}
		e.state.AddToolResultMessage(item.id, item.content)
		summary.LastResult = item.content
		summary.Results[item.id] = item.content
		if item.readSkill != "" && !slices.Contains(summary.ReadSkillNames, item.readSkill) {
			summary.ReadSkillNames = append(summary.ReadSkillNames, item.readSkill)
		}
		if summary.TerminalTool == "" && item.terminal != "" {
			summary.TerminalTool = item.terminal
		}
	}
	return summary
}

func webPinnedOperationalWriteInCalls(calls []llm.ToolCall) bool {
	for _, call := range calls {
		if call.Name != "propose_patch" {
			continue
		}
		action, _ := call.Arguments["action"].(string)
		if strings.TrimSpace(action) != "operational" {
			continue
		}
		priority, _ := call.Arguments["priority"].(string)
		switch memoryindex.NormalizePriority(priority) {
		case memoryindex.PriorityCritical, memoryindex.PriorityHigh:
			return true
		}
	}
	return false
}

func (e *webToolExecutor) executeOne(ctx context.Context, call llm.ToolCall) (string, map[string]any, string, error) {
	args := call.Arguments
	if len(e.allowlist) == 0 || !slices.Contains(e.allowlist, strings.ToLower(call.Name)) {
		return toolregistry.FormatFatalToolError(fmt.Errorf("tool %q is not allowed for this agent", call.Name)), args, "permission", nil
	}
	if e.tools == nil {
		return toolregistry.FormatFatalToolError(errors.New("tool registry unavailable")), args, "error", nil
	}
	toolCtx := ctx
	var cancel context.CancelFunc
	if e.toolTimeout > 0 {
		toolCtx, cancel = context.WithTimeout(toolCtx, e.toolTimeout)
		defer cancel()
	}
	toolCtx = toolregistry.WithAllowedToolNames(toolCtx, e.allowlist)
	if e.userID != "" {
		toolCtx = toolregistry.WithUserID(toolCtx, e.userID)
		convID := e.conversationID
		if convID == "" {
			convID = e.userID
		}
		toolCtx = toolregistry.WithConversationID(toolCtx, convID)
	}
	out, err := e.tools.Execute(toolCtx, call.Name, args)
	if err != nil {
		return out, args, toolregistry.ClassifyToolError(err), err
	}
	return out, args, "", nil
}

func cleanWebToolList(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func terminalToolName(name string, ok bool) string {
	if ok && agent.IsTerminalTool(name) {
		return name
	}
	return ""
}

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

func trimWebMessages(messages []llm.Message) []llm.Message {
	if len(messages) <= webChatMaxMessages {
		return llm.CloneMessages(messages)
	}
	drop := len(messages) - webChatMaxMessages
	return llm.CloneMessages(messages[drop:])
}

func cloneToolArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	return out
}

func limitToolContent(content string, maxChars int) string {
	if maxChars <= 0 {
		return content
	}
	runes := []rune(content)
	if len(runes) <= maxChars {
		return content
	}
	if maxChars <= 16 {
		return string(runes[:maxChars])
	}
	return string(runes[:maxChars-15]) + "\n...[truncated]"
}
