// debug_memory_quality is a hermetic scorecard for Aura's daily second-brain
// usefulness. It seeds realistic source/archive evidence, runs everyday
// memory questions through search_memory, creates review-gated proposals for
// scenarios that should grow the wiki, and reports quality metrics.
//
//	go run ./cmd/debug_memory_quality
//	go run ./cmd/debug_memory_quality -json
//	go run ./cmd/debug_memory_quality -keep
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
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
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	rep, wikiDir, err := run(ctx)
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
	if !rep.OK {
		os.Exit(1)
	}
}

func run(ctx context.Context) (report, string, error) {
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
	for _, sc := range scenarios() {
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
	rep.OK = rep.EvidenceHitRate >= 0.90 && rep.ProposalQualityRate >= 0.90 && rep.SourceEvidenceHits > 0 && rep.ArchiveEvidenceHits > 0
	if !rep.OK {
		rep.Warnings = append(rep.Warnings, "memory quality gate failed: want >=90% evidence hit rate and >=90% proposal quality")
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
