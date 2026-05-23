package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/memoryindex"
)

const (
	DefaultMemoryJudgeBatchSize         = 20
	DefaultMemoryJudgeExistingLimit     = 50
	DefaultMemoryJudgePromptBudgetBytes = 8192
)

// Lesson is the bounded operational-memory write captured from a successful
// propose_patch action=operational call in the just-finished turn.
type Lesson struct {
	ID          string
	ToolName    string
	ErrorClass  string
	Text        string
	Priority    string
	ContentHash string
}

type MemoryJudgeHook struct {
	Enabled         bool
	Client          llm.Client
	Model           string
	ReasoningEffort string
	Logger          *slog.Logger
}

func NewMemoryJudgeHook(enabled bool, client llm.Client, model, reasoningEffort string, logger *slog.Logger) PostTurnHook {
	if !enabled || client == nil {
		return nil
	}
	return MemoryJudgeHook{
		Enabled:         true,
		Client:          client,
		Model:           model,
		ReasoningEffort: reasoningEffort,
		Logger:          logger,
	}
}

func (h MemoryJudgeHook) Apply(ctx context.Context, turn TurnRecord, store *memoryindex.Store) []error {
	if !h.Enabled || len(turn.OperationalWrites) == 0 {
		return nil
	}
	err := ResolveBatch(ctx, store, turn.OperationalWrites,
		WithMemoryJudgeClient(h.Client, h.Model, h.ReasoningEffort),
		WithMemoryJudgeLogger(h.Logger),
	)
	if err != nil {
		return []error{err}
	}
	return nil
}

type memoryJudgeConfig struct {
	client            llm.Client
	model             string
	reasoningEffort   string
	logger            *slog.Logger
	maxBatchSize      int
	existingLimit     int
	promptBudgetBytes int
	now               func() time.Time
}

type MemoryJudgeOption func(*memoryJudgeConfig)

func WithMemoryJudgeClient(client llm.Client, model, reasoningEffort string) MemoryJudgeOption {
	return func(cfg *memoryJudgeConfig) {
		cfg.client = client
		cfg.model = model
		cfg.reasoningEffort = reasoningEffort
	}
}

func WithMemoryJudgeLogger(logger *slog.Logger) MemoryJudgeOption {
	return func(cfg *memoryJudgeConfig) {
		cfg.logger = logger
	}
}

// ResolveBatch reconciles just-written operational lessons against the existing
// operational store. It is intentionally best-effort: malformed judge output or
// LLM failure returns/logs an error without corrupting the rows already added by
// propose_patch.
func ResolveBatch(ctx context.Context, store *memoryindex.Store, newLessons []Lesson, options ...MemoryJudgeOption) error {
	if store == nil {
		return fmt.Errorf("memory_judge: memory store unavailable")
	}
	cfg := memoryJudgeConfig{
		logger:            slog.Default(),
		maxBatchSize:      DefaultMemoryJudgeBatchSize,
		existingLimit:     DefaultMemoryJudgeExistingLimit,
		promptBudgetBytes: DefaultMemoryJudgePromptBudgetBytes,
		now:               func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		if option != nil {
			option(&cfg)
		}
	}
	lessons := normalizeJudgeLessons(newLessons)
	if len(lessons) == 0 {
		return nil
	}
	allNewIDs := map[string]struct{}{}
	for _, lesson := range lessons {
		allNewIDs[lesson.ID] = struct{}{}
	}
	for start := 0; start < len(lessons); start += cfg.maxBatchSize {
		end := start + cfg.maxBatchSize
		if end > len(lessons) {
			end = len(lessons)
		}
		if err := resolveJudgeChunk(ctx, store, lessons[start:end], allNewIDs, cfg); err != nil {
			return err
		}
	}
	return nil
}

