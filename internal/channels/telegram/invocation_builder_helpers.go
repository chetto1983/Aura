package telegramadapter

import (
	"context"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/agent/agentdef"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/memoryindex"
	tgtelegram "github.com/aura/aura/internal/telegram"

	tele "gopkg.in/telebot.v4"
)

// AttachHub completes the composition-root wiring after the shared chat.Hub
// exists. Build is lazy (called per-message), so installing these references
// after chat.New returns is safe.
func (ib *InvocationBuilder) AttachHub(hub *chat.Hub, outbound *Outbound) {
	if ib == nil {
		return
	}
	ib.hub = hub
	ib.outbound = outbound
}

func (ib *InvocationBuilder) executeToolCalls(ctx context.Context, c tele.Context, convCtx *conversation.Context, userID string, calls []llm.ToolCall, delegateTools []tools.Tool) agent.ToolExecutionSummary {
	ib.announceDelegates(c, calls, delegateTools)
	runner := agentdef.NewToolOverlay(ib.b.ToolRegistry(), delegateTools)
	return agent.ExecuteToolCalls(ctx, runner, convCtx, userID, tgtelegram.ChatIDFromTeleContext(c), calls, ib.b.TerminalToolPolicyEnabled(), ib.b.Logger(),
		agent.WithToolAttemptRecording(identity.RunIDFromContext(ctx), ib.b.ToolAttemptsRepo()),
		agent.WithTokenJuice(ib.b.TokenJuiceEnabled()),
		agent.WithPayloadSummarizer(ib.payloadSummarizer))
}

func (ib *InvocationBuilder) announceDelegates(c tele.Context, calls []llm.ToolCall, delegateTools []tools.Tool) {
	if ib == nil || ib.b == nil {
		return
	}
	for _, text := range agentdef.AnnouncementsForCalls(delegateTools, calls) {
		ib.b.SendAssistantText(c, text)
	}
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
