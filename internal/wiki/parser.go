package wiki

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aura/aura/internal/llm"
	"gopkg.in/yaml.v3"
)

// MaxWriteRetries is the maximum number of retry attempts for wiki writes.
const MaxWriteRetries = 3

// RetryTimeout is the per-retry timeout for LLM calls during wiki write retries.
const RetryTimeout = 30 * time.Second

// Writer handles LLM-assisted wiki writes with schema validation and retry.
type Writer struct {
	store  *Store
	llm    llm.Client
	logger *slog.Logger
}

// NewWriter creates a new wiki Writer.
func NewWriter(store *Store, llm llm.Client, logger *slog.Logger) *Writer {
	return &Writer{
		store:  store,
		llm:    llm,
		logger: logger,
	}
}

// WriteFromLLMOutput parses LLM output, validates it, and writes it to the wiki.
// It auto-detects MD (frontmatter+body) or legacy YAML format.
// If validation fails, it retries the LLM with schema error feedback.
func (w *Writer) WriteFromLLMOutput(ctx context.Context, rawOutput string, promptVersion string) (*Page, error) {
	page, err := parseWikiOutput(rawOutput)
	if err != nil {
		return nil, fmt.Errorf("parsing wiki output: %w", err)
	}

	page.SchemaVersion = CurrentSchemaVersion
	page.PromptVersion = promptVersion

	if validateErr := Validate(page); validateErr == nil {
		if writeErr := w.store.WritePage(ctx, page); writeErr != nil {
			return nil, fmt.Errorf("writing wiki page: %w", writeErr)
		}
		return page, nil
	} else {
		err = validateErr
	}

	w.logger.Info("schema validation failed, retrying with feedback",
		"error", err,
		"title", page.Title,
	)

	return w.retryWithFeedback(ctx, rawOutput, err, promptVersion)
}

func (w *Writer) retryWithFeedback(ctx context.Context, originalOutput string, validationErr error, promptVersion string) (*Page, error) {
	var lastErr error
	lastErr = validationErr
	currentOutput := originalOutput

	for attempt := 1; attempt <= MaxWriteRetries; attempt++ {
		w.logger.Info("retrying wiki write", "attempt", attempt, "error", lastErr)

		retryCtx, cancel := context.WithTimeout(ctx, RetryTimeout)

		var sb strings.Builder
		sb.WriteString("Your previous wiki output had validation errors:\n")
		fmt.Fprintf(&sb, "- %s\n", lastErr.Error())
		sb.WriteString("\nPlease correct the output using markdown format with YAML frontmatter:\n")
		sb.WriteString("```markdown\n---\ntitle: ...\nschema_version: 2\n...\n---\n\n# Title\n\nContent with [[links]].\n```\n")
		sb.WriteString("\nOriginal output:\n```\n")
		sb.WriteString(currentOutput)
		sb.WriteString("\n```\n")

		req := llm.Request{
			Messages: []llm.Message{
				{Role: "system", Content: "You are a wiki editor. Output a markdown file with YAML frontmatter conforming to the wiki schema."},
				{Role: "user", Content: sb.String()},
			},
			Temperature: llm.Float64Ptr(0),
		}

		resp, err := w.llm.Send(retryCtx, req)
		cancel()
		if err != nil {
			lastErr = fmt.Errorf("LLM retry %d failed: %w", attempt, err)
			continue
		}

		currentOutput = resp.Content

		page, err := parseWikiOutput(currentOutput)
		if err != nil {
			w.logger.Warn("parse error on retry", "attempt", attempt, "error", err)
			lastErr = fmt.Errorf("failed to parse wiki output: %w", err)
			continue
		}

		page.SchemaVersion = CurrentSchemaVersion
		page.PromptVersion = promptVersion

		if err := Validate(page); err != nil {
			lastErr = err
			continue
		}

		if writeErr := w.store.WritePage(ctx, page); writeErr != nil {
			return nil, fmt.Errorf("writing wiki page: %w", writeErr)
		}

		w.logger.Info("wiki write succeeded after retry", "attempt", attempt, "title", page.Title)
		return page, nil
	}

	return nil, fmt.Errorf("schema validation failed after %d retries: %w", MaxWriteRetries, lastErr)
}

// parseWikiOutput auto-detects format: MD with frontmatter, or legacy YAML.
func parseWikiOutput(raw string) (*Page, error) {
	// Try MD format first (starts with ---)
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "---") {
		if page, err := ParseMD([]byte(trimmed)); err == nil {
			return page, nil
		}
	}

	// Fall back to legacy YAML extraction
	return parseYAMLOutput(raw)
}

