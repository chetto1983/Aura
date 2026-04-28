package wiki

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/aura/aura/internal/llm"
)

// MaxWriteRetries is the maximum number of retry attempts for wiki writes.
const MaxWriteRetries = 3

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

// WriteFromLLMOutput parses LLM output as YAML, validates it, and writes it to the wiki.
// If validation fails, it retries the LLM with schema error feedback.
func (w *Writer) WriteFromLLMOutput(ctx context.Context, rawOutput string, promptVersion string) (*Page, error) {
	page, err := parseYAML(rawOutput)
	if err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	// Set schema metadata
	page.SchemaVersion = CurrentSchemaVersion
	page.PromptVersion = promptVersion

	if err := Validate(page); err == nil {
		if writeErr := w.store.WritePage(ctx, page); writeErr != nil {
			return nil, fmt.Errorf("writing wiki page: %w", writeErr)
		}
		return page, nil
	}

	// Retry with schema error feedback
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

		// Build feedback message
		var sb strings.Builder
		sb.WriteString("Your previous YAML output had the following validation errors:\n")
		sb.WriteString(fmt.Sprintf("- %s\n", lastErr.Error()))
		sb.WriteString("\nPlease correct the YAML and provide the complete page again.\n")
		sb.WriteString("\nOriginal output:\n```\n")
		sb.WriteString(currentOutput)
		sb.WriteString("\n```\n")

		req := llm.Request{
			Messages: []llm.Message{
				{Role: "system", Content: "You are a wiki editor. Output valid YAML conforming to the wiki schema."},
				{Role: "user", Content: sb.String()},
			},
		}

		resp, err := w.llm.Send(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("LLM retry %d failed: %w", attempt, err)
		}

		currentOutput = resp.Content

		page, err := parseYAML(currentOutput)
		if err != nil {
			w.logger.Warn("YAML parse error on retry", "attempt", attempt, "error", err)
			lastErr = fmt.Errorf("failed to parse YAML: %w", err)
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

// parseYAML extracts YAML content from LLM output.
// LLM output may contain markdown code blocks around the YAML.
func parseYAML(raw string) (*Page, error) {
	content := raw

	// Extract YAML from code blocks if present
	if idx := strings.Index(content, "```yaml"); idx != -1 {
		content = content[idx+7:]
		if end := strings.Index(content, "```"); end != -1 {
			content = content[:end]
		}
	} else if idx := strings.Index(content, "```"); idx != -1 {
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
