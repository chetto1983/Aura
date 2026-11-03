package main

import (
	"context"
	"log/slog"
	"time"
)

func sweepSignInBrowsers(ctx context.Context, chat *chatEnv) {
	if chat == nil || chat.sandboxRouter == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := chat.sandboxRouter.SweepSignIns(ctx); err != nil {
		slog.Warn("aura serve: could not remove sign-in desktops left by the previous process", "err", err)
	}
}
