// Spike 095 — llama-cpp-reasoning-effort-wire-contract
//
// Given llama.cpp server-cuda serving gemma-4-E2B-it-qat (a validated local
// thinking model, spike 048/050) on the RTX A2000 4 GB, when the SAME
// reasoning-inducing prompt is sent under each candidate PER-REQUEST wire
// shape, then observe which field actually toggles/graduates the
// `reasoning_content` channel — yielding the exact wire branch Aura's
// internal/llm/openai_compat client needs for the llama.cpp path (Phase 37E
// D-08, composer off/low/mid/high/auto effort selector).
//
// Candidate per-request shapes probed:
//   - baseline               (no reasoning field → server default = auto)
//   - aura_nested_none        {"reasoning":{"effort":"none","exclude":true}}  (Aura's CURRENT OpenRouter shape)
//   - aura_nested_high        {"reasoning":{"effort":"high"}}
//   - llama_reasoning_off     {"reasoning":"off"}                              (llama.cpp native string)
//   - llama_reasoning_on      {"reasoning":"on"}
//   - enable_thinking_false   {"chat_template_kwargs":{"enable_thinking":false}} (spike 050 knob)
//   - enable_thinking_true    {"chat_template_kwargs":{"enable_thinking":true}}
//   - reasoning_effort_low    {"reasoning_effort":"low"}                        (OpenAI top-level)
//   - reasoning_budget_0      {"reasoning_budget":0}                            (immediate-end)
//   - reasoning_budget_64     {"reasoning_budget":64}                           (small budget → graduation?)
//
// Server (PowerShell, never Git-Bash — MSYS mangles /models; --jinja is REQUIRED
// for chat_template_kwargs + reasoning parsing):
//
//	docker run -d --name spike095 --gpus all -p 8096:8080 `
//	  -v D:\tmp\spike-095-models:/models:ro `
//	  ghcr.io/ggml-org/llama.cpp:server-cuda `
//	  -m /models/gemma-4-E2B-it-qat-UD-Q4_K_XL.gguf `
//	  -ngl 99 -c 4096 --temp 0 --jinja --reasoning-format auto `
//	  --host 0.0.0.0 --port 8080
//
// then: go run ./.planning/spikes/095-llama-cpp-reasoning-effort-wire-contract
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	baseURL      = "http://127.0.0.1:8099"
	bootDeadline = 240 * time.Second
	maxTokens    = 768
	// A prompt that reliably induces a short chain-of-thought on a thinking model,
	// so "thinking ON" yields a non-empty reasoning_content and "thinking OFF"
	// answers directly. Kept deterministic (temp 0 on the server).
	prompt = "A train travels 90 km in 1 hour 30 minutes. A second train covers 120 km in 2 hours. Which train is faster, and by how many km/h? Give the final answer in one short sentence."
)

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format(time.RFC3339), cat, fmt.Sprintf(format, a...))
}

// variant is one per-request wire shape. extra is merged onto the base body.
type variant struct {
	name  string
	extra map[string]any
}

type result struct {
	name         string
	httpStatus   int
	reasoningLen int
	contentLen   int
	finish       string
	promptTok    int
	completTok   int
	err          string
	contentHead  string
}

