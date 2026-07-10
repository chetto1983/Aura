// Spike 096 — openrouter-reasoning-effort-wire-contract
//
// Counterpart to spike 095 (llama.cpp). Given Aura's PRIMARY chat backend
// (OpenRouter, DeepSeek-V4-Flash), when the composer effort selector's
// candidate wire shapes are sent per-request, then measure which fields
// actually toggle/graduate reasoning — settling the OpenRouter half of Phase
// 37E's D-03/D-08/D-09. The decisive question: does DeepSeek honor
// `reasoning.max_tokens` (real gradation) where `reasoning.effort` collapses
// low/med→high (spikes 040/042)? If yes, the TOKEN-BUDGET model unifies both
// backends (D-09 recommendation).
//
// Reads OPENROUTER_API_KEY (required) + AURA_LLM_MODEL (optional) from the env.
// Run (key loaded from .env WITHOUT printing it):
//
//	set -a; . ./.env; set +a; \
//	  go run ./.planning/spikes/096-openrouter-reasoning-effort-wire-contract
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
	endpoint = "https://openrouter.ai/api/v1/chat/completions"
	maxTok   = 2048
	prompt   = "A train travels 90 km in 1 hour 30 minutes. A second train covers 120 km in 2 hours. Which train is faster, and by how many km/h? Give the final answer in one short sentence."
)

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format(time.RFC3339), cat, fmt.Sprintf(format, a...))
}

type variant struct {
	name  string
	extra map[string]any
}

type result struct {
	name         string
	httpStatus   int
	reasoningLen int
	reasoningTok int
	contentLen   int
	completTok   int
	finish       string
	err          string
	contentHead  string
}

func main() {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		logf("FATAL", "OPENROUTER_API_KEY not set in env (source .env before running)")
		os.Exit(1)
	}
	model := os.Getenv("AURA_LLM_MODEL")
	if model == "" {
		model = "deepseek/deepseek-v4-flash"
	}
	logf("SETUP", "model=%s endpoint=%s", model, endpoint)

	tru := true
	fal := false
	variants := []variant{
		{"baseline", nil},
		{"effort_none", map[string]any{"reasoning": map[string]any{"effort": "none"}}},
		{"effort_low", map[string]any{"reasoning": map[string]any{"effort": "low"}}},
		{"effort_medium", map[string]any{"reasoning": map[string]any{"effort": "medium"}}},
		{"effort_high", map[string]any{"reasoning": map[string]any{"effort": "high"}}},
		{"max_tokens_256", map[string]any{"reasoning": map[string]any{"max_tokens": 256}}},
		{"max_tokens_512", map[string]any{"reasoning": map[string]any{"max_tokens": 512}}},
		{"max_tokens_2048", map[string]any{"reasoning": map[string]any{"max_tokens": 2048}}},
		{"enabled_false", map[string]any{"reasoning": map[string]any{"enabled": &fal}}},
		{"enabled_true", map[string]any{"reasoning": map[string]any{"enabled": &tru}}},
		{"effort_high_exclude", map[string]any{"reasoning": map[string]any{"effort": "high", "exclude": &tru}}},
	}

	results := make([]result, 0, len(variants))
	for _, v := range variants {
		r := probe(key, model, v)
		results = append(results, r)
		logf("PROBE", "%-20s http=%d reasoning=%dB/%dtok content=%dB completTok=%d finish=%s %s",
			r.name, r.httpStatus, r.reasoningLen, r.reasoningTok, r.contentLen, r.completTok, r.finish, r.err)
		time.Sleep(500 * time.Millisecond) // gentle on rate limits
	}
	summarize(results)
}

func probe(key, model string, v variant) result {
	body := map[string]any{
		"model":       model,
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
		"max_tokens":  maxTok,
		"temperature": 0,
		"stream":      false,
	}
	for k, val := range v.extra {
		body[k] = val
	}
	raw, _ := json.Marshal(body)

	r := result{name: v.name}
	req, _ := http.NewRequest("POST", endpoint, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Title", "aura-spike-096")
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
		r.err = "body: " + head(string(payload), 200)
		return r
	}

	var parsed struct {
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			CompletionTokens        int `json:"completion_tokens"`
			CompletionTokensDetails struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(payload, &parsed); err != nil {
		r.err = "decode: " + err.Error()
		return r
	}
	if len(parsed.Choices) == 0 {
		r.err = "no choices: " + head(string(payload), 160)
		return r
	}
	msg := parsed.Choices[0].Message
	r.reasoningLen = len(msg.Reasoning)
	r.reasoningTok = parsed.Usage.CompletionTokensDetails.ReasoningTokens
	r.contentLen = len(msg.Content)
	r.completTok = parsed.Usage.CompletionTokens
	r.finish = parsed.Choices[0].FinishReason
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
	logf("SUMMARY", "=== OpenRouter reasoning matrix (reasoning_tokens = ground truth; bytes hidden when exclude=true) ===")
	var baseTok int
	for _, r := range rs {
		if r.name == "baseline" {
			baseTok = r.reasoningTok
		}
	}
	logf("SUMMARY", "baseline reasoning_tokens = %d (thinking ON if >0)", baseTok)
	for _, r := range rs {
		logf("SUMMARY", "%-20s reasoning_tok=%-5d bytes=%-5d content=%-4d finish=%-6s | ans: %s",
			r.name, r.reasoningTok, r.reasoningLen, r.contentLen, r.finish, r.contentHead)
	}
	fmt.Println()
	logf("SUMMARY", "OFF-switch = reasoning_tokens→0 with a non-empty answer (effort_none / enabled_false).")
	logf("SUMMARY", "GRADATION test = do max_tokens_256 < max_tokens_512 < max_tokens_2048 in reasoning_tokens?")
	logf("SUMMARY", "COLLAPSE check = do effort_low/medium/high all yield ~same reasoning_tokens? (prior finding: yes on DeepSeek)")
	logf("SUMMARY", "If max_tokens graduates but effort collapses → 37E should define levels by reasoning.max_tokens (unifies w/ llama.cpp thinking_budget_tokens).")
}
