package handlers

import (
	"context"
	"time"

	"github.com/chetto1983/aura/internal/learningretention"
)

// KindLearningCompaction is the system-seeded learned-example compactor.
const KindLearningCompaction TaskKind = "learning_compaction"

const learningCompactionMaxDuration = 5 * time.Minute

type learningCompactor interface {
	CompactBatch(context.Context) (learningretention.Report, error)
}

type learningCompactionHandler struct{ compactor learningCompactor }

// NewLearningCompactionHandler constructs the scheduled compaction owner.
func NewLearningCompactionHandler(compactor learningCompactor) Handler {
	return &learningCompactionHandler{compactor: compactor}
}

func (h *learningCompactionHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindLearningCompaction, MaxDuration: learningCompactionMaxDuration}
}

func (h *learningCompactionHandler) Run(context.Context, Job) (string, error) {
	return "learning compaction: disabled", nil
}
