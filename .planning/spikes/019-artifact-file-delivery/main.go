//go:build spike_telegram

// Spike 019 — artifact file delivery via sendDocument (operator requirement 2026-06-07).
//
// Run: python3 ./.planning/spikes/019-artifact-file-delivery/fixtures.py D:/tmp/spike-019-fixtures
//
//	set -a; source <(tr -d '\r' < .env); set +a
//	go run -tags spike_telegram ./.planning/spikes/019-artifact-file-delivery
//
// Sends real xlsx/pdf/docx/csv artifacts to the operator's own chat and asserts the
// API-returned Document metadata (filename + Telegram-detected MIME + size round-trip).
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

var artifacts = []struct {
	file     string
	wantMIME string // expected Telegram-side detection ("" = just log what comes back)
}{
	{"report.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	{"report.pdf", "application/pdf"},
	{"report.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
	{"report.csv", "text/csv"},
}

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID, err := strconv.ParseInt(os.Getenv("AURA_E2E_CHAT_ID"), 10, 64)
	if token == "" || err != nil {
		logf("SUMMARY", "INVALIDATED — env missing: %v", err)
		os.Exit(1)
	}
	fixDir := os.Getenv("AURA_SPIKE019_FIXTURES")
	if fixDir == "" {
		fixDir = "D:/tmp/spike-019-fixtures"
	}

	b, err := tele.NewBot(tele.Settings{Token: token})
	if err != nil {
		logf("SUMMARY", "INVALIDATED — NewBot: %v", err)
		os.Exit(1)
	}
	to := tele.ChatID(chatID)
	tag := fmt.Sprintf("AURA-SPIKE-019-%d", time.Now().Unix())
	failures := 0

	for _, a := range artifacts {
		path := fixDir + "/" + a.file
		st, err := os.Stat(path)
		if err != nil {
			logf("SEND", "%s missing — run fixtures.py first: %v", a.file, err)
			failures++
			continue
		}
		doc := &tele.Document{
			File:     tele.FromDisk(path),
			FileName: a.file,
			Caption:  tag + " " + a.file,
		}
		start := time.Now()
		msg, err := b.Send(to, doc)
		if err != nil {
			logf("SEND", "%s sendDocument FAILED: %v", a.file, err)
			failures++
			continue
		}
		ms := time.Since(start).Milliseconds()
		if msg.Document == nil {
			logf("SEND", "%s delivered but response has no Document payload", a.file)
			failures++
			continue
		}
		d := msg.Document
		mimeOK := a.wantMIME == "" || strings.EqualFold(d.MIME, a.wantMIME)
		sizeOK := d.FileSize == st.Size()
		logf("READBACK", "%s msg=%d: filename=%q mime=%q (want %q, ok=%v) size=%d/%d (ok=%v) thumb=%v %dms",
			a.file, msg.ID, d.FileName, d.MIME, a.wantMIME, mimeOK, d.FileSize, st.Size(), sizeOK, d.Thumbnail != nil, ms)
		if !mimeOK || !sizeOK || d.FileName != a.file {
			failures++
		}
	}

	if failures > 0 {
		logf("SUMMARY", "INVALIDATED — %d artifact deliveries failed assertions", failures)
		os.Exit(1)
	}
	logf("SUMMARY", "VALIDATED (wire-level) — all artifact types delivered with correct filename+MIME+size; openability verdict deferred to human checkpoint")
}
