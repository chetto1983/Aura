package handlers

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/chetto1983/aura/internal/learningretention"
)

// KindLearningCompaction is the system-seeded learned-example compactor.
const KindLearningCompaction TaskKind = "learning_compaction"

const learningCompactionMaxDuration = 5 * time.Minute

type learningCompactor interface {
	CompactBatch(context.Context) (learningretention.Report, error)
}

type learningCompactionHandler struct {
	compactor learningCompactor
	running   atomic.Bool
}

// NewLearningCompactionHandler constructs the scheduled compaction owner.
func NewLearningCompactionHandler(compactor learningCompactor) Handler {
	return &learningCompactionHandler{compactor: compactor}
}

func (h *learningCompactionHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindLearningCompaction, MaxDuration: learningCompactionMaxDuration}
}

func (h *learningCompactionHandler) Run(ctx context.Context, _ Job) (string, error) {
	if h.compactor == nil {
		return "learning compaction: disabled (no compactor)", nil
	}
	if !h.running.CompareAndSwap(false, true) {
		return "learning compaction: skipped (already running)", nil
	}
	defer h.running.Store(false)
	report, err := h.compactor.CompactBatch(ctx)
	if err != nil {
		return "", fmt.Errorf("learning compaction: %w", err)
	}
	return fmt.Sprintf("learning compaction ok: scanned %d, expired %d, evicted %d",
		report.Scanned, report.Expired, report.Evicted), nil
}
