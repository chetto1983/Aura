package conversations

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

const (
	maxManagedHistoryRows      = 10000
	maxManagedHistoryBytes     = 32 << 20
	maxManagedSidecarBytes     = 8 << 20
	managedHistoryNoBranchLeaf = 0
)

// ErrManagedHistoryWorkLimit refuses pathological resume work instead of returning a
// partial transcript that looks successful.
var ErrManagedHistoryWorkLimit = errors.New("managed conversation history exceeds its safe loading limit")

// ErrManagedHistoryPath reports a broken or cyclic selected-branch parent chain.
var ErrManagedHistoryPath = errors.New("managed conversation branch path is invalid")

type managedHistoryLoad struct {
	turns []Turn
	cache compactionCache
}

type compactionSnapshot struct {
	delegate compactionCache
	stored   Compaction
	readable bool
	writable bool
}

func (c *compactionSnapshot) LoadCompaction(context.Context, string, string) (Compaction, bool, error) {
	return c.stored, c.readable, nil
}

func (c *compactionSnapshot) SaveCompaction(
	ctx context.Context, conversationID, branchID string, value Compaction,
) error {
	if !c.writable || c.delegate == nil {
		return nil
	}
	return c.delegate.SaveCompaction(ctx, conversationID, branchID, value)
}

type managedHistoryWork struct {
	rows        int
	inlineBytes int64
}

func (w *managedHistoryWork) add(turn Turn) error {
	w.rows++
	w.inlineBytes += int64(len(turn.Role) + len(turn.Content) + len(turn.ContentSidecarPath) +
		len(turn.ToolCallID) + len(turn.ToolCalls))
	if w.rows > maxManagedHistoryRows || w.inlineBytes > maxManagedHistoryBytes {
		return fmt.Errorf("%w (rows=%d/%d, inline_bytes=%d/%d)",
			ErrManagedHistoryWorkLimit, w.rows, maxManagedHistoryRows,
			w.inlineBytes, maxManagedHistoryBytes)
	}
	return nil
}

func (w *managedHistoryWork) pageSize(configured int) int {
	return min(configured, maxManagedHistoryRows-w.rows+1)
}

func (s *Store) loadManagedLinearTurns(
	ctx context.Context,
	conversationID, branchID string,
	pageSize, evictAfter int,
) (managedHistoryLoad, error) {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return managedHistoryLoad{}, fmt.Errorf("load managed turns: %w", err)
	}
	stored, hasStored := loadStoredCompaction(ctx, s, conversationID, branchID)
	work := &managedHistoryWork{}
	var head, descending []Turn
	foundBoundary := false
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		rows, qErr := q.ListManagedHistoryHead(ctx, id)
		if qErr != nil {
			return fmt.Errorf("load managed history head %s: %w", conversationID, qErr)
		}
		for _, row := range rows {
			turn := turnFromRow(sqlc.ListTurnsBySeqRow(row))
			if err := work.add(turn); err != nil {
				return err
			}
			head = append(head, turn)
		}
		cursor := int32(math.MaxInt32)
		for {
			rows, pageErr := q.ListManagedTurnsPageBySeq(ctx, sqlc.ListManagedTurnsPageBySeqParams{
				TargetConversationID: id,
				CursorSeq:            cursor,
				PageSize:             int32(work.pageSize(pageSize)),
			})
			if pageErr != nil {
				return fmt.Errorf("load managed turns page %s before %d: %w", conversationID, cursor, pageErr)
			}
			if len(rows) == 0 {
				break
			}
			for _, row := range rows {
				turn := turnFromRow(sqlc.ListTurnsBySeqRow(row))
				if err := work.add(turn); err != nil {
					return err
				}
				if usableCompactionBoundary(stored, hasStored) && turn.Seq == stored.CoversThroughSeq {
					foundBoundary = true
					break
				}
				descending = append(descending, turn)
			}
			if foundBoundary {
				break
			}
			last := rows[len(rows)-1].Seq
			if last <= 1 {
				break
			}
			cursor = last - 1
		}
		return nil
	}); err != nil {
		return managedHistoryLoad{}, err
	}
	return s.finishManagedHistory(ctx, conversationID, head, descending, stored, hasStored,
		foundBoundary, evictAfter, work)
}

