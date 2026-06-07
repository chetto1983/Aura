//go:build spike_multimodal

// Spike 021 harness — 2026 multimodal candidate probe via host Ollama (D-15 probe channel).
//
// Arm the dep first (not yet adopted by the module — spike 014-016 convention):
//
//	go get golang.org/x/image@latest
//	go run -tags spike_multimodal ./.planning/spikes/021-survey-2026-shortlist 2>&1 | tee /d/tmp/spike-021-run.log
//	git checkout go.mod go.sum   # at session end
//
// Probes per candidate: pull (timed) → vision color sanity (3-band PNG) → vision OCR
// (Italian table PNG, x/image renderer from spike 018b) → audio acceptance via CLI
// (SAPI TTS known-text WAVs from make-probe-clips.ps1) → `ollama ps` GPU/CPU split →
// VRAM peak → unload. Forensic log + final [SHORTLIST] table.
//
// GPU must be FREE: all spike020 vLLM containers down before running.
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
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

const (
	ollamaAPI   = "http://127.0.0.1:11434"
	pullTimeout = 30 * time.Minute
	genTimeout  = 5 * time.Minute
	clipDir     = `D:\tmp\spike-021`
)

type candidate struct {
	tag    string
	vision bool
	audio  bool
}

var candidates = []candidate{
	{"gemma4:e2b", true, true},
	{"gemma4:e4b", true, true}, // THE PRD baseline model
	{"qwen3-vl:2b", true, false},
	{"qwen3-vl:4b", true, false},
	{"openbmb/minicpm-v4.6", true, false},
	{"openbmb/minicpm-o4.5", true, true}, // omni stretch — expect partial offload
}

type result struct {
	tag        string
	pullSize   string
	pullSecs   float64
	colorPass  bool
	ocrPass    bool
	ocrReply   string
	audioEN    string // TRANSCRIBED / REJECTED / UNCLEAR / n-a
	audioIT    string
	coldMs     int64 // first vision request (includes model load)
	warmMs     []int64
	processor  string // ollama ps split — THE fit signal
	loadedSize string
	vramPeak   int
	failure    string
}

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format("2006-01-02T15:04:05.000"), cat, fmt.Sprintf(format, a...))
}

// ---------- probe images ----------

func threeBandPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 300, 300))
	bands := []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}}
	for y := 0; y < 300; y++ {
		for x := 0; x < 300; x++ {
			img.Set(x, y, bands[y/100])
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

const ocrTableMD = `| Città | Regione | Abitanti |
|---|---|---|
| Cuneo | Piemonte | 56000 |
| Caraglio | Piemonte | 6800 |
| Mondovì | Piemonte | 22000 |`

// renderPNG: gridded table renderer lifted from spike 018b (proven on-device).
func renderTablePNG() []byte {
	parseFace := func(ttf []byte) font.Face {
		ft, err := opentype.Parse(ttf)
		if err != nil {
			panic(err)
		}
		face, err := opentype.NewFace(ft, &opentype.FaceOptions{Size: 28, DPI: 72, Hinting: font.HintingFull})
		if err != nil {
			panic(err)
		}
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

	const padX, padY, marginP = 16, 10, 24
	metrics := regular.Metrics()
	rowH := metrics.Height.Ceil() + 2*padY
	ascent := metrics.Ascent.Ceil()
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
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// ---------- VRAM sampler (pattern from spike 020) ----------

type vramSampler struct {
	mu   sync.Mutex
	max  int
	stop chan struct{}
	done chan struct{}
}

func startVRAM() *vramSampler {
	s := &vramSampler{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				out, err := exec.Command("nvidia-smi", "--query-gpu=memory.used", "--format=csv,noheader,nounits").Output()
				if err != nil {
					continue
				}
				if v, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil {
					s.mu.Lock()
					if v > s.max {
						s.max = v
					}
					s.mu.Unlock()
				}
			}
		}
	}()
	return s
}

func (s *vramSampler) peak() int {
	close(s.stop)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.max
}

// ---------- Ollama probes ----------

func ollamaChat(tag, prompt string, imagePNG []byte) (string, int64, error) {
	msg := map[string]any{"role": "user", "content": prompt}
	if imagePNG != nil {
		msg["images"] = []string{base64.StdEncoding.EncodeToString(imagePNG)}
	}
	body, _ := json.Marshal(map[string]any{"model": tag, "messages": []any{msg}, "stream": false})
	ctx, cancel := context.WithTimeout(context.Background(), genTimeout)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", ollamaAPI+"/api/chat", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	t0 := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	wallMs := time.Since(t0).Milliseconds()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", wallMs, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", wallMs, fmt.Errorf("unparseable: %s", string(raw))
	}
	return out.Message.Content, wallMs, nil
}

// audioProbe shells the CLI (API audio field undocumented); the file path rides in the prompt.
func audioProbe(tag, wavPath string, expects []string) string {
	if _, err := os.Stat(wavPath); err != nil {
		return "n-a (clip missing — run make-probe-clips.ps1)"
	}
	ctx, cancel := context.WithTimeout(context.Background(), genTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ollama", "run", tag,
		"Transcribe this audio file exactly, output only the transcription: "+wavPath).CombinedOutput()
	text := strings.ToLower(string(out))
	if err != nil {
		return fmt.Sprintf("REJECTED (%v: %.120s)", err, strings.TrimSpace(string(out)))
	}
	for _, e := range expects {
		if strings.Contains(text, e) {
			return fmt.Sprintf("TRANSCRIBED (%.80s)", strings.TrimSpace(string(out)))
		}
	}
	return fmt.Sprintf("UNCLEAR (%.120s)", strings.TrimSpace(string(out)))
}

func ollamaPS(tag string) (size, processor string) {
	out, err := exec.Command("ollama", "ps").Output()
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, strings.Split(tag, ":")[0]) {
			f := strings.Fields(line)
			if len(f) >= 5 {
				// NAME ID SIZE+unit PROCESSOR... (e.g. "gemma4:e2b abc 3.4 GB 100% GPU ...")
				return f[2] + f[3], strings.Join(f[4:min(len(f), 6)], " ")
			}
		}
	}
	return "", ""
}

