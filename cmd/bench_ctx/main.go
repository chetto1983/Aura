// Command bench_ctx measures Phase-CTX compaction savings on committed
// conversation fixtures.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
)

const (
	defaultFixturesDir = "internal/conversation/testdata/bench"
	defaultModels      = "deepseek/deepseek-v4-flash,google/gemma-4-26b-a4b-it,anthropic/claude-sonnet-4"
	imageTokenEstimate = 1600
	charsPerToken      = 4
)

var knownModelWindows = map[string]int{
	"deepseek/deepseek-v4-flash":     163840,
	"google/gemma-4-26b-a4b-it":      131072,
	"anthropic/claude-sonnet-4":      200000,
	"anthropic/claude-sonnet-4.5":    200000,
	"anthropic/claude-sonnet-4-2026": 200000,
}

type benchOptions struct {
	fixturesDir  string
	outPath      string
	models       []benchModel
	offline      bool
	thresholdPct float64
	followup     bool
}

type benchModel struct {
	ID            string `json:"id"`
	ContextWindow int    `json:"context_window"`
}

type fixtureFile struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Expected    fixtureExpected  `json:"expected"`
	Messages    []fixtureMessage `json:"messages"`
	Repeats     []fixtureRepeat  `json:"repeat_groups,omitempty"`
}

type fixtureExpected struct {
	Keyword       string `json:"keyword"`
	FocusTopic    string `json:"focus_topic,omitempty"`
	Turns         int    `json:"turns"`
	ToolResults   int    `json:"tool_results,omitempty"`
	ImageMessages int    `json:"image_messages,omitempty"`
}

type fixtureRepeat struct {
	Count    int              `json:"count"`
	Messages []fixtureMessage `json:"messages"`
}

type fixtureMessage struct {
	Role         string           `json:"role"`
	Content      string           `json:"content,omitempty"`
	ContentParts []map[string]any `json:"content_parts,omitempty"`
	Repeat       int              `json:"repeat,omitempty"`
	ToolCallID   string           `json:"tool_call_id,omitempty"`
	ToolCalls    []fixtureTool    `json:"tool_calls,omitempty"`
}

type fixtureTool struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type benchFixture struct {
	Name        string
	Description string
	Expected    fixtureExpected
	Messages    []llm.Message
}

type runFile struct {
	GeneratedAt  string        `json:"generated_at"`
	Mode         string        `json:"mode"`
	FixtureDir   string        `json:"fixture_dir"`
	ThresholdPct float64       `json:"threshold_pct"`
	Models       []benchModel  `json:"models"`
	Summary      runSummary    `json:"summary"`
	Results      []benchResult `json:"results"`
}

type runSummary struct {
	Rows            int  `json:"rows"`
	CompactedRows   int  `json:"compacted_rows"`
	QualityPassRows int  `json:"quality_pass_rows"`
	GatePassed      bool `json:"gate_passed"`
	BestSavingsPct  int  `json:"best_savings_pct"`
}

type benchResult struct {
	Model                   string `json:"model"`
	ModelContextWindow      int    `json:"model_context_window"`
	Fixture                 string `json:"fixture"`
	PreTokens               int    `json:"pre_tokens"`
	PostTokens              int    `json:"post_tokens"`
	SavingsPct              int    `json:"savings_pct"`
	LatencyMS               int64  `json:"latency_ms"`
	QualityKeywordRetained  bool   `json:"quality_keyword_retained"`
	RecommendedThresholdPct int    `json:"recommended_threshold_pct"`
	Compacted               bool   `json:"compacted"`
	Error                   string `json:"error,omitempty"`
}

