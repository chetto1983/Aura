// Spike 048 — gemma4-mtp-gpu-fit
//
// Given llama.cpp server-cuda (built 2026-06-11, post-MTP-merge) serving
// gemma-4 E2B QAT UD-Q4_K_XL + its mtp- draft on the RTX A2000 4 GB,
// when the server boots with full offload and answers a chat completion,
// then VRAM must stay <= 4096 MiB and the reply must be non-empty.
//
// Run (server first, PowerShell — never Git-Bash, MSYS mangles /models):
//
//	docker run -d --name spike048 --gpus all -p 8095:8080 `
//	  -v D:\tmp\spike-048-models:/models:ro `
//	  ghcr.io/ggml-org/llama.cpp:server-cuda `
//	  -m /models/gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf `
//	  --model-draft /models/mtp-gemma-4-E2B-it.gguf `
//	  --spec-type draft-mtp --spec-draft-n-max 2 `
//	  -ngl 99 --spec-draft-ngl 99 -c 4096 --temp 0 --host 0.0.0.0 --port 8080
//
// then: go run ./.planning/spikes/048-gemma4-mtp-gpu-fit
package main

import (
	"bytes"
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
	vramCeiling  = 4096 // MiB, total card
	bootDeadline = 240 * time.Second
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

type vramSampler struct {
	mu   sync.Mutex
	peak int
	stop chan struct{}
	done chan struct{}
}

func startSampler() *vramSampler {
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

func (s *vramSampler) finish() int {
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
				logf("SETUP", "server healthy: %s", strings.TrimSpace(string(body)))
				return nil
			}
			logf("SETUP", "health %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("server not healthy within %s", bootDeadline)
}

func postJSON(path string, payload, out any) error {
	b, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("%s -> %d: %s", path, resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, out)
}

func main() {
	logf("SETUP", "spike 048 — gemma4 E2B QAT Q4 + MTP draft on A2000 4GB")

	if v, err := vramUsedMiB(); err == nil {
		logf("VRAM", "baseline before probe: %d MiB used", v)
	} else {
		logf("VRAM", "nvidia-smi unavailable: %v", err)
	}

	if err := waitHealthy(); err != nil {
		logf("SUMMARY", "INVALIDATED — %v", err)
		os.Exit(1)
	}

	sampler := startSampler()

	// Probe 1: OpenAI-compat chat completion, Italian (production wire shape).
	// Gemma 4 is a thinking model: reasoning_content consumes token budget
	// before content. max_tokens must leave headroom past the thinking phase.
	var chat struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Timings map[string]any `json:"timings"`
	}
	start := time.Now()
	err := postJSON("/v1/chat/completions", map[string]any{
		"model": "gemma-4-e2b",
		"messages": []map[string]string{
			{"role": "user", "content": "Spiega in due frasi cos'è la predizione multi-token (MTP) in un modello linguistico."},
		},
		"max_tokens":  1024,
		"temperature": 0,
	}, &chat)
	if err != nil || len(chat.Choices) == 0 || strings.TrimSpace(chat.Choices[0].Message.Content) == "" {
		logf("PROBE", "chat completion FAILED: err=%v choices=%d", err, len(chat.Choices))
		logf("SUMMARY", "INVALIDATED — no usable chat reply")
		os.Exit(1)
	}
	tj0, _ := json.Marshal(chat.Timings)
	logf("PROBE", "chat OK in %.1fs (reasoning %d chars): %q", time.Since(start).Seconds(), len(chat.Choices[0].Message.Reasoning), chat.Choices[0].Message.Content)
	logf("PROBE", "chat timings=%s", tj0)

	// Probe 2: native /completion for timings ground truth (incl. draft stats).
	var comp struct {
		Content string         `json:"content"`
		Timings map[string]any `json:"timings"`
	}
	if err := postJSON("/completion", map[string]any{
		"prompt":    "Elenca tre città del Piemonte e una caratteristica per ciascuna.\n",
		"n_predict": 192,
	}, &comp); err != nil {
		logf("PROBE", "native /completion FAILED: %v", err)
		logf("SUMMARY", "INVALIDATED — timings probe failed")
		os.Exit(1)
	}
	tj, _ := json.Marshal(comp.Timings)
	logf("PROBE", "completion OK, timings=%s", tj)
	logf("PROBE", "completion content: %q", comp.Content)

	peak := sampler.finish()
	logf("VRAM", "peak during generation: %d MiB (ceiling %d)", peak, vramCeiling)

	if peak > 0 && peak > vramCeiling {
		logf("SUMMARY", "INVALIDATED — VRAM peak %d MiB exceeds %d", peak, vramCeiling)
		os.Exit(1)
	}
	logf("SUMMARY", "VALIDATED — E2B+MTP served on GPU, reply OK, VRAM peak %d MiB <= %d", peak, vramCeiling)
}
