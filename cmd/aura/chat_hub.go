package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/aura/aura/internal/agent"
	telegramadapter "github.com/aura/aura/internal/channels/telegram"
	webadapter "github.com/aura/aura/internal/channels/web"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/telegram"
)

type sharedChatHub struct {
	hub             *chat.Hub
	webRouter       *webadapter.Router
	webStreamRouter *webadapter.StreamRouter
	webSessionStore *agent.SessionStore
}

// newSharedChatHub is the composition-root owner for the user-facing chat Hub.
// Web and Telegram share this Hub; cron keeps a separate lifecycle in wireBot.
func newSharedChatHub(
	cfg *config.Config,
	deps *telegram.Deps,
	bot *telegram.Bot,
	logger *slog.Logger,
	archiveRepo conversation.ArchiveRepository,
	archiveAppender conversation.TurnAppender,
) (*sharedChatHub, error) {
	if deps == nil {
		return nil, errors.New("shared chat hub: deps are required")
	}
	builders := make(map[chat.Channel]chat.InvocationBuilder, 2)

	webStreamRouter := webadapter.NewStreamRouter()
	webBuilder, webSessions := newWebInvocationBuilder(cfg, deps, logger, deps.Budget, archiveRepo, archiveAppender, webStreamRouter)
	if webBuilder != nil {
		builders[chat.ChannelWeb] = webBuilder.Build
	}

	var telegramBuilder *telegramadapter.InvocationBuilder
	var telegramOutbound *telegramadapter.Outbound
	if bot != nil {
		telegramBuilder = telegramadapter.NewInvocationBuilder(bot)
		builders[chat.ChannelTelegram] = telegramBuilder.Build
	}
	if len(builders) == 0 {
		return nil, errors.New("shared chat hub: no invocation builders configured")
	}

	loop, err := chat.NewAgentLoopAdapter(multiplexInvocationBuilder(builders))
	if err != nil {
		return nil, err
	}
	hub, err := chat.New(chat.Config{Loop: loop, LifecycleStore: deps.RunStore, Logger: logger})
	if err != nil {
		return nil, err
	}
	if webBuilder != nil {
		webBuilder.AttachHub(hub)
	}

	webRouter := webadapter.NewRouter()
	hub.RegisterInbound(webadapter.New())
	hub.RegisterOutbound(webRouter)
	hub.RegisterOutbound(webStreamRouter)

	if bot != nil {
		telegramOutbound = telegramadapter.NewOutbound(logger)
		telegramOutbound.SetTTS(bot.TTSClient())
		telegramOutbound.SetVoicePolicy(bot.VoicePolicy())
		telegramOutbound.SetDispatchDB(bot.Pool())
		hub.RegisterInbound(telegramadapter.New())
		hub.RegisterOutbound(telegramOutbound)
		telegramBuilder.AttachHub(hub, telegramOutbound)
	}

	return &sharedChatHub{
		hub:             hub,
		webRouter:       webRouter,
		webStreamRouter: webStreamRouter,
		webSessionStore: webSessions,
	}, nil
}

func multiplexInvocationBuilder(builders map[chat.Channel]chat.InvocationBuilder) chat.InvocationBuilder {
	return func(ctx context.Context, run *chat.Run, msg chat.InboundMessage) (agent.Invocation, error) {
		build, ok := builders[msg.Channel]
		if !ok || build == nil {
			return agent.Invocation{}, fmt.Errorf("shared chat hub: no invocation builder for channel %q", msg.Channel)
		}
		return build(ctx, run, msg)
	}
}
