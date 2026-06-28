//go:build spike_multimodal

// Spike 030 harness — OpenRouter xiaomi/mimo-v2.5 multimodal probe (operator
// directive 2026-06-08: "I find one more model can read image audio and video,
// do a small test"). MiMo-V2.5 declares input modalities text+audio+image+video
// (OpenRouter models API), 1M ctx, $0.14/M prompt / $0.28/M completion — cheaper
// than minimax-m3 and the only shortlisted cloud model that natively accepts AUDIO.
//
// Mirrors spike 024 (minimax-m3 vision) probe-for-probe so the VISION result is
// apples-to-apples with the committed cloud-tier choice. Stdlib-only (no x/image):
// the Italian OCR-table PNG is pre-rendered to disk by PIL, the audio is the real
// 37s Italian voice note from spike 027.
//
// Probes (auto-skip if the asset file is absent):
//   - color:     stdlib 3-band PNG → name colors (sanity)
//   - ocr-it:    /d/tmp/spike-030/ocr-table.png → read cities + populations (the 9c signal)
//   - ocr-photo: /d/tmp/spike-030/photo.jpg (optional real photo)
//   - audio:     /d/tmp/spike-030/audio.{mp3,wav} → transcribe (the leg minimax-m3 lacks)
//
// Run: set -a; source <(tr -d '\r' < .env); set +a
//
//	go run -tags spike_multimodal ./.planning/spikes/030-openrouter-mimo-v2.5-multimodal
//
// PAID — authorized by operator directive 2026-06-08.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	model    = "xiaomi/mimo-v2.5"
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

type usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
}

// kind: "image" → image_url data URL; "audio" → input_audio base64.
func chat(ctx context.Context, key, prompt, kind, mime string, data []byte) (string, usage, time.Duration, error) {
	b64 := base64.StdEncoding.EncodeToString(data)
	var part map[string]any
	switch kind {
	case "audio":
		format := "wav"
		if strings.Contains(mime, "mp3") || strings.Contains(mime, "mpeg") {
			format = "mp3"
		}
		part = map[string]any{"type": "input_audio", "input_audio": map[string]string{"data": b64, "format": format}}
	default:
		part = map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + mime + ";base64," + b64}}
	}
	payload := map[string]any{
		"model": model,
		"usage": map[string]bool{"include": true},
		"messages": []map[string]any{{
			"role":    "user",
			"content": []map[string]any{part, {"type": "text", "text": prompt}},
		}},
		"max_tokens": 300,
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
		return "", usage{}, elapsed, fmt.Errorf("HTTP %d: %.400s", resp.StatusCode, string(raw))
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
		return "", usage{}, elapsed, fmt.Errorf("unparseable: %.400s", string(raw))
	}
	return out.Choices[0].Message.Content, out.Usage, elapsed, nil
}

type probe struct {
	name    string
	prompt  string
	kind    string
	mime    string
	data    []byte
	expects []string // lowercase substrings, all required
}

func main() {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		logf("SUMMARY", "INVALIDATED — OPENROUTER_API_KEY unset (set -a; source <(tr -d '\\r' < .env); set +a)")
		os.Exit(1)
	}
	dir := `D:\tmp\spike-030` // Windows-Go resolves /d/tmp to \d\tmp, not D:\tmp — use the real path

	probes := []probe{
		{"color", "Name the three horizontal color bands top to bottom, comma-separated.", "image", "image/png", threeBandPNG(), []string{"red", "green", "blue"}},
	}
	if data, err := os.ReadFile(filepath.Join(dir, "ocr-table.png")); err == nil {
		probes = append(probes, probe{"ocr-it", "Leggi la tabella nell'immagine ed elenca le città con il numero di abitanti.", "image", "image/png", data, []string{"cuneo", "caraglio", "mondov", "56000", "6800", "22000"}})
	} else {
		logf("SETUP", "ocr-table.png absent — skipping ocr-it probe")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "photo.jpg")); err == nil {
		probes = append(probes, probe{"ocr-photo", "Descrivi cosa mostra questa foto. Se c'è del testo, trascrivilo.", "image", "image/jpeg", data, nil})
		logf("SETUP", "optional photo.jpg found (%d bytes) — added ocr-photo probe", len(data))
	}
	for _, an := range []string{"audio.mp3", "audio.wav"} {
		if data, err := os.ReadFile(filepath.Join(dir, an)); err == nil {
			mime := "audio/mpeg"
			if strings.HasSuffix(an, ".wav") {
				mime = "audio/wav"
			}
			probes = append(probes, probe{"audio", "Trascrivi esattamente l'audio in italiano. Riporta solo il testo parlato.", "audio", mime, data, nil})
			logf("SETUP", "audio %s found (%d bytes) — added audio probe", an, len(data))
			break
		}
	}

	var totalCost float64
	failures := 0
	for _, p := range probes {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		var lat []time.Duration
		var firstReply string
		var u usage
		ok := true
		runs := repeatN
		if p.name == "ocr-photo" || p.name == "audio" {
			runs = 1 // qualitative, single
		}
		var perr error
		for i := 0; i < runs; i++ {
			reply, uu, d, err := chat(ctx, key, p.prompt, p.kind, p.mime, p.data)
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
		// Digit-normalize so Italian thousand-separators ("56.000") match "56000".
		lowNorm := strings.NewReplacer(".", "", ",", "", " ", "").Replace(low)
		missing := []string{}
		for _, e := range p.expects {
			if !strings.Contains(low, e) && !strings.Contains(lowNorm, e) {
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
	logf("MODALITY", "mimo-v2.5 declared input = text+audio+image+video (OpenRouter models API)")
	if failures > 0 {
		logf("SUMMARY", "mimo-v2.5 multimodal probe: %d/%d probes had issues — see above", failures, len(probes))
		os.Exit(1)
	}
	logf("SUMMARY", "mimo-v2.5 VALIDATED — all probes pass, $%.5f", totalCost)
}
