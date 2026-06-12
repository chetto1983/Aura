// Spike 050 — gemma4-mtp-multimodal
//
// Frontier integration probe: can the spike-048 GPU lane (gemma-4 E2B QAT Q4
// + MTP draft, full offload on the 4 GB A2000) ALSO do vision + audio in the
// SAME server, with the multimodal projector (mmproj) loaded? This is the
// question of whether one GPU sidecar collapses the three CPU sidecars the
// 9c design needs (spikes 025/026 OCR-VL, 027 STT).
//
// Three modalities:
//   - image: known Italian OCR page (spike-025 corpus, ground truth on disk)
//   - audio: Kokoro-generated known Italian sentence (≤30 s, Gemma audio cap)
//   - video: llama.cpp mtmd has NO video decoder — the path is client-side
//     frame sampling → multi-image single turn; probe proves multi-image works
//
// Serve (PowerShell — MSYS mangles /models):
//
//	docker run -d --name spike050 --gpus all -p 8095:8080 `
//	  -v D:\tmp\spike-048-models:/models:ro `
//	  ghcr.io/ggml-org/llama.cpp:server-cuda `
//	  -m /models/gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf `
//	  --mmproj /models/mmproj-BF16.gguf --no-mmproj-offload `
//	  --model-draft /models/mtp-gemma-4-E2B-it.gguf `
//	  --spec-type draft-mtp --spec-draft-n-max 2 `
//	  -ngl 99 --spec-draft-ngl 99 -c 8192 --temp 0 --host 0.0.0.0 --port 8080
//
// then: go run ./.planning/spikes/050-gemma4-mtp-multimodal
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	baseURL      = "http://127.0.0.1:8095"
	vramCeiling  = 4096
	bootDeadline = 240 * time.Second
	imagePath    = `D:\tmp\paddleocr-test\page-1.png`
	frame2Path   = `D:\tmp\llm_wiki\logo.jpg`
	audioPath    = `D:\tmp\spike-050-assets\probe-it.wav`
)

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format(time.RFC3339), cat, fmt.Sprintf(format, a...))
}

func vramUsedMiB() (int, error) {
	out, err := exec.Command("nvidia-smi", "--query-gpu=memory.used", "--format=csv,noheader,nounits").Output()
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

type sampler struct {
	mu   sync.Mutex
	peak int
	stop chan struct{}
	done chan struct{}
}

func startSampler() *sampler {
	s := &sampler{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-t.C:
				if v, err := vramUsedMiB(); err == nil {
					s.mu.Lock()
					if v > s.peak {
						s.peak = v
						logf("VRAM", "new peak %d MiB", v)
					}
					s.mu.Unlock()
				}
			}
		}
	}()
	return s
}

func (s *sampler) finish() int {
	close(s.stop)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peak
}

func waitHealthy() error {
	deadline := time.Now().Add(bootDeadline)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				logf("SETUP", "server healthy")
				return nil
			}
			_ = body
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("not healthy within %s", bootDeadline)
}

