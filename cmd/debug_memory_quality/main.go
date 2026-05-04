// debug_memory_quality is a hermetic scorecard for Aura's daily second-brain
// usefulness. It seeds realistic source/archive evidence, runs everyday
// memory questions through search_memory, creates review-gated proposals for
// scenarios that should grow the wiki, and reports quality metrics.
//
//	go run ./cmd/debug_memory_quality
//	go run ./cmd/debug_memory_quality -json
//	go run ./cmd/debug_memory_quality -keep
//	go run ./cmd/debug_memory_quality -live-llm
//	go run ./cmd/debug_memory_quality -live-llm -report-dir reports/memory-quality
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/tools"
)

type scenario struct {
	Name             string   `json:"name"`
	Question         string   `json:"question"`
	Query            string   `json:"query"`
	Scope            string   `json:"scope,omitempty"`
	ExpectKinds      []string `json:"expect_kinds,omitempty"`
	ShouldPropose    bool     `json:"should_propose,omitempty"`
	ProposalAction   string   `json:"proposal_action,omitempty"`
	ProposalTarget   string   `json:"proposal_target,omitempty"`
	ProposalCategory string   `json:"proposal_category,omitempty"`
}

type scenarioResult struct {
	Name            string   `json:"name"`
	Question        string   `json:"question"`
	Query           string   `json:"query"`
	Pass            bool     `json:"pass"`
	EvidenceCount   int      `json:"evidence_count"`
	EvidenceKinds   []string `json:"evidence_kinds"`
	ProposalID      int64    `json:"proposal_id,omitempty"`
	ProposalOK      bool     `json:"proposal_ok,omitempty"`
	QualityIssues   []string `json:"quality_issues,omitempty"`
	MissingEvidence []string `json:"missing_evidence,omitempty"`
}

type report struct {
	OK                  bool             `json:"ok"`
	Questions           int              `json:"questions"`
	Passed              int              `json:"passed"`
	EvidenceHitRate     float64          `json:"evidence_hit_rate"`
	ProposalScenarios   int              `json:"proposal_scenarios"`
	ProposalsCreated    int              `json:"proposals_created"`
	ProposalQualityRate float64          `json:"proposal_quality_rate"`
	SourceEvidenceHits  int              `json:"source_evidence_hits"`
	ArchiveEvidenceHits int              `json:"archive_evidence_hits"`
	Warnings            []string         `json:"warnings,omitempty"`
	Results             []scenarioResult `json:"results"`
}

type liveReport struct {
	OK                      bool                 `json:"ok"`
	Model                   string               `json:"model"`
	Questions               int                  `json:"questions"`
	Passed                  int                  `json:"passed"`
	RoutingPassRate         float64              `json:"routing_pass_rate"`
	LatencyBudgetMS         int64                `json:"latency_budget_ms"`
	AvgScenarioMS           int64                `json:"avg_scenario_ms"`
	MaxScenarioMS           int64                `json:"max_scenario_ms"`
	SlowScenarios           int                  `json:"slow_scenarios"`
	SearchMemoryCalls       int                  `json:"search_memory_calls"`
	ProposalCalls           int                  `json:"proposal_calls"`
	UnexpectedProposalCalls int                  `json:"unexpected_proposal_calls"`
	LLMCalls                int                  `json:"llm_calls"`
	ToolCalls               int                  `json:"tool_calls"`
	ElapsedMS               int64                `json:"elapsed_ms"`
	Warnings                []string             `json:"warnings,omitempty"`
	Results                 []liveScenarioResult `json:"results"`
}

type liveScenarioResult struct {
	Name               string   `json:"name"`
	Question           string   `json:"question"`
	Pass               bool     `json:"pass"`
	ToolCalls          []string `json:"tool_calls"`
	LLMCalls           int      `json:"llm_calls"`
	ElapsedMS          int64    `json:"elapsed_ms"`
	LatencyBudgetMS    int64    `json:"latency_budget_ms,omitempty"`
	EvidenceCount      int      `json:"evidence_count"`
	EvidenceKinds      []string `json:"evidence_kinds"`
	ProposalOK         bool     `json:"proposal_ok,omitempty"`
	UnexpectedProposal bool     `json:"unexpected_proposal,omitempty"`
	Issues             []string `json:"issues,omitempty"`
	Final              string   `json:"final,omitempty"`
}

type savedReport struct {
	GeneratedAt string         `json:"generated_at"`
	Mode        string         `json:"mode"`
	OK          bool           `json:"ok"`
	Summary     map[string]any `json:"summary"`
	Hermetic    *report        `json:"hermetic,omitempty"`
	Live        *liveReport    `json:"live,omitempty"`
	Graph       qualityGraph   `json:"graph"`
}

type qualityGraph struct {
	Nodes []graphNode `json:"nodes"`
	Edges []graphEdge `json:"edges"`
}

type graphNode struct {
	ID      string         `json:"id"`
	Label   string         `json:"label"`
	Kind    string         `json:"kind"`
	Status  string         `json:"status,omitempty"`
	Metrics map[string]any `json:"metrics,omitempty"`
}

type graphEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"`
	Weight int    `json:"weight,omitempty"`
}

type evidenceEnvelope struct {
	Query    string         `json:"query"`
	Items    []evidenceItem `json:"items"`
	Warnings []string       `json:"warnings,omitempty"`
}

type evidenceItem struct {
	Kind    string  `json:"kind"`
	ID      string  `json:"id"`
	Title   string  `json:"title,omitempty"`
	Role    string  `json:"role,omitempty"`
	Page    int     `json:"page,omitempty"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`
}

