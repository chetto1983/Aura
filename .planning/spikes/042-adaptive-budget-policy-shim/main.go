package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

type Complexity string

const (
	NoReasoning    Complexity = "NO_REASONING"
	SmallReasoning Complexity = "SMALL_REASONING"
	DeepReasoning  Complexity = "DEEP_REASONING"
)

type Decision struct {
	Complexity      Complexity
	Confidence      float64
	MaxTokens       int
	Reason          string
	Reasoning       ReasoningProjection
	ReasoningJSON   string
	ReasoningSource string
}

type ReasoningProjection struct {
	Effort  string `json:"effort"`
	Exclude bool   `json:"exclude"`
}

type Policy struct {
	NoReasoningMaxTokens    int
	SmallReasoningMaxTokens int
	DeepReasoningMaxTokens  int
}

type sample struct {
	Query        string
	Want         Complexity
	WantTokens   int
	WantEffort   string
	BenchmarkTag string
}

func main() {
	p := Policy{
		NoReasoningMaxTokens:    512,
		SmallReasoningMaxTokens: 2048,
		DeepReasoningMaxTokens:  4096,
	}
	samples := []sample{
		{Query: "ciao", Want: NoReasoning, WantTokens: p.NoReasoningMaxTokens, WantEffort: "none", BenchmarkTag: "operator/no-reasoning"},
		{Query: "cerca notizie di cuneo", Want: SmallReasoning, WantTokens: p.SmallReasoningMaxTokens, WantEffort: "low", BenchmarkTag: "operator/small-reasoning"},
		{Query: "scrivi uno script di scraping di la stampa", Want: DeepReasoning, WantTokens: p.DeepReasoningMaxTokens, WantEffort: "high", BenchmarkTag: "operator/deep-reasoning"},
		{Query: "What is 15 + 27?", Want: NoReasoning, WantTokens: p.NoReasoningMaxTokens, WantEffort: "none", BenchmarkTag: "baseline/trivial"},
		{Query: "Summarize this meeting in one sentence.", Want: NoReasoning, WantTokens: p.NoReasoningMaxTokens, WantEffort: "none", BenchmarkTag: "baseline/simple-generation"},
		{Query: "Prove that the square root of 2 is irrational using contradiction.", Want: DeepReasoning, WantTokens: p.DeepReasoningMaxTokens, WantEffort: "high", BenchmarkTag: "baseline/proof"},
		{Query: "Analyze the tradeoffs between local memory retrieval and remote long-context prompting for a production agent.", Want: DeepReasoning, WantTokens: p.DeepReasoningMaxTokens, WantEffort: "high", BenchmarkTag: "baseline/architecture"},
		{Query: "Debug this failing distributed scheduler: two workers claim the same job after a crash and DST transition.", Want: DeepReasoning, WantTokens: p.DeepReasoningMaxTokens, WantEffort: "high", BenchmarkTag: "baseline/debug"},
		{Query: "Who wrote The Name of the Rose?", Want: NoReasoning, WantTokens: p.NoReasoningMaxTokens, WantEffort: "none", BenchmarkTag: "baseline/factual"},
	}

	failures := 0
	for _, s := range samples {
		req := llm.Request{
			Model:       "deepseek/deepseek-v4-flash:exacto",
			Temperature: 0.2,
			MaxTokens:   4096,
			ToolChoice:  "auto",
			Messages: []llm.Message{
				{Role: llm.RoleSystem, Content: "STATIC-AURA-SYSTEM"},
				{Role: llm.RoleUser, Content: s.Query},
			},
		}

		before, _ := json.Marshal(req.Messages[0])
		adapted, decision := p.Apply(req, s.Query)
		afterOriginal, _ := json.Marshal(req.Messages[0])
		afterAdapted, _ := json.Marshal(adapted.Messages[0])

		if decision.Complexity != s.Want {
			failures++
			log("MISS", "tag=%s query=%q complexity=%s want=%s reason=%s", s.BenchmarkTag, s.Query, decision.Complexity, s.Want, decision.Reason)
			continue
		}
		if string(before) != string(afterOriginal) || string(before) != string(afterAdapted) {
			failures++
			log("MISS", "tag=%s query=%q mutated messages[0]", s.BenchmarkTag, s.Query)
			continue
		}
		if adapted.ToolChoice != req.ToolChoice {
			failures++
			log("MISS", "tag=%s query=%q changed ToolChoice from %q to %q", s.BenchmarkTag, s.Query, req.ToolChoice, adapted.ToolChoice)
			continue
		}
		if adapted.MaxTokens != s.WantTokens {
			failures++
			log("MISS", "tag=%s query=%q max_tokens=%d want=%d", s.BenchmarkTag, s.Query, adapted.MaxTokens, s.WantTokens)
			continue
		}
		if decision.Reasoning.Effort != s.WantEffort {
			failures++
			log("MISS", "tag=%s query=%q reasoning_effort=%q want=%q", s.BenchmarkTag, s.Query, decision.Reasoning.Effort, s.WantEffort)
			continue
		}
		log("BENCH", "tag=%s query=%q -> tier=%s effort=%s max_tokens=%d confidence=%.2f reasoning=%s reason=%s",
			s.BenchmarkTag, s.Query, decision.Complexity, decision.Reasoning.Effort, adapted.MaxTokens, decision.Confidence, decision.ReasoningJSON, decision.Reason)
	}

	if failures > 0 {
		log("SUMMARY", "PARTIAL: %d policy assertions failed", failures)
		os.Exit(1)
	}

	log("FINDING", "Portable AutoThink subset = classify query difficulty, set provider-neutral MaxTokens, and project OpenRouter reasoning effort separately.")
	log("FINDING", "Operator benchmark: ciao -> none, cerca notizie di cuneo -> low, scraping script -> high.")
	log("FINDING", "The shim leaves messages[0] byte-stable and does not require ToolChoice changes, hidden-state access, or think-tag injection.")
	log("FINDING", "Accuracy claims remain unvalidated; this spike validates integration shape, not GPQA/MMLU performance.")
	log("SUMMARY", "VALIDATED: adaptive budget + reasoning-tier shim fits Aura's current llm.Request seam")
}