func b64file(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// chat sends an OpenAI-compat /chat/completions request and returns the
// assistant content plus the timings map (draft stats live there).
func chat(content []any) (string, map[string]any, error) {
	// enable_thinking=false: Gemma 4 E2B runs away in reasoning_content on
	// dense vision/OCR tasks and never emits visible content (probed: 7029
	// reasoning chars, finish=length, empty content). repeat_penalty breaks
	// the greedy repetition loop a tiny model falls into on dense text at
	// temp 0 ("Modbus RTU RS 485" x90). Both needed for a usable vision lane.
	payload, _ := json.Marshal(map[string]any{
		"model":                "gemma-4-e2b",
		"messages":             []map[string]any{{"role": "user", "content": content}},
		"max_tokens":           768,
		"temperature":          0,
		"repeat_penalty":       1.3,
		"repeat_last_n":        128,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	})
	resp, err := http.Post(baseURL+"/v1/chat/completions", "application/json", bytes.NewReader(payload))
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", nil, fmt.Errorf("%d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning_content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Timings map[string]any `json:"timings"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", nil, err
	}
	if len(out.Choices) == 0 {
		return "", out.Timings, fmt.Errorf("no choices")
	}
	c := out.Choices[0]
	if strings.TrimSpace(c.Message.Content) == "" && c.Message.Reasoning != "" {
		logf("NOTE", "empty content, %d reasoning chars, finish=%s — thinking overran budget", len(c.Message.Reasoning), c.FinishReason)
	}
	return c.Message.Content, out.Timings, nil
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

func containsAny(haystack string, needles ...string) []string {
	h := strings.ToLower(haystack)
	var hit []string
	for _, n := range needles {
		if strings.Contains(h, strings.ToLower(n)) {
			hit = append(hit, n)
		}
	}
	return hit
}

func main() {
	logf("SETUP", "spike 050 — gemma4 E2B + mmproj + MTP draft: vision/audio/video on the 4GB GPU lane")
	if err := waitHealthy(); err != nil {
		logf("SUMMARY", "INVALIDATED — %v", err)
		os.Exit(1)
	}
	smp := startSampler()
	pass, fail := 0, 0

	// ---- Modality 1: VISION (image) ----
	if img, err := b64file(imagePath); err != nil {
		logf("IMAGE", "asset missing: %v", err)
		fail++
	} else {
		start := time.Now()
		txt, tim, err := chat([]any{
			map[string]any{"type": "text", "text": "Trascrivi il testo principale di questa immagine. Includi codici prodotto e termini tecnici."},
			map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + img}},
		})
		if err != nil {
			logf("IMAGE", "FAILED: %v", err)
			fail++
		} else {
			hits := containsAny(txt, "Analizzatore", "Modbus", "6MBU00242200", "RS 485", "RS485", "TCP/IP")
			tj, _ := json.Marshal(tim)
			logf("IMAGE", "%.1fs, ground-truth hits=%v timings=%s", time.Since(start).Seconds(), hits, tj)
			logf("IMAGE", "reply: %q", truncate(txt, 400))
			if len(hits) >= 2 {
				logf("IMAGE", "PASS (%d/6 ground-truth terms recalled)", len(hits))
				pass++
			} else {
				logf("IMAGE", "WEAK (%d/6 terms) — vision path answered but low recall", len(hits))
				fail++
			}
		}
	}

	// ---- Modality 2: AUDIO ----
	if au, err := b64file(audioPath); err != nil {
		logf("AUDIO", "asset missing (run asset-gen step): %v", err)
		fail++
	} else {
		start := time.Now()
		txt, tim, err := chat([]any{
			map[string]any{"type": "text", "text": "Trascrivi esattamente l'audio in italiano."},
			map[string]any{"type": "input_audio", "input_audio": map[string]string{"data": au, "format": "wav"}},
		})
		if err != nil {
			logf("AUDIO", "FAILED: %v", err)
			fail++
		} else {
			hits := containsAny(txt, "Piemonte", "trascrizione", "Aura", "prova", "italiano")
			tj, _ := json.Marshal(tim)
			logf("AUDIO", "%.1fs, known-phrase hits=%v timings=%s", time.Since(start).Seconds(), hits, tj)
			logf("AUDIO", "reply: %q", truncate(txt, 400))
			if len(hits) >= 2 {
				logf("AUDIO", "PASS (%d/5 known words transcribed)", len(hits))
				pass++
			} else {
				logf("AUDIO", "WEAK (%d/5) — audio accepted but transcription poor", len(hits))
				fail++
			}
		}
	}

	// ---- Modality 3: VIDEO (frame-sequence path) ----
	// llama.cpp mtmd has no video decoder; video = client samples frames and
	// sends them as multiple images in one turn. Probe that multi-image works.
	f1, e1 := b64file(imagePath)
	f2, e2 := b64file(frame2Path)
	if e1 != nil || e2 != nil {
		logf("VIDEO", "asset missing: %v / %v", e1, e2)
		fail++
	} else {
		start := time.Now()
		txt, _, err := chat([]any{
			map[string]any{"type": "text", "text": "Questi sono due fotogrammi successivi di un video. Descrivi in una frase cosa mostra ciascun fotogramma."},
			map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + f1}},
			map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:image/jpeg;base64," + f2}},
		})
		if err != nil {
			logf("VIDEO", "multi-image (frame-seq) FAILED: %v", err)
			fail++
		} else {
			logf("VIDEO", "%.1fs, multi-image accepted: %q", time.Since(start).Seconds(), truncate(txt, 300))
			logf("VIDEO", "PASS (frame-sequence path works; no native video decoder — sample frames client-side)")
			pass++
		}
	}

	peak := smp.finish()
	logf("VRAM", "peak with mmproj loaded: %d MiB (ceiling %d)", peak, vramCeiling)
	if peak > vramCeiling {
		logf("SUMMARY", "INVALIDATED — VRAM peak %d exceeds %d", peak, vramCeiling)
		os.Exit(1)
	}
	logf("SUMMARY", "modality results: %d pass / %d fail; VRAM peak %d MiB", pass, fail, peak)
	if fail > 0 {
		os.Exit(1)
	}
	logf("SUMMARY", "VALIDATED — vision+audio+video(frame-seq) on one 4GB GPU sidecar with MTP")
}
