// Spike 049 — mtp-speedup-headtohead (049a baseline vs 049b MTP-on)
//
// Benches the CURRENTLY RUNNING llama-server at :8095 over a fixed IT/EN
// prompt set via the native /completion endpoint (timings ground truth,
// including draft acceptance stats when speculative decoding is active).
// Run it once per server config and diff the JSON results:
//
//	049a baseline:  docker run ... (048 command WITHOUT --spec-type/--model-draft)
//	                go run ./.planning/spikes/049-mtp-speedup-headtohead -label mtp-off
//	049b MTP on:    docker run ... (full 048 command, sweep --spec-draft-n-max)
//	                go run ./.planning/spikes/049-mtp-speedup-headtohead -label mtp-on-n2
//
// Results land in D:\tmp\spike-049-results\<label>.json plus forensic stdout.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const baseURL = "http://127.0.0.1:8095"

var prompts = []struct {
	ID     string
	Prompt string
}{
	{"it-city", "Elenca tre città del Piemonte e descrivi una caratteristica per ciascuna.\n"},
	{"it-story", "Scrivi un breve racconto (circa 150 parole) su un robot che impara a cucinare la polenta.\n"},
	{"it-explain", "Spiega a un bambino di dieci anni come funziona una batteria al litio.\n"},
	{"en-code", "Write a Go function that reverses a slice of strings in place, with a short explanation.\n"},
	{"en-reason", "A train leaves Turin at 9:00 at 120 km/h, another leaves Milan at 9:30 at 90 km/h toward Turin, 140 km apart. When do they meet? Show the steps.\n"},
	{"en-list", "List eight practical tips for writing maintainable unit tests.\n"},
}

type result struct {
	ID         string         `json:"id"`
	PredTokS   float64        `json:"predicted_per_second"`
	PromptTokS float64        `json:"prompt_per_second"`
	PredN      float64        `json:"predicted_n"`
	WallS      float64        `json:"wall_seconds"`
	Content    string         `json:"content"`
	Timings    map[string]any `json:"timings"`
}

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format(time.RFC3339), cat, fmt.Sprintf(format, a...))
}

func runOne(id, prompt string) (result, error) {
	payload, _ := json.Marshal(map[string]any{
		"prompt":      prompt,
		"n_predict":   256,
		"temperature": 0,
	})
	start := time.Now()
	resp, err := http.Post(baseURL+"/completion", "application/json", bytes.NewReader(payload))
	if err != nil {
		return result{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return result{}, fmt.Errorf("/completion -> %d: %s", resp.StatusCode, string(body))
	}
	var raw struct {
		Content string         `json:"content"`
		Timings map[string]any `json:"timings"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return result{}, err
	}
	r := result{ID: id, WallS: time.Since(start).Seconds(), Content: raw.Content, Timings: raw.Timings}
	if v, ok := raw.Timings["predicted_per_second"].(float64); ok {
		r.PredTokS = v
	}
	if v, ok := raw.Timings["prompt_per_second"].(float64); ok {
		r.PromptTokS = v
	}
	if v, ok := raw.Timings["predicted_n"].(float64); ok {
		r.PredN = v
	}
	return r, nil
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}

func main() {
	label := flag.String("label", "run", "config label, e.g. mtp-off / mtp-on-n2")
	flag.Parse()
	logf("SETUP", "spike 049 bench, label=%s, %d prompts, n_predict=256, greedy", *label, len(prompts))

	deadline := time.Now().Add(240 * time.Second)
	for {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			logf("SUMMARY", "FAILED — server never became healthy")
			os.Exit(1)
		}
		time.Sleep(3 * time.Second)
	}

	// Warm-up (excluded): loads weights into cache paths, primes CUDA graphs.
	if _, err := runOne("warmup", "Ciao, come stai?\n"); err != nil {
		logf("SUMMARY", "FAILED — server unreachable: %v", err)
		os.Exit(1)
	}
	logf("SETUP", "warm-up done")

	var results []result
	var speeds []float64
	for _, p := range prompts {
		r, err := runOne(p.ID, p.Prompt)
		if err != nil {
			logf("BENCH", "%s FAILED: %v", p.ID, err)
			os.Exit(1)
		}
		results = append(results, r)
		speeds = append(speeds, r.PredTokS)
		tj, _ := json.Marshal(r.Timings)
		logf("BENCH", "%s: %.1f tok/s gen (%.0f tokens, wall %.1fs) timings=%s", p.ID, r.PredTokS, r.PredN, r.WallS, tj)
	}

	sort.Float64s(speeds)
	p50, p95 := percentile(speeds, 0.5), percentile(speeds, 0.95)
	logf("SUMMARY", "label=%s gen tok/s p50=%.1f p95=%.1f min=%.1f max=%.1f", *label, p50, p95, speeds[0], speeds[len(speeds)-1])

	outDir := `D:\tmp\spike-049-results`
	_ = os.MkdirAll(outDir, 0o755)
	out := map[string]any{"label": *label, "p50": p50, "p95": p95, "results": results, "timestamp": time.Now().Format(time.RFC3339)}
	b, _ := json.MarshalIndent(out, "", "  ")
	path := filepath.Join(outDir, *label+".json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		logf("SUMMARY", "result write failed: %v", err)
		os.Exit(1)
	}
	logf("SUMMARY", "results written to %s", path)
}