func (s *Store) loadManagedBranchTurns(
	ctx context.Context,
	conversationID, branchID string,
	leafSeq, pageSize, evictAfter int,
) (managedHistoryLoad, error) {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return managedHistoryLoad{}, fmt.Errorf("load managed branch turns: %w", err)
	}
	stored, hasStored := loadStoredCompaction(ctx, s, conversationID, branchID)
	work := &managedHistoryWork{}
	var head, descending []Turn
	foundBoundary := false
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		rows, qErr := q.ListManagedHistoryHead(ctx, id)
		if qErr != nil {
			return fmt.Errorf("load managed branch head %s: %w", conversationID, qErr)
		}
		for _, row := range rows {
			turn := turnFromRow(sqlc.ListTurnsBySeqRow(row))
			if err := work.add(turn); err != nil {
				return err
			}
			head = append(head, turn)
		}
		if leafSeq <= managedHistoryNoBranchLeaf || leafSeq > math.MaxInt32 {
			return nil
		}
		cursor := int32(leafSeq)
		seen := make(map[int32]struct{})
		for {
			rows, pageErr := q.ListManagedBranchPathPage(ctx, sqlc.ListManagedBranchPathPageParams{
				TargetConversationID: id,
				CursorSeq:            cursor,
				PageSize:             int32(work.pageSize(pageSize)),
			})
			if pageErr != nil {
				return fmt.Errorf("load managed branch page %s at %d: %w", conversationID, cursor, pageErr)
			}
			if len(rows) == 0 {
				return fmt.Errorf("%w: seq %d is absent", ErrManagedHistoryPath, cursor)
			}
			for _, row := range rows {
				if _, duplicate := seen[row.Seq]; duplicate {
					return fmt.Errorf("%w: cycle repeats seq %d", ErrManagedHistoryPath, row.Seq)
				}
				seen[row.Seq] = struct{}{}
				if row.ParentSeq.Valid && (row.ParentSeq.Int32 <= 0 || row.ParentSeq.Int32 >= row.Seq) {
					return fmt.Errorf("%w: seq %d has non-older parent %d",
						ErrManagedHistoryPath, row.Seq, row.ParentSeq.Int32)
				}
				turn := managedBranchPageTurn(row)
				if err := work.add(turn); err != nil {
					return err
				}
				if usableCompactionBoundary(stored, hasStored) && turn.Seq == stored.CoversThroughSeq {
					foundBoundary = true
					break
				}
				if turn.Role != "system" {
					descending = append(descending, turn)
				}
			}
			if foundBoundary {
				break
			}
			last := rows[len(rows)-1]
			if !last.ParentSeq.Valid {
				break
			}
			cursor = last.ParentSeq.Int32
		}
		return nil
	}); err != nil {
		return managedHistoryLoad{}, err
	}
	return s.finishManagedHistory(ctx, conversationID, head, descending, stored, hasStored,
		foundBoundary, evictAfter, work)
}

func usableCompactionBoundary(stored Compaction, ok bool) bool {
	return ok && stored.CoversThroughSeq > 0 && strings.TrimSpace(stored.Summary) != ""
}

func (s *Store) finishManagedHistory(
	ctx context.Context,
	conversationID string,
	head, descending []Turn,
	stored Compaction,
	hasStored, foundBoundary bool,
	evictAfter int,
	work *managedHistoryWork,
) (managedHistoryLoad, error) {
	slices.Reverse(descending)
	turns := make([]Turn, 0, len(head)+len(descending)+1)
	turns = append(turns, head...)
	if foundBoundary {
		turns = append(turns, compactionTurn(stored.Summary, stored.CoversThroughSeq))
	}
	turns = append(turns, descending...)
	turns = applyL1(turns, evictAfter)
	if err := rehydrateManagedSidecars(ctx, s, conversationID, turns); err != nil {
		return managedHistoryLoad{}, err
	}
	cache := &compactionSnapshot{
		delegate: s,
		stored:   stored,
		readable: foundBoundary,
		writable: !hasStored || foundBoundary,
	}
	return managedHistoryLoad{turns: turns, cache: cache}, nil
}

func rehydrateManagedSidecars(ctx context.Context, store *Store, conversationID string, turns []Turn) error {
	inlineBytes := int64(0)
	for _, turn := range turns {
		inlineBytes += int64(len(turn.Content) + len(turn.ToolCalls) + len(turn.ToolCallID))
	}
	if inlineBytes > maxManagedHistoryBytes {
		return fmt.Errorf("%w (model_bytes=%d/%d)",
			ErrManagedHistoryWorkLimit, inlineBytes, maxManagedHistoryBytes)
	}
	remaining := int64(maxManagedHistoryBytes) - inlineBytes
	for i := range turns {
		if turns[i].ContentSidecarPath == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if remaining <= 0 {
			return fmt.Errorf("%w (no bytes remain for sidecar seq %d)",
				ErrManagedHistoryWorkLimit, turns[i].Seq)
		}
		reserve := func(size int64) error {
			if size > remaining {
				return fmt.Errorf("%w (sidecar_bytes exceed remaining %d)",
					ErrManagedHistoryWorkLimit, remaining)
			}
			remaining -= size
			return nil
		}
		readLimit := min(int64(maxManagedSidecarBytes), remaining)
		data, err := store.readTurnSidecarReserved(
			conversationID, turns[i].Seq, readLimit, reserve)
		if err != nil {
			return fmt.Errorf("load managed turns %s seq %d: read sidecar: %w",
				conversationID, turns[i].Seq, err)
		}
		turns[i].Content = string(data)
	}
	return nil
}

func managedBranchPageTurn(row sqlc.ListManagedBranchPathPageRow) Turn {
	return turnFromRow(sqlc.ListTurnsBySeqRow{
		ConversationID:     row.ConversationID,
		Seq:                row.Seq,
		Role:               row.Role,
		Content:            row.Content,
		ContentSidecarPath: row.ContentSidecarPath,
		ToolCallID:         row.ToolCallID,
		ToolCalls:          row.ToolCalls,
		CreatedAt:          row.CreatedAt,
		InputTokens:        row.InputTokens,
		OutputTokens:       row.OutputTokens,
		CachedTokens:       row.CachedTokens,
	})
}