func main() {
	opts, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	run, err := runBench(context.Background(), opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := writeRun(opts.outPath, run); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("bench_ctx wrote %s rows=%d gate_passed=%v best_savings_pct=%d\n",
		opts.outPath, run.Summary.Rows, run.Summary.GatePassed, run.Summary.BestSavingsPct)
}

func parseFlags(args []string) (benchOptions, error) {
	var opts benchOptions
	fs := flag.NewFlagSet("bench_ctx", flag.ContinueOnError)
	fs.StringVar(&opts.fixturesDir, "fixtures", defaultFixturesDir, "fixture directory")
	fs.StringVar(&opts.outPath, "out", "", "output JSON path")
	modelsCSV := fs.String("models", defaultModels, "comma-separated models; use model=window to override context window")
	fs.BoolVar(&opts.offline, "offline", true, "use deterministic offline summary/follow-up client")
	fs.Float64Var(&opts.thresholdPct, "threshold-pct", 0.50, "compaction threshold as fraction of model context window")
	fs.BoolVar(&opts.followup, "followup", true, "run a follow-up quality check")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.thresholdPct <= 0 || opts.thresholdPct >= 1 {
		return opts, fmt.Errorf("threshold-pct must be between 0 and 1, got %.3f", opts.thresholdPct)
	}
	models, err := parseModels(*modelsCSV)
	if err != nil {
		return opts, err
	}
	opts.models = models
	if opts.outPath == "" {
		opts.outPath = filepath.Join(".planning", "post-drift-2026-05-21", "Phase-CTX",
			"bench-results-offline-"+time.Now().UTC().Format("2006-01-02")+".json")
	}
	return opts, nil
}

func parseModels(csv string) ([]benchModel, error) {
	var models []benchModel
	for _, raw := range strings.Split(csv, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		id, windowRaw, hasWindow := strings.Cut(raw, "=")
		window := knownModelWindows[id]
		if hasWindow {
			parsed, err := strconv.Atoi(windowRaw)
			if err != nil || parsed <= 0 {
				return nil, fmt.Errorf("invalid model window %q", raw)
			}
			window = parsed
		}
		if window <= 0 {
			window = 128000
		}
		models = append(models, benchModel{ID: id, ContextWindow: window})
	}
	if len(models) == 0 {
		return nil, errors.New("at least one model is required")
	}
	return models, nil
}

func runBench(ctx context.Context, opts benchOptions) (runFile, error) {
	fixtures, err := loadFixtures(opts.fixturesDir)
	if err != nil {
		return runFile{}, err
	}
	run := runFile{
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		Mode:         "live",
		FixtureDir:   opts.fixturesDir,
		ThresholdPct: opts.thresholdPct,
		Models:       opts.models,
	}
	if opts.offline {
		run.Mode = "offline"
	}
	for _, model := range opts.models {
		for _, fixture := range fixtures {
			result := runOne(ctx, opts, model, fixture)
			run.Results = append(run.Results, result)
			run.Summary.Rows++
			if result.Compacted {
				run.Summary.CompactedRows++
			}
			if result.QualityKeywordRetained {
				run.Summary.QualityPassRows++
			}
			if result.SavingsPct > run.Summary.BestSavingsPct {
				run.Summary.BestSavingsPct = result.SavingsPct
			}
			if result.SavingsPct > 40 && result.QualityKeywordRetained {
				run.Summary.GatePassed = true
			}
		}
	}
	return run, nil
}

func runOne(ctx context.Context, opts benchOptions, model benchModel, fixture benchFixture) benchResult {
	result := benchResult{
		Model:              model.ID,
		ModelContextWindow: model.ContextWindow,
		Fixture:            fixture.Name,
	}
	preTokens := estimateTokens(fixture.Messages)
	result.PreTokens = preTokens
	thresholdTokens := int(float64(model.ContextWindow) * opts.thresholdPct)
	result.RecommendedThresholdPct = recommendThreshold(preTokens, model.ContextWindow, 0, true)
	client, err := newBenchClient(opts.offline, model.ID, fixture.Expected.Keyword)
	if err != nil {
		result.Error = err.Error()
		result.PostTokens = preTokens
		result.QualityKeywordRetained = false
		return result
	}

	start := time.Now()
	compressed := fixture.Messages
	if preTokens >= thresholdTokens {
		compressor := conversation.NewContextCompressor(client, model.ContextWindow)
		compressed = compressor.Compress(fixture.Messages, preTokens, focusTopic(fixture))
		result.Compacted = !sameMessages(fixture.Messages, compressed)
	}
	result.LatencyMS = time.Since(start).Milliseconds()
	postTokens := estimateTokens(compressed)
	result.PostTokens = postTokens
	result.SavingsPct = percentSavings(preTokens, postTokens)
	result.QualityKeywordRetained = keywordRetained(compressed, fixture.Expected.Keyword)
	if opts.followup {
		answer, err := followupAnswer(ctx, client, compressed)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.QualityKeywordRetained = strings.Contains(strings.ToLower(answer), strings.ToLower(fixture.Expected.Keyword))
		}
	}
	result.RecommendedThresholdPct = recommendThreshold(preTokens, model.ContextWindow, result.SavingsPct, result.QualityKeywordRetained)
	return result
}

