package memoryindex

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/storage/freshness"
	source "github.com/aura/aura/internal/storage/sources/store"
)

const (
	sourceIndexReadLimit = 12000
	// Zero asks the archive reader for a full scan. Rebuild must refresh the
	// whole compact archive; bounded dashboard reads still pass positive limits.
	archiveIndexScanLimit = 0
	proposalIndexLimit    = 500
	sourceSnippetLimit    = 2400
	archiveSnippetLimit   = 1600
	proposalSnippetLimit  = 1200
)

var sourcePageHeadingRE = regexp.MustCompile(`(?m)^## Page ([0-9]+)\s*$`)

type RebuildInput struct {
	Sources          source.Repository
	Archive          conversation.TurnReader
	Proposals        summarizer.ProposalLister
	SkipVector       bool
	FreshnessStore   *freshness.Store // nil = skip freshness tracking
	EmbeddingModelID string
	IndexBuildID     string
}

type RebuildReport struct {
	SourcesIndexed   int
	ArchiveIndexed   int
	ProposalsIndexed int
	Vector           VectorReport
}

func Rebuild(ctx context.Context, store *Store, in RebuildInput) (RebuildReport, error) {
	if store == nil {
		return RebuildReport{}, fmt.Errorf("memoryindex: store required")
	}
	var report RebuildReport
	replaced := false
	if in.Sources != nil {
		docs, err := sourceDocuments(in.Sources)
		if err != nil {
			return report, err
		}
		stampFreshnessFields(docs, in.EmbeddingModelID, in.IndexBuildID)
		if err := store.ReplaceKind(ctx, KindSource, docs); err != nil {
			return report, err
		}
		replaced = true
		report.SourcesIndexed = len(docs)
	}
	if in.Archive != nil {
		turns, err := in.Archive.ListAll(ctx, archiveIndexScanLimit)
		if err != nil {
			return report, fmt.Errorf("memoryindex: list archive: %w", err)
		}
		docs := make([]Document, 0, len(turns))
		for _, turn := range turns {
			if eligible, reason := ArchiveTurnEligibility(turn); !eligible {
				slog.Debug("compact_archive_skipped", "role", turn.Role, "reason", reason)
				continue
			}
			if doc, ok := ArchiveDocument(turn); ok {
				docs = append(docs, doc)
			}
		}
		stampFreshnessFields(docs, in.EmbeddingModelID, in.IndexBuildID)
		if err := store.ReplaceKind(ctx, KindArchive, docs); err != nil {
			return report, err
		}
		replaced = true
		report.ArchiveIndexed = len(docs)
	}
	if in.Proposals != nil {
		proposals, err := in.Proposals.List(ctx, "", proposalIndexLimit)
		if err != nil {
			return report, fmt.Errorf("memoryindex: list proposals: %w", err)
		}
		docs := make([]Document, 0, len(proposals))
		for _, proposal := range proposals {
			if doc, ok := ProposalDocument(proposal); ok {
				docs = append(docs, doc)
			}
		}
		stampFreshnessFields(docs, in.EmbeddingModelID, in.IndexBuildID)
		if err := store.ReplaceKind(ctx, KindProposal, docs); err != nil {
			return report, err
		}
		replaced = true
		report.ProposalsIndexed = len(docs)
	}
	if replaced {
		if err := store.RebuildFTS(ctx); err != nil {
			return report, err
		}
	}
	if !in.SkipVector {
		vectorReport, err := store.SyncVector(ctx)
		if err != nil {
			return report, fmt.Errorf("memoryindex: sync vector mirror: %w", err)
		}
		report.Vector = vectorReport
	}
	if in.FreshnessStore != nil && in.IndexBuildID != "" {
		if err := in.FreshnessStore.MarkRebuildComplete(ctx, compactMemoryProjectionID, in.IndexBuildID, in.EmbeddingModelID); err != nil {
			slog.Debug("memoryindex: freshness mark rebuild complete failed", "error", err)
		}
	}
	return report, nil
}

