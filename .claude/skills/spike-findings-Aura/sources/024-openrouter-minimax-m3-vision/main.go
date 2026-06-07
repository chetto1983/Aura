//go:build spike_multimodal

// Spike 024 harness — OpenRouter minimax/minimax-m3 vision probe (operator pivot
// 2026-06-07: local 4GB vLLM can't reserve KV for a multimodal model → use the cloud
// model for the VISION half of 9c).
//
// Arm: go get golang.org/x/image@latest (already armed by spike 021)
// Run: set -a; source <(tr -d '\r' < .env); set +a
//
//	go run -tags spike_multimodal ./.planning/spikes/024-openrouter-minimax-m3-vision
//
// Three image probes (the real photo.go use cases):
//   - color: 3-band PNG → name colors (sanity)
//   - ocr-it: rendered Italian city table PNG → read the cities (OCR recall, the 9c signal)
//   - ocr-photo: an optional real photo at D:\tmp\spike-024\photo.jpg if present
//
// Reports per probe: pass/fail, reply, latency; final cost from OpenRouter usage block.
// PAID — authorized by operator directive 2026-06-07 ("prova con openrouter minimax/minimax-m3").
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	model    = "minimax/minimax-m3"
	endpoint = "https://openrouter.ai/api/v1/chat/completions"
	repeatN  = 3
)

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format("2006-01-02T15:04:05.000"), cat, fmt.Sprintf(format, a...))
}

func threeBandPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 300, 300))
	bands := []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}}
	for y := 0; y < 300; y++ {
		for x := 0; x < 300; x++ {
			img.Set(x, y, bands[y/100])
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

const ocrTableMD = `| Città | Regione | Abitanti |
| Cuneo | Piemonte | 56000 |
| Caraglio | Piemonte | 6800 |
| Mondovì | Piemonte | 22000 |`

func renderTablePNG() []byte {
	parseFace := func(ttf []byte) font.Face {
		ft, _ := opentype.Parse(ttf)
		face, _ := opentype.NewFace(ft, &opentype.FaceOptions{Size: 28, DPI: 72, Hinting: font.HintingFull})
		return face
	}
	regular, bold := parseFace(gomono.TTF), parseFace(gomonobold.TTF)
	var rows [][]string
	for _, line := range strings.Split(ocrTableMD, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		rows = append(rows, cells)
	}
	const padX, padY, marginP = 16, 10, 24
	m := regular.Metrics()
	rowH := m.Height.Ceil() + 2*padY
	ascent := m.Ascent.Ceil()
	measure := func(f font.Face, s string) int { return (&font.Drawer{Face: f}).MeasureString(s).Ceil() }
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
	draw.Draw(img, image.Rect(marginP, marginP, totalW-marginP, marginP+rowH),
		image.NewUniform(color.RGBA{0xEE, 0xEE, 0xEE, 0xFF}), image.Point{}, draw.Src)
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
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

type usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
}

func chat(ctx context.Context, key, prompt, mime string, img []byte) (string, usage, time.Duration, error) {
	b64 := base64.StdEncoding.EncodeToString(img)
	payload := map[string]any{
		"model": model,
		"usage": map[string]bool{"include": true},
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "image_url", "image_url": map[string]string{"url": "data:" + mime + ";base64," + b64}},
				{"type": "text", "text": prompt},
			},
		}},
		"max_tokens": 200,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	t0 := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", usage{}, 0, err
	}
	defer resp.Body.Close()
	elapsed := time.Since(t0)
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", usage{}, elapsed, fmt.Errorf("HTTP %d: %.300s", resp.StatusCode, string(raw))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage usage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return "", usage{}, elapsed, fmt.Errorf("unparseable: %.300s", string(raw))
	}
	return out.Choices[0].Message.Content, out.Usage, elapsed, nil
}

type probe struct {
	name    string
	prompt  string
	mime    string
	img     []byte
	expects []string // lowercase substrings, all required
}

func main() {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		logf("SUMMARY", "INVALIDATED — OPENROUTER_API_KEY unset (set -a; source <(tr -d '\\r' < .env); set +a)")
		os.Exit(1)
	}
	_ = os.MkdirAll(`/d/tmp/spike-024`, 0o755)
	tablePNG := renderTablePNG()
	_ = os.WriteFile(`/d/tmp/spike-024/ocr-table.png`, tablePNG, 0o644)

	probes := []probe{
		{"color", "Name the three horizontal color bands top to bottom, comma-separated.", "image/png", threeBandPNG(), []string{"red", "green", "blue"}},
		{"ocr-it", "Leggi la tabella nell'immagine ed elenca le città con il numero di abitanti.", "image/png", tablePNG, []string{"cuneo", "caraglio", "mondov", "56000", "6800", "22000"}},
	}
	// optional real photo
	if data, err := os.ReadFile(`/d/tmp/spike-024/photo.jpg`); err == nil {
		probes = append(probes, probe{"ocr-photo", "Descrivi cosa mostra questa foto. Se c'è del testo, trascrivilo.", "image/jpeg", data, nil})
		logf("SETUP", "optional photo.jpg found (%d bytes) — added ocr-photo probe", len(data))
	}

	var totalCost float64
	failures := 0
	for _, p := range probes {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		var lat []time.Duration
		var firstReply string
		var u usage
		ok := true
		runs := repeatN
		if p.name == "ocr-photo" {
			runs = 1 // qualitative, single
		}
		var perr error
		for i := 0; i < runs; i++ {
			reply, uu, d, err := chat(ctx, key, p.prompt, p.mime, p.img)
			if err != nil {
				perr = err
				ok = false
				break
			}
			lat = append(lat, d)
			totalCost += uu.Cost
			if i == 0 {
				firstReply, u = reply, uu
			}
		}
		cancel()
		if !ok {
			logf("PROBE", "%s FAILED: %v", p.name, perr)
			failures++
			continue
		}
		low := strings.ToLower(firstReply)
		missing := []string{}
		for _, e := range p.expects {
			if !strings.Contains(low, e) {
				missing = append(missing, e)
			}
		}
		sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
		p50 := lat[len(lat)/2]
		verdict := "PASS"
		if len(missing) > 0 {
			verdict = fmt.Sprintf("PARTIAL (missing %v)", missing)
			if len(p.expects) > 0 {
				failures++
			}
		}
		logf("PROBE", "%s %s p50=%s tokens=%d/%d reply=%q", p.name, verdict, p50.Round(time.Millisecond), u.PromptTokens, u.CompletionTokens, strings.TrimSpace(firstReply))
	}

	logf("COST", "total this run: $%.5f over %d probes", totalCost, len(probes))
	logf("MODALITY", "minimax-m3 input = text+image+video; NO audio → STT half of 9c needs a separate path")
	if failures > 0 {
		logf("SUMMARY", "minimax-m3 vision probe: %d/%d probes had issues — see above", failures, len(probes))
		os.Exit(1)
	}
	logf("SUMMARY", "minimax-m3 VISION VALIDATED — all probes pass, color+Italian-OCR recalled, $%.5f", totalCost)
}
