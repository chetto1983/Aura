//go:build spike_telegram

// Spike 018c — markdown table → restructured per-row key-value blocks (MarkdownV2), live send.
//
// Run: set -a; source <(tr -d '\r' < .env); set +a
//
//	go run -tags spike_telegram ./.planning/spikes/018c-table-restructured
//
// Same T1/T2/T3 fixtures as 018a/018b. Abandons the tabular shape entirely:
// first column becomes a bold row header, remaining columns become "key: value" lines.
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

var fixtures = []struct {
	name string
	md   string
}{
	{"T1-narrow-2x4", `| Comando | Effetto |
|---|---|
| /cancel | Interrompe il run |
| /cost | Spesa di oggi |
| /search | Cerca nei turni |
| /help | Mostra i comandi |`},
	{"T2-realistic-4x5", `| Fase | Nome | Coverage | Stato |
|---|---|---|---|
| 11 | Skills | 91.7% | Done |
| 12 | AG-UI Gateway | — | Next |
| 13 | Telegram | — | Spiking |
| 14 | Onboarding | — | Todo |
| 15 | Memory | — | Todo |`},
	{"T3-wide-6x6", `| # | Spike | Type | Validates | Verdict | Tags |
|---|---|---|---|---|---|
| 014 | agui-sdk-module-pin | standard | SHA pin resolves and builds under Go 1.26 | VALIDATED | agui, go-mod |
| 015 | agui-event-surface | standard | core/events covers the PRD acceptance list | VALIDATED | agui, events |
| 016 | agui-sse-roundtrip | standard | iter.Seq2 translator round-trips over SSE | VALIDATED | agui, sse |
| 017 | telebot-v4-pin | standard | tag pin + live MarkdownV2 send without 400 | VALIDATED | telegram |
| 018 | table-rendering | comparison | tables readable on a phone-width client | PENDING | telegram, tables |`},
}

func parseMD(md string) [][]string {
	var rows [][]string
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		sep := true
		for _, c := range cells {
			if strings.Trim(strings.TrimSpace(c), "-:") != "" {
				sep = false
				break
			}
		}
		if sep {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		rows = append(rows, cells)
	}
	return rows
}

func escapeMDV2Text(s string) string {
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

// renderKV restructures rows: bold "first-col value" as row header, the rest as
// indented "header: value" lines. Headers come from row 0.
func renderKV(rows [][]string) string {
	headers := rows[0]
	var b strings.Builder
	for _, r := range rows[1:] {
		b.WriteString("▪ *" + escapeMDV2Text(r[0]) + "*")
		if len(headers) > 0 {
			b.WriteString(" _\\(" + escapeMDV2Text(headers[0]) + "\\)_")
		}
		b.WriteByte('\n')
		for i := 1; i < len(r) && i < len(headers); i++ {
			if r[i] == "" || r[i] == "—" {
				continue
			}
			b.WriteString("   " + escapeMDV2Text(headers[i]) + ": " + escapeMDV2Text(r[i]) + "\n")
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID, err := strconv.ParseInt(os.Getenv("AURA_E2E_CHAT_ID"), 10, 64)
	if token == "" || err != nil {
		logf("SUMMARY", "INVALIDATED — env missing: %v", err)
		os.Exit(1)
	}
	b, err := tele.NewBot(tele.Settings{Token: token})
	if err != nil {
		logf("SUMMARY", "INVALIDATED — NewBot: %v", err)
		os.Exit(1)
	}
	to := tele.ChatID(chatID)
	tag := fmt.Sprintf("AURA-SPIKE-018c-%d", time.Now().Unix())

	if _, err := b.Send(to, escapeMDV2Text(tag+" — key-value variant (3 tabelle ristrutturate)"), tele.ModeMarkdownV2); err != nil {
		logf("SUMMARY", "INVALIDATED — header send: %v", err)
		os.Exit(1)
	}

	failures := 0
	for _, f := range fixtures {
		rendered := renderKV(parseMD(f.md))
		payload := "*" + escapeMDV2Text(f.name) + "*\n\n" + rendered
		logf("RENDER", "%s: %d bytes (vs pre-block, no width constraint)", f.name, len(payload))
		msg, err := b.Send(to, payload, tele.ModeMarkdownV2)
		if err != nil {
			logf("SEND", "%s FAILED: %v", f.name, err)
			failures++
			continue
		}
		logf("SEND", "%s delivered: message_id=%d entities=%d", f.name, msg.ID, len(msg.Entities))
	}

	if failures > 0 {
		logf("SUMMARY", "INVALIDATED — %d sends failed", failures)
		os.Exit(1)
	}
	logf("SUMMARY", "VALIDATED (wire-level) — all key-value renderings delivered; readability verdict deferred to human checkpoint")
}
