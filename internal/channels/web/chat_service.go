package webadapter

import (
	"context"
	"errors"
	"strings"

	"github.com/aura/aura/internal/chat"
)

// HubReceiver is the narrow subset of chat.Hub this package needs. Lets
// tests inject a fake without dragging the full Hub.
type HubReceiver interface {
	Receive(ctx context.Context, inboundChannel chat.Channel, raw any) (*chat.Run, error)
}

// ChatReply mirrors internal/api.ChatReply so the API package can call into
// the bridge without importing webadapter (avoiding an import cycle). The
// HTTP handler does the final JSON marshal.
type ChatReply struct {
	RunID     string
	Reply     string
	ElapsedMs int64
	LLMCalls  int
	ToolCalls int
	Tokens    int
	// CacheHit is true when at least one LLM call in this run was served
	// (partially) from the provider's prompt cache.
	CacheHit bool
	// ToolsUsed lists the unique tool names invoked during the run, in the
	// order first seen. Captured from EventToolStart events by Buffer.apply.
	// Used by quality-bench to verify tool-selection (e.g., did Aura use
	// web_search when only wiki tools were appropriate?).
	ToolsUsed []string
	// BudgetWarning is populated once when the shared budget runtime crosses
	// its soft threshold during this turn.
	BudgetWarning string
	// PendingQuestion is non-nil when ask_user paused the run and the client
	// must answer before Aura can continue.
	PendingQuestion *PendingQuestion
}

// ChatService is the production bridge from api.ChatService onto chat.Hub.
// Pattern:
//
//  1. Boot wires ONE Router into the Hub via Hub.RegisterOutbound.
//  2. Each HTTP request constructs a ChatService and calls Chat.
//  3. Chat dispatches the InboundMessage through the Hub. The Router
//     captures events per-RunID; once ReceiveMessage returns the
//     buffer is finalised.
//  4. Chat reads the buffer by Run.ID, drops it, and returns ChatReply.
type ChatService struct {
	hub    HubReceiver
	router *Router
}

// NewChatService wires a ChatService against an existing chat.Hub and
// its registered Router. Returns nil when either is nil so callers can
// pass through unconditionally (matching internal/telegram.NewChatPipeService).
func NewChatService(hub HubReceiver, router *Router) *ChatService {
	if hub == nil || router == nil {
		return nil
	}
	return &ChatService{hub: hub, router: router}
}

// Chat dispatches a single chat turn through the Hub and waits for the
// terminal Result. Returns a ChatReply identical in shape to the public
// api.ChatReply so the HTTP handler stays byte-identical.
func (s *ChatService) Chat(ctx context.Context, userID, threadID, message string) (ChatReply, error) {
	return s.dispatch(ctx, userID, threadID, message, nil)
}

// Answer resumes a run paused by ask_user by recording a question answer and
// dispatching the answer text through the same Hub path as a normal web turn.
func (s *ChatService) Answer(ctx context.Context, userID, threadID, questionID, answer string, selectedOptionIDs []string) (ChatReply, error) {
	return s.dispatch(ctx, userID, threadID, answer, &chat.QuestionAnswer{
		QuestionID:        strings.TrimSpace(questionID),
		SelectedOptionIDs: append([]string(nil), selectedOptionIDs...),
		FreeText:          strings.TrimSpace(answer),
	})
}

func (s *ChatService) dispatch(ctx context.Context, userID, threadID, message string, answer *chat.QuestionAnswer) (ChatReply, error) {
	if s == nil || s.hub == nil || s.router == nil {
		return ChatReply{}, errors.New("webadapter: hub unavailable")
	}
	run, runErr := s.hub.Receive(ctx, chat.ChannelWeb, InboundRequest{
		UserID:   userID,
		ThreadID: threadID,
		Message:  message,
		Question: answer,
	})
	if run == nil {
		return ChatReply{}, runErr
	}
	buf := s.router.Reserve(run.ID)
	defer s.router.Drop(run.ID)
	// ReceiveMessage blocks until terminal; Wait returns immediately.
	res, err := buf.Wait(ctx)
	if err != nil {
		return ChatReply{}, err
	}
	if runErr != nil {
		return ChatReply{
			RunID:         run.ID,
			Reply:         res.FinalContent,
			ElapsedMs:     res.ElapsedMs,
			LLMCalls:      res.LLMCalls,
			ToolCalls:     res.ToolCalls,
			Tokens:        res.TokensTotal,
			CacheHit:      res.CacheReadTokens > 0,
			ToolsUsed:     res.ToolsUsed,
			BudgetWarning: res.BudgetWarning,
			PendingQuestion: clonePendingQuestion(
				res.PendingQuestion,
			),
		}, runErr
	}
	return ChatReply{
		RunID:         run.ID,
		Reply:         res.FinalContent,
		ElapsedMs:     res.ElapsedMs,
		LLMCalls:      res.LLMCalls,
		ToolCalls:     res.ToolCalls,
		Tokens:        res.TokensTotal,
		CacheHit:      res.CacheReadTokens > 0,
		ToolsUsed:     res.ToolsUsed,
		BudgetWarning: res.BudgetWarning,
		PendingQuestion: clonePendingQuestion(
			res.PendingQuestion,
		),
	}, nil
}

func clonePendingQuestion(q *PendingQuestion) *PendingQuestion {
	if q == nil {
		return nil
	}
	return &PendingQuestion{
		ID:       q.ID,
		Question: q.Question,
		Options:  append([]string(nil), q.Options...),
		Kind:     q.Kind,
	}
}

func ThreadID(userID, threadID string) string {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		userID = "anonymous"
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		threadID = "default"
	}
	return "web:" + userID + ":" + threadID
}