func main() {
	jsonOut := flag.Bool("json", false, "print machine-readable JSON only")
	keep := flag.Bool("keep", false, "keep the temporary wiki directory")
	liveLLM := flag.Bool("live-llm", false, "drive the scorecard through the live LLM/tool loop")
	limit := flag.Int("limit", 0, "optional number of scenarios to run")
	liveTimeout := flag.Duration("live-timeout", 60*time.Second, "hard per-scenario timeout for -live-llm")
	liveLatencyBudget := flag.Duration("live-latency-budget", 30*time.Second, "end-user latency budget per live scenario; <=0 disables the slow-response gate")
	reportDir := flag.String("report-dir", "", "optional directory for timestamped JSON reports with graph data")
	flag.Parse()

	runTimeout := 30 * time.Second
	if *liveLLM {
		count := len(selectedScenarios(*limit))
		runTimeout = time.Duration(count) * (*liveTimeout + 30*time.Second)
		if runTimeout < 5*time.Minute {
			runTimeout = 5 * time.Minute
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	if *liveLLM {
		rep, wikiDir, err := runLive(ctx, *limit, *liveTimeout, *liveLatencyBudget)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
			os.Exit(1)
		}
		if !*keep {
			defer os.RemoveAll(wikiDir)
		}
		if *jsonOut {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(rep)
		} else {
			printLiveReport(rep, wikiDir)
		}
		if *reportDir != "" {
			path, err := saveLiveQualityReport(*reportDir, rep)
			if err != nil {
				fmt.Fprintf(os.Stderr, "FAIL: save report: %v\n", err)
				os.Exit(1)
			}
			if !*jsonOut {
				fmt.Printf("report=%s\n", path)
			}
		}
		if !rep.OK {
			os.Exit(1)
		}
		return
	}

	rep, wikiDir, err := run(ctx, *limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	if !*keep {
		defer os.RemoveAll(wikiDir)
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
	} else {
		printReport(rep, wikiDir)
	}
	if *reportDir != "" {
		path, err := saveHermeticQualityReport(*reportDir, rep)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: save report: %v\n", err)
			os.Exit(1)
		}
		if !*jsonOut {
			fmt.Printf("report=%s\n", path)
		}
	}
	if !rep.OK {
		os.Exit(1)
	}
}

func run(ctx context.Context, limit int) (report, string, error) {
	wikiDir, err := os.MkdirTemp("", "aura-debug-memory-quality-*")
	if err != nil {
		return report{}, "", fmt.Errorf("create temp wiki: %w", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	srcStore, err := source.NewStore(wikiDir, logger)
	if err != nil {
		return report{}, wikiDir, fmt.Errorf("source store: %w", err)
	}
	sched, err := scheduler.OpenStore(filepath.Join(wikiDir, "aura-debug.db"))
	if err != nil {
		return report{}, wikiDir, fmt.Errorf("scheduler store: %w", err)
	}
	defer sched.Close()

	archive, err := conversation.NewArchiveStore(sched.DB())
	if err != nil {
		return report{}, wikiDir, fmt.Errorf("archive store: %w", err)
	}
	summaries := summarizer.NewSummariesStore(sched.DB())
	if err := seedMemory(ctx, srcStore, archive); err != nil {
		return report{}, wikiDir, err
	}

	searchTool := tools.NewSearchMemoryTool(nil, srcStore, archive)
	proposalTool := tools.NewProposeWikiChangeTool(summaries)
	if searchTool == nil || proposalTool == nil {
		return report{}, wikiDir, fmt.Errorf("memory tools unavailable")
	}

	rep := report{Results: make([]scenarioResult, 0, len(scenarios()))}
	for _, sc := range selectedScenarios(limit) {
		result := runScenario(ctx, sc, searchTool, proposalTool)
		rep.Results = append(rep.Results, result)
		rep.Questions++
		if result.EvidenceCount > 0 {
			rep.Passed++
		}
		if hasKind(result.EvidenceKinds, "source") {
			rep.SourceEvidenceHits++
		}
		if hasKind(result.EvidenceKinds, "archive") {
			rep.ArchiveEvidenceHits++
		}
		if sc.ShouldPropose {
			rep.ProposalScenarios++
			if result.ProposalID > 0 {
				rep.ProposalsCreated++
			}
			if result.ProposalOK {
				rep.ProposalQualityRate++
			}
		}
	}
	if rep.Questions > 0 {
		rep.EvidenceHitRate = float64(rep.Passed) / float64(rep.Questions)
	}
	if rep.ProposalScenarios > 0 {
		rep.ProposalQualityRate = rep.ProposalQualityRate / float64(rep.ProposalScenarios)
	}
	proposalGate := rep.ProposalScenarios == 0 || rep.ProposalQualityRate >= 0.90
	rep.OK = rep.EvidenceHitRate >= 0.90 && proposalGate && rep.SourceEvidenceHits > 0 && rep.ArchiveEvidenceHits > 0
	if !rep.OK {
		rep.Warnings = append(rep.Warnings, "memory quality gate failed: want >=90% evidence hit rate and >=90% proposal quality")
	}
	return rep, wikiDir, nil
}

func runLive(ctx context.Context, limit int, liveTimeout, liveLatencyBudget time.Duration) (liveReport, string, error) {
	if err := loadDotEnv(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return liveReport{}, "", fmt.Errorf("load .env: %w", err)
	}
	apiKey := strings.TrimSpace(os.Getenv("LLM_API_KEY"))
	if apiKey == "" {
		return liveReport{}, "", fmt.Errorf("LLM_API_KEY is required for -live-llm")
	}
	baseURL := envDefault("LLM_BASE_URL", "https://api.openai.com/v1")
	model := envDefault("LLM_MODEL", "gpt-4")

	wikiDir, err := os.MkdirTemp("", "aura-debug-memory-routing-*")
	if err != nil {
		return liveReport{}, "", fmt.Errorf("create temp wiki: %w", err)
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	srcStore, err := source.NewStore(wikiDir, logger)
	if err != nil {
		return liveReport{}, wikiDir, fmt.Errorf("source store: %w", err)
	}
	sched, err := scheduler.OpenStore(filepath.Join(wikiDir, "aura-debug.db"))
	if err != nil {
		return liveReport{}, wikiDir, fmt.Errorf("scheduler store: %w", err)
	}
	defer sched.Close()
	archive, err := conversation.NewArchiveStore(sched.DB())
	if err != nil {
		return liveReport{}, wikiDir, fmt.Errorf("archive store: %w", err)
	}
	summaries := summarizer.NewSummariesStore(sched.DB())
	if err := seedMemory(ctx, srcStore, archive); err != nil {
		return liveReport{}, wikiDir, err
	}

	reg := tools.NewRegistry(logger)
	if tool := tools.NewSearchMemoryTool(nil, srcStore, archive); tool != nil {
		reg.Register(tool)
	}
	if tool := tools.NewProposeWikiChangeTool(summaries); tool != nil {
		reg.Register(tool)
	}

	runner, err := agent.NewRunner(agent.Config{
		LLM:           llm.NewOpenAIClient(llm.OpenAIConfig{APIKey: apiKey, BaseURL: baseURL, Model: model}),
		Tools:         reg,
		Model:         model,
		MaxIterations: 4,
		Timeout:       liveTimeout,
		ToolTimeout:   20 * time.Second,
		Logger:        logger,
	})
	if err != nil {
		return liveReport{}, wikiDir, err
	}

	rep := liveReport{
		Model:           model,
		LatencyBudgetMS: liveLatencyBudget.Milliseconds(),
		Results:         make([]liveScenarioResult, 0, len(selectedScenarios(limit))),
	}
	started := time.Now()
	for _, sc := range selectedScenarios(limit) {
		result := runLiveScenario(ctx, runner, sc, liveLatencyBudget)
		rep.Results = append(rep.Results, result)
		rep.Questions++
		if result.Pass {
			rep.Passed++
		}
		rep.AvgScenarioMS += result.ElapsedMS
		if result.ElapsedMS > rep.MaxScenarioMS {
			rep.MaxScenarioMS = result.ElapsedMS
		}
		if liveLatencyBudget > 0 && result.ElapsedMS > liveLatencyBudget.Milliseconds() {
			rep.SlowScenarios++
		}
		rep.LLMCalls += result.LLMCalls
		rep.ToolCalls += len(result.ToolCalls)
		for _, name := range result.ToolCalls {
			switch name {
			case "search_memory":
				rep.SearchMemoryCalls++
			case "propose_wiki_change":
				rep.ProposalCalls++
				if !sc.ShouldPropose {
					rep.UnexpectedProposalCalls++
				}
			}
		}
	}
	rep.ElapsedMS = time.Since(started).Milliseconds()
	if rep.Questions > 0 {
		rep.RoutingPassRate = float64(rep.Passed) / float64(rep.Questions)
		rep.AvgScenarioMS = rep.AvgScenarioMS / int64(rep.Questions)
	}
	rep.OK = rep.RoutingPassRate >= 0.85 &&
		rep.SearchMemoryCalls >= rep.Questions &&
		rep.UnexpectedProposalCalls == 0 &&
		rep.SlowScenarios == 0
	if !rep.OK {
		rep.Warnings = append(rep.Warnings, "live usefulness gate failed: want >=85% pass rate, search_memory on every question, no unexpected proposals, and no slow scenarios over the end-user latency budget")
	}
	return rep, wikiDir, nil
}

func runScenario(ctx context.Context, sc scenario, searchTool tools.Tool, proposalTool tools.Tool) scenarioResult {
	res := scenarioResult{Name: sc.Name, Question: sc.Question, Query: sc.Query}
	args := map[string]any{"query": sc.Query, "limit": 6}
	if sc.Scope != "" {
		args["scope"] = sc.Scope
	}
	out, err := searchTool.Execute(ctx, args)
	if err != nil {
		res.QualityIssues = append(res.QualityIssues, err.Error())
		return res
	}
	env, err := parseEnvelope(out)
	if err != nil {
		res.QualityIssues = append(res.QualityIssues, err.Error())
		return res
	}
	res.EvidenceCount = len(env.Items)
	res.EvidenceKinds = evidenceKinds(env.Items)
	for _, kind := range sc.ExpectKinds {
		if !hasKind(res.EvidenceKinds, kind) {
			res.MissingEvidence = append(res.MissingEvidence, kind)
		}
	}
	res.Pass = len(res.MissingEvidence) == 0 && res.EvidenceCount > 0
	if sc.ShouldPropose {
		res.ProposalID, res.ProposalOK, res.QualityIssues = createAndScoreProposal(ctx, sc, env.Items, proposalTool)
		res.Pass = res.Pass && res.ProposalOK
	}
	return res
}

func runLiveScenario(ctx context.Context, runner *agent.Runner, sc scenario, liveLatencyBudget time.Duration) liveScenarioResult {
	res := liveScenarioResult{Name: sc.Name, Question: sc.Question}
	started := time.Now()
	result, err := runner.Run(ctx, agent.Task{
		SystemPrompt:       liveSystemPrompt(),
		Prompt:             liveScenarioPrompt(sc),
		ToolAllowlist:      []string{"search_memory", "propose_wiki_change"},
		UserID:             "9001",
		Temperature:        llm.Float64Ptr(0),
		MaxToolCalls:       3,
		MaxToolResultChars: 12000,
		CompleteOnDeadline: true,
	})
	res.ElapsedMS = time.Since(started).Milliseconds()
	if liveLatencyBudget > 0 {
		res.LatencyBudgetMS = liveLatencyBudget.Milliseconds()
		if res.ElapsedMS > res.LatencyBudgetMS {
			res.Issues = append(res.Issues, fmt.Sprintf("slow response: %dms over %dms end-user budget", res.ElapsedMS, res.LatencyBudgetMS))
		}
	}
	res.LLMCalls = result.LLMCalls
	res.Final = singleLine(result.Content, 220)
	if err != nil {
		res.Issues = append(res.Issues, err.Error())
	}
	if strings.Contains(result.Content, "interrupted before a final answer") {
		res.Issues = append(res.Issues, "runner returned a deadline partial instead of a final answer")
	}
	envelopes, proposalOK := inspectLiveMessages(result.Messages)
	res.EvidenceKinds = evidenceKinds(envelopes)
	res.EvidenceCount = len(envelopes)
	res.ProposalOK = proposalOK
	res.ToolCalls = toolCallNames(result.Messages)

	if !hasString(res.ToolCalls, "search_memory") {
		res.Issues = append(res.Issues, "missing search_memory call")
	}
	for _, kind := range sc.ExpectKinds {
		if !hasKind(res.EvidenceKinds, kind) {
			res.Issues = append(res.Issues, "missing evidence kind "+kind)
		}
	}
	if sc.ShouldPropose {
		if !hasString(res.ToolCalls, "propose_wiki_change") {
			res.Issues = append(res.Issues, "missing propose_wiki_change call")
		}
		if !res.ProposalOK {
			res.Issues = append(res.Issues, "proposal was not created cleanly")
		}
	} else if hasString(res.ToolCalls, "propose_wiki_change") {
		res.UnexpectedProposal = true
		res.Issues = append(res.Issues, "unexpected proposal for answer-only question")
	}
	res.Pass = len(res.Issues) == 0 && res.EvidenceCount > 0
	return res
}

func createAndScoreProposal(ctx context.Context, sc scenario, evidence []evidenceItem, proposalTool tools.Tool) (int64, bool, []string) {
	issues := []string{}
	if len(evidence) == 0 {
		issues = append(issues, "proposal has no evidence")
	}
	action := sc.ProposalAction
	if action == "" {
		action = "patch"
	}
	args := map[string]any{
		"action":        action,
		"fact":          proposalFact(sc),
		"category":      sc.ProposalCategory,
		"origin_tool":   "search_memory",
		"origin_reason": "debug_memory_quality scenario: " + sc.Name,
		"confidence":    0.82,
		"evidence":      evidenceArgs(evidence),
	}
	if action == "patch" {
		args["target_slug"] = sc.ProposalTarget
	}
	out, err := proposalTool.Execute(tools.WithUserID(ctx, "9001"), args)
	if err != nil {
		issues = append(issues, err.Error())
		return 0, false, issues
	}
	var resp struct {
		OK       bool  `json:"ok"`
		ID       int64 `json:"id"`
		Evidence int   `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		issues = append(issues, "proposal response JSON: "+err.Error())
		return 0, false, issues
	}
	if !resp.OK || resp.ID == 0 {
		issues = append(issues, "proposal response not ok")
	}
	if resp.Evidence == 0 {
		issues = append(issues, "proposal lost evidence refs")
	}
	if strings.TrimSpace(sc.ProposalCategory) == "" {
		issues = append(issues, "proposal category is empty")
	}
	if action == "patch" && strings.TrimSpace(sc.ProposalTarget) == "" {
		issues = append(issues, "patch proposal target is empty")
	}
	return resp.ID, len(issues) == 0, issues
}

func seedMemory(ctx context.Context, sources *source.Store, archive *conversation.ArchiveStore) error {
	if _, err := seedSource(ctx, sources, source.KindText, "weekly-admin-note.txt", "text/plain", []byte(`
Weekly admin note:
- commercialista deadline: send invoice documents by Friday morning.
- dentist appointment is Tuesday at 18:00.
- passport renewal should be checked before the Lisbon trip.
- budget review belongs in the weekly planning ritual.
`)); err != nil {
		return err
	}
	pdf, err := seedSource(ctx, sources, source.KindPDF, "rental-contract.pdf", "application/pdf", []byte("pdf fixture"))
	if err != nil {
		return err
	}
	ocr := `# OCR: rental-contract.pdf

## Page 1

Lease starts in May. Deposit is already paid.

## Page 2

Cancellation clause: tenant must notify 60 days before leaving. Keep this clause for future housing decisions.
`
	if err := os.WriteFile(sources.Path(pdf.ID, "ocr.md"), []byte(ocr), 0o644); err != nil {
		return fmt.Errorf("seed ocr: %w", err)
	}
	if _, err := sources.Update(pdf.ID, func(s *source.Source) error {
		s.Status = source.StatusOCRComplete
		s.PageCount = 2
		return nil
	}); err != nil {
		return fmt.Errorf("seed update pdf: %w", err)
	}
	return seedArchive(ctx, archive)
}

func seedSource(ctx context.Context, store *source.Store, kind source.Kind, filename, mime string, body []byte) (*source.Source, error) {
	src, _, err := store.Put(ctx, source.PutInput{Kind: kind, Filename: filename, MimeType: mime, Bytes: body})
	if err != nil {
		return nil, fmt.Errorf("seed source %s: %w", filename, err)
	}
	return src, nil
}

func seedArchive(ctx context.Context, archive *conversation.ArchiveStore) error {
	turns := []conversation.Turn{
		{ChatID: 7001, UserID: 9001, TurnIndex: 1, Role: "user", Content: "I prefer deep work in the morning and admin tasks after lunch."},
		{ChatID: 7001, UserID: 9001, TurnIndex: 2, Role: "assistant", Content: "Noted: morning deep work; admin after lunch."},
		{ChatID: 7001, UserID: 9001, TurnIndex: 3, Role: "user", Content: "Aura milestone: proposal review must stay review-gated with evidence before wiki writes."},
		{ChatID: 7001, UserID: 9001, TurnIndex: 4, Role: "user", Content: "If I ask for a second brain audit, prioritize stale pages, missing evidence, and workflow proposals."},
		{ChatID: 7001, UserID: 9001, TurnIndex: 5, Role: "user", Content: "Remember that Q2 KPI report review belongs in Friday planning."},
		{ChatID: 7001, UserID: 9001, TurnIndex: 6, Role: "user", Content: "After important web research, Aura should propose durable memory only when source-backed and include provenance."},
		{ChatID: 7001, UserID: 9001, TurnIndex: 7, Role: "assistant", Content: "Daily briefings should focus on deadlines, stale memory, pending proposals, source inbox, and calendar-like tasks."},
	}
	for _, turn := range turns {
		if err := archive.Append(ctx, turn); err != nil {
			return fmt.Errorf("seed archive: %w", err)
		}
	}
	return nil
}

func scenarios() []scenario {
	return []scenario{
		{Name: "deadlines_week", Question: "Che scadenze ho questa settimana?", Query: "commercialista deadline Friday dentist Tuesday passport renewal", Scope: "all", ExpectKinds: []string{"source"}},
		{Name: "rental_clause", Question: "Cosa devo ricordare del contratto di affitto?", Query: "rental cancellation clause 60 days tenant", Scope: "sources", ExpectKinds: []string{"source"}},
		{Name: "personal_preferences", Question: "Quali preferenze personali hai su come lavoro meglio?", Query: "morning deep work admin after lunch", Scope: "archive", ExpectKinds: []string{"archive"}},
		{Name: "friday_planning", Question: "Cosa devo mettere nel planning di venerdi?", Query: "Q2 KPI report Friday planning commercialista", Scope: "all", ExpectKinds: []string{"source", "archive"}},
		{Name: "passport_trip", Question: "Prima del viaggio cosa devo controllare?", Query: "passport renewal Lisbon trip", Scope: "sources", ExpectKinds: []string{"source"}},
		{Name: "housing_decision", Question: "Se cambio casa, quale vincolo devo ricordare?", Query: "housing decisions cancellation clause 60 days", Scope: "sources", ExpectKinds: []string{"source"}},
		{Name: "second_brain_audit", Question: "Fai audit del mio second brain: cosa va controllato?", Query: "second brain audit stale pages missing evidence workflow proposals", Scope: "archive", ExpectKinds: []string{"archive"}, ShouldPropose: true, ProposalAction: "patch", ProposalTarget: "aura-memory", ProposalCategory: "project"},
		{Name: "review_policy", Question: "Come deve comportarsi Aura prima di scrivere in wiki?", Query: "proposal review review-gated evidence before wiki writes", Scope: "archive", ExpectKinds: []string{"archive"}, ShouldPropose: true, ProposalAction: "patch", ProposalTarget: "aura-memory", ProposalCategory: "project"},
		{Name: "weekly_ritual", Question: "Che rituale ricorrente dovrei avere questa settimana?", Query: "budget review weekly planning ritual", Scope: "sources", ExpectKinds: []string{"source"}},
		{Name: "admin_timing", Question: "Quando e' meglio fare task amministrativi?", Query: "admin tasks after lunch", Scope: "archive", ExpectKinds: []string{"archive"}},
		{Name: "invoice_docs", Question: "Cosa devo mandare al commercialista?", Query: "send invoice documents commercialista Friday morning", Scope: "sources", ExpectKinds: []string{"source"}},
		{Name: "remembered_milestone", Question: "Qual e' il milestone Aura sulla memoria?", Query: "Aura milestone proposal review evidence wiki writes", Scope: "archive", ExpectKinds: []string{"archive"}},
		{Name: "tuesday_appointment", Question: "Che appuntamento ho martedi?", Query: "dentist appointment Tuesday 18:00", Scope: "sources", ExpectKinds: []string{"source"}},
		{Name: "budget_memory", Question: "Dove abbiamo parlato del budget?", Query: "budget review weekly planning ritual", Scope: "all", ExpectKinds: []string{"source"}},
		{Name: "research_policy", Question: "Se cerco online una cosa, quando va salvata?", Query: "important web research propose durable memory source-backed provenance", Scope: "archive", ExpectKinds: []string{"archive"}, ShouldPropose: true, ProposalAction: "patch", ProposalTarget: "aura-memory", ProposalCategory: "workflow"},
		{Name: "daily_briefing_focus", Question: "Cosa deve includere il briefing giornaliero?", Query: "daily briefings deadlines stale memory pending proposals source inbox calendar tasks", Scope: "archive", ExpectKinds: []string{"archive"}},
		{Name: "missing_evidence_rule", Question: "Cosa controllo quando faccio audit?", Query: "stale pages missing evidence workflow proposals", Scope: "archive", ExpectKinds: []string{"archive"}},
		{Name: "contract_document", Question: "Quale documento contiene la clausola di uscita?", Query: "rental-contract.pdf cancellation clause tenant", Scope: "sources", ExpectKinds: []string{"source"}},
		{Name: "after_lunch_admin", Question: "Che lavoro metto dopo pranzo?", Query: "admin tasks after lunch", Scope: "all", ExpectKinds: []string{"archive"}},
		{Name: "provenance_requirement", Question: "Che prove deve portare una proposta wiki?", Query: "proposal durable memory source-backed include provenance", Scope: "archive", ExpectKinds: []string{"archive"}, ShouldPropose: true, ProposalAction: "patch", ProposalTarget: "aura-memory", ProposalCategory: "workflow"},
	}
}

func selectedScenarios(limit int) []scenario {
	all := scenarios()
	if limit <= 0 || limit >= len(all) {
		return all
	}
	return all[:limit]
}

func liveSystemPrompt() string {
	return `You are Aura's live memory routing evaluator.
For every user question:
- Call search_memory first with the most useful query.
- Answer only from the returned evidence.
- If the question asks about durable Aura/project/workflow memory policy, call propose_wiki_change after search_memory so the wiki can grow through review.
- Do not call propose_wiki_change for ordinary answer-only questions.
- When calling propose_wiki_change from search_memory, set origin_tool="search_memory" and pass evidence refs copied from the Evidence envelope. Include kind, id, title/page/snippet when available.
- Do not repeat search_memory unless the first call returned no matching evidence.
- If a tool returns {"ok":false,...} and retryable=true, fix the arguments from the hint and retry that tool once.
- Keep the final answer concise and mention evidence IDs naturally.`
}

func liveScenarioPrompt(sc scenario) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "User question: %s\n", sc.Question)
	fmt.Fprintf(&sb, "Useful search query: %s\n", sc.Query)
	if sc.Scope != "" {
		fmt.Fprintf(&sb, "Preferred memory scope: %s\n", sc.Scope)
	}
	if sc.ShouldPropose {
		fmt.Fprintf(&sb, "This should create a reviewed wiki proposal. action=%s target_slug=%s category=%s\n",
			emptyDefault(sc.ProposalAction, "patch"), sc.ProposalTarget, sc.ProposalCategory)
	} else {
		sb.WriteString("This is answer-only. Do not create a wiki proposal.\n")
	}
	return sb.String()
}

func proposalFact(sc scenario) string {
	return fmt.Sprintf("For [[%s]], preserve this durable memory insight from the daily question %q: %s.", sc.ProposalTarget, sc.Question, sc.Query)
}

func parseEnvelope(out string) (evidenceEnvelope, error) {
	const marker = "Evidence envelope:\n"
	idx := strings.LastIndex(out, marker)
	if idx < 0 {
		return evidenceEnvelope{}, fmt.Errorf("missing evidence envelope")
	}
	var env evidenceEnvelope
	if err := json.Unmarshal([]byte(strings.TrimSpace(out[idx+len(marker):])), &env); err != nil {
		return evidenceEnvelope{}, fmt.Errorf("parse evidence envelope: %w", err)
	}
	return env, nil
}

func evidenceKinds(items []evidenceItem) []string {
	seen := map[string]struct{}{}
	for _, item := range items {
		if item.Kind != "" {
			seen[item.Kind] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for kind := range seen {
		out = append(out, kind)
	}
	sort.Strings(out)
	return out
}

func evidenceArgs(items []evidenceItem) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"kind":    item.Kind,
			"id":      item.ID,
			"title":   item.Title,
			"page":    item.Page,
			"snippet": item.Snippet,
		})
	}
	return out
}

func hasKind(kinds []string, want string) bool {
	for _, kind := range kinds {
		if kind == want {
			return true
		}
	}
	return false
}

func inspectLiveMessages(messages []llm.Message) ([]evidenceItem, bool) {
	var evidence []evidenceItem
	proposalOK := false
	for _, msg := range messages {
		if msg.Role != "tool" {
			continue
		}
		if env, err := parseEnvelope(msg.Content); err == nil {
			evidence = append(evidence, env.Items...)
		}
		var proposal struct {
			OK       bool  `json:"ok"`
			ID       int64 `json:"id"`
			Evidence int   `json:"evidence"`
		}
		if err := json.Unmarshal([]byte(strings.TrimSpace(msg.Content)), &proposal); err == nil {
			if proposal.OK && proposal.ID > 0 && proposal.Evidence > 0 {
				proposalOK = true
			}
		}
	}
	return evidence, proposalOK
}

func toolCallNames(messages []llm.Message) []string {
	var names []string
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, call := range msg.ToolCalls {
			names = append(names, call.Name)
		}
	}
	return names
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func saveHermeticQualityReport(dir string, rep report) (string, error) {
	artifact := savedReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Mode:        "hermetic",
		OK:          rep.OK,
		Summary: map[string]any{
			"questions":             rep.Questions,
			"passed":                rep.Passed,
			"evidence_hit_rate":     rep.EvidenceHitRate,
			"proposal_scenarios":    rep.ProposalScenarios,
			"proposals_created":     rep.ProposalsCreated,
			"proposal_quality_rate": rep.ProposalQualityRate,
			"source_evidence_hits":  rep.SourceEvidenceHits,
			"archive_evidence_hits": rep.ArchiveEvidenceHits,
		},
		Hermetic: &rep,
		Graph:    hermeticQualityGraph(rep),
	}
	return writeQualityArtifact(dir, "hermetic", artifact)
}

func saveLiveQualityReport(dir string, rep liveReport) (string, error) {
	artifact := savedReport{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Mode:        "live-llm",
		OK:          rep.OK,
		Summary: map[string]any{
			"model":                     rep.Model,
			"questions":                 rep.Questions,
			"passed":                    rep.Passed,
			"routing_pass_rate":         rep.RoutingPassRate,
			"latency_budget_ms":         rep.LatencyBudgetMS,
			"avg_scenario_ms":           rep.AvgScenarioMS,
			"max_scenario_ms":           rep.MaxScenarioMS,
			"slow_scenarios":            rep.SlowScenarios,
			"search_memory_calls":       rep.SearchMemoryCalls,
			"proposal_calls":            rep.ProposalCalls,
			"unexpected_proposal_calls": rep.UnexpectedProposalCalls,
			"llm_calls":                 rep.LLMCalls,
			"tool_calls":                rep.ToolCalls,
			"elapsed_ms":                rep.ElapsedMS,
		},
		Live:  &rep,
		Graph: liveQualityGraph(rep),
	}
	return writeQualityArtifact(dir, "live-llm", artifact)
}

func writeQualityArtifact(dir, mode string, artifact savedReport) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("report dir is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create report dir: %w", err)
	}
	name := fmt.Sprintf("%s-%s.json", time.Now().UTC().Format("20060102-150405"), mode)
	path := filepath.Join(dir, name)
	body, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal report: %w", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	return path, nil
}

func hermeticQualityGraph(rep report) qualityGraph {
	g := newGraphBuilder()
	g.addNode(graphNode{
		ID:     "scorecard:hermetic",
		Label:  "Hermetic memory scorecard",
		Kind:   "scorecard",
		Status: passStatus(rep.OK),
		Metrics: map[string]any{
			"questions":             rep.Questions,
			"evidence_hit_rate":     rep.EvidenceHitRate,
			"proposal_quality_rate": rep.ProposalQualityRate,
		},
	})
	g.addNode(graphNode{ID: "tool:search_memory", Label: "search_memory", Kind: "tool"})
	g.addNode(graphNode{ID: "tool:propose_wiki_change", Label: "propose_wiki_change", Kind: "tool"})
	for _, r := range rep.Results {
		scenarioID := "scenario:" + r.Name
		g.addNode(graphNode{
			ID:     scenarioID,
			Label:  r.Name,
			Kind:   "scenario",
			Status: passStatus(r.Pass),
			Metrics: map[string]any{
				"evidence_count": r.EvidenceCount,
			},
		})
		g.addEdge("scorecard:hermetic", scenarioID, "contains", 0)
		g.addEdge(scenarioID, "tool:search_memory", "uses", 1)
		for _, kind := range r.EvidenceKinds {
			evidenceID := "evidence:" + kind
			g.addNode(graphNode{ID: evidenceID, Label: kind, Kind: "evidence_kind"})
			g.addEdge(scenarioID, evidenceID, "found_evidence", 1)
		}
		if r.ProposalID > 0 {
			proposalID := fmt.Sprintf("proposal:%s:%d", r.Name, r.ProposalID)
			g.addNode(graphNode{ID: proposalID, Label: fmt.Sprintf("%s proposal", r.Name), Kind: "proposal", Status: passStatus(r.ProposalOK)})
			g.addEdge(scenarioID, "tool:propose_wiki_change", "uses", 1)
			g.addEdge(scenarioID, proposalID, "proposes", 1)
		}
	}
	return g.graph()
}

func liveQualityGraph(rep liveReport) qualityGraph {
	g := newGraphBuilder()
	g.addNode(graphNode{
		ID:     "scorecard:live-llm",
		Label:  "Live LLM memory scorecard",
		Kind:   "scorecard",
		Status: passStatus(rep.OK),
		Metrics: map[string]any{
			"model":             rep.Model,
			"questions":         rep.Questions,
			"routing_pass_rate": rep.RoutingPassRate,
			"latency_budget_ms": rep.LatencyBudgetMS,
			"avg_scenario_ms":   rep.AvgScenarioMS,
			"max_scenario_ms":   rep.MaxScenarioMS,
			"slow_scenarios":    rep.SlowScenarios,
			"elapsed_ms":        rep.ElapsedMS,
			"llm_calls":         rep.LLMCalls,
			"tool_calls":        rep.ToolCalls,
		},
	})
	for _, r := range rep.Results {
		scenarioID := "scenario:" + r.Name
		g.addNode(graphNode{
			ID:     scenarioID,
			Label:  r.Name,
			Kind:   "scenario",
			Status: passStatus(r.Pass),
			Metrics: map[string]any{
				"evidence_count":    r.EvidenceCount,
				"elapsed_ms":        r.ElapsedMS,
				"latency_budget_ms": r.LatencyBudgetMS,
				"llm_calls":         r.LLMCalls,
			},
		})
		g.addEdge("scorecard:live-llm", scenarioID, "contains", 0)
		for _, tool := range r.ToolCalls {
			toolID := "tool:" + tool
			g.addNode(graphNode{ID: toolID, Label: tool, Kind: "tool"})
			g.addEdge(scenarioID, toolID, "uses", 1)
		}
		for _, kind := range r.EvidenceKinds {
			evidenceID := "evidence:" + kind
			g.addNode(graphNode{ID: evidenceID, Label: kind, Kind: "evidence_kind"})
			g.addEdge(scenarioID, evidenceID, "found_evidence", 1)
		}
		if r.ProposalOK || hasString(r.ToolCalls, "propose_wiki_change") {
			proposalID := "proposal:" + r.Name
			g.addNode(graphNode{ID: proposalID, Label: r.Name + " proposal", Kind: "proposal", Status: passStatus(r.ProposalOK)})
			g.addEdge(scenarioID, proposalID, "proposes", 1)
		}
	}
	return g.graph()
}

type graphBuilder struct {
	nodes map[string]graphNode
	edges map[string]graphEdge
	order []string
}

func newGraphBuilder() *graphBuilder {
	return &graphBuilder{
		nodes: make(map[string]graphNode),
		edges: make(map[string]graphEdge),
	}
}

func (g *graphBuilder) addNode(node graphNode) {
	if strings.TrimSpace(node.ID) == "" {
		return
	}
	if _, exists := g.nodes[node.ID]; !exists {
		g.order = append(g.order, node.ID)
	}
	g.nodes[node.ID] = node
}

func (g *graphBuilder) addEdge(from, to, kind string, weight int) {
	if from == "" || to == "" || kind == "" {
		return
	}
	key := from + "\x00" + to + "\x00" + kind
	edge := g.edges[key]
	if edge.From == "" {
		edge = graphEdge{From: from, To: to, Kind: kind}
	}
	if weight > 0 {
		edge.Weight += weight
	}
	g.edges[key] = edge
}

func (g *graphBuilder) graph() qualityGraph {
	nodes := make([]graphNode, 0, len(g.nodes))
	for _, id := range g.order {
		nodes = append(nodes, g.nodes[id])
	}
	edges := make([]graphEdge, 0, len(g.edges))
	for _, edge := range g.edges {
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From == edges[j].From {
			if edges[i].To == edges[j].To {
				return edges[i].Kind < edges[j].Kind
			}
			return edges[i].To < edges[j].To
		}
		return edges[i].From < edges[j].From
	})
	return qualityGraph{Nodes: nodes, Edges: edges}
}

func passStatus(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

func envDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func emptyDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func singleLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		return s[:max] + "..."
	}
	return s
}

func printReport(rep report, wikiDir string) {
	status := "PASS"
	if !rep.OK {
		status = "FAIL"
	}
	fmt.Printf("%s debug_memory_quality\n", status)
	fmt.Printf("wiki_dir=%s\n", wikiDir)
	fmt.Printf("questions=%d passed=%d evidence_hit_rate=%.0f%% proposals=%d/%d proposal_quality=%.0f%% source_hits=%d archive_hits=%d\n",
		rep.Questions,
		rep.Passed,
		rep.EvidenceHitRate*100,
		rep.ProposalsCreated,
		rep.ProposalScenarios,
		rep.ProposalQualityRate*100,
		rep.SourceEvidenceHits,
		rep.ArchiveEvidenceHits,
	)
	for _, r := range rep.Results {
		mark := "PASS"
		if !r.Pass {
			mark = "FAIL"
		}
		fmt.Printf("- %s %s evidence=%d kinds=%s", mark, r.Name, r.EvidenceCount, strings.Join(r.EvidenceKinds, ","))
		if r.ProposalID > 0 {
			fmt.Printf(" proposal=%d quality=%t", r.ProposalID, r.ProposalOK)
		}
		if len(r.MissingEvidence) > 0 {
			fmt.Printf(" missing=%s", strings.Join(r.MissingEvidence, ","))
		}
		if len(r.QualityIssues) > 0 {
			fmt.Printf(" issues=%s", strings.Join(r.QualityIssues, "; "))
		}
		fmt.Println()
	}
	for _, warning := range rep.Warnings {
		fmt.Printf("warning: %s\n", warning)
	}
}

func printLiveReport(rep liveReport, wikiDir string) {
	status := "PASS"
	if !rep.OK {
		status = "FAIL"
	}
	fmt.Printf("%s debug_memory_quality live-llm\n", status)
	fmt.Printf("model=%s wiki_dir=%s\n", rep.Model, wikiDir)
	fmt.Printf("questions=%d passed=%d routing_pass_rate=%.0f%% slow=%d budget_ms=%d avg_ms=%d max_ms=%d search_memory_calls=%d proposal_calls=%d unexpected_proposals=%d llm_calls=%d tool_calls=%d elapsed_ms=%d\n",
		rep.Questions,
		rep.Passed,
		rep.RoutingPassRate*100,
		rep.SlowScenarios,
		rep.LatencyBudgetMS,
		rep.AvgScenarioMS,
		rep.MaxScenarioMS,
		rep.SearchMemoryCalls,
		rep.ProposalCalls,
		rep.UnexpectedProposalCalls,
		rep.LLMCalls,
		rep.ToolCalls,
		rep.ElapsedMS,
	)
	for _, r := range rep.Results {
		mark := "PASS"
		if !r.Pass {
			mark = "FAIL"
		}
		fmt.Printf("- %s %s evidence=%d kinds=%s tools=%s llm_calls=%d elapsed_ms=%d",
			mark,
			r.Name,
			r.EvidenceCount,
			strings.Join(r.EvidenceKinds, ","),
			strings.Join(r.ToolCalls, ","),
			r.LLMCalls,
			r.ElapsedMS,
		)
		if r.ProposalOK {
			fmt.Printf(" proposal=true")
		}
		if r.UnexpectedProposal {
			fmt.Printf(" unexpected_proposal=true")
		}
		if len(r.Issues) > 0 {
			fmt.Printf(" issues=%s", strings.Join(r.Issues, "; "))
		}
		if r.Final != "" {
			fmt.Printf(" final=%q", r.Final)
		}
		fmt.Println()
	}
	for _, warning := range rep.Warnings {
		fmt.Printf("warning: %s\n", warning)
	}
}
