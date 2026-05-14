package memoryindex

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/source"
)

const (
	sourceIndexReadLimit  = 12000
	archiveIndexScanLimit = 500
	proposalIndexLimit    = 500
	sourceSnippetLimit    = 2400
	archiveSnippetLimit   = 1600
	proposalSnippetLimit  = 1200
)

var sourcePageHeadingRE = regexp.MustCompile(`(?m)^## Page ([0-9]+)\s*$`)

type RebuildInput struct {
	Sources    source.Repository
	Archive    conversation.TurnReader
	Proposals  summarizer.ProposalLister
	SkipVector bool
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
			if doc, ok := ArchiveDocument(turn); ok {
				docs = append(docs, doc)
			}
		}
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
	return report, nil
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
	next  conversation.TurnAppender
	index *Store
}

func NewIndexingTurnAppender(next conversation.TurnAppender, index *Store) *IndexingTurnAppender {
	return &IndexingTurnAppender{next: next, index: index}
}

type IndexingArchiveRepository struct {
	conversation.ArchiveRepository
	index *Store
}

func NewIndexingArchiveRepository(next conversation.ArchiveRepository, index *Store) conversation.ArchiveRepository {
	if next == nil || index == nil {
		return next
	}
	return &IndexingArchiveRepository{ArchiveRepository: next, index: index}
}

func (r *IndexingArchiveRepository) Append(ctx context.Context, turn conversation.Turn) error {
	appender := IndexingTurnAppender{next: r.ArchiveRepository, index: r.index}
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
		if turn.ID == 0 {
			turn = a.persistedTurn(ctx, turn)
		}
		if doc, ok := ArchiveDocument(turn); ok {
			_ = a.index.Upsert(ctx, doc)
		}
	}
	return nil
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

func compactForIndex(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if limit > 0 && len(value) > limit {
		value = value[:limit]
	}
	return strings.TrimSpace(value)
}