// stampFreshnessFields sets ContentHash, EmbeddingModelID, and IndexBuildID on
// each document using the given embedding model and build ID.
func stampFreshnessFields(docs []Document, embeddingModelID, indexBuildID string) {
	for i := range docs {
		docs[i].ContentHash = ContentHash(docs[i].Kind, docs[i].Body, embeddingModelID)
		docs[i].EmbeddingModelID = embeddingModelID
		docs[i].IndexBuildID = indexBuildID
	}
}

func sourceDocuments(store source.Repository) ([]Document, error) {
	sources, err := store.List(source.ListFilter{})
	if err != nil {
		return nil, fmt.Errorf("memoryindex: list sources: %w", err)
	}
	var docs []Document
	for _, src := range sources {
		body, err := readSourceIndexBody(store, src)
		if err != nil || strings.TrimSpace(body) == "" {
			continue
		}
		pageDocs := sourcePageDocuments(src, body)
		docs = append(docs, pageDocs...)
	}
	return docs, nil
}

func sourcePageDocuments(src *source.Source, body string) []Document {
	if src == nil {
		return nil
	}
	pages := splitSourcePages(body)
	if len(pages) == 0 {
		pages = []sourcePage{{Number: 0, Body: body}}
	}
	docs := make([]Document, 0, len(pages))
	for _, page := range pages {
		text := compactForIndex(page.Body, sourceSnippetLimit)
		if text == "" {
			continue
		}
		id := "source:" + src.ID
		handle := id
		if page.Number > 0 {
			id = fmt.Sprintf("source:%s#page=%d", src.ID, page.Number)
			handle = id
		}
		docs = append(docs, Document{
			ID:        id,
			Kind:      KindSource,
			Title:     src.Filename,
			Body:      text,
			Handle:    handle,
			SourceID:  src.ID,
			Page:      page.Number,
			Status:    string(src.Status),
			Tags:      []string{string(src.Kind), string(src.Status)},
			UpdatedAt: src.CreatedAt,
		})
	}
	return docs
}

type sourcePage struct {
	Number int
	Body   string
}

func splitSourcePages(body string) []sourcePage {
	matches := sourcePageHeadingRE.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]sourcePage, 0, len(matches))
	for i, match := range matches {
		start := match[1]
		end := len(body)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		number, _ := strconv.Atoi(body[match[2]:match[3]])
		out = append(out, sourcePage{
			Number: number,
			Body:   strings.TrimSpace(body[start:end]),
		})
	}
	return out
}

func readSourceIndexBody(store source.Repository, src *source.Source) (string, error) {
	for _, name := range []string{"ocr.md", source.ExtractMarkdownFile, source.OriginalFilenameForKind(src.Kind, src.Filename)} {
		path := store.Path(src.ID, name)
		if path == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if len(data) > sourceIndexReadLimit {
			data = data[:sourceIndexReadLimit]
		}
		return string(data), nil
	}
	return "", fmt.Errorf("memoryindex: no readable source body for %s", src.ID)
}

func ArchiveDocument(turn conversation.Turn) (Document, bool) {
	body := compactForIndex(turn.Content, archiveSnippetLimit)
	if body == "" {
		return Document{}, false
	}
	id := fmt.Sprintf("archive:%d", turn.ID)
	handle := fmt.Sprintf("conversation:%d", turn.ID)
	if turn.ID == 0 {
		id = fmt.Sprintf("archive:%d:%d", turn.ChatID, turn.TurnIndex)
		handle = fmt.Sprintf("conversation:chat:%d#turn=%d", turn.ChatID, turn.TurnIndex)
	}
	title := fmt.Sprintf("chat=%d turn=%d", turn.ChatID, turn.TurnIndex)
	updatedAt := turn.CreatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	return Document{
		ID:             id,
		Kind:           KindArchive,
		Title:          title,
		Body:           body,
		Handle:         handle,
		ChatID:         turn.ChatID,
		ConversationID: turn.ID,
		Tags:           []string{turn.Role},
		UpdatedAt:      updatedAt,
	}, true
}

