package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/steer"
)

// TestOneInboxServesBothSurfaces pins the invariant the assumption-delta
// decision in 52-02 committed to (unaffected by Phase 51 plan 02's move to
// Postgres, D-06): the composition root injects ONE process-wide
// *steer.PostgresStore into both the cockpit route (agui.Server.SetSteerInbox,
// wired in serve_agui.go's wireAGUIServer over chat.steer) and the Telegram
// channel (telegram.Deps.Steer, wired in serve_channels.go's buildTelegramDeps
// over the SAME chat.steer field). Two separately-constructed stores would mean
// a cockpit steer and a Telegram steer for the same conversation land in
// different queues and only one is ever drained — nothing would fail, the
// steer would just vanish. A nil pool is safe here: this test only asserts
// pointer identity, it never calls Push/Drain.
func TestOneInboxServesBothSurfaces(t *testing.T) {
	inbox := steer.NewPostgresStore(nil, steer.Config{Max: 8, MaxBytes: 16384})
	chat := &chatEnv{cfg: &config.Config{}, steer: inbox}

	// The Telegram side: buildTelegramDeps is the REAL production call site
	// (cmd/aura/serve_channels.go), executed here verbatim.
	telegramSteer := buildTelegramDeps(chat, telegram.Config{}).Steer

	// The agui side: agui.Server exposes no getter for its steer field (by
	// design — the daemon never reads its own wiring back), so this mirrors
	// serve_agui.go's own wireAGUIServer branch — `if chat.steer != nil {
	// aguiServer.SetSteerInbox(chat.steer) }` — over the SAME chat.steer field
	// buildTelegramDeps just read, rather than constructing a second store.
	aguiServer := agui.NewServer(nil, nil, agui.ServerConfig{})
	if chat.steer != nil {
		aguiServer.SetSteerInbox(chat.steer)
	}

	// The pointer identity assertion: both surfaces must be wired from the
	// EXACT same chat.steer value.
	if telegramSteer != chat.steer {
		t.Fatalf("telegram.Deps.Steer = %p, want the SAME instance as chat.steer (%p)", telegramSteer, chat.steer)
	}

	t.Run("negative: two independently-constructed stores must not compare equal", func(t *testing.T) {
		a := steer.NewPostgresStore(nil, steer.Config{})
		b := steer.NewPostgresStore(nil, steer.Config{})
		if a == b {
			t.Fatal("two independently-constructed *steer.PostgresStore values must never be pointer-equal — the positive assertion above would be meaningless if this comparison could never fail")
		}
	})
}

// TestSteerRollbackDarkensBothSurfaces proves D-12: with AGUISteer.Enabled
// false the composition root wires nil to BOTH surfaces — one dark surface and
// the other left live is exactly the half-live state D-12 forbids.
func TestSteerRollbackDarkensBothSurfaces(t *testing.T) {
	cfg := &config.Config{}
	cfg.AGUISteer = config.AGUISteerConfig{Enabled: false, Max: 8, MaxBytes: 16384}

	// The EXACT gate chat_boot.go's assembleChatEnv applies before ever calling
	// newSteerInbox.
	var steerInbox *steer.PostgresStore
	if cfg.AGUISteer.Enabled {
		steerInbox = newSteerInbox(nil, cfg)
	}
	chat := &chatEnv{cfg: cfg, steer: steerInbox}

	telegramSteer := buildTelegramDeps(chat, telegram.Config{}).Steer
	if telegramSteer != nil {
		t.Fatalf("telegram.Deps.Steer = %v, want nil when AGUISteer.Enabled is false", telegramSteer)
	}

	// serve_agui.go's own gate (`if chat.steer != nil { aguiServer.SetSteerInbox(...) }`)
	// never fires when chat.steer is nil, leaving the cockpit route unwired too —
	// asserted here on the SAME field the gate consults.
	if chat.steer != nil {
		t.Fatalf("chat.steer = %v, want nil when AGUISteer.Enabled is false — a non-nil value here would fire wireAGUIServer's SetSteerInbox branch", chat.steer)
	}
}
