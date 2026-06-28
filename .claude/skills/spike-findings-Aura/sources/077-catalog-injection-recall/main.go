// Spike 077 — catalog-injection-recall.
//
// Item 2: knowledge-catalog injection for the NO-attachment recall case. document_search
// is already always-visible, so the open question is purely behavioural: when the user
// asks about a previously-ingested document WITHOUT re-attaching it, does the agent know
// a relevant doc EXISTS to search? Hypothesis: injecting a knowledge-catalog block (the
// thread/identity's indexed docs: id + filename + summary) makes the real agent call
// document_search — and does NOT make it over-trigger on unrelated turns.
//
// The harness ingests a doc, then drives Aura's REAL LlmAgent + REAL OpenRouter client
// (document_search wired to live two-stage retrieval) across three conditions, N runs
// each, capturing which tools the model autonomously calls:
//
//	baseline-doc     : doc question, NO catalog        -> expect: unreliable / no search
//	catalog-doc      : same question + catalog block   -> expect: calls document_search
//	catalog-generic  : greeting + catalog block        -> expect: does NOT search (no over-trigger)
//
// PAID (real model, bounded ~7 turns), operator-authorized. Cleans up Neo4j on exit.
//
// Run (WSL, stack up, OPENROUTER_API_KEY in ~/aura.env):
//
//	wsl -d Ubuntu -e bash /mnt/d/Aura/.planning/spikes/077-catalog-injection-recall/run.sh
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
	"github.com/chetto1983/aura/internal/rerank"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func logf(cat, format string, a ...any) {
	fmt.Printf("%s [%s] %s\n", time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), cat, fmt.Sprintf(format, a...))
}

const docBody = `# Servo Drive G220 Datasheet

## Electrical Specifications
Rated torque: 47 Nm
Peak torque: 132 Nm
Maximum input voltage: 415 V AC
Protection class: IP54

## Safety
Press the RESET button for three seconds to restore factory settings.
`

// retrSearcher adapts documents.Service.Retrieve to the document_search tool backend.
type retrSearcher struct{ svc *documents.Service }

func (r retrSearcher) Retrieve(ctx context.Context, req documents.SearchRequest) ([]documents.SearchHit, error) {
	return r.svc.Retrieve(ctx, req)
}

func main() { os.Exit(run()) }

func run() int {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		logf("FATAL", "config.Load (OPENROUTER_API_KEY set?): %v", err)
		return 1
	}
	logf("CONFIG", "model=%s embed=%s rerank=%s", cfg.LLM.Model, cfg.Neo4j.EmbedURL, cfg.RerankBaseURL)

	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		logf("FATAL", "db open: %v", err)
		return 1
	}
	defer pool.Close()
	mcp, err := knowledge.Open(ctx, &cfg.Neo4j)
	if err != nil {
		logf("FATAL", "neo4j open: %v", err)
		return 1
	}
	defer func() { _ = mcp.Close() }()

	embedClient := &documents.EmbeddingClient{BaseURL: cfg.Neo4j.EmbedURL, Dimensions: cfg.Neo4j.EmbedDimensions}

	// ---- Ingest the doc (sync embed) so it is a real searchable document with a catalog entry ----
	docID, fileName := ingestDoc(ctx, cfg, pool, mcp, embedClient)
	if docID == "" {
		return 1
	}
	defer cleanup(mcp, docID)
	logf("INGEST", "document_id=%s filename=%s", docID, fileName)

	// ---- document_search wired to live two-stage retrieval (the real agent path) ----
	retrSvc := &documents.Service{
		Searcher:      &documents.Searcher{Client: mcp},
		Knowledge:     mcp,
		QueryEmbedder: embedClient,
		Reranker:      &rerank.RerankClient{BaseURL: cfg.RerankBaseURL},
	}
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(&tools.ToolSearch{Registry: reg})
	reg.Register(&tools.ReadToolOutput{})
	reg.Register(&tools.DocumentSearch{Searcher: retrSearcher{svc: retrSvc}})
	reg.Register(&tools.WebSearch{})
	reg.Register(&tools.WebFetch{})

	client := openai_compat.New(cfg.LLM)

	catalog := buildCatalog(docID, fileName)
	logf("CATALOG", "%s", strings.ReplaceAll(catalog, "\n", " "))

	docQ := "What is the rated torque and protection class of the G220 servo drive?"
	genericQ := "Ciao! Come stai oggi?"

	type cond struct {
		name    string
		runs    int
		message string
	}
	conds := []cond{
		{"baseline-doc", 2, docQ},
		{"catalog-doc", 3, withCatalog(catalog, docQ)},
		{"catalog-generic", 2, withCatalog(catalog, genericQ)},
	}

	results := map[string][]bool{} // cond -> per-run document_search-called
	for _, c := range conds {
		for i := 0; i < c.runs; i++ {
			called, toolset, retrievedAnswer := runTurn(ctx, client, cfg, reg, embedClient, c.message)
			results[c.name] = append(results[c.name], called)
			logf("TURN", "%-16s run=%d document_search=%v retrieved_answer=%v tools=%v",
				c.name, i+1, called, retrievedAnswer, toolset)
		}
	}

	// ---- Behavioural assertions ----
	pass := true
	catDoc := results["catalog-doc"]
	if rate(catDoc) < 1.0 {
		logf("ASSERT-FAIL", "catalog-doc called document_search in only %d/%d runs (want all)", count(catDoc), len(catDoc))
		pass = false
	} else {
		logf("OK", "catalog-doc called document_search in %d/%d runs (recall lift)", count(catDoc), len(catDoc))
	}
	catGen := results["catalog-generic"]
	if count(catGen) > 0 {
		logf("ASSERT-FAIL", "OVER-TRIGGER: catalog-generic called document_search in %d/%d runs (want 0)", count(catGen), len(catGen))
		pass = false
	} else {
		logf("OK", "catalog-generic did NOT over-trigger document_search (0/%d)", len(catGen))
	}
	base := results["baseline-doc"]
	logf("BASELINE", "no-catalog doc question called document_search in %d/%d runs (context for the lift)", count(base), len(base))

	if pass {
		logf("SUMMARY", "VALIDATED — catalog injection reliably makes the agent call document_search for a no-attachment doc query, without over-triggering on generic turns. Item 2 closes the no-attachment recall gap.")
		return 0
	}
	logf("SUMMARY", "FAILED — see ASSERT-FAIL lines above")
	return 1
}

