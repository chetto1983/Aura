package main

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/agentdef"
	"github.com/aura/aura/internal/agent/tools/attempts"
	toolregistry "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/channels/askuser"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/memoryindex"
)

func (b *webInvocationBuilder) announceDelegates(ctx context.Context, msg chat.InboundMessage, runID string, calls []llm.ToolCall, delegateTools []toolregistry.Tool) {
	if b == nil || b.streamRouter == nil || msg.Mode != chat.DeliveryModeStreaming {
		return
	}
	for _, text := range agentdef.AnnouncementsForCalls(delegateTools, calls) {
		_ = b.streamRouter.Deliver(ctx, chat.OutboundEvent{
			Type:     chat.EventMessageDelta,
			RunID:    runID,
			ThreadID: msg.ThreadID,
			Content:  text + "\n\n",
		})
	}
}

func (b *webInvocationBuilder) addWebUserInput(
	ctx context.Context,
	run *chat.Run,
	msg chat.InboundMessage,
	convCtx *conversation.Context,
	session *agent.Session,
	logger *slog.Logger,
) (bool, error) {
	if b == nil || b.hub == nil || convCtx == nil {
		return false, nil
	}
	pending, hasDurablePending, pendingErr := b.hub.PendingQuestion(ctx, msg.ThreadID, msg.Channel)
	if pendingErr != nil && logger != nil {
		logger.Warn("web ask_user: pending question lookup failed", "error", pendingErr)
	}
	status, hasThreadStatus := b.hub.ThreadRunStatus(msg.ThreadID)
	if msg.Question == nil && !hasDurablePending && (!hasThreadStatus || status != chat.RunStatusWaitingForUser) {
		return false, nil
	}
	answerText := strings.TrimSpace(msg.Text)
	if answerText == "" && msg.Question != nil {
		answerText = strings.TrimSpace(msg.Question.FreeText)
		if answerText == "" && len(msg.Question.SelectedOptionIDs) == 1 {
			answerText = strings.TrimSpace(msg.Question.SelectedOptionIDs[0])
		}
	}
	resume, ok := askuser.PrepareResumeInput(answerText, convCtx.Messages(), pending.Options, pending.Kind, hasDurablePending)
	if !ok {
		return false, nil
	}
	if resume.Rejected {
		if run != nil {
			run.Status = chat.RunStatusWaitingForUser
		}
		if session != nil {
			session.Finish()
		}
		return true, fmt.Errorf("ask_user: out-of-range reply, question still pending")
	}
	if hasDurablePending {
		questionAnswer := chat.QuestionAnswer{
			QuestionID:        pending.ID,
			SelectedOptionIDs: resume.SelectedOptionIDs,
			FreeText:          resume.Content,
			AnsweredMessageID: msg.ID,
		}
		if msg.Question != nil {
			if id := strings.TrimSpace(msg.Question.QuestionID); id != "" {
				questionAnswer.QuestionID = id
			}
			if len(msg.Question.SelectedOptionIDs) > 0 {
				questionAnswer.SelectedOptionIDs = append([]string(nil), msg.Question.SelectedOptionIDs...)
			}
			if msg.Question.AnsweredMessageID != "" {
				questionAnswer.AnsweredMessageID = msg.Question.AnsweredMessageID
			}
		}
		if recordErr := b.hub.RecordQuestionAnswer(ctx, run, msg, questionAnswer); recordErr != nil {
			if logger != nil {
				logger.Warn("web ask_user: failed to record question answer",
					"question_id", questionAnswer.QuestionID,
					"error", recordErr)
			}
			if msg.Question != nil {
				return true, recordErr
			}
		}
	}
	if resume.HasPendingCall {
		convCtx.AddToolResultMessage(resume.CallID, resume.Content)
		return true, nil
	}
	convCtx.AddUserMessage("Answer to pending " + resume.Kind + " question: " + resume.Content)
	return true, nil
}

func (b *webInvocationBuilder) consumeSoftBudgetWarning() string {
	if b == nil || b.budgetRuntime == nil || !b.budgetRuntime.ShouldNotifySoftBudget() {
		return ""
	}
	status := b.budgetRuntime.Status()
	return fmt.Sprintf("Soft budget reached ($%.2f / $%.2f). LLM calls continue until hard budget is hit.", status.TotalCost, status.SoftBudget)
}

func (b *webInvocationBuilder) archiveWebTurn(
	ctx context.Context,
	logger *slog.Logger,
	msg chat.InboundMessage,
	userID string,
	convCtx *conversation.Context,
	preLoopIdx int,
	stats agent.Stats,
	elapsedMS int64,
) {
	if b == nil || b.archiveRepo == nil || b.archiveAppender == nil || convCtx == nil {
		return
	}
	chatID := webArchiveID("chat", msg.ThreadID)
	nextIndex := int64(0)
	if maxIdx, err := b.archiveRepo.MaxTurnIndex(ctx, chatID); err == nil {
		nextIndex = maxIdx + 1
	} else if logger != nil {
		logger.Warn("web archive: max turn_index lookup failed", "thread_id", msg.ThreadID, "error", err)
	}
	conversation.ArchiveConversationTurns(ctx, logger, b.archiveAppender, conversation.ArchiveTurnInput{
		Channel:      string(chat.ChannelWeb),
		ChatID:       chatID,
		UserID:       webArchiveID("user", userID),
		NextIndex:    nextIndex,
		UserText:     msg.Text,
		LoopMessages: convCtx.MessagesSince(preLoopIdx),
		LLMCalls:     stats.LLMCalls,
		ToolCalls:    stats.ToolCalls,
		ElapsedMS:    elapsedMS,
		TokensIn:     convCtx.TotalTokensUsed(),
	})
}

func webArchiveID(kind, value string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(kind))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strings.TrimSpace(value)))
	id := int64(h.Sum64() & 0x7fffffffffffffff)
	if id == 0 {
		return 1
	}
	return id
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
	if hook := b.reflectionHook; hook != nil && b.postTurnStore != nil {
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
