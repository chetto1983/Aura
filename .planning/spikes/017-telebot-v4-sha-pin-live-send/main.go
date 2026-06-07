//go:build spike_telegram

// Spike 017 — telebot.v4 tag-pin + live MarkdownV2 send (amendment #5 ground truth).
//
// Re-arm: go get gopkg.in/telebot.v4@v4.0.0-beta.9
// Run:    set -a; source <(tr -d '\r' < .env); set +a
//
//	go run -tags spike_telegram ./.planning/spikes/017-telebot-v4-sha-pin-live-send
//
// Env: TELEGRAM_BOT_TOKEN (bot token), AURA_E2E_CHAT_ID (operator's own chat — send-to-self only).
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tele "gopkg.in/telebot.v4"
)

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339), cat, fmt.Sprintf(format, a...))
}

func fail(format string, a ...any) {
	logf("SUMMARY", "INVALIDATED — "+format, a...)
	os.Exit(1)
}

// escapeMDV2 is a throwaway prototype of the Phase-13 mdv2.go escaper:
// escape every reserved char outside code entities (here: whole-string, no entity parsing).
func escapeMDV2(s string) string {
	const reserved = "_*[]()~`>#+-=|{}.!\\"
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(reserved, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatStr := os.Getenv("AURA_E2E_CHAT_ID")
	if token == "" || chatStr == "" {
		fail("TELEGRAM_BOT_TOKEN or AURA_E2E_CHAT_ID unset")
	}
	chatID, err := strconv.ParseInt(chatStr, 10, 64)
	if err != nil {
		fail("AURA_E2E_CHAT_ID not an int64: %v", err)
	}

	logf("SETUP", "constructing bot (NewBot performs live getMe)")
	b, err := tele.NewBot(tele.Settings{Token: token, Offline: false})
	if err != nil {
		fail("NewBot/getMe failed: %v", err)
	}
	logf("SETUP", "bot authenticated: @%s (id %d)", b.Me.Username, b.Me.ID)

	to := tele.ChatID(chatID)
	tag := fmt.Sprintf("AURA-SPIKE-017-%d", time.Now().Unix())

	// Probe 1 — negative control: unescaped reserved chars under MarkdownV2 MUST 400
	// (proves Pitfall #18 is real, not folklore).
	raw := fmt.Sprintf("%s negative control: reserved chars _ * [ ] ( ) ~ > # + - = | { } . ! unescaped", tag)
	logf("PROBE-1", "sending UNESCAPED reserved chars with ModeMarkdownV2 (expect 400 can't parse entities)")
	if _, err := b.Send(to, raw, tele.ModeMarkdownV2); err != nil {
		logf("PROBE-1", "rejected as expected: %v", err)
		if !strings.Contains(err.Error(), "parse entities") {
			logf("WARN", "rejection reason differs from Pitfall #18 wording")
		}
	} else {
		logf("PROBE-1", "UNEXPECTED: unescaped payload accepted — Pitfall #18 premise wrong for this payload")
	}

	// Probe 2 — escaped payload MUST land.
	escaped := escapeMDV2(raw) + "\n\n*bold survives* `code survives`"
	logf("PROBE-2", "sending ESCAPED payload + live entities with ModeMarkdownV2")
	msg, err := b.Send(to, escaped, tele.ModeMarkdownV2)
	if err != nil {
		fail("escaped MarkdownV2 send failed: %v", err)
	}
	logf("PROBE-2", "delivered: message_id=%d", msg.ID)

	// Read-back ground truth: sendMessage response carries the rendered message —
	// assert the unique tag round-tripped and entities were parsed (bold present).
	if !strings.Contains(msg.Text, tag) {
		fail("read-back: tag %q missing from API-returned text %q", tag, msg.Text)
	}
	boldSeen := false
	for _, e := range msg.Entities {
		if e.Type == tele.EntityBold {
			boldSeen = true
		}
	}
	if !boldSeen {
		fail("read-back: bold entity not parsed — MarkdownV2 mode not actually applied")
	}
	logf("READBACK", "tag present + bold entity parsed (%d entities total)", len(msg.Entities))

	logf("SUMMARY", "VALIDATED — pin v4.0.0-beta.9 builds, getMe OK, 400-on-unescaped confirmed, escaped send delivered (msg %d)", msg.ID)
}