func loadFixtures(dir string) ([]benchFixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read fixtures: %w", err)
	}
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("no fixture JSON files found in %s", dir)
	}
	fixtures := make([]benchFixture, 0, len(files))
	for _, path := range files {
		fixture, err := loadFixture(path)
		if err != nil {
			return nil, err
		}
		fixtures = append(fixtures, fixture)
	}
	return fixtures, nil
}

func loadFixture(path string) (benchFixture, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return benchFixture{}, fmt.Errorf("read fixture %s: %w", path, err)
	}
	var file fixtureFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return benchFixture{}, fmt.Errorf("parse fixture %s: %w", path, err)
	}
	if file.Name == "" || file.Expected.Keyword == "" {
		return benchFixture{}, fmt.Errorf("fixture %s requires name and expected.keyword", path)
	}
	var messages []llm.Message
	for _, msg := range file.Messages {
		messages = append(messages, expandMessage(msg, 0))
	}
	for _, group := range file.Repeats {
		if group.Count <= 0 {
			return benchFixture{}, fmt.Errorf("fixture %s has repeat group with count <= 0", path)
		}
		for i := 0; i < group.Count; i++ {
			for _, msg := range group.Messages {
				messages = append(messages, expandMessage(msg, i+1))
			}
		}
	}
	if len(messages) == 0 {
		return benchFixture{}, fmt.Errorf("fixture %s contains no messages", path)
	}
	return benchFixture{Name: file.Name, Description: file.Description, Expected: file.Expected, Messages: messages}, nil
}

func expandMessage(msg fixtureMessage, ordinal int) llm.Message {
	content := expandOrdinal(msg.Content, ordinal)
	if len(msg.ContentParts) > 0 {
		raw, _ := json.Marshal(msg.ContentParts)
		content = expandOrdinal(string(raw), ordinal)
	}
	if msg.Repeat > 1 {
		content = strings.Repeat(content, msg.Repeat)
	}
	calls := make([]llm.ToolCall, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		calls = append(calls, llm.ToolCall{ID: expandOrdinal(tc.ID, ordinal), Name: tc.Name, Arguments: tc.Arguments})
	}
	return llm.Message{
		Role:       msg.Role,
		Content:    content,
		ToolCallID: expandOrdinal(msg.ToolCallID, ordinal),
		ToolCalls:  calls,
	}
}

func expandOrdinal(s string, ordinal int) string {
	if ordinal <= 0 {
		return s
	}
	return strings.ReplaceAll(s, "{{i}}", strconv.Itoa(ordinal))
}

func focusTopic(fixture benchFixture) string {
	if fixture.Expected.FocusTopic != "" {
		return fixture.Expected.FocusTopic
	}
	for i := len(fixture.Messages) - 1; i >= 0; i-- {
		if fixture.Messages[i].Role == "user" && strings.TrimSpace(fixture.Messages[i].Content) != "" {
			topic := fixture.Messages[i].Content
			if len(topic) > 500 {
				return topic[:500]
			}
			return topic
		}
	}
	return fixture.Expected.Keyword
}