func ProposalDocument(proposal summarizer.ProposedUpdate) (Document, bool) {
	body := compactForIndex(proposal.Fact, proposalSnippetLimit)
	if body == "" {
		return Document{}, false
	}
	title := strings.TrimSpace(proposal.TargetSlug)
	if title == "" {
		title = proposal.Action
	}
	updatedAt := proposal.CreatedAt
	if updatedAt.IsZero() {
		updatedAt = time.Now().UTC()
	}
	return Document{
		ID:         fmt.Sprintf("proposal:%d", proposal.ID),
		Kind:       KindProposal,
		Title:      title,
		Body:       body,
		Handle:     fmt.Sprintf("proposal:%d", proposal.ID),
		ChatID:     proposal.ChatID,
		ProposalID: proposal.ID,
		Status:     proposal.Status,
		Tags:       append([]string{proposal.Action, proposal.Category, proposal.Status}, proposal.RelatedSlugs...),
		UpdatedAt:  updatedAt,
	}, true
}

type IndexingTurnAppender struct {
	next             conversation.TurnAppender
	index            *Store
	freshnessStore   *freshness.Store // nil = skip freshness tracking
	embeddingModelID string
}

func NewIndexingTurnAppender(next conversation.TurnAppender, index *Store) *IndexingTurnAppender {
	return &IndexingTurnAppender{next: next, index: index}
}

// WithFreshness wires freshness tracking onto the appender. Returns the appender
// for chaining. Safe to call after construction and before first Append.
func (a *IndexingTurnAppender) WithFreshness(fs *freshness.Store, embeddingModelID string) *IndexingTurnAppender {
	a.freshnessStore = fs
	a.embeddingModelID = embeddingModelID
	return a
}

type IndexingArchiveRepository struct {
	conversation.ArchiveRepository
	index            *Store
	freshnessStore   *freshness.Store
	embeddingModelID string
}

func NewIndexingArchiveRepository(next conversation.ArchiveRepository, index *Store) *IndexingArchiveRepository {
	return &IndexingArchiveRepository{ArchiveRepository: next, index: index}
}

// WithFreshness wires freshness tracking into the archive repository's append path.
func (r *IndexingArchiveRepository) WithFreshness(fs *freshness.Store, embeddingModelID string) *IndexingArchiveRepository {
	r.freshnessStore = fs
	r.embeddingModelID = embeddingModelID
	return r
}

func (r *IndexingArchiveRepository) Append(ctx context.Context, turn conversation.Turn) error {
	appender := IndexingTurnAppender{
		next:             r.ArchiveRepository,
		index:            r.index,
		freshnessStore:   r.freshnessStore,
		embeddingModelID: r.embeddingModelID,
	}
	return appender.Append(ctx, turn)
}

func (r *IndexingArchiveRepository) DeleteByChat(ctx context.Context, chatID int64) (int64, error) {
	if purgeErr := r.index.PurgeArchiveByChat(ctx, chatID); purgeErr != nil {
		return 0, purgeErr
	}
	return r.ArchiveRepository.DeleteByChat(ctx, chatID)
}

func (r *IndexingArchiveRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	if purgeErr := r.index.PurgeArchiveOlderThan(ctx, cutoff); purgeErr != nil {
		return 0, purgeErr
	}
	return r.ArchiveRepository.DeleteOlderThan(ctx, cutoff)
}

func (r *IndexingArchiveRepository) DeleteAll(ctx context.Context) (int64, error) {
	if purgeErr := r.index.PurgeArchiveAll(ctx); purgeErr != nil {
		return 0, purgeErr
	}
	return r.ArchiveRepository.DeleteAll(ctx)
}