func (p Policy) Apply(req llm.Request, query string) (llm.Request, Decision) {
	decision := classify(query)
	out := req
	out.Messages = append([]llm.Message(nil), req.Messages...)
	out.Tools = append([]llm.ToolDef(nil), req.Tools...)
	switch decision.Complexity {
	case DeepReasoning:
		out.MaxTokens = p.DeepReasoningMaxTokens
	case SmallReasoning:
		out.MaxTokens = p.SmallReasoningMaxTokens
	default:
		out.MaxTokens = p.NoReasoningMaxTokens
	}
	decision.MaxTokens = out.MaxTokens
	decision.Reasoning = projectOpenRouterReasoning(decision.Complexity)
	reasoningJSON, _ := json.Marshal(decision.Reasoning)
	decision.ReasoningJSON = string(reasoningJSON)
	decision.ReasoningSource = "projected-only: llm.Request has no request-side Reasoning field"
	return out, decision
}

func classify(query string) Decision {
	q := strings.ToLower(query)
	deepIndicators := []string{
		"analyze", "compare", "debug", "prove", "justify", "derive", "design",
		"tradeoff", "tradeoffs", "architecture", "distributed", "multi-step",
		"step by step", "complex", "why", "how", "failure", "production",
		"scheduler", "memory retrieval", "contradiction", "script", "scraping",
		"scrape", "crawler", "codice", "codifica", "implement",
	}
	smallIndicators := []string{
		"cerca", "notizie", "news", "search", "trova", "aggiornamenti",
		"lookup", "ricerca", "recupera", "fonti", "sources",
	}
	deepHits := 0
	for _, indicator := range deepIndicators {
		if strings.Contains(q, indicator) {
			deepHits++
		}
	}
	smallHits := 0
	for _, indicator := range smallIndicators {
		if strings.Contains(q, indicator) {
			smallHits++
		}
	}
	wordCount := len(strings.Fields(query))
	deepScore := float64(deepHits)*0.35 + minFloat(float64(wordCount)/40.0, 0.55)
	if deepHits >= 1 && deepScore >= 0.55 {
		return Decision{
			Complexity: DeepReasoning,
			Confidence: minFloat(0.55+deepScore/2, 0.95),
			Reason:     fmt.Sprintf("%d deep indicators, %d small indicators, %d words", deepHits, smallHits, wordCount),
		}
	}
	if smallHits >= 1 {
		score := minFloat(float64(smallHits)*0.25+float64(wordCount)/80.0, 0.70)
		return Decision{
			Complexity: SmallReasoning,
			Confidence: minFloat(0.65+score/2, 0.90),
			Reason:     fmt.Sprintf("%d deep indicators, %d small indicators, %d words", deepHits, smallHits, wordCount),
		}
	}
	return Decision{
		Complexity: NoReasoning,
		Confidence: maxFloat(0.60, 0.90-deepScore/2),
		Reason:     fmt.Sprintf("%d deep indicators, %d small indicators, %d words", deepHits, smallHits, wordCount),
	}
}

func projectOpenRouterReasoning(c Complexity) ReasoningProjection {
	switch c {
	case DeepReasoning:
		return ReasoningProjection{Effort: "high", Exclude: false}
	case SmallReasoning:
		return ReasoningProjection{Effort: "low", Exclude: false}
	default:
		return ReasoningProjection{Effort: "none", Exclude: true}
	}
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func log(category, format string, args ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().Format(time.RFC3339), category, fmt.Sprintf(format, args...))
}
