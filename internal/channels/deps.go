// Package channels is the daemon channels framework (Phase 13 / Slice 9a, UX-02):
// a Channel interface + Registry that the bootServe lifecycle mounts as a
// fail-soft subsystem, with Telegram (internal/channels/telegram) the first real
// channel.
//
// This file is the dependency ANCHOR for Phase 13's Wave-1 substrate. The
// external deps (telebot.v4 transport, qrterminal onboarding QR) are pinned by
// amendment #58 and gated through the package legitimacy checkpoint (Plan 13-01
// Task 1), but their first real consumers land in later Wave plans
// (13-05..13-08). Without a blank import here `go mod tidy` would drop the pins
// from go.mod (nothing imports them yet), breaking the amendment #58 CI pin gate
// (the literal `gopkg.in/telebot.v4 v4.0.0-beta.9` grep). The blank imports keep
// them in the DIRECT require block until the consumers replace them. This mirrors
// the project's existing anchor idiom (internal/cron/tzdata.go blank-imports
// time/tzdata; the OTel train is anchored the same way).
//
// NOTE (Plan 13-03): `golang.org/x/image` is no longer anchored here — its real
// consumer internal/channels/telegram/tables.go now imports opentype + gomono +
// gomonobold genuinely, keeping x/image DIRECT at the amendment-#58 pin v0.41.0
// (verified: `go mod tidy` + `go build ./...` stay green after the anchor's
// removal). telebot/qrterminal anchors remain until their downstream consumers land.
package channels

import (
	// telebot.v4 — Telegram bot transport (polling, Send, sendDocument/Photo/Voice),
	// consumed by internal/channels/telegram/bot.go (Plan 13-05).
	_ "gopkg.in/telebot.v4"

	// qrterminal/v3 — terminal ASCII QR for the onboarding deep-link
	// (internal/setup onboarding, Plan 13-07; D-04).
	_ "github.com/mdp/qrterminal/v3"
)