func (a *IndexingTurnAppender) Append(ctx context.Context, turn conversation.Turn) error {
	if a == nil || a.next == nil {
		return fmt.Errorf("memoryindex: archive appender unavailable")
	}
	if err := a.next.Append(ctx, turn); err != nil {
		return err
	}
	if a.index != nil {
		if eligible, reason := ArchiveTurnEligibility(turn); !eligible {
			slog.Debug("compact_archive_skipped", "role", turn.Role, "reason", reason)
			return nil
		}
		if turn.ID == 0 {
			turn = a.persistedTurn(ctx, turn)
		}
		if doc, ok := ArchiveDocument(turn); ok {
			if a.freshnessStore != nil {
				a.stampAndBump(ctx, &doc)
			}
			_ = a.index.Upsert(ctx, doc)
		}
	}
	return nil
}

// stampAndBump sets freshness columns on doc and calls BumpPending when the
// content hash differs from what is already stored. Freshness tracking is
// best-effort: errors are logged at Debug and do not fail the Append.
// Uses doc.Kind as the "role" argument to ContentHash so hashes are consistent
// with the Rebuild path (which also uses Kind).
func (a *IndexingTurnAppender) stampAndBump(ctx context.Context, doc *Document) {
	projRow, found, err := a.freshnessStore.Get(ctx, compactMemoryProjectionID)
	if err != nil || !found {
		return
	}
	newHash := ContentHash(doc.Kind, doc.Body, a.embeddingModelID)
	doc.ContentHash = newHash
	doc.EmbeddingModelID = a.embeddingModelID
	doc.IndexBuildID = projRow.IndexBuildID

	// Detect hash change against the currently-stored row.
	var oldHash string
	if a.index != nil && a.index.db != nil {
		row := a.index.db.QueryRowContext(ctx,
			`SELECT content_hash FROM compact_memory_documents WHERE id = ?`, doc.ID)
		_ = row.Scan(&oldHash)
	}
	if oldHash != newHash {
		if err := a.freshnessStore.BumpPending(ctx, compactMemoryProjectionID, 1); err != nil {
			slog.Debug("memoryindex: freshness bump failed on append", "doc_id", doc.ID, "error", err)
		} else {
			slog.Debug("memoryindex: freshness pending bumped on hash change", "doc_id", doc.ID)
		}
	}
}

func (a *IndexingTurnAppender) persistedTurn(ctx context.Context, turn conversation.Turn) conversation.Turn {
	reader, ok := a.next.(conversation.ChatTurnReader)
	if !ok || turn.ChatID == 0 {
		return turn
	}
	turns, err := reader.ListByChat(ctx, turn.ChatID, 8)
	if err != nil {
		return turn
	}
	for _, candidate := range turns {
		if candidate.TurnIndex == turn.TurnIndex && candidate.Role == turn.Role {
			return candidate
		}
	}
	return turn
}

// ArchiveTurnEligibility reports whether a persisted conversation turn should
// be written to compact_memory_documents. reason is a stable snake_case token,
// empty when eligible. The raw conversations table remains lossless; this gate
// only protects retrieval-facing compact archive rows.
func ArchiveTurnEligibility(turn conversation.Turn) (eligible bool, reason string) {
	if eligible, reason := ArchiveEligibility(turn.Role, turn.Content); !eligible {
		return eligible, reason
	}
	if turn.Role == "assistant" && strings.TrimSpace(turn.ToolCalls) != "" {
		return false, "assistant_tool_calls_excluded"
	}
	return true, ""
}

// ArchiveEligibility reports whether a role/content pair should be written to
// compact_memory_documents. reason is a stable snake_case token, empty when eligible.
// Assistant tool-call rows require ArchiveTurnEligibility because the tool-call
// JSON lives outside content.
func ArchiveEligibility(role, _ string) (eligible bool, reason string) {
	switch role {
	case "tool":
		return false, "role_tool_excluded"
	case "system":
		return false, "role_system_excluded"
	default:
		return true, ""
	}
}

func compactForIndex(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if limit > 0 && len(value) > limit {
		value = value[:limit]
	}
	return strings.TrimSpace(value)
}
