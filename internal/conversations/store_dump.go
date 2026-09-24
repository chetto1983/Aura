package conversations

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Dump is the owner's raw record of one conversation (prd.md §7): every persisted turn of
// every branch plus each branch's compaction summary. It carries reasoning, so it must
// never feed the llm.Message rebuild — only the export renderer reads it.
type Dump struct {
	Turns       []DumpTurn
	Compactions []DumpCompaction
}

// DumpTurn is one aura.conversation_turns row as persisted, sidecar content rehydrated.
// ParentSeq is 0 at the root, where the column is NULL.
type DumpTurn struct {
	Seq                 int
	Role                string
	Content             string
	ToolCallID          string
	ToolCalls           []byte
	Reasoning           string
	ReasoningDurationMS int64
	BranchID            string
	ParentSeq           int
	AttachmentIDs       []string
	DeliveryKey         string
	InputTokens         int
	OutputTokens        int
	CachedTokens        int
	ContextTokens       int
	CreatedAt           time.Time
}

// DumpCompaction is one branch's durable summary of its earlier turns.
type DumpCompaction struct {
	BranchID         string
	CoversThroughSeq int
	SourceTurns      int
	Summary          string
	Model            string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// LoadDump reads the whole conversation in one identity-scoped transaction. Unlike
// LoadHistory it applies no tool-pair repair: an orphaned result is part of the record.
func (s *Store) LoadDump(ctx context.Context, conversationID string) (Dump, error) {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return Dump{}, fmt.Errorf("load dump: %w", err)
	}
	var turnRows []sqlc.ListTurnDumpRow
	var compactionRows []sqlc.ListConversationCompactionsRow
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		var qErr error
		if turnRows, qErr = q.ListTurnDump(ctx, id); qErr != nil {
			return fmt.Errorf("load dump turns %s: %w", conversationID, qErr)
		}
		if compactionRows, qErr = q.ListConversationCompactions(ctx, id); qErr != nil {
			return fmt.Errorf("load dump compactions %s: %w", conversationID, qErr)
		}
		return nil
	}); err != nil {
		return Dump{}, err
	}
	d := Dump{
		Turns:       make([]DumpTurn, 0, len(turnRows)),
		Compactions: make([]DumpCompaction, 0, len(compactionRows)),
	}
	for _, r := range turnRows {
		t := dumpTurnFromRow(r)
		if r.ContentSidecarPath.String != "" {
			data, rerr := s.readTurnSidecar(conversationID, t.Seq)
			if rerr != nil {
				return Dump{}, fmt.Errorf("load dump %s seq %d: read sidecar: %w", conversationID, t.Seq, rerr)
			}
			t.Content = string(data)
		}
		d.Turns = append(d.Turns, t)
	}
	for _, r := range compactionRows {
		d.Compactions = append(d.Compactions, DumpCompaction{
			BranchID:         uuid.UUID(r.BranchID.Bytes).String(),
			CoversThroughSeq: int(r.CoversThroughSeq),
			SourceTurns:      int(r.SourceTurns),
			Summary:          r.Summary,
			Model:            r.Model,
			CreatedAt:        r.CreatedAt.Time.UTC(),
			UpdatedAt:        r.UpdatedAt.Time.UTC(),
		})
	}
	return d, nil
}

func dumpTurnFromRow(r sqlc.ListTurnDumpRow) DumpTurn {
	return DumpTurn{
		Seq:                 int(r.Seq),
		Role:                r.Role,
		Content:             r.Content.String,
		ToolCallID:          r.ToolCallID.String,
		ToolCalls:           r.ToolCalls,
		Reasoning:           r.Reasoning.String,
		ReasoningDurationMS: r.ReasoningDurationMs.Int64,
		BranchID:            uuid.UUID(r.BranchID.Bytes).String(),
		ParentSeq:           int(r.ParentSeq.Int32),
		AttachmentIDs:       uuidStrings(r.AttachmentIds),
		DeliveryKey:         r.DeliveryKey.String,
		InputTokens:         int(r.InputTokens),
		OutputTokens:        int(r.OutputTokens),
		CachedTokens:        int(r.CachedTokens),
		ContextTokens:       int(r.ContextTokens),
		CreatedAt:           r.CreatedAt.Time.UTC(),
	}
}

// uuidStrings drops an element that failed to scan rather than emitting the zero uuid,
// which would send the reader looking for a row that cannot exist.
func uuidStrings(raw []pgtype.UUID) []string {
	out := make([]string, 0, len(raw))
	for _, u := range raw {
		if !u.Valid {
			continue
		}
		out = append(out, uuid.UUID(u.Bytes).String())
	}
	return out
}
