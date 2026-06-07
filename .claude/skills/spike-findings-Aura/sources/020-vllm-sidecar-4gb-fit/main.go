// Spike 020 harness — vLLM sidecar fit on RTX A2000 Laptop 4GB.
//
// Modes:
//
//	-mode=vision  probe vllm-vision  (:18086): 3-band PNG → chat completion → color assertion
//	-mode=stt     probe vllm-stt     (:18087): synth WAV → /v1/audio/transcriptions → 200+JSON
//	-mode=dual    probe both halves  (:18088 vision, :18089 stt) with BOTH resident
//
// Forensic log: ISO-timestamped [CATEGORY] lines, [SUMMARY] verdict, exit 0 = fit proven.
// VRAM sampled via nvidia-smi every 2s for the whole run (max + steady reported).
// Latency measured host→container (production-shaped: aura.exe also calls 127.0.0.1).
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	visionModel = "Qwen/Qwen3-VL-2B-Instruct-FP8"
	sttModel    = "openai/whisper-large-v3-turbo"
	readyWait   = 25 * time.Minute // first boot downloads the model (~2GB)
	latencyN    = 5
)

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format("2006-01-02T15:04:05.000"), cat, fmt.Sprintf(format, a...))
}

// ---------- VRAM sampler ----------

type vramSampler struct {
	mu      sync.Mutex
	samples []int
	stop    chan struct{}
	done    chan struct{}
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
				v, err := strconv.Atoi(strings.TrimSpace(string(out)))
				if err != nil {
					continue
				}
				s.mu.Lock()
				s.samples = append(s.samples, v)
				s.mu.Unlock()
			}
		}
	}()
	return s
}

func (s *vramSampler) report() (max, steady int) {
	close(s.stop)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.samples) == 0 {
		return 0, 0
	}
	for _, v := range s.samples {
		if v > max {
			max = v
		}
	}
	tail := s.samples
	if len(tail) > 5 {
		tail = tail[len(tail)-5:]
	}
	sum := 0
	for _, v := range tail {
		sum += v
	}
	return max, sum / len(tail)
}

// ---------- readiness ----------

func waitReady(base, container string) error {
	logf("READY", "polling %s/v1/models (deadline %s)", base, readyWait)
	deadline := time.Now().Add(readyWait)
	start := time.Now()
	lastLog := time.Now()
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/v1/models")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				logf("READY", "server up after %s — %s", time.Since(start).Round(time.Second), strings.TrimSpace(string(body)))
				return nil
			}
		}
		if time.Since(lastLog) > 30*time.Second {
			logf("READY", "still waiting (%s elapsed)", time.Since(start).Round(time.Second))
			lastLog = time.Now()
		}
		time.Sleep(5 * time.Second)
	}
	if container != "" {
		if out, err := exec.Command("docker", "logs", "--tail", "40", container).CombinedOutput(); err == nil {
			logf("CRASH", "container logs tail:\n%s", string(out))
		}
	}
	return fmt.Errorf("server at %s never became ready within %s", base, readyWait)
}

// ---------- vision probe ----------

// threeBandPNG renders red/green/blue horizontal bands — color recall is assertable
// without fonts or external assets.
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

func visionRequest(base string) (string, time.Duration, error) {
	b64 := base64.StdEncoding.EncodeToString(threeBandPNG())
	payload := map[string]any{
		"model": visionModel,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{
				{"type": "image_url", "image_url": map[string]string{"url": "data:image/png;base64," + b64}},
				{"type": "text", "text": "This image has three solid horizontal color bands. Name the three colors from top to bottom, comma-separated, nothing else."},
			},
		}},
		"max_tokens": 50,
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
		return "", elapsed, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return "", elapsed, fmt.Errorf("unparseable response: %s", string(raw))
	}
	return out.Choices[0].Message.Content, elapsed, nil
}