func main() {
	if !waitHealthy() {
		logf("FATAL", "server never became healthy at %s within %s", baseURL, bootDeadline)
		os.Exit(1)
	}

	variants := []variant{
		{"baseline", nil},
		{"aura_nested_none", map[string]any{"reasoning": map[string]any{"effort": "none", "exclude": true}}},
		{"aura_nested_high", map[string]any{"reasoning": map[string]any{"effort": "high"}}},
		{"llama_reasoning_off", map[string]any{"reasoning": "off"}},
		{"llama_reasoning_on", map[string]any{"reasoning": "on"}},
		{"enable_thinking_false", map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": false}}},
		{"enable_thinking_true", map[string]any{"chat_template_kwargs": map[string]any{"enable_thinking": true}}},
		{"reasoning_effort_low", map[string]any{"reasoning_effort": "low"}},
		// llama.cpp per-request thinking budget (discussion #21445). Server MUST
		// be started WITHOUT --reasoning-budget (default -1) for these to apply.
		// baseline reasoning ≈ 528 tokens, so 64/128/256 should CAP visibly.
		{"thinking_budget_64", map[string]any{"thinking_budget_tokens": 64}},
		{"thinking_budget_128", map[string]any{"thinking_budget_tokens": 128}},
		{"thinking_budget_256", map[string]any{"thinking_budget_tokens": 256}},
		{"thinking_budget_1024", map[string]any{"thinking_budget_tokens": 1024}},
	}

	results := make([]result, 0, len(variants))
	for _, v := range variants {
		r := probe(v)
		results = append(results, r)
		logf("PROBE", "%-22s http=%d reasoning=%d content=%d finish=%s tok(p/c)=%d/%d %s",
			r.name, r.httpStatus, r.reasoningLen, r.contentLen, r.finish, r.promptTok, r.completTok, r.err)
	}

	summarize(results)
}

func waitHealthy() bool {
	logf("SETUP", "waiting for %s/health (deadline %s)…", baseURL, bootDeadline)
	deadline := time.Now().Add(bootDeadline)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				logf("SETUP", "server healthy")
				return true
			}
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

func probe(v variant) result {
	body := map[string]any{
		"model":       "gemma-4-e2b",
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
		"max_tokens":  maxTokens,
		"temperature": 0,
		"stream":      false,
	}
	for k, val := range v.extra {
		body[k] = val
	}
	raw, _ := json.Marshal(body)

	r := result{name: v.name}
	req, _ := http.NewRequest("POST", baseURL+"/v1/chat/completions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		r.err = "req: " + err.Error()
		return r
	}
	defer resp.Body.Close()
	r.httpStatus = resp.StatusCode
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		r.err = "body: " + head(string(payload), 160)
		return r
	}

	var parsed struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		r.err = "decode: " + err.Error()
		return r
	}
	if len(parsed.Choices) == 0 {
		r.err = "no choices"
		return r
	}
	msg := parsed.Choices[0].Message
	r.reasoningLen = len(msg.ReasoningContent)
	r.contentLen = len(msg.Content)
	r.finish = parsed.Choices[0].FinishReason
	r.promptTok = parsed.Usage.PromptTokens
	r.completTok = parsed.Usage.CompletionTokens
	r.contentHead = head(msg.Content, 80)
	return r
}

func head(s string, n int) string {
	s = trimNL(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func trimNL(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' {
			r = ' '
		}
		out = append(out, r)
	}
	return string(out)
}

func summarize(rs []result) {
	fmt.Println()
	logf("SUMMARY", "=== reasoning-toggle matrix (reasoning_content length in bytes) ===")
	var baseReasoning int
	for _, r := range rs {
		if r.name == "baseline" {
			baseReasoning = r.reasoningLen
		}
	}
	logf("SUMMARY", "baseline reasoning_content = %d bytes (thinking ON if >0)", baseReasoning)
	for _, r := range rs {
		verdict := classify(r, baseReasoning)
		logf("SUMMARY", "%-22s reasoning=%-6d content=%-5d → %s | ans: %s",
			r.name, r.reasoningLen, r.contentLen, verdict, r.contentHead)
	}
	fmt.Println()
	logf("SUMMARY", "OFF-switch = a variant whose reasoning_content drops to ~0 while content stays non-empty.")
	logf("SUMMARY", "GRADATION = thinking_budget_* reasoning_content scales with the budget (proves llama.cpp webui Low/Med/High/Max).")
	logf("SUMMARY", "Aura today = aura_nested_* ; if those match baseline, llama-server IGNORES the OpenRouter shape → 37E needs a llama.cpp wire branch.")
}

func classify(r result, base int) string {
	switch {
	case r.err != "":
		return "ERR " + r.err
	case base > 0 && r.reasoningLen == 0 && r.contentLen > 0:
		return "OFF (thinking suppressed, answered)"
	case base > 0 && r.reasoningLen > 0 && r.reasoningLen < base/2:
		return "REDUCED (partial budget)"
	case r.reasoningLen > 0:
		return "ON (thinking present)"
	case r.reasoningLen == 0 && r.contentLen == 0:
		return "EMPTY (runaway/blocked?)"
	default:
		return "?"
	}
}
