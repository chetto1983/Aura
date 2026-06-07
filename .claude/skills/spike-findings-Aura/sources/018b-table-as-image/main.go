//go:build spike_telegram

// Spike 018b — markdown table → PNG (pure Go, x/image + gofont/gomono), live sendPhoto vs sendDocument.
//
// Run: set -a; source <(tr -d '\r' < .env); set +a
//
//	go run -tags spike_telegram ./.planning/spikes/018b-table-as-image
//
// Same T1/T2/T3 fixtures as 018a; measures render latency + PNG size, and compares
// Telegram's photo re-encode (returned PhotoSize) against the original dimensions.
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
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

const (
	fontPx  = 28 // 14pt @2x for crispness against Telegram's JPEG re-encode
	padX    = 16
	padY    = 10
	marginP = 24
)

// renderPNG draws rows as a gridded table; returns PNG bytes + dimensions.
func renderPNG(rows [][]string, regular, bold font.Face) ([]byte, int, int, error) {
	metrics := regular.Metrics()
	rowH := metrics.Height.Ceil() + 2*padY
	ascent := metrics.Ascent.Ceil()

	measure := func(f font.Face, s string) int {
		return (&font.Drawer{Face: f}).MeasureString(s).Ceil()
	}
	widths := make([]int, len(rows[0]))
	for ri, r := range rows {
		f := regular
		if ri == 0 {
			f = bold
		}
		for i, c := range r {
			if i < len(widths) && measure(f, c) > widths[i] {
				widths[i] = measure(f, c)
			}
		}
	}
	totalW := marginP * 2
	for _, w := range widths {
		totalW += w + 2*padX
	}
	totalH := marginP*2 + rowH*len(rows)

	img := image.NewRGBA(image.Rect(0, 0, totalW, totalH))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	headerBg := image.NewUniform(color.RGBA{0xEE, 0xEE, 0xEE, 0xFF})
	draw.Draw(img, image.Rect(marginP, marginP, totalW-marginP, marginP+rowH), headerBg, image.Point{}, draw.Src)
	gridC := color.RGBA{0xBB, 0xBB, 0xBB, 0xFF}
	for ri := 0; ri <= len(rows); ri++ {
		y := marginP + ri*rowH
		for x := marginP; x < totalW-marginP; x++ {
			img.Set(x, y, gridC)
		}
	}
	for ri, r := range rows {
		f := regular
		if ri == 0 {
			f = bold
		}
		x := marginP + padX
		y := marginP + ri*rowH + padY + ascent
		for i, c := range r {
			d := &font.Drawer{Dst: img, Src: image.Black, Face: f, Dot: fixed.P(x, y)}
			d.DrawString(c)
			if i < len(widths) {
				x += widths[i] + 2*padX
			}
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, 0, 0, err
	}
	return buf.Bytes(), totalW, totalH, nil
}

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	chatID, err := strconv.ParseInt(os.Getenv("AURA_E2E_CHAT_ID"), 10, 64)
	if token == "" || err != nil {
		logf("SUMMARY", "INVALIDATED — env missing: %v", err)
		os.Exit(1)
	}

	parseFace := func(ttf []byte) font.Face {
		ft, err := opentype.Parse(ttf)
		if err != nil {
			logf("SUMMARY", "INVALIDATED — font parse: %v", err)
			os.Exit(1)
		}
		face, err := opentype.NewFace(ft, &opentype.FaceOptions{Size: fontPx, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			logf("SUMMARY", "INVALIDATED — font face: %v", err)
			os.Exit(1)
		}
		return face
	}
	regular := parseFace(gomono.TTF)
	bold := parseFace(gomonobold.TTF)

	b, err := tele.NewBot(tele.Settings{Token: token})
	if err != nil {
		logf("SUMMARY", "INVALIDATED — NewBot: %v", err)
		os.Exit(1)
	}
	to := tele.ChatID(chatID)
	tag := fmt.Sprintf("AURA-SPIKE-018b-%d", time.Now().Unix())
	failures := 0

	for _, f := range fixtures {
		start := time.Now()
		data, w, h, err := renderPNG(parseMD(f.md), regular, bold)
		renderMs := time.Since(start).Milliseconds()
		if err != nil {
			logf("RENDER", "%s FAILED: %v", f.name, err)
			failures++
			continue
		}
		logf("RENDER", "%s: %dx%dpx, %d bytes PNG, %dms", f.name, w, h, len(data), renderMs)
		_ = os.WriteFile(fmt.Sprintf("/d/tmp/spike-018b-%s.png", f.name), data, 0o644)

		photo := &tele.Photo{File: tele.FromReader(bytes.NewReader(data)), Caption: tag + " photo " + f.name}
		msg, err := b.Send(to, photo)
		if err != nil {
			logf("SEND", "%s sendPhoto FAILED: %v", f.name, err)
			failures++
		} else {
			served := "n/a"
			if msg.Photo != nil {
				served = fmt.Sprintf("%dx%d (re-encoded, %d bytes)", msg.Photo.Width, msg.Photo.Height, msg.Photo.FileSize)
			}
			logf("SEND", "%s photo delivered msg=%d: original %dx%d → served %s", f.name, msg.ID, w, h, served)
		}

		doc := &tele.Document{File: tele.FromReader(bytes.NewReader(data)), FileName: f.name + ".png", Caption: tag + " document " + f.name}
		dmsg, err := b.Send(to, doc)
		if err != nil {
			logf("SEND", "%s sendDocument FAILED: %v", f.name, err)
			failures++
		} else {
			logf("SEND", "%s document delivered msg=%d (lossless, %d bytes)", f.name, dmsg.ID, len(data))
		}
	}

	if failures > 0 {
		logf("SUMMARY", "INVALIDATED — %d operations failed", failures)
		os.Exit(1)
	}
	logf("SUMMARY", "VALIDATED (wire-level) — PNG renders + photo/document pairs delivered; crispness verdict deferred to human checkpoint")
}