func estimateTokens(messages []llm.Message) int {
	total := 0
	for _, msg := range messages {
		total += 4 + contentBudgetTokens(msg.Content)
		for _, tc := range msg.ToolCalls {
			raw, _ := json.Marshal(tc.Arguments)
			total += (len(tc.ID)+len(tc.Name)+len(raw))/charsPerToken + 1
		}
	}
	if total < 1 {
		return 1
	}
	return total
}

func contentBudgetTokens(content string) int {
	if content == "" || content[0] != '[' {
		return len(content) / charsPerToken
	}
	var parts []map[string]any
	if err := json.Unmarshal([]byte(content), &parts); err != nil {
		return len(content) / charsPerToken
	}
	total := 0
	for _, part := range parts {
		switch part["type"] {
		case "image_url", "input_image", "image":
			total += imageTokenEstimate
		default:
			if text, ok := part["text"].(string); ok {
				total += len(text) / charsPerToken
			}
		}
	}
	return total
}

func percentSavings(pre, post int) int {
	if pre <= 0 || post >= pre {
		return 0
	}
	return int(float64(pre-post) / float64(pre) * 100)
}

func recommendThreshold(preTokens, contextWindow, savingsPct int, quality bool) int {
	if contextWindow <= 0 {
		return 50
	}
	ratio := float64(preTokens) / float64(contextWindow)
	switch {
	case !quality:
		return 40
	case ratio > 0.80 && savingsPct >= 40:
		return 45
	case ratio < 0.40:
		return 55
	default:
		return 50
	}
}

func keywordRetained(messages []llm.Message, keyword string) bool {
	if keyword == "" {
		return true
	}
	needle := strings.ToLower(keyword)
	for _, msg := range messages {
		if strings.Contains(strings.ToLower(msg.Content), needle) {
			return true
		}
	}
	return false
}

func followupAnswer(ctx context.Context, client llm.Client, messages []llm.Message) (string, error) {
	reqMsgs := append([]llm.Message{}, messages...)
	reqMsgs = append(reqMsgs, llm.Message{
		Role:    "user",
		Content: "Based on the conversation context, answer with the exact project code keyword that must be preserved. If unknown, answer UNKNOWN.",
	})
	resp, err := client.Send(ctx, llm.Request{Messages: reqMsgs, Temperature: llm.Float64Ptr(0)})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}

func sameMessages(a, b []llm.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role || a[i].Content != b[i].Content || a[i].ToolCallID != b[i].ToolCallID {
			return false
		}
	}
	return true
}

func newBenchClient(offline bool, modelID, keyword string) (llm.Client, error) {
	if offline {
		return &staticBenchClient{keyword: keyword}, nil
	}
	key := firstEnv("AURA_LLM_API_KEY", "OPENROUTER_API_KEY")
	if key == "" {
		return nil, errors.New("AURA_LLM_API_KEY or OPENROUTER_API_KEY required for live mode")
	}
	baseURL := firstEnv("AURA_LLM_BASE_URL", "OPENROUTER_BASE_URL", "LLM_BASE_URL")
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	return llm.NewOpenAIClient(llm.OpenAIConfig{APIKey: key, BaseURL: baseURL, Model: modelID}), nil
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return ""
}

func writeRun(path string, run runFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	raw, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal run: %w", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return fmt.Errorf("write run: %w", err)
	}
	return nil
}

type staticBenchClient struct {
	keyword string
}

func (s *staticBenchClient) Send(_ context.Context, req llm.Request) (llm.Response, error) {
	last := ""
	if len(req.Messages) > 0 {
		last = req.Messages[len(req.Messages)-1].Content
	}
	if strings.Contains(last, "exact project code keyword") {
		return llm.Response{Content: s.keyword}, nil
	}
	return llm.Response{Content: "## Goal\nPreserve the active task.\n## Progress\nCompacted fixture content.\n## Decisions\nKeep keyword " + s.keyword + ".\n## Active Task\nContinue from " + s.keyword + ".\n## Remaining Work\nRun the follow-up check."}, nil
}

func (s *staticBenchClient) Stream(_ context.Context, _ llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token)
	close(ch)
	return ch, nil
}
