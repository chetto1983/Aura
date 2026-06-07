//go:build spike_telegram

// Spike 018a — markdown table → aligned monospace pre-block (MarkdownV2), live send.
//
// Run: set -a; source <(tr -d '\r' < .env); set +a
//
//	go run -tags spike_telegram ./.planning/spikes/018a-table-pre-block
//
// Sends T1/T2/T3 tables + a width-ruler to the operator's own chat for the
// mobile-readability human checkpoint.
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

// Test fixtures — realistic Aura-style LLM markdown tables (Italian, like production replies).
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

// parseMD parses a GitHub-style markdown table into rows of cells.
func parseMD(md string) [][]string {
	var rows [][]string
	for _, line := range strings.Split(md, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		// drop the |---|---| separator row
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

// renderPre renders rows as space-aligned monospace columns with a header rule.
func renderPre(rows [][]string) string {
	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, c := range r {
			if i < len(widths) && len([]rune(c)) > widths[i] {
				widths[i] = len([]rune(c))
			}
		}
	}
	var b strings.Builder
	for ri, r := range rows {
		for i, c := range r {
			b.WriteString(c)
			if i < len(r)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-len([]rune(c))+2))
			}
		}
		b.WriteByte('\n')
		if ri == 0 {
			total := 0
			for _, w := range widths {
				total += w + 2
			}
			b.WriteString(strings.Repeat("─", total-2) + "\n")
		}
	}
	return b.String()
}

// escapePre escapes the only two chars reserved inside MarkdownV2 pre/code entities.
func escapePre(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, "`", "\\`")
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
	tag := fmt.Sprintf("AURA-SPIKE-018a-%d", time.Now().Unix())

	if _, err := b.Send(to, escapeMDV2Text(tag+" — pre-block variant (3 tabelle + width ruler)"), tele.ModeMarkdownV2); err != nil {
		logf("SUMMARY", "INVALIDATED — header send: %v", err)
		os.Exit(1)
	}

	failures := 0
	for _, f := range fixtures {
		rendered := renderPre(parseMD(f.md))
		maxW := 0
		for _, l := range strings.Split(rendered, "\n") {
			if n := len([]rune(l)); n > maxW {
				maxW = n
			}
		}
		payload := "*" + escapeMDV2Text(f.name) + "*\n```\n" + escapePre(rendered) + "```"
		logf("RENDER", "%s: widest line %d chars, payload %d bytes (limit 4096)", f.name, maxW, len(payload))
		msg, err := b.Send(to, payload, tele.ModeMarkdownV2)
		if err != nil {
			logf("SEND", "%s FAILED: %v", f.name, err)
			failures++
			continue
		}
		preSeen := false
		for _, e := range msg.Entities {
			if e.Type == tele.EntityCodeBlock {
				preSeen = true
			}
		}
		logf("SEND", "%s delivered: message_id=%d pre_entity=%v", f.name, msg.ID, preSeen)
	}

	// Width ruler — operator reads on the phone which row first wraps.
	var ruler strings.Builder
	for w := 20; w <= 56; w += 4 {
		line := fmt.Sprintf("%d|", w)
		line += strings.Repeat("·", w-len([]rune(line))-1) + "#"
		ruler.WriteString(line + "\n")
	}
	if _, err := b.Send(to, "*width ruler — dal telefono: prima riga che va a capo?*\n```\n"+escapePre(ruler.String())+"```", tele.ModeMarkdownV2); err != nil {
		logf("SEND", "ruler FAILED: %v", err)
		failures++
	} else {
		logf("SEND", "width ruler delivered (rows 20..56 chars)")
	}

	if failures > 0 {
		logf("SUMMARY", "INVALIDATED — %d sends failed", failures)
		os.Exit(1)
	}
	logf("SUMMARY", "VALIDATED (wire-level) — all pre-blocks delivered; readability verdict deferred to human checkpoint")
}

// escapeMDV2Text escapes reserved chars for text outside code entities.
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