func probe(c candidate, bandPNG, tablePNG []byte) result {
	r := result{tag: c.tag, audioEN: "n-a", audioIT: "n-a"}
	logf("PULL", "%s starting", c.tag)
	ctx, cancel := context.WithTimeout(context.Background(), pullTimeout)
	defer cancel()
	t0 := time.Now()
	if out, err := exec.CommandContext(ctx, "ollama", "pull", c.tag).CombinedOutput(); err != nil {
		r.failure = fmt.Sprintf("pull failed: %v — %.200s", err, string(out))
		logf("PULL", "%s FAILED: %s", c.tag, r.failure)
		return r
	}
	r.pullSecs = time.Since(t0).Seconds()
	logf("PULL", "%s done in %.0fs", c.tag, r.pullSecs)

	vram := startVRAM()
	defer func() {
		r.vramPeak = vram.peak()
		_ = exec.Command("ollama", "stop", c.tag).Run()
	}()

	if c.vision {
		reply, ms, err := ollamaChat(c.tag, "This image has three solid horizontal color bands. Name the three colors from top to bottom, comma-separated, nothing else.", bandPNG)
		r.coldMs = ms
		if err != nil {
			r.failure = fmt.Sprintf("vision color probe: %v", err)
			logf("VISION", "%s color FAILED: %v", c.tag, err)
			return r
		}
		low := strings.ToLower(reply)
		r.colorPass = strings.Contains(low, "red") && strings.Contains(low, "green") && strings.Contains(low, "blue")
		logf("VISION", "%s color cold=%dms pass=%v reply=%q", c.tag, ms, r.colorPass, reply)

		for i := 0; i < 2; i++ {
			reply, ms, err = ollamaChat(c.tag, "Leggi la tabella nell'immagine. Quali città sono elencate? Rispondi solo con i nomi.", tablePNG)
			if err != nil {
				r.failure = fmt.Sprintf("vision OCR probe: %v", err)
				logf("VISION", "%s OCR FAILED: %v", c.tag, err)
				return r
			}
			r.warmMs = append(r.warmMs, ms)
		}
		low = strings.ToLower(reply)
		r.ocrPass = strings.Contains(low, "cuneo") && strings.Contains(low, "caraglio") && strings.Contains(low, "mondov")
		r.ocrReply = reply
		logf("VISION", "%s OCR warm=%v pass=%v reply=%q", c.tag, r.warmMs, r.ocrPass, reply)
	}

	r.loadedSize, r.processor = ollamaPS(c.tag)
	logf("FIT", "%s ollama-ps size=%s processor=%q", c.tag, r.loadedSize, r.processor)

	if c.audio {
		r.audioEN = audioProbe(c.tag, clipDir+`\probe-en.wav`, []string{"two plus two", "2 plus 2", "2 + 2", "2+2"})
		logf("AUDIO", "%s EN: %s", c.tag, r.audioEN)
		r.audioIT = audioProbe(c.tag, clipDir+`\probe-it.wav`, []string{"due più due", "due piu due", "2 più 2", "2+2"})
		logf("AUDIO", "%s IT: %s", c.tag, r.audioIT)
	}
	return r
}

func main() {
	logf("SETUP", "spike 021 — %d candidates, Ollama %s", len(candidates), ollamaAPI)
	if out, err := exec.Command("ollama", "--version").Output(); err == nil {
		logf("SETUP", "%s", strings.TrimSpace(string(out)))
	}
	bandPNG, tablePNG := threeBandPNG(), renderTablePNG()
	_ = os.WriteFile(clipDir+`\ocr-table.png`, tablePNG, 0o644)

	var results []result
	for _, c := range candidates {
		results = append(results, probe(c, bandPNG, tablePNG))
	}

	logf("SHORTLIST", "tag | pull_s | color | ocr | audioEN | audioIT | cold_ms | warm_ms | processor | vram_peak | failure")
	failures := 0
	for _, r := range results {
		logf("SHORTLIST", "%s | %.0f | %v | %v | %s | %s | %d | %v | %s | %dMiB | %s",
			r.tag, r.pullSecs, r.colorPass, r.ocrPass, r.audioEN, r.audioIT, r.coldMs, r.warmMs, r.processor, r.vramPeak, r.failure)
		if r.failure != "" {
			failures++
		}
	}
	if failures == len(results) {
		logf("SUMMARY", "FAILED — every candidate failed")
		os.Exit(1)
	}
	logf("SUMMARY", "shortlist produced — %d/%d candidates probed clean", len(results)-failures, len(results))
}
