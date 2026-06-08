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
	Low  Complexity = "LOW"
	High Complexity = "HIGH"
)

type Decision struct {
	Complexity Complexity
	Confidence float64
	MaxTokens  int
	Reason     string
}

type Policy struct {
	LowMaxTokens  int
	HighMaxTokens int
}

type sample struct {
	Query string
	Want  Complexity
}

func main() {
	p := Policy{LowMaxTokens: 1024, HighMaxTokens: 4096}
	samples := []sample{
		{Query: "What is 15 + 27?", Want: Low},
		{Query: "Summarize this meeting in one sentence.", Want: Low},
		{Query: "Prove that the square root of 2 is irrational using contradiction.", Want: High},
		{Query: "Analyze the tradeoffs between local memory retrieval and remote long-context prompting for a production agent.", Want: High},
		{Query: "Debug this failing distributed scheduler: two workers claim the same job after a crash and DST transition.", Want: High},
		{Query: "Who wrote The Name of the Rose?", Want: Low},
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
			log("MISS", "query=%q complexity=%s want=%s reason=%s", s.Query, decision.Complexity, s.Want, decision.Reason)
			continue
		}
		if string(before) != string(afterOriginal) || string(before) != string(afterAdapted) {
			failures++
			log("MISS", "query=%q mutated messages[0]", s.Query)
			continue
		}
		if adapted.ToolChoice != req.ToolChoice {
			failures++
			log("MISS", "query=%q changed ToolChoice from %q to %q", s.Query, req.ToolChoice, adapted.ToolChoice)
			continue
		}
		wantTokens := p.LowMaxTokens
		if s.Want == High {
			wantTokens = p.HighMaxTokens
		}
		if adapted.MaxTokens != wantTokens {
			failures++
			log("MISS", "query=%q max_tokens=%d want=%d", s.Query, adapted.MaxTokens, wantTokens)
			continue
		}
		log("CHECK", "query=%q -> complexity=%s confidence=%.2f max_tokens=%d", s.Query, decision.Complexity, decision.Confidence, adapted.MaxTokens)
	}

	if failures > 0 {
		log("SUMMARY", "PARTIAL: %d policy assertions failed", failures)
		os.Exit(1)
	}

	log("FINDING", "Portable AutoThink subset = classify query difficulty and set provider-neutral MaxTokens.")
	log("FINDING", "The shim leaves messages[0] byte-stable and does not require ToolChoice changes, hidden-state access, or think-tag injection.")
	log("FINDING", "Accuracy claims remain unvalidated; this spike validates integration shape, not GPQA/MMLU performance.")
	log("SUMMARY", "VALIDATED: adaptive budget shim fits Aura's current llm.Request seam")
}

func (p Policy) Apply(req llm.Request, query string) (llm.Request, Decision) {
	decision := classify(query)
	out := req
	out.Messages = append([]llm.Message(nil), req.Messages...)
	out.Tools = append([]llm.ToolDef(nil), req.Tools...)
	if decision.Complexity == High {
		out.MaxTokens = p.HighMaxTokens
	} else {
		out.MaxTokens = p.LowMaxTokens
	}
	decision.MaxTokens = out.MaxTokens
	return out, decision
}

func classify(query string) Decision {
	q := strings.ToLower(query)
	indicators := []string{
		"analyze", "compare", "debug", "prove", "justify", "derive", "design",
		"tradeoff", "tradeoffs", "architecture", "distributed", "multi-step",
		"step by step", "complex", "why", "how", "failure", "production",
		"scheduler", "memory retrieval", "contradiction",
	}
	hits := 0
	for _, indicator := range indicators {
		if strings.Contains(q, indicator) {
			hits++
		}
	}
	wordCount := len(strings.Fields(query))
	score := float64(hits)*0.35 + minFloat(float64(wordCount)/40.0, 0.55)
	if hits >= 1 && score >= 0.55 {
		return Decision{
			Complexity: High,
			Confidence: minFloat(0.55+score/2, 0.95),
			Reason:     fmt.Sprintf("%d complexity indicators, %d words", hits, wordCount),
		}
	}
	return Decision{
		Complexity: Low,
		Confidence: maxFloat(0.60, 0.90-score/2),
		Reason:     fmt.Sprintf("%d complexity indicators, %d words", hits, wordCount),
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