func resolveJudgeChunk(ctx context.Context, store *memoryindex.Store, newLessons []Lesson, allNewIDs map[string]struct{}, cfg memoryJudgeConfig) error {
	fetchLimit := cfg.existingLimit + len(newLessons) + 20
	if fetchLimit < cfg.existingLimit*2 {
		fetchLimit = cfg.existingLimit * 2
	}
	all, err := store.FetchOperationalForJudge(ctx, fetchLimit)
	if err != nil {
		return fmt.Errorf("memory_judge: fetch candidates: %w", err)
	}
	existing := make([]memoryindex.Document, 0, len(all))
	for _, doc := range all {
		if _, isNew := allNewIDs[doc.ID]; isNew {
			continue
		}
		existing = append(existing, doc)
	}

	pending := removeExactHashDuplicates(ctx, store, existing, newLessons, cfg)
	if len(pending) == 0 || len(existing) == 0 {
		return nil
	}
	if cfg.client == nil {
		return fmt.Errorf("memory_judge: llm client unavailable")
	}

	candidates := selectJudgeCandidates(existing, cfg.existingLimit, cfg.promptBudgetBytes)
	if len(candidates) == 0 {
		return nil
	}
	resp, err := callMemoryJudge(ctx, cfg, candidates, pending)
	if err != nil {
		return fmt.Errorf("memory_judge: llm call: %w", err)
	}
	decisions, err := parseMemoryJudgeResponse(resp.Content)
	if err != nil {
		if cfg.logger != nil {
			cfg.logger.Warn("memory_judge: malformed judge response; keeping original ADD rows", "error", err)
		}
		return nil
	}
	if err := applyMemoryJudgeDecisions(ctx, store, candidates, pending, decisions, cfg); err != nil {
		return err
	}
	return nil
}

func normalizeJudgeLessons(in []Lesson) []Lesson {
	out := make([]Lesson, 0, len(in))
	seen := map[string]struct{}{}
	for _, lesson := range in {
		lesson.ToolName = strings.TrimSpace(lesson.ToolName)
		lesson.ErrorClass = strings.TrimSpace(lesson.ErrorClass)
		lesson.Text = strings.TrimSpace(lesson.Text)
		if lesson.ToolName == "" || lesson.ErrorClass == "" || lesson.Text == "" {
			continue
		}
		if strings.TrimSpace(lesson.ID) == "" {
			lesson.ID = operationalLessonID(lesson.ToolName, lesson.ErrorClass, lesson.Text)
		}
		lesson.Priority = memoryindex.NormalizePriority(lesson.Priority)
		if lesson.ContentHash == "" {
			lesson.ContentHash = memoryindex.ContentHash(memoryindex.KindOperational, lesson.Text, "")
		}
		if _, ok := seen[lesson.ID]; ok {
			continue
		}
		seen[lesson.ID] = struct{}{}
		out = append(out, lesson)
	}
	return out
}

func removeExactHashDuplicates(ctx context.Context, store *memoryindex.Store, existing []memoryindex.Document, newLessons []Lesson, cfg memoryJudgeConfig) []Lesson {
	existingHashes := map[string]struct{}{}
	for _, doc := range existing {
		if hash := strings.TrimSpace(doc.ContentHash); hash != "" {
			existingHashes[hash] = struct{}{}
		}
	}
	pending := make([]Lesson, 0, len(newLessons))
	for _, lesson := range newLessons {
		hashes := []string{strings.TrimSpace(lesson.ContentHash)}
		hashes = append(hashes, memoryindex.ContentHash(memoryindex.KindOperational, lesson.Text, store.EmbeddingModelID))
		hashes = append(hashes, memoryindex.ContentHash(memoryindex.KindOperational, lesson.Text, ""))
		duplicate := false
		for _, hash := range hashes {
			if hash == "" {
				continue
			}
			if _, ok := existingHashes[hash]; ok {
				duplicate = true
				break
			}
		}
		if duplicate {
			if err := store.DeleteDocument(ctx, lesson.ID); err != nil && cfg.logger != nil {
				cfg.logger.Warn("memory_judge: failed to remove duplicate new lesson", "error", err)
			}
			continue
		}
		pending = append(pending, lesson)
	}
	return pending
}

func selectJudgeCandidates(existing []memoryindex.Document, limit, budgetBytes int) []memoryindex.Document {
	if limit <= 0 {
		limit = DefaultMemoryJudgeExistingLimit
	}
	if budgetBytes <= 0 {
		budgetBytes = DefaultMemoryJudgePromptBudgetBytes
	}
	docs := append([]memoryindex.Document(nil), existing...)
	sort.SliceStable(docs, func(i, j int) bool {
		if !docs[i].UpdatedAt.Equal(docs[j].UpdatedAt) {
			return docs[i].UpdatedAt.After(docs[j].UpdatedAt)
		}
		return docs[i].ID < docs[j].ID
	})
	if len(docs) > limit {
		docs = docs[:limit]
	}
	out := make([]memoryindex.Document, 0, len(docs))
	used := 0
	for _, doc := range docs {
		cost := len(doc.ID) + len(doc.Title) + len(doc.Body) + 32
		if len(out) > 0 && used+cost > budgetBytes {
			break
		}
		out = append(out, doc)
		used += cost
	}
	return out
}