// ParseMD parses a markdown file with YAML frontmatter into a Page.
// Format: ---\n<yaml frontmatter>\n---\n<markdown body>
func ParseMD(data []byte) (*Page, error) {
	content := string(data)

	// Must start with ---
	if !strings.HasPrefix(content, "---") {
		return nil, fmt.Errorf("markdown file must start with --- frontmatter delimiter")
	}

	// Find closing --- (must be on its own line)
	rest := content[3:]
	// Skip the first newline after opening ---
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	} else if len(rest) > 1 && rest[0] == '\r' && rest[1] == '\n' {
		rest = rest[2:]
	}

	closeIdx := findClosingDelimiter(rest)
	if closeIdx == -1 {
		return nil, fmt.Errorf("markdown file missing closing --- frontmatter delimiter")
	}

	fmContent := rest[:closeIdx]

	// Compute body position relative to original content
	// rest = content[offset:], closeIdx is within rest
	// body starts after the closing --- delimiter + newline
	bodyStartInRest := closeIdx + 3 // skip past ---
	if bodyStartInRest < len(rest) && rest[bodyStartInRest] == '\n' {
		bodyStartInRest++
	} else if bodyStartInRest+1 < len(rest) && rest[bodyStartInRest] == '\r' && rest[bodyStartInRest+1] == '\n' {
		bodyStartInRest += 2
	}

	body := ""
	if bodyStartInRest < len(rest) {
		body = rest[bodyStartInRest:]
	}

	var page Page
	if err := yaml.Unmarshal([]byte(fmContent), &page); err != nil {
		return nil, fmt.Errorf("parsing frontmatter: %w", err)
	}
	page.Body = strings.TrimSpace(body)
	return &page, nil
}

// findClosingDelimiter finds the position of a line that is just "---"
func findClosingDelimiter(s string) int {
	for i := 0; i < len(s); i++ {
		// Check if this line starts with ---
		if s[i] == '-' && i+2 < len(s) && s[i+1] == '-' && s[i+2] == '-' {
			// Verify it's on its own line (preceded by newline or start)
			if i == 0 || s[i-1] == '\n' {
				// Verify it ends at newline or end-of-string
				end := i + 3
				if end >= len(s) || s[end] == '\n' || (s[end] == '\r' && end+1 < len(s) && s[end+1] == '\n') {
					return i
				}
			}
		}
	}
	return -1
}

// parseYAMLOutput extracts YAML from LLM output (legacy format).
func parseYAMLOutput(raw string) (*Page, error) {
	content := raw

	if yamlIdx := strings.LastIndex(content, "```yaml"); yamlIdx != -1 {
		content = content[yamlIdx+7:]
		if end := strings.Index(content, "```"); end != -1 {
			content = content[:end]
		}
	} else if idx := strings.LastIndex(content, "```"); idx != -1 {
		content = content[idx+3:]
		if end := strings.Index(content, "```"); end != -1 {
			content = content[:end]
		}
	}

	content = strings.TrimSpace(content)

	page, err := ParseYAML([]byte(content))
	if err != nil {
		return nil, err
	}

	return page, nil
}

// MarshalMD serializes a Page into markdown-with-frontmatter format.
func MarshalMD(page *Page) ([]byte, error) {
	// Marshal frontmatter fields (excluding Body which has yaml:"-")
	fm := struct {
		Title         string   `yaml:"title"`
		Tags          []string `yaml:"tags,omitempty"`
		Category      string   `yaml:"category,omitempty"`
		Related       []string `yaml:"related,omitempty"`
		Sources       []string `yaml:"sources,omitempty"`
		SchemaVersion int      `yaml:"schema_version"`
		PromptVersion string   `yaml:"prompt_version"`
		CreatedAt     string   `yaml:"created_at"`
		UpdatedAt     string   `yaml:"updated_at"`
	}{
		Title:         page.Title,
		Tags:          page.Tags,
		Category:      page.Category,
		Related:       page.Related,
		Sources:       page.Sources,
		SchemaVersion: page.SchemaVersion,
		PromptVersion: page.PromptVersion,
		CreatedAt:     page.CreatedAt,
		UpdatedAt:     page.UpdatedAt,
	}

	fmData, err := yaml.Marshal(&fm)
	if err != nil {
		return nil, fmt.Errorf("marshaling frontmatter: %w", err)
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(fmData)
	buf.WriteString("---\n")
	if page.Body != "" {
		buf.WriteString(page.Body)
		if !strings.HasSuffix(page.Body, "\n") {
			buf.WriteByte('\n')
		}
	}

	return buf.Bytes(), nil
}