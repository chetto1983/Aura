package main

import (
	"context"
	"errors"
	"io"

	"github.com/aura/aura/internal/api"
	webadapter "github.com/aura/aura/internal/channels/web"
	"github.com/aura/aura/internal/chat"
)

type webStreamChatService struct {
	hub    *chat.Hub
	router *webadapter.StreamRouter
}

func newWebStreamChatService(hub *chat.Hub, router *webadapter.StreamRouter) api.ChatStreamService {
	if hub == nil || router == nil {
		return nil
	}
	return &webStreamChatService{hub: hub, router: router}
}

func (s *webStreamChatService) ChatStream(ctx context.Context, userID, threadID, message string, w io.Writer, flush func()) error {
	if s == nil || s.hub == nil || s.router == nil {
		return errors.New("web stream chat service unavailable")
	}
	normalizedThreadID := webadapter.ThreadID(userID, threadID)
	end, err := s.router.Begin(normalizedThreadID, w, flush)
	if err != nil {
		_ = webadapter.WriteUIMessageError(w, err.Error())
		_ = webadapter.WriteUIMessageDone(w)
		if flush != nil {
			flush()
		}
		return err
	}
	defer end()
	_, err = s.hub.Receive(ctx, chat.ChannelWeb, webadapter.InboundRequest{
		UserID:   userID,
		ThreadID: threadID,
		Message:  message,
		Mode:     chat.DeliveryModeStreaming,
	})
	return err
}
