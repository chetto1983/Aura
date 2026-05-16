package webadapter

import (
	"context"
	"errors"
	"time"

	"github.com/aura/aura/internal/chat"
)

// HubReceiver is the narrow subset of chat.Hub this package needs. Lets
// tests inject a fake without dragging the full Hub.
type HubReceiver interface {
	ReceiveMessage(ctx context.Context, msg chat.InboundMessage) (*chat.Run, error)
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
func (s *ChatService) Chat(ctx context.Context, userID, message string) (ChatReply, error) {
	if s == nil || s.hub == nil || s.router == nil {
		return ChatReply{}, errors.New("webadapter: hub unavailable")
	}
	msg := chat.InboundMessage{
		Channel:     chat.ChannelWeb,
		PrincipalID: userID,
		Text:        message,
		Mode:        chat.DeliveryModeDeferred,
		CreatedAt:   time.Now().UTC(),
	}
	run, runErr := s.hub.ReceiveMessage(ctx, msg)
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
			RunID:     run.ID,
			Reply:     res.FinalContent,
			ElapsedMs: res.ElapsedMs,
			LLMCalls:  res.LLMCalls,
			ToolCalls: res.ToolCalls,
			Tokens:    res.TokensTotal,
		}, runErr
	}
	return ChatReply{
		RunID:     run.ID,
		Reply:     res.FinalContent,
		ElapsedMs: res.ElapsedMs,
		LLMCalls:  res.LLMCalls,
		ToolCalls: res.ToolCalls,
		Tokens:    res.TokensTotal,
	}, nil
}