func probeVision(base string) error {
	reply, first, err := visionRequest(base)
	if err != nil {
		return fmt.Errorf("vision first request: %w", err)
	}
	logf("VISION", "first request %s reply=%q", first.Round(time.Millisecond), reply)
	low := strings.ToLower(reply)
	missing := []string{}
	for _, c := range []string{"red", "green", "blue"} {
		if !strings.Contains(low, c) {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("vision assertion FAILED — colors missing from reply: %v (reply=%q)", missing, reply)
	}
	logf("VISION", "color assertion PASS (red+green+blue all named)")

	lat := make([]time.Duration, 0, latencyN)
	for i := 0; i < latencyN; i++ {
		_, d, err := visionRequest(base)
		if err != nil {
			return fmt.Errorf("vision warm request %d: %w", i+1, err)
		}
		lat = append(lat, d)
	}
	reportLatency("VISION", first, lat)
	return nil
}

// ---------- STT probe ----------

// sineWAV synthesizes 2s of 440Hz @16kHz mono 16-bit PCM. No speech content —
// this probes ENDPOINT FIT (200 + JSON shape), not WER (that is spike 022).
func sineWAV() []byte {
	const (
		rate = 16000
		secs = 2
	)
	n := rate * secs
	var buf bytes.Buffer
	w := func(v any) { _ = binary.Write(&buf, binary.LittleEndian, v) }
	buf.WriteString("RIFF")
	w(uint32(36 + n*2))
	buf.WriteString("WAVEfmt ")
	w(uint32(16))
	w(uint16(1)) // PCM
	w(uint16(1)) // mono
	w(uint32(rate))
	w(uint32(rate * 2))
	w(uint16(2))
	w(uint16(16))
	buf.WriteString("data")
	w(uint32(n * 2))
	for i := 0; i < n; i++ {
		w(int16(10000 * math.Sin(2*math.Pi*440*float64(i)/rate)))
	}
	return buf.Bytes()
}

func sttRequest(base string) (string, time.Duration, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "probe.wav")
	_, _ = fw.Write(sineWAV())
	_ = mw.WriteField("model", sttModel)
	_ = mw.Close()
	t0 := time.Now()
	resp, err := http.Post(base+"/v1/audio/transcriptions", mw.FormDataContentType(), &body)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	elapsed := time.Since(t0)
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", elapsed, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", elapsed, fmt.Errorf("unparseable response: %s", string(raw))
	}
	return out.Text, elapsed, nil
}

func probeSTT(base string) error {
	text, first, err := sttRequest(base)
	if err != nil {
		return fmt.Errorf("stt first request: %w", err)
	}
	logf("STT", "first request %s — endpoint answered 200, text=%q (sine wave: content not asserted)", first.Round(time.Millisecond), text)
	lat := make([]time.Duration, 0, latencyN)
	for i := 0; i < latencyN; i++ {
		_, d, err := sttRequest(base)
		if err != nil {
			return fmt.Errorf("stt warm request %d: %w", i+1, err)
		}
		lat = append(lat, d)
	}
	reportLatency("STT", first, lat)
	return nil
}

// ---------- shared reporting ----------

func reportLatency(cat string, first time.Duration, warm []time.Duration) {
	sorted := append([]time.Duration(nil), warm...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	p50 := sorted[len(sorted)/2]
	max := sorted[len(sorted)-1]
	logf(cat, "latency first=%s warm-p50=%s warm-max=%s (n=%d, host→container production-shaped)",
		first.Round(time.Millisecond), p50.Round(time.Millisecond), max.Round(time.Millisecond), len(warm))
}

func main() {
	mode := flag.String("mode", "vision", "vision | stt | dual")
	flag.Parse()

	logf("SETUP", "spike 020 mode=%s", *mode)
	vram := startVRAM()
	var err error

	switch *mode {
	case "vision":
		if err = waitReady("http://127.0.0.1:18086", "spike020-vllm-vision"); err == nil {
			err = probeVision("http://127.0.0.1:18086")
		}
	case "stt":
		if err = waitReady("http://127.0.0.1:18087", "spike020-vllm-stt"); err == nil {
			err = probeSTT("http://127.0.0.1:18087")
		}
	case "dual":
		if err = waitReady("http://127.0.0.1:18088", "spike020-vllm-vision-half"); err == nil {
			if err = waitReady("http://127.0.0.1:18089", "spike020-vllm-stt-half"); err == nil {
				if err = probeVision("http://127.0.0.1:18088"); err == nil {
					err = probeSTT("http://127.0.0.1:18089")
				}
			}
		}
	default:
		err = fmt.Errorf("unknown mode %q", *mode)
	}

	maxV, steadyV := vram.report()
	logf("VRAM", "max=%dMiB steady=%dMiB (card total 4096MiB)", maxV, steadyV)

	if err != nil {
		logf("SUMMARY", "mode=%s FAILED: %v", *mode, err)
		os.Exit(1)
	}
	if maxV > 4096 {
		logf("SUMMARY", "mode=%s FAILED: VRAM max %dMiB exceeds 4096MiB card", *mode, maxV)
		os.Exit(1)
	}
	logf("SUMMARY", "mode=%s FIT PROVEN — probes pass, VRAM max=%dMiB steady=%dMiB ≤ 4096MiB", *mode, maxV, steadyV)
}
