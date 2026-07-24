package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/semindex"
)

const (
	defaultModelURL = "http://127.0.0.1:18080"
	defaultEmbedURL = "http://127.0.0.1:8081"
	embedDim        = 768
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Minute)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "[SUMMARY] INVALIDATED error=%q\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	runID := "adaptive-102-" + time.Now().UTC().Format("20060102T150405Z")
	repeats := envInt("AURA_ADAPTIVE_PROOF_REPEATS", 2)
	workers := envInt("AURA_ADAPTIVE_BENCH_WORKERS", 1)
	modelURL := envOr("AURA_ADAPTIVE_BENCH_MODEL_URL", defaultModelURL)
	embedURL := envOr("AURA_EMBED_BASE_URL", defaultEmbedURL)
	logf("SETUP", "run=%s model=%s embed=%s repeats=%d workers=%d", runID, modelURL, embedURL, repeats, workers)

	embedder := &documents.EmbeddingClient{
		BaseURL:    embedURL,
		Model:      "granite-embedding-311m-multilingual-r2",
		Dimensions: embedDim,
		Client:     &http.Client{Timeout: 90 * time.Second},
	}
	scenarios := append(reasoningScenarios(), toolScenarios()...)
	scenarios = append(scenarios, skillScenarios()...)
	knowledge, facts := knowledgeCorpus()
	scenarios = append(scenarios, knowledge...)
	prompts := make([]string, len(scenarios))
	for i := range scenarios {
		prompts[i] = scenarios[i].Prompt
	}
	vectors, err := embedder.Embed(ctx, prompts)
	if err != nil {
		return fmt.Errorf("embed scenarios: %w", err)
	}
	for i := range scenarios {
		scenarios[i].Embedding = vectors[i]
	}

	factTexts := make([]string, len(facts))
	for i := range facts {
		factTexts[i] = facts[i].Text
	}
	factVectors, err := embedder.Embed(ctx, factTexts)
	if err != nil {
		return fmt.Errorf("embed facts: %w", err)
	}
	graph, err := openFactGraph(ctx, os.Getenv("NEO4J_PASSWORD"), runID, embedDim)
	if err != nil {
		return fmt.Errorf("open Neo4j fact graph: %w", err)
	}
	defer graph.close(context.Background())
	if err := graph.seed(ctx, facts, factVectors); err != nil {
		return fmt.Errorf("seed Neo4j fact graph: %w", err)
	}

	toolCandidates, err := buildCandidates(ctx, embedder, toolCatalog(), scenarios, "tool")
	if err != nil {
		return err
	}
	skillCandidates, err := buildCandidates(ctx, embedder, skillCatalog(), scenarios, "skill")
	if err != nil {
		return err
	}
	for i := range scenarios {
		switch scenarios[i].Domain {
		case "tool":
			scenarios[i].Candidates = toolCandidates[scenarios[i].ID]
		case "skill":
			scenarios[i].Candidates = skillCandidates[scenarios[i].ID]
		case "knowledge":
			vectorContext, graphContext, err := graph.contexts(ctx, scenarios[i].Embedding)
			if err != nil {
				return fmt.Errorf("%s retrieve: %w", scenarios[i].ID, err)
			}
			scenarios[i].Contexts = map[string]string{
				"none":         "",
				"vector_top1":  vectorContext,
				"graph_expand": graphContext,
			}
		}
	}
	logf("CORPUS", "scenarios=%d facts=%d tools=%d skills=%d", len(scenarios), len(facts), len(toolCatalog()), len(skillCatalog()))

	client := &llmClient{baseURL: modelURL, client: &http.Client{Timeout: 90 * time.Second}}
	jobs := make(chan job)
	results := make(chan outcome)
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				results <- execute(ctx, client, j)
			}
		}()
	}
	allJobs := make([]job, 0, len(scenarios)*3*repeats)
	for _, sc := range scenarios {
		for _, action := range actions(sc.Domain) {
			for repeat := 0; repeat < repeats; repeat++ {
				allJobs = append(allJobs, job{Scenario: sc, Action: action, Repeat: repeat})
			}
		}
	}
	rand.New(rand.NewSource(9701)).Shuffle(len(allJobs), func(i, j int) {
		allJobs[i], allJobs[j] = allJobs[j], allJobs[i]
	})
	go func() {
		for _, item := range allJobs {
			jobs <- item
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()
	outcomes := make([]outcome, 0, len(scenarios)*3*repeats)
	for result := range results {
		outcomes = append(outcomes, result)
		status := "ok"
		if result.Error != "" {
			status = "error"
		}
		logf("RUN", "scenario=%s domain=%s action=%s repeat=%d correct=%t tokens=%d latency_ms=%.1f status=%s",
			result.ScenarioID, result.Domain, result.Action, result.Repeat, result.Correct, result.TotalTokens, result.LatencyMS, status)
	}
	sort.Slice(outcomes, func(i, j int) bool {
		if outcomes[i].ScenarioID != outcomes[j].ScenarioID {
			return outcomes[i].ScenarioID < outcomes[j].ScenarioID
		}
		if outcomes[i].Action != outcomes[j].Action {
			return outcomes[i].Action < outcomes[j].Action
		}
		return outcomes[i].Repeat < outcomes[j].Repeat
	})
	assignRewards(outcomes)
	summaries := summarize(outcomes)
	proof, err := evaluateProof(ctx, scenarios, outcomes, embedder, repeats)
	if err != nil {
		return fmt.Errorf("evaluate proof: %w", err)
	}
	surface := rewardSurface{
		SchemaVersion: "2.0",
		RunID:         runID,
		CreatedAt:     time.Now().UTC(),
		Model:         "unsloth/Qwen3.5-2B-GGUF:Qwen3.5-2B-Q4_K_M.gguf",
		ModelURL:      modelURL,
		EmbedURL:      embedURL,
		EmbedDim:      embedDim,
		Repeats:       repeats,
		Scenarios:     scenarios,
		Outcomes:      outcomes,
		Summary:       summaries,
		Proof:         proof,
	}
	dir := filepath.Join(".planning", "spikes", "102-adaptive-generalization-ope-gate", "artifacts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(surface, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "generalization-ope-proof.json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return err
	}
	for key, item := range summaries {
		logf("METRIC", "%s runs=%d accuracy=%.3f tokens=%.1f latency_ms=%.1f errors=%d",
			key, item.Runs, item.Accuracy, item.MeanTokens, item.MeanLatencyMS, item.ErrorCount)
	}
	if hasErrors(outcomes) {
		return fmt.Errorf("one or more live executions failed; artifact retained at %s", path)
	}
	for _, metric := range proof.PolicyMetrics {
		logf("PROOF", "policy=%s utility=%.4f regret=%.4f accuracy=%.3f delta=%.4f ci=[%.4f,%.4f]",
			metric.Policy, metric.MeanUtility, metric.MeanRegret, metric.Accuracy,
			metric.UtilityDelta, metric.DeltaCILower, metric.DeltaCIUpper)
	}
	for _, metric := range proof.OPE {
		logf("OPE", "estimator=%s estimate=%.4f truth=%.4f abs_error=%.4f ess=%.1f ci=[%.4f,%.4f] covers=%t",
			metric.Estimator, metric.Estimate, metric.GroundTruth, metric.AbsoluteError,
			metric.EffectiveN, metric.CILower, metric.CIUpper, metric.CoversTruth)
	}
	if proof.Verdict != "VALIDATED" {
		return fmt.Errorf("proof gate %s (generalization=%t ope=%t); artifact retained at %s",
			proof.Verdict, proof.GeneralizationPassed, proof.OPEPassed, path)
	}
	logf("SUMMARY", "VALIDATED artifact=%s outcomes=%d", path, len(outcomes))
	return nil
}

type job struct {
	Scenario scenario
	Action   string
	Repeat   int
}

func actions(domain string) []string {
	switch domain {
	case "reasoning":
		return []string{"off", "low_256", "high_768"}
	case "tool":
		return []string{"semantic_top1", "qwen_top3", "qwen_full"}
	case "skill":
		return []string{"semantic_top1", "qwen_top3", "qwen_full"}
	case "knowledge":
		return []string{"none", "vector_top1", "graph_expand"}
	default:
		panic("unknown domain " + domain)
	}
}

func execute(ctx context.Context, client *llmClient, j job) outcome {
	base := outcome{ScenarioID: j.Scenario.ID, Domain: j.Scenario.Domain, Action: j.Action, Repeat: j.Repeat}
	var result llmResult
	var err error
	switch j.Scenario.Domain {
	case "reasoning":
		opts := llmOptions{System: "Return only the requested final answer with no explanation.", MaxTokens: 1024}
		switch j.Action {
		case "off":
			opts.Thinking = false
		case "low_256":
			opts.Thinking, opts.ThinkingBudget = true, 256
		case "high_768":
			opts.Thinking, opts.ThinkingBudget = true, 768
		}
		result, err = client.complete(ctx, j.Scenario.Prompt, opts)
		base.Selected = j.Action
	case "tool":
		candidates := j.Scenario.Candidates[j.Action]
		if j.Action == "semantic_top1" {
			base.Selected = candidates[0]
			base.Correct = sameAnswer(base.Selected, j.Scenario.Expected)
			return base
		}
		items := selectCatalog(toolCatalog(), candidates)
		tools := make([]map[string]any, 0, len(items))
		for _, item := range items {
			tools = append(tools, functionTool(item))
		}
		result, err = client.complete(ctx, j.Scenario.Prompt, llmOptions{
			System: "Call exactly one available function that best fulfills the request. Do not answer in text.",
			Tools:  tools, Thinking: false, MaxTokens: 192,
		})
		base.Selected = result.ToolName
	case "skill":
		candidates := j.Scenario.Candidates[j.Action]
		if j.Action == "semantic_top1" {
			base.Selected = candidates[0]
			base.Correct = sameAnswer(base.Selected, j.Scenario.Expected)
			return base
		}
		items := selectCatalog(skillCatalog(), candidates)
		result, err = client.complete(ctx,
			"Available skills:\n"+catalogPrompt(items)+"\nUser request:\n"+j.Scenario.Prompt,
			llmOptions{
				System: "Call activate_skill exactly once with the best matching skill. Do not answer in text.",
				Tools:  []map[string]any{skillTool(items)}, Thinking: false, MaxTokens: 192,
			})
		base.Selected = extractSkillName(result.ToolArguments, result.ToolName)
	case "knowledge":
		contextText := j.Scenario.Contexts[j.Action]
		prompt := j.Scenario.Prompt
		if contextText != "" {
			prompt = "Private context:\n" + contextText + "\n\nQuestion:\n" + prompt
		}
		result, err = client.complete(ctx, prompt, llmOptions{
			System:   "Answer only from supplied private context. If it is insufficient, answer UNKNOWN. Return only the short answer.",
			Thinking: false, MaxTokens: 96,
		})
		base.Selected = j.Action
	}
	if err != nil {
		base.Error = err.Error()
		return base
	}
	base.Content = strings.TrimSpace(result.Content)
	base.ReasoningChars = len([]rune(result.Reasoning))
	base.PromptTokens = result.PromptTokens
	base.CompletionTokens = result.CompletionTokens
	base.TotalTokens = result.TotalTokens
	base.LatencyMS = result.LatencyMS
	if j.Scenario.Domain == "tool" {
		base.Correct = sameAnswer(base.Selected, j.Scenario.Expected)
	} else if j.Scenario.Domain == "skill" {
		if base.Selected == "" {
			base.Selected = parseSkillArgument(result.Content)
		}
		base.Correct = sameAnswer(base.Selected, j.Scenario.Expected)
	} else {
		base.Correct = sameAnswer(base.Content, j.Scenario.Expected)
	}
	return base
}

func buildCandidates(ctx context.Context, embedder semindex.Embedder, catalog []catalogItem, scenarios []scenario, domain string) (map[string]map[string][]string, error) {
	docs := make([]string, len(catalog))
	docToName := make(map[string]string, len(catalog))
	for i, item := range catalog {
		docs[i] = item.Name + ": " + item.Description
		docToName[docs[i]] = item.Name
	}
	ranker := semindex.NewRanker(embedder)
	if err := ranker.Add(ctx, docs...); err != nil {
		return nil, fmt.Errorf("build %s ranker: %w", domain, err)
	}
	out := make(map[string]map[string][]string)
	for _, sc := range scenarios {
		if sc.Domain != domain {
			continue
		}
		result := ranker.Rank(ctx, sc.Prompt, len(catalog))
		if result.Err != nil {
			return nil, result.Err
		}
		ranked := make([]string, 0, len(result.Results))
		for _, item := range result.Results {
			ranked = append(ranked, docToName[item.Label])
		}
		out[sc.ID] = map[string][]string{
			"semantic_top1": ranked[:1],
			"qwen_top3":     ranked[:min(3, len(ranked))],
			"qwen_full":     ranked,
		}
	}
	return out, nil
}

func selectCatalog(catalog []catalogItem, names []string) []catalogItem {
	byName := make(map[string]catalogItem, len(catalog))
	for _, item := range catalog {
		byName[item.Name] = item
	}
	out := make([]catalogItem, 0, len(names))
	for _, name := range names {
		if item, ok := byName[name]; ok {
			out = append(out, item)
		}
	}
	return out
}

func assignRewards(outcomes []outcome) {
	maxTokens := map[string]float64{}
	maxLatency := map[string]float64{}
	for _, item := range outcomes {
		maxTokens[item.Domain] = math.Max(maxTokens[item.Domain], float64(item.TotalTokens))
		maxLatency[item.Domain] = math.Max(maxLatency[item.Domain], item.LatencyMS)
	}
	profiles := map[string][2]float64{
		"quality_first": {0.02, 0.01},
		"balanced":      {0.08, 0.04},
		"economy":       {0.18, 0.08},
	}
	for i := range outcomes {
		item := &outcomes[i]
		quality := 0.0
		if item.Correct {
			quality = 1
		}
		if item.Error != "" {
			quality = -0.25
		}
		tokenNorm, latencyNorm := 0.0, 0.0
		if maxTokens[item.Domain] > 0 {
			tokenNorm = float64(item.TotalTokens) / maxTokens[item.Domain]
		}
		if maxLatency[item.Domain] > 0 {
			latencyNorm = item.LatencyMS / maxLatency[item.Domain]
		}
		item.Rewards = make(map[string]float64, len(profiles))
		for name, weights := range profiles {
			item.Rewards[name] = quality - weights[0]*tokenNorm - weights[1]*latencyNorm
		}
	}
}

func summarize(outcomes []outcome) map[string]summary {
	type acc struct {
		runs, correct, errors int
		tokens, latency       float64
	}
	accs := map[string]*acc{}
	for _, item := range outcomes {
		key := item.Domain + "/" + item.Action
		if accs[key] == nil {
			accs[key] = &acc{}
		}
		a := accs[key]
		a.runs++
		if item.Correct {
			a.correct++
		}
		if item.Error != "" {
			a.errors++
		}
		a.tokens += float64(item.TotalTokens)
		a.latency += item.LatencyMS
	}
	out := make(map[string]summary, len(accs))
	for key, a := range accs {
		out[key] = summary{
			Runs:          a.runs,
			Accuracy:      float64(a.correct) / float64(a.runs),
			MeanTokens:    a.tokens / float64(a.runs),
			MeanLatencyMS: a.latency / float64(a.runs),
			ErrorCount:    a.errors,
		}
	}
	return out
}

func sameAnswer(got, expected string) bool {
	normalize := func(value string) string {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.Trim(value, "`'\".,;:!?$€ ")
		value = strings.ReplaceAll(value, "seven", "7")
		value = strings.ReplaceAll(value, "eighteen", "18")
		value = strings.ReplaceAll(value, "ninety", "90")
		value = strings.ReplaceAll(value, "nine", "9")
		value = strings.ReplaceAll(value, "twenty-three", "23")
		value = strings.ReplaceAll(value, "twenty three", "23")
		value = strings.ReplaceAll(value, "sixty", "60")
		value = strings.Join(strings.Fields(value), " ")
		value = strings.ReplaceAll(value, " at ", " ")
		return value
	}
	g, e := normalize(got), normalize(expected)
	return g == e || strings.Contains(g, e)
}

func extractSkillName(arguments, toolName string) string {
	if toolName != "activate_skill" {
		return ""
	}
	var args struct {
		Name string `json:"name"`
	}
	if json.Unmarshal([]byte(arguments), &args) == nil && args.Name != "" {
		return args.Name
	}
	return parseSkillArgument(arguments)
}

func parseSkillArgument(content string) string {
	for _, item := range skillCatalog() {
		if strings.Contains(strings.ToLower(content), strings.ToLower(item.Name)) {
			return item.Name
		}
	}
	return ""
}

func hasErrors(outcomes []outcome) bool {
	for _, item := range outcomes {
		if item.Error != "" {
			return true
		}
	}
	return false
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func logf(category, format string, args ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format(time.RFC3339Nano), category, fmt.Sprintf(format, args...))
}
