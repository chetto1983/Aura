package wiki

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aura/aura/internal/llm"
	"gopkg.in/yaml.v3"
)

func validPage() *Page {
	return &Page{
		Title:         "Test Page",
		Content:       "This is test content.",
		Tags:          []string{"test"},
		SchemaVersion: 1,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	}
}

func TestStoreWriteAndRead(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wiki-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger := slog.Default()
	store, err := NewStore(tmpDir, logger)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	page := validPage()
	ctx := context.Background()

	if err := store.WritePage(ctx, page); err != nil {
		t.Fatalf("WritePage() error = %v", err)
	}

	readPage, err := store.ReadPage(Slug(page.Title))
	if err != nil {
		t.Fatalf("ReadPage() error = %v", err)
	}

	if readPage.Title != page.Title {
		t.Errorf("Title = %q, want %q", readPage.Title, page.Title)
	}
	if readPage.Content != page.Content {
		t.Errorf("Content = %q, want %q", readPage.Content, page.Content)
	}
}

func TestStoreAtomicWrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wiki-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger := slog.Default()
	store, err := NewStore(tmpDir, logger)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	page := validPage()
	ctx := context.Background()

	if err := store.WritePage(ctx, page); err != nil {
		t.Fatalf("WritePage() error = %v", err)
	}

	// Check no .tmp files remain
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file should not remain: %s", e.Name())
		}
	}
}

func TestStoreValidationBeforeWrite(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wiki-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger := slog.Default()
	store, err := NewStore(tmpDir, logger)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	page := &Page{
		Title:         "Bad Page",
		Content:       "",
		SchemaVersion: 1,
		PromptVersion: "v1",
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
	}

	ctx := context.Background()
	err = store.WritePage(ctx, page)
	if err == nil {
		t.Error("expected error for invalid page")
	}
}

func TestStoreDeletePage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wiki-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger := slog.Default()
	store, err := NewStore(tmpDir, logger)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	page := validPage()
	ctx := context.Background()

	if err := store.WritePage(ctx, page); err != nil {
		t.Fatalf("WritePage() error = %v", err)
	}

	slug := Slug(page.Title)
	if err := store.DeletePage(ctx, slug); err != nil {
		t.Fatalf("DeletePage() error = %v", err)
	}

	_, err = store.ReadPage(slug)
	if err == nil {
		t.Error("expected error reading deleted page")
	}
}

func TestStoreListPages(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wiki-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger := slog.Default()
	store, err := NewStore(tmpDir, logger)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	ctx := context.Background()

	page1 := validPage()
	page1.Title = "First Page"
	store.WritePage(ctx, page1)

	page2 := validPage()
	page2.Title = "Second Page"
	store.WritePage(ctx, page2)

	slugs, err := store.ListPages()
	if err != nil {
		t.Fatalf("ListPages() error = %v", err)
	}

	if len(slugs) < 2 {
		t.Errorf("ListPages() returned %d pages, want at least 2", len(slugs))
	}
}

func TestStoreFileMutex(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wiki-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger := slog.Default()
	store, err := NewStore(tmpDir, logger)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Verify per-file mutex returns the same mutex for the same slug
	mu1 := store.fileMutex("test-slug")
	mu2 := store.fileMutex("test-slug")
	if mu1 != mu2 {
		t.Error("fileMutex should return same mutex for same slug")
	}

	// Different slug should return different mutex
	mu3 := store.fileMutex("other-slug")
	if mu1 == mu3 {
		t.Error("fileMutex should return different mutex for different slug")
	}
}

func TestParseYAMLWithCodeBlock(t *testing.T) {
	page := validPage()
	data, err := yaml.Marshal(page)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	raw := "Here is the page:\n```yaml\n" + string(data) + "\n```\n"

	result, err := parseYAML(raw)
	if err != nil {
		t.Fatalf("parseYAML() error = %v", err)
	}
	if result.Title != page.Title {
		t.Errorf("Title = %q, want %q", result.Title, page.Title)
	}
}

func TestWriteFromLLMOutput(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wiki-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger := slog.Default()
	store, err := NewStore(tmpDir, logger)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	mockLLM := &wikiMockLLM{}

	writer := NewWriter(store, mockLLM, logger)

	page := validPage()
	data, err := yaml.Marshal(page)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	ctx := context.Background()
	result, err := writer.WriteFromLLMOutput(ctx, string(data), "v1")
	if err != nil {
		t.Fatalf("WriteFromLLMOutput() error = %v", err)
	}
	if result.Title != page.Title {
		t.Errorf("Title = %q, want %q", result.Title, page.Title)
	}
}

func TestWriteFromLLMOutputInvalidYAML(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wiki-test-*")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logger := slog.Default()
	store, err := NewStore(tmpDir, logger)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	validData, _ := yaml.Marshal(validPage())
	mockLLM := &wikiMockLLM{
		response: string(validData),
	}

	writer := NewWriter(store, mockLLM, logger)

	ctx := context.Background()
	_, err = writer.WriteFromLLMOutput(ctx, "not: valid: yaml: [", "v1")
	if err == nil {
		t.Error("expected error for invalid YAML input")
	}
}

// wikiMockLLM implements llm.Client for wiki tests.
type wikiMockLLM struct {
	response string
	sendFn   func(ctx context.Context, req llm.Request) (llm.Response, error)
}

func (m *wikiMockLLM) Send(ctx context.Context, req llm.Request) (llm.Response, error) {
	if m.sendFn != nil {
		return m.sendFn(ctx, req)
	}
	return llm.Response{Content: m.response}, nil
}

func (m *wikiMockLLM) Stream(ctx context.Context, req llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token, 1)
	ch <- llm.Token{Done: true}
	close(ch)
	return ch, nil
}
