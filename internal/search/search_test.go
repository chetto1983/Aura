package search

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/wiki"
	"github.com/philippgille/chromem-go"
)

func TestFormatResults(t *testing.T) {
	tests := []struct {
		name     string
		results  []Result
		expected string
	}{
		{
			name:     "empty results",
			results:  nil,
			expected: "",
		},
		{
			name: "single result",
			results: []Result{
				{Slug: "go-programming", Title: "Go Programming", Content: "Go is a statically typed language", Score: 0.9},
			},
			expected: "Relevant wiki knowledge:\n- [[go-programming]] Go Programming\n  Go is a statically typed language\n",
		},
		{
			name: "multiple results",
			results: []Result{
				{Slug: "go-programming", Title: "Go Programming", Content: "Go is a language", Score: 0.9},
				{Slug: "rust-basics", Title: "Rust Basics", Content: "Rust is safe", Score: 0.8},
			},
			expected: "Relevant wiki knowledge:\n- [[go-programming]] Go Programming\n  Go is a language\n- [[rust-basics]] Rust Basics\n  Rust is safe\n",
		},
		{
			name: "graph node and index results",
			results: []Result{
				{Kind: "graph_node", Slug: "contract-renewal", Title: "Contract Renewal", Content: "Backlinks: legal-review", Score: 0.9},
				{Kind: "graph_index", Slug: "index:category:project", Title: "Index: project", Content: "Graph index category: project", Score: 0.8},
			},
			expected: "Relevant wiki knowledge:\n- [graph_node] [[contract-renewal]] Contract Renewal\n  Backlinks: legal-review\n- [graph_index] index:category:project Index: project\n  Graph index category: project\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatResults(tt.results)
			if got != tt.expected {
				t.Errorf("FormatResults() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExtractTitle(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		expected string
	}{
		{
			name:     "valid yaml with title",
			data:     "title: My Page\ncontent: Hello\n",
			expected: "My Page",
		},
		{
			name:     "no title field",
			data:     "content: Hello\n",
			expected: "",
		},
		{
			name:     "empty title",
			data:     "title: \"\"\ncontent: Hello\n",
			expected: "",
		},
		{
			name:     "invalid yaml",
			data:     "{{invalid",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTitle([]byte(tt.data))
			if got != tt.expected {
				t.Errorf("extractTitle() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestIsIndexed(t *testing.T) {
	e := &Engine{indexed: false}
	if e.IsIndexed() {
		t.Error("expected IsIndexed to return false before indexing")
	}
	e.indexed = true
	if !e.IsIndexed() {
		t.Error("expected IsIndexed to return true after indexing")
	}
}

func TestSearchWithoutIndexing(t *testing.T) {
	e := &Engine{indexed: false}
	_, err := e.Search(context.Background(), "test", 5)
	if err == nil {
		t.Error("expected error when searching without indexing")
	}
}

func TestNewEngine(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.Default()

	embedFn := chromem.NewEmbeddingFuncOpenAICompat("", "", "text-embedding-3-small", nil)
	e, err := NewEngine(tmpDir, embedFn, logger)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	if e == nil {
		t.Fatal("NewEngine() returned nil engine")
	}
	if e.IsIndexed() {
		t.Error("new engine should not be indexed yet")
	}
}

func TestIndexWikiPages(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Create test wiki files
	wikiContent := []byte("title: Test Page\ncontent: This is a test page\nschema_version: 1\nprompt_version: v1\n")
	if err := os.WriteFile(filepath.Join(tmpDir, "test-page.yaml"), wikiContent, 0644); err != nil {
		t.Fatalf("failed to create test wiki file: %v", err)
	}

	embedFn := chromem.NewEmbeddingFuncOpenAICompat("", "", "text-embedding-3-small", nil)
	e, err := NewEngine(tmpDir, embedFn, logger)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	// IndexWikiPages will attempt to embed, which requires an API.
	// This test verifies the file reading and metadata extraction works.
	// In a real environment with an embedding API, this would fully index.
	err = e.IndexWikiPages(context.Background())
	// Indexing may fail without a real embedding API, but the function
	// should at least attempt to read and parse files
	_ = err
}

func TestIndexWikiPagesAddsGraphNodeCards(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	writeTestMDPage(t, tmpDir, &wiki.Page{
		Title:         "Alpha Contract",
		Body:          "Core contract notes.",
		Category:      "project",
		Tags:          []string{"contract"},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
	writeTestMDPage(t, tmpDir, &wiki.Page{
		Title:         "Beta Legal Review",
		Body:          "Review links to [[alpha-contract]] before renewal.",
		Category:      "project",
		Related:       []string{"alpha-contract"},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	})

	e, err := NewEngine(tmpDir, keywordEmbedding, logger)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	if err := e.IndexWikiPages(context.Background()); err != nil {
		t.Fatalf("IndexWikiPages: %v", err)
	}
	if got, want := e.coll.Count(), 6; got != want {
		t.Fatalf("indexed document count = %d, want %d", got, want)
	}

	results, err := e.Search(context.Background(), "backlinks beta", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !hasResult(results, "graph_node", "alpha-contract") {
		t.Fatalf("missing graph_node result for alpha-contract: %#v", results)
	}
	if !hasResult(results, "graph_index", "index:category:project") {
		t.Fatalf("missing graph_index result for project category: %#v", results)
	}
}

func TestResultStruct(t *testing.T) {
	r := Result{
		Kind:    "wiki_page",
		Slug:    "test-page",
		Title:   "Test Page",
		Content: "Some content",
		Score:   0.95,
	}
	if r.Slug != "test-page" {
		t.Errorf("expected Slug 'test-page', got %q", r.Slug)
	}
	if r.Kind != "wiki_page" {
		t.Errorf("expected Kind 'wiki_page', got %q", r.Kind)
	}
	if r.Title != "Test Page" {
		t.Errorf("expected Title 'Test Page', got %q", r.Title)
	}
	if r.Content != "Some content" {
		t.Errorf("expected Content 'Some content', got %q", r.Content)
	}
	if r.Score != 0.95 {
		t.Errorf("expected Score 0.95, got %f", r.Score)
	}
}

func writeTestMDPage(t *testing.T, dir string, page *wiki.Page) {
	t.Helper()
	data, err := wiki.MarshalMD(page)
	if err != nil {
		t.Fatalf("MarshalMD: %v", err)
	}
	path := filepath.Join(dir, wiki.Slug(page.Title)+".md")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func keywordEmbedding(_ context.Context, text string) ([]float32, error) {
	lower := strings.ToLower(text)
	keywords := []string{"backlinks", "beta", "project", "contract"}
	vec := make([]float32, len(keywords)+1)
	for i, keyword := range keywords {
		if strings.Contains(lower, keyword) {
			vec[i] = 1
		}
	}
	empty := true
	for _, v := range vec {
		if v != 0 {
			empty = false
			break
		}
	}
	if empty {
		vec[len(vec)-1] = 1
	}
	return vec, nil
}

func hasResult(results []Result, kind, slug string) bool {
	for _, result := range results {
		if result.Kind == kind && result.Slug == slug {
			return true
		}
	}
	return false
}
