package main

import (
	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/cron/handlers"
	"github.com/chetto1983/aura/internal/idempotency"
)

// replaySweepBatch is how many replay payloads one nightly pass may clear: the idempotency
// store's own ceiling, since the pass runs once a day rather than in a loop.
const replaySweepBatch = 1000

// buildTelegramTokenSweep gives the nightly retention job the Telegram onboarding tokens a
// setup abandoned before consuming. Each setup mints one row with a short expiry, and before
// this nothing deleted the ones nobody redeemed.
//
// The return type is the INTERFACE and a missing pool returns a bare nil, which the retention
// handler skips; a nil-pointer sweep inside a non-nil interface would be called.
func buildTelegramTokenSweep(chat *chatEnv) handlers.Sweeper {
	if chat.pool == nil {
		return nil
	}
	return telegram.New(chat.pool)
}

// buildReplayBodySweep gives the nightly retention job the idempotency replay payloads past
// ReplayRetention. Readers stop honouring a payload at its replay_expires_at, but before this
// nothing cleared the bytes, so every retained response body stayed in
// aura.idempotency_operations. The store walks each identity in its own scoped transaction
// (the table is under fail-closed RLS), and the registry rows and audit links stay.
func buildReplayBodySweep(chat *chatEnv) handlers.Sweeper {
	if chat.pool == nil {
		return nil
	}
	return idempotency.New(chat.pool, idempotency.Config{ExpiryBatch: replaySweepBatch})
}
