package main

import (
	"log/slog"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/pimprovider"
)

func wirePIMProviderApps(server *agui.Server, chat *chatEnv) {
	store, err := pimprovider.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Error("PIM provider-app store unavailable; provider routes and managed account creates answer 503", "err", err)
		return
	}
	server.SetPIMProviderApps(store)
}