func callMemoryJudge(ctx context.Context, cfg memoryJudgeConfig, existing []memoryindex.Document, pending []Lesson) (llm.Response, error) {
	temp := 0.0
	return cfg.client.Send(ctx, llm.Request{
		Model:           cfg.model,
		Temperature:     &temp,
		ReasoningEffort: cfg.reasoningEffort,
		Messages: []llm.Message{
			{Role: "system", Content: memoryJudgeSystemPrompt},
			{Role: "user", Content: buildMemoryJudgePrompt(existing, pending)},
		},
	})
}

const memoryJudgeSystemPrompt = `You reconcile Aura operational lessons.
Return JSON only: {"memory":[{"id":"new:0","text":"final lesson text","event":"ADD|UPDATE|DELETE|NOOP","old_memory":"existing text when relevant"}]}.
Use ADD to keep a new lesson, UPDATE to rewrite an existing lesson, DELETE to remove an obsolete existing lesson, and NOOP to drop a duplicate or low-value new lesson.
For UPDATE and DELETE, id must be one of the existing numeric ids. For ADD and NOOP, id must be one of the new ids. Never invent ids.`

func buildMemoryJudgePrompt(existing []memoryindex.Document, pending []Lesson) string {
	var sb strings.Builder
	sb.WriteString("Existing operational lessons:\n")
	for i, doc := range existing {
		fmt.Fprintf(&sb, "[%d]\npriority: %s\ntitle: %s\ntext: %s\n\n",
			i, memoryindex.NormalizePriority(doc.Priority), compactJudgeLine(doc.Title), compactJudgeLine(doc.Body))
	}
	sb.WriteString("New lessons from this turn:\n")
	for i, lesson := range pending {
		fmt.Fprintf(&sb, "[new:%d]\ntool: %s\nerror_class: %s\npriority: %s\ntext: %s\n\n",
			i, compactJudgeLine(lesson.ToolName), compactJudgeLine(lesson.ErrorClass), memoryindex.NormalizePriority(lesson.Priority), compactJudgeLine(lesson.Text))
	}
	sb.WriteString("Choose one event per new lesson when possible. Prefer UPDATE over ADD when an existing lesson covers the same operational pattern.")
	return sb.String()
}

func compactJudgeLine(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

type memoryJudgeResponse struct {
	Memory []memoryJudgeDecision `json:"memory"`
}

type memoryJudgeDecision struct {
	ID        string `json:"id"`
	Text      string `json:"text"`
	Event     string `json:"event"`
	OldMemory string `json:"old_memory"`
}

func parseMemoryJudgeResponse(content string) ([]memoryJudgeDecision, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, fmt.Errorf("empty response")
	}
	content = extractJudgeJSONObject(content)
	var parsed memoryJudgeResponse
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, err
	}
	return parsed.Memory, nil
}

func extractJudgeJSONObject(content string) string {
	if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end >= start {
		return content[start : end+1]
	}
	return content
}

