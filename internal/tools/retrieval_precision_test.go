//go:build integration

package tools

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type fixtureLine struct {
	Prompt       string `json:"prompt"`
	ExpectedTool string `json:"expected_tool"`
}

// TestToolRetrieval_Precision is build-tag gated to integration. Run with:
//
//	go test -tags=integration -run TestToolRetrieval_Precision ./internal/tools/
//
// The test calibrates the precision@5 threshold at 0.80 per RESEARCH.md A6.
//
// BLOCKER 1 of 2026-05-11 plan revision 2: Registry.Search takes
// (query string, limit int, excluded ...string) — NO ctx parameter.
// Verified at internal/tools/registry_search.go:19.
func TestToolRetrieval_Precision(t *testing.T) {
	f, err := os.Open("testdata/retrieval_fixture.jsonl")
	if err != nil {
		t.Skipf("fixture not available: %v", err)
	}
	defer f.Close()

	var lines []fixtureLine
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		txt := strings.TrimSpace(scan.Text())
		if txt == "" {
			continue
		}
		var fl fixtureLine
		if unmarshalErr := json.Unmarshal([]byte(txt), &fl); unmarshalErr != nil {
			t.Fatalf("malformed fixture line: %v\n%s", unmarshalErr, txt)
		}
		lines = append(lines, fl)
	}
	if len(lines) < 15 {
		t.Fatalf("fixture has %d lines, want at least 15", len(lines))
	}

	registry := buildPrecisionTestRegistry(t)

	// Attempt to enable Qdrant vector search if QDRANT_URL is set.
	// If unavailable, falls back to lexical (FTS) scoring.
	qdrantURL := os.Getenv("QDRANT_URL")
	embedURL := os.Getenv("EMBEDDING_BASE_URL")
	embedKey := os.Getenv("EMBEDDING_API_KEY")
	embedModel := os.Getenv("EMBEDDING_MODEL")
	if qdrantURL != "" && embedURL != "" && embedKey != "" {
		cfg := ToolVectorConfig{
			Backend:      "qdrant",
			TopK:         5,
			QdrantURL:    qdrantURL,
			QdrantAPIKey: os.Getenv("QDRANT_API_KEY"),
			Collection:   "aura_tool_precision_test",
			EmbedBaseURL: embedURL,
			EmbedAPIKey:  embedKey,
			EmbedModel:   embedModel,
		}
		registry.BuildVectorIndex(cfg)
		h := registry.ToolVectorHealth()
		if h.Fallback || h.DocCount == 0 {
			t.Logf("Qdrant vector index not ready (%s); running lexical-only precision test", h.LastError)
		} else {
			t.Logf("Qdrant vector index ready: backend=%s docs=%d model=%s", h.Backend, h.DocCount, h.EmbedModel)
		}
	} else {
		t.Logf("QDRANT_URL / EMBEDDING_BASE_URL / EMBEDDING_API_KEY not set; running lexical-only precision test")
	}

	var hits, misses int
	for _, fl := range lines {
		// BLOCKER 1: TWO-arg Search call (no ctx). Real signature:
		//   func (r *Registry) Search(query string, limit int, excluded ...string) []ToolSearchResult
		results := registry.Search(fl.Prompt, 5)
		found := false
		for _, r := range results {
			if r.Name == fl.ExpectedTool {
				found = true
				break
			}
		}
		if found {
			hits++
		} else {
			misses++
			t.Logf("MISS: prompt=%q expected=%q got=%v", fl.Prompt, fl.ExpectedTool, namesOf(results))
		}
	}
	precision := float64(hits) / float64(len(lines))
	if precision < 0.80 {
		t.Fatalf("precision@5 = %.2f, want >= 0.80 (hits=%d, misses=%d)", precision, hits, misses)
	}
	t.Logf("precision@5 = %.2f (hits=%d / total=%d)", precision, hits, len(lines))
}

func namesOf(results []ToolSearchResult) []string {
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, r.Name)
	}
	return out
}

// buildPrecisionTestRegistry creates a Registry populated with tool names and
// descriptions matching the production tool set. This allows the precision test
// to exercise lexical and vector retrieval paths without the full production
// dependency graph (DB, wiki store, etc.).
func buildPrecisionTestRegistry(t *testing.T) *Registry {
	t.Helper()
	reg := NewRegistry(nil)

	// Each entry is (name, description) matching real registered tool names.
	// WARNING 6 of 2026-05-11 plan revision 2: every name here must be a real
	// registered tool whose Name() returns this string. Verified against live
	// grep output on 2026-05-11.
	precisionSeeds := []struct{ name, description string }{
		{"write_wiki_page", "Write or update a wiki page in the knowledge base. Use to store notes, facts, memories, and summaries for long-term recall."},
		{"search_memory", "Search the wiki memory for relevant information. Returns pages matching the query from the knowledge base."},
		{"task", "Manage scheduled tasks: schedule reminders/recurring jobs, list, cancel, or run a saved routine immediately."},
		{"request_dashboard_token", "Generate a secure login link to access the Aura web dashboard."},
		{"web", "Search the web or fetch a URL: action=search (SearXNG) or action=fetch (HTML extraction)."},
		{"store_source", "Upload and store a document as a source. Accepts PDF and other file formats for later OCR and ingestion."},
		{"ocr_source", "Run OCR on an uploaded source document. Extracts text from a PDF or image using document AI."},
		{"ingest_source", "Ingest a source document into the wiki. Processes the OCR output and creates wiki pages from the content."},
		{"create_xlsx", "Create an Excel (.xlsx) spreadsheet file. Generates a formatted Excel workbook from the provided data."},
		{"create_docx", "Create a Word (.docx) document file. Generates a formatted Word document from the provided content."},
		{"create_pdf", "Create a PDF document file. Generates a formatted PDF from the provided content."},
		{"execute_code", "Execute Python code in a sandboxed environment. Runs scripts, performs calculations, and processes data."},
		{"write_file", "Write text content to a file in the workspace. Saves the content to the specified file path."},
		{"read_file", "Read the content of a file from the workspace. Returns the text content of the specified file."},
		{"list_files", "List files in the workspace directory. Returns a directory listing of the workspace root."},
		{"search_files", "Search for files in the workspace by name or content. Finds files matching the search pattern."},
		{"apply_patch", "Apply a patch or diff to a file in the workspace. Updates files using unified diff format."},
		{"daily_briefing", "Generate a daily briefing or summary. Compiles recent notes and scheduled items into a digest."},
		{"list_sources", "List all uploaded source documents. Returns metadata for all stored source files."},
		{"read_source", "Read the content of an uploaded source document. Returns the extracted text of the source."},
		{"lint_sources", "Lint and validate source documents. Checks sources for issues or missing metadata."},
		{"delete_source", "Delete an uploaded source document. Removes the source file and its associated data."},
		{"dev_tool", "Manage the Python script registry used by execute_code: list, read, or save reusable snippets."},
		{"execute_shell", "Execute shell commands in the Aura container. Runs system commands with git, rg, jq, and sqlite."},
	}

	for _, s := range precisionSeeds {
		reg.Register(namedDescribedTool{name: s.name, description: s.description})
	}
	return reg
}