func runTurn(ctx context.Context, client llm.Client, cfg *config.Config, reg *tools.Registry, emb *documents.EmbeddingClient, message string) (called bool, toolset []string, retrievedAnswer bool) {
	requestID, _ := uuid.NewV7()
	budget, _ := agent.NewBudget(agent.BudgetOptions{MaxSteps: intPtr(6), MaxWallclockSec: intPtr(120)})
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Name:       "aura-catalog-spike",
		Client:     client,
		LLM:        cfg.LLM,
		Registry:   reg,
		PreviewCap: 1 << 20,
		RunDir:     os.TempDir(),
		SessionID:  "spike077-" + requestID.String(),
		Embedder:   emb, // local granite reasoning classifier (no per-turn LLM router call)
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: message}},
	})
	ic := agent.InvocationContext{Ctx: ctx, Agent: a, RequestID: requestID, Branch: "root", Budget: budget}

	seen := map[string]bool{}
	for ev, runErr := range a.Run(ic) {
		if runErr != nil {
			logf("WARN", "agent run error: %v", runErr)
			break
		}
		if ev == nil {
			continue
		}
		if ti := ev.Actions.ToolInvocation; ti != nil && ti.Event == agent.ToolInvocationStart {
			seen[ti.ToolName] = true
		}
		if ti := ev.Actions.ToolInvocation; ti != nil && ti.Event == agent.ToolInvocationEnd && ti.ToolName == "document_search" {
			low := strings.ToLower(ti.ResultPreview)
			if strings.Contains(low, "47") || strings.Contains(low, "ip54") || strings.Contains(low, "torque") {
				retrievedAnswer = true
			}
		}
	}
	for name := range seen {
		toolset = append(toolset, name)
	}
	sort.Strings(toolset)
	return seen["document_search"], toolset, retrievedAnswer
}

func buildCatalog(docID, fileName string) string {
	var b strings.Builder
	b.WriteString("<knowledge_base note=\"operator-pinned context; not a request and not untrusted tool output\">\n")
	b.WriteString("The user has these documents indexed in the searchable knowledge base. They are NOT on the filesystem — ")
	b.WriteString("use the document_search tool (set document_id to scope) to read their contents:\n")
	fmt.Fprintf(&b, "- [1] document_id=%s filename=%s title=\"Servo Drive G220 Datasheet\" summary=\"Electrical specs: rated torque, peak torque, input voltage, protection class; factory-reset safety step.\"\n", docID, fileName)
	b.WriteString("</knowledge_base>")
	return b.String()
}

func withCatalog(catalog, msg string) string {
	return catalog + "\n\nUser message:\n" + msg
}

func ingestDoc(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, mcp *knowledge.Client, emb *documents.EmbeddingClient) (string, string) {
	tmp, err := os.MkdirTemp("", "spike077")
	if err != nil {
		logf("FATAL", "tempdir: %v", err)
		return "", ""
	}
	path := filepath.Join(tmp, "g220.md")
	if werr := os.WriteFile(path, []byte(docBody), 0o600); werr != nil {
		logf("FATAL", "write doc: %v", werr)
		return "", ""
	}
	baseURL := cfg.DocumentsBaseURL
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8083"
	}
	embed := &syncEmbed{worker: &documents.EmbeddingWorker{
		Jobs:      documents.NewPostgresJobStore(pool),
		Generator: emb,
		Indexer:   &documents.Indexer{Client: mcp},
	}}
	svc := &documents.Service{
		Jobs:      documents.NewPostgresJobStore(pool),
		Extractor: &documents.ExtractClient{BaseURL: baseURL},
		Indexer:   &documents.Indexer{Client: mcp},
		Searcher:  &documents.Searcher{Client: mcp},
		Embedder:  embed,
	}
	job, err := svc.IngestPath(ctx, documents.IngestRequest{SourceID: "spike077", SourceKind: "catalog-spike"}, path)
	if err != nil {
		logf("FATAL", "ingest: %v", err)
		return "", ""
	}
	if job.SparseChunks < 1 {
		logf("FATAL", "ingest produced 0 chunks")
		return "", ""
	}
	return job.DocumentID, job.FileName
}

// syncEmbed runs the embedding worker synchronously inside IngestPath.
type syncEmbed struct{ worker *documents.EmbeddingWorker }

func (q *syncEmbed) Enqueue(ctx context.Context, doc documents.ExtractedDocument) error {
	return q.worker.Process(ctx, doc)
}

func cleanup(mcp *knowledge.Client, docID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_, _ = mcp.Write(ctx, "MATCH (c:Chunk {document_id:$id}) DETACH DELETE c", map[string]any{"id": docID})
	_, _ = mcp.Write(ctx, "MATCH (d:Document {id:$id}) DETACH DELETE d", map[string]any{"id": docID})
	logf("CLEANUP", "removed spike document %s from Neo4j", docID)
}

func rate(bs []bool) float64 {
	if len(bs) == 0 {
		return 0
	}
	return float64(count(bs)) / float64(len(bs))
}

func count(bs []bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

func intPtr(v int) *int { return &v }