func applyMemoryJudgeDecisions(ctx context.Context, store *memoryindex.Store, existing []memoryindex.Document, pending []Lesson, decisions []memoryJudgeDecision, cfg memoryJudgeConfig) error {
	existingByID := map[string]memoryindex.Document{}
	for i, doc := range existing {
		existingByID[fmt.Sprintf("%d", i)] = doc
		existingByID[doc.ID] = doc
	}
	newByID := map[string]Lesson{}
	for i, lesson := range pending {
		newByID[fmt.Sprintf("new:%d", i)] = lesson
		newByID[lesson.ID] = lesson
	}
	singlePendingID := ""
	if len(pending) == 1 {
		singlePendingID = pending[0].ID
	}
	for _, decision := range decisions {
		event := normalizeJudgeEvent(decision.Event)
		if event == "" {
			continue
		}
		id := strings.TrimSpace(decision.ID)
		switch event {
		case "ADD":
			lesson, ok := newByID[id]
			if !ok && len(pending) == 1 {
				lesson = pending[0]
				ok = true
			}
			if !ok {
				logJudgeSkip(cfg, "add_unknown_id")
				continue
			}
			text := strings.TrimSpace(decision.Text)
			if text != "" {
				lesson.Text = text
			}
			if err := store.Upsert(ctx, documentFromLesson(lesson, cfg.now())); err != nil {
				return fmt.Errorf("memory_judge: apply ADD: %w", err)
			}
		case "UPDATE":
			doc, ok := existingByID[id]
			if !ok {
				logJudgeSkip(cfg, "update_unknown_id")
				continue
			}
			text := strings.TrimSpace(decision.Text)
			if text == "" {
				logJudgeSkip(cfg, "update_empty_text")
				continue
			}
			doc.Body = text
			doc.Priority = highestPriority(doc.Priority, firstPendingPriority(pending))
			doc.UpdatedAt = cfg.now()
			doc.ContentHash = ""
			if err := store.Upsert(ctx, doc); err != nil {
				return fmt.Errorf("memory_judge: apply UPDATE: %w", err)
			}
			if singlePendingID != "" {
				if err := store.DeleteDocument(ctx, singlePendingID); err != nil {
					return fmt.Errorf("memory_judge: remove updated new row: %w", err)
				}
			}
		case "DELETE":
			if doc, ok := existingByID[id]; ok {
				if err := store.DeleteDocument(ctx, doc.ID); err != nil {
					return fmt.Errorf("memory_judge: apply DELETE: %w", err)
				}
				continue
			}
			if lesson, ok := newByID[id]; ok {
				if err := store.DeleteDocument(ctx, lesson.ID); err != nil {
					return fmt.Errorf("memory_judge: apply DELETE new: %w", err)
				}
				continue
			}
			logJudgeSkip(cfg, "delete_unknown_id")
		case "NOOP":
			if lesson, ok := newByID[id]; ok {
				if err := store.DeleteDocument(ctx, lesson.ID); err != nil {
					return fmt.Errorf("memory_judge: apply NOOP: %w", err)
				}
				continue
			}
			if singlePendingID != "" {
				if err := store.DeleteDocument(ctx, singlePendingID); err != nil {
					return fmt.Errorf("memory_judge: apply NOOP single: %w", err)
				}
			}
		}
	}
	return nil
}

func documentFromLesson(lesson Lesson, now time.Time) memoryindex.Document {
	id := strings.TrimSpace(lesson.ID)
	if id == "" {
		id = operationalLessonID(lesson.ToolName, lesson.ErrorClass, lesson.Text)
	}
	return memoryindex.Document{
		ID:        id,
		Kind:      memoryindex.KindOperational,
		Title:     fmt.Sprintf("Lesson: %s %s", lesson.ToolName, lesson.ErrorClass),
		Body:      lesson.Text,
		Handle:    id,
		Status:    "active",
		Priority:  memoryindex.NormalizePriority(lesson.Priority),
		UpdatedAt: now,
	}
}

func normalizeJudgeEvent(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "ADD":
		return "ADD"
	case "UPDATE":
		return "UPDATE"
	case "DELETE":
		return "DELETE"
	case "NOOP", "NONE":
		return "NOOP"
	default:
		return ""
	}
}

func firstPendingPriority(pending []Lesson) string {
	for _, lesson := range pending {
		if priority := memoryindex.NormalizePriority(lesson.Priority); priority != memoryindex.PriorityNormal {
			return priority
		}
	}
	return memoryindex.PriorityNormal
}

func highestPriority(a, b string) string {
	if priorityRankValue(b) < priorityRankValue(a) {
		return memoryindex.NormalizePriority(b)
	}
	return memoryindex.NormalizePriority(a)
}

func priorityRankValue(priority string) int {
	switch memoryindex.NormalizePriority(priority) {
	case memoryindex.PriorityCritical:
		return 0
	case memoryindex.PriorityHigh:
		return 1
	default:
		return 2
	}
}

func logJudgeSkip(cfg memoryJudgeConfig, reason string) {
	if cfg.logger != nil {
		cfg.logger.Warn("memory_judge: skipped hallucinated or invalid judge action", "reason", reason)
	}
}
