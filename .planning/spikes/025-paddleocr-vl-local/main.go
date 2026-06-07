//go:build spike_multimodal

// Spike 025 harness — PaddleOCR-VL-1.5 (0.9B GGUF) on llama.cpp CUDA server, 4GB GPU.
//
// Run: go run -tags spike_multimodal ./.planning/spikes/025-paddleocr-vl-local
//
// Probes the local llama-server (:18090, OpenAI-compat /v1/chat/completions) with the
// rendered Italian city table + an optional real document photo. Asserts Italian OCR
// recall, measures cold/warm latency p50/p95, samples VRAM the whole run.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
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
	base      = "http://127.0.0.1:18090"
	readyWait = 8 * time.Minute
	warmN     = 5
)

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format("2006-01-02T15:04:05.000"), cat, fmt.Sprintf(format, a...))
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

// ---------- VRAM sampler ----------

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

// ---------- llama-server probe ----------

func waitReady() error {
	logf("READY", "polling %s/health (deadline %s)", base, readyWait)
	deadline := time.Now().Add(readyWait)
	start := time.Now()
	last := time.Now()
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				logf("READY", "server up after %s", time.Since(start).Round(time.Second))
				return nil
			}
		}
		if time.Since(last) > 20*time.Second {
			logf("READY", "still waiting (%s)", time.Since(start).Round(time.Second))
			last = time.Now()
		}
		time.Sleep(3 * time.Second)
	}
	if out, err := exec.Command("docker", "logs", "--tail", "30", "spike025-llama-ocr").CombinedOutput(); err == nil {
		logf("CRASH", "container log tail:\n%s", string(out))
	}
	return fmt.Errorf("server not ready within %s", readyWait)
}

func ocr(prompt, mime string, img []byte) (string, time.Duration, error) {
	b64 := base64.StdEncoding.EncodeToString(img)
	payload := map[string]any{
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "image_url", "image_url": map[string]string{"url": "data:" + mime + ";base64," + b64}},
				{"type": "text", "text": prompt},
			},
		}},
		"max_tokens":  512,
		"temperature": 0,
	}
	body, _ := json.Marshal(payload)
	t0 := time.Now()
	resp, err := http.Post(base+"/v1/chat/completions", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	elapsed := time.Since(t0)
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", elapsed, fmt.Errorf("HTTP %d: %.300s", resp.StatusCode, string(raw))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return "", elapsed, fmt.Errorf("unparseable: %.300s", string(raw))
	}
	return out.Choices[0].Message.Content, elapsed, nil
}

func main() {
	flag.Parse()
	logf("SETUP", "spike 025 — PaddleOCR-VL-1.5 local @ %s", base)
	vram := startVRAM()

	if err := waitReady(); err != nil {
		logf("VRAM", "peak=%dMiB", vram.peak())
		logf("SUMMARY", "FAILED: %v", err)
		os.Exit(1)
	}

	tablePNG := renderTablePNG()
	_ = os.MkdirAll(`/d/tmp/spike-025`, 0o755)
	_ = os.WriteFile(`/d/tmp/spike-025/ocr-table.png`, tablePNG, 0o644)

	// cold + warm on the Italian table
	cold, firstD, err := ocr("Trascrivi esattamente tutto il testo della tabella, riga per riga.", "image/png", tablePNG)
	if err != nil {
		logf("VRAM", "peak=%dMiB", vram.peak())
		logf("SUMMARY", "FAILED OCR cold: %v", err)
		os.Exit(1)
	}
	logf("OCR-IT", "cold=%s reply=%q", firstD.Round(time.Millisecond), strings.TrimSpace(cold))
	low := strings.ToLower(cold)
	hits, want := 0, []string{"cuneo", "caraglio", "mondov", "56000", "6800", "22000", "piemonte"}
	missing := []string{}
	for _, w := range want {
		if strings.Contains(low, w) {
			hits++
		} else {
			missing = append(missing, w)
		}
	}
	logf("OCR-IT", "recall %d/%d tokens%s", hits, len(want), func() string {
		if len(missing) > 0 {
			return fmt.Sprintf(" (missing %v)", missing)
		}
		return ""
	}())

	var warm []time.Duration
	for i := 0; i < warmN; i++ {
		_, d, err := ocr("Trascrivi esattamente tutto il testo della tabella.", "image/png", tablePNG)
		if err != nil {
			logf("OCR-IT", "warm %d failed: %v", i+1, err)
			continue
		}
		warm = append(warm, d)
	}
	if len(warm) > 0 {
		sort.Slice(warm, func(i, j int) bool { return warm[i] < warm[j] })
		p50 := warm[len(warm)/2]
		p95 := warm[len(warm)*95/100]
		logf("LATENCY", "cold=%s warm-p50=%s warm-p95=%s (n=%d)", firstD.Round(time.Millisecond), p50.Round(time.Millisecond), p95.Round(time.Millisecond), len(warm))
	}

	// optional real document
	if data, err := os.ReadFile(`/d/tmp/spike-025/document.jpg`); err == nil {
		reply, d, err := ocr("Trascrivi tutto il testo del documento mantenendo la struttura.", "image/jpeg", data)
		if err != nil {
			logf("DOC", "failed: %v", err)
		} else {
			logf("DOC", "%s reply=%q", d.Round(time.Millisecond), strings.TrimSpace(reply))
			_ = os.WriteFile(`/d/tmp/spike-025/document-ocr.txt`, []byte(reply), 0o644)
		}
	}

	peak := vram.peak()
	logf("VRAM", "peak=%dMiB (card 4096MiB)", peak)
	if hits < 5 {
		logf("SUMMARY", "PARTIAL — Italian OCR recall %d/%d below threshold; inspect reply (model is CN/EN-first)", hits, len(want))
		os.Exit(1)
	}
	logf("SUMMARY", "PaddleOCR-VL-1.5 LOCAL VALIDATED — Italian OCR %d/%d, VRAM peak=%dMiB ≤4096", hits, len(want), peak)
}
