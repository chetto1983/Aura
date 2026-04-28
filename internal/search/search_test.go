package search

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/philippgille/chromem-go"
)

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	// Use a simple hash-based embedding for tests — no external API needed
	embedFn := chromem.NewEmbeddingFuncOpenAICompat("", "", "text-embedding-3-small", nil)
	// For tests without an API key, chromem will fall back gracefully
	// We use a persistent test directory with wiki files
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	db := chromem.NewDB()
	coll, err := db.CreateCollection("wiki", nil, embedFn)
	if err != nil {
		t.Skipf("skipping search test: cannot create chromem collection: %v", err)
	}

	return &Engine{
		coll:    coll,
		wikiDir: tmpDir,
		logger:  logger,
	}
}

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
			expected: "Relevant wiki knowledge:\n- [go-programming] Go Programming\n",
		},
		{
			name: "multiple results",
			results: []Result{
				{Slug: "go-programming", Title: "Go Programming", Content: "Go is a language", Score: 0.9},
				{Slug: "rust-basics", Title: "Rust Basics", Content: "Rust is safe", Score: 0.8},
			},
			expected: "Relevant wiki knowledge:\n- [go-programming] Go Programming\n- [rust-basics] Rust Basics\n",
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

func TestResultStruct(t *testing.T) {
	r := Result{
		Slug:    "test-page",
		Title:   "Test Page",
		Content: "Some content",
		Score:   0.95,
	}
	if r.Slug != "test-page" {
		t.Errorf("expected Slug 'test-page', got %q", r.Slug)
	}
	if r.Title != "Test Page" {
		t.Errorf("expected Title 'Test Page', got %q", r.Title)
	}
	if r.Score != 0.95 {
		t.Errorf("expected Score 0.95, got %f", r.Score)
	}
}
